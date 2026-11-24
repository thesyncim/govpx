//go:build goexperiment.simd && arm64 && !purego

package pixel

import "testing"

func TestTMPredictNativeByteArithmetic(t *testing.T) {
	for _, w := range []int{8, 16} {
		dst := make([]byte, w*w+17)
		above, left := make([]byte, w), make([]byte, w)
		for d := -255; d <= 255; d++ {
			a, top := byte(max(d, 0)), byte(max(-d, 0))
			for i := range above {
				above[i] = a
			}
			for l := 0; l < 256; l++ {
				for i := range left {
					left[i] = byte(l)
				}
				TMPredict(dst[:w*w:w*w], w, above, left, top, w)
				want := byte(min(max(d+l, 0), 255))
				for i, got := range dst {
					expected := want
					if i >= w*w {
						expected = 0
					}
					if got != expected {
						t.Fatalf("w%d delta%d left%d byte%d got%d want%d", w, d, l, i, got, expected)
					}
				}
			}
		}
	}
}
