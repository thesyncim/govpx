//go:build goexperiment.simd && (amd64 || arm64) && !purego

package pixel

import (
	"bytes"
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
	for x := range 256 {
		for y := range 256 {
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

var algo133Round3Pixel [8192]byte

func TestAlgo133Round3TM(t *testing.T) {
	for _, w := range []int{4, 8, 16, 32} {
		for _, ds := range []int{1, w, w + 7} {
			for _, ao := range []int{256, 512, 2048} {
				for _, lo := range []int{256, 257, 512, 2050} {
					for _, top := range []byte{0, 1, 127, 254, 255} {
						var a, b [4096]byte
						for i := range a {
							a[i] = byte(i*53 + 97)
						}
						b = a
						TMPredict(a[256:], ds, a[ao:], a[lo:], top, w)
						algo133Round3FrozenTM(b[256:], ds, b[ao:], b[lo:], top, w)
						if !bytes.Equal(a[:], b[:]) {
							t.Fatalf("w%d ds%d above%d left%d top%d", w, ds, ao, lo, top)
						}
					}
				}
			}
		}
	}
}

func BenchmarkAlgo133Round3TM(b *testing.B) {
	above := [32]byte{}
	left := [32]byte{}
	for i := range above {
		above[i] = byte(i * 71)
		left[i] = byte(i * 31)
	}
	for _, w := range []int{4, 8, 16, 32} {
		for _, top := range []byte{0, 128, 255} {
			b.Run(fmt.Sprintf("w%d/top%d", w, top), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					TMPredict(algo133Round3Pixel[:], 64, above[:], left[:], top, w)
				}
			})
		}
	}
}

func algo133Round3FrozenTM(dst []byte, ds int, above, left []byte, top byte, w int) {
	if !Enabled {
		TMPredictScalar(dst, ds, above, left, top, w)
		return
	}

	if TMNative {
		if w == 4 {
			TMPredict4Native(dst, ds, above, left, top, false)
			return
		}
		TMPredictNative(dst, ds, above, left, top, w)
		return
	}

	if (w == 8 || w == 16) && TMPredictBytes(dst, ds, above, left, top, w) {
		return
	}

	t := archsimd.BroadcastInt32x4(int32(top))
	for y := range w {
		v := archsimd.BroadcastInt32x4(int32(left[y])).Sub(t)
		for x := 0; x < w; x += 4 {
			StorePixels4(dst[y*ds+x:], LoadPixels4(above[x:]).Add(v))
		}
	}
}
