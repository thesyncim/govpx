package dsp

import (
	"fmt"
	"slices"
	"testing"
)

var dsp02PRSink int

func dsp02PRData(n int) []byte {
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
func dsp02PRResidualRef(d []byte, ds int, r *[16]int16) {
	for y := 0; y < 4; y++ {
		row := (*[4]byte)(d[y*ds : y*ds+4])
		for x := 0; x < 4; x++ {
			row[x] = ClipPixel(int(row[x]) + int(r[y*4+x]))
		}
	}
}
func TestDSP02PRResidual(t *testing.T) {
	for k := 0; k < 1000; k++ {
		var r [16]int16
		for i := range r {
			r[i] = int16(k*1517 + i*3791)
		}
		for _, ds := range []int{4, 7, 24} {
			a, c := dsp02PRData(4*ds), dsp02PRData(4*ds)
			AddResidual4x4(a, ds, &r)
			dsp02PRResidualRef(c, ds, &r)
			if !slices.Equal(a, c) {
				t.Fatalf("k%d ds%d", k, ds)
			}
		}
	}
}
func BenchmarkDSP02PRResidual(b *testing.B) {
	for _, ds := range []int{4, 24} {
		for _, dense := range []bool{false, true} {
			b.Run(fmt.Sprintf("stride%d/dense%v", ds, dense), func(b *testing.B) {
				d := dsp02PRData(4 * ds)
				var r [16]int16
				for i := range r {
					r[i] = int16(i - 7)
					if dense {
						r[i] = int16(i * 4000)
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					AddResidual4x4(d, ds, &r)
				}
				dsp02PRSink = int(d[0])
			})
		}
	}
}
