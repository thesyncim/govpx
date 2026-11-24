package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderRejectsOutOfRangeDeltaQUV(t *testing.T) {
	for _, delta := range []int{-16, 16, -100, 100} {
		opts := govpx.VP9EncoderOptions{
			Width:    64,
			Height:   64,
			FPS:      30,
			DeltaQUV: delta,
		}
		if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidQuantizer) {
			t.Fatalf("DeltaQUV=%d err = %v, want govpx.ErrInvalidQuantizer", delta, err)
		}
	}
}

func TestVP9EncoderRejectsLosslessWithDeltaQUV(t *testing.T) {
	opts := govpx.VP9EncoderOptions{
		Width:    64,
		Height:   64,
		FPS:      30,
		Lossless: true,
		DeltaQUV: 3,
	}
	if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidQuantizer) {
		t.Fatalf("lossless+DeltaQUV err = %v, want govpx.ErrInvalidQuantizer", err)
	}
}

func TestVP9EncoderDeltaQUVAppliesToHeader(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:    64,
		Height:   64,
		FPS:      30,
		DeltaQUV: 7,
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
	if hdr.Quant.UvDcDeltaQ != 7 || hdr.Quant.UvAcDeltaQ != 7 {
		t.Fatalf("UvDcDeltaQ=%d UvAcDeltaQ=%d, want both 7",
			hdr.Quant.UvDcDeltaQ, hdr.Quant.UvAcDeltaQ)
	}
}

func TestVP9EncoderRuntimeDeltaQUVAppliesToHeader(t *testing.T) {
	cases := []struct {
		name  string
		delta int
	}{
		{"positive", 5},
		{"negative", -3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
			if err != nil {
				t.Fatalf("govpx.NewVP9Encoder: %v", err)
			}
			if err := e.SetDeltaQUV(tc.delta); err != nil {
				t.Fatalf("SetDeltaQUV(%d): %v", tc.delta, err)
			}
			hdr := encodeVP9HeaderForDeltaQUVTest(t, e)
			if int(hdr.Quant.UvDcDeltaQ) != tc.delta || int(hdr.Quant.UvAcDeltaQ) != tc.delta {
				t.Fatalf("delta=%d produced UvDcDeltaQ=%d UvAcDeltaQ=%d",
					tc.delta, hdr.Quant.UvDcDeltaQ, hdr.Quant.UvAcDeltaQ)
			}
		})
	}

	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetDeltaQUV(16); !errors.Is(err, govpx.ErrInvalidQuantizer) {
		t.Fatalf("SetDeltaQUV(16) err = %v, want govpx.ErrInvalidQuantizer", err)
	}
}

func encodeVP9HeaderForDeltaQUVTest(t *testing.T, e *govpx.VP9Encoder) vp9dec.UncompressedHeader {
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
