package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderEquator360AQValidation(t *testing.T) {
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
				AQMode: VP9AQEquator360,
			}
			tc.mut(&opts)
			if _, err := NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestVP9EncoderEquator360AQAcceptsConfiguration(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		AQMode: VP9AQEquator360,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.opts.AQMode != VP9AQEquator360 {
		t.Fatalf("opts.AQMode = %d, want %d", e.opts.AQMode, VP9AQEquator360)
	}
	dst := make([]byte, 65536)
	src := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
}
