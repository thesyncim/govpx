package encoder

import (
	"bytes"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

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
