package dsp

import (
	"fmt"
	"testing"
)

var dsp04PRSink int

func dsp04PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP04PRDequant(t *testing.T) {
	for k := 0; k < 1000; k++ {
		var q, d, a, z [16]int16
		for i := range q {
			q[i] = int16(k*1517 + i*3791)
			d[i] = int16(k*913 + i*1777)
			z[i] = q[i] * d[i]
		}
		DequantizeBlock(&q, &d, &a)
		if a != z {
			t.Fatalf("k%d", k)
		}
		DequantizeBlock(&q, &d, &q)
		if q != z {
			t.Fatalf("inplace k%d", k)
		}
	}
	for _, off := range []int{1, 7, 15} {
		var a, z [48]int16
		for i := range a {
			a[i] = int16(i * 7851)
			z[i] = a[i]
		}
		DequantizeBlock((*[16]int16)(a[:]), (*[16]int16)(a[32:]), (*[16]int16)(a[off:]))
		for i := 0; i < 16; i++ {
			z[off+i] = z[i] * z[32+i]
		}
		if a != z {
			t.Fatalf("alias%d", off)
		}
	}
}
func BenchmarkDSP04PRDequant(b *testing.B) {
	for _, same := range []bool{false, true} {
		b.Run(fmt.Sprintf("inplace%v", same), func(b *testing.B) {
			var q, d, o [16]int16
			for i := range q {
				q[i] = int16(i * 1517)
				d[i] = int16(i * 1777)
			}
			out := &o
			if same {
				out = &q
			}
			b.ReportAllocs()
			for b.Loop() {
				DequantizeBlock(&q, &d, out)
			}
			dsp04PRSink = int(out[0])
		})
	}
}
