package govpx

import "testing"

// TestRateControlAccumulatesKeyFrameOverspend pins the
// vp8_adjust_key_frame_context post-pack overspend split: 7/8 to
// kf_overspend_bits, 1/8 to gf_overspend_bits when single-layer. With
// keyFrameCount==1 the libvpx bootstrap uses key_freq -- with no
// configured frequency the estimate is 1, so kf_bitrate_adjustment
// equals kf_overspend_bits.
func TestRateControlAccumulatesKeyFrameOverspend(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  30,
		bitsPerFrame:      1000,
		bufferLevelBits:   500,
		maximumBufferBits: 5000,
	}
	// 2000 bytes = 16000 bits; perFrameBandwidth=1000 -> overspend=15000.
	rc.postEncodeFrameWithPacketContext(2000, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	if got := rc.kfOverspendBits; got != 15000*7/8 {
		t.Fatalf("kfOverspendBits = %d, want %d (7/8 of 15000)", got, 15000*7/8)
	}
	if got := rc.gfOverspendBits; got != 15000/8 {
		t.Fatalf("gfOverspendBits = %d, want %d (1/8 of 15000)", got, 15000/8)
	}
	// First keyframe -> kf_bitrate_adjustment = kf_overspend_bits / 1.
	if rc.kfBitrateAdjustment != rc.kfOverspendBits {
		t.Fatalf("kfBitrateAdjustment = %d, want %d", rc.kfBitrateAdjustment, rc.kfOverspendBits)
	}
}

func TestRateControlKeyFrameOverspendSeedsGoldenRecoveryAdjustment(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlVBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		bufferLevelBits:       500,
		maximumBufferBits:     5000,
		framesTillGFUpdateDue: libvpxDefaultGFInterval,
		outputFrameRate:       30,
	}
	rc.postEncodeFrameWithPacketContext(2000, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	if rc.kfBitrateAdjustment != (15000*7/8)/61 {
		t.Fatalf("kfBitrateAdjustment = %d, want two-second bootstrap drain %d",
			rc.kfBitrateAdjustment, (15000*7/8)/61)
	}
	if rc.nonGFBitrateAdjustment != (15000/8)/libvpxDefaultGFInterval {
		t.Fatalf("nonGFBitrateAdjustment = %d, want key-as-golden drain %d",
			rc.nonGFBitrateAdjustment, (15000/8)/libvpxDefaultGFInterval)
	}
}

func TestRateControlTemporalKeyFrameOverspendUsesLayerFrameRate(t *testing.T) {
	rc := rateControlState{
		mode:                          RateControlCBR,
		minQuantizer:                  4,
		maxQuantizer:                  56,
		currentQuantizer:              30,
		bitsPerFrame:                  23333,
		currentTemporalLayers:         3,
		currentLayerPerFrameBandwidth: 18666,
		currentLayerOutputFrameRate:   7,
		bufferLevelBits:               500,
		maximumBufferBits:             5000,
		outputFrameRate:               30,
		keyFrameCount:                 1,
	}
	rc.postEncodeFrameWithPacketContext(3685, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	const overspend = 3685*8 - 18666
	if rc.kfOverspendBits != overspend {
		t.Fatalf("temporal kfOverspendBits = %d, want %d", rc.kfOverspendBits, overspend)
	}
	if got, want := rc.kfBitrateAdjustment, overspend/(1+7*2); got != want {
		t.Fatalf("temporal kfBitrateAdjustment = %d, want layer-rate drain %d", got, want)
	}
	if rc.outputFrameRate != 30 {
		t.Fatalf("outputFrameRate after temporal overspend = %d, want restored 30", rc.outputFrameRate)
	}
}

// TestRateControlUndersizeKeyFrameSkipsOverspend pins the libvpx guard:
// when projected_frame_size <= per_frame_bandwidth, neither
// kf_overspend_bits nor gf_overspend_bits accumulate.
func TestRateControlUndersizeKeyFrameSkipsOverspend(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  30,
		bitsPerFrame:      4000,
		bufferLevelBits:   500,
		maximumBufferBits: 5000,
	}
	rc.postEncodeFrameWithPacketContext(100, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	}) // 800 bits < 4000.
	if rc.kfOverspendBits != 0 || rc.gfOverspendBits != 0 || rc.kfBitrateAdjustment != 0 {
		t.Fatalf("undersize KF accumulated overspend: kf=%d gf=%d adj=%d",
			rc.kfOverspendBits, rc.gfOverspendBits, rc.kfBitrateAdjustment)
	}
}

func TestRateControlUndersizeKeyFrameRefreshesGoldenRecoveryAdjustment(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       30,
		bitsPerFrame:           4000,
		gfOverspendBits:        20080,
		nonGFBitrateAdjustment: 2510,
		framesTillGFUpdateDue:  10,
		bufferLevelBits:        500,
		maximumBufferBits:      5000,
	}
	rc.postEncodeFrameWithPacketContext(100, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	}) // 800 bits < 4000.
	if rc.kfOverspendBits != 0 || rc.gfOverspendBits != 20080 {
		t.Fatalf("undersize KF overspend changed: kf=%d gf=%d", rc.kfOverspendBits, rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 2008 {
		t.Fatalf("nonGFBitrateAdjustment = %d, want existing gf overspend / key GF interval", rc.nonGFBitrateAdjustment)
	}
}

func TestRateControlKeyFrameOverspendUsesDecimationBoostedBandwidth(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlCBR,
		minQuantizer:        4,
		maxQuantizer:        56,
		currentQuantizer:    30,
		bitsPerFrame:        1000,
		dropFrameAllowed:    true,
		decimationFactor:    1,
		bufferLevelBits:     500,
		maximumBufferBits:   5000,
		framesSinceKeyframe: 1,
		outputFrameRate:     30,
		keyFrameCount:       1,
	}
	// vp8_check_drop_buffer boosts cpi->per_frame_bandwidth by 3/2 before
	// key-frame encode too. Keys are not dropped, but post-pack
	// vp8_adjust_key_frame_context compares against that boosted bandwidth.
	rc.postEncodeFrameWithPacketContext(250, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	const overspend = 250*8 - 1500
	if rc.kfOverspendBits != overspend*7/8 {
		t.Fatalf("kfOverspendBits = %d, want boosted-bandwidth split %d", rc.kfOverspendBits, overspend*7/8)
	}
	if rc.gfOverspendBits != overspend/8 {
		t.Fatalf("gfOverspendBits = %d, want boosted-bandwidth split %d", rc.gfOverspendBits, overspend/8)
	}
}

// TestRateControlKFOverspendDrainsBeforeGFOverspend pins libvpx's
// ordering inside calc_pframe_target_size: kf_overspend recovery is
// applied first against per_frame_bandwidth, then gf_overspend is drained
// against the post-KF target. min_frame_target=per_frame_bandwidth/4
// caps both adjustments.
func TestRateControlKFOverspendDrainsBeforeGFOverspend(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       30,
		bitsPerFrame:           1000,
		bufferLevelBits:        2000,
		bufferOptimalBits:      2000,
		maximumBufferBits:      4000,
		rollingTargetBits:      1000,
		kfOverspendBits:        4000,
		kfBitrateAdjustment:    300,
		gfOverspendBits:        1000,
		nonGFBitrateAdjustment: 100,
	}
	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{
		temporalLayerCount: 1,
	})
	// per_frame_bandwidth=1000, min_frame_target=250.
	// KF drain: 300, residue kfOverspendBits=3700, post-KF target=700.
	// GF drain: 100, residue gfOverspendBits=900, post-GF target=600.
	if rc.kfOverspendBits != 3700 {
		t.Fatalf("kfOverspendBits residue = %d, want 3700", rc.kfOverspendBits)
	}
	if rc.gfOverspendBits != 900 {
		t.Fatalf("gfOverspendBits residue = %d, want 900", rc.gfOverspendBits)
	}
	if rc.frameTargetBits != 600 {
		t.Fatalf("frameTargetBits = %d, want 600 (700-100 after GF drain)", rc.frameTargetBits)
	}
	if rc.interFrameTarget != 600 {
		t.Fatalf("interFrameTarget = %d, want 600", rc.interFrameTarget)
	}
}
