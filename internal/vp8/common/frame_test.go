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
	if fb.Img.YStride != 16 || fb.Img.UStride != 16 || fb.Img.VStride != 16 {
		t.Fatalf("strides = %d/%d/%d, want 16/16/16", fb.Img.YStride, fb.Img.UStride, fb.Img.VStride)
	}
	if len(fb.Img.Y) != 37 {
		t.Fatalf("len(Y) = %d, want 37", len(fb.Img.Y))
	}
	if len(fb.Img.U) != 19 || len(fb.Img.V) != 19 {
		t.Fatalf("len(U/V) = %d/%d, want 19/19", len(fb.Img.U), len(fb.Img.V))
	}
	if fb.BufferLen() != 240 {
		t.Fatalf("BufferLen = %d, want 240", fb.BufferLen())
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
