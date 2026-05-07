package dsp

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Benchmarks for the 16x16 second-pass bilinear hot path. The
// generic version walks an arbitrary stride/pixelStep pair so the
// compiler keeps the indexing in registers instead of constant-
// folding it; the specialised variant pins both to 16 and uses
// fixed-size array views to drop per-pixel bounds checks. Per the
// cmd/govpx-bench cpuprofile baseline this leaf is the single
// largest at 11.1% flat self-time.

func benchSrc16x16() ([]byte, []byte) {
	const stride = 64
	src := make([]byte, stride*32)
	ref := make([]byte, stride*32)
	for i := range src {
		src[i] = byte(7 + i*3)
		ref[i] = byte(11 + i*5)
	}
	return src, ref
}

func BenchmarkBilinearSecondPass16Specialised(b *testing.B) {
	var src [17 * 16]uint16
	for i := range src {
		src[i] = uint16(i)
	}
	dst := make([]byte, 16*16)
	filter := tables.BilinearFilters[5]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearSecondPass16(&src, dst, 16, filter)
	}
}

func BenchmarkBilinearSecondPass16Generic(b *testing.B) {
	var src [17 * 16]uint16
	for i := range src {
		src[i] = uint16(i)
	}
	dst := make([]byte, 16*16)
	filter := tables.BilinearFilters[5]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bilinearSecondPassGeneric(&src, dst, 16, 16, 16, 16, filter)
	}
}

func BenchmarkSubpelVariance16x16Dispatch(b *testing.B) {
	src, ref := benchSrc16x16()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = SubpelVariance16x16(src, 64, 3, 5, ref, 64)
	}
}

// bilinearSecondPassGeneric mirrors the original (pre-specialisation)
// generic loop verbatim so the benchmark reports a like-for-like
// comparison even after varFilterBlock2DBilinearSecondPass starts
// dispatching to the 16-wide specialisation.
func bilinearSecondPassGeneric(src *[17 * 16]uint16, dst []byte, srcStride int, pixelStep int, height int, width int, filter [2]int16) {
	for y := 0; y < height; y++ {
		srcRow := y * srcStride
		dstRow := y * width
		for x := 0; x < width; x++ {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+x+pixelStep])*int(filter[1])
			dst[dstRow+x] = byte((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

// TestBilinearSecondPass16ParityVsGeneric proves bit-exact equivalence
// between the 16-wide specialisation and the generic scalar path at
// every (filter, height) combination the encoder uses.
func TestBilinearSecondPass16ParityVsGeneric(t *testing.T) {
	var src [17 * 16]uint16
	for i := range src {
		src[i] = uint16((i * 7) & 0xff)
	}
	specDst := make([]byte, 16*16)
	genDst := make([]byte, 16*16)

	for f := 0; f < 8; f++ {
		filter := tables.BilinearFilters[f]
		for height := 1; height <= 16; height++ {
			for i := range specDst {
				specDst[i] = 0
				genDst[i] = 0
			}
			varFilterBlock2DBilinearSecondPass16(&src, specDst, height, filter)
			bilinearSecondPassGeneric(&src, genDst, 16, 16, height, 16, filter)
			for i, want := range genDst {
				if specDst[i] != want {
					t.Fatalf("filter=%d height=%d off=%d: spec=%d generic=%d", f, height, i, specDst[i], want)
				}
			}
		}
	}
}
