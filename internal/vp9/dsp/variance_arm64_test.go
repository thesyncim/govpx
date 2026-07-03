//go:build arm64 && !purego

package dsp

import (
	"math/rand/v2"
	"testing"
	"unsafe"
)

var varianceKernelBenchSink VarianceStats

func benchmarkVarianceKernel(b *testing.B, w, h int, dot bool) {
	r := rand.New(rand.NewPCG(0x2ac1, 0x6d5f))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(64+off+8))
	ref := make([]uint8, stride*(64+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	srcPtr := unsafe.SliceData(src[off*stride+off:])
	refPtr := unsafe.SliceData(ref[off*stride+off:])
	b.ResetTimer()
	var sum int32
	var sse uint32
	for i := 0; i < b.N; i++ {
		if dot {
			if w == 16 {
				varianceDot16xNNEON(srcPtr, stride, refPtr, stride, h, &sum, &sse)
			} else {
				varianceDotChunksNEON(srcPtr, stride, refPtr, stride, h, w/16, &sum, &sse)
			}
		} else if w == 16 {
			varianceBlock16xNNEON(srcPtr, stride, refPtr, stride, h, &sum, &sse)
		} else {
			varianceBlock16ChunksNEON(srcPtr, stride, refPtr, stride, h, w/16, &sum, &sse)
		}
	}
	varianceKernelBenchSink = varianceStatsFromSumSSE(sum, sse, w, h)
}

func BenchmarkVP9VarianceKernels16x16Base(b *testing.B) {
	benchmarkVarianceKernel(b, 16, 16, false)
}

func BenchmarkVP9VarianceKernels16x16Dot(b *testing.B) {
	benchmarkVarianceKernel(b, 16, 16, true)
}

func BenchmarkVP9VarianceKernels32x16Base(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 16, false)
}

func BenchmarkVP9VarianceKernels32x16Dot(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 16, true)
}

func BenchmarkVP9VarianceKernels32x32Base(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 32, false)
}

func BenchmarkVP9VarianceKernels32x32Dot(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 32, true)
}

func BenchmarkVP9VarianceKernels64x32Base(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 32, false)
}

func BenchmarkVP9VarianceKernels64x32Dot(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 32, true)
}

func BenchmarkVP9VarianceKernels64x64Base(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 64, false)
}

func BenchmarkVP9VarianceKernels64x64Dot(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 64, true)
}
