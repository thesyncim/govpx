package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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

func TestRateControlSelectQuantizerUsesLibvpxBitsPerMBModel(t *testing.T) {
	if got := vp8enc.LibvpxRegulatedQuantizer(false, 12000, 60, 4, 56, 1.0); got != 24 {
		t.Fatalf("inter regulated quantizer = %d, want libvpx table q24", got)
	}
	if got := vp8enc.LibvpxRegulatedQuantizer(true, 72000, 60, 4, 56, 1.0); got != 4 {
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

	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
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
	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 57 || activeWorst != 80 {
		t.Fatalf("optimal-buffer active bounds = %d/%d, want inter_minq[80]/ni_av_qi 57/80", activeBest, activeWorst)
	}

	rc.bufferLevelBits = 1500
	activeBest, activeWorst = rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 27 || activeWorst != 70 {
		t.Fatalf("mid-full-buffer active bounds = %d/%d, want libvpx scaled CBR bounds 27/70", activeBest, activeWorst)
	}

	rc.bufferLevelBits = 2000
	activeBest, activeWorst = rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
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

	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
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

	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	wantBest := vp8enc.LibvpxInterMinQ[51]
	if wantBest >= rc.cqLevel {
		t.Fatalf("test fixture invalid: inter_minq[51] = %d, want below CQ level %d", wantBest, rc.cqLevel)
	}
	if activeBest != wantBest || activeWorst != 51 {
		t.Fatalf("Q active bounds = %d/%d, want no CQ floor %d/51", activeBest, activeWorst, wantBest)
	}
}

func TestRateControlPreservesCQActiveBestAcrossRuntimeCBRForcedKey(t *testing.T) {
	timing := timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1}
	var rc rateControlState
	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             30,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}, timing); err != nil {
		t.Fatalf("apply CQ config: %v", err)
	}

	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	rc.selectQuantizerForFrameKind(false, false, 4)
	cqQ := vp8common.PublicQuantizerToQIndex(30)
	if rc.activeBestQuantizer != cqQ {
		t.Fatalf("CQ active best = %d, want cq target q%d", rc.activeBestQuantizer, cqQ)
	}

	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}, timing); err != nil {
		t.Fatalf("apply CBR config: %v", err)
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame: true,
		timing:         timing,
	})
	rc.selectQuantizerForFrameKind(true, false, 4)
	if rc.activeBestQuantizer != cqQ || rc.currentQuantizer != cqQ {
		t.Fatalf("forced-key active/current Q = %d/%d, want preserved CQ q%d", rc.activeBestQuantizer, rc.currentQuantizer, cqQ)
	}

	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	if rc.activeBestQuantizer != rc.minQuantizer {
		t.Fatalf("post-key inter active best = %d, want reset to best q%d", rc.activeBestQuantizer, rc.minQuantizer)
	}
}

func TestRateControlCQKeyFrameResetsStickyActiveBest(t *testing.T) {
	timing := timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1}
	var rc rateControlState
	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}, timing); err != nil {
		t.Fatalf("apply CQ config: %v", err)
	}

	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	rc.selectQuantizerForFrameKind(false, false, 16)
	if rc.activeBestQuantizer <= rc.minQuantizer {
		t.Fatalf("CQ inter active best = %d, want raised above best q%d", rc.activeBestQuantizer, rc.minQuantizer)
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	rc.selectQuantizerForFrameKind(true, false, 16)
	if rc.activeBestQuantizer != rc.minQuantizer {
		t.Fatalf("CQ key active best = %d, want reset to best q%d", rc.activeBestQuantizer, rc.minQuantizer)
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

func TestRateControlScreenContentLimitsInterRecodeQuantizerDrop(t *testing.T) {
	rc := rateControlState{
		mode:               RateControlCBR,
		minQuantizer:       2,
		maxQuantizer:       94,
		currentQuantizer:   30,
		lastInterQuantizer: 50,
		bitsPerFrame:       120000,
		frameTargetBits:    120000,
		bufferOptimalBits:  3500000,
		bufferLevelBits:    3500000,
		maximumBufferBits:  4200000,
	}

	recode := rc.newFrameSizeRecodeState(false, false)
	recode.onePass = true
	recode.screenContentMode = 1
	got, ok := rc.frameSizeRecodeQuantizerWithContextBits(100, false, false, 60, &recode)
	if !ok || got != 29 {
		t.Fatalf("screen-content inter recode quantizer = %d ok=%t, want q_high-limited 29", got, ok)
	}

	rc.currentQuantizer = 30
	recode = rc.newFrameSizeRecodeState(false, false)
	got, ok = rc.frameSizeRecodeQuantizerWithContextBits(100, false, false, 60, &recode)
	if !ok || got >= 29 {
		t.Fatalf("non-screen inter recode quantizer = %d ok=%t, want unbounded drop below q_high", got, ok)
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

// TestActiveBestQuantizerForcedKeyFramePass2Clamp verifies the libvpx
// onyx_if.c:3636-3642 forced-key sub-clamp. For a pass-2 KF emitted because
// the maximum key-frame interval was hit (this_key_frame_forced=true),
// active_best_quality must land in
// [avg_frame_qindex >> 2, avg_frame_qindex * 7 / 8].
func TestActiveBestQuantizerForcedKeyFramePass2Clamp(t *testing.T) {
	// Case 1: kf_high_motion_minq[active_worst] would normally be below
	// avg_frame_qindex >> 2; clamp lifts active_best to the lower bound.
	rc := rateControlState{
		mode:                      RateControlVBR,
		minQuantizer:              0,
		maxQuantizer:              127,
		normalInterFrames:         151,
		normalInterAvgQuantizer:   60,
		pass2ActiveWorstQValid:    true,
		pass2ActiveWorstQOverride: 60,
		avgFrameQuantizer:         80,
		thisKeyFrameForced:        true,
	}
	activeBest, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	// kf_high_motion_minq[60] == 6 from vp8_ratecontrol_tables.go, well below
	// avg_frame_qindex>>2 == 80>>2 == 20. Expect lift to 20.
	if activeBest != 20 {
		t.Fatalf("forced-key pass2 KF active_best = %d, want lift to avg>>2 = 20", activeBest)
	}

	// Case 2: clamp upper bound. Synthesize a high active_worst that
	// would push kf_high_motion_minq lookup above avg*7/8.
	rc2 := rateControlState{
		mode:                      RateControlVBR,
		minQuantizer:              0,
		maxQuantizer:              127,
		normalInterFrames:         151,
		normalInterAvgQuantizer:   100,
		pass2ActiveWorstQValid:    true,
		pass2ActiveWorstQOverride: 127,
		avgFrameQuantizer:         24,
		thisKeyFrameForced:        true,
	}
	activeBest2, _ := rc2.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	// kf_high_motion_minq[127] == 30 from vp8_ratecontrol_tables.go, above
	// avg_frame_qindex*7/8 == 24*7/8 == 21. Expect clamp down to 21.
	if activeBest2 != 21 {
		t.Fatalf("forced-key pass2 KF active_best = %d, want clamp to avg*7/8 = 21", activeBest2)
	}

	// Case 3: forced-key flag NOT set; clamp must not fire. Confirms the
	// clamp is gated correctly.
	rc3 := rc
	rc3.thisKeyFrameForced = false
	activeBest3, _ := rc3.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	if activeBest3 != vp8enc.LibvpxKeyFrameHighMotionMinQ[60] {
		t.Fatalf("non-forced pass2 KF active_best = %d, want raw kf_high_motion_minq[60] = %d", activeBest3, vp8enc.LibvpxKeyFrameHighMotionMinQ[60])
	}

	// Case 4: forced-key flag set but pass-2 surface inactive (one-pass);
	// libvpx 3636-3642 is inside the `cpi->pass == 2` arm so the clamp
	// must NOT fire in one-pass mode.
	rc4 := rc
	rc4.pass2ActiveWorstQValid = false
	activeBest4, _ := rc4.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	// One-pass KF: libvpxActiveWorstQuantizerForFrame returns maxQuantizer
	// (127), so kf_high_motion_minq[127] == 30 (unclamped).
	if activeBest4 != vp8enc.LibvpxKeyFrameHighMotionMinQ[127] {
		t.Fatalf("forced-key one-pass KF active_best = %d, want raw kf_high_motion_minq[127] = %d (no clamp)", activeBest4, vp8enc.LibvpxKeyFrameHighMotionMinQ[127])
	}
}

// TestActiveBestQuantizerPass2CQGoldenFrame15Over16Lowering verifies the
// libvpx onyx_if.c:3677-3679 "Constrained quality use slightly lower active
// best" lowering. For pass-2 CQ GF/ARF frames, active_best is multiplied by
// 15/16 after the gf_*_motion_minq lookup.
func TestActiveBestQuantizerPass2CQGoldenFrame15Over16Lowering(t *testing.T) {
	rc := rateControlState{
		mode:                      RateControlCQ,
		minQuantizer:              0,
		maxQuantizer:              127,
		cqLevel:                   30,
		normalInterFrames:         151,
		normalInterAvgQuantizer:   80,
		pass2ActiveWorstQValid:    true,
		pass2ActiveWorstQOverride: 80,
		avgFrameQuantizer:         60,
		framesSinceKeyframe:       10,
	}
	activeBest, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	// q is min(active_worst=80, avg_frame_qindex=60) = 60. cqFloor:
	// q=60 already above cqLevel=30, so no lift. Then
	// gf_high_motion_minq[60] = 23 from vp8_ratecontrol_tables.go (row
	// 48-63: 17,17,18,18,19,19,20,20,21,21,22,22,23,23,24,24).
	// 15/16 lowering: 23 * 15 / 16 = 21.
	wantActiveBest := vp8enc.LibvpxGoldenFrameHighMotionMinQ[60] * 15 / 16
	if activeBest != wantActiveBest {
		t.Fatalf("pass2 CQ GF active_best = %d, want gf_high_motion_minq[60]*15/16 = %d", activeBest, wantActiveBest)
	}

	// Without the CQ mode the 15/16 must not fire (VBR pass-2 GF).
	rc2 := rc
	rc2.mode = RateControlVBR
	activeBest2, _ := rc2.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	if activeBest2 != vp8enc.LibvpxGoldenFrameHighMotionMinQ[60] {
		t.Fatalf("pass2 VBR GF active_best = %d, want raw gf_high_motion_minq[60] = %d", activeBest2, vp8enc.LibvpxGoldenFrameHighMotionMinQ[60])
	}

	// One-pass CQ GF: libvpx 3677-3679 is inside the pass==2 arm; the
	// 15/16 must not fire for one-pass.
	rc3 := rc
	rc3.pass2ActiveWorstQValid = false
	rc3.pass2ActiveWorstQOverride = 0
	activeBest3, _ := rc3.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	// One-pass inter: active_worst = maxQuantizer (127, since
	// bufferOptimalBits is zero → unbuffered fallthrough). avg=60 <
	// active_worst=127, so q=60. cqFloor lifts q to cqLevel only when
	// q<cqLevel; q=60 > cqLevel=30 so no lift. gf_high_motion_minq[60] =
	// 40, no 15/16 multiplier in one-pass.
	if activeBest3 != vp8enc.LibvpxGoldenFrameHighMotionMinQ[60] {
		t.Fatalf("one-pass CQ GF active_best = %d, want raw gf_high_motion_minq[60] = %d (no 15/16)", activeBest3, vp8enc.LibvpxGoldenFrameHighMotionMinQ[60])
	}
}
