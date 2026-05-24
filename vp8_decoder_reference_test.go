package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestDecoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	ref := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(ref, 33, 44, 55)
	if err := d.SetReferenceFrame(govpx.ReferenceLast, ref); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetReferenceFrame before initialization error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	if err := d.SetReferenceFrame(govpx.ReferenceLast, ref); err != nil {
		t.Fatalf("SetReferenceFrame error = %v, want nil", err)
	}
	if err := d.Decode(vp8test.InterFramePacketWithFirstPartition(vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.OnePartition, false, 0))); err != nil {
		t.Fatalf("inter Decode error = %v, want nil", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no inter frame")
	}
	assertVP8FacadeImagesEqual(t, "inter from replaced last reference", ref, got)
}

func TestDecoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8FacadeImage(16, 16)
	if err := d.CopyReferenceFrame(govpx.ReferenceGolden, &dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("CopyReferenceFrame before initialization error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	ref := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(ref, 21, 22, 23)
	if err := d.SetReferenceFrame(govpx.ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame error = %v, want nil", err)
	}
	fillVP8FacadeImage(dst, 0, 0, 0)
	if err := d.CopyReferenceFrame(govpx.ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame error = %v, want nil", err)
	}
	assertVP8FacadeImagesEqual(t, "copied golden reference", ref, dst)
}

func TestDecoderReferenceFrameValidation(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	src := newVP8FacadeImage(16, 16)
	wrongSize := newVP8FacadeImage(8, 8)
	tests := []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: d.SetReferenceFrame(govpx.ReferenceFrame(0), src)},
		{name: "set multi ref", err: d.SetReferenceFrame(govpx.ReferenceFrame(govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden), src)},
		{name: "set wrong size", err: d.SetReferenceFrame(govpx.ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: d.CopyReferenceFrame(govpx.ReferenceFrame(0), &src)},
		{name: "copy nil dst", err: d.CopyReferenceFrame(govpx.ReferenceLast, nil)},
		{name: "copy wrong size", err: d.CopyReferenceFrame(govpx.ReferenceLast, &wrongSize)},
	}
	for _, tt := range tests {
		if !errors.Is(tt.err, govpx.ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tt.name, tt.err)
		}
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close error = %v, want nil", err)
	}
	if err := d.SetReferenceFrame(govpx.ReferenceLast, src); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := d.CopyReferenceFrame(govpx.ReferenceLast, &src); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}
