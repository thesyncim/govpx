package dsp

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Reference scalar implementations of the three SIMD-accelerated
// 16-wide kernels. These mirror the pure-Go fallbacks bit-for-bit
// (see variance_*_other.go) so the SIMD parity test can compare to
// them on every architecture, regardless of build tags. They are
// intentionally simple straight-line ports of the libvpx scalar
// reference; no Go-compiler tricks. Used only for testing.

func referenceVarianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum := 0
	sse := 0
	for y := range 16 {
		for x := range 16 {
			diff := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}

func referenceFirstPass16(src []byte, srcStride int, dst *[17 * 16]uint16, height int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := range height {
		for x := range 16 {
			v := int(src[y*srcStride+x])*f0 + int(src[y*srcStride+x+1])*f1
			dst[y*16+x] = uint16((v + round) >> shift)
		}
	}
}

func referenceSecondPass16(src *[17 * 16]uint16, dst []byte, height int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := range height {
		for x := range 16 {
			v := int(src[y*16+x])*f0 + int(src[(y+1)*16+x])*f1
			dst[y*16+x] = byte((v + round) >> shift)
		}
	}
}

// TestSubpelVarianceBilinearSIMDMatchesScalar exhaustively cross-checks
// the dispatched SIMD-accelerated 16-wide bilinear / variance kernels
// against the pure-Go reference implementations defined above. On
// arm64 the dispatched paths route through the NEON assembly (see
// variance_*_arm64.s); on amd64 they route through the SSE2 assembly
// (see variance_*_amd64.s); on every other arch they route through
// the scalar fallback (which is also exercised here by comparing
// against an inlined copy).
//
// The inputs are pseudo-random to maximise coverage of the byte
// range; the seed is fixed so failures are reproducible. Each of the
// three kernels is exercised over every bilinear filter index (0-7)
// and every supported height (1-17 for the first pass, 1-16 for the
// second pass).
func TestSubpelVarianceBilinearSIMDMatchesScalar(t *testing.T) {
	const seed int64 = 0xc0de1571
	rng := rand.New(rand.NewSource(seed))

	// Variance block parity. Random 32x32 byte buffer, varying offsets
	// and strides, 100 iterations.
	for iter := range 100 {
		src := make([]byte, 64*64)
		ref := make([]byte, 64*64)
		for i := range src {
			src[i] = byte(rng.Intn(256))
			ref[i] = byte(rng.Intn(256))
		}
		srcStride := 16 + rng.Intn(48)
		refStride := 16 + rng.Intn(48)
		// Pick offsets that leave at least 16 rows + 16 cols.
		srcOff := rng.Intn(64*64 - 16*srcStride - 16)
		refOff := rng.Intn(64*64 - 16*refStride - 16)
		gotSum, gotSSE := varianceBlock16x16(src[srcOff:], srcStride, ref[refOff:], refStride)
		wantSum, wantSSE := referenceVarianceBlock16x16(src[srcOff:], srcStride, ref[refOff:], refStride)
		if gotSum != wantSum || gotSSE != wantSSE {
			t.Fatalf("iter=%d srcStride=%d refStride=%d: varianceBlock16x16 = (%d,%d), want (%d,%d)",
				iter, srcStride, refStride, gotSum, gotSSE, wantSum, wantSSE)
		}
	}

	// First-pass bilinear parity: varying height, filter, stride.
	for f := range 8 {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 17; height++ {
			for trial := range 3 {
				const stride = 32
				src := make([]byte, stride*(height+2))
				for i := range src {
					src[i] = byte(rng.Intn(256))
				}
				var got, want [17 * 16]uint16
				varFilterBlock2DBilinearFirstPass16(src, stride, &got, height, filter)
				referenceFirstPass16(src, stride, &want, height, filter)
				for i := 0; i < height*16; i++ {
					if got[i] != want[i] {
						t.Fatalf("filter=%d height=%d trial=%d off=%d: firstPass16 = %d, want %d",
							f, height, trial, i, got[i], want[i])
					}
				}
			}
		}
	}

	// Second-pass bilinear parity: varying height, filter.
	for f := range 8 {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 16; height++ {
			for trial := range 3 {
				var src [17 * 16]uint16
				// Second-pass inputs are first-pass outputs, range [0, 255].
				for i := range src {
					src[i] = uint16(rng.Intn(256))
				}
				got := make([]byte, 16*16)
				want := make([]byte, 16*16)
				varFilterBlock2DBilinearSecondPass16(&src, got, height, filter)
				referenceSecondPass16(&src, want, height, filter)
				for i := 0; i < height*16; i++ {
					if got[i] != want[i] {
						t.Fatalf("filter=%d height=%d trial=%d off=%d: secondPass16 = %d, want %d",
							f, height, trial, i, got[i], want[i])
					}
				}
			}
		}
	}
}

// referenceVarianceBlockSized is a size-agnostic scalar reference for
// the SIMD-accelerated varianceBlockSized kernels (widths 16/8/4 with
// arbitrary heights). Same math as referenceVarianceBlock16x16 but
// parameterised by (w, h).
func referenceVarianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, w, h int) (int, int) {
	sum := 0
	sse := 0
	for y := range h {
		for x := range w {
			diff := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}

func referenceFirstPassSized(src []byte, srcStride int, dst *[17 * 16]uint16, w, h int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := range h {
		for x := range w {
			v := int(src[y*srcStride+x])*f0 + int(src[y*srcStride+x+1])*f1
			dst[y*w+x] = uint16((v + round) >> shift)
		}
	}
}

func referenceSecondPassSized(src *[17 * 16]uint16, dst []byte, w, h int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := range h {
		for x := range w {
			v := int(src[y*w+x])*f0 + int(src[(y+1)*w+x])*f1
			dst[y*w+x] = byte((v + round) >> shift)
		}
	}
}

// TestVarianceBlockSizedSIMDMatchesScalar covers the non-16x16 SIMD
// variance kernels (width 16 height N, width 8, width 4) used by the
// VP8 inter-mode picker.
func TestVarianceBlockSizedSIMDMatchesScalar(t *testing.T) {
	const seed int64 = 0xb10c5172
	rng := rand.New(rand.NewSource(seed))

	sizes := [][2]int{
		{16, 16}, {16, 8},
		{8, 16}, {8, 8}, {8, 4},
		{4, 8}, {4, 4},
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		for iter := range 50 {
			src := make([]byte, 64*64)
			ref := make([]byte, 64*64)
			for i := range src {
				src[i] = byte(rng.Intn(256))
				ref[i] = byte(rng.Intn(256))
			}
			srcStride := w + rng.Intn(48)
			refStride := w + rng.Intn(48)
			srcOff := rng.Intn(64*64 - h*srcStride - w)
			refOff := rng.Intn(64*64 - h*refStride - w)
			gotSum, gotSSE := varianceBlockSized(src[srcOff:], srcStride, ref[refOff:], refStride, w, h)
			wantSum, wantSSE := referenceVarianceBlockSized(src[srcOff:], srcStride, ref[refOff:], refStride, w, h)
			if gotSum != wantSum || gotSSE != wantSSE {
				t.Fatalf("size=%dx%d iter=%d: varianceBlockSized = (%d,%d), want (%d,%d)",
					w, h, iter, gotSum, gotSSE, wantSum, wantSSE)
			}
		}
	}
}

// TestSubpelVarianceBilinearSizedSIMDMatchesScalar covers the SIMD
// first-pass / second-pass kernels at widths 8 and 4 across every
// bilinear filter index and every supported height.
func TestSubpelVarianceBilinearSizedSIMDMatchesScalar(t *testing.T) {
	const seed int64 = 0x517e85ed
	rng := rand.New(rand.NewSource(seed))

	for _, w := range []int{8, 4} {
		// First-pass parity.
		for f := range 8 {
			filter := tables.BilinearFilters[f]
			for height := 1; height <= 17; height++ {
				for trial := range 3 {
					stride := w + 16
					src := make([]byte, stride*(height+2))
					for i := range src {
						src[i] = byte(rng.Intn(256))
					}
					var got, want [17 * 16]uint16
					switch w {
					case 8:
						varFilterBlock2DBilinearFirstPass8(src, stride, &got, height, filter)
					case 4:
						varFilterBlock2DBilinearFirstPass4(src, stride, &got, height, filter)
					}
					referenceFirstPassSized(src, stride, &want, w, height, filter)
					for i := 0; i < height*w; i++ {
						if got[i] != want[i] {
							t.Fatalf("w=%d filter=%d height=%d trial=%d off=%d: firstPass = %d, want %d",
								w, f, height, trial, i, got[i], want[i])
						}
					}
				}
			}
		}

		// Second-pass parity.
		for f := range 8 {
			filter := tables.BilinearFilters[f]
			for height := 1; height <= 16; height++ {
				for trial := range 3 {
					var src [17 * 16]uint16
					for i := range src {
						src[i] = uint16(rng.Intn(256))
					}
					got := make([]byte, 16*16)
					want := make([]byte, 16*16)
					switch w {
					case 8:
						varFilterBlock2DBilinearSecondPass8(&src, got, height, filter)
					case 4:
						varFilterBlock2DBilinearSecondPass4(&src, got, height, filter)
					}
					referenceSecondPassSized(&src, want, w, height, filter)
					for i := 0; i < height*w; i++ {
						if got[i] != want[i] {
							t.Fatalf("w=%d filter=%d height=%d trial=%d off=%d: secondPass = %d, want %d",
								w, f, height, trial, i, got[i], want[i])
						}
					}
				}
			}
		}
	}
}

// TestSubpelVarianceSizedFullPipelineMatchesScalar drives the public
// SubpelVariance{16x8,8x16,8x8,8x4,4x8,4x4} entry points and verifies
// equivalence with a fully scalar reference pipeline for every
// (xOffset, yOffset) pair.
func TestSubpelVarianceSizedFullPipelineMatchesScalar(t *testing.T) {
	const seed int64 = 0xed5e2517
	rng := rand.New(rand.NewSource(seed))

	const stride = 64
	const h = 64
	src := make([]byte, stride*h)
	ref := make([]byte, stride*h)
	for i := range src {
		src[i] = byte(rng.Intn(256))
		ref[i] = byte(rng.Intn(256))
	}

	tests := []struct {
		name string
		w, h int
		fn   func([]byte, int, int, int, []byte, int) (int, int)
	}{
		{"16x8", 16, 8, SubpelVariance16x8},
		{"8x16", 8, 16, SubpelVariance8x16},
		{"8x8", 8, 8, SubpelVariance8x8},
		{"8x4", 8, 4, SubpelVariance8x4},
		{"4x8", 4, 8, SubpelVariance4x8},
		{"4x4", 4, 4, SubpelVariance4x4},
	}
	for _, tc := range tests {
		for xOff := range 8 {
			for yOff := range 8 {
				var firstPass [17 * 16]uint16
				filtered := make([]byte, tc.w*tc.h)
				referenceFirstPassSized(src, stride, &firstPass, tc.w, tc.h+1, tables.BilinearFilters[xOff])
				referenceSecondPassSized(&firstPass, filtered, tc.w, tc.h, tables.BilinearFilters[yOff])
				wantSum, wantSSE := referenceVarianceBlockSized(filtered, tc.w, ref, stride, tc.w, tc.h)
				wantVar := wantSSE - wantSum*wantSum/(tc.w*tc.h)

				gotVar, gotSSE := tc.fn(src, stride, xOff, yOff, ref, stride)
				if gotVar != wantVar || gotSSE != wantSSE {
					t.Fatalf("%s xOff=%d yOff=%d: full pipeline = (%d,%d), want (%d,%d)",
						tc.name, xOff, yOff, gotVar, gotSSE, wantVar, wantSSE)
				}
			}
		}
	}
}

// TestSubpelVarianceFullPipelineSIMDMatchesScalar cross-checks the
// full subpel-variance pipeline (first-pass bilinear -> second-pass
// bilinear -> variance block) for every (xOffset, yOffset) pair. This
// exercises the SIMD kernels in concert at the full SubpelVariance16x16
// entry point, since callers feed first-pass output into the
// second-pass which then feeds into the variance block.
func TestSubpelVarianceFullPipelineSIMDMatchesScalar(t *testing.T) {
	const seed int64 = 0x5ee45ed
	rng := rand.New(rand.NewSource(seed))

	const w = 64
	const h = 64
	src := make([]byte, w*h)
	ref := make([]byte, w*h)
	for i := range src {
		src[i] = byte(rng.Intn(256))
		ref[i] = byte(rng.Intn(256))
	}

	for xOff := range 8 {
		for yOff := range 8 {
			// Reference: pure scalar pipeline.
			var firstPass [17 * 16]uint16
			var filtered [16 * 16]byte
			referenceFirstPass16(src, w, &firstPass, 17, tables.BilinearFilters[xOff])
			referenceSecondPass16(&firstPass, filtered[:], 16, tables.BilinearFilters[yOff])
			wantSum, wantSSE := referenceVarianceBlock16x16(filtered[:], 16, ref, w)
			wantVar := wantSSE - wantSum*wantSum/(16*16)

			gotVar, gotSSE := SubpelVariance16x16(src, w, xOff, yOff, ref, w)
			if gotVar != wantVar || gotSSE != wantSSE {
				t.Fatalf("xOff=%d yOff=%d: SubpelVariance16x16 = (%d,%d), want (%d,%d)",
					xOff, yOff, gotVar, gotSSE, wantVar, wantSSE)
			}
			gotPtrVar, gotPtrSSE := SubpelVariance16x16PtrFast(&src[0], w, xOff, yOff, &ref[0], w)
			if gotPtrVar != wantVar || gotPtrSSE != wantSSE {
				t.Fatalf("xOff=%d yOff=%d: SubpelVariance16x16PtrFast = (%d,%d), want (%d,%d)",
					xOff, yOff, gotPtrVar, gotPtrSSE, wantVar, wantSSE)
			}
		}
	}
}
