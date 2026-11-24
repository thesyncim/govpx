//go:build arm64 && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"math/rand/v2"
	"testing"
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
	b.ResetTimer()
	var sum int32
	var sse uint32
	for i := 0; i < b.N; i++ {
		if dot {
			su, se := pixel.Stats(src[off*stride+off:], stride, ref[off*stride+off:], stride, w, h)
			sum, sse = int32(su), se
		} else {
			sse0, sum0 := computeVariance(src, off*stride+off, stride, ref, off*stride+off, stride, w, h)
			sum, sse = int32(sum0), sse0
		}
	}
	varianceKernelBenchSink = varianceStatsFromSumSSE(sum, sse, w, h)
}

func BenchmarkVP9VarianceKernels16x16Scalar(b *testing.B) {
	benchmarkVarianceKernel(b, 16, 16, false)
}

func BenchmarkVP9VarianceKernels16x16GoSIMD(b *testing.B) {
	benchmarkVarianceKernel(b, 16, 16, true)
}

func BenchmarkVP9VarianceKernels32x16Scalar(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 16, false)
}

func BenchmarkVP9VarianceKernels32x16GoSIMD(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 16, true)
}

func BenchmarkVP9VarianceKernels32x32Scalar(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 32, false)
}

func BenchmarkVP9VarianceKernels32x32GoSIMD(b *testing.B) {
	benchmarkVarianceKernel(b, 32, 32, true)
}

func BenchmarkVP9VarianceKernels64x32Scalar(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 32, false)
}

func BenchmarkVP9VarianceKernels64x32GoSIMD(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 32, true)
}

func BenchmarkVP9VarianceKernels64x64Scalar(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 64, false)
}

func BenchmarkVP9VarianceKernels64x64GoSIMD(b *testing.B) {
	benchmarkVarianceKernel(b, 64, 64, true)
}
