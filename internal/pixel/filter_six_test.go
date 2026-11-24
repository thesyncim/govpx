package pixel

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func TestSixTapNarrowBoundsAndRounding(t *testing.T) {
	taps := [][6]int16{{0, 0, 128, 0, 0, 0}, {0, -6, 123, 12, -1, 0}, {2, -11, 108, 36, -8, 1}, {0, -9, 93, 50, -6, 0}, {3, -16, 77, 77, -16, 3}, {0, -6, 50, 93, -9, 0}, {1, -8, 36, 108, -11, 2}, {0, -1, 12, 123, -6, 0}, {128, -64, 128, 128, -64, 128}, {128, -128, 128, 128, 0, 128}, {0, -128, 0, 0, 0, 0}}
	rng := rand.New(rand.NewPCG(99201, 67319))
	for _, w := range []int{4, 8, 16, 32} {
		for _, h := range []int{1, 3, 16} {
			for _, vertical := range []bool{false, true} {
				ss, ds := w+7, w+3
				step := 1
				if vertical {
					step = ss
				}
				for align := range 4 {
					for trial := range 6 {
						src := make([]byte, align+(h-1)*ss+w+5*step)
						for i := range src {
							switch trial {
							case 0:
								src[i] = 0
							case 1:
								src[i] = 255
							case 2:
								src[i] = byte((i % 2) * 255)
							default:
								src[i] = byte(rng.Uint32())
							}
						}
						for _, f := range taps {
							got := bytes.Repeat([]byte{217}, align+(h-1)*ds+w)
							want := bytes.Clone(got)
							Filter(src[align:], ss, got[align:], ds, w, h, step, f[:])
							FilterScalar(src[align:], ss, want[align:], ds, w, h, step, f[:])
							if !bytes.Equal(got, want) {
								t.Fatalf("w=%d h=%d vertical=%v align=%d trial=%d taps=%v", w, h, vertical, align, trial, f)
							}
						}
					}
				}
			}
		}
	}
}
