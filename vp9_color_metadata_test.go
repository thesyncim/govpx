package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderRejectsOutOfRangeColorSpace(t *testing.T) {
	for _, cs := range []govpx.VP9ColorSpace{8, 9, 255} {
		opts := govpx.VP9EncoderOptions{
			Width:      64,
			Height:     64,
			FPS:        30,
			ColorSpace: cs,
		}
		if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("ColorSpace=%d err = %v, want govpx.ErrInvalidConfig", cs, err)
		}
	}
}

func TestVP9EncoderRejectsSRGBOnProfile0(t *testing.T) {
	opts := govpx.VP9EncoderOptions{
		Width:      64,
		Height:     64,
		FPS:        30,
		ColorSpace: govpx.VP9ColorSpaceSRGB,
	}
	if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SRGB+profile0 err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRejectsOutOfRangeColorRange(t *testing.T) {
	opts := govpx.VP9EncoderOptions{
		Width:      64,
		Height:     64,
		FPS:        30,
		ColorRange: govpx.VP9ColorRange(2),
	}
	if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("ColorRange=2 err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func TestVP9EncoderColorSpaceAppliesToHeader(t *testing.T) {
	cases := []struct {
		name string
		cs   govpx.VP9ColorSpace
		want common.ColorSpace
	}{
		{"BT601", govpx.VP9ColorSpaceBT601, common.CSBT601},
		{"BT709", govpx.VP9ColorSpaceBT709, common.CSBT709},
		{"BT2020", govpx.VP9ColorSpaceBT2020, common.CSBT2020},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
				Width:      64,
				Height:     64,
				FPS:        30,
				ColorSpace: tc.cs,
			})
			if err != nil {
				t.Fatalf("govpx.NewVP9Encoder: %v", err)
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
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:      64,
		Height:     64,
		FPS:        30,
		ColorSpace: govpx.VP9ColorSpaceBT709,
		ColorRange: govpx.VP9ColorRangeFull,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
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

func TestVP9EncoderRuntimeColorSpaceAppliesToHeader(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetColorSpace(govpx.VP9ColorSpaceBT709); err != nil {
		t.Fatalf("SetColorSpace(BT709): %v", err)
	}
	hdr := encodeVP9HeaderForColorMetadataTest(t, e)
	if hdr.BitDepthColor.ColorSpace != common.CSBT709 {
		t.Fatalf("ColorSpace = %d, want CSBT709", hdr.BitDepthColor.ColorSpace)
	}
	if err := e.SetColorSpace(govpx.VP9ColorSpace(8)); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetColorSpace(8) err = %v, want govpx.ErrInvalidConfig", err)
	}
	if err := e.SetColorSpace(govpx.VP9ColorSpaceSRGB); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetColorSpace(SRGB) err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRuntimeColorRangeAppliesToHeader(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetColorSpace(govpx.VP9ColorSpaceBT709); err != nil {
		t.Fatalf("SetColorSpace(BT709): %v", err)
	}
	if err := e.SetColorRange(govpx.VP9ColorRangeFull); err != nil {
		t.Fatalf("SetColorRange(Full): %v", err)
	}
	hdr := encodeVP9HeaderForColorMetadataTest(t, e)
	if hdr.BitDepthColor.ColorRange != common.CRFullRange {
		t.Fatalf("ColorRange = %d, want CRFullRange", hdr.BitDepthColor.ColorRange)
	}
	if err := e.SetColorRange(govpx.VP9ColorRange(2)); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetColorRange(2) err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func encodeVP9HeaderForColorMetadataTest(t *testing.T, e *govpx.VP9Encoder) vp9dec.UncompressedHeader {
	t.Helper()
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(64, 64, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto wrote %d bytes", n)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	return hdr
}
