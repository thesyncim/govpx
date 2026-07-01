package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestInterPredictorCopyFresh: subpel offsets both zero + ref=0 →
// straight copy of (w x h) from src to dst.
func TestInterPredictorCopyFresh(t *testing.T) {
	src := []byte{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	dst := make([]byte, 16)
	for i := range dst {
		dst[i] = 99
	}
	InterPredictor(src, 4, dst, 4, 0, 0, &tables.SubPelFilters8, 16, 16, 4, 4, 0, 0)
	for i := range src {
		if dst[i] != src[i] {
			t.Errorf("[%d]: got %d want %d", i, dst[i], src[i])
		}
	}
}

// TestInterPredictorCopyAvg: subpel offsets both zero + ref=1 → blend
// src with existing dst.
func TestInterPredictorCopyAvg(t *testing.T) {
	src := []byte{10, 20, 30, 40}
	dst := []byte{50, 60, 70, 80}
	want := []byte{30, 40, 50, 60}
	InterPredictor(src, 4, dst, 4, 0, 0, &tables.SubPelFilters8, 16, 16, 4, 1, 1, 0)
	for i := range want {
		if dst[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, dst[i], want[i])
		}
	}
}

// TestInterPredictorScaledHorizDispatchHitsKernel catches the scaled-ref
// integer-origin path: subpel offsets are zero, but x_step_q4 != 16 must
// still dispatch through the horizontal convolve kernel instead of copy.
func TestInterPredictorScaledHorizDispatchHitsKernel(t *testing.T) {
	src := make([]byte, 24*4)
	for i := range src {
		src[i] = byte((i*7 + 3) % 251)
	}
	dst1 := make([]byte, 4*4)
	dst2 := make([]byte, 4*4)
	srcOffset := 3
	InterPredictor(src, 24, dst1, 4, 0, 0, &tables.SubPelFilters8, 32, 16, 4, 4, 0, srcOffset)
	dsp.VpxConvolve8Horiz(src, 24, dst2, 4, &tables.SubPelFilters8, 0, 32, 0, 16, 4, 4, srcOffset)
	for i := range dst1 {
		if dst1[i] != dst2[i] {
			t.Errorf("[%d]: got %d want %d", i, dst1[i], dst2[i])
		}
	}
}

// TestInterPredictorHorizDispatchHitsKernel routes a hasHoriz=1 call
// through VpxConvolve8Horiz and matches a direct call output.
func TestInterPredictorHorizDispatchHitsKernel(t *testing.T) {
	// 16x4 source block with a smooth gradient — gives the H subpel
	// filter a non-degenerate signal to chew on.
	src := make([]byte, 16*4)
	for i := range src {
		src[i] = byte((i * 5) % 250)
	}
	dst1 := make([]byte, 4*4)
	dst2 := make([]byte, 4*4)
	for i := range dst1 {
		dst1[i] = 0
		dst2[i] = 0
	}
	// Pre-offset src by 3 so the H tap window covers pos 0..7.
	srcOffset := 3
	InterPredictor(src, 16, dst1, 4, 4, 0, &tables.SubPelFilters8, 16, 16, 4, 4, 0, srcOffset)
	dsp.VpxConvolve8Horiz(src, 16, dst2, 4, &tables.SubPelFilters8, 4, 16, 0, 16, 4, 4, srcOffset)
	for i := range dst1 {
		if dst1[i] != dst2[i] {
			t.Errorf("[%d]: got %d want %d", i, dst1[i], dst2[i])
		}
	}
}

func TestInterPredictorWithScratchMatchesPooledFull2D(t *testing.T) {
	src := make([]byte, 32*24)
	for i := range src {
		src[i] = byte((i*19 + 11) & 0xff)
	}
	srcOffset := 3*32 + 3
	for _, ref := range []int{0, 1} {
		dstPooled := make([]byte, 16*16)
		dstScratch := make([]byte, 16*16)
		for i := range dstPooled {
			v := byte((i*13 + 5) & 0xff)
			dstPooled[i] = v
			dstScratch[i] = v
		}
		var scratch dsp.Convolve8Scratch
		InterPredictor(src, 32, dstPooled, 16, 4, 8, &tables.SubPelFilters8,
			16, 16, 16, 16, ref, srcOffset)
		InterPredictorWithScratch(src, 32, dstScratch, 16, 4, 8,
			&tables.SubPelFilters8, 16, 16, 16, 16, ref, srcOffset, &scratch)
		for i := range dstPooled {
			if dstScratch[i] != dstPooled[i] {
				t.Fatalf("ref=%d [%d]: scratch got %d want %d",
					ref, i, dstScratch[i], dstPooled[i])
			}
		}
	}
}

func TestInterPredictorDoesNotAllocate(t *testing.T) {
	src := make([]byte, 32*16)
	dst := make([]byte, 8*8)
	for i := range src {
		src[i] = byte((i*11 + 7) & 0xff)
	}
	var scratch dsp.Convolve8Scratch

	allocs := testing.AllocsPerRun(1000, func() {
		InterPredictor(src, 32, dst, 8, 4, 8, &tables.SubPelFilters8, 16, 16, 8, 8, 0, 3*32+3)
		InterPredictorWithScratch(src, 32, dst, 8, 4, 8,
			&tables.SubPelFilters8, 16, 16, 8, 8, 0, 3*32+3, &scratch)
		InterPredictor(src, 32, dst, 8, 0, 0, &tables.SubPelFilters8, 16, 16, 8, 8, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("InterPredictor allocations = %.1f, want 0", allocs)
	}
}
