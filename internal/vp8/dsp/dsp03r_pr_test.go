package dsp

import (
	"fmt"
	"slices"
	"testing"
)

var dsp03rPRSink int

func dsp03rPRData(n int) []byte {
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
func TestDSP03RPRZeroDC(t *testing.T) {
	for _, c := range []int16{-32768, -10, -5, -4, -1, 0, 1, 3, 4, 10, 32767} {
		for _, ds := range []int{4, 24} {
			for _, same := range []bool{false, true} {
				s := dsp03rPRData(4 * ds)
				a, z := slices.Clone(s), slices.Clone(s)
				p := s
				if same {
					p = a
				}
				DCOnlyIDCT4x4Add(c, p, ds, a, ds)
				p = s
				if same {
					p = z
				}
				dcOnlyIDCT4x4AddScalar(c, p, ds, z, ds)
				if !slices.Equal(a, z) {
					t.Fatalf("c%d ds%d", c, ds)
				}
			}
		}
	}
}
func BenchmarkDSP03RPRZeroDC(b *testing.B) {
	for _, c := range []int16{0, 8} {
		for _, same := range []bool{true, false} {
			b.Run(fmt.Sprintf("dc%d/inplace%v", c, same), func(b *testing.B) {
				s, d := dsp03rPRData(4*24), dsp03rPRData(4*24)
				if same {
					s = d
				}
				b.ReportAllocs()
				for b.Loop() {
					DCOnlyIDCT4x4Add(c, s, 24, d, 24)
				}
				dsp03rPRSink = int(d[0])
			})
		}
	}
}
