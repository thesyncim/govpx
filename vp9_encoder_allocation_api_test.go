package govpx_test

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

const (
	vp9EncoderKeyframeAllocRunsForTest = 10
	vp9EncoderInterAllocRunsForTest    = 3
)

func TestVP9EncoderTileRowsSteadyStateAlloc(t *testing.T) {
	const width, height = 1024, 128
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := [4]*image.YCbCr{}
	for i := range frames {
		frames[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	dst := make([]byte, 1<<20)
	for i := range frames {
		if _, err := e.EncodeInto(frames[i], dst); err != nil {
			t.Fatalf("warm EncodeInto[%d]: %v", i, err)
		}
	}
	idx := 0
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		frame := frames[idx&3]
		idx++
		if _, err := e.EncodeInto(frame, dst); err != nil {
			t.Fatalf("EncodeInto tile-row alloc run: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("tile-row EncodeInto steady-state allocs = %f, want 0", allocs)
	}
}
