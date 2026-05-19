package encoder

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestCopySourceToFrameBufferPadsVisibleToCoded(t *testing.T) {
	const width, height = 17, 9
	src := testSourceImage(width, height, 10, 80, 160)
	var dst vp8common.FrameBuffer
	if err := dst.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	CopySourceToFrameBuffer(&dst, src)
	img := &dst.Img
	for y := range height {
		for x := range width {
			got := img.Y[y*img.YStride+x]
			want := src.Y[y*src.YStride+x]
			if got != want {
				t.Fatalf("visible Y[%d,%d] = %d, want %d", y, x, got, want)
			}
		}
		last := src.Y[y*src.YStride+width-1]
		for x := width; x < img.CodedWidth; x++ {
			if got := img.Y[y*img.YStride+x]; got != last {
				t.Fatalf("coded-right Y[%d,%d] = %d, want %d", y, x, got, last)
			}
		}
	}
	lastRow := height - 1
	for y := height; y < img.CodedHeight; y++ {
		for x := range img.CodedWidth {
			got := img.Y[y*img.YStride+x]
			want := img.Y[lastRow*img.YStride+x]
			if got != want {
				t.Fatalf("coded-bottom Y[%d,%d] = %d, want %d", y, x, got, want)
			}
		}
	}
}

func TestCopySourceToFrameBufferActiveLeavesInactiveMacroblocks(t *testing.T) {
	const width, height = 32, 16
	src := testSourceImage(width, height, 200, 100, 50)
	var dst vp8common.FrameBuffer
	if err := dst.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	const sentinel = byte(0xc3)
	for y := range height {
		row := dst.Img.Y[y*dst.Img.YStride:]
		for x := range width {
			row[x] = sentinel
		}
	}
	activeMap := []uint8{1, 0}

	CopySourceToFrameBufferActive(&dst, src, activeMap, 1, 2)

	for y := range height {
		for x := range 16 {
			if got := dst.Img.Y[y*dst.Img.YStride+x]; got != src.Y[y*src.YStride+x] {
				t.Fatalf("active Y[%d,%d] = %d, want source %d",
					y, x, got, src.Y[y*src.YStride+x])
			}
		}
		for x := 16; x < width; x++ {
			if got := dst.Img.Y[y*dst.Img.YStride+x]; got != sentinel {
				t.Fatalf("inactive Y[%d,%d] = %d, want sentinel %d",
					y, x, got, sentinel)
			}
		}
	}
}

func testSourceImage(width int, height int, yBase byte, uBase byte, vBase byte) SourceImage {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	src := SourceImage{
		Width:    width,
		Height:   height,
		UVWidth:  uvWidth,
		UVHeight: uvHeight,
		YStride:  width,
		UStride:  uvWidth,
		VStride:  uvWidth,
		Y:        make([]byte, width*height),
		U:        make([]byte, uvWidth*uvHeight),
		V:        make([]byte, uvWidth*uvHeight),
	}
	for y := range height {
		for x := range width {
			src.Y[y*src.YStride+x] = yBase + byte((y*width+x)&0x1f)
		}
	}
	for y := range uvHeight {
		for x := range uvWidth {
			src.U[y*src.UStride+x] = uBase + byte((y*uvWidth+x)&0x0f)
			src.V[y*src.VStride+x] = vBase + byte((y*uvWidth+x)&0x0f)
		}
	}
	return src
}
