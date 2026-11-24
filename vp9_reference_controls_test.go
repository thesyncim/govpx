package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9EncoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	const width, height = 32, 32
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}

	key := vp9test.NewYCbCr(width, height, 9, 10, 11)
	dst := make([]byte, 1<<16)
	keyResult, err := e.EncodeIntoWithResult(key, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult returned error: %v", err)
	}
	if !keyResult.KeyFrame {
		t.Fatal("first packet was not a key frame")
	}
	keyPacket := append([]byte(nil), keyResult.Data...)

	refYCbCr := vp9test.NewYCbCr(width, height, 33, 44, 55)
	ref := vp9ImageFromYCbCrForTest(refYCbCr)
	if err := e.SetReferenceFrame(govpx.ReferenceLast, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}

	interResult, err := e.EncodeIntoWithFlagsResult(refYCbCr, dst,
		govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithFlagsResult returned error: %v", err)
	}
	if interResult.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want externally seeded LAST reference")
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	if err := d.SetReferenceFrame(govpx.ReferenceLast, ref); err != nil {
		t.Fatalf("decoder SetReferenceFrame returned error: %v", err)
	}
	if err := d.Decode(interResult.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter NextFrame returned no frame")
	}
	assertVP9ImagesEqualForTest(t, ref, got)
}

func TestVP9EncoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	const width, height = 33, 31
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}

	refYCbCr := vp9test.NewMotionYCbCr(width, height)
	ref := vp9ImageFromYCbCrForTest(refYCbCr)
	want := cloneVP9PublicImageForTest(ref)
	if err := e.SetReferenceFrame(govpx.ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	ref.Y[0] ^= 0xff
	ref.U[0] ^= 0x7f
	ref.V[0] ^= 0x3f

	dstYCbCr := vp9test.NewYCbCr(width, height, 0, 0, 0)
	dst := vp9ImageFromYCbCrForTest(dstYCbCr)
	if err := e.CopyReferenceFrame(govpx.ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame returned error: %v", err)
	}
	assertVP9ImagesEqualForTest(t, want, dst)
}

func TestVP9EncoderReferenceFrameValidation(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 16, Height: 16, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}
	src := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(16, 16, 1, 2, 3))
	wrongSize := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(32, 16, 1, 2, 3))
	dst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(16, 16, 0, 0, 0))

	if err := e.CopyReferenceFrame(govpx.ReferenceLast, &dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("CopyReferenceFrame before reference error = %v, want ErrInvalidConfig", err)
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: e.SetReferenceFrame(govpx.ReferenceFrame(0), src)},
		{name: "set multi ref", err: e.SetReferenceFrame(govpx.ReferenceFrame(govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden), src)},
		{name: "set wrong size", err: e.SetReferenceFrame(govpx.ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: e.CopyReferenceFrame(govpx.ReferenceFrame(0), &dst)},
		{name: "copy nil dst", err: e.CopyReferenceFrame(govpx.ReferenceLast, nil)},
		{name: "copy wrong size", err: e.CopyReferenceFrame(govpx.ReferenceLast, &wrongSize)},
	} {
		if !errors.Is(tc.err, govpx.ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tc.name, tc.err)
		}
	}

	if err := e.SetReferenceFrame(govpx.ReferenceLast, src); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	if err := e.CopyReferenceFrame(govpx.ReferenceLast, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame after set returned error: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := e.SetReferenceFrame(govpx.ReferenceLast, src); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := e.CopyReferenceFrame(govpx.ReferenceLast, &dst); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderSetReferenceFrameAffectsNextInterFrame(t *testing.T) {
	const width, height = 32, 32
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}

	key := vp9test.NewYCbCr(width, height, 9, 10, 11)
	dst := make([]byte, 1<<16)
	keyResult, err := e.EncodeIntoWithResult(key, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult returned error: %v", err)
	}
	keyPacket := append([]byte(nil), keyResult.Data...)

	refYCbCr := vp9test.NewYCbCr(width, height, 33, 44, 55)
	ref := vp9ImageFromYCbCrForTest(refYCbCr)
	if err := e.SetReferenceFrame(govpx.ReferenceLast, ref); err != nil {
		t.Fatalf("encoder SetReferenceFrame returned error: %v", err)
	}
	interResult, err := e.EncodeIntoWithFlagsResult(refYCbCr, dst,
		govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithFlagsResult returned error: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if err := d.SetReferenceFrame(govpx.ReferenceLast, ref); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetReferenceFrame before decode error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	if err := d.SetReferenceFrame(govpx.ReferenceLast, ref); err != nil {
		t.Fatalf("decoder SetReferenceFrame returned error: %v", err)
	}
	if err := d.Decode(interResult.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter NextFrame returned no frame")
	}
	assertVP9ImagesEqualForTest(t, ref, got)
}

func TestVP9DecoderCopyReferenceFrameCopiesSelectedReference(t *testing.T) {
	const width, height = 33, 31
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	key := vp9test.StubPacket(t, width, height, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}

	ref := vp9ImageFromYCbCrForTest(vp9test.NewMotionYCbCr(width, height))
	want := cloneVP9PublicImageForTest(ref)
	if err := d.SetReferenceFrame(govpx.ReferenceGolden, ref); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	ref.Y[0] ^= 0xff
	ref.U[0] ^= 0x7f
	ref.V[0] ^= 0x3f

	dst := newVP9TestImageForTest(width, height)
	if err := d.CopyReferenceFrame(govpx.ReferenceGolden, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame returned error: %v", err)
	}
	assertVP9ImagesEqualForTest(t, want, dst)
}

func TestVP9DecoderCopyCurrentFrameCopiesWithoutConsumingQueue(t *testing.T) {
	const width, height = 33, 31
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	dst := newVP9TestImageForTest(width, height)
	if err := d.CopyCurrentFrame(&dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("CopyCurrentFrame before decode error = %v, want ErrInvalidConfig", err)
	}

	key := vp9test.StubPacket(t, width, height, 0, common.VPred)
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
	assertVP9ImagesEqualForTest(t, frame, dst)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("CopyCurrentFrame re-queued NextFrame output")
	}
}

func TestVP9DecoderCopyCurrentFrameAfterDecodeInto(t *testing.T) {
	const width, height = 33, 31
	packet := vp9test.StubPacket(t, width, height, 0, common.VPred)
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	into := newVP9TestImageForTest(width, height)
	if _, err := d.DecodeInto(packet, &into); err != nil {
		t.Fatalf("DecodeInto returned error: %v", err)
	}
	copied := newVP9TestImageForTest(width, height)
	if err := d.CopyCurrentFrame(&copied); err != nil {
		t.Fatalf("CopyCurrentFrame after DecodeInto returned error: %v", err)
	}
	assertVP9ImagesEqualForTest(t, into, copied)

	wrongSize := newVP9TestImageForTest(width+1, height)
	if err := d.CopyCurrentFrame(&wrongSize); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("CopyCurrentFrame wrong-size error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := d.CopyCurrentFrame(&copied); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed CopyCurrentFrame error = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderReferenceFrameValidation(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	src := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(16, 16, 1, 2, 3))
	wrongSize := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(32, 16, 1, 2, 3))
	dst := newVP9TestImageForTest(16, 16)

	if err := d.SetReferenceFrame(govpx.ReferenceLast, src); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetReferenceFrame before decode error = %v, want ErrInvalidConfig", err)
	}
	if err := d.CopyReferenceFrame(govpx.ReferenceLast, &dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("CopyReferenceFrame before decode error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Decode(vp9test.StubPacket(t, 16, 16, 0, common.DcPred)); err != nil {
		t.Fatalf("Decode keyframe returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("key NextFrame returned no frame")
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "set invalid ref", err: d.SetReferenceFrame(govpx.ReferenceFrame(0), src)},
		{name: "set multi ref", err: d.SetReferenceFrame(govpx.ReferenceFrame(govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden), src)},
		{name: "set wrong size", err: d.SetReferenceFrame(govpx.ReferenceLast, wrongSize)},
		{name: "copy invalid ref", err: d.CopyReferenceFrame(govpx.ReferenceFrame(0), &dst)},
		{name: "copy nil dst", err: d.CopyReferenceFrame(govpx.ReferenceLast, nil)},
		{name: "copy wrong size", err: d.CopyReferenceFrame(govpx.ReferenceLast, &wrongSize)},
	} {
		if !errors.Is(tc.err, govpx.ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tc.name, tc.err)
		}
	}
	if err := d.SetReferenceFrame(govpx.ReferenceLast, src); err != nil {
		t.Fatalf("SetReferenceFrame returned error: %v", err)
	}
	if err := d.CopyReferenceFrame(govpx.ReferenceLast, &dst); err != nil {
		t.Fatalf("CopyReferenceFrame after set returned error: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := d.SetReferenceFrame(govpx.ReferenceLast, src); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetReferenceFrame error = %v, want ErrClosed", err)
	}
	if err := d.CopyReferenceFrame(govpx.ReferenceLast, &dst); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed CopyReferenceFrame error = %v, want ErrClosed", err)
	}
}
