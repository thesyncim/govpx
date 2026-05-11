package govpx

import "testing"

func TestRateControlPostEncodeFactorAccountsForZbinOverQuant(t *testing.T) {
	// When zbin_over_quant scales the frame down at encode time, the post-
	// encode rate correction factor must use the same scaling so it does not
	// over-attribute the size shrink to "Q was higher than needed". Without
	// the libvpx-style zbin adjustment in the projected size, an oversize
	// frame at active zbin_oq damps the factor toward 1.0 even when the next
	// frame should still be biased to higher Q.
	makeRC := func(zbin int) *rateControlState {
		return &rateControlState{
			mode:                   RateControlCBR,
			minQuantizer:           4,
			maxQuantizer:           63,
			currentQuantizer:       127,
			currentZbinOverQuant:   zbin,
			bitsPerFrame:           12000,
			frameTargetBits:        12000,
			bufferOptimalBits:      60000,
			bufferLevelBits:        48000,
			undershootPct:          defaultRateControlUndershootPct,
			overshootPct:           defaultRateControlOvershootPct,
			rateCorrectionFactor:   1.0,
			goldenCorrectionFactor: 1.0,
		}
	}

	noZbin := makeRC(0)
	noZbin.postEncodeFrameWithContext(3000, false, false, 60)
	withZbin := makeRC(32)
	withZbin.postEncodeFrameWithContext(3000, false, false, 60)
	if !(withZbin.rateCorrectionFactor > noZbin.rateCorrectionFactor) {
		t.Fatalf("zbin=32 factor (%g) should exceed zbin=0 factor (%g) for same actual bits",
			withZbin.rateCorrectionFactor, noZbin.rateCorrectionFactor)
	}
}

func TestRateControlUpdatesLibvpxRateCorrectionFactor(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       24,
		bitsPerFrame:           12000,
		frameTargetBits:        12000,
		bufferOptimalBits:      60000,
		bufferLevelBits:        48000,
		undershootPct:          defaultRateControlUndershootPct,
		overshootPct:           defaultRateControlOvershootPct,
		rateCorrectionFactor:   1.0,
		goldenCorrectionFactor: 1.0,
	}

	rc.postEncodeFrameWithContext(3000, false, false, 60)
	if rc.rateCorrectionFactor != 1.25 {
		t.Fatalf("rate correction factor after oversize frame = %g, want 1.25", rc.rateCorrectionFactor)
	}

	rc.currentQuantizer = 24
	rc.postEncodeFrameWithContext(375, false, false, 60)
	if rc.rateCorrectionFactor != 1.0 {
		t.Fatalf("rate correction factor after undersize frame = %g, want 1.0", rc.rateCorrectionFactor)
	}
}

func TestRateControlRTCExternalAlwaysUpdatesCorrectionFactor(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       24,
		bitsPerFrame:           12000,
		frameTargetBits:        12000,
		bufferOptimalBits:      60000,
		bufferLevelBits:        48000,
		undershootPct:          defaultRateControlUndershootPct,
		overshootPct:           defaultRateControlOvershootPct,
		rateCorrectionFactor:   1.0,
		goldenCorrectionFactor: 1.0,
		activeWorstQChanged:    true,
	}

	rc.postEncodeFrameWithPacketContext(3000, rateControlPostEncodeContext{
		macroblocks:        60,
		showFrame:          true,
		alwaysUpdateFactor: true,
	})
	if rc.rateCorrectionFactor != 1.25 {
		t.Fatalf("RTC external rate correction factor = %g, want 1.25", rc.rateCorrectionFactor)
	}
	if rc.activeWorstQChanged {
		t.Fatalf("activeWorstQChanged = true, want cleared after post-encode")
	}
}

func TestRateControlUpdatesSeparateLibvpxCorrectionFactors(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlCBR,
		minQuantizer:             4,
		maxQuantizer:             56,
		currentQuantizer:         24,
		bitsPerFrame:             12000,
		frameTargetBits:          12000,
		bufferOptimalBits:        60000,
		bufferLevelBits:          48000,
		undershootPct:            defaultRateControlUndershootPct,
		overshootPct:             defaultRateControlOvershootPct,
		gfCBRBoostPct:            150,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	rc.postEncodeFrameWithContext(3000, false, true, 60)
	if rc.goldenCorrectionFactor != 1.25 {
		t.Fatalf("golden correction factor = %g, want 1.25", rc.goldenCorrectionFactor)
	}
	if rc.rateCorrectionFactor != 1.0 {
		t.Fatalf("inter correction factor changed to %g, want 1.0", rc.rateCorrectionFactor)
	}

	rc.currentQuantizer = 4
	rc.postEncodeFrameWithContext(20000, true, false, 60)
	if rc.keyFrameCorrectionFactor <= 1.0 {
		t.Fatalf("key correction factor = %g, want increase", rc.keyFrameCorrectionFactor)
	}
	if rc.goldenCorrectionFactor != 1.25 {
		t.Fatalf("golden correction factor changed to %g, want 1.25", rc.goldenCorrectionFactor)
	}
}

func TestRateControlUnboostedGoldenFrameUsesLibvpxInterCorrectionFactor(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       24,
		bitsPerFrame:           12000,
		frameTargetBits:        12000,
		bufferOptimalBits:      60000,
		bufferLevelBits:        48000,
		undershootPct:          defaultRateControlUndershootPct,
		overshootPct:           defaultRateControlOvershootPct,
		gfCBRBoostPct:          100,
		rateCorrectionFactor:   1.0,
		goldenCorrectionFactor: 1.0,
	}

	rc.postEncodeFrameWithContext(3000, false, true, 60)

	if rc.rateCorrectionFactor != 1.25 {
		t.Fatalf("inter correction factor = %g, want 1.25 for gf_noboost_onepass_cbr", rc.rateCorrectionFactor)
	}
	if rc.goldenCorrectionFactor != 1.0 {
		t.Fatalf("golden correction factor = %g, want unchanged 1.0", rc.goldenCorrectionFactor)
	}
}

func TestRateControlBoostedGoldenFrameCorrectionBranchingMirrorsLibvpx(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlCBR,
		minQuantizer:             4,
		maxQuantizer:             56,
		currentQuantizer:         24,
		bitsPerFrame:             12000,
		frameTargetBits:          12000,
		bufferOptimalBits:        60000,
		bufferLevelBits:          48000,
		undershootPct:            defaultRateControlUndershootPct,
		overshootPct:             defaultRateControlOvershootPct,
		gfCBRBoostPct:            101,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	rc.postEncodeFrameWithContext(3000, false, true, 60)

	if rc.goldenCorrectionFactor != 1.25 {
		t.Fatalf("boosted CBR golden correction factor = %g, want 1.25", rc.goldenCorrectionFactor)
	}
	if rc.rateCorrectionFactor != 1.0 {
		t.Fatalf("boosted CBR inter correction factor = %g, want unchanged 1.0", rc.rateCorrectionFactor)
	}
}

func TestRateControlVBRGoldenFrameUsesGoldenCorrectionFactor(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlVBR,
		minQuantizer:             4,
		maxQuantizer:             56,
		currentQuantizer:         24,
		bitsPerFrame:             12000,
		frameTargetBits:          12000,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	rc.postEncodeFrameWithContext(3000, false, true, 60)

	if rc.goldenCorrectionFactor != 1.25 {
		t.Fatalf("VBR golden correction factor = %g, want 1.25", rc.goldenCorrectionFactor)
	}
	if rc.rateCorrectionFactor != 1.0 {
		t.Fatalf("VBR inter correction factor = %g, want unchanged 1.0", rc.rateCorrectionFactor)
	}
}

func TestRateControlCQRegulatesQuantizerAboveCQFloor(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlCQ,
		minQuantizer:             4,
		maxQuantizer:             56,
		cqLevel:                  20,
		currentQuantizer:         20,
		bitsPerFrame:             1000,
		frameTargetBits:          1000,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	rc.selectQuantizerForFrameKind(false, false, 60)
	if rc.currentQuantizer <= rc.cqLevel {
		t.Fatalf("CQ low-bitrate quantizer = %d, want regulated above CQ floor %d", rc.currentQuantizer, rc.cqLevel)
	}

	rc.currentQuantizer = 20
	rc.frameTargetBits = 1 << 20
	rc.selectQuantizerForFrameKind(false, false, 60)
	if rc.currentQuantizer != rc.cqLevel {
		t.Fatalf("CQ high-bitrate quantizer = %d, want CQ floor %d", rc.currentQuantizer, rc.cqLevel)
	}
}

func TestRateControlPostEncodeTracksLibvpxQuantizerAverages(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            56,
		currentQuantizer:        20,
		avgFrameQuantizer:       56,
		normalInterAvgQuantizer: 56,
		bitsPerFrame:            1000,
		frameTargetBits:         1000,
	}

	rc.postEncodeFrameWithContext(125, false, false, 1)

	if rc.avgFrameQuantizer != 47 {
		t.Fatalf("avgFrameQuantizer = %d, want libvpx average 47", rc.avgFrameQuantizer)
	}
	if rc.normalInterFrames != 1 || rc.normalInterAvgQuantizer != 38 {
		t.Fatalf("normal inter average = frames:%d q:%d, want 1/38", rc.normalInterFrames, rc.normalInterAvgQuantizer)
	}
}

func TestRateControlPostEncodeTracksLibvpxRollingBitAverages(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      20,
		bitsPerFrame:          1000,
		frameTargetBits:       3000,
		rollingActualBits:     2000,
		rollingTargetBits:     1000,
		longRollingActualBits: 1600,
		longRollingTargetBits: 3200,
	}

	rc.postEncodeFrameWithContext(500, false, false, 0)

	if rc.rollingActualBits != 2500 || rc.rollingTargetBits != 1500 {
		t.Fatalf("short rolling bits = actual:%d target:%d, want libvpx 2500/1500", rc.rollingActualBits, rc.rollingTargetBits)
	}
	if rc.longRollingActualBits != 1675 || rc.longRollingTargetBits != 3194 {
		t.Fatalf("long rolling bits = actual:%d target:%d, want libvpx 1675/3194", rc.longRollingActualBits, rc.longRollingTargetBits)
	}
}

func TestRateControlPostDropFrameDoesNotUpdateLibvpxRollingBitAverages(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		bitsPerFrame:          1000,
		frameTargetBits:       3000,
		bufferLevelBits:       2000,
		maximumBufferBits:     6000,
		rollingActualBits:     2000,
		rollingTargetBits:     1000,
		longRollingActualBits: 1600,
		longRollingTargetBits: 3200,
	}

	rc.postDropFrame()

	if rc.rollingActualBits != 2000 || rc.rollingTargetBits != 1000 {
		t.Fatalf("short rolling bits after drop = actual:%d target:%d, want unchanged 2000/1000", rc.rollingActualBits, rc.rollingTargetBits)
	}
	if rc.longRollingActualBits != 1600 || rc.longRollingTargetBits != 3200 {
		t.Fatalf("long rolling bits after drop = actual:%d target:%d, want unchanged 1600/3200", rc.longRollingActualBits, rc.longRollingTargetBits)
	}
	if rc.bufferLevelBits != 3000 || rc.framesSinceKeyframe != 1 {
		t.Fatalf("drop accounting = buffer:%d frames:%d, want 3000/1", rc.bufferLevelBits, rc.framesSinceKeyframe)
	}
}

func TestRateControlPostEncodeCarriesLibvpxNegativeBufferDebt(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		frameTargetBits:   1000,
		bufferLevelBits:   500,
		maximumBufferBits: 6000,
	}

	rc.postEncodeFrameWithContext(375, false, false, 0)

	if rc.bufferLevelBits != -1500 {
		t.Fatalf("buffer debt = %d, want libvpx bits_off_target -1500", rc.bufferLevelBits)
	}
}

func TestRateControlScreenContentClampsLibvpxNegativeBufferDebt(t *testing.T) {
	rc := rateControlState{
		bufferLevelBits:   -7000,
		maximumBufferBits: 6000,
	}

	rc.clampScreenContentBufferDebt(0)
	if rc.bufferLevelBits != -7000 {
		t.Fatalf("non-screen buffer debt = %d, want unchanged -7000", rc.bufferLevelBits)
	}

	rc.clampScreenContentBufferDebt(1)
	if rc.bufferLevelBits != -6000 {
		t.Fatalf("screen buffer debt = %d, want libvpx clamp -6000", rc.bufferLevelBits)
	}
}

func TestRateControlDropsOnlyOnLibvpxBufferUnderrun(t *testing.T) {
	rc := rateControlState{
		mode:             RateControlCBR,
		dropFrameAllowed: true,
		bitsPerFrame:     1000,
		frameTargetBits:  1000,
	}

	rc.bufferLevelBits = 0
	if rc.shouldDropInterFrame() {
		t.Fatalf("drop at zero buffer = true, want false until libvpx buffer underrun")
	}
	rc.bufferLevelBits = -1
	if !rc.shouldDropInterFrame() {
		t.Fatalf("drop at negative buffer = false, want true")
	}
}

func TestRateControlInvisibleFrameUsesLibvpxBufferOverheadAccounting(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      20,
		bitsPerFrame:          1000,
		frameTargetBits:       3000,
		bufferLevelBits:       5000,
		maximumBufferBits:     8000,
		framesSinceKeyframe:   9,
		framesSinceGolden:     4,
		framesTillGFUpdateDue: 3,
	}

	rc.postEncodeFrameWithPacketContext(100, rateControlPostEncodeContext{
		macroblocks: 0,
		showFrame:   false,
	})

	if rc.bufferLevelBits != 4200 {
		t.Fatalf("invisible buffer = %d, want previous minus frame size 4200", rc.bufferLevelBits)
	}
	if rc.rollingActualBits != 200 || rc.rollingTargetBits != 750 {
		t.Fatalf("invisible rolling bits = actual:%d target:%d, want libvpx 200/750", rc.rollingActualBits, rc.rollingTargetBits)
	}
	if rc.framesSinceKeyframe != 9 || rc.framesSinceGolden != 4 || rc.framesTillGFUpdateDue != 3 {
		t.Fatalf("invisible counters = framesSinceKey:%d framesSinceGolden:%d framesTillGF:%d, want unchanged 9/4/3",
			rc.framesSinceKeyframe, rc.framesSinceGolden, rc.framesTillGFUpdateDue)
	}
}
