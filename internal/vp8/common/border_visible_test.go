package common

import "testing"

// TestExtendBordersFromVisibleOverwritesCodedPadding verifies that
// ExtendBordersFromVisible mirrors libvpx vp8_yv12_extend_frame_borders_c:
// extend_plane is called with width=y_crop_width / height=y_crop_height,
// so the coded-but-padded region between the visible edge and the
// 16-aligned coded edge is overwritten with the visible-edge sample
// rather than preserved.
func TestExtendBordersFromVisibleOverwritesCodedPadding(t *testing.T) {
	fb, err := NewFrameBuffer(5, 3, 2, 8)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	uvWidth := (fb.Img.Width + 1) >> 1
	codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
	// Visible right-edge column (col Width-1=4) is the value libvpx
	// would replicate; the coded-padding cell (col Width=5) starts with
	// a sentinel that ExtendBordersFromVisible must overwrite.
	fb.Img.Y[fb.Img.Width-1] = 90
	fb.Img.Y[fb.Img.Width] = 91
	fb.Img.U[uvWidth-1] = 60
	fb.Img.U[uvWidth] = 61

	fb.ExtendBordersFromVisible()

	yPlane := fb.buf[fb.yPlaneOff:fb.uPlaneOff]
	top := fb.border
	left := fb.border
	stride := fb.Img.YStride
	row0 := top * stride
	if got := yPlane[row0+left+fb.Img.Width]; got != 90 {
		t.Fatalf("Y coded padding at col Width = %d, want visible-edge 90", got)
	}
	if got := yPlane[row0+left+fb.Img.CodedWidth]; got != 90 {
		t.Fatalf("right Y border = %d, want visible-edge 90", got)
	}

	uPlane := fb.buf[fb.uPlaneOff:fb.vPlaneOff]
	uvBorder := (fb.border + 1) >> 1
	uvRow0 := uvBorder * fb.Img.UStride
	if got := uPlane[uvRow0+uvBorder+uvWidth]; got != 60 {
		t.Fatalf("U coded padding at col uvWidth = %d, want visible-edge 60", got)
	}
	if got := uPlane[uvRow0+uvBorder+codedUVWidth]; got != 60 {
		t.Fatalf("right U border = %d, want visible-edge 60", got)
	}
}

// TestExtendBordersFromVisible16AlignedMatchesExtendBorders confirms
// the libvpx-faithful path collapses to the existing coded-edge extend
// when the visible dimensions already match the 16-aligned coded
// dimensions (the most common case).
func TestExtendBordersFromVisible16AlignedMatchesExtendBorders(t *testing.T) {
	fbA, err := NewFrameBuffer(16, 16, 2, 8)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	fbB, err := NewFrameBuffer(16, 16, 2, 8)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	for y := 0; y < fbA.Img.CodedHeight; y++ {
		for x := 0; x < fbA.Img.CodedWidth; x++ {
			v := byte((y*7 + x*11) & 0xff)
			fbA.Img.Y[y*fbA.Img.YStride+x] = v
			fbB.Img.Y[y*fbB.Img.YStride+x] = v
		}
	}
	fbA.ExtendBorders()
	fbB.ExtendBordersFromVisible()
	if string(fbA.buf) != string(fbB.buf) {
		t.Fatalf("ExtendBordersFromVisible should match ExtendBorders on 16-aligned frames")
	}
}
