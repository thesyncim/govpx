package libgopx

import (
	"errors"
	"testing"
)

func TestNewVP8DecoderValidation(t *testing.T) {
	_, err := NewVP8Decoder(DecoderOptions{Threads: -1})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeRequiresInitialKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8InterFramePacket(0, 0, true))
	if !errors.Is(err, ErrNeedKeyFrame) {
		t.Fatalf("error = %v, want ErrNeedKeyFrame", err)
	}
}

func TestDecodeStubReturnsUnsupportedAfterValidation(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{MaxWidth: 640, MaxHeight: 480})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.DecodeWithPTS(vp8KeyFramePacket(320, 240, 0, 0, true), 44)
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("error = %v, want ErrUnsupportedFeature", err)
	}
	if d.lastInfo.Width != 320 || d.lastInfo.Height != 240 || d.lastInfo.PTS != 44 {
		t.Fatalf("lastInfo = %+v, want validated frame metadata", d.lastInfo)
	}
}

func TestDecodeInitializesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacket(5, 3, 0, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.current.Img.Width != 5 || d.current.Img.Height != 3 {
		t.Fatalf("current visible dimensions = %dx%d, want 5x3", d.current.Img.Width, d.current.Img.Height)
	}
	if d.current.Img.CodedWidth != 16 || d.current.Img.CodedHeight != 16 {
		t.Fatalf("current coded dimensions = %dx%d, want 16x16", d.current.Img.CodedWidth, d.current.Img.CodedHeight)
	}
	if d.lastRef.BufferLen() == 0 || d.goldenRef.BufferLen() == 0 || d.altRef.BufferLen() == 0 {
		t.Fatalf("reference buffers were not initialized")
	}
}

func TestDecodeReusesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacket(16, 16, 0, 0, true)
	_ = d.Decode(packet)
	firstY := &d.current.Img.Y[0]
	firstLastY := &d.lastRef.Img.Y[0]

	_ = d.Decode(packet)

	if &d.current.Img.Y[0] != firstY {
		t.Fatalf("current frame buffer was reallocated for same resolution")
	}
	if &d.lastRef.Img.Y[0] != firstLastY {
		t.Fatalf("last reference buffer was reallocated for same resolution")
	}
}

func TestDecodeIntoRejectsNilImage(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_, err = d.DecodeInto(vp8KeyFramePacket(16, 16, 0, 0, true), nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecoderHotPathAllocs(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacket(64, 64, 0, 0, true)
	dst := Image{Width: 64, Height: 64}

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "Decode", fn: func() { _ = d.Decode(packet) }},
		{name: "DecodeInto", fn: func() { _, _ = d.DecodeInto(packet, &dst) }},
		{name: "NextFrame", fn: func() { _, _ = d.NextFrame() }},
		{name: "Reset", fn: func() { d.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}
}
