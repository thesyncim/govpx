package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9EncoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}

	key := newVP9YCbCrForTest(width, height, 9, 10, 11)
	dst := make([]byte, 1<<16)
	keyResult, err := e.EncodeIntoWithResult(key, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult returned error: %v", err)
	}
	if !keyResult.KeyFrame {
		t.Fatal("first packet was not a key frame")
	}
	keyPacket := append([]byte(nil), keyResult.Data...)

	refYCbCr := newVP9YCbCrForTest(width, height, 33, 44, 55)
	ref := vp9ImageFromYCbCrForTest(refYCbCr)
	if err := e.SetReferenceFrame(ReferenceLast, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}

	interResult, err := e.EncodeIntoWithFlagsResult(refYCbCr, dst,
		EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithFlagsResult returned error: %v", err)
	}
	if interResult.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want externally seeded LAST reference")
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	d.refFrames[vp9LastRefSlot].store(ref)
	if err := d.Decode(interResult.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "inter from encoder-set LAST", ref, got)
}

func TestVP9EncoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	const width, height = 33, 31
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}

	refYCbCr := newVP9MotionYCbCrForTest(width, height)
	ref := vp9ImageFromYCbCrForTest(refYCbCr)
	want := clonePublicImage(ref)
	if err := e.SetReferenceFrame(ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	ref.Y[0] ^= 0xff
	ref.U[0] ^= 0x7f
	ref.V[0] ^= 0x3f

	dstYCbCr := newVP9YCbCrForTest(width, height, 0, 0, 0)
	dst := vp9ImageFromYCbCrForTest(dstYCbCr)
	if err := e.CopyReferenceFrame(ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame returned error: %v", err)
	}
	assertImagesEqual(t, "copied GOLDEN reference", want, dst)
}

func TestVP9EncoderReferenceFrameValidation(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 16, Height: 16, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}
	src := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(16, 16, 1, 2, 3))
	wrongSize := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(32, 16, 1, 2, 3))
	dst := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(16, 16, 0, 0, 0))

	if err := e.CopyReferenceFrame(ReferenceLast, &dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyReferenceFrame before reference error = %v, want ErrInvalidConfig", err)
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: e.SetReferenceFrame(ReferenceFrame(0), src)},
		{name: "set multi ref", err: e.SetReferenceFrame(ReferenceFrame(ReferenceFlagLast|ReferenceFlagGolden), src)},
		{name: "set wrong size", err: e.SetReferenceFrame(ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: e.CopyReferenceFrame(ReferenceFrame(0), &dst)},
		{name: "copy nil dst", err: e.CopyReferenceFrame(ReferenceLast, nil)},
		{name: "copy wrong size", err: e.CopyReferenceFrame(ReferenceLast, &wrongSize)},
	} {
		if !errors.Is(tc.err, ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tc.name, tc.err)
		}
	}

	if err := e.SetReferenceFrame(ReferenceLast, src); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	if err := e.CopyReferenceFrame(ReferenceLast, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame after set returned error: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := e.SetReferenceFrame(ReferenceLast, src); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := e.CopyReferenceFrame(ReferenceLast, &dst); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}

	key := newVP9YCbCrForTest(width, height, 9, 10, 11)
	dst := make([]byte, 1<<16)
	keyResult, err := e.EncodeIntoWithResult(key, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult returned error: %v", err)
	}
	keyPacket := append([]byte(nil), keyResult.Data...)

	refYCbCr := newVP9YCbCrForTest(width, height, 33, 44, 55)
	ref := vp9ImageFromYCbCrForTest(refYCbCr)
	if err := e.SetReferenceFrame(ReferenceLast, ref); err != nil {
		t.Fatalf("encoder SetReferenceFrame returned error: %v", err)
	}
	interResult, err := e.EncodeIntoWithFlagsResult(refYCbCr, dst,
		EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithFlagsResult returned error: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if err := d.SetReferenceFrame(ReferenceLast, ref); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetReferenceFrame before decode error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	if err := d.SetReferenceFrame(ReferenceLast, ref); err != nil {
		t.Fatalf("decoder SetReferenceFrame returned error: %v", err)
	}
	if err := d.Decode(interResult.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "inter from decoder-set LAST", ref, got)
}

func TestVP9DecoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	const width, height = 33, 31
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	key := vp9StubPacketForTest(t, width, height, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}

	ref := vp9ImageFromYCbCrForTest(newVP9MotionYCbCrForTest(width, height))
	want := clonePublicImage(ref)
	if err := d.SetReferenceFrame(ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	ref.Y[0] ^= 0xff
	ref.U[0] ^= 0x7f
	ref.V[0] ^= 0x3f

	dst := newTestImage(width, height)
	if err := d.CopyReferenceFrame(ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame returned error: %v", err)
	}
	assertImagesEqual(t, "copied decoder GOLDEN reference", want, dst)
}

func TestVP9DecoderCopyCurrentFrameCopiesWithoutConsumingQueue(t *testing.T) {
	const width, height = 33, 31
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	dst := newTestImage(width, height)
	if err := d.CopyCurrentFrame(&dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyCurrentFrame before decode error = %v, want ErrInvalidConfig", err)
	}

	key := vp9StubPacketForTest(t, width, height, 0, common.VPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	if err := d.CopyCurrentFrame(&dst); err != nil {
		t.Fatalf("CopyCurrentFrame after consumed NextFrame returned error: %v", err)
	}
	assertImagesEqual(t, "copied current VP9 frame", frame, dst)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("CopyCurrentFrame re-queued NextFrame output")
	}
}

func TestVP9DecoderCopyCurrentFrameAfterDecodeInto(t *testing.T) {
	const width, height = 33, 31
	packet := vp9StubPacketForTest(t, width, height, 0, common.VPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	into := newTestImage(width, height)
	if _, err := d.DecodeInto(packet, &into); err != nil {
		t.Fatalf("DecodeInto returned error: %v", err)
	}
	copied := newTestImage(width, height)
	if err := d.CopyCurrentFrame(&copied); err != nil {
		t.Fatalf("CopyCurrentFrame after DecodeInto returned error: %v", err)
	}
	assertImagesEqual(t, "copied current VP9 DecodeInto frame", into, copied)

	wrongSize := newTestImage(width+1, height)
	if err := d.CopyCurrentFrame(&wrongSize); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyCurrentFrame wrong-size error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := d.CopyCurrentFrame(&copied); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed CopyCurrentFrame error = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderReferenceFrameValidation(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	src := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(16, 16, 1, 2, 3))
	wrongSize := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(32, 16, 1, 2, 3))
	dst := newTestImage(16, 16)

	if err := d.SetReferenceFrame(ReferenceLast, src); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetReferenceFrame before decode error = %v, want ErrInvalidConfig", err)
	}
	if err := d.CopyReferenceFrame(ReferenceLast, &dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyReferenceFrame before decode error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(vp9StubPacketForTest(t, 16, 16, 0, common.DcPred)); err != nil {
		t.Fatalf("Decode keyframe returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: d.SetReferenceFrame(ReferenceFrame(0), src)},
		{name: "set multi ref", err: d.SetReferenceFrame(ReferenceFrame(ReferenceFlagLast|ReferenceFlagGolden), src)},
		{name: "set wrong size", err: d.SetReferenceFrame(ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: d.CopyReferenceFrame(ReferenceFrame(0), &dst)},
		{name: "copy nil dst", err: d.CopyReferenceFrame(ReferenceLast, nil)},
		{name: "copy wrong size", err: d.CopyReferenceFrame(ReferenceLast, &wrongSize)},
	} {
		if !errors.Is(tc.err, ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tc.name, tc.err)
		}
	}
	if err := d.SetReferenceFrame(ReferenceLast, src); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	if err := d.CopyReferenceFrame(ReferenceLast, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame after set returned error: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := d.SetReferenceFrame(ReferenceLast, src); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := d.CopyReferenceFrame(ReferenceLast, &dst); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}
