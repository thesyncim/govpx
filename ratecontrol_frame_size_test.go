package govpx

import "testing"

// TestRateControlPickFrameSizeReturnsFalseOnUnderrun pins the libvpx
// vp8_pick_frame_size drop-frame contract: when buffer_level < 0 and
// drop_frames_allowed in CBR, vp8_pick_frame_size returns 0 and the
// frame is skipped. govpx's wrapper returns false in this case and
// internally invokes postDropFrame so the buffer is refunded.
func TestRateControlPickFrameSizeReturnsFalseOnUnderrun(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		bufferLevelBits:   -100,
		bufferOptimalBits: 2000,
		maximumBufferBits: 4000,
		dropFrameAllowed:  true,
		rollingTargetBits: 1000,
	}
	ok := rc.pickFrameSize(false, 0, rateControlFrameContext{temporalLayerCount: 1})
	if ok {
		t.Fatalf("pickFrameSize returned true on buffer underrun, want drop")
	}
	if rc.bufferLevelBits != 900 {
		t.Fatalf("buffer level after drop = %d, want 900 (refund of bitsPerFrame=1000)", rc.bufferLevelBits)
	}
}

// TestRateControlPickFrameSizeReturnsTrueOnHealthyBuffer pins the
// happy-path where vp8_pick_frame_size returns 1 and the frame is
// kept.
func TestRateControlPickFrameSizeReturnsTrueOnHealthyBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		bufferLevelBits:   2000,
		bufferOptimalBits: 2000,
		maximumBufferBits: 4000,
		dropFrameAllowed:  true,
		rollingTargetBits: 1000,
	}
	ok := rc.pickFrameSize(false, 0, rateControlFrameContext{temporalLayerCount: 1})
	if !ok {
		t.Fatalf("pickFrameSize returned false on healthy buffer, want keep")
	}
	if rc.frameTargetBits <= 0 {
		t.Fatalf("frameTargetBits = %d, want positive after pickFrameSize", rc.frameTargetBits)
	}
}

// TestRateControlPickFrameSizeKeyFrameAlwaysKept pins libvpx's contract
// that calc_iframe_target_size never sets drop_frame; vp8_pick_frame_size
// always returns 1 for key frames.
func TestRateControlPickFrameSizeKeyFrameAlwaysKept(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		bufferLevelBits:   -2000,
		bufferOptimalBits: 2000,
		maximumBufferBits: 4000,
		dropFrameAllowed:  true,
	}
	ok := rc.pickFrameSize(true, 1000, rateControlFrameContext{firstFrame: true, temporalLayerCount: 1})
	if !ok {
		t.Fatalf("pickFrameSize on key frame returned false, want kept")
	}
}

func TestRateControlDecimationDropIsNotCBRGated(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlVBR,
		bitsPerFrame:        1000,
		bufferLevelBits:     0,
		bufferOptimalBits:   1000,
		maximumBufferBits:   4000,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		decimationFactor:    0,
		decimationCount:     0,
	}
	rc.prepareDecimationForFrame()
	if rc.decimationFactor != 1 {
		t.Fatalf("VBR decimationFactor = %d, want 1 when buffer is below drop mark", rc.decimationFactor)
	}
	if got := rc.decimationBoostedBitsPerFrame(); got != 1500 {
		t.Fatalf("VBR boosted bits/frame = %d, want 1500", got)
	}
	if rc.checkDropBuffer(false) {
		t.Fatalf("first VBR checkDropBuffer dropped with count=0, want seed only")
	}
	if rc.decimationCount != 1 {
		t.Fatalf("VBR decimationCount = %d, want 1 after seed", rc.decimationCount)
	}
	if !rc.checkDropBuffer(false) {
		t.Fatalf("second VBR checkDropBuffer kept frame, want decimation drop")
	}
}

// TestRateControlEstimateKeyFrameFrequencyBootstraps pins libvpx's
// estimate_keyframe_frequency special case for keyFrameCount==1: the
// bootstrap assumes a keyframe every two seconds, clamped to key_freq only
// when auto_key is enabled.
func TestRateControlEstimateKeyFrameFrequencyBootstraps(t *testing.T) {
	rc := rateControlState{
		keyFrameCount:     1,
		keyFrameFrequency: 999,
		outputFrameRate:   30,
	}
	if got := rc.estimateKeyFrameFrequency(); got != 61 {
		t.Fatalf("first keyframe estimate = %d, want two-second bootstrap 61", got)
	}
	rc = rateControlState{
		keyFrameCount:     1,
		keyFrameFrequency: 24,
		autoKeyFrames:     true,
		outputFrameRate:   30,
	}
	if got := rc.estimateKeyFrameFrequency(); got != 24 {
		t.Fatalf("auto-key first keyframe estimate = %d, want clamped key_freq 24", got)
	}
	rc = rateControlState{keyFrameCount: 1}
	if got := rc.estimateKeyFrameFrequency(); got != 1 {
		t.Fatalf("first keyframe estimate without freq = %d, want 1", got)
	}
	rc = rateControlState{}
	if got := rc.estimateKeyFrameFrequency(); got != 1 {
		t.Fatalf("zero-value first keyframe estimate = %d, want defensive bootstrap 1", got)
	}
}

func TestRateControlApplyConfigSeedsKeyFrameHistory(t *testing.T) {
	var rc rateControlState
	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1}); err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	if rc.keyFrameCount != 1 {
		t.Fatalf("keyFrameCount = %d, want libvpx cold-start seed 1", rc.keyFrameCount)
	}
	for i, got := range rc.priorKeyFrameDistance {
		if got != 30 {
			t.Fatalf("priorKeyFrameDistance[%d] = %d, want output framerate 30", i, got)
		}
	}
}

func TestRateControlForcedKFSecondEstimateUsesSeededHistory(t *testing.T) {
	rc := rateControlState{
		keyFrameCount:         2,
		framesSinceKeyframe:   1,
		priorKeyFrameDistance: [5]int{30, 30, 30, 30, 61},
	}
	got := rc.estimateKeyFrameFrequency()
	want := (1*30 + 2*30 + 3*30 + 4*61 + 5*2) / 15
	if got != want {
		t.Fatalf("second forced-key estimate = %d, want %d from FPS-seeded history", got, want)
	}
}

// TestRateControlUpdatesRecentRefFrameUsage pins libvpx's
// update_golden_frame_stats accumulation: counts add up across the GF
// section, with the immediate post-GF frame (frames_since_golden==1)
// excluded.
func TestRateControlUpdatesRecentRefFrameUsage(t *testing.T) {
	rc := rateControlState{
		framesSinceGolden:         5,
		recentRefFrameUsageIntra:  10,
		recentRefFrameUsageLast:   100,
		recentRefFrameUsageGolden: 5,
		recentRefFrameUsageAltRef: 0,
	}
	rc.updateRecentRefFrameUsage(2, 50, 3, 0)
	if rc.recentRefFrameUsageIntra != 12 ||
		rc.recentRefFrameUsageLast != 150 ||
		rc.recentRefFrameUsageGolden != 8 ||
		rc.recentRefFrameUsageAltRef != 0 {
		t.Fatalf("after update = (%d,%d,%d,%d), want (12,150,8,0)",
			rc.recentRefFrameUsageIntra, rc.recentRefFrameUsageLast,
			rc.recentRefFrameUsageGolden, rc.recentRefFrameUsageAltRef)
	}
	// libvpx skips frames_since_golden <= 1 to suppress the noisy first
	// frame after a GF refresh.
	rc.framesSinceGolden = 1
	rc.updateRecentRefFrameUsage(99, 99, 99, 99)
	if rc.recentRefFrameUsageIntra != 12 {
		t.Fatalf("post-GF frame leaked into recent_ref_frame_usage: got %d, want unchanged 12",
			rc.recentRefFrameUsageIntra)
	}
}

// TestRateControlResetsRecentRefFrameUsageOnGFRefresh pins libvpx's
// {1,1,1,1} reset and gf_active_count = mb_rows*mb_cols on GF refresh.
func TestRateControlResetsRecentRefFrameUsageOnGFRefresh(t *testing.T) {
	rc := rateControlState{
		recentRefFrameUsageIntra:  100,
		recentRefFrameUsageLast:   200,
		recentRefFrameUsageGolden: 50,
		recentRefFrameUsageAltRef: 10,
	}
	rc.resetRecentRefFrameUsage(1500)
	if rc.recentRefFrameUsageIntra != 1 ||
		rc.recentRefFrameUsageLast != 1 ||
		rc.recentRefFrameUsageGolden != 1 ||
		rc.recentRefFrameUsageAltRef != 1 ||
		rc.gfActiveCount != 1500 {
		t.Fatalf("post-reset state = (%d,%d,%d,%d) gfActive=%d, want (1,1,1,1) and 1500",
			rc.recentRefFrameUsageIntra, rc.recentRefFrameUsageLast,
			rc.recentRefFrameUsageGolden, rc.recentRefFrameUsageAltRef, rc.gfActiveCount)
	}
}

// TestVBRMinFrameBandwidthBits pins libvpx's
// `min_frame_bandwidth = av_per_frame_bandwidth * two_pass_vbrmin_section / 100`.
func TestVBRMinFrameBandwidthBits(t *testing.T) {
	if got := vbrMinFrameBandwidthBits(10000, 50); got != 5000 {
		t.Fatalf("vbrMinFrameBandwidthBits(10000,50) = %d, want 5000", got)
	}
	if got := vbrMinFrameBandwidthBits(10000, 0); got != 0 {
		t.Fatalf("vbrMinFrameBandwidthBits(10000,0) = %d, want 0", got)
	}
	// Pick perFrameBandwidth so the int64 product exceeds INT_MAX
	// (2^31-1) but stays well within int64; libvpx clamps to INT_MAX.
	const perFrame = libvpxIntMax / 2
	if got := vbrMinFrameBandwidthBits(perFrame, 100); got != libvpxIntMax/2 {
		t.Fatalf("perFrame=INT_MAX/2 pct=100 = %d, want INT_MAX/2", got)
	}
	if got := vbrMinFrameBandwidthBits(perFrame, 300); got != libvpxIntMax {
		t.Fatalf("overflow guard = %d, want libvpxIntMax", got)
	}
}

// TestLibvpxAutoGoldOnePassRefreshDecision pins the
// vp8/encoder/ratectrl.c calc_pframe_target_size auto_gold one-pass
// refresh decision: refresh GF when this_frame_percent_intra < 15 or
// gf_frame_usage >= 5.
func TestLibvpxAutoGoldOnePassRefreshDecision(t *testing.T) {
	// Low intra triggers refresh regardless of usage.
	if !libvpxAutoGoldOnePassRefreshDecision(10, 100, 900, 0, 0, 0, 1000) {
		t.Fatalf("low intra should trigger GF refresh")
	}
	// High intra with low gf_frame_usage does NOT refresh.
	if libvpxAutoGoldOnePassRefreshDecision(20, 100, 900, 0, 0, 0, 1000) {
		t.Fatalf("high intra with low gf_frame_usage should not refresh")
	}
	// gf_frame_usage = (50+0)*100/1000 = 5 -> refresh.
	if !libvpxAutoGoldOnePassRefreshDecision(20, 100, 850, 50, 0, 0, 1000) {
		t.Fatalf("gf_frame_usage>=5 should trigger refresh")
	}
	// pctGFActive=10 wins over gf_frame_usage=4 -> refresh.
	if !libvpxAutoGoldOnePassRefreshDecision(20, 100, 860, 40, 0, 100, 1000) {
		t.Fatalf("pct_gf_active>=5 should trigger refresh")
	}
	// All-zero ref usage and zero gf_active_count -> no refresh.
	if libvpxAutoGoldOnePassRefreshDecision(20, 0, 0, 0, 0, 0, 1000) {
		t.Fatalf("zero ref usage and gf_active should not refresh at high intra")
	}
}

// TestRateControlEstimateKeyFrameFrequencyWeightedAverage pins libvpx's
// rolling weighted-average over prior_key_frame_distance with weights
// {1,2,3,4,5}. Seed the buffer with values 10,20,30,40,50 and set
// framesSinceKeyframe=60. After one call, the buffer shifts left and
// the new tail value is the libvpx-equivalent frames_since_key, which is
// `framesSinceKeyframe+1` (govpx's counter excludes the KF's own
// end-of-frame increment that libvpx folds into `cpi->frames_since_key`;
// see the estimateKeyFrameFrequency doc comment). The expected weighted
// average is (1*20 + 2*30 + 3*40 + 4*50 + 5*61) / 15 = 705/15 = 47.
func TestRateControlEstimateKeyFrameFrequencyWeightedAverage(t *testing.T) {
	rc := rateControlState{
		keyFrameCount:         2,
		framesSinceKeyframe:   60,
		priorKeyFrameDistance: [5]int{10, 20, 30, 40, 50},
	}
	got := rc.estimateKeyFrameFrequency()
	want := (1*20 + 2*30 + 3*40 + 4*50 + 5*61) / 15
	if got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}

// TestSelectQuantizerARFRefreshUsesARFTable pins libvpx's
// `cm->refresh_alt_ref_frame` branch in `onyx_if.c:3650-3684`: in single-
// layer one-pass mode an ARF refresh shares the active-best floor with a
// golden refresh and reads gf_high_motion_minq[Q]. The test drives the
// active-best/worst bounds with altRefFrame=true and asserts the floor
// matches libvpxGoldenFrameHighMotionMinQ[Q] (instead of the regular
// inter_minq[Q] floor or the unwarmed minQuantizer floor).
