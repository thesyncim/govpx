package libgopx

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
