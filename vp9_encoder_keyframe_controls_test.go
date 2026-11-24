package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderAdaptiveKeyFramesPromotesSceneCutResetsCounter(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		MaxKeyframeInterval: 999,
		AdaptiveKeyFrames:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 16, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	if !key.KeyFrame {
		t.Fatal("first VP9 frame was not a keyframe")
	}
	cut, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 240, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode scene cut: %v", err)
	}
	if !cut.KeyFrame {
		t.Fatal("adaptive scene-cut frame KeyFrame = false, want true")
	}
	if e.framesSinceKey != 0 {
		t.Fatalf("framesSinceKey after adaptive keyframe = %d, want 0",
			e.framesSinceKey)
	}
}
