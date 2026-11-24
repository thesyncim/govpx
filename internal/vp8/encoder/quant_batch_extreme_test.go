package encoder

import (
	"reflect"
	"testing"
)

func TestQuantBatchExtremesScalar(t *testing.T) {
	seed := uint32(0x1931)
	next := func() int16 { seed = seed*1664525 + 1013904223; return int16(seed >> 16) }
	for _, count := range []int{0, 1, 2, 7, 8, 16, 25} {
		for trial := range 80 {
			var q BlockQuant
			for i := range 16 {
				q.Round[i] = next()
				q.QuantFast[i] = next()
				q.Dequant[i] = next()
				if trial%2 == 0 {
					q.Round[i] &= 32767
					q.QuantFast[i] &= 32767
				}
			}
			c := make([]int16, count*16)
			for i := range c {
				c[i] = next()
			}
			if len(c) > 0 {
				c[0] = -32768
			}
			gotQ, gotD, wantQ, wantD := make([]int16, len(c)), make([]int16, len(c)), make([]int16, len(c)), make([]int16, len(c))
			gotE, wantE := make([]uint8, count), make([]uint8, count)
			FastQuantizeBlockBatch(c, &q, gotQ, gotD, gotE, count)
			fastQuantizeBlockBatchScalar(c, &q, wantQ, wantD, wantE, count)
			if !reflect.DeepEqual(gotQ, wantQ) || !reflect.DeepEqual(gotD, wantD) || !reflect.DeepEqual(gotE, wantE) {
				t.Fatalf("count%d trial%d", count, trial)
			}
		}
	}
}
