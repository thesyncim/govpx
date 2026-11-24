package govpx

import "testing"

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
	if rc.rateCorrectionFactor != recode.correctionFactor {
		t.Fatalf("oversized recode correction factor = %.9f, want persisted %.9f", rc.rateCorrectionFactor, recode.correctionFactor)
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

	if !ok || got != 80 || recode.qHigh != 80 || recode.regulateHigh != 100 || !recode.activeWorstQChanged || !rc.activeWorstQChanged {
		t.Fatalf("active-worst recode = q:%d ok:%t state:%+v rcChanged:%t, want relaxed active-worst while local q_high stays pinned", got, ok, recode, rc.activeWorstQChanged)
	}
	if recode.correctionFactor != 1.0 {
		t.Fatalf("active-worst correction factor = %g, want unchanged when active worst changed", recode.correctionFactor)
	}
}

func TestRateControlFrameSizeRecodeDoesNotReopenNarrowedQHigh(t *testing.T) {
	rc := rateControlState{
		mode:             RateControlCBR,
		minQuantizer:     2,
		maxQuantizer:     106,
		currentQuantizer: 2,
		bitsPerFrame:     10_000,
		frameTargetBits:  7061,
	}
	recode := frameSizeRecodeState{
		qLow:             2,
		qHigh:            2,
		regulateLow:      2,
		regulateHigh:     106,
		correctionFactor: 0.483560357,
		undershootSeen:   true,
	}

	got, ok := rc.frameSizeRecodeQuantizerWithContextBits(8496, false, true, 16, &recode)

	if !ok || got != 2 || recode.qHigh != 2 || recode.regulateHigh != 106 || recode.activeWorstQChanged {
		t.Fatalf("narrowed q_high recode = q:%d ok:%t state:%+v, want q2 accepted without active-worst relaxation", got, ok, recode)
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

func TestFrameSizeRecodeRetriesRegulatorUntilBoundsSatisfied(t *testing.T) {
	rc := rateControlState{
		mode:                 RateControlCBR,
		minQuantizer:         2,
		maxQuantizer:         106,
		currentQuantizer:     2,
		frameTargetBits:      14468,
		rateCorrectionFactor: 0.76812792,
	}
	recode := rc.newFrameSizeRecodeState(false, true)

	next, ok := rc.frameSizeRecodeQuantizerWithContextBits(16604, false, true, 16, &recode)
	if !ok {
		t.Fatalf("frameSizeRecodeQuantizerWithContextBits did not recode")
	}
	if next != 3 {
		t.Fatalf("recode quantizer = %d, want q3", next)
	}
	if recode.correctionFactor < 1.08 || recode.correctionFactor > 1.10 {
		t.Fatalf("recode correction factor = %.9f, want libvpx retry-updated factor near 1.091", recode.correctionFactor)
	}
}
