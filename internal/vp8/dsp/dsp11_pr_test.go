package dsp

import (
	"fmt"
	"testing"
)

var dsp11PRSink int

func dsp11PRData(n int) []byte {
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
func TestDSP11PRSAD4(t *testing.T) {
	for _, ss := range []int{0, 4, 24} {
		for _, rs := range []int{0, 4, 31} {
			s, r := dsp11PRData(4*24), dsp11PRData(4*31)
			for k := 0; k < 100; k++ {
				for i := range s {
					s[i] += byte(k * 11)
				}
				for i := range r {
					r[i] += byte(k * 31)
				}
				x, y := SAD4x4(s, ss, r, rs), sadBlockScalar(s, ss, r, rs, 4, 4)
				if x != y {
					t.Fatalf("k%d", k)
				}
			}
		}
	}
}
func BenchmarkDSP11PRSAD4(b *testing.B) {
	for _, stride := range []int{4, 24} {
		b.Run(fmt.Sprintf("stride%d", stride), func(b *testing.B) {
			s, r := dsp11PRData(4*stride), dsp11PRData(4*stride)
			v := 0
			b.ReportAllocs()
			for b.Loop() {
				v = SAD4x4(s, stride, r, stride)
			}
			dsp11PRSink = v
		})
	}
}
