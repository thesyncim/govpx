package dsp

import (
	"fmt"
	"testing"
)

var dsp12PRSink int

func dsp12PRData(n int) []byte {
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
func TestDSP12PRSSE4(t *testing.T) {
	for _, h := range []int{4, 8} {
		for _, ss := range []int{0, 4, 24} {
			for _, rs := range []int{0, 4, 31} {
				s, r := dsp12PRData(h*24), dsp12PRData(h*31)
				for k := 0; k < 100; k++ {
					for i := range s {
						s[i] += byte(k * 11)
					}
					for i := range r {
						r[i] += byte(k * 31)
					}
					x := SSE4x4(s, ss, r, rs)
					if h == 8 {
						x = SSE4x8(s, ss, r, rs)
					}
					_, y := varianceBlockGeneric(s, ss, r, rs, 4, h)
					if x != y {
						t.Fatalf("h%d k%d", h, k)
					}
				}
			}
		}
	}
}
func BenchmarkDSP12PRSSE4(b *testing.B) {
	for _, h := range []int{4, 8} {
		for _, stride := range []int{4, 24} {
			b.Run(fmt.Sprintf("h%d/stride%d", h, stride), func(b *testing.B) {
				s, r := dsp12PRData(h*stride), dsp12PRData(h*stride)
				v := 0
				b.ReportAllocs()
				for b.Loop() {
					if h == 4 {
						v = SSE4x4(s, stride, r, stride)
					} else {
						v = SSE4x8(s, stride, r, stride)
					}
				}
				dsp12PRSink = v
			})
		}
	}
}
