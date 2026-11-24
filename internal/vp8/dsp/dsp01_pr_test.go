package dsp

import (
	"fmt"
	"slices"
	"testing"
)

var dsp01PRSink int

func dsp01PRData(n int) []byte {
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
func dsp01PRBilinear(w int, s []byte, ss, x, y int, d []byte, ds int) {
	if w == 4 {
		BilinearPredict4x4(s, ss, x, y, d, ds)
	} else {
		BilinearPredict8x4(s, ss, x, y, d, ds)
	}
}
func dsp01PRBilinearRef(w int, s []byte, ss, x, y int, d []byte, ds int) {
	var tmp [5 * 8]int
	for r := 0; r < 5; r++ {
		for c := 0; c < w; c++ {
			tmp[r*w+c] = (int(s[r*ss+c])*(128-x*16) + int(s[r*ss+c+1])*x*16 + 64) >> 7
		}
	}
	for r := 0; r < 4; r++ {
		for c := 0; c < w; c++ {
			d[r*ds+c] = byte((tmp[r*w+c]*(128-y*16) + tmp[(r+1)*w+c]*y*16 + 64) >> 7)
		}
	}
}
func TestDSP01PRBilinear(t *testing.T) {
	for _, w := range []int{4, 8} {
		for _, pad := range []int{0, 11} {
			ss, ds := w+1+pad, w+pad
			for x := 0; x < 8; x++ {
				for y := 0; y < 8; y++ {
					s := dsp01PRData(5 * ss)
					a, b := dsp01PRData(4*ds), dsp01PRData(4*ds)
					dsp01PRBilinear(w, s, ss, x, y, a, ds)
					dsp01PRBilinearRef(w, s, ss, x, y, b, ds)
					if !slices.Equal(a, b) {
						t.Fatalf("w%d %d,%d", w, x, y)
					}
				}
			}
		}
		for _, off := range []int{0, 1, 9} {
			a, b := dsp01PRData(256), dsp01PRData(256)
			dsp01PRBilinear(w, a, 24, 3, 5, a[off:], 16)
			dsp01PRBilinearRef(w, b, 24, 3, 5, b[off:], 16)
			if !slices.Equal(a, b) {
				t.Fatalf("alias%d", off)
			}
		}
	}
}
func BenchmarkDSP01PRBilinear(b *testing.B) {
	for _, w := range []int{4, 8} {
		for _, xy := range [][2]int{{0, 0}, {4, 0}, {0, 4}, {3, 5}} {
			b.Run(fmt.Sprintf("w%d/%d_%d", w, xy[0], xy[1]), func(b *testing.B) {
				s, d := dsp01PRData(5*24), make([]byte, 4*16)
				b.ReportAllocs()
				for b.Loop() {
					dsp01PRBilinear(w, s, 24, xy[0], xy[1], d, 16)
				}
				dsp01PRSink = int(d[7])
			})
		}
	}
}
