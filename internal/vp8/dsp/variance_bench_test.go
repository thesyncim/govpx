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

// BenchmarkBilinearFirstPass16Specialised measures the dispatched
// first-pass path - on arm64 this routes through the NEON assembly,
// on every other platform it routes through the scalar fallback.
func BenchmarkBilinearFirstPass16Specialised(b *testing.B) {
	src, _ := benchSrc16x16()
	var dst [17 * 16]uint16
	filter := tables.BilinearFilters[3]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearFirstPass16(src, 64, &dst, 17, filter)
	}
}

// BenchmarkBilinearFirstPass16Generic exercises the generic
// arbitrary-stride loop in variance.go for like-for-like comparison.
func BenchmarkBilinearFirstPass16Generic(b *testing.B) {
	src, _ := benchSrc16x16()
	var dst [17 * 16]uint16
	filter := tables.BilinearFilters[3]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bilinearFirstPassGeneric(src, 64, &dst, 16, 17, filter)
	}
}

func bilinearFirstPassGeneric(src []byte, srcStride int, dst *[17 * 16]uint16, width int, height int, filter [2]int16) {
	for y := range height {
		srcRow := y * srcStride
		dstRow := y * width
		for x := range width {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+x+1])*int(filter[1])
			dst[dstRow+x] = uint16((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

// BenchmarkBilinearSecondPass16Specialised measures the dispatched
// path - on arm64 this routes through the NEON assembly, on every
// other platform it routes through the scalar specialisation.
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

// BenchmarkBilinearSecondPass16Scalar always runs the scalar
// specialisation, even on arm64. Lets us compare the NEON win over
// the scalar baseline without the build-tag dance.
func BenchmarkBilinearSecondPass16Scalar(b *testing.B) {
	var src [17 * 16]uint16
	for i := range src {
		src[i] = uint16(i)
	}
	dst := make([]byte, 16*16)
	filter := tables.BilinearFilters[5]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		varFilterBlock2DBilinearSecondPass16Scalar(&src, dst, 16, filter)
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

func BenchmarkSubpelVariance16x16PtrFast(b *testing.B) {
	src, ref := benchSrc16x16()
	srcPtr := &src[0]
	refPtr := &ref[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = SubpelVariance16x16PtrFast(srcPtr, 64, 3, 5, refPtr, 64)
	}
}

func BenchmarkSubpelVariance16x16HorizontalOnly(b *testing.B) {
	src, ref := benchSrc16x16()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = SubpelVariance16x16(src, 64, 5, 0, ref, 64)
	}
}

func BenchmarkSubpelVariance16x16HorizontalOnlyPtrFast(b *testing.B) {
	src, ref := benchSrc16x16()
	srcPtr := &src[0]
	refPtr := &ref[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = SubpelVariance16x16PtrFast(srcPtr, 64, 5, 0, refPtr, 64)
	}
}

func BenchmarkSubpelVariance16x16VerticalOnly(b *testing.B) {
	src, ref := benchSrc16x16()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = SubpelVariance16x16(src, 64, 0, 5, ref, 64)
	}
}

func BenchmarkSubpelVariance16x16VerticalOnlyPtrFast(b *testing.B) {
	src, ref := benchSrc16x16()
	srcPtr := &src[0]
	refPtr := &ref[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = SubpelVariance16x16PtrFast(srcPtr, 64, 0, 5, refPtr, 64)
	}
}

// bilinearSecondPassGeneric mirrors the original (pre-specialisation)
// generic loop verbatim so the benchmark reports a like-for-like
// comparison even after varFilterBlock2DBilinearSecondPass starts
// dispatching to the 16-wide specialisation.
func bilinearSecondPassGeneric(src *[17 * 16]uint16, dst []byte, srcStride int, pixelStep int, height int, width int, filter [2]int16) {
	for y := range height {
		srcRow := y * srcStride
		dstRow := y * width
		for x := range width {
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

	for f := range 8 {
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

func BenchmarkVarianceBlock16x16NEON(b *testing.B) {
	src, ref := benchSrc16x16()
	for i := 0; i < b.N; i++ {
		_, _ = varianceBlock16x16(src, 64, ref, 64)
	}
}

// BenchmarkVarianceBlock16x16Generic measures the original scalar
// loop without the 16x16 dispatch, for like-for-like comparison
// against the NEON path.
func BenchmarkVarianceBlock16x16Generic(b *testing.B) {
	src, ref := benchSrc16x16()
	for i := 0; i < b.N; i++ {
		_, _ = varianceBlockScalarReference(src, 64, ref, 64)
	}
}

func varianceBlockScalarReference(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum := 0
	sse := 0
	for y := range 16 {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := range 16 {
			diff := int(srcRow[x]) - int(refRow[x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}
