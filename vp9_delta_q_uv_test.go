package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderRejectsOutOfRangeDeltaQUV(t *testing.T) {
	for _, delta := range []int{-16, 16, -100, 100} {
		opts := VP9EncoderOptions{
			Width:    64,
			Height:   64,
			FPS:      30,
			DeltaQUV: delta,
		}
		if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidQuantizer) {
			t.Fatalf("DeltaQUV=%d err = %v, want ErrInvalidQuantizer", delta, err)
		}
	}
}

func TestVP9EncoderRejectsLosslessWithDeltaQUV(t *testing.T) {
	opts := VP9EncoderOptions{
		Width:    64,
		Height:   64,
		FPS:      30,
		Lossless: true,
		DeltaQUV: 3,
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("lossless+DeltaQUV err = %v, want ErrInvalidQuantizer", err)
	}
}

func TestVP9EncoderDeltaQUVAppliesToHeader(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    64,
		Height:   64,
		FPS:      30,
		DeltaQUV: 7,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(newVP9YCbCrForTest(64, 64, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto wrote %d bytes", n)
	}
	hdr, _ := parseVP9EncoderHeaderForTest(t, dst[:n])
	if hdr.Quant.UvDcDeltaQ != 7 || hdr.Quant.UvAcDeltaQ != 7 {
		t.Fatalf("UvDcDeltaQ=%d UvAcDeltaQ=%d, want both 7",
			hdr.Quant.UvDcDeltaQ, hdr.Quant.UvAcDeltaQ)
	}
}

func TestVP9EncoderSetDeltaQUVUpdatesOption(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetDeltaQUV(5); err != nil {
		t.Fatalf("SetDeltaQUV(5): %v", err)
	}
	if e.opts.DeltaQUV != 5 {
		t.Fatalf("opts.DeltaQUV = %d, want 5", e.opts.DeltaQUV)
	}
	if err := e.SetDeltaQUV(-3); err != nil {
		t.Fatalf("SetDeltaQUV(-3): %v", err)
	}
	if e.opts.DeltaQUV != -3 {
		t.Fatalf("opts.DeltaQUV = %d, want -3", e.opts.DeltaQUV)
	}
	if err := e.SetDeltaQUV(16); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetDeltaQUV(16) err = %v, want ErrInvalidQuantizer", err)
	}
}
