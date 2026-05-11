package govpx

import "testing"

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

func TestDefaultRateControlConfigUsesLibvpxBufferDefaults(t *testing.T) {
	cfg := defaultRateControlConfig(EncoderOptions{
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if cfg.BufferSizeMs != libvpxDefaultBufferSizeMs ||
		cfg.BufferInitialSizeMs != libvpxDefaultBufferInitialMs ||
		cfg.BufferOptimalSizeMs != libvpxDefaultBufferOptimalMs {
		t.Fatalf("default buffers = size:%d initial:%d optimal:%d, want libvpx %d/%d/%d",
			cfg.BufferSizeMs, cfg.BufferInitialSizeMs, cfg.BufferOptimalSizeMs,
			libvpxDefaultBufferSizeMs, libvpxDefaultBufferInitialMs, libvpxDefaultBufferOptimalMs)
	}
}

func TestRateControlVBRUsesLibvpxLocalPlaybackBufferModel(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
	if err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	if rc.bufferSizeMs != libvpxVBRBufferSizeMs ||
		rc.bufferInitialSizeMs != libvpxVBRBufferInitialMs ||
		rc.bufferOptimalSizeMs != libvpxVBRBufferOptimalMs {
		t.Fatalf("VBR buffer ms = size:%d initial:%d optimal:%d, want libvpx local playback %d/%d/%d",
			rc.bufferSizeMs, rc.bufferInitialSizeMs, rc.bufferOptimalSizeMs,
			libvpxVBRBufferSizeMs, libvpxVBRBufferInitialMs, libvpxVBRBufferOptimalMs)
	}
	if rc.bufferInitialBits != 42_000_000 || rc.bufferOptimalBits != 42_000_000 || rc.maximumBufferBits != 168_000_000 {
		t.Fatalf("VBR buffer bits = initial:%d optimal:%d maximum:%d, want 42000000/42000000/168000000",
			rc.bufferInitialBits, rc.bufferOptimalBits, rc.maximumBufferBits)
	}
	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{firstFrame: true})
	if rc.frameTargetBits != 1_050_000 {
		t.Fatalf("initial VBR key target = %d, want target_bandwidth*3/2 = 1050000", rc.frameTargetBits)
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

func TestRateControlQUsesConstantQualitySemantics(t *testing.T) {
	var rc rateControlState
	err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlQ,
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
	if rc.mode != RateControlQ {
		t.Fatalf("mode = %d, want RateControlQ", rc.mode)
	}
	cqQIndex := libvpxPublicQuantizerToQIndex(32)
	minQIndex := libvpxPublicQuantizerToQIndex(4)
	if rc.cqLevel != cqQIndex {
		t.Fatalf("Q cqLevel = %d, want qindex %d", rc.cqLevel, cqQIndex)
	}
	if rc.currentQuantizer != minQIndex {
		t.Fatalf("Q current quantizer = %d, want min qindex %d", rc.currentQuantizer, minQIndex)
	}
	if rc.bufferSizeMs != 600 || rc.bufferInitialSizeMs != 400 || rc.bufferOptimalSizeMs != 500 {
		t.Fatalf("Q buffer ms = size:%d initial:%d optimal:%d, want public 600/400/500",
			rc.bufferSizeMs, rc.bufferInitialSizeMs, rc.bufferOptimalSizeMs)
	}

	rc.beginFrame(false)
	if rc.currentQuantizer != minQIndex {
		t.Fatalf("beginFrame Q quantizer = %d, want no CQ floor at min qindex %d", rc.currentQuantizer, minQIndex)
	}
}

func TestRateControlCQAndQValidateLevelAgainstBounds(t *testing.T) {
	modes := []struct {
		name string
		mode RateControlMode
	}{
		{name: "cq", mode: RateControlCQ},
		{name: "q", mode: RateControlQ},
	}
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
	for _, mode := range modes {
		for _, tc := range tests {
			tc.cfg.Mode = mode.mode
			var rc rateControlState
			err := rc.applyConfig(tc.cfg, timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1})
			if err != ErrInvalidQuantizer {
				t.Fatalf("%s %s error = %v, want ErrInvalidQuantizer", mode.name, tc.name, err)
			}
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

	// libvpx default rc_undershoot_pct=100 gates the buffer-aware shrink
	// at percent_low = (optimal-level)/onePercentBits = (2000-900)/21 = 52,
	// uncapped at the libvpx default. target = 1000 - 1000*52/200 = 740.
	if rc.frameTargetBits != 740 {
		t.Fatalf("frameTargetBits = %d, want libvpx low-buffer target 740", rc.frameTargetBits)
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

func TestRateControlVBRHighBufferUsesTotalActualBits(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlVBR,
		overshootPct:      100,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   5000,
		totalActualBits:   1000,
	}
	got := rc.bufferAdjustedFrameTargetBits(1000)
	if got != 1500 {
		t.Fatalf("VBR high-buffer target = %d, want 1500 after total-byte-count capped +50%% boost", got)
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
