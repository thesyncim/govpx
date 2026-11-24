//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestSixTapPreservesEightPixelAliasOrder(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	for _, w := range []int{8, 16, 32} {
		const h, ss = 7, 40
		for _, ds := range []int{0, w - 1, w, w + 1} {
			for _, dest := range []int{0, 1, 7, 17, 47, 127} {
				for _, taps := range [][6]int16{{3, -16, 77, 77, -16, 3}, {0, -9, 93, 50, -6, 0}} {
					got := make([]byte, 1024)
					for i := range got {
						got[i] = byte(i*37 + 17)
					}
					want := bytes.Clone(got)
					Filter(got, ss, got[dest:], ds, w, h, ss, taps[:])
					// The previous kernel loads a complete eight-pixel packet before
					// writing it. Model that sequencing independently for aliased slices.
					for y := range h {
						for x := 0; x < w; x += 8 {
							var packet [8]byte
							for i := range 8 {
								sum := 64
								for k, c := range taps {
									sum += int(want[y*ss+x+i+k*ss]) * int(c)
								}
								packet[i] = byte(min(255, max(0, sum>>7)))
							}
							copy(want[dest+y*ds+x:], packet[:])
						}
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("w%d ds%d dest%d taps%v", w, ds, dest, taps)
					}
				}
			}
		}
	}
}
