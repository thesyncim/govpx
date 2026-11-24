package encoder

import (
	"fmt"
	"testing"
)

func TestAlgo134Round3Variance4(t *testing.T) {
	var a, b [4096]byte
	for i := range a {
		a[i] = byte(i*97 + 17)
		b[i] = byte(i*53 + 71)
	}
	for _, key := range []bool{false, true} {
		for _, stride := range []int{8, 13, 64} {
			for _, wh := range [][2]int{{8, 8}, {7, 7}, {13, 9}, {64, 64}} {
				if wh[0] > stride {
					continue
				}
				for _, xy := range [][2]int{{0, 0}, {1, 1}, {5, 3}, {10, 7}, {63, 63}} {
					var x, y V8x8
					for i := range x.Split {
						x.Split[i].PartVariances.None.Variance = 97
					}
					y = x
					fillVariance4x4Avg(a[:], stride, b[:], stride, xy[0], xy[1], &x, wh[0], wh[1], key)
					algo134Round3FrozenVariance4(a[:], stride, b[:], stride, xy[0], xy[1], &y, wh[0], wh[1], key)
					if x != y {
						t.Fatalf("key%v stride%d wh%v xy%v", key, stride, wh, xy)
					}
				}
			}
		}
	}
}

var algo134Round3Tree V8x8

func BenchmarkAlgo134Round3Variance4(b *testing.B) {
	var a, p [4096]byte
	for i := range a {
		a[i] = byte(i*97 + 17)
		p[i] = byte(i*53 + 71)
	}
	for _, key := range []bool{false, true} {
		for _, xy := range [][2]int{{0, 0}, {7, 11}, {60, 60}} {
			b.Run(fmt.Sprintf("key%v/x%d", key, xy[0]), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					fillVariance4x4Avg(a[:], 64, p[:], 64, xy[0], xy[1], &algo134Round3Tree, 64, 64, key)
				}
			})
		}
	}
}

func algo134Round3FrozenVariance4(src []uint8, srcStride int, dst []uint8, dstStride int,
	x8Idx, y8Idx int, vst *V8x8, pixelsWide, pixelsHigh int, isKeyFrame bool,
) {
	for k := range 4 {
		x4Idx := x8Idx + ((k & 1) << 2)
		y4Idx := y8Idx + ((k >> 1) << 2)
		var sse uint32
		var sum int32
		if x4Idx < pixelsWide && y4Idx < pixelsHigh {
			sAvg := avg4x4Clamped(src, srcStride, x4Idx, y4Idx,
				pixelsWide, pixelsHigh)
			dAvg := 128
			if !isKeyFrame {
				dAvg = avg4x4Clamped(dst, dstStride, x4Idx, y4Idx,
					pixelsWide, pixelsHigh)
			}
			diff := sAvg - dAvg
			sum = int32(diff)
			sse = uint32(diff * diff)
		}
		fillVariance(sse, sum, 0, &vst.Split[k].PartVariances.None)
	}
}

// Frozen from production870699ce; candidate source never generates expectations.
