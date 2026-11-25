//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestTMPredictBytesPacketLaneSelection(t *testing.T) {
	for _, w := range []int{8, 16} {
		for _, ds := range []int{0, 1, w - 1, w, 37} {
			for _, top := range []byte{0, 1, 127, 128, 254, 255} {
				for shift := 0; shift < 256; shift++ {
					span := (w-1)*ds + w
					got := bytes.Repeat([]byte{39}, span+47)
					want := bytes.Clone(got)
					a, l := make([]byte, w+31), make([]byte, w+31)
					for i := 0; i < w; i++ {
						a[7+i] = byte(i*37 + shift)
						l[15+i] = byte(i*101 + shift)
					}
					ab, lb := bytes.Clone(a), bytes.Clone(l)
					tmBytesPrior(want[15:15+span:15+span], ds, a[7:7+w:7+w], l[15:15+w:15+w], top, w)
					if !TMPredictBytes(got[15:15+span:15+span], ds, a[7:7+w:7+w], l[15:15+w:15+w], top, w) {
						t.Fatal("eligible disjoint packet declined")
					}
					if !bytes.Equal(got, want) || !bytes.Equal(a, ab) || !bytes.Equal(l, lb) {
						t.Fatalf("w%d ds%d top%d shift%d: lane, row-store order or backing mismatch", w, ds, top, shift)
					}
				}
			}
		}
	}
}

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
