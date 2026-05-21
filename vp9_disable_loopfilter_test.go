package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderRejectsInvalidDisableLoopfilter(t *testing.T) {
	opts := VP9EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		DisableLoopfilter: VP9DisableLoopfilter(3),
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("DisableLoopfilter=3 err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderDisableLoopfilterAllZerosFilterLevel(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		DisableLoopfilter: VP9LoopfilterDisableAll,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
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
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		DisableLoopfilter: VP9LoopfilterDisableInter,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
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

func TestVP9EncoderSetDisableLoopfilterUpdatesOption(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetDisableLoopfilter(VP9LoopfilterDisableAll); err != nil {
		t.Fatalf("SetDisableLoopfilter(All): %v", err)
	}
	if e.opts.DisableLoopfilter != VP9LoopfilterDisableAll {
		t.Fatalf("opts.DisableLoopfilter = %d, want All",
			e.opts.DisableLoopfilter)
	}
	if err := e.SetDisableLoopfilter(VP9LoopfilterEnabled); err != nil {
		t.Fatalf("SetDisableLoopfilter(Enabled): %v", err)
	}
	if e.opts.DisableLoopfilter != VP9LoopfilterEnabled {
		t.Fatalf("opts.DisableLoopfilter = %d, want Enabled",
			e.opts.DisableLoopfilter)
	}
	if err := e.SetDisableLoopfilter(VP9DisableLoopfilter(3)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetDisableLoopfilter(3) err = %v, want ErrInvalidConfig", err)
	}
}

func copyTo(dst []byte, src []byte) ([]byte, bool) {
	if len(src) > len(dst) {
		return nil, false
	}
	n := copy(dst, src)
	return dst[:n], true
}
