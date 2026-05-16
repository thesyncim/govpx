package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, width, height)
	if len(cr.segMap) != n {
		t.Fatalf("segMap len = %d, want %d", len(cr.segMap), n)
	}
	if len(cr.lastCodedQMap) != n {
		t.Fatalf("lastCodedQMap len = %d, want %d", len(cr.lastCodedQMap), n)
	}
	// libvpx: vp9_aq_cyclicrefresh.c:49 — last_coded_q_map memset to MAXQ.
	for i, v := range cr.lastCodedQMap {
		if v != vp9dec.MaxQ {
			t.Fatalf("lastCodedQMap[%d] = %d, want %d at alloc", i, v, vp9dec.MaxQ)
		}
	}

	cr.prepareFrame(true, miRows, miCols)
	if !cr.apply {
		t.Fatalf("cyclic refresh apply=false after prepareFrame, want true")
	}
	wantBlockCount := cr.percentRefresh * miRows * miCols / 100
	// libvpx pads up to a whole superblock so the actual target is at
	// least block_count and bounded by block_count + (sb-area - 1).
	if cr.targetNumSegBlocks < wantBlockCount {
		t.Fatalf("targetNumSegBlocks = %d, want at least %d (percent_refresh=%d)",
			cr.targetNumSegBlocks, wantBlockCount, cr.percentRefresh)
	}
	// libvpx's 64x64 superblock rounding means target <= block_count + 63.
	if cr.targetNumSegBlocks > wantBlockCount+vp9CyclicRefreshSuperblockMi*vp9CyclicRefreshSuperblockMi {
		t.Fatalf("targetNumSegBlocks = %d, exceeds block_count + sb-area %d",
			cr.targetNumSegBlocks, wantBlockCount+vp9CyclicRefreshSuperblockMi*vp9CyclicRefreshSuperblockMi)
	}

	// Count BOOST1 entries in the map.
	boosted := 0
	for _, v := range cr.segMap {
		if v == vp9CyclicRefreshSegmentBoost1 {
			boosted++
		}
	}
	if boosted != cr.targetNumSegBlocks {
		t.Fatalf("segMap BOOST1 count = %d, want %d", boosted, cr.targetNumSegBlocks)
	}

	// libvpx rotates sb_index through the frame on each call.
	prevIdx := cr.sbIndex
	cr.prepareFrame(true, miRows, miCols)
	if cr.sbIndex == prevIdx && cr.targetNumSegBlocks > 0 {
		t.Fatalf("sb_index = %d unchanged across frames, want rotation", cr.sbIndex)
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
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, width, height)
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	// Drive the libvpx setup() path with a synthetic post-key state
	// (rate_ratio_qdelta = 2.0 at frames_since_key >= 40).
	cr.vp9CyclicRefreshUpdateParameters(vp9CyclicRefreshUpdateParametersArgs{
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
	if !cr.applyCyclicRefresh {
		t.Fatalf("applyCyclicRefresh=false post-keyframe, want true")
	}
	if cr.rateRatioQdelta != 2.0 {
		t.Fatalf("rate_ratio_qdelta = %v, want 2.0 for frames_since_key=60", cr.rateRatioQdelta)
	}
	cr.vp9CyclicRefreshSetup(vp9CyclicRefreshSetupArgs{
		CurrentVideoFrame: 60,
		BaseQindex:        80,
	})
	if cr.qindexDelta[1] >= 0 {
		t.Fatalf("qindexDelta[1] = %d, want negative boost", cr.qindexDelta[1])
	}
	if cr.qindexDelta[2] >= cr.qindexDelta[1] {
		t.Fatalf("qindexDelta = %d/%d, want segment-2 stronger (more negative) than segment-1",
			cr.qindexDelta[1], cr.qindexDelta[2])
	}
	// libvpx clamps -deltaq to max_qdelta_perc * q / 100 = 60 * 80 / 100 = 48.
	maxDrop := cr.maxQdeltaPerc * 80 / 100
	if -cr.qindexDelta[1] > maxDrop {
		t.Fatalf("qindexDelta[1] = %d, exceeds max_qdelta_perc cap %d",
			cr.qindexDelta[1], -maxDrop)
	}
	if -cr.qindexDelta[2] > maxDrop {
		t.Fatalf("qindexDelta[2] = %d, exceeds max_qdelta_perc cap %d",
			cr.qindexDelta[2], -maxDrop)
	}
	// Cross-check: at frames_since_key < 4 * (100/percent_refresh) = 40,
	// rate_ratio_qdelta should be 3.0 (libvpx vp9_aq_cyclicrefresh.c:516-520).
	cr2 := &vp9CyclicRefreshState{}
	cr2.configure(true, width, height)
	cr2.vp9CyclicRefreshUpdateParameters(vp9CyclicRefreshUpdateParametersArgs{
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
	if cr2.rateRatioQdelta != 3.0 {
		t.Fatalf("post-key rate_ratio_qdelta = %v, want 3.0 for frames_since_key=10",
			cr2.rateRatioQdelta)
	}
}

// TestVP9CyclicRefreshChangesEncodedAtSpeed8RT pins the rule libvpx
// installs in vp9_encoder.c:4262 — when aq_mode == CYCLIC_REFRESH_AQ
// and the frame is non-intra-only, vp9_cyclic_refresh_setup() runs,
// emitting per-segment AltQ deltas that flip the encoded bitstream
// vs the aq-mode=0 baseline at the same CBR target.
func TestVP9CyclicRefreshChangesEncodedAtSpeed8RT(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	makeEnc := func(aq VP9AQMode) (*VP9Encoder, error) {
		return NewVP9Encoder(VP9EncoderOptions{
			Width:              width,
			Height:             height,
			FPS:                30,
			TargetBitrateKbps:  300,
			RateControlModeSet: true,
			RateControlMode:    RateControlCBR,
			Deadline:           DeadlineRealtime,
			CpuUsed:            -8,
			AQMode:             aq,
		})
	}
	baseEnc, err := makeEnc(VP9AQNone)
	if err != nil {
		t.Fatalf("base NewVP9Encoder: %v", err)
	}
	cyclEnc, err := makeEnc(VP9AQCyclicRefresh)
	if err != nil {
		t.Fatalf("cyclic NewVP9Encoder: %v", err)
	}

	dst := make([]byte, 65536)
	src1 := newVP9YCbCrForTest(width, height, 96, 128, 128)
	src2 := newVP9YCbCrForTest(width, height, 116, 128, 128)

	baseKeyLen, err := baseEnc.EncodeInto(src1, dst)
	if err != nil {
		t.Fatalf("base key: %v", err)
	}
	_ = append([]byte(nil), dst[:baseKeyLen]...)
	baseInterLen, err := baseEnc.EncodeInto(src2, dst)
	if err != nil {
		t.Fatalf("base inter: %v", err)
	}
	basePacket := append([]byte(nil), dst[:baseInterLen]...)

	cyclKeyLen, err := cyclEnc.EncodeInto(src1, dst)
	if err != nil {
		t.Fatalf("cyclic key: %v", err)
	}
	_ = append([]byte(nil), dst[:cyclKeyLen]...)
	cyclInterLen, err := cyclEnc.EncodeInto(src2, dst)
	if err != nil {
		t.Fatalf("cyclic inter: %v", err)
	}
	cyclPacket := append([]byte(nil), dst[:cyclInterLen]...)

	if bytes.Equal(basePacket, cyclPacket) {
		t.Fatalf("cyclic refresh encoded == baseline encoded — cyclic refresh must change the bitstream at speed=8 RT")
	}
	if !cyclEnc.cyclicAQ.enabled {
		t.Fatalf("cyclic AQ disabled, want enabled")
	}
	if !cyclEnc.cyclicAQ.apply {
		t.Fatalf("cyclic AQ apply=false after inter frame, want true")
	}
}

// TestVP9CyclicRefreshComputeDeltaqMatchesLibvpxQDeltaTable pins
// compute_deltaq()'s clamping (vp9_aq_cyclicrefresh.c:90-99). The
// returned delta is bounded by -max_qdelta_perc * q / 100.
func TestVP9CyclicRefreshComputeDeltaqMatchesLibvpxQDeltaTable(t *testing.T) {
	cr := &vp9CyclicRefreshState{maxQdeltaPerc: 60}
	for _, q := range []int{16, 32, 64, 100, 150, 200} {
		// Use a high rate ratio so the unclamped delta would exceed the cap.
		delta := cr.vp9CyclicRefreshComputeDeltaq(q, 4.0, false)
		maxDrop := cr.maxQdeltaPerc * q / 100
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
	cr := &vp9CyclicRefreshState{percentRefresh: 10, contentMode: true}
	got := cr.vp9CyclicRefreshSetGoldenUpdate(vp9CyclicRefreshSetGoldenUpdateArgs{
		AvgFrameLowMotion: 90,
		FramesSinceKey:    20,
	})
	if got != 40 {
		t.Fatalf("baseline_gf_interval = %d, want 40 for percent_refresh=10", got)
	}
	got = cr.vp9CyclicRefreshSetGoldenUpdate(vp9CyclicRefreshSetGoldenUpdateArgs{
		RateControlIsVBR:  true,
		AvgFrameLowMotion: 90,
		FramesSinceKey:    20,
	})
	if got != 20 {
		t.Fatalf("VBR baseline_gf_interval = %d, want 20", got)
	}
	got = cr.vp9CyclicRefreshSetGoldenUpdate(vp9CyclicRefreshSetGoldenUpdateArgs{
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
	cr := &vp9CyclicRefreshState{percentRefresh: 10}
	q := 30
	cr.vp9CyclicRefreshLimitQ(50, &q)
	if q != 42 {
		t.Fatalf("q after limit = %d, want 42 (50 - 8)", q)
	}
	q = 48
	cr.vp9CyclicRefreshLimitQ(50, &q)
	if q != 48 {
		t.Fatalf("q after limit = %d, want unchanged 48 when within 8 of previous", q)
	}
}

// TestVP9CyclicRefreshPostencodeCountsActualSegBlocks pins
// vp9_aq_cyclicrefresh.c:271-288 — after encoding, the actual segment
// 1 / 2 counts come from a walk over the segmentation map.
func TestVP9CyclicRefreshPostencodeCountsActualSegBlocks(t *testing.T) {
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, 64, 64)
	cr.prepareFrame(true, 8, 8)
	// Force a known segmentation pattern.
	for i := range cr.segMap {
		switch i % 4 {
		case 0:
			cr.segMap[i] = vp9CyclicRefreshSegmentBoost1
		case 1:
			cr.segMap[i] = vp9CyclicRefreshSegmentBoost2
		default:
			cr.segMap[i] = vp9CyclicRefreshSegmentBase
		}
	}
	cr.vp9CyclicRefreshPostencode(vp9CyclicRefreshPostencodeArgs{})
	if cr.actualNumSeg1Blocks != 16 {
		t.Fatalf("actualNumSeg1Blocks = %d, want 16", cr.actualNumSeg1Blocks)
	}
	if cr.actualNumSeg2Blocks != 16 {
		t.Fatalf("actualNumSeg2Blocks = %d, want 16", cr.actualNumSeg2Blocks)
	}
}

// TestVP9CyclicRefreshResetResize pins
// vp9_aq_cyclicrefresh.c:686-696 — reset on resize zeros the refresh
// map, resets last_coded_q_map to MAXQ, and parks sb_index.
func TestVP9CyclicRefreshResetResize(t *testing.T) {
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, 64, 64)
	for i := range cr.refreshMap {
		cr.refreshMap[i] = 7
	}
	for i := range cr.lastCodedQMap {
		cr.lastCodedQMap[i] = 33
	}
	cr.sbIndex = 5
	cr.counterEncodeMaxqSceneChange = 3
	cr.vp9CyclicRefreshResetResize()
	for i, v := range cr.refreshMap {
		if v != 0 {
			t.Fatalf("refreshMap[%d] = %d, want 0", i, v)
		}
	}
	for i, v := range cr.lastCodedQMap {
		if v != vp9dec.MaxQ {
			t.Fatalf("lastCodedQMap[%d] = %d, want %d", i, v, vp9dec.MaxQ)
		}
	}
	if cr.sbIndex != 0 {
		t.Fatalf("sb_index = %d, want 0 after reset", cr.sbIndex)
	}
	if cr.counterEncodeMaxqSceneChange != 0 {
		t.Fatalf("counter_encode_maxq_scene_change = %d, want 0", cr.counterEncodeMaxqSceneChange)
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
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, width, height)
	miCols := cr.miCols
	if len(cr.consecZeroMv) != cr.miRows*cr.miCols {
		t.Fatalf("consecZeroMv len = %d, want %d", len(cr.consecZeroMv), cr.miRows*cr.miCols)
	}
	// Toggle MV at a fixed SB across 5 frames; pin (mi_row, mi_col) = (1, 2).
	// libvpx clamps consec_zero_mv reset to LAST_FRAME inter blocks
	// only with segment_id <= CR_SEGMENT_ID_BOOST2.
	const miRow, miCol = 1, 2
	const bw, bh = 1, 1
	// Frame 1: zero MV → counter[idx]==1.
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost1)
	idx := miRow*miCols + miCol
	if got := cr.consecZeroMv[idx]; got != 1 {
		t.Fatalf("frame1 counter = %d, want 1 after first zero MV", got)
	}
	// Frame 2: zero MV → counter[idx]==2.
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 2 {
		t.Fatalf("frame2 counter = %d, want 2", got)
	}
	// Frame 3: large MV (libvpx uses |mv|>=8 to reset) → counter[idx]==0.
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		16, 16, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 0 {
		t.Fatalf("frame3 counter = %d, want 0 after large MV", got)
	}
	// Frame 4: zero MV → counter[idx]==1.
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 1 {
		t.Fatalf("frame4 counter = %d, want 1", got)
	}
	// Frame 5: zero MV but block is INTRA → counter unchanged (libvpx
	// vp9_encodeframe.c:6012 gates on is_inter_block).
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.IntraFrame, false, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 1 {
		t.Fatalf("frame5 (intra) counter = %d, want unchanged 1", got)
	}
	// Frame 6: zero MV but ref is GOLDEN_FRAME → counter unchanged
	// (libvpx vp9_encodeframe.c:6012 gates on ref_frame[0] == LAST_FRAME).
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.GoldenFrame, true, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 1 {
		t.Fatalf("frame6 (golden ref) counter = %d, want unchanged 1", got)
	}
	// Frame 7: zero MV but segment_id > BOOST2 → counter unchanged
	// (libvpx vp9_encodeframe.c:6013 gates on segment_id <= BOOST2).
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost2+1)
	if got := cr.consecZeroMv[idx]; got != 1 {
		t.Fatalf("frame7 (seg>BOOST2) counter = %d, want unchanged 1", got)
	}
	// Saturation check: bump up to 255 then once more — should clamp.
	cr.consecZeroMv[idx] = 254
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 255 {
		t.Fatalf("saturation step 1: counter = %d, want 255", got)
	}
	cr.vp9CyclicRefreshUpdateZeroMVCnt(miRow, miCol, bw, bh,
		0, 0, vp9dec.LastFrame, true, vp9CyclicRefreshSegmentBoost1)
	if got := cr.consecZeroMv[idx]; got != 255 {
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
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, width, height)
	miRows := cr.miRows
	miCols := cr.miCols
	// Pre-prime consec_zero_mv: half the frame stationary for many frames,
	// half the frame recently-moved.
	stationary := uint8(120) // > consec_zero_mv_thresh (100 for non-screen).
	moved := uint8(0)
	for r := range miRows {
		for c := range miCols {
			idx := r*miCols + c
			if c < miCols/2 {
				cr.consecZeroMv[idx] = stationary
			} else {
				cr.consecZeroMv[idx] = moved
			}
		}
	}
	// Set last_coded_q_map below qindex_thresh on the whole frame so the
	// |last_coded_q_map > qindex_thresh| branch of the filter doesn't fire.
	for i := range cr.lastCodedQMap {
		cr.lastCodedQMap[i] = 10
	}
	// All blocks start as refresh candidates (refreshMap == 0).
	for i := range cr.refreshMap {
		cr.refreshMap[i] = 0
	}
	// Drive update_map with a high percent_refresh + non-screen content.
	cr.percentRefresh = 100 // try to refresh the whole frame.
	cr.contentMode = true
	cr.vp9CyclicRefreshUpdateMap(cr.consecZeroMv, 50, 100, false)
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
			if cr.segMap[idx] != vp9CyclicRefreshSegmentBoost1 {
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
	cr := &vp9CyclicRefreshState{}
	cr.configure(true, width, height)
	cr.qindexDelta[vp9CyclicRefreshSegmentBase] = 0
	cr.qindexDelta[vp9CyclicRefreshSegmentBoost1] = -20
	cr.qindexDelta[vp9CyclicRefreshSegmentBoost2] = -40
	for i := range cr.lastCodedQMap {
		cr.lastCodedQMap[i] = vp9dec.MaxQ
	}
	// Non-inter-skip block at BOOST1 with base_qindex=100 → q=80.
	cr.vp9CyclicRefreshUpdateSegmentPostencode(0, 0, 1, 1, 100,
		vp9CyclicRefreshSegmentBoost1, true /*inter*/, false /*skip*/)
	if got := cr.lastCodedQMap[0]; got != 80 {
		t.Fatalf("BOOST1 non-skip lastCodedQMap[0] = %d, want 80", got)
	}
	// Pre-fill a low value, then inter-skip BOOST1 at base 200 → min(180, 50)=50.
	cr.lastCodedQMap[1] = 50
	cr.vp9CyclicRefreshUpdateSegmentPostencode(0, 1, 1, 1, 200,
		vp9CyclicRefreshSegmentBoost1, true /*inter*/, true /*skip*/)
	if got := cr.lastCodedQMap[1]; got != 50 {
		t.Fatalf("BOOST1 inter-skip lastCodedQMap[1] = %d, want min(180, 50) = 50", got)
	}
	// Inter-skip BOOST2 at base 100 (q=60) with prior 200 → min(60, 200)=60.
	cr.lastCodedQMap[2] = 200
	cr.vp9CyclicRefreshUpdateSegmentPostencode(0, 2, 1, 1, 100,
		vp9CyclicRefreshSegmentBoost2, true /*inter*/, true /*skip*/)
	if got := cr.lastCodedQMap[2]; got != 60 {
		t.Fatalf("BOOST2 inter-skip lastCodedQMap[2] = %d, want min(60, 200) = 60", got)
	}
}

// TestVP9CyclicRefreshEncoderConsecZeroMVPlumbing pins the end-to-end
// wiring of vp9_encodeframe.c:5999-6022 (update_zeromv_cnt) into the
// govpx encode loop: after encoding a keyframe + an inter frame with
// cyclic AQ active, the per-SB postencode hook has had a chance to
// run for every SB. We seed the consec_zero_mv slice with sentinel
// values, then verify the hook walked every 8x8 block in the frame
// (either resetting on large MVs or bumping on small MVs).
func TestVP9CyclicRefreshEncoderConsecZeroMVPlumbing(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		Deadline:           DeadlineRealtime,
		CpuUsed:            -8,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	src1 := newVP9YCbCrForTest(width, height, 96, 128, 128)
	if _, err := enc.EncodeInto(src1, dst); err != nil {
		t.Fatalf("key: %v", err)
	}
	// Validate the slice is the right size — alloc happened on first frame.
	want := enc.cyclicAQ.miRows * enc.cyclicAQ.miCols
	if len(enc.cyclicAQ.consecZeroMv) != want {
		t.Fatalf("consec_zero_mv len = %d, want mi_rows*mi_cols = %d",
			len(enc.cyclicAQ.consecZeroMv), want)
	}
	// Seed every entry with a sentinel so we can observe whether the
	// per-SB hook touched each (mi_row, mi_col). libvpx's hook either
	// bumps (zero MV) or resets to 0 (large MV) every LAST_FRAME inter
	// block; any non-sentinel value confirms the hook walked the SB.
	const sentinel uint8 = 250
	for i := range enc.cyclicAQ.consecZeroMv {
		enc.cyclicAQ.consecZeroMv[i] = sentinel
	}
	src2 := newVP9YCbCrForTest(width, height, 116, 128, 128)
	if _, err := enc.EncodeInto(src2, dst); err != nil {
		t.Fatalf("inter: %v", err)
	}
	touched := 0
	for _, v := range enc.cyclicAQ.consecZeroMv {
		if v != sentinel {
			touched++
		}
	}
	if touched == 0 {
		t.Fatalf("consec_zero_mv all sentinel after inter frame: per-SB postencode hook not wired into encode loop")
	}
	// At a CBR/RT speed-8 path the encoder is expected to pick LAST_FRAME
	// inter on most blocks of a constant-grey source — touching the
	// full mi_rows*mi_cols footprint.
	if touched < want/2 {
		t.Fatalf("consec_zero_mv hook only touched %d / %d blocks: SB walker incomplete",
			touched, want)
	}
}

// TestVP9CyclicRefreshEncoderPostencodeUpdatesLastCodedQMap pins that
// the per-SB postencode hook in writeVP9ModesTileBounds actually fires
// on encoded inter frames — by checking last_coded_q_map drops below
// MAXQ on at least some blocks after a frame.
func TestVP9CyclicRefreshEncoderPostencodeUpdatesLastCodedQMap(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		Deadline:           DeadlineRealtime,
		CpuUsed:            -8,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	src1 := newVP9YCbCrForTest(width, height, 96, 128, 128)
	src2 := newVP9YCbCrForTest(width, height, 116, 128, 128)
	if _, err := enc.EncodeInto(src1, dst); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, err := enc.EncodeInto(src2, dst); err != nil {
		t.Fatalf("inter: %v", err)
	}
	// After at least one inter frame with cyclic AQ on, last_coded_q_map
	// must show at least one entry below MAXQ — meaning the per-SB
	// postencode hook walked the SB and clamp()-stamped the chosen q.
	below := 0
	for _, v := range enc.cyclicAQ.lastCodedQMap {
		if v < vp9dec.MaxQ {
			below++
		}
	}
	if below == 0 {
		t.Fatalf("last_coded_q_map all MAXQ after inter frame: postencode hook not firing")
	}
	// Sanity: the bsize lookup constants stay aligned.
	if common.MiBlockSize != vp9CyclicRefreshSuperblockMi {
		t.Fatalf("common.MiBlockSize = %d, want %d (mi-per-sb)",
			common.MiBlockSize, vp9CyclicRefreshSuperblockMi)
	}
}
