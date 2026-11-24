package govpx

import (
	"math/rand"
	"testing"
)

func TestApplyTemporalFilterDispatchMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0xa4f17e2))
	for _, blockSize := range []int{8, 16} {
		for strength := range 7 {
			for iter := range 80 {
				srcStride := blockSize + rng.Intn(5)
				predStride := blockSize + rng.Intn(5)
				n := blockSize * blockSize
				src := make([]byte, srcStride*blockSize)
				pred := make([]byte, predStride*blockSize)
				for i := range src {
					src[i] = byte(rng.Intn(256))
				}
				for i := range pred {
					pred[i] = byte(rng.Intn(256))
				}
				accDispatch := make([]uint32, n)
				accScalar := make([]uint32, n)
				countDispatch := make([]uint32, n)
				countScalar := make([]uint32, n)
				for i := range n {
					acc := uint32(rng.Intn(4096))
					cnt := uint32(rng.Intn(64))
					accDispatch[i] = acc
					accScalar[i] = acc
					countDispatch[i] = cnt
					countScalar[i] = cnt
				}
				filterWeight := 1 + rng.Intn(2)

				applyTemporalFilter(src, srcStride, pred, predStride, strength, filterWeight, accDispatch, countDispatch)
				applyTemporalFilterScalar(src, srcStride, pred, predStride, blockSize, strength, filterWeight, accScalar, countScalar)

				for i := range n {
					if accDispatch[i] != accScalar[i] || countDispatch[i] != countScalar[i] {
						t.Fatalf("block=%d strength=%d iter=%d index=%d acc=%d want %d count=%d want %d",
							blockSize, strength, iter, i,
							accDispatch[i], accScalar[i],
							countDispatch[i], countScalar[i])
					}
				}
			}
		}
	}
}

func BenchmarkApplyTemporalFilter16Dispatch(b *testing.B) {
	src, pred := benchmarkTemporalFilterBuffers(16)
	acc := make([]uint32, 16*16)
	count := make([]uint32, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		applyTemporalFilter(src, 16, pred, 16, 3, 2, acc, count)
	}
}

func BenchmarkApplyTemporalFilter16Scalar(b *testing.B) {
	src, pred := benchmarkTemporalFilterBuffers(16)
	acc := make([]uint32, 16*16)
	count := make([]uint32, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		applyTemporalFilterScalar(src, 16, pred, 16, 16, 3, 2, acc, count)
	}
}

func BenchmarkApplyTemporalFilter8Dispatch(b *testing.B) {
	src, pred := benchmarkTemporalFilterBuffers(8)
	acc := make([]uint32, 8*8)
	count := make([]uint32, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		applyTemporalFilter(src, 8, pred, 8, 3, 2, acc, count)
	}
}

func BenchmarkApplyTemporalFilter8Scalar(b *testing.B) {
	src, pred := benchmarkTemporalFilterBuffers(8)
	acc := make([]uint32, 8*8)
	count := make([]uint32, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		applyTemporalFilterScalar(src, 8, pred, 8, 8, 3, 2, acc, count)
	}
}

func benchmarkTemporalFilterBuffers(blockSize int) ([]byte, []byte) {
	src := make([]byte, blockSize*blockSize)
	pred := make([]byte, blockSize*blockSize)
	for i := range src {
		src[i] = byte(96 + (i & 31))
		pred[i] = byte(94 + (i & 31))
	}
	return src, pred
}
