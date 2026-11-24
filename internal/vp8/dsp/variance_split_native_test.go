package dsp

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestSplitSubpelMinimalWindows(t *testing.T) {
	for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}} {
		w, h := shape[0], shape[1]
		for align := range 4 {
			ss, rs := w+1, w+3
			src, ref := make([]byte, align+h*ss+w+1), make([]byte, align+(h-1)*rs+w)
			for i := range src {
				src[i] = byte(i*37 + i/ss*11)
			}
			for i := range ref {
				ref[i] = byte(i*71 + i/rs*19)
			}
			savedSrc, savedRef := bytes.Clone(src), bytes.Clone(ref)
			for x := range 8 {
				for y := range 8 {
					gotV, gotS := subpelVariance(src[align:], ss, x, y, ref[align:], rs, w, h)
					fx, fy := tables.BilinearFilters[x], tables.BilinearFilters[y]
					sum, sse := 0, 0
					for row := range h {
						for col := range w {
							at := align + row*ss + col
							a := (int(src[at])*int(fx[0]) + int(src[at+1])*int(fx[1]) + 64) >> 7
							b := (int(src[at+ss])*int(fx[0]) + int(src[at+ss+1])*int(fx[1]) + 64) >> 7
							value := (a*int(fy[0]) + b*int(fy[1]) + 64) >> 7
							delta := value - int(ref[align+row*rs+col])
							sum += delta
							sse += delta * delta
						}
					}
					if gotV != sse-sum*sum/(w*h) || gotS != sse {
						t.Fatalf("shape=%v align=%d phase=%d,%d got=%d,%d want=%d,%d", shape, align, x, y, gotV, gotS, sse-sum*sum/(w*h), sse)
					}
				}
			}
			if !bytes.Equal(src, savedSrc) || !bytes.Equal(ref, savedRef) {
				t.Fatal("subpixel variance changed an input")
			}
		}
	}
}
