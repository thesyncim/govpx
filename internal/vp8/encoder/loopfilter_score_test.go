package encoder

import (
	"bytes"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

var lumaSSENativeSink int

func BenchmarkLoopFilterLumaSSENative(b *testing.B) {
	for _, tc := range []struct {
		name    string
		w, h    int
		partial bool
	}{
		{"128x128", 128, 128, false}, {"720p", 1280, 720, false}, {"720pPartial", 1280, 720, true}, {"1088p", 1920, 1088, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			ss, rs := tc.w+32, tc.w+64
			a, ref := make([]byte, (tc.h-1)*ss+tc.w), make([]byte, (tc.h-1)*rs+tc.w)
			for i := range a {
				a[i] = byte(i*37 + 11)
			}
			for i := range ref {
				ref[i] = byte(i*19 + 53)
			}
			src := SourceImage{Y: a, YStride: ss, Width: tc.w, Height: tc.h}
			img := vp8common.Image{Y: ref, YStride: rs, CodedWidth: tc.w, CodedHeight: tc.h}
			rows, cols := tc.h/16, tc.w/16
			start, count := 0, rows
			if tc.partial {
				start, count = LoopFilterPartialFrameWindow(rows)
			}
			want := 0
			for y := start * 16; y < (start+count)*16; y++ {
				for x := range tc.w {
					d := int(a[y*ss+x]) - int(ref[y*rs+x])
					want += d * d
				}
			}
			got := 0
			b.ReportAllocs()
			for b.Loop() {
				got = LoopFilterLumaSSE(src, &img, rows, cols, tc.partial)
			}
			if got != want {
				b.Fatalf("got%d want%d", got, want)
			}
			lumaSSENativeSink = got
		})
	}
}

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

func TestLibvpxInitialLoopFilterLevelUsesBaseQThreeEighths(t *testing.T) {
	tests := []struct {
		qIndex int
		want   int
	}{
		{qIndex: 0, want: 0},
		{qIndex: 6, want: 2},
		{qIndex: 16, want: 6},
		{qIndex: 20, want: 7},
		{qIndex: 127, want: 47},
		{qIndex: 1000, want: 63},
	}
	for _, tt := range tests {
		if got := LibvpxInitialLoopFilterLevel(tt.qIndex); got != tt.want {
			t.Fatalf("q=%d loop filter level = %d, want %d", tt.qIndex, got, tt.want)
		}
	}
}

func TestLoopFilterPartialFrameWindowMirrorsLibvpxMiddleSlice(t *testing.T) {
	tests := []struct {
		rows      int
		wantStart int
		wantCount int
	}{
		{rows: 0, wantStart: 0, wantCount: 0},
		{rows: 1, wantStart: 0, wantCount: 1},
		{rows: 2, wantStart: 1, wantCount: 1},
		{rows: 4, wantStart: 2, wantCount: 1},
		{rows: 8, wantStart: 4, wantCount: 1},
		{rows: 16, wantStart: 8, wantCount: 2},
	}
	for _, tt := range tests {
		start, count := LoopFilterPartialFrameWindow(tt.rows)
		if start != tt.wantStart || count != tt.wantCount {
			t.Fatalf("rows=%d partial window = %d,%d want %d,%d", tt.rows, start, count, tt.wantStart, tt.wantCount)
		}
	}
}

func TestLoopFilterLumaSSEPartialScoresOnlyMiddleWindow(t *testing.T) {
	src := testSourceImage(64, 64, 20, 128, 128)
	for i := range src.Y {
		src.Y[i] = 20
	}
	ref := testLoopFilterFrame(t, 64, 64, 20)
	for row := range 16 {
		for col := range 64 {
			ref.Img.Y[row*ref.Img.YStride+col] = 100
		}
	}
	for row := 32; row < 48; row++ {
		for col := range 64 {
			ref.Img.Y[row*ref.Img.YStride+col] = 23
		}
	}

	got := LoopFilterLumaSSE(src, &ref.Img, 4, 4, true)
	want := 4 * 16 * 16 * 3 * 3
	if got != want {
		t.Fatalf("partial luma SSE = %d, want %d", got, want)
	}
}

func TestCopyLoopFilterPartialLumaCopiesLibvpxStrideWindow(t *testing.T) {
	src := testLoopFilterFrame(t, 64, 128, 10)
	dst := testLoopFilterFrame(t, 64, 128, 99)
	for i := range src.Img.YFull {
		src.Img.YFull[i] = byte(i*17 + 3)
	}
	for i := range dst.Img.YFull {
		dst.Img.YFull[i] = 0
	}

	startRow, rowCount := LoopFilterPartialFrameWindow(8)
	CopyLoopFilterPartialLuma(&dst.Img, &src.Img, startRow, rowCount)

	startY := startRow*16 - 4
	endY := (startRow + rowCount) * 16
	for y := startY; y < endY; y++ {
		srcOff := src.Img.YOrigin + y*src.Img.YStride
		dstOff := dst.Img.YOrigin + y*dst.Img.YStride
		got := dst.Img.YFull[dstOff : dstOff+dst.Img.YStride]
		want := src.Img.YFull[srcOff : srcOff+src.Img.YStride]
		if !bytes.Equal(got, want) {
			t.Fatalf("copied row %d differs from libvpx y_stride window", y)
		}
	}

	beforeOff := dst.Img.YOrigin + (startY-1)*dst.Img.YStride
	beforeSrc := src.Img.YOrigin + (startY-1)*src.Img.YStride
	if bytes.Equal(dst.Img.YFull[beforeOff:beforeOff+dst.Img.YStride], src.Img.YFull[beforeSrc:beforeSrc+src.Img.YStride]) {
		t.Fatalf("row before partial window was copied")
	}
}

func TestCopyLoopFilterPartialLumaCopiesLibvpxTopContext(t *testing.T) {
	src := testLoopFilterFrame(t, 16, 16, 10)
	dst := testLoopFilterFrame(t, 16, 16, 99)
	for i := range src.Img.YFull {
		src.Img.YFull[i] = byte(i*17 + 3)
	}
	for i := range dst.Img.YFull {
		dst.Img.YFull[i] = 0
	}

	startRow, rowCount := LoopFilterPartialFrameWindow(1)
	CopyLoopFilterPartialLuma(&dst.Img, &src.Img, startRow, rowCount)

	top := src.Img.YFull[src.Img.YOrigin : src.Img.YOrigin+src.Img.YStride]
	for y := -4; y < 0; y++ {
		dstOff := dst.Img.YOrigin + y*dst.Img.YStride
		if got := dst.Img.YFull[dstOff : dstOff+dst.Img.YStride]; !bytes.Equal(got, top) {
			t.Fatalf("top context row %d differs from libvpx top-row fill", y)
		}
	}
	for y := range 16 {
		srcOff := src.Img.YOrigin + y*src.Img.YStride
		dstOff := dst.Img.YOrigin + y*dst.Img.YStride
		got := dst.Img.YFull[dstOff : dstOff+dst.Img.YStride]
		want := src.Img.YFull[srcOff : srcOff+src.Img.YStride]
		if !bytes.Equal(got, want) {
			t.Fatalf("visible row %d differs from libvpx y_stride window", y)
		}
	}
	beforeOff := dst.Img.YOrigin - 5*dst.Img.YStride
	if bytes.Equal(dst.Img.YFull[beforeOff:beforeOff+dst.Img.YStride], top) {
		t.Fatalf("row before top context was copied")
	}
}

func testLoopFilterFrame(t *testing.T, width int, height int, y byte) *vp8common.FrameBuffer {
	t.Helper()
	var fb vp8common.FrameBuffer
	if err := fb.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	for i := range fb.Img.Y {
		fb.Img.Y[i] = y
	}
	return &fb
}
