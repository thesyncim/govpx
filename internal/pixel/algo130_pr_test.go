//go:build goexperiment.simd && (amd64 || arm64) && !purego

package pixel

import (
	"fmt"
	"simd/archsimd"
	"testing"
)

func TestAlgo130Round3Average(t *testing.T) {
	for _, w := range []int{1, 4, 8, 16, 17, 32, 64} {
		for _, h := range []int{0, 1, 8, 64} {
			for _, pad := range []int{0, 3} {
				for _, delta := range []int{-16, -9, -8, -1, 0, 1, 8, 9, 16, 4096} {
					var a, b [16384]byte
					for i := range a {
						a[i] = byte(i*57 + 11)
					}
					b = a
					ss, ds := w+pad, w+pad
					src, dst := 1024, 1024+delta
					Average(a[src:], ss, a[dst:], ds, w, h)
					algo130Round3FrozenAverage(b[src:], ss, b[dst:], ds, w, h)
					if a != b {
						t.Fatalf("w%d h%d pad%d delta%d", w, h, pad, delta)
					}
				}
			}
		}
	}
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			a := [8]byte{byte(x), byte(x), byte(x), byte(x), byte(x), byte(x), byte(x), byte(x)}
			d := [8]byte{byte(y), byte(y), byte(y), byte(y), byte(y), byte(y), byte(y), byte(y)}
			Average(a[:], 8, d[:], 8, 8, 1)
			for _, v := range d {
				if v != byte((x+y+1)>>1) {
					t.Fatal("round")
				}
			}
		}
	}
}

var algo130Round3Pixel [8192]byte

func BenchmarkAlgo130Round3Average(b *testing.B) {
	var src [8192]byte
	for i := range src {
		src[i] = byte(i*73 + 91)
		algo130Round3Pixel[i] = byte(i * 37)
	}
	for _, w := range []int{4, 8, 16, 32, 64} {
		for _, pad := range []int{0, 7} {
			b.Run(fmt.Sprintf("w%d/pad%d", w, pad), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					Average(src[:], w+pad, algo130Round3Pixel[:], w+pad, w, w)
				}
			})
		}
	}
}

func algo130Round3FrozenAverage(src []byte, ss int, dst []byte, ds, w, h int) {
	if !Enabled {
		AverageScalar(src, ss, dst, ds, w, h)
		return
	}

	one := archsimd.BroadcastInt16x8(1)
	for y := range h {
		a, b := src[y*ss:], dst[y*ds:]
		x := 0
		for ; x+8 <= w; x += 8 {
			v := widen8(load8(a[x:])).Add(widen8(load8(b[x:]))).Add(one).ShiftAllRight(1)
			storeBytes8(b[x:], packBytes(v))
		}
		for ; x < w; x++ {
			b[x] = byte((int(a[x]) + int(b[x]) + 1) >> 1)
		}
	}
}
