package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestNewVP9DecoderRejectsInvalidByteAlignment(t *testing.T) {
	cases := []int{-1, 1, 16, 31, 33, 48, 2048}
	for _, alignment := range cases {
		_, err := NewVP9Decoder(VP9DecoderOptions{
			ByteAlignment: alignment,
		})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("ByteAlignment=%d err = %v, want ErrInvalidConfig",
				alignment, err)
		}
	}
}

func TestVP9DecoderSetByteAlignmentValidation(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for _, alignment := range []int{0, 32, 64, 1024} {
		if err := d.SetByteAlignment(alignment); err != nil {
			t.Fatalf("SetByteAlignment(%d): %v", alignment, err)
		}
		if got := d.opts.ByteAlignment; got != alignment {
			t.Fatalf("SetByteAlignment(%d) stored %d", alignment, got)
		}
	}
	for _, alignment := range []int{-1, 1, 16, 48, 2048} {
		if err := d.SetByteAlignment(alignment); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetByteAlignment(%d) err = %v, want ErrInvalidConfig",
				alignment, err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SetByteAlignment(64); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetByteAlignment err = %v, want ErrClosed", err)
	}
	var nilDecoder *VP9Decoder
	if err := nilDecoder.SetByteAlignment(64); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil SetByteAlignment err = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderByteAlignmentAlignsVisiblePlanes(t *testing.T) {
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{ByteAlignment: 128})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no visible frame")
	}
	assertVP9NeutralFrame(t, frame, 64, 64)
	assertVP9PlaneAligned(t, "Y", frame.Y, 128)
	assertVP9PlaneAligned(t, "U", frame.U, 128)
	assertVP9PlaneAligned(t, "V", frame.V, 128)
}

func TestVP9DecoderSetByteAlignmentAppliesToFutureFrames(t *testing.T) {
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("initial Decode: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("initial NextFrame returned no visible frame")
	}
	if err := d.SetByteAlignment(256); err != nil {
		t.Fatalf("SetByteAlignment: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("aligned Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("aligned NextFrame returned no visible frame")
	}
	assertVP9PlaneAligned(t, "Y", frame.Y, 256)
	assertVP9PlaneAligned(t, "U", frame.U, 256)
	assertVP9PlaneAligned(t, "V", frame.V, 256)
}

func TestVP9DecoderByteAlignmentPreservesPixels(t *testing.T) {
	packet := vp9StubPacketForTest(t, 96, 80, 0, common.DcPred)
	want := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{}, packet)
	got := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{ByteAlignment: 512}, packet)
	assertVP9ImagesEqual(t, want, got)
	assertVP9PlaneAligned(t, "Y", got.Y, 512)
	assertVP9PlaneAligned(t, "U", got.U, 512)
	assertVP9PlaneAligned(t, "V", got.V, 512)
}

func TestVP9DecoderByteAlignmentAlignsShowExistingFrame(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{ByteAlignment: 256})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned no keyframe")
	}
	if err := d.Decode(vp9ShowExistingFramePacketForTest(5)); err != nil {
		t.Fatalf("Decode show-existing: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no show-existing frame")
	}
	assertVP9NeutralFrame(t, frame, 64, 64)
	assertVP9PlaneAligned(t, "Y", frame.Y, 256)
	assertVP9PlaneAligned(t, "U", frame.U, 256)
	assertVP9PlaneAligned(t, "V", frame.V, 256)
}

func TestVP9DecoderByteAlignmentAlignsPostProcessedFrame(t *testing.T) {
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{
		ByteAlignment:    128,
		PostProcessFlags: PostProcessDeblock,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no visible frame")
	}
	assertVP9PlaneAligned(t, "Y", frame.Y, 128)
	assertVP9PlaneAligned(t, "U", frame.U, 128)
	assertVP9PlaneAligned(t, "V", frame.V, 128)
}

func assertVP9PlaneAligned(t *testing.T, name string, plane []byte, alignment int) {
	t.Helper()
	if len(plane) == 0 {
		t.Fatalf("%s plane is empty", name)
	}
	ptr := uintptr(unsafe.Pointer(&plane[0]))
	if ptr%uintptr(alignment) != 0 {
		t.Fatalf("%s plane pointer %#x is not %d-byte aligned",
			name, ptr, alignment)
	}
}
