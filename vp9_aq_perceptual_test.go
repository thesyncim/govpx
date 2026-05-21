package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderPerceptualAQValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*VP9EncoderOptions)
		err  error
	}{
		{"lossless", func(o *VP9EncoderOptions) {
			o.Lossless = true
		}, ErrInvalidConfig},
		{"static segmentation", func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := VP9EncoderOptions{
				Width:  64,
				Height: 64,
				FPS:    30,
				AQMode: VP9AQPerceptual,
			}
			tc.mut(&opts)
			if _, err := NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestVP9EncoderPerceptualAQAcceptsConfiguration(t *testing.T) {
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

func TestVP9EncoderPerceptualAQEncodesFrame(t *testing.T) {
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
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if !hdr.Seg.Enabled {
		t.Fatal("segmentation header disabled; perceptual AQ expected to enable it")
	}
}

func TestVP9EncoderPerceptualAQTinyFrameSuppressesNeutralSegmentation(t *testing.T) {
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
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Seg.Enabled {
		t.Fatal("segmentation header enabled; tiny perceptual AQ frame should be a no-op")
	}
}
