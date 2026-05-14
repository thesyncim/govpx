package coracle

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVpxencVP9EncodeI420RejectsInvalidInputBeforePathLookup(t *testing.T) {
	if _, _, err := VpxencVP9EncodeI420(nil, 16, 16, 1); err == nil {
		t.Fatalf("VpxencVP9EncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencVP9NotBuilt) {
		t.Fatalf("VpxencVP9EncodeI420 looked up vpxenc before validating input")
	}
	if _, _, err := VpxencVP9EncodeI420(make([]byte, 384), 0, 16, 1); err == nil {
		t.Fatalf("VpxencVP9EncodeI420 accepted zero width")
	}
}

func TestVpxencVP9EncodeI420ProducesProfile0IVF(t *testing.T) {
	if _, err := VpxencVP9Path(); err != nil {
		if errors.Is(err, ErrVpxencVP9NotBuilt) {
			t.Skip("vpxenc-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxencVP9Path: %v", err)
	}

	const width, height, frames = 32, 32, 2
	raw := makeGeneratedVP9I420(width, height, frames)
	ivf, diag, err := VpxencVP9EncodeI420(raw, width, height, frames)
	if err != nil {
		t.Fatalf("VpxencVP9EncodeI420 failed: %v\n%s", err, diag)
	}
	h, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader: %v", err)
	}
	if h.FourCC != [4]byte{'V', 'P', '9', '0'} {
		t.Fatalf("FourCC = %q, want VP90", h.FourCC)
	}
	if h.Width != width || h.Height != height || h.FrameCount != frames {
		t.Fatalf("header = %dx%d frames=%d, want %dx%d frames=%d",
			h.Width, h.Height, h.FrameCount, width, height, frames)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != frames {
		t.Fatalf("IVF frame count = %d, want %d", count, frames)
	}
}

func makeGeneratedVP9I420(width int, height int, frames int) []byte {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	frameSize := width*height + 2*uvWidth*uvHeight
	raw := make([]byte, 0, frameSize*frames)
	for frame := 0; frame < frames; frame++ {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				raw = append(raw, byte(24+(x*7+y*11+frame*13)%208))
			}
		}
		for y := 0; y < uvHeight; y++ {
			for x := 0; x < uvWidth; x++ {
				raw = append(raw, byte(80+(x*5+y*3+frame*9)%96))
			}
		}
		for y := 0; y < uvHeight; y++ {
			for x := 0; x < uvWidth; x++ {
				raw = append(raw, byte(96+(x*3+y*7+frame*5)%96))
			}
		}
	}
	return raw
}
