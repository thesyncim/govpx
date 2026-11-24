//go:build arm64 && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"math/rand/v2"
	"testing"
)

var sadKernelBenchSink uint32

func benchmarkSadWideKernel(b *testing.B, rows, width int, dot bool) {
	r := rand.New(rand.NewPCG(0x7a31, 0x52d9))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(64+off+8))
	ref := make([]uint8, stride*(64+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	b.ResetTimer()
	var out uint32
	for i := 0; i < b.N; i++ {
		if dot {
			out = uint32(pixel.SAD(src[off*stride+off:], stride, ref[off*stride+off:], stride, width, rows))
		} else {
			out = uint32(sad(src, off*stride+off, stride, ref, off*stride+off, stride, width, rows))
		}
	}
	sadKernelBenchSink = out
}

func BenchmarkVP9SadWideKernels32x16Scalar(b *testing.B) {
	benchmarkSadWideKernel(b, 16, 32, false)
}

func BenchmarkVP9SadWideKernels32x16GoSIMD(b *testing.B) {
	benchmarkSadWideKernel(b, 16, 32, true)
}

func BenchmarkVP9SadWideKernels32x32Scalar(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 32, false)
}

func BenchmarkVP9SadWideKernels32x32GoSIMD(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 32, true)
}

func BenchmarkVP9SadWideKernels32x64Scalar(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 32, false)
}

func BenchmarkVP9SadWideKernels32x64GoSIMD(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 32, true)
}

func BenchmarkVP9SadWideKernels64x32Scalar(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 64, false)
}

func BenchmarkVP9SadWideKernels64x32GoSIMD(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 64, true)
}

func BenchmarkVP9SadWideKernels64x64Scalar(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 64, false)
}

func BenchmarkVP9SadWideKernels64x64GoSIMD(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 64, true)
}
