package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderRejectsInvalidDisableLoopfilter(t *testing.T) {
	opts := govpx.VP9EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		DisableLoopfilter: govpx.VP9DisableLoopfilter(3),
	}
	if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("DisableLoopfilter=3 err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func TestVP9EncoderDisableLoopfilterAllZerosFilterLevel(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		DisableLoopfilter: govpx.VP9LoopfilterDisableAll,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(width, height, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Loopfilter.FilterLevel != 0 {
		t.Fatalf("DisableAll FilterLevel = %d, want 0", hdr.Loopfilter.FilterLevel)
	}
}

func TestVP9EncoderDisableLoopfilterInterOnlyAffectsNonKeyFrames(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		DisableLoopfilter: govpx.VP9LoopfilterDisableInter,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	// Keyframe should still carry a non-zero filter level (the encoder
	// derives the default level from base qindex on key frames).
	key, err := e.Encode(vp9test.NewYCbCr(width, height, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, ok := copyTo(dst, key); !ok {
		t.Fatalf("keyframe too large for dst (%d > %d)", len(key), len(dst))
	}
	keyHdr, _ := vp9test.ParseHeader(t, dst[:len(key)])
	if keyHdr.Loopfilter.FilterLevel == 0 {
		t.Fatalf("DisableInter zeroed keyframe FilterLevel; want non-zero")
	}
	// Non-keyframe must zero the filter level.
	inter, err := e.Encode(vp9test.NewYCbCr(width, height, 64, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if _, ok := copyTo(dst, inter); !ok {
		t.Fatalf("inter too large for dst (%d > %d)", len(inter), len(dst))
	}
	interHdr, _ := vp9test.ParseHeader(t, dst[:len(inter)])
	if interHdr.Loopfilter.FilterLevel != 0 {
		t.Fatalf("DisableInter inter FilterLevel = %d, want 0",
			interHdr.Loopfilter.FilterLevel)
	}
}

func TestVP9EncoderRuntimeDisableLoopfilterAffectsHeaders(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetDisableLoopfilter(govpx.VP9LoopfilterDisableAll); err != nil {
		t.Fatalf("SetDisableLoopfilter(All): %v", err)
	}
	hdr := encodeVP9HeaderForDisableLoopfilterTest(t, e)
	if hdr.Loopfilter.FilterLevel != 0 {
		t.Fatalf("DisableAll FilterLevel = %d, want 0", hdr.Loopfilter.FilterLevel)
	}

	e, err = govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetDisableLoopfilter(govpx.VP9LoopfilterEnabled); err != nil {
		t.Fatalf("SetDisableLoopfilter(Enabled): %v", err)
	}
	hdr = encodeVP9HeaderForDisableLoopfilterTest(t, e)
	if hdr.Loopfilter.FilterLevel == 0 {
		t.Fatalf("Enabled FilterLevel = 0, want non-zero")
	}
	if err := e.SetDisableLoopfilter(govpx.VP9DisableLoopfilter(3)); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetDisableLoopfilter(3) err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func encodeVP9HeaderForDisableLoopfilterTest(t *testing.T, e *govpx.VP9Encoder) vp9dec.UncompressedHeader {
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

func copyTo(dst []byte, src []byte) ([]byte, bool) {
	if len(src) > len(dst) {
		return nil, false
	}
	n := copy(dst, src)
	return dst[:n], true
}
