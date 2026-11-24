package encoder

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// Keep the original column accumulation as an independent scalar oracle,
// including its uint32 wrap for heights beyond the plane fast path.
func loopFilterColumnScalar(src SourceImage, img *vp8common.Image, rows, cols int, partial bool) int {
	start, count := 0, rows
	if partial {
		start, count = LoopFilterPartialFrameWindow(rows)
	}
	var total int
	for c := range cols {
		var column uint32
		for y := start * 16; y < (start+count)*16; y++ {
			for x := c * 16; x < (c+1)*16; x++ {
				d := int(src.Y[y*src.YStride+x]) - int(img.Y[y*img.YStride+x])
				column += uint32(d * d)
			}
		}
		total += int(column)
	}
	return total
}

func TestLoopFilterLumaSSEPlaneMatchesColumns(t *testing.T) {
	for _, size := range [][2]int{{16, 16}, {32, 4096}, {32, 4144}, {128, 128}, {1280, 720}, {16400, 16}} {
		w, h := size[0], size[1]
		ss, rs := w+3, w+7
		a, b := make([]byte, 3+(h-1)*ss+w)[3:], make([]byte, 5+(h-1)*rs+w)[5:]
		for mode := range 2 {
			for i := range a {
				if mode == 0 {
					a[i] = byte(i*37 + 11)
				} else {
					a[i] = 255
				}
			}
			for i := range b {
				if mode == 0 {
					b[i] = byte(i*19 + 53)
				} else {
					b[i] = 0
				}
			}
			src := SourceImage{Y: a, YStride: ss, Width: w, Height: h}
			img := vp8common.Image{Y: b, YStride: rs, CodedWidth: w, CodedHeight: h}
			for _, partial := range []bool{false, true} {
				want := loopFilterColumnScalar(src, &img, h/16, w/16, partial)
				got := LoopFilterLumaSSE(src, &img, h/16, w/16, partial)
				if got != want {
					t.Fatalf("%dx%d mode %d partial %v got %d want %d", w, h, mode, partial, got, want)
				}
			}
		}
	}
}

func TestLoopFilterLumaSSEPlaneKeepsClampedEdges(t *testing.T) {
	w, h := 31, 29
	ss, rs := w+3, w+7
	a, b := make([]byte, (h-1)*ss+w), make([]byte, (h-1)*rs+w)
	for i := range a {
		a[i] = byte(i*37 + 11)
	}
	for i := range b {
		b[i] = byte(i*19 + 53)
	}
	src := SourceImage{Y: a, YStride: ss, Width: w, Height: h}
	img := vp8common.Image{Y: b, YStride: rs, CodedWidth: w, CodedHeight: h}
	for _, partial := range []bool{false, true} {
		start, count := 0, 2
		if partial {
			start, count = LoopFilterPartialFrameWindow(2)
		}
		want := 0
		for y := start * 16; y < (start+count)*16; y++ {
			for x := range 32 {
				d := int(a[min(y, h-1)*ss+min(x, w-1)]) - int(b[min(y, h-1)*rs+min(x, w-1)])
				want += d * d
			}
		}
		if got := LoopFilterLumaSSE(src, &img, 2, 2, partial); got != want {
			t.Fatalf("partial %v got %d want %d", partial, got, want)
		}
	}
}
