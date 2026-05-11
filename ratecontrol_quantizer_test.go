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
		{
			name:      "q",
			rc:        rateControlState{mode: RateControlQ},
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

func TestRateControlQActiveQuantizerBoundsDoNotUseCQFloor(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlQ,
		minQuantizer:            4,
		maxQuantizer:            51,
		cqLevel:                 43,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 51,
	}

	activeBest, activeWorst := rc.libvpxActiveQuantizerBounds(false, false)
	wantBest := libvpxInterMinQ[51]
	if wantBest >= rc.cqLevel {
		t.Fatalf("test fixture invalid: inter_minq[51] = %d, want below CQ level %d", wantBest, rc.cqLevel)
	}
	if activeBest != wantBest || activeWorst != 51 {
		t.Fatalf("Q active bounds = %d/%d, want no CQ floor %d/51", activeBest, activeWorst, wantBest)
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
