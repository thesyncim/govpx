package govpx

import "testing"

// TestRateControlGoldenFrameAccumulatesOverspend pins the libvpx
// update_golden_frame_stats post-pack accumulation: GF overspend equals
// projected_frame_size - inter_frame_target, and non_gf_bitrate_adjustment
// is gf_overspend_bits / frames_till_gf_update_due. Pre-seed
// inter_frame_target and frames_till_gf_update_due to mirror the state
// the encoder publishes before invoking calc_pframe_target_size for the
// GF refresh frame.
func TestRateControlGoldenFrameAccumulatesOverspend(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		bufferLevelBits:       500,
		maximumBufferBits:     5000,
		interFrameTarget:      900,
		framesTillGFUpdateDue: 10,
	}
	// 500 bytes = 4000 bits. Overspend over inter_frame_target=900 -> 3100.
	rc.postEncodeFrameWithPacketContext(500, rateControlPostEncodeContext{
		goldenFrame: true,
		macroblocks: 1,
		showFrame:   true,
	})
	if rc.gfOverspendBits != 3100 {
		t.Fatalf("gfOverspendBits = %d, want 3100", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 310 {
		t.Fatalf("nonGFBitrateAdjustment = %d, want 310 (3100/10)", rc.nonGFBitrateAdjustment)
	}
	// Golden refresh resets framesSinceGolden to 0.
	if rc.framesSinceGolden != 0 {
		t.Fatalf("framesSinceGolden = %d, want 0", rc.framesSinceGolden)
	}
}

func TestRateControlGoldenFrameUsesStaleZeroInterTarget(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		frameTargetBits:       3000,
		bufferLevelBits:       5000,
		maximumBufferBits:     8000,
		framesTillGFUpdateDue: 10,
	}
	// When a frame refreshes ALTREF in one-pass mode, calc_pframe_target_size
	// skips refreshing inter_frame_target. libvpx's later
	// update_golden_frame_stats uses that stale zero value as-is.
	rc.postEncodeFrameWithPacketContext(500, rateControlPostEncodeContext{
		goldenFrame: true,
		altRefFrame: true,
		showFrame:   true,
	})
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("gfOverspendBits = %d, want full 4000 bits against stale zero inter target", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 400 {
		t.Fatalf("nonGFBitrateAdjustment = %d, want 400", rc.nonGFBitrateAdjustment)
	}
}

func TestRateControlAltRefPacketAccumulatesFullPostPackOverspend(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		frameTargetBits:       3000,
		bufferLevelBits:       5000,
		maximumBufferBits:     8000,
		interFrameTarget:      900,
		framesTillGFUpdateDue: 10,
		framesSinceKeyframe:   9,
		framesSinceGolden:     4,
	}
	// 500 bytes = 4000 bits. Unlike GF refresh, ARF post-pack accounting
	// accumulates the full hidden-frame packet size. Hidden-ARF mode
	// requires autoAltRef=true (libvpx oxcf.play_alternate); without it
	// libvpx's update_alt_ref_frame_stats does not run and govpx mirrors
	// that gate (see vp8_ratecontrol_postencode.go).
	rc.postEncodeFrameWithPacketContext(500, rateControlPostEncodeContext{
		altRefFrame: true,
		autoAltRef:  true,
		macroblocks: 1,
		showFrame:   false,
	})
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("ARF gfOverspendBits = %d, want full packet size 4000", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 400 {
		t.Fatalf("ARF nonGFBitrateAdjustment = %d, want 400 (4000/10)", rc.nonGFBitrateAdjustment)
	}
	if rc.framesSinceKeyframe != 9 || rc.framesSinceGolden != 4 || rc.framesTillGFUpdateDue != 10 {
		t.Fatalf("ARF invisible counters = framesSinceKey:%d framesSinceGolden:%d framesTillGF:%d, want unchanged 9/4/10",
			rc.framesSinceKeyframe, rc.framesSinceGolden, rc.framesTillGFUpdateDue)
	}
}

func TestRateControlPass2SkipsPostPackOverspend(t *testing.T) {
	tests := []struct {
		name string
		ctx  rateControlPostEncodeContext
	}{
		{
			name: "key",
			ctx: rateControlPostEncodeContext{
				keyFrame:    true,
				macroblocks: 1,
				showFrame:   true,
			},
		},
		{
			name: "golden",
			ctx: rateControlPostEncodeContext{
				goldenFrame: true,
				macroblocks: 1,
				showFrame:   true,
			},
		},
		{
			name: "altref",
			ctx: rateControlPostEncodeContext{
				altRefFrame: true,
				macroblocks: 1,
				showFrame:   false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := rateControlState{
				mode:                  RateControlCBR,
				minQuantizer:          4,
				maxQuantizer:          56,
				currentQuantizer:      30,
				bitsPerFrame:          1000,
				frameTargetBits:       3000,
				bufferLevelBits:       5000,
				maximumBufferBits:     8000,
				interFrameTarget:      900,
				framesTillGFUpdateDue: 10,
			}
			tt.ctx.skipPostPackOverspend = true
			rc.postEncodeFrameWithPacketContext(500, tt.ctx)
			if rc.kfOverspendBits != 0 || rc.gfOverspendBits != 0 ||
				rc.kfBitrateAdjustment != 0 || rc.nonGFBitrateAdjustment != 0 ||
				rc.keyFrameCount != 0 {
				t.Fatalf("pass2 %s overspend state = kf:%d gf:%d kfAdj:%d gfAdj:%d keyCount:%d, want all zero",
					tt.name, rc.kfOverspendBits, rc.gfOverspendBits,
					rc.kfBitrateAdjustment, rc.nonGFBitrateAdjustment, rc.keyFrameCount)
			}
		})
	}
}

// TestRateControlAccumulatesPostPackAltRefOverspend pins the libvpx
// update_alt_ref_frame_stats branch: unlike GF refresh (which
// accumulates projected_frame_size - inter_frame_target), the ARF
// accumulates the full projected_frame_size into gf_overspend_bits,
// then divides by frames_till_gf_update_due for the drain rate.
func TestRateControlAccumulatesPostPackAltRefOverspend(t *testing.T) {
	rc := rateControlState{
		framesTillGFUpdateDue: 10,
	}
	// 4000 bits goes fully into gf_overspend_bits.
	rc.accumulatePostPackAltRefOverspend(4000, false)
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("gf_overspend_bits after ARF = %d, want 4000", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 400 {
		t.Fatalf("non_gf_bitrate_adjustment = %d, want 400 (4000/10)",
			rc.nonGFBitrateAdjustment)
	}
}

// TestRateControlAccumulatesPostPackAltRefOverspendNoUpdateDueZeroDrain
// pins the libvpx `if (frames_till_gf_update_due > 0)` guard on the
// non_gf_bitrate_adjustment update.
func TestRateControlAccumulatesPostPackAltRefOverspendNoUpdateDueZeroDrain(t *testing.T) {
	rc := rateControlState{framesTillGFUpdateDue: 0}
	rc.accumulatePostPackAltRefOverspend(4000, false)
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("gf_overspend_bits = %d, want 4000", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 0 {
		t.Fatalf("non_gf_bitrate_adjustment with no update due = %d, want 0",
			rc.nonGFBitrateAdjustment)
	}
}

// TestRateControlAccumulatesPostPackAltRefOverspendIgnoresZeroBits pins
// the libvpx `if (actualBits > 0)` guard.
func TestRateControlAccumulatesPostPackAltRefOverspendIgnoresZeroBits(t *testing.T) {
	rc := rateControlState{framesTillGFUpdateDue: 10}
	rc.accumulatePostPackAltRefOverspend(0, false)
	if rc.gfOverspendBits != 0 {
		t.Fatalf("zero ARF bits should not accumulate: %d", rc.gfOverspendBits)
	}
}

func TestOnePassAltRefRefreshUsesStaleTargetWithoutOverspendDrain(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		bitsPerFrame:           23333,
		frameTargetBits:        18769,
		bufferLevelBits:        400000,
		bufferOptimalBits:      3500000,
		undershootPct:          100,
		overshootPct:           100,
		kfOverspendBits:        10419,
		kfBitrateAdjustment:    400,
		gfOverspendBits:        1099,
		nonGFBitrateAdjustment: 156,
		interFrameTarget:       18769,
	}

	rc.beginOnePassAltRefRefreshFrameWithTargetAndContext(rc.bitsPerFrame, rateControlFrameContext{})

	if rc.frameTargetBits != 10511 {
		t.Fatalf("frameTargetBits = %d, want stale target shaped by buffer to 10511", rc.frameTargetBits)
	}
	if rc.kfOverspendBits != 10419 || rc.gfOverspendBits != 1099 {
		t.Fatalf("overspend bits = kf:%d gf:%d, want unchanged 10419/1099",
			rc.kfOverspendBits, rc.gfOverspendBits)
	}
	if rc.interFrameTarget != 18769 {
		t.Fatalf("interFrameTarget = %d, want stale 18769", rc.interFrameTarget)
	}
}

func TestOnePassAltRefRefreshAppliesMinFrameTargetFloor(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		bitsPerFrame:           40000,
		frameTargetBits:        8970,
		bufferLevelBits:        2955906,
		bufferOptimalBits:      6000000,
		undershootPct:          100,
		overshootPct:           100,
		kfOverspendBits:        3761,
		kfBitrateAdjustment:    63,
		gfOverspendBits:        -111931,
		nonGFBitrateAdjustment: 55,
		interFrameTarget:       10000,
	}

	rc.beginOnePassAltRefRefreshFrameWithTargetAndContext(rc.bitsPerFrame, rateControlFrameContext{})

	if rc.frameTargetBits != 7500 {
		t.Fatalf("frameTargetBits = %d, want stale target floored then buffer-shaped to 7500", rc.frameTargetBits)
	}
	if rc.kfOverspendBits != 3761 || rc.gfOverspendBits != -111931 {
		t.Fatalf("overspend bits = kf:%d gf:%d, want unchanged 3761/-111931",
			rc.kfOverspendBits, rc.gfOverspendBits)
	}
	if rc.interFrameTarget != 10000 {
		t.Fatalf("interFrameTarget = %d, want unchanged stale 10000", rc.interFrameTarget)
	}
}
