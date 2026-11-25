package dsp

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

var nativeVarianceResult int

func BenchmarkVariance16x16Native(b *testing.B) {
	const stride = 19
	var src, ref [15*stride + 16]byte
	var sum, sse int
	for i := range src {
		src[i] = byte(i*73 + 19)
		ref[i] = byte(i*131 + 53)
	}
	for y := range 16 {
		for x := range 16 {
			d := int(src[y*stride+x]) - int(ref[y*stride+x])
			sum += d
			sse += d * d
		}
	}
	want := sse - (sum * sum >> 8)
	if got := Variance16x16(src[:], stride, ref[:], stride); got != want {
		b.Fatalf("variance %d want %d", got, want)
	}
	b.ReportAllocs()
	var result int
	for b.Loop() {
		result = Variance16x16(src[:], stride, ref[:], stride)
	}
	nativeVarianceResult = result
	if result != want {
		b.Fatal("timed variance changed")
	}
}

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
