package govpx

import (
	"errors"
	"testing"
)

func TestDecoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	ref := newTestImage(16, 16)
	fillImage(ref, 33, 44, 55)
	if err := d.SetReferenceFrame(ReferenceLast, ref); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetReferenceFrame before initialization error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	if err := d.SetReferenceFrame(ReferenceLast, ref); err != nil {
		t.Fatalf("SetReferenceFrame error = %v, want nil", err)
	}
	if err := d.Decode(vp8InterFramePacketWithFirstPartition(vp8InterFirstPartitionLastZeroMV())); err != nil {
		t.Fatalf("inter Decode error = %v, want nil", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no inter frame")
	}
	assertImagesEqual(t, "inter from replaced last reference", ref, got)
}

func TestDecoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	if err := d.CopyReferenceFrame(ReferenceGolden, &dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyReferenceFrame before initialization error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	ref := newTestImage(16, 16)
	fillImage(ref, 21, 22, 23)
	if err := d.SetReferenceFrame(ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame error = %v, want nil", err)
	}
	fillImage(dst, 0, 0, 0)
	if err := d.CopyReferenceFrame(ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame error = %v, want nil", err)
	}
	assertImagesEqual(t, "copied golden reference", ref, dst)
}

func TestDecoderReferenceFrameValidation(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	src := newTestImage(16, 16)
	wrongSize := newTestImage(8, 8)
	tests := []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: d.SetReferenceFrame(ReferenceFrame(0), src)},
		{name: "set multi ref", err: d.SetReferenceFrame(ReferenceFrame(ReferenceFlagLast|ReferenceFlagGolden), src)},
		{name: "set wrong size", err: d.SetReferenceFrame(ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: d.CopyReferenceFrame(ReferenceFrame(0), &src)},
		{name: "copy nil dst", err: d.CopyReferenceFrame(ReferenceLast, nil)},
		{name: "copy wrong size", err: d.CopyReferenceFrame(ReferenceLast, &wrongSize)},
	}
	for _, tt := range tests {
		if !errors.Is(tt.err, ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tt.name, tt.err)
		}
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close error = %v, want nil", err)
	}
	if err := d.SetReferenceFrame(ReferenceLast, src); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := d.CopyReferenceFrame(ReferenceLast, &src); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}
