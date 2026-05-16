package govpx

import (
	"bytes"
	"testing"

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
