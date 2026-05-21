package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9EncoderRejectsOutOfRangeColorSpace(t *testing.T) {
	for _, cs := range []VP9ColorSpace{8, 9, 255} {
		opts := VP9EncoderOptions{
			Width:      64,
			Height:     64,
			FPS:        30,
			ColorSpace: cs,
		}
		if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("ColorSpace=%d err = %v, want ErrInvalidConfig", cs, err)
		}
	}
}

func TestVP9EncoderRejectsSRGBOnProfile0(t *testing.T) {
	opts := VP9EncoderOptions{
		Width:      64,
		Height:     64,
		FPS:        30,
		ColorSpace: VP9ColorSpaceSRGB,
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SRGB+profile0 err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRejectsOutOfRangeColorRange(t *testing.T) {
	opts := VP9EncoderOptions{
		Width:      64,
		Height:     64,
		FPS:        30,
		ColorRange: VP9ColorRange(2),
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("ColorRange=2 err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderColorSpaceAppliesToHeader(t *testing.T) {
	cases := []struct {
		name string
		cs   VP9ColorSpace
		want common.ColorSpace
	}{
		{"BT601", VP9ColorSpaceBT601, common.CSBT601},
		{"BT709", VP9ColorSpaceBT709, common.CSBT709},
		{"BT2020", VP9ColorSpaceBT2020, common.CSBT2020},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:      64,
				Height:     64,
				FPS:        30,
				ColorSpace: tc.cs,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			dst := make([]byte, 65536)
			n, err := e.EncodeInto(vp9test.NewYCbCr(64, 64, 128, 128, 128), dst)
			if err != nil {
				t.Fatalf("EncodeInto: %v", err)
			}
			if n <= 0 {
				t.Fatalf("EncodeInto wrote %d bytes", n)
			}
			hdr, _ := vp9test.ParseHeader(t, dst[:n])
			if hdr.BitDepthColor.ColorSpace != tc.want {
				t.Fatalf("ColorSpace = %d, want %d",
					hdr.BitDepthColor.ColorSpace, tc.want)
			}
		})
	}
}

func TestVP9EncoderColorRangeAppliesToHeader(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:      64,
		Height:     64,
		FPS:        30,
		ColorSpace: VP9ColorSpaceBT709,
		ColorRange: VP9ColorRangeFull,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(64, 64, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.BitDepthColor.ColorRange != common.CRFullRange {
		t.Fatalf("ColorRange = %d, want CRFullRange", hdr.BitDepthColor.ColorRange)
	}
}

func TestVP9EncoderSetColorSpaceUpdatesOption(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetColorSpace(VP9ColorSpaceBT709); err != nil {
		t.Fatalf("SetColorSpace(BT709): %v", err)
	}
	if e.opts.ColorSpace != VP9ColorSpaceBT709 {
		t.Fatalf("opts.ColorSpace = %d, want BT709", e.opts.ColorSpace)
	}
	if err := e.SetColorSpace(VP9ColorSpace(8)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetColorSpace(8) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetColorSpace(VP9ColorSpaceSRGB); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetColorSpace(SRGB) err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetColorRangeUpdatesOption(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetColorRange(VP9ColorRangeFull); err != nil {
		t.Fatalf("SetColorRange(Full): %v", err)
	}
	if e.opts.ColorRange != VP9ColorRangeFull {
		t.Fatalf("opts.ColorRange = %d, want Full", e.opts.ColorRange)
	}
	if err := e.SetColorRange(VP9ColorRange(2)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetColorRange(2) err = %v, want ErrInvalidConfig", err)
	}
}
