package govpx

import (
	"testing"
)

func TestVP9EncoderSetRTCExternalRateControlSuppressesSceneCutPromotion(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		AdaptiveKeyFrames:   true,
		MaxKeyframeInterval: 256,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyframe := newVP9CheckerYCbCrForTest(width, height, 16, 240, 64, 192)
	if _, err := e.Encode(keyframe); err != nil {
		t.Fatalf("Encode first frame: %v", err)
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	sceneCutSrc := newVP9YCbCrForTest(width, height, 200, 64, 64)
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	if got := e.shouldEncodeVP9SceneCutKeyFrame(sceneCutSrc, EncodeFlags(0),
		false, rows, cols); got {
		t.Fatalf("RTC-external scene-cut promotion = true, want false")
	}
	if err := e.SetRTCExternalRateControl(false); err != nil {
		t.Fatalf("SetRTCExternalRateControl(false): %v", err)
	}
	// With RTC-external off, the same scene-cut input should be eligible
	// for promotion again (subject to the other gates). We only assert the
	// option toggle restores the gating predicate; the actual decision
	// belongs to TestVP9EncoderAdaptiveKeyframePromotesSceneCut.
	if e.opts.RTCExternalRateControl {
		t.Fatal("RTCExternalRateControl stayed true after disable")
	}
}

