package dsp

import (
	"slices"
	"testing"
)

var dsp06PRSink int

func dsp06PRData(n int) []byte {
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
func TestDSP06PRSparseDequant(t *testing.T) {
	for _, dc := range []int16{-32768, -32765, -1000, -4, 0, 3, 1000, 32767} {
		for _, dq := range []int16{-32768, -17, 0, 1, 17, 32767} {
			for _, dense := range []bool{false, true} {
				var a, z, q [16]int16
				a[0] = dc
				for i := range q {
					q[i] = dq
					if dense {
						a[i] = int16(i * 1517)
					}
				}
				z = a
				x, y := dsp06PRData(4*24), dsp06PRData(4*24)
				DequantIDCT4x4Add(&a, &q, x, 24)
				for i := range z {
					z[i] *= q[i]
				}
				IDCT4x4Add(&z, y, 24, y, 24)
				clear(z[:])
				if a != z || !slices.Equal(x, y) {
					t.Fatalf("dc%d dq%d dense%v", dc, dq, dense)
				}
			}
		}
	}
}
func BenchmarkDSP06PRSparseDequant(b *testing.B) {
	for _, kind := range []string{"zero", "dc", "dense"} {
		b.Run(kind, func(b *testing.B) {
			var in, q, seed [16]int16
			for i := range q {
				q[i] = 117
				if kind == "dense" {
					seed[i] = int16(i*71 - 300)
				}
			}
			if kind == "dc" {
				seed[0] = 17
			}
			d := dsp06PRData(4 * 24)
			b.ReportAllocs()
			for b.Loop() {
				in = seed
				DequantIDCT4x4Add(&in, &q, d, 24)
			}
			dsp06PRSink = int(d[0])
		})
	}
}
