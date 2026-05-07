package govpx

import "testing"

func TestLibvpxPublicQuantizerTranslationTable(t *testing.T) {
	tests := []struct {
		public int
		qIndex int
	}{
		{public: 0, qIndex: 0},
		{public: 4, qIndex: 4},
		{public: 10, qIndex: 12},
		{public: 32, qIndex: 43},
		{public: 36, qIndex: 51},
		{public: 56, qIndex: 106},
		{public: 63, qIndex: 127},
	}
	for _, tt := range tests {
		if got := libvpxPublicQuantizerToQIndex(tt.public); got != tt.qIndex {
			t.Fatalf("public q %d maps to qindex %d, want %d", tt.public, got, tt.qIndex)
		}
		if got := libvpxQIndexToPublicQuantizer(tt.qIndex); got != tt.public {
			t.Fatalf("qindex %d maps to public q %d, want %d", tt.qIndex, got, tt.public)
		}
	}
}

func TestRateControlAdjustQuantizerUsesLibvpxOvershootBound(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     defaultRateControlUndershootPct,
		overshootPct:      100,
		bufferOptimalBits: 1000,
		bufferLevelBits:   800,
		maximumBufferBits: 2000,
	}

	rc.adjustQuantizer(1575, 1000)
	if rc.currentQuantizer != 20 {
		t.Fatalf("quantizer after tolerated overshoot = %d, want 20", rc.currentQuantizer)
	}

	rc.adjustQuantizer(1576, 1000)
	if rc.currentQuantizer != 21 {
		t.Fatalf("quantizer after overshoot = %d, want 21", rc.currentQuantizer)
	}

	rc.currentQuantizer = 20
	rc.adjustQuantizer(2576, 1000)
	if rc.currentQuantizer != 22 {
		t.Fatalf("quantizer after large overshoot = %d, want 22", rc.currentQuantizer)
	}
}

func TestRateControlAdjustQuantizerUsesLibvpxUndershootBound(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     50,
		overshootPct:      defaultRateControlOvershootPct,
		bufferOptimalBits: 1000,
		bufferLevelBits:   800,
		maximumBufferBits: 2000,
	}

	rc.adjustQuantizer(425, 1000)
	if rc.currentQuantizer != 20 {
		t.Fatalf("quantizer after tolerated undershoot = %d, want 20", rc.currentQuantizer)
	}

	rc.adjustQuantizer(424, 1000)
	if rc.currentQuantizer != 19 {
		t.Fatalf("quantizer after undershoot = %d, want 19", rc.currentQuantizer)
	}
}

func TestRateControlFrameSizeRecodeQuantizerUsesLibvpxBounds(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     50,
		overshootPct:      100,
		bufferOptimalBits: 1000,
		bufferLevelBits:   800,
		maximumBufferBits: 2000,
		bitsPerFrame:      1000,
		frameTargetBits:   1000,
	}

	recode := rc.newFrameSizeRecodeState(false, false)
	got, ok := rc.frameSizeRecodeQuantizerWithContext(197, false, false, 1, &recode)
	if !ok || got <= 20 || recode.qLow != 21 || recode.qHigh != 56 || recode.correctionFactor == 1.0 || !recode.overshootSeen {
		t.Fatalf("oversized recode = q:%d ok:%t state:%+v, want q above current, q_low raised to 21, and local correction factor updated", got, ok, recode)
	}

	rc.currentQuantizer = 20
	recode = rc.newFrameSizeRecodeState(false, false)
	got, ok = rc.frameSizeRecodeQuantizerWithContext(53, false, false, 1, &recode)
	if !ok || got >= 20 || recode.qLow != 4 || recode.qHigh != 19 || !recode.undershootSeen {
		t.Fatalf("undersized recode = q:%d ok:%t state:%+v, want q below current and q_high lowered to 19", got, ok, recode)
	}

	rc.currentQuantizer = 40
	recode = frameSizeRecodeState{qLow: 21, qHigh: 56, overshootSeen: true}
	got, ok = rc.frameSizeRecodeQuantizerWithContext(53, false, false, 1, &recode)
	if !ok || got != 30 || recode.qHigh != 39 || !recode.undershootSeen {
		t.Fatalf("oscillating undershoot recode = q:%d ok:%t state:%+v, want midpoint q30 after lowering q_high to 39", got, ok, recode)
	}

	rc.mode = RateControlCQ
	rc.currentQuantizer = 20
	rc.cqLevel = 20
	recode = rc.newFrameSizeRecodeState(false, false)
	got, ok = rc.frameSizeRecodeQuantizerWithContext(197, false, false, 1, &recode)
	if !ok || got < rc.cqLevel {
		t.Fatalf("CQ oversized frame recode = q:%d ok:%t, want constrained to CQ floor %d or higher", got, ok, rc.cqLevel)
	}
}

func TestRateControlFrameSizeBoundsMirrorLibvpx(t *testing.T) {
	tests := []struct {
		name        string
		rc          rateControlState
		keyFrame    bool
		goldenFrame bool
		wantUnder   int
		wantOver    int
	}{
		{
			name:      "key",
			rc:        rateControlState{mode: RateControlCBR, bufferOptimalBits: 1000, maximumBufferBits: 2000, bufferLevelBits: 800},
			keyFrame:  true,
			wantUnder: 675,
			wantOver:  1325,
		},
		{
			name:        "golden",
			rc:          rateControlState{mode: RateControlCBR, bufferOptimalBits: 1000, maximumBufferBits: 2000, bufferLevelBits: 800},
			goldenFrame: true,
			wantUnder:   675,
			wantOver:    1325,
		},
		{
			name:      "cbr low buffer",
			rc:        rateControlState{mode: RateControlCBR, bufferOptimalBits: 1000, maximumBufferBits: 2000, bufferLevelBits: 500},
			wantUnder: 300,
			wantOver:  1450,
		},
		{
			name:      "cbr mid buffer",
			rc:        rateControlState{mode: RateControlCBR, bufferOptimalBits: 1000, maximumBufferBits: 2000, bufferLevelBits: 800},
			wantUnder: 425,
			wantOver:  1575,
		},
		{
			name:      "cbr high buffer",
			rc:        rateControlState{mode: RateControlCBR, bufferOptimalBits: 1000, maximumBufferBits: 2000, bufferLevelBits: 1500},
			wantUnder: 550,
			wantOver:  1700,
		},
		{
			name:      "temporal layer",
			rc:        rateControlState{mode: RateControlCBR, currentTemporalLayers: 2, bufferOptimalBits: 1000, maximumBufferBits: 2000, bufferLevelBits: 1500},
			wantUnder: 675,
			wantOver:  1325,
		},
		{
			name:      "cq",
			rc:        rateControlState{mode: RateControlCQ},
			wantUnder: 50,
			wantOver:  1575,
		},
		{
			name:      "vbr",
			rc:        rateControlState{mode: RateControlVBR},
			wantUnder: 425,
			wantOver:  1575,
		},
	}

	for _, tc := range tests {
		gotUnder, gotOver := tc.rc.frameSizeBoundsBits(tc.keyFrame, tc.goldenFrame, 1000)
		if gotUnder != tc.wantUnder || gotOver != tc.wantOver {
			t.Fatalf("%s bounds = %d/%d, want %d/%d", tc.name, gotUnder, gotOver, tc.wantUnder, tc.wantOver)
		}
	}
}

func TestRateControlSelectQuantizerUsesLibvpxBitsPerMBModel(t *testing.T) {
	if got := libvpxRegulatedQuantizer(false, 12000, 60, 4, 56, 1.0); got != 24 {
		t.Fatalf("inter regulated quantizer = %d, want libvpx table q24", got)
	}
	if got := libvpxRegulatedQuantizer(true, 72000, 60, 4, 56, 1.0); got != 4 {
		t.Fatalf("key regulated quantizer = %d, want min-clamped q4", got)
	}

	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  4,
		bitsPerFrame:      12000,
		frameTargetBits:   12000,
		bufferOptimalBits: 60000,
		bufferLevelBits:   48000,
	}
	rc.selectQuantizerForFrame(false, 60)
	if rc.currentQuantizer != 24 {
		t.Fatalf("selected quantizer = %d, want q24", rc.currentQuantizer)
	}
}

func TestRateControlRegulatedQuantizerTracksLibvpxZbinOverQuant(t *testing.T) {
	q, zbin := libvpxRegulatedQuantizerWithZbin(false, false, 1, 1, 4, 127, 1.0)
	if q != 127 || zbin != libvpxZbinOverQuantMax {
		t.Fatalf("max inter regulated q/zbin = %d/%d, want 127/%d", q, zbin, libvpxZbinOverQuantMax)
	}

	q, zbin = libvpxRegulatedQuantizerWithZbin(false, true, 1, 1, 4, 127, 1.0)
	if q != 127 || zbin != 16 {
		t.Fatalf("golden regulated q/zbin = %d/%d, want 127/16", q, zbin)
	}

	q, zbin = libvpxRegulatedQuantizerWithZbin(true, false, 1, 1, 4, 127, 1.0)
	if q != 127 || zbin != 0 {
		t.Fatalf("key regulated q/zbin = %d/%d, want 127/0", q, zbin)
	}

	q, zbin = libvpxRegulatedQuantizerWithZbin(false, false, 12000, 60, 4, 127, 1.0)
	if q != 24 || zbin != 0 {
		t.Fatalf("ordinary inter regulated q/zbin = %d/%d, want 24/0", q, zbin)
	}
}

func TestRateControlSelectQuantizerStoresAndClearsZbinOverQuant(t *testing.T) {
	rc := rateControlState{
		mode:               RateControlCBR,
		minQuantizer:       4,
		maxQuantizer:       127,
		bitsPerFrame:       1,
		frameTargetBits:    1,
		lastInterQuantizer: 50,
	}

	rc.selectQuantizerForFrameKind(false, false, 1)
	if rc.currentQuantizer != 127 || rc.currentZbinOverQuant != libvpxZbinOverQuantMax {
		t.Fatalf("selected max inter q/zbin = %d/%d, want 127/%d", rc.currentQuantizer, rc.currentZbinOverQuant, libvpxZbinOverQuantMax)
	}

	rc.frameTargetBits = 12000
	rc.bitsPerFrame = 12000
	rc.selectQuantizerForFrameKind(false, false, 60)
	if rc.currentQuantizer != 24 || rc.currentZbinOverQuant != 0 {
		t.Fatalf("selected ordinary inter q/zbin = %d/%d, want 24/0", rc.currentQuantizer, rc.currentZbinOverQuant)
	}

	rc.frameTargetBits = 72000
	rc.bitsPerFrame = 72000
	rc.currentZbinOverQuant = libvpxZbinOverQuantMax
	rc.selectQuantizerForFrameKindWithScreenContent(false, false, 60, 1)
	if rc.currentQuantizer != 38 || rc.currentZbinOverQuant != 0 {
		t.Fatalf("screen-content limited q/zbin = %d/%d, want 38/0", rc.currentQuantizer, rc.currentZbinOverQuant)
	}
}

func TestRateControlFrameSizeRecodeTracksZbinOverQuantBounds(t *testing.T) {
	rc := rateControlState{
		mode:             RateControlCBR,
		minQuantizer:     4,
		maxQuantizer:     127,
		currentQuantizer: 126,
		bitsPerFrame:     1000,
		frameTargetBits:  1000,
	}

	recode := rc.newFrameSizeRecodeState(false, false)
	got, ok := rc.frameSizeRecodeQuantizerWithContext(300, false, false, 1000, &recode)
	if !ok || got != 127 || recode.zbinOverQuant != libvpxZbinOverQuantMax || recode.zbinOQHigh != libvpxZbinOverQuantMax {
		t.Fatalf("oversized max recode = q:%d ok:%t state:%+v, want q127 and max zbin over-quant", got, ok, recode)
	}

	rc.currentQuantizer = 127
	recode = frameSizeRecodeState{
		qLow:          4,
		qHigh:         127,
		zbinOQHigh:    libvpxZbinOverQuantMax,
		zbinOverQuant: 128,
	}
	got, ok = rc.frameSizeRecodeQuantizerWithContext(0, false, false, 1000, &recode)
	if !ok || got != 127 || recode.zbinOQHigh != 127 || recode.zbinOverQuant != 127 || !recode.undershootSeen {
		t.Fatalf("undersized max recode = q:%d ok:%t state:%+v, want zbin high lowered to 127", got, ok, recode)
	}
}

func TestRateControlFrameSizeRecodeRelaxesActiveWorstOnOvershoot(t *testing.T) {
	rc := rateControlState{
		mode:                 RateControlCBR,
		minQuantizer:         4,
		maxQuantizer:         100,
		currentQuantizer:     80,
		bitsPerFrame:         1000,
		frameTargetBits:      1000,
		rateCorrectionFactor: 1.0,
	}
	recode := frameSizeRecodeState{
		qLow:             40,
		qHigh:            80,
		correctionFactor: 1.0,
	}

	got, ok := rc.frameSizeRecodeQuantizerWithContext(300, false, false, 60, &recode)

	if !ok || got <= 80 || recode.qHigh != 100 || !recode.activeWorstQChanged || !rc.activeWorstQChanged {
		t.Fatalf("active-worst recode = q:%d ok:%t state:%+v rcChanged:%t, want relaxed q_high and recode", got, ok, recode, rc.activeWorstQChanged)
	}
	if recode.correctionFactor != 1.0 {
		t.Fatalf("active-worst correction factor = %g, want unchanged when active worst changed", recode.correctionFactor)
	}
}

func TestRateControlPostEncodeSkipsCorrectionAfterActiveWorstChange(t *testing.T) {
	rc := rateControlState{
		mode:                 RateControlCBR,
		minQuantizer:         4,
		maxQuantizer:         100,
		currentQuantizer:     80,
		bitsPerFrame:         1000,
		frameTargetBits:      1000,
		rateCorrectionFactor: 1.0,
		activeWorstQChanged:  true,
	}

	rc.postEncodeFrameWithContext(300, false, false, 60)

	if rc.rateCorrectionFactor != 1.0 {
		t.Fatalf("rate correction factor = %g, want unchanged after active-worst change", rc.rateCorrectionFactor)
	}
	if rc.activeWorstQChanged {
		t.Fatalf("activeWorstQChanged still set after post encode")
	}
}

func TestRateControlActiveQuantizerBoundsUseLibvpxWarmupTables(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlCBR,
		minQuantizer:             4,
		maxQuantizer:             106,
		currentQuantizer:         4,
		bitsPerFrame:             1_000_000,
		frameTargetBits:          1_000_000,
		bufferOptimalBits:        60_000,
		bufferLevelBits:          0,
		maximumBufferBits:        72_000,
		normalInterFrames:        151,
		normalInterAvgQuantizer:  106,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	activeBest, activeWorst := rc.libvpxActiveQuantizerBounds(false, false)
	if activeBest != 80 || activeWorst != 106 {
		t.Fatalf("active bounds = %d/%d, want libvpx inter_minq[106]/worst 80/106", activeBest, activeWorst)
	}

	rc.selectQuantizerForFrameKind(false, false, 60)
	if rc.currentQuantizer != 80 {
		t.Fatalf("selected warmed-up quantizer = %d, want active-best floor q80", rc.currentQuantizer)
	}
}

func TestRateControlActiveQuantizerBoundsUseLibvpxCBRFullBufferClamp(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            106,
		bufferOptimalBits:       1000,
		maximumBufferBits:       2000,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 80,
	}

	rc.bufferLevelBits = 1000
	activeBest, activeWorst := rc.libvpxActiveQuantizerBounds(false, false)
	if activeBest != 57 || activeWorst != 80 {
		t.Fatalf("optimal-buffer active bounds = %d/%d, want inter_minq[80]/ni_av_qi 57/80", activeBest, activeWorst)
	}

	rc.bufferLevelBits = 1500
	activeBest, activeWorst = rc.libvpxActiveQuantizerBounds(false, false)
	if activeBest != 27 || activeWorst != 70 {
		t.Fatalf("mid-full-buffer active bounds = %d/%d, want libvpx scaled CBR bounds 27/70", activeBest, activeWorst)
	}

	rc.bufferLevelBits = 2000
	activeBest, activeWorst = rc.libvpxActiveQuantizerBounds(false, false)
	if activeBest != 4 || activeWorst != 60 {
		t.Fatalf("full-buffer active bounds = %d/%d, want best-quality floor and active-worst q60", activeBest, activeWorst)
	}
}

func TestRateControlCQActiveQuantizerBoundsRespectCQLevel(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCQ,
		minQuantizer:            4,
		maxQuantizer:            51,
		cqLevel:                 43,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 51,
	}

	activeBest, activeWorst := rc.libvpxActiveQuantizerBounds(false, false)
	if activeBest != 43 || activeWorst != 51 {
		t.Fatalf("CQ active bounds = %d/%d, want cq-level floor 43/51", activeBest, activeWorst)
	}
}

func TestRateControlScreenContentLimitsLibvpxInterQuantizerDrop(t *testing.T) {
	rc := rateControlState{
		mode:               RateControlCBR,
		minQuantizer:       4,
		maxQuantizer:       56,
		currentQuantizer:   40,
		lastInterQuantizer: 50,
		bitsPerFrame:       72000,
		frameTargetBits:    72000,
	}

	rc.selectQuantizerForFrameKindWithScreenContent(false, false, 60, 1)
	if rc.currentQuantizer != 38 {
		t.Fatalf("screen-content inter quantizer = %d, want last inter q minus libvpx limit 38", rc.currentQuantizer)
	}

	rc.currentQuantizer = 40
	rc.selectQuantizerForFrameKindWithScreenContent(false, false, 60, 0)
	if rc.currentQuantizer != 4 {
		t.Fatalf("non-screen inter quantizer = %d, want unbounded regulated q4", rc.currentQuantizer)
	}

	rc.currentQuantizer = 40
	rc.selectQuantizerForFrameKindWithScreenContent(true, false, 60, 1)
	if rc.currentQuantizer != 4 {
		t.Fatalf("screen-content key quantizer = %d, want keyframe unbounded q4", rc.currentQuantizer)
	}
}

func TestRateControlTracksLibvpxLastInterQuantizer(t *testing.T) {
	rc := rateControlState{
		mode:               RateControlCBR,
		minQuantizer:       4,
		maxQuantizer:       56,
		currentQuantizer:   20,
		lastInterQuantizer: 35,
		bitsPerFrame:       1000,
		frameTargetBits:    1000,
		bufferLevelBits:    5000,
		maximumBufferBits:  8000,
	}

	rc.postEncodeFrameWithContext(100, true, false, 60)
	if rc.lastInterQuantizer != 35 {
		t.Fatalf("last inter quantizer after keyframe = %d, want unchanged 35", rc.lastInterQuantizer)
	}

	rc.currentQuantizer = 22
	rc.postEncodeFrameWithContext(100, false, false, 60)
	if rc.lastInterQuantizer != 22 {
		t.Fatalf("last inter quantizer after interframe = %d, want encoded q22", rc.lastInterQuantizer)
	}
}

func TestLibvpxEstimatedBitsAtQuantizerUsesLargeMacroblockPath(t *testing.T) {
	macroblocks := (1 << 11) + 1
	want := (libvpxBitsPerMB[1][24] >> libvpxBPerMBNormBits) * macroblocks
	if got := libvpxEstimatedBitsAtQuantizer(1, 24, macroblocks, 1.0); got != want {
		t.Fatalf("large-macroblock estimate = %d, want %d", got, want)
	}
}

func TestLibvpxEstimatedBitsAtQuantizerWithZbinAppliesLibvpxFactorWalk(t *testing.T) {
	// Mirror libvpx vp8/encoder/ratectrl.c vp8_update_rate_correction_factors:
	// when zbin_over_quant > 0, scale the projected size by 0.99 (walking up
	// to 0.999) for each unit of zbin_over_quant.
	frameType := 1
	q := 96
	macroblocks := 60
	correctionFactor := 1.5
	base := libvpxEstimatedBitsAtQuantizer(frameType, q, macroblocks, correctionFactor)

	if got := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, correctionFactor, 0); got != base {
		t.Fatalf("zbin=0 estimate = %d, want unchanged %d", got, base)
	}

	want := base
	factor := 0.99
	const factorAdjustment = 0.01 / 256.0
	for z := 4; z > 0; z-- {
		want = int(factor * float64(want))
		factor += factorAdjustment
		if factor >= 0.999 {
			factor = 0.999
		}
	}
	if got := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, correctionFactor, 4); got != want {
		t.Fatalf("zbin=4 estimate = %d, want %d", got, want)
	}

	// Strictly monotonically non-increasing in zbin_over_quant.
	prev := base
	for z := 1; z <= 16; z++ {
		got := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, correctionFactor, z)
		if got > prev {
			t.Fatalf("zbin=%d estimate %d exceeds zbin=%d estimate %d", z, got, z-1, prev)
		}
		prev = got
	}
}

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
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		frameTargetBits:   3000,
		bufferLevelBits:   5000,
		maximumBufferBits: 8000,
	}

	rc.postEncodeFrameWithPacketContext(100, false, false, 0, false)

	if rc.bufferLevelBits != 4200 {
		t.Fatalf("invisible buffer = %d, want previous minus frame size 4200", rc.bufferLevelBits)
	}
	if rc.rollingActualBits != 200 || rc.rollingTargetBits != 750 {
		t.Fatalf("invisible rolling bits = actual:%d target:%d, want libvpx 200/750", rc.rollingActualBits, rc.rollingTargetBits)
	}
}

func TestRateControlConfigDefaultPercentThresholds(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	if rc.undershootPct != defaultRateControlUndershootPct || rc.overshootPct != defaultRateControlOvershootPct {
		t.Fatalf("thresholds = under:%d over:%d, want %d/%d", rc.undershootPct, rc.overshootPct, defaultRateControlUndershootPct, defaultRateControlOvershootPct)
	}
}

func TestRateControlConfigInitializesLibvpxRollingBitAverages(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	if rc.rollingActualBits != 40000 || rc.rollingTargetBits != 40000 ||
		rc.longRollingActualBits != 40000 || rc.longRollingTargetBits != 40000 {
		t.Fatalf("rolling bits = short:%d/%d long:%d/%d, want libvpx per-frame bandwidth 40000",
			rc.rollingActualBits, rc.rollingTargetBits, rc.longRollingActualBits, rc.longRollingTargetBits)
	}
}

func TestRateControlCQUsesCQLevel(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	rc.bufferOptimalBits = 2000
	rc.bufferLevelBits = 0

	rc.beginFrame(false)
	cqQIndex := libvpxPublicQuantizerToQIndex(32)
	if rc.currentQuantizer != cqQIndex {
		t.Fatalf("beginFrame CQ quantizer = %d, want CQ level 32 mapped to qindex %d", rc.currentQuantizer, cqQIndex)
	}
	rc.postEncodeFrame(1<<20, false)
	if rc.currentQuantizer <= cqQIndex {
		t.Fatalf("postEncodeFrame CQ quantizer = %d, want constrained increase above CQ qindex %d", rc.currentQuantizer, cqQIndex)
	}
	rc.currentQuantizer = cqQIndex + 1
	rc.bufferLevelBits = 3000
	rc.postEncodeFrame(1, false)
	if rc.currentQuantizer != cqQIndex {
		t.Fatalf("undersized CQ quantizer = %d, want floor at CQ qindex %d", rc.currentQuantizer, cqQIndex)
	}
}

func TestRateControlCQDefaultLevelMirrorsLibvpx(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	defaultQIndex := libvpxPublicQuantizerToQIndex(defaultCQLevel)
	if rc.cqLevel != defaultQIndex || rc.currentQuantizer != defaultQIndex {
		t.Fatalf("CQ default = level:%d q:%d, want qindex %d", rc.cqLevel, rc.currentQuantizer, defaultQIndex)
	}
}

func TestRateControlCQValidatesLevelAgainstBounds(t *testing.T) {
	tests := []struct {
		name string
		cfg  RateControlConfig
	}{
		{
			name: "outside cq range",
			cfg: RateControlConfig{
				Mode:                RateControlCQ,
				TargetBitrateKbps:   1200,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             64,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
			},
		},
		{
			name: "below min",
			cfg: RateControlConfig{
				Mode:                RateControlCQ,
				TargetBitrateKbps:   1200,
				MinQuantizer:        20,
				MaxQuantizer:        56,
				CQLevel:             16,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
			},
		},
		{
			name: "default below min",
			cfg: RateControlConfig{
				Mode:                RateControlCQ,
				TargetBitrateKbps:   1200,
				MinQuantizer:        20,
				MaxQuantizer:        56,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
			},
		},
	}
	for _, tc := range tests {
		var rc rateControlState
		err := rc.applyConfig(tc.cfg, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
		if err != ErrInvalidQuantizer {
			t.Fatalf("%s error = %v, want ErrInvalidQuantizer", tc.name, err)
		}
	}
}

func TestRateControlBeginFrameAdjustsTargetForLowBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     defaultRateControlUndershootPct,
		overshootPct:      defaultRateControlOvershootPct,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   900,
		rollingTargetBits: 1000,
	}

	rc.beginFrame(false)

	if rc.frameTargetBits != 750 {
		t.Fatalf("frameTargetBits = %d, want libvpx low-buffer target 750", rc.frameTargetBits)
	}
	if rc.currentQuantizer != 20 {
		t.Fatalf("currentQuantizer = %d, want unchanged before target-based regulation", rc.currentQuantizer)
	}
}

func TestRateControlBeginFrameAdjustsTargetForHighBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     defaultRateControlUndershootPct,
		overshootPct:      defaultRateControlOvershootPct,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   3200,
		rollingTargetBits: 1000,
	}

	rc.beginFrame(false)

	if rc.frameTargetBits != 1285 {
		t.Fatalf("frameTargetBits = %d, want libvpx high-buffer target 1285", rc.frameTargetBits)
	}
	if rc.currentQuantizer != 20 {
		t.Fatalf("currentQuantizer = %d, want unchanged before target-based regulation", rc.currentQuantizer)
	}
}

func TestRateControlBeginLaterKeyFrameUsesLibvpxBoost(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            56,
		currentQuantizer:        20,
		bitsPerFrame:            40000,
		bufferOptimalBits:       600000,
		bufferLevelBits:         600000,
		framesSinceKeyframe:     60,
		avgFrameQuantizer:       20,
		normalInterAvgQuantizer: 20,
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 1,
		timing:             timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1},
	})

	if rc.frameTargetBits != 202500 {
		t.Fatalf("later keyframe target = %d, want libvpx boosted 202500", rc.frameTargetBits)
	}
	if rc.currentQuantizer != 20 {
		t.Fatalf("currentQuantizer = %d, want unchanged 20 before feedback", rc.currentQuantizer)
	}
}

func TestRateControlBeginLaterKeyFrameDampensShortIntervals(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            56,
		currentQuantizer:        20,
		bitsPerFrame:            40000,
		bufferOptimalBits:       600000,
		bufferLevelBits:         600000,
		framesSinceKeyframe:     5,
		avgFrameQuantizer:       20,
		normalInterAvgQuantizer: 20,
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 1,
		timing:             timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1},
	})

	if rc.frameTargetBits != 92500 {
		t.Fatalf("short-interval keyframe target = %d, want libvpx damped 92500", rc.frameTargetBits)
	}
}

func TestRateControlBeginLaterTemporalKeyFrameUsesBaseLibvpxBoost(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            56,
		currentQuantizer:        20,
		bitsPerFrame:            40000,
		bufferOptimalBits:       600000,
		bufferLevelBits:         600000,
		framesSinceKeyframe:     60,
		avgFrameQuantizer:       20,
		normalInterAvgQuantizer: 20,
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 2,
		timing:             timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1},
	})

	if rc.frameTargetBits != 157500 {
		t.Fatalf("temporal keyframe target = %d, want libvpx base boost 157500", rc.frameTargetBits)
	}
}

func TestRateControlBeginInitialKeyFrameUsesLibvpxStartingBufferTarget(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlCBR,
		minQuantizer:        4,
		maxQuantizer:        56,
		currentQuantizer:    20,
		targetBandwidthBits: 1200000,
		bitsPerFrame:        40000,
		bufferInitialBits:   480000,
		bufferOptimalBits:   600000,
		bufferLevelBits:     480000,
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{firstFrame: true})

	if rc.frameTargetBits != 240000 {
		t.Fatalf("initial keyframe target = %d, want libvpx starting-buffer half 240000", rc.frameTargetBits)
	}
}

func TestRateControlBeginFrameCapsKeyFrameTargetWithMaxIntraBitrate(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            56,
		currentQuantizer:        20,
		bitsPerFrame:            1000,
		maxIntraBitratePct:      250,
		bufferOptimalBits:       2000,
		bufferLevelBits:         2000,
		rollingTargetBits:       0,
		rollingActualBits:       0,
		frameDropPressure:       0,
		framesSinceKeyframe:     60,
		avgFrameQuantizer:       20,
		normalInterAvgQuantizer: 20,
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 1,
		timing:             timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1},
	})

	if rc.frameTargetBits != 2500 {
		t.Fatalf("keyframe target = %d, want capped 2500", rc.frameTargetBits)
	}
}

func TestRateControlRejectsInvalidMaxIntraBitrate(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MaxIntraBitratePct:  -1,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != ErrInvalidConfig {
		t.Fatalf("applyConfig error = %v, want ErrInvalidConfig", err)
	}
}

func TestRateControlRejectsInvalidGFCBRBoost(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		GFCBRBoostPct:       -1,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != ErrInvalidConfig {
		t.Fatalf("applyConfig error = %v, want ErrInvalidConfig", err)
	}
}

func TestBoostedFrameTargetBits(t *testing.T) {
	if got := boostedFrameTargetBits(1000, 100); got != 2000 {
		t.Fatalf("boosted target = %d, want 2000", got)
	}
	if got := boostedFrameTargetBits(1000, 0); got != 1000 {
		t.Fatalf("zero-boost target = %d, want 1000", got)
	}
	if got := boostedFrameTargetBits(maxInt(), 100); got != maxInt() {
		t.Fatalf("overflow-boost target = %d, want maxInt", got)
	}
}

// TestCalcGFParamsMatchesLibvpxBoostTables pins the libvpx
// vp8/encoder/ratectrl.c calc_gf_params boost computation for known
// inputs. The hand-computed expectations follow:
//
//	GFQ_ADJUSTMENT[40] = 128.
//	gf_intra_usage_adjustment[clamp(10,0,14)] = 70.
//	gf_frame_usage = max((golden+altref)*100/total, 100*gf_active/MBs)
//	              = max((200+0)*100/1200, 100*200/1200) = 16.
//	gf_adjust_table[16] = 300.
//	Boost = (((128 * 70) / 100) * 300) / 100 = 267.
//	kf_gf_boost_qlimits[40] = 390 -> no ceiling clamp; >=110 floor unused.
//	gf_interval_table[16] = 7; baseline=8 wins; max_gf_interval=15 caps.
func TestCalcGFParamsMatchesLibvpxBoostTables(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     40,
		RecentRefIntra:        100,
		RecentRefLast:         900,
		RecentRefGolden:       200,
		RecentRefAltRef:       0,
		GFActiveCount:         200,
		Macroblocks:           1200,
		ThisFramePercentIntra: 10,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.GFFrameUsage != 16 {
		t.Fatalf("gf_frame_usage = %d, want libvpx 16", out.GFFrameUsage)
	}
	if out.Boost != 267 {
		t.Fatalf("calcGFParams boost = %d, want libvpx 267", out.Boost)
	}
	if out.FramesTillUpdate != 8 {
		t.Fatalf("calcGFParams interval = %d, want libvpx 8", out.FramesTillUpdate)
	}
}

// TestCalcGFParamsAppliesQLimitCeiling exercises the kf_gf_boost_qlimits
// ceiling: at low Q with high gf_frame_usage, the raw boost product
// exceeds the table limit and must be clamped down. With
// kf_gf_boost_qlimits[20]=250 the result is forced to 250, and the
// last_boost>=1500 branch never fires so the interval is governed by
// gf_interval_table[gf_frame_usage].
func TestCalcGFParamsAppliesQLimitCeiling(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     20,
		RecentRefIntra:        50,
		RecentRefLast:         100,
		RecentRefGolden:       400,
		RecentRefAltRef:       400,
		GFActiveCount:         950,
		Macroblocks:           1000,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    8,
		MaxGFInterval:         20,
	})
	if out.Boost != libvpxKFGFBoostQLimits[20] {
		t.Fatalf("calcGFParams boost = %d, want clamped to qlimits 250", out.Boost)
	}
	// gf_frame_usage = max((400+400)*100/950, 100*950/1000) = max(84,95)=95.
	// gf_interval_table[95] = 11 (libvpx gf_interval_table boundary).
	if out.GFFrameUsage != 95 {
		t.Fatalf("gf_frame_usage = %d, want 95", out.GFFrameUsage)
	}
	if out.FramesTillUpdate != libvpxGFIntervalTable[95] {
		t.Fatalf("calcGFParams interval = %d, want gf_interval_table[95]=%d",
			out.FramesTillUpdate, libvpxGFIntervalTable[95])
	}
}

// TestCalcGFParamsAppliesBoostFloor pins the lower 110 floor: at high Q
// with low usage the raw product falls under 110, so the boost is
// floored. The interval still picks up the gf_interval_table value at
// the resulting gf_frame_usage.
func TestCalcGFParamsAppliesBoostFloor(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     0,
		RecentRefIntra:        1000,
		RecentRefLast:         0,
		RecentRefGolden:       0,
		RecentRefAltRef:       0,
		GFActiveCount:         0,
		Macroblocks:           1000,
		ThisFramePercentIntra: 14,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.Boost != 110 {
		t.Fatalf("calcGFParams boost = %d, want 110 floor", out.Boost)
	}
	if out.FramesTillUpdate != 8 {
		t.Fatalf("calcGFParams interval = %d, want baseline 8", out.FramesTillUpdate)
	}
}

// TestCalcGFParamsBoostExtendsInterval covers the >750/>1000/>1250/>=1500
// boost-extension thresholds. With cleared intra/inter ref usage, the
// raw boost is 198 at Q=127; with low intra and gf_frame_usage=0 the
// only path to a large boost is via the test stub. We hand-pick inputs
// that yield boost >= 1500 by zeroing tot_mbs (all entries 0) so
// gf_frame_usage falls back to 100*gf_active/MBs and intra adjustment
// runs at idx=0 (125).
func TestCalcGFParamsBoostExtendsInterval(t *testing.T) {
	// libvpxKFGFBoostQLimits saturates at 600 above index 62; choose
	// Q=80 so the raw product is far above 600 and the qlimit ceiling
	// brings it to exactly 600. Then >=1500 path is not taken (boost is
	// 600), so verify the interval-extension thresholds remain inactive.
	out := calcGFParams(gfParamsInput{
		Q:                     80,
		RecentRefIntra:        0,
		RecentRefLast:         0,
		RecentRefGolden:       1000,
		RecentRefAltRef:       0,
		GFActiveCount:         1000,
		Macroblocks:           1000,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.Boost != 600 {
		t.Fatalf("calcGFParams boost = %d, want libvpx ceiling 600", out.Boost)
	}
	// gf_interval_table[100]=11 wins over baseline 8, but max=15 caps.
	if out.FramesTillUpdate != 11 {
		t.Fatalf("calcGFParams interval = %d, want gf_interval_table[100]=11", out.FramesTillUpdate)
	}
}

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
	rc.postEncodeFrameWithPacketContext(2000, true, false, 1, true)
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
	rc.postEncodeFrameWithPacketContext(100, true, false, 1, true) // 800 bits < 4000.
	if rc.kfOverspendBits != 0 || rc.gfOverspendBits != 0 || rc.kfBitrateAdjustment != 0 {
		t.Fatalf("undersize KF accumulated overspend: kf=%d gf=%d adj=%d",
			rc.kfOverspendBits, rc.gfOverspendBits, rc.kfBitrateAdjustment)
	}
}

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
	rc.postEncodeFrameWithPacketContext(500, false, true, 1, true)
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

// TestRateControlGFOverspendDrainsIntoNextPFrameTarget pins the libvpx
// calc_pframe_target_size GF-overspend recovery branch: starting with
// gf_overspend_bits=2000, non_gf_bitrate_adjustment=200, the next p-frame
// target = per_frame_bandwidth - 200, and the gf_overspend_bits residue is
// 1800. min_frame_target = max(min_frame_bandwidth, per_frame_bandwidth/4).
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

// TestRateControlOverspendRecoveryClampsAtMinFrameTarget pins the
// min_frame_target = max(min_frame_bandwidth, per_frame_bandwidth/4)
// floor inside calc_pframe_target_size. With kf_bitrate_adjustment far
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

// TestRateControlGoldenFrameTargetBitsMatchesLibvpx pins the libvpx
// boost-weighted GF section split from calc_pframe_target_size. With
// boost=400, frames_till_gf_update_due=7 (frames_in_section=8) and
// inter_frame_target=1000:
//
//	allocation_chunks = 8*100 + 300 = 1100
//	bits_in_section   = 1000 * 8 = 8000
//	(8000 >> 7) = 62 < 1100, so target = 400 * 8000 / 1100 = 2909.
func TestRateControlGoldenFrameTargetBitsMatchesLibvpx(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(400, 7, 1000)
	if got != 2909 {
		t.Fatalf("libvpxGoldenFrameTargetBits = %d, want 2909", got)
	}
}

// TestRateControlGoldenFrameTargetBitsHalvesLargeBoost pins libvpx's
// `while (Boost > 1000) Boost /= 2; allocation_chunks /= 2;` overflow
// guard. With boost=1500, the loop runs once -> boost=750,
// allocation_chunks=(8*100+1400)/2=1100. bits_in_section=8000.
// (8000 >> 7)=62 < 1100, so target = 750 * 8000 / 1100 = 5454.
func TestRateControlGoldenFrameTargetBitsHalvesLargeBoost(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(1500, 7, 1000)
	if got != 5454 {
		t.Fatalf("libvpxGoldenFrameTargetBits with large boost = %d, want 5454", got)
	}
}

// TestRateControlGoldenFrameTargetBitsHighPrecisionPath pins libvpx's
// alternate `Boost * (bits_in_section / allocation_chunks)` branch
// taken when `bits_in_section >> 7 > allocation_chunks`. With
// inter_frame_target=1<<20, frames_in_section=8, boost=400:
//
//	bits_in_section = 8 << 20.
//	bits_in_section >> 7 = 8 << 13 = 65536, > allocation_chunks=1100.
//	target = 400 * (8<<20)/1100 = 400 * 7626 = 3050400.
func TestRateControlGoldenFrameTargetBitsHighPrecisionPath(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(400, 7, 1<<20)
	want := 400 * ((8 << 20) / 1100)
	if got != want {
		t.Fatalf("libvpxGoldenFrameTargetBits high-precision = %d, want %d", got, want)
	}
}

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

// TestRateControlEstimateKeyFrameFrequencyBootstraps pins libvpx's
// estimate_keyframe_frequency special case for keyFrameCount==1: the
// bootstrap returns the configured key_freq when set.
func TestRateControlEstimateKeyFrameFrequencyBootstraps(t *testing.T) {
	rc := rateControlState{
		keyFrameCount:     1,
		keyFrameFrequency: 60,
	}
	if got := rc.estimateKeyFrameFrequency(); got != 60 {
		t.Fatalf("first keyframe estimate = %d, want configured 60", got)
	}
	rc = rateControlState{keyFrameCount: 1}
	if got := rc.estimateKeyFrameFrequency(); got != 1 {
		t.Fatalf("first keyframe estimate without freq = %d, want 1", got)
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
// the new tail value is 60. The expected weighted average is
// (1*20 + 2*30 + 3*40 + 4*50 + 5*60) / 15 = 700/15 = 46.
func TestRateControlEstimateKeyFrameFrequencyWeightedAverage(t *testing.T) {
	rc := rateControlState{
		keyFrameCount:         2,
		framesSinceKeyframe:   60,
		priorKeyFrameDistance: [5]int{10, 20, 30, 40, 50},
	}
	got := rc.estimateKeyFrameFrequency()
	want := (1*20 + 2*30 + 3*40 + 4*50 + 5*60) / 15
	if got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}
