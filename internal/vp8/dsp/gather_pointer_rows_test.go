package dsp

import (
	"bytes"
	"testing"
)

func TestResidualGatherPointerRows(t *testing.T) {
	for _, w := range []int{8, 16} {
		for _, strides := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 3}, {w - 1, w + 1}, {32, 33}, {1312, 1320}} {
			for align := range 8 {
				for pattern := range 3 {
					ss, ps := strides[0], strides[1]
					src, pred := make([]byte, align+(w-1)*ss+w+13), make([]byte, align+(w-1)*ps+w+13)
					for i := range src {
						src[i] = byte(i*73 + 197)
						if pattern == 1 {
							src[i] = 0
						}
						if pattern == 2 {
							src[i] = 255
						}
					}
					for i := range pred {
						pred[i] = byte(i*131 + 7)
						if pattern == 1 {
							pred[i] = 255
						}
						if pattern == 2 {
							pred[i] = 0
						}
					}
					beforeSrc, beforePred := bytes.Clone(src), bytes.Clone(pred)
					out := make([]int16, w*w+16)
					for i := range out {
						out[i] = 12345
					}
					fn := ResidualGather16x16PtrFast
					if w == 8 {
						fn = ResidualGather8x8PtrFast
					}
					fn(&src[align], ss, &pred[align], ps, &out[8])
					for y := range w {
						for x := range w {
							at := 8 + ((y/4)*(w/4)+x/4)*16 + (y%4)*4 + x%4
							want := int16(src[align+y*ss+x]) - int16(pred[align+y*ps+x])
							if out[at] != want {
								t.Fatalf("w%d strides%v align%d pattern%d xy%d,%d got%d want%d", w, strides, align, pattern, x, y, out[at], want)
							}
						}
					}
					for i := range 8 {
						if out[i] != 12345 || out[len(out)-1-i] != 12345 {
							t.Fatal("output guard changed")
						}
					}
					if !bytes.Equal(src, beforeSrc) || !bytes.Equal(pred, beforePred) {
						t.Fatal("input changed")
					}
				}
			}
		}
	}
}
