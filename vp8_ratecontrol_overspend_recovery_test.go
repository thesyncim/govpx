package govpx

import "testing"

// TestRateControlGFOverspendDrainsIntoNextPFrameTarget pins the libvpx
// calc_pframe_target_size GF-overspend recovery branch: starting with
// gf_overspend_bits=2000, non_gf_bitrate_adjustment=200, the next p-frame
// target = per_frame_bandwidth - 200, and the gf_overspend_bits residue is
// 1800. In one-pass mode min_frame_target is per_frame_bandwidth/4.
// The buffered-mode percent_low/percent_high pass is suppressed by
// keeping bufferLevelBits at bufferOptimalBits.
func TestRateControlGFOverspendDrainsIntoNextPFrameTarget(t *testing.T) {
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
		gfOverspendBits:        2000,
		nonGFBitrateAdjustment: 200,
	}
	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{
		temporalLayerCount: 1,
	})
	if rc.frameTargetBits != 800 {
		t.Fatalf("frameTargetBits = %d, want 800 (1000 - 200 GF drain)", rc.frameTargetBits)
	}
	if rc.gfOverspendBits != 1800 {
		t.Fatalf("gfOverspendBits = %d, want 1800 residue", rc.gfOverspendBits)
	}
	if rc.interFrameTarget != 800 {
		t.Fatalf("interFrameTarget = %d, want 800 (recorded after recovery)", rc.interFrameTarget)
	}
}

// TestRateControlOverspendRecoveryClampsAtMinFrameTarget pins the
// one-pass min_frame_target = per_frame_bandwidth/4 floor inside
// calc_pframe_target_size. With kf_bitrate_adjustment far
// exceeding the available headroom, the drain saturates at
// per_frame_bandwidth - min_frame_target and the residue is reduced
// accordingly.
func TestRateControlOverspendRecoveryClampsAtMinFrameTarget(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlCBR,
		minQuantizer:        4,
		maxQuantizer:        56,
		currentQuantizer:    30,
		bitsPerFrame:        1000,
		bufferLevelBits:     2000,
		bufferOptimalBits:   2000,
		maximumBufferBits:   4000,
		rollingTargetBits:   1000,
		kfOverspendBits:     20000,
		kfBitrateAdjustment: 5000,
	}
	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{
		temporalLayerCount: 1,
	})
	// min_frame_target=250, max KF drain=750, residue kfOverspendBits=19250.
	if rc.kfOverspendBits != 19250 {
		t.Fatalf("kfOverspendBits residue = %d, want 19250", rc.kfOverspendBits)
	}
	if rc.frameTargetBits != 250 {
		t.Fatalf("frameTargetBits = %d, want min_frame_target 250", rc.frameTargetBits)
	}
}
