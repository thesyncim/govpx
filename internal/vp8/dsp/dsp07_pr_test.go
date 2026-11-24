package dsp

import (
	"fmt"
	"testing"
)

var dsp07PRSink int

func dsp07PRData(n int) []byte {
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
func dsp07PRSplitRef(src []byte, ss, x, y int, ref []byte, rs, w, h int) (int, int) {
	tmp := make([]byte, (h+1)*w)
	f := make([]byte, h*w)
	for r := 0; r < h+1; r++ {
		for c := 0; c < w; c++ {
			tmp[r*w+c] = byte((int(src[r*ss+c])*(128-x*16) + int(src[r*ss+c+1])*x*16 + 64) >> 7)
		}
	}
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			f[r*w+c] = byte((int(tmp[r*w+c])*(128-y*16) + int(tmp[(r+1)*w+c])*y*16 + 64) >> 7)
		}
	}
	sum, sse := varianceBlockGeneric(f, w, ref, rs, w, h)
	return sse - sum*sum/(w*h), sse
}
func dsp07PRSplit(s []byte, ss, x, y int, r []byte, rs, w, h int) (int, int) {
	if w == 4 && h == 4 {
		return SubpelVariance4x4(s, ss, x, y, r, rs)
	}
	if w == 4 {
		return SubpelVariance4x8(s, ss, x, y, r, rs)
	}
	if h == 4 {
		return SubpelVariance8x4(s, ss, x, y, r, rs)
	}
	if h == 8 {
		return SubpelVariance8x8(s, ss, x, y, r, rs)
	}
	return SubpelVariance8x16(s, ss, x, y, r, rs)
}
func TestDSP07PRSplit(t *testing.T) {
	for _, wh := range [][2]int{{4, 4}, {4, 8}, {8, 4}, {8, 8}, {8, 16}} {
		w, h := wh[0], wh[1]
		s, r := dsp07PRData((h+1)*24), dsp07PRData(h*32)
		for x := 0; x < 8; x++ {
			for y := 0; y < 8; y++ {
				a, b := dsp07PRSplit(s, 24, x, y, r, 32, w, h)
				c, d := dsp07PRSplitRef(s, 24, x, y, r, 32, w, h)
				if a != c || b != d {
					t.Fatalf("%dx%d %d,%d", w, h, x, y)
				}
			}
		}
	}
}
func BenchmarkDSP07PRSplit(b *testing.B) {
	for _, wh := range [][2]int{{4, 4}, {4, 8}, {8, 4}, {8, 8}, {8, 16}} {
		for _, xy := range [][2]int{{4, 0}, {0, 4}, {3, 5}} {
			b.Run(fmt.Sprintf("%dx%d/%d_%d", wh[0], wh[1], xy[0], xy[1]), func(b *testing.B) {
				s, r := dsp07PRData(17*24), dsp07PRData(16*32)
				a, z := 0, 0
				b.ReportAllocs()
				for b.Loop() {
					a, z = dsp07PRSplit(s, 24, xy[0], xy[1], r, 32, wh[0], wh[1])
				}
				dsp07PRSink = a + z
			})
		}
	}
}
