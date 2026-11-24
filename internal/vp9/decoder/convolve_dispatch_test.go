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

func TestInterPredictorWithScratchMatchesPooledDispatchCases(t *testing.T) {
	src := make([]byte, 48*32)
	for i := range src {
		src[i] = byte((i*19 + 11) & 0xff)
	}
	srcOffset := 8*48 + 8
	cases := []struct {
		name             string
		subpelX, subpelY int
		ref              int
	}{
		{"case3_avg_vert", 0, 8, 1},
		{"case5_avg_horiz", 4, 0, 1},
		{"case6_full2d", 4, 8, 0},
		{"case7_avg_full2d", 4, 8, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dstPooled := make([]byte, 16*16)
			dstScratch := make([]byte, 16*16)
			dstDirect := make([]byte, 16*16)
			for i := range dstPooled {
				v := byte((i*13 + 5) & 0xff)
				dstPooled[i] = v
				dstScratch[i] = v
				dstDirect[i] = v
			}
			var scratch dsp.Convolve8Scratch
			var directScratch dsp.Convolve8Scratch
			InterPredictor(src, 48, dstPooled, 16, tc.subpelX, tc.subpelY,
				&tables.SubPelFilters8, 16, 16, 16, 16, tc.ref, srcOffset)
			InterPredictorWithScratch(src, 48, dstScratch, 16, tc.subpelX,
				tc.subpelY, &tables.SubPelFilters8, 16, 16, 16, 16, tc.ref,
				srcOffset, &scratch)
			switch tc.name {
			case "case3_avg_vert":
				dsp.VpxConvolve8AvgVertWithScratch(src, 48, dstDirect, 16,
					&tables.SubPelFilters8, tc.subpelX, 16, tc.subpelY, 16,
					16, 16, srcOffset, &directScratch)
			case "case5_avg_horiz":
				dsp.VpxConvolve8AvgHorizWithScratch(src, 48, dstDirect, 16,
					&tables.SubPelFilters8, tc.subpelX, 16, tc.subpelY, 16,
					16, 16, srcOffset, &directScratch)
			case "case6_full2d":
				dsp.VpxConvolve8WithScratch(src, 48, dstDirect, 16,
					&tables.SubPelFilters8, tc.subpelX, 16, tc.subpelY, 16,
					16, 16, srcOffset, &directScratch)
			case "case7_avg_full2d":
				dsp.VpxConvolve8AvgWithScratch(src, 48, dstDirect, 16,
					&tables.SubPelFilters8, tc.subpelX, 16, tc.subpelY, 16,
					16, 16, srcOffset, &directScratch)
			}
			for i := range dstPooled {
				if dstScratch[i] != dstDirect[i] {
					t.Fatalf("scratch [%d]: got %d want direct %d",
						i, dstScratch[i], dstDirect[i])
				}
				if dstPooled[i] != dstDirect[i] {
					t.Fatalf("pooled [%d]: got %d want direct %d",
						i, dstPooled[i], dstDirect[i])
				}
			}
		})
	}
}

func TestInterPredictorWithScratchDoesNotAllocate(t *testing.T) {
	src := make([]byte, 48*32)
	dst := make([]byte, 16*16)
	for i := range src {
		src[i] = byte((i*11 + 7) & 0xff)
	}
	for i := range dst {
		dst[i] = byte((i*17 + 3) & 0xff)
	}
	srcOffset := 8*48 + 8
	var scratch dsp.Convolve8Scratch

	allocs := testing.AllocsPerRun(1000, func() {
		InterPredictorWithScratch(src, 48, dst, 16, 0, 8,
			&tables.SubPelFilters8, 16, 16, 16, 16, 1, srcOffset, &scratch)
		InterPredictorWithScratch(src, 48, dst, 16, 4, 0,
			&tables.SubPelFilters8, 16, 16, 16, 16, 1, srcOffset, &scratch)
		InterPredictorWithScratch(src, 48, dst, 16, 4, 8,
			&tables.SubPelFilters8, 16, 16, 16, 16, 0, srcOffset, &scratch)
		InterPredictorWithScratch(src, 48, dst, 16, 4, 8,
			&tables.SubPelFilters8, 16, 16, 16, 16, 1, srcOffset, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("InterPredictorWithScratch allocations = %.1f, want 0", allocs)
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
