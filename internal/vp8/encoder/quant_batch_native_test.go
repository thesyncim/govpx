package encoder

import (
	"fmt"
	"testing"
)

var quantBatchNativeSink uint64

// Identical public-call contract for old assembly and every Go candidate.
func BenchmarkFastQuantizeBatchNative(b *testing.B) {
	for _, count := range []int{1, 2, 8, 16, 25} {
		b.Run(fmt.Sprintf("blocks%d", count), func(b *testing.B) {
			var dequant [16]int16
			for i := range dequant {
				dequant[i] = int16(i + 10)
			}
			var quant BlockQuant
			InitFastBlockQuant(&dequant, &quant)
			c, q, dq, e := make([]int16, count*16), make([]int16, count*16), make([]int16, count*16), make([]uint8, count)
			for i := range c {
				c[i] = int16((i*17)%8191 - 4095)
				if i%7 < 3 {
					c[i] = 0
				}
			}
			wantQ, wantDQ, wantE := make([]int16, len(c)), make([]int16, len(c)), make([]uint8, count)
			for i := range count {
				wantE[i] = uint8(fastQuantizeBlockScalar((*[16]int16)(c[i*16:]), &quant, (*[16]int16)(wantQ[i*16:]), (*[16]int16)(wantDQ[i*16:])))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				FastQuantizeBlockBatch(c, &quant, q, dq, e, count)
			}
			b.StopTimer()
			for i := range q {
				if q[i] != wantQ[i] || dq[i] != wantDQ[i] {
					b.Fatalf("coefficient %d differs from scalar oracle", i)
				}
			}
			for i := range e {
				if e[i] != wantE[i] {
					b.Fatalf("EOB %d differs from scalar oracle", i)
				}
			}
			var sink uint64
			for i := range q {
				sink += uint64(uint16(q[i])) + uint64(uint16(dq[i]))
			}
			for _, v := range e {
				sink += uint64(v)
			}
			quantBatchNativeSink = sink
		})
	}
}
