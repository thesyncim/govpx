package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderPerceptualAQEnablesState(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.perceptualAQ.Enabled {
		t.Fatal("perceptualAQ.Enabled = false, want true")
	}
}

func TestVP9EncoderPerceptualAQSetsReadyAfterEncode(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  256,
		Height: 128,
		FPS:    30,
		AQMode: VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// A non-flat checker pattern exercises both ZERO and AC coefficients.
	src := vp9test.NewCheckerYCbCr(256, 128, 32, 224, 128, 128)
	dst := make([]byte, 1<<20)
	n, err := e.EncodeInto(src, dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto returned %d bytes", n)
	}
	if !e.perceptualAQ.Ready {
		t.Fatal("perceptualAQ.Ready = false after encode")
	}
}

func TestVP9EncoderPerceptualAQTinyFrameLeavesStateNotReady(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(src, dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto returned %d bytes", n)
	}
	if e.perceptualAQ.Ready {
		t.Fatal("perceptualAQ.Ready = true for a frame too small to cluster")
	}
}
