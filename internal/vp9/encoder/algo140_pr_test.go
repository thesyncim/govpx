package encoder

import (
	"fmt"
	"testing"
)

func algo140R3sFirstInput(pattern string) (a, p [16384]byte) {
	for i := range a {
		if pattern == "flat" {
			a[i] = 127
			p[i] = 127
		} else {
			a[i] = byte((i*73 + i/128*19) ^ ((i / 128) * 37))
			p[i] = byte(i*47 + i/128*71)
		}
	}
	if pattern == "same" {
		p = a
	}
	if pattern == "shift" {
		copy(p[129:], a[:len(a)-129])
	}
	return
}

func TestAlgo140Round3FirstPass(t *testing.T) {
	for _, pattern := range []string{"flat", "same", "shift", "dense"} {
		a, p := algo140R3sFirstInput(pattern)
		for _, xy := range [][2]int{{0, 0}, {7, 13}, {48, 57}, {112, 112}} {
			for _, size := range [][2]int{{4, 4}, {8, 8}, {16, 16}, {11, 7}} {
				x, r, c := FirstPassMotionSearch(a[:], 128, p[:], 128, xy[0], xy[1], size[0], size[1], 128, 128)
				y, s, d := algo140R3sFrozenFirstPass(a[:], 128, p[:], 128, xy[0], xy[1], size[0], size[1], 128, 128)
				if x != y || r != s || c != d {
					t.Fatalf("%s xy%v size%v got%d/%d/%d want%d/%d/%d", pattern, xy, size, x, r, c, y, s, d)
				}
			}
		}
	}
}

var algo140R3sError uint64

var algo140R3sRow, r3sCol int16

func BenchmarkAlgo140Round3FirstPass(b *testing.B) {
	for _, pattern := range []string{"flat", "same", "shift", "dense"} {
		a, p := algo140R3sFirstInput(pattern)
		for _, size := range []int{8, 16} {
			b.Run(fmt.Sprintf("%s/%d", pattern, size), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					algo140R3sError, algo140R3sRow, r3sCol = FirstPassMotionSearch(a[:], 128, p[:], 128, 48, 48, size, size, 128, 128)
				}
			})
		}
	}
}

func algo140R3sFrozenFirstPass(src []byte, srcStride int, ref []byte, refStride int,
	x int, y int, w int, h int, width int, height int,
) (best uint64, bestRowQ3 int16, bestColQ3 int16) {
	best = ^uint64(0)
	for dy := -FirstPassSearchRange; dy <= FirstPassSearchRange; dy++ {
		refY := y + dy
		if refY < 0 || refY+h > height {
			continue
		}
		for dx := -FirstPassSearchRange; dx <= FirstPassSearchRange; dx++ {
			refX := x + dx
			if refX < 0 || refX+w > width {
				continue
			}
			err := BlockSSE(src, srcStride, ref, refStride, x, y, refX, refY, w, h)
			if err < best {
				best = err
				bestRowQ3 = int16(dy << 3)
				bestColQ3 = int16(dx << 3)
			}
		}
	}
	return best, bestRowQ3, bestColQ3
}
