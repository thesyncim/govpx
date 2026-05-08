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
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
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
	for y := 0; y < height; y++ {
		for x := 0; x < 16; x++ {
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
	for y := 0; y < height; y++ {
		for x := 0; x < 16; x++ {
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
	for iter := 0; iter < 100; iter++ {
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
	for f := 0; f < 8; f++ {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 17; height++ {
			for trial := 0; trial < 3; trial++ {
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
	for f := 0; f < 8; f++ {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 16; height++ {
			for trial := 0; trial < 3; trial++ {
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

	for xOff := 0; xOff < 8; xOff++ {
		for yOff := 0; yOff < 8; yOff++ {
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
		}
	}
}
