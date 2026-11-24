package govpx

import (
	"testing"
)

var frame02framePRSink int

func frame02frameRefsmoothSkinMap(rows int, cols int, skinMap []uint8) {
	if rows < 3 || cols < 3 || len(skinMap) < rows*cols {
		return
	}
	for row := 1; row < rows-1; row++ {
		for col := 1; col < cols-1; col++ {
			index := row*cols + col
			neighbors := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if skinMap[(row+dy)*cols+col+dx] != 0 {
						neighbors++
					}
				}
			}
			if skinMap[index] != 0 && neighbors < 2 {
				skinMap[index] = 0
			}
			if skinMap[index] == 0 && neighbors == 8 {
				skinMap[index] = 1
			}
		}
	}
}

func TestFramePR02(t *testing.T) {
	for _, shape := range [][2]int{{0, 0}, {1, 1}, {3, 3}, {7, 9}, {45, 80}} {
		for seed := range 10 {
			r, c := shape[0], shape[1]
			a := make([]uint8, r*c)
			for i := range a {
				a[i] = uint8((i*37 + seed*23) % (seed + 2))
			}
			z := append([]uint8{}, a...)
			smoothSkinMap(r, c, a)
			frame02frameRefsmoothSkinMap(r, c, z)
			for i := range a {
				if a[i] != z[i] {
					t.Fatalf("smooth %v seed=%d at=%d", shape, seed, i)
				}
			}
		}
	}
}

func BenchmarkFramePR02(b *testing.B) {
	for _, kind := range []string{"empty", "skin", "mixed"} {
		b.Run(kind, func(b *testing.B) {
			const r, c = 45, 80
			raw := make([]uint8, r*c)
			m := make([]uint8, r*c)
			for i := range raw {
				if kind == "skin" || kind == "mixed" && i%7 < 4 {
					raw[i] = 1
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				copy(m, raw)
				smoothSkinMap(r, c, m)
			}
			frame02framePRSink = int(m[1+c])
		})
	}
}
