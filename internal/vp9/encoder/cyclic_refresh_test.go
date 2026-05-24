package encoder_test

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9CyclicRefreshSegmentationCounts pins libvpx
// vp9/encoder/vp9_aq_cyclicrefresh.c:379 — block_count =
// percent_refresh * mi_rows * mi_cols / 100 — and the
// vp9_aq_cyclicrefresh.c:401-471 raster-walk that fills the
// segmentation map with BOOST1 entries in superblock order.
func TestVP9CyclicRefreshSegmentationCounts(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	n := miRows * miCols
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, width, height)
	if len(cr.SegMap) != n {
		t.Fatalf("SegMap len = %d, want %d", len(cr.SegMap), n)
	}
	if len(cr.LastCodedQMap) != n {
		t.Fatalf("LastCodedQMap len = %d, want %d", len(cr.LastCodedQMap), n)
	}
	// libvpx: vp9_aq_cyclicrefresh.c:49 — last_coded_q_map memset to MAXQ.
	for i, v := range cr.LastCodedQMap {
		if v != vp9dec.MaxQ {
			t.Fatalf("LastCodedQMap[%d] = %d, want %d at alloc", i, v, vp9dec.MaxQ)
		}
	}

	cr.PrepareFrame(true, miRows, miCols)
	if !cr.Apply {
		t.Fatalf("cyclic refresh Apply=false after prepareFrame, want true")
	}
	wantBlockCount := cr.PercentRefresh * miRows * miCols / 100
	// libvpx pads up to a whole superblock so the actual target is at
	// least block_count and bounded by block_count + (sb-area - 1).
	if cr.TargetNumSegBlocks < wantBlockCount {
		t.Fatalf("TargetNumSegBlocks = %d, want at least %d (percent_refresh=%d)",
			cr.TargetNumSegBlocks, wantBlockCount, cr.PercentRefresh)
	}
	// libvpx's 64x64 superblock rounding means target <= block_count + 63.
	if cr.TargetNumSegBlocks > wantBlockCount+vp9enc.CyclicRefreshSuperblockMI*vp9enc.CyclicRefreshSuperblockMI {
		t.Fatalf("TargetNumSegBlocks = %d, exceeds block_count + sb-area %d",
			cr.TargetNumSegBlocks, wantBlockCount+vp9enc.CyclicRefreshSuperblockMI*vp9enc.CyclicRefreshSuperblockMI)
	}

	// Count BOOST1 entries in the map.
	boosted := 0
	for _, v := range cr.SegMap {
		if v == vp9enc.CyclicRefreshSegmentBoost1 {
			boosted++
		}
	}
	if boosted != cr.TargetNumSegBlocks {
		t.Fatalf("SegMap BOOST1 count = %d, want %d", boosted, cr.TargetNumSegBlocks)
	}

	// libvpx rotates sb_index through the frame on each call.
	prevIdx := cr.SBIndex
	cr.PrepareFrame(true, miRows, miCols)
	if cr.SBIndex == prevIdx && cr.TargetNumSegBlocks > 0 {
		t.Fatalf("sb_index = %d unchanged across frames, want rotation", cr.SBIndex)
	}
}

// TestVP9CyclicRefreshQindexDeltas pins libvpx
// vp9_aq_cyclicrefresh.c:659-675 — qindex_delta[1] =
// compute_deltaq(rc, q, rate_ratio_qdelta); qindex_delta[2] =
// compute_deltaq(rc, q, min(CR_MAX_RATE_TARGET_RATIO,
// 0.1 * rate_boost_fac * rate_ratio_qdelta)).
func TestVP9CyclicRefreshQindexDeltas(t *testing.T) {
	// libvpx vp9_aq_cyclicrefresh.c:546-554 adds a low-resolution branch
	// (width*height <= 352*288) that nudges rate_ratio_qdelta up to 2.5;
	// run the assertion on a non-low-res frame so we exercise the plain
	// post-key 2.0 fall-through.
	const (
		width  = 640
		height = 480
	)
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, width, height)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	// Drive the libvpx setup() path with a synthetic post-key state
	// (rate_ratio_qdelta = 2.0 at frames_since_key >= 40).
	cr.UpdateParameters(vp9enc.CyclicRefreshUpdateParametersArgs{
		Macroblocks:          (miRows * miCols) >> 2,
		FrameIsIntraOnly:     false,
		AvgFrameQindexInter:  80,
		AvgFrameLowMotion:    50,
		FramesSinceKey:       60,
		BestQuality:          0,
		AvgFrameBandwidth:    8000,
		Width:                width,
		Height:               height,
		NumberTemporalLayers: 1,
	})
	if !cr.ApplyCyclicRefresh {
		t.Fatalf("ApplyCyclicRefresh=false post-keyframe, want true")
	}
	if cr.RateRatioQDelta != 2.0 {
		t.Fatalf("rate_ratio_qdelta = %v, want 2.0 for frames_since_key=60", cr.RateRatioQDelta)
	}
	cr.Setup(vp9enc.CyclicRefreshSetupArgs{
		CurrentVideoFrame: 60,
		BaseQindex:        80,
	})
	if cr.QIndexDelta[1] >= 0 {
		t.Fatalf("QIndexDelta[1] = %d, want negative boost", cr.QIndexDelta[1])
	}
	if cr.QIndexDelta[2] >= cr.QIndexDelta[1] {
		t.Fatalf("QIndexDelta = %d/%d, want segment-2 stronger (more negative) than segment-1",
			cr.QIndexDelta[1], cr.QIndexDelta[2])
	}
	// libvpx clamps -deltaq to max_qdelta_perc * q / 100 = 60 * 80 / 100 = 48.
	maxDrop := cr.MaxQDeltaPerc * 80 / 100
	if -cr.QIndexDelta[1] > maxDrop {
		t.Fatalf("QIndexDelta[1] = %d, exceeds max_qdelta_perc cap %d",
			cr.QIndexDelta[1], -maxDrop)
	}
	if -cr.QIndexDelta[2] > maxDrop {
		t.Fatalf("QIndexDelta[2] = %d, exceeds max_qdelta_perc cap %d",
			cr.QIndexDelta[2], -maxDrop)
	}
	// Cross-check: at frames_since_key < 4 * (100/percent_refresh) = 40,
	// rate_ratio_qdelta should be 3.0 (libvpx vp9_aq_cyclicrefresh.c:516-520).
	cr2 := &vp9enc.CyclicRefreshState{}
	cr2.Configure(true, width, height)
	cr2.UpdateParameters(vp9enc.CyclicRefreshUpdateParametersArgs{
		Macroblocks:          (miRows * miCols) >> 2,
		AvgFrameQindexInter:  80,
		AvgFrameLowMotion:    50,
		FramesSinceKey:       10,
		BestQuality:          0,
		AvgFrameBandwidth:    8000,
		Width:                width,
		Height:               height,
		NumberTemporalLayers: 1,
	})
	if cr2.RateRatioQDelta != 3.0 {
		t.Fatalf("post-key rate_ratio_qdelta = %v, want 3.0 for frames_since_key=10",
			cr2.RateRatioQDelta)
	}
}

// TestVP9CyclicRefreshComputeDeltaqMatchesLibvpxQDeltaTable pins
// compute_deltaq()'s clamping (vp9_aq_cyclicrefresh.c:90-99). The
// returned delta is bounded by -max_qdelta_perc * q / 100.
func TestVP9CyclicRefreshComputeDeltaqMatchesLibvpxQDeltaTable(t *testing.T) {
	cr := &vp9enc.CyclicRefreshState{MaxQDeltaPerc: 60}
	for _, q := range []int{16, 32, 64, 100, 150, 200} {
		// Use a high rate ratio so the unclamped delta would exceed the cap.
		delta := cr.ComputeDeltaQ(q, 4.0, false)
		maxDrop := cr.MaxQDeltaPerc * q / 100
		if -delta > maxDrop {
			t.Fatalf("q=%d delta=%d exceeds max_qdelta cap %d", q, delta, -maxDrop)
		}
		if delta > 0 {
			t.Fatalf("q=%d delta=%d positive, want a negative boost", q, delta)
		}
	}
}

// TestVP9CyclicRefreshSetGoldenUpdate pins
// vp9_aq_cyclicrefresh.c:320-334 — set_golden_update picks a
// baseline_gf_interval based on percent_refresh, then clamps by
// avg_frame_low_motion / frames_since_key.
func TestVP9CyclicRefreshSetGoldenUpdate(t *testing.T) {
	cr := &vp9enc.CyclicRefreshState{PercentRefresh: 10, ContentMode: true}
	got := cr.SetGoldenUpdate(vp9enc.CyclicRefreshSetGoldenUpdateArgs{
		AvgFrameLowMotion: 90,
		FramesSinceKey:    20,
	})
	if got != 40 {
		t.Fatalf("baseline_gf_interval = %d, want 40 for percent_refresh=10", got)
	}
	got = cr.SetGoldenUpdate(vp9enc.CyclicRefreshSetGoldenUpdateArgs{
		RateControlIsVBR:  true,
		AvgFrameLowMotion: 90,
		FramesSinceKey:    20,
	})
	if got != 20 {
		t.Fatalf("VBR baseline_gf_interval = %d, want 20", got)
	}
	got = cr.SetGoldenUpdate(vp9enc.CyclicRefreshSetGoldenUpdateArgs{
		AvgFrameLowMotion: 30,
		FramesSinceKey:    60,
	})
	if got != 10 {
		t.Fatalf("low-motion baseline_gf_interval = %d, want 10", got)
	}
}

// TestVP9CyclicRefreshLimitQ pins vp9_aq_cyclicrefresh.c:698-705 —
// when percent_refresh > 0, the frame-level q decrease vs the
// previous frame is capped at 8.
func TestVP9CyclicRefreshLimitQ(t *testing.T) {
	cr := &vp9enc.CyclicRefreshState{PercentRefresh: 10}
	q := 30
	cr.LimitQ(50, &q)
	if q != 42 {
		t.Fatalf("q after limit = %d, want 42 (50 - 8)", q)
	}
	q = 48
	cr.LimitQ(50, &q)
	if q != 48 {
		t.Fatalf("q after limit = %d, want unchanged 48 when within 8 of previous", q)
	}
}

// TestVP9CyclicRefreshPostencodeCountsActualSegBlocks pins
// vp9_aq_cyclicrefresh.c:271-288 — after encoding, the actual segment
// 1 / 2 counts come from a walk over the segmentation map.
func TestVP9CyclicRefreshPostencodeCountsActualSegBlocks(t *testing.T) {
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, 64, 64)
	cr.PrepareFrame(true, 8, 8)
	// Force a known segmentation pattern.
	for i := range cr.SegMap {
		switch i % 4 {
		case 0:
			cr.SegMap[i] = vp9enc.CyclicRefreshSegmentBoost1
		case 1:
			cr.SegMap[i] = vp9enc.CyclicRefreshSegmentBoost2
		default:
			cr.SegMap[i] = vp9enc.CyclicRefreshSegmentBase
		}
	}
	cr.Postencode(vp9enc.CyclicRefreshPostencodeArgs{})
	if cr.ActualNumSeg1Blocks != 16 {
		t.Fatalf("ActualNumSeg1Blocks = %d, want 16", cr.ActualNumSeg1Blocks)
	}
	if cr.ActualNumSeg2Blocks != 16 {
		t.Fatalf("ActualNumSeg2Blocks = %d, want 16", cr.ActualNumSeg2Blocks)
	}
}

// TestVP9CyclicRefreshResetResize pins
// vp9_aq_cyclicrefresh.c:686-696 — reset on resize zeros the refresh
// map, resets last_coded_q_map to MAXQ, and parks sb_index.
func TestVP9CyclicRefreshResetResize(t *testing.T) {
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, 64, 64)
	for i := range cr.RefreshMap {
		cr.RefreshMap[i] = 7
	}
	for i := range cr.LastCodedQMap {
		cr.LastCodedQMap[i] = 33
	}
	cr.SBIndex = 5
	cr.CounterEncodeMaxqSceneChange = 3
	cr.ResetResize()
	for i, v := range cr.RefreshMap {
		if v != 0 {
			t.Fatalf("RefreshMap[%d] = %d, want 0", i, v)
		}
	}
	for i, v := range cr.LastCodedQMap {
		if v != vp9dec.MaxQ {
			t.Fatalf("LastCodedQMap[%d] = %d, want %d", i, v, vp9dec.MaxQ)
		}
	}
	if cr.SBIndex != 0 {
		t.Fatalf("sb_index = %d, want 0 after reset", cr.SBIndex)
	}
	if cr.CounterEncodeMaxqSceneChange != 0 {
		t.Fatalf("counter_encode_maxq_scene_change = %d, want 0", cr.CounterEncodeMaxqSceneChange)
	}
}

// TestVP9CyclicRefreshConsecZeroMVCounterUpdates pins libvpx
// update_zeromv_cnt (vp9/encoder/vp9_encodeframe.c:5999-6022): for an
// inter LAST_FRAME block whose MV magnitude is < 8 on both axes, the
// per-(8x8) consec_zero_mv counter increments (saturating at 255);
// a single large-MV frame at the same block resets the counter to 0.
func TestVP9CyclicRefreshConsecZeroMVCounterUpdates(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, width, height)
	miCols := cr.MICols
	if len(cr.ConsecZeroMV) != cr.MIRows*cr.MICols {
		t.Fatalf("ConsecZeroMV len = %d, want %d", len(cr.ConsecZeroMV), cr.MIRows*cr.MICols)
	}
	// Toggle MV at a fixed SB across 5 frames; pin (mi_row, mi_col) = (1, 2).
	// libvpx clamps consec_zero_mv reset to LAST_FRAME inter blocks
	// only with segment_id <= CR_SEGMENT_ID_BOOST2.
	const miRow, miCol = 1, 2
	const bw, bh = 1, 1
	// Frame 1: zero MV → counter[idx]==1.
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	idx := miRow*miCols + miCol
	if got := cr.ConsecZeroMV[idx]; got != 1 {
		t.Fatalf("frame1 counter = %d, want 1 after first zero MV", got)
	}
	// Frame 2: zero MV → counter[idx]==2.
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 2 {
		t.Fatalf("frame2 counter = %d, want 2", got)
	}
	// Frame 3: large MV (libvpx uses |mv|>=8 to reset) → counter[idx]==0.
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		16, 16, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 0 {
		t.Fatalf("frame3 counter = %d, want 0 after large MV", got)
	}
	// Frame 4: zero MV → counter[idx]==1.
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 1 {
		t.Fatalf("frame4 counter = %d, want 1", got)
	}
	// Frame 5: zero MV but block is INTRA → counter unchanged (libvpx
	// vp9_encodeframe.c:6012 gates on is_inter_block).
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.IntraFrame, false, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 1 {
		t.Fatalf("frame5 (intra) counter = %d, want unchanged 1", got)
	}
	// Frame 6: zero MV but ref is GOLDEN_FRAME → counter unchanged
	// (libvpx vp9_encodeframe.c:6012 gates on ref_frame[0] == LAST_FRAME).
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.GoldenFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 1 {
		t.Fatalf("frame6 (golden ref) counter = %d, want unchanged 1", got)
	}
	// Frame 7: zero MV but segment_id > BOOST2 → counter unchanged
	// (libvpx vp9_encodeframe.c:6013 gates on segment_id <= BOOST2).
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost2+1)
	if got := cr.ConsecZeroMV[idx]; got != 1 {
		t.Fatalf("frame7 (seg>BOOST2) counter = %d, want unchanged 1", got)
	}
	// Saturation check: bump up to 255 then once more — should clamp.
	cr.ConsecZeroMV[idx] = 254
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 255 {
		t.Fatalf("saturation step 1: counter = %d, want 255", got)
	}
	cr.UpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9enc.CyclicRefreshSegmentBoost1)
	if got := cr.ConsecZeroMV[idx]; got != 255 {
		t.Fatalf("saturation clamp: counter = %d, want stays at 255", got)
	}
}

// TestVP9CyclicRefreshEligibilityFilterFiresOnStationaryBlocks pins
// the update_map eligibility filter from libvpx
// vp9_aq_cyclicrefresh.c:437-442: with consec_zero_mv populated, blocks
// that have been stationary for longer than consec_zero_mv_thresh are
// excluded from the refresh candidate pool (eligibility=false), while
// recently-moved blocks stay eligible. This mirrors the libvpx
// behaviour that drives the bulk of the BD-rate win — refresh is steered
// away from already-stable regions and toward changing content.
func TestVP9CyclicRefreshEligibilityFilterFiresOnStationaryBlocks(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, width, height)
	miRows := cr.MIRows
	miCols := cr.MICols
	// Pre-prime consec_zero_mv: half the frame stationary for many frames,
	// half the frame recently-moved.
	stationary := uint8(120) // > consec_zero_mv_thresh (100 for non-screen).
	moved := uint8(0)
	for r := range miRows {
		for c := range miCols {
			idx := r*miCols + c
			if c < miCols/2 {
				cr.ConsecZeroMV[idx] = stationary
			} else {
				cr.ConsecZeroMV[idx] = moved
			}
		}
	}
	// Set last_coded_q_map below qindex_thresh on the whole frame so the
	// |last_coded_q_map > qindex_thresh| branch of the filter doesn't fire.
	for i := range cr.LastCodedQMap {
		cr.LastCodedQMap[i] = 10
	}
	// All blocks start as refresh candidates (RefreshMap == 0).
	for i := range cr.RefreshMap {
		cr.RefreshMap[i] = 0
	}
	// Drive update_map with a high percent_refresh + non-screen content.
	cr.PercentRefresh = 100 // try to refresh the whole frame.
	cr.ContentMode = true
	cr.UpdateMap(cr.ConsecZeroMV, 50, 100, false)
	// The half-superblock heuristic in vp9_aq_cyclicrefresh.c:450 means
	// every SB whose sum_map >= xmis*ymis/2 gets stamped BOOST1. With
	// half the cols stationary, the SB-eligibility split depends on the
	// SB layout (8x8 mi blocks per SB); count seg-blocks to confirm
	// stationarity reduces the refresh population.
	boostedStationary := 0
	boostedMoved := 0
	for r := range miRows {
		for c := range miCols {
			idx := r*miCols + c
			if cr.SegMap[idx] != vp9enc.CyclicRefreshSegmentBoost1 {
				continue
			}
			if c < miCols/2 {
				boostedStationary++
			} else {
				boostedMoved++
			}
		}
	}
	// libvpx's filter sums per-SB; we expect the moved half to dominate
	// the refresh population.
	if boostedMoved <= boostedStationary {
		t.Fatalf("eligibility filter did not steer refresh toward moved blocks: stationary=%d moved=%d",
			boostedStationary, boostedMoved)
	}
}

// TestVP9CyclicRefreshUpdateSbPostencodeUpdatesLastCodedQMap pins the
// update_sb_postencode loop from libvpx vp9_aq_cyclicrefresh.c:225-255:
// each 8x8 block in the SB gets its last_coded_q_map[bl] set to
// clamp(base_qindex + qindex_delta[segment_id], 0, MAXQ) when the
// block is not inter-skip, and the min() of the new and old value
// when the block is inter-skip.
func TestVP9CyclicRefreshUpdateSbPostencodeUpdatesLastCodedQMap(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	cr := &vp9enc.CyclicRefreshState{}
	cr.Configure(true, width, height)
	cr.QIndexDelta[vp9enc.CyclicRefreshSegmentBase] = 0
	cr.QIndexDelta[vp9enc.CyclicRefreshSegmentBoost1] = -20
	cr.QIndexDelta[vp9enc.CyclicRefreshSegmentBoost2] = -40
	for i := range cr.LastCodedQMap {
		cr.LastCodedQMap[i] = vp9dec.MaxQ
	}
	// Non-inter-skip block at BOOST1 with base_qindex=100 → q=80.
	cr.UpdateSegmentPostencode(0, 0, 1, 1, 100,
		vp9enc.CyclicRefreshSegmentBoost1, true /*inter*/, false /*skip*/)
	if got := cr.LastCodedQMap[0]; got != 80 {
		t.Fatalf("BOOST1 non-skip LastCodedQMap[0] = %d, want 80", got)
	}
	// Pre-fill a low value, then inter-skip BOOST1 at base 200 → min(180, 50)=50.
	cr.LastCodedQMap[1] = 50
	cr.UpdateSegmentPostencode(0, 1, 1, 1, 200,
		vp9enc.CyclicRefreshSegmentBoost1, true /*inter*/, true /*skip*/)
	if got := cr.LastCodedQMap[1]; got != 50 {
		t.Fatalf("BOOST1 inter-skip LastCodedQMap[1] = %d, want min(180, 50) = 50", got)
	}
	// Inter-skip BOOST2 at base 100 (q=60) with prior 200 → min(60, 200)=60.
	cr.LastCodedQMap[2] = 200
	cr.UpdateSegmentPostencode(0, 2, 1, 1, 100,
		vp9enc.CyclicRefreshSegmentBoost2, true /*inter*/, true /*skip*/)
	if got := cr.LastCodedQMap[2]; got != 60 {
		t.Fatalf("BOOST2 inter-skip LastCodedQMap[2] = %d, want min(60, 200) = 60", got)
	}
}
