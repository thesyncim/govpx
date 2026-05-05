package common

import "testing"

func TestFrameBufferLayoutOddDimensions(t *testing.T) {
	fb, err := NewFrameBuffer(5, 3, 2, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}

	if fb.Img.Width != 5 || fb.Img.Height != 3 {
		t.Fatalf("dimensions = %dx%d, want 5x3", fb.Img.Width, fb.Img.Height)
	}
	if fb.Img.CodedWidth != 16 || fb.Img.CodedHeight != 16 {
		t.Fatalf("coded dimensions = %dx%d, want 16x16", fb.Img.CodedWidth, fb.Img.CodedHeight)
	}
	if fb.Img.YStride != 32 || fb.Img.UStride != 16 || fb.Img.VStride != 16 {
		t.Fatalf("strides = %d/%d/%d, want 32/16/16", fb.Img.YStride, fb.Img.UStride, fb.Img.VStride)
	}
	if len(fb.Img.Y) != 496 {
		t.Fatalf("len(Y) = %d, want 496", len(fb.Img.Y))
	}
	if len(fb.Img.U) != 120 || len(fb.Img.V) != 120 {
		t.Fatalf("len(U/V) = %d/%d, want 120/120", len(fb.Img.U), len(fb.Img.V))
	}
	if fb.BufferLen() != 960 {
		t.Fatalf("BufferLen = %d, want 960", fb.BufferLen())
	}
}

func TestFrameBufferRejectsInvalidSize(t *testing.T) {
	_, err := NewFrameBuffer(0, 16, 0, 16)
	if err != ErrInvalidFrameSize {
		t.Fatalf("error = %v, want ErrInvalidFrameSize", err)
	}
}

func TestFrameBufferResizeReusesAllocation(t *testing.T) {
	fb, err := NewFrameBuffer(16, 16, 4, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		_ = fb.Resize(16, 16, 4, 32)
	})
	if allocs != 0 {
		t.Fatalf("Resize reuse allocs = %v, want 0", allocs)
	}
}

func TestExtendBorders(t *testing.T) {
	fb, err := NewFrameBuffer(3, 3, 2, 8)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	for y := 0; y < fb.Img.Height; y++ {
		for x := 0; x < fb.Img.Width; x++ {
			fb.Img.Y[y*fb.Img.YStride+x] = byte(10*y + x + 1)
		}
	}
	uvWidth := (fb.Img.Width + 1) >> 1
	uvHeight := (fb.Img.Height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		for x := 0; x < uvWidth; x++ {
			fb.Img.U[y*fb.Img.UStride+x] = byte(40 + 10*y + x)
			fb.Img.V[y*fb.Img.VStride+x] = byte(70 + 10*y + x)
		}
	}

	fb.ExtendBorders()

	yPlane := fb.buf[fb.yPlaneOff:fb.uPlaneOff]
	top := fb.border
	left := fb.border
	stride := fb.Img.YStride
	if yPlane[top*stride+left-1] != 1 {
		t.Fatalf("left Y border = %d, want 1", yPlane[top*stride+left-1])
	}
	if yPlane[top*stride+left+fb.Img.Width] != 3 {
		t.Fatalf("right Y border = %d, want 3", yPlane[top*stride+left+fb.Img.Width])
	}
	if yPlane[(top-1)*stride+left] != 1 {
		t.Fatalf("top Y border = %d, want 1", yPlane[(top-1)*stride+left])
	}
	if yPlane[(top+fb.Img.Height)*stride+left+2] != 23 {
		t.Fatalf("bottom Y border = %d, want 23", yPlane[(top+fb.Img.Height)*stride+left+2])
	}

	uPlane := fb.buf[fb.uPlaneOff:fb.vPlaneOff]
	uvBorder := (fb.border + 1) >> 1
	if uPlane[uvBorder*fb.Img.UStride+uvBorder-1] != 40 {
		t.Fatalf("left U border = %d, want 40", uPlane[uvBorder*fb.Img.UStride+uvBorder-1])
	}
	if uPlane[(uvBorder+uvHeight)*fb.Img.UStride+uvBorder+uvWidth-1] != 51 {
		t.Fatalf("bottom U border = %d, want 51", uPlane[(uvBorder+uvHeight)*fb.Img.UStride+uvBorder+uvWidth-1])
	}
}

func TestExtendBordersFillsCodedPadding(t *testing.T) {
	fb, err := NewFrameBuffer(5, 3, 2, 8)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	for y := 0; y < fb.Img.Height; y++ {
		for x := 0; x < fb.Img.Width; x++ {
			fb.Img.Y[y*fb.Img.YStride+x] = byte(10*y + x + 1)
		}
	}
	uvWidth := (fb.Img.Width + 1) >> 1
	uvHeight := (fb.Img.Height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		for x := 0; x < uvWidth; x++ {
			fb.Img.U[y*fb.Img.UStride+x] = byte(40 + 10*y + x)
		}
	}

	fb.ExtendBorders()

	yPlane := fb.buf[fb.yPlaneOff:fb.uPlaneOff]
	top := fb.border
	left := fb.border
	stride := fb.Img.YStride
	row0 := top * stride
	row0Edge := yPlane[row0+left+fb.Img.Width-1]
	if got := yPlane[row0+left+fb.Img.Width]; got != row0Edge {
		t.Fatalf("first Y coded padding = %d, want edge %d", got, row0Edge)
	}
	if got := yPlane[row0+left+fb.Img.CodedWidth+fb.border-1]; got != row0Edge {
		t.Fatalf("far right Y border = %d, want edge %d", got, row0Edge)
	}
	bottomEdge := yPlane[(top+fb.Img.Height-1)*stride+left+fb.Img.Width-1]
	bottomRow := top + fb.Img.CodedHeight + fb.border - 1
	if got := yPlane[bottomRow*stride+left+fb.Img.CodedWidth+fb.border-1]; got != bottomEdge {
		t.Fatalf("far bottom Y border = %d, want edge %d", got, bottomEdge)
	}

	uPlane := fb.buf[fb.uPlaneOff:fb.vPlaneOff]
	uvBorder := (fb.border + 1) >> 1
	codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
	codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
	uvRow0 := uvBorder * fb.Img.UStride
	uvEdge := uPlane[uvRow0+uvBorder+uvWidth-1]
	if got := uPlane[uvRow0+uvBorder+uvWidth]; got != uvEdge {
		t.Fatalf("first U coded padding = %d, want edge %d", got, uvEdge)
	}
	uvBottomEdge := uPlane[(uvBorder+uvHeight-1)*fb.Img.UStride+uvBorder+uvWidth-1]
	uvBottomRow := uvBorder + codedUVHeight + uvBorder - 1
	if got := uPlane[uvBottomRow*fb.Img.UStride+uvBorder+codedUVWidth+uvBorder-1]; got != uvBottomEdge {
		t.Fatalf("far bottom U border = %d, want edge %d", got, uvBottomEdge)
	}
}

func TestExtendBordersAllocatesZero(t *testing.T) {
	fb, err := NewFrameBuffer(16, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		fb.ExtendBorders()
	})
	if allocs != 0 {
		t.Fatalf("ExtendBorders allocs = %v, want 0", allocs)
	}
}
