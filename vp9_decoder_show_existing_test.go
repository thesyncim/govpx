package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9DecoderShowExistingFrameUsesReferenceSlot(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe err = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after visible keyframe")
	}

	if err := d.Decode(vp9test.ShowExistingFramePacket(5)); err != nil {
		t.Fatalf("Decode show-existing err = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after show-existing frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter after show-existing err = %v, want nil", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
}

// TestVP9DecoderRejectsShowExistingMissingReference rejects a show-
// existing packet before any frame has refreshed the requested slot.

func TestVP9DecoderRejectsShowExistingMissingReference(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(vp9test.ShowExistingFramePacket(0))
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame published output for invalid show-existing frame")
	}
}

// TestVP9DecoderDecodeIntoCopiesVisibleFrame mirrors the VP8
// caller-owned-output path for the VP9 reconstruction slice.
