package dsp

import (
	"fmt"
	"testing"
)

// Identical public-call workloads are overlaid on each pinned reference.
func BenchmarkSixTapPhases(b *testing.B) {
	for _, w := range []int{4, 8, 16} {
		for _, phase := range [][2]int{{4, 0}, {0, 4}, {2, 4}, {3, 5}} {
			b.Run(fmt.Sprintf("%dx%d/%d,%d", w, w, phase[0], phase[1]), func(b *testing.B) {
				src := make([]byte, 64*64)
				dst := make([]byte, w*w)
				for i := range src {
					src[i] = byte(i*37 + i/64*11)
				}
				b.ReportAllocs()
				for b.Loop() {
					switch w {
					case 4:
						SixTapPredict4x4(src, 64, phase[0], phase[1], dst, w)
					case 8:
						SixTapPredict8x8(src, 64, phase[0], phase[1], dst, w)
					case 16:
						SixTapPredict16x16(src, 64, phase[0], phase[1], dst, w)
					}
				}
			})
		}
	}
}
