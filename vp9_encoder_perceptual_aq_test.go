package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderPerceptualAQValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*govpx.VP9EncoderOptions)
		err  error
	}{
		{"lossless", func(o *govpx.VP9EncoderOptions) {
			o.Lossless = true
		}, govpx.ErrInvalidConfig},
		{"static segmentation", func(o *govpx.VP9EncoderOptions) {
			o.Segmentation.Enabled = true
		}, govpx.ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := govpx.VP9EncoderOptions{
				Width:  64,
				Height: 64,
				FPS:    30,
				AQMode: govpx.VP9AQPerceptual,
			}
			tc.mut(&opts)
			if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestVP9EncoderPerceptualAQEmitsSegmentationHeader(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  256,
		Height: 128,
		FPS:    30,
		AQMode: govpx.VP9AQPerceptual,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewCheckerYCbCr(256, 128, 32, 224, 128, 128)
	dst := make([]byte, 1<<20)
	n, err := e.EncodeInto(src, dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto returned %d bytes", n)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if !hdr.Seg.Enabled {
		t.Fatal("segmentation header disabled; perceptual AQ expected to enable it")
	}
}

func TestVP9EncoderPerceptualAQTinyFrameLeavesSegmentationDisabled(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: govpx.VP9AQPerceptual,
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
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Seg.Enabled {
		t.Fatal("segmentation header enabled; tiny perceptual AQ frame should be a no-op")
	}
}
