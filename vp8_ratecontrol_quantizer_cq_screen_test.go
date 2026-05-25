package govpx

import "testing"

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
