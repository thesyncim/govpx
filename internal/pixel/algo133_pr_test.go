//go:build goexperiment.simd && (amd64 || arm64) && !purego

package pixel

import (
	"bytes"
	"fmt"
	"simd/archsimd"
	"testing"
)

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
