package govpx

import "testing"

func TestVP9EncoderLastQuantizerInvalidBeforeCommit(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if pub, internal, ok := e.LastQuantizer(); ok || pub != 0 || internal != 0 {
		t.Fatalf("LastQuantizer before encode = (%d, %d, %v), want (0, 0, false)",
			pub, internal, ok)
	}
}

func TestVP9EncoderLastQuantizerMirrorsEncodeResult(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	src := newVP9YCbCrForTest(64, 64, 96, 128, 128)
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	pub, internal, ok := e.LastQuantizer()
	if !ok {
		t.Fatal("LastQuantizer after first encode reports !ok")
	}
	if pub != result.Quantizer || internal != result.InternalQuantizer {
		t.Fatalf("LastQuantizer = (%d, %d), want (%d, %d)",
			pub, internal, result.Quantizer, result.InternalQuantizer)
	}
	inter, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult inter: %v", err)
	}
	pub2, internal2, ok := e.LastQuantizer()
	if !ok {
		t.Fatal("LastQuantizer after inter encode reports !ok")
	}
	if pub2 != inter.Quantizer || internal2 != inter.InternalQuantizer {
		t.Fatalf("LastQuantizer after inter = (%d, %d), want (%d, %d)",
			pub2, internal2, inter.Quantizer, inter.InternalQuantizer)
	}
}

func TestVP9EncoderLastQuantizerNilAndClosed(t *testing.T) {
	var nilEnc *VP9Encoder
	if pub, internal, ok := nilEnc.LastQuantizer(); ok || pub != 0 || internal != 0 {
		t.Fatalf("nil LastQuantizer = (%d, %d, %v), want zeros/false",
			pub, internal, ok)
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9YCbCrForTest(64, 64, 96, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(src, dst); err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if _, _, ok := e.LastQuantizer(); !ok {
		t.Fatal("LastQuantizer pre-close reports !ok")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if pub, internal, ok := e.LastQuantizer(); ok || pub != 0 || internal != 0 {
		t.Fatalf("LastQuantizer after Close = (%d, %d, %v), want zeros/false",
			pub, internal, ok)
	}
}
