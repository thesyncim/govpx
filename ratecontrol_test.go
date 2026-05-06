package govpx

import "testing"

func TestRateControlAdjustQuantizerUsesOvershootPct(t *testing.T) {
	rc := rateControlState{
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     defaultRateControlUndershootPct,
		overshootPct:      100,
		bufferOptimalBits: 1000,
		bufferLevelBits:   800,
	}

	rc.adjustQuantizer(200, 100)
	if rc.currentQuantizer != 20 {
		t.Fatalf("quantizer after tolerated overshoot = %d, want 20", rc.currentQuantizer)
	}

	rc.adjustQuantizer(201, 100)
	if rc.currentQuantizer != 21 {
		t.Fatalf("quantizer after overshoot = %d, want 21", rc.currentQuantizer)
	}

	rc.currentQuantizer = 20
	rc.adjustQuantizer(301, 100)
	if rc.currentQuantizer != 22 {
		t.Fatalf("quantizer after large overshoot = %d, want 22", rc.currentQuantizer)
	}
}

func TestRateControlAdjustQuantizerUsesUndershootPct(t *testing.T) {
	rc := rateControlState{
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     50,
		overshootPct:      defaultRateControlOvershootPct,
		bufferOptimalBits: 1000,
		bufferLevelBits:   1200,
	}

	rc.adjustQuantizer(50, 100)
	if rc.currentQuantizer != 20 {
		t.Fatalf("quantizer after tolerated undershoot = %d, want 20", rc.currentQuantizer)
	}

	rc.adjustQuantizer(49, 100)
	if rc.currentQuantizer != 19 {
		t.Fatalf("quantizer after undershoot = %d, want 19", rc.currentQuantizer)
	}
}

func TestRateControlFrameSizeFeedbackQuantizerUsesProjectedFrameSize(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		undershootPct:     50,
		overshootPct:      100,
		bufferOptimalBits: 1000,
		bufferLevelBits:   800,
		bitsPerFrame:      100,
		frameTargetBits:   100,
	}

	if got := rc.frameSizeFeedbackQuantizer(38); got != 22 {
		t.Fatalf("oversized frame feedback q = %d, want 22", got)
	}

	rc.currentQuantizer = 20
	rc.bufferLevelBits = 2000
	if got := rc.frameSizeFeedbackQuantizer(4); got != 19 {
		t.Fatalf("undersized frame feedback q = %d, want 19", got)
	}

	rc.mode = RateControlCQ
	rc.currentQuantizer = 20
	rc.cqLevel = 20
	if got := rc.frameSizeFeedbackQuantizer(38); got != 22 {
		t.Fatalf("CQ oversized frame feedback q = %d, want constrained increase to 22", got)
	}
	rc.currentQuantizer = 21
	rc.bufferLevelBits = 2000
	if got := rc.frameSizeFeedbackQuantizer(4); got != 20 {
		t.Fatalf("CQ undersized frame feedback q = %d, want floor at CQ level 20", got)
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
	if rc.currentQuantizer != 32 {
		t.Fatalf("beginFrame CQ quantizer = %d, want CQ level 32", rc.currentQuantizer)
	}
	rc.postEncodeFrame(1<<20, false)
	if rc.currentQuantizer <= 32 {
		t.Fatalf("postEncodeFrame CQ quantizer = %d, want constrained increase above CQ level 32", rc.currentQuantizer)
	}
	rc.currentQuantizer = 33
	rc.bufferLevelBits = 3000
	rc.postEncodeFrame(1, false)
	if rc.currentQuantizer != 32 {
		t.Fatalf("undersized CQ quantizer = %d, want floor at CQ level 32", rc.currentQuantizer)
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
	if rc.cqLevel != defaultCQLevel || rc.currentQuantizer != defaultCQLevel {
		t.Fatalf("CQ default = level:%d q:%d, want %d", rc.cqLevel, rc.currentQuantizer, defaultCQLevel)
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

func TestRateControlBeginFrameAdjustsTargetAndQuantizerForLowBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   900,
		rollingTargetBits: 1000,
	}

	rc.beginFrame(false)

	if rc.frameTargetBits != 500 {
		t.Fatalf("frameTargetBits = %d, want 500 for low buffer", rc.frameTargetBits)
	}
	if rc.currentQuantizer != 22 {
		t.Fatalf("currentQuantizer = %d, want 22 for low buffer", rc.currentQuantizer)
	}
}

func TestRateControlBeginFrameAdjustsTargetAndQuantizerForHighBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   3200,
		rollingTargetBits: 1000,
	}

	rc.beginFrame(false)

	if rc.frameTargetBits != 1500 {
		t.Fatalf("frameTargetBits = %d, want 1500 for high buffer", rc.frameTargetBits)
	}
	if rc.currentQuantizer != 18 {
		t.Fatalf("currentQuantizer = %d, want 18 for high buffer", rc.currentQuantizer)
	}
}

func TestRateControlBeginFrameKeepsFirstFrameTargetStable(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  20,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   900,
	}

	rc.beginFrame(true)

	if rc.frameTargetBits != 4000 {
		t.Fatalf("keyframe target = %d, want unadjusted boosted 4000", rc.frameTargetBits)
	}
	if rc.currentQuantizer != 20 {
		t.Fatalf("currentQuantizer = %d, want unchanged 20 before feedback", rc.currentQuantizer)
	}
}

func TestRateControlBeginFrameCapsKeyFrameTargetWithMaxIntraBitrate(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlCBR,
		minQuantizer:        4,
		maxQuantizer:        56,
		currentQuantizer:    20,
		bitsPerFrame:        1000,
		maxIntraBitratePct:  250,
		bufferOptimalBits:   2000,
		bufferLevelBits:     2000,
		rollingTargetBits:   0,
		rollingActualBits:   0,
		frameDropPressure:   0,
		framesSinceKeyframe: 0,
	}

	rc.beginFrame(true)

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
