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
