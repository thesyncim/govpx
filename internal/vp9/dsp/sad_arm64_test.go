//go:build arm64 && !purego

package dsp

import (
	"math/rand/v2"
	"testing"
	"unsafe"
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
	srcPtr := unsafe.SliceData(src[off*stride+off:])
	refPtr := unsafe.SliceData(ref[off*stride+off:])
	groups := width / 32
	b.ResetTimer()
	var out uint32
	for i := 0; i < b.N; i++ {
		if dot {
			out = sadDotWideNEON(srcPtr, stride, refPtr, stride, rows, groups)
		} else if width == 64 {
			out = sad64xNNEON(srcPtr, stride, refPtr, stride, rows)
		} else {
			out = sad32xNNEON(srcPtr, stride, refPtr, stride, rows)
		}
	}
	sadKernelBenchSink = out
}

func BenchmarkVP9SadWideKernels32x16Base(b *testing.B) {
	benchmarkSadWideKernel(b, 16, 32, false)
}

func BenchmarkVP9SadWideKernels32x16Dot(b *testing.B) {
	benchmarkSadWideKernel(b, 16, 32, true)
}

func BenchmarkVP9SadWideKernels32x32Base(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 32, false)
}

func BenchmarkVP9SadWideKernels32x32Dot(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 32, true)
}

func BenchmarkVP9SadWideKernels32x64Base(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 32, false)
}

func BenchmarkVP9SadWideKernels32x64Dot(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 32, true)
}

func BenchmarkVP9SadWideKernels64x32Base(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 64, false)
}

func BenchmarkVP9SadWideKernels64x32Dot(b *testing.B) {
	benchmarkSadWideKernel(b, 32, 64, true)
}

func BenchmarkVP9SadWideKernels64x64Base(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 64, false)
}

func BenchmarkVP9SadWideKernels64x64Dot(b *testing.B) {
	benchmarkSadWideKernel(b, 64, 64, true)
}
