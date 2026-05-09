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
	if len(fb.Img.YFull) != fb.Img.YStride*fb.yRows || len(fb.Img.UFull) != fb.Img.UStride*fb.uRows || len(fb.Img.VFull) != fb.Img.VStride*fb.vRows {
		t.Fatalf("full plane lengths = %d/%d/%d, want %d/%d/%d", len(fb.Img.YFull), len(fb.Img.UFull), len(fb.Img.VFull), fb.Img.YStride*fb.yRows, fb.Img.UStride*fb.uRows, fb.Img.VStride*fb.vRows)
	}
	if fb.Img.YOrigin != fb.yOff || fb.Img.UOrigin != fb.uOff || fb.Img.VOrigin != fb.vOff {
		t.Fatalf("origins = %d/%d/%d, want %d/%d/%d", fb.Img.YOrigin, fb.Img.UOrigin, fb.Img.VOrigin, fb.yOff, fb.uOff, fb.vOff)
	}
	if fb.Img.YBorder != 2 || fb.Img.UVBorder != 1 {
		t.Fatalf("borders = %d/%d, want 2/1", fb.Img.YBorder, fb.Img.UVBorder)
	}
	fb.Img.Y[0] = 77
	if fb.Img.YFull[fb.Img.YOrigin] != 77 {
		t.Fatalf("YFull origin does not alias visible Y")
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
	for y := 0; y < fb.Img.CodedHeight; y++ {
		for x := 0; x < fb.Img.CodedWidth; x++ {
			fb.Img.Y[y*fb.Img.YStride+x] = byte(10*y + x + 1)
		}
	}
	uvWidth := (fb.Img.CodedWidth + 1) >> 1
	uvHeight := (fb.Img.CodedHeight + 1) >> 1
	for y := range uvHeight {
		for x := range uvWidth {
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
	if yPlane[top*stride+left+fb.Img.CodedWidth] != 16 {
		t.Fatalf("right Y border = %d, want 16", yPlane[top*stride+left+fb.Img.CodedWidth])
	}
	if yPlane[(top-1)*stride+left] != 1 {
		t.Fatalf("top Y border = %d, want 1", yPlane[(top-1)*stride+left])
	}
	if yPlane[(top+fb.Img.CodedHeight)*stride+left+2] != 153 {
		t.Fatalf("bottom Y border = %d, want 153", yPlane[(top+fb.Img.CodedHeight)*stride+left+2])
	}

	uPlane := fb.buf[fb.uPlaneOff:fb.vPlaneOff]
	uvBorder := (fb.border + 1) >> 1
	if uPlane[uvBorder*fb.Img.UStride+uvBorder-1] != 40 {
		t.Fatalf("left U border = %d, want 40", uPlane[uvBorder*fb.Img.UStride+uvBorder-1])
	}
	if uPlane[(uvBorder+uvHeight)*fb.Img.UStride+uvBorder+uvWidth-1] != 117 {
		t.Fatalf("bottom U border = %d, want 117", uPlane[(uvBorder+uvHeight)*fb.Img.UStride+uvBorder+uvWidth-1])
	}
}

func TestExtendBordersPreservesCodedPadding(t *testing.T) {
	fb, err := NewFrameBuffer(5, 3, 2, 8)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	uvWidth := (fb.Img.Width + 1) >> 1
	codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
	codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
	fb.Img.Y[fb.Img.Width] = 91
	fb.Img.Y[fb.Img.CodedWidth-1] = 92
	fb.Img.Y[(fb.Img.CodedHeight-1)*fb.Img.YStride+fb.Img.CodedWidth-1] = 93
	fb.Img.U[uvWidth] = 61
	fb.Img.U[codedUVWidth-1] = 62
	fb.Img.U[(codedUVHeight-1)*fb.Img.UStride+codedUVWidth-1] = 63

	fb.ExtendBorders()

	yPlane := fb.buf[fb.yPlaneOff:fb.uPlaneOff]
	top := fb.border
	left := fb.border
	stride := fb.Img.YStride
	row0 := top * stride
	if got := yPlane[row0+left+fb.Img.Width]; got != 91 {
		t.Fatalf("first Y coded padding = %d, want preserved 91", got)
	}
	if got := yPlane[row0+left+fb.Img.CodedWidth]; got != 92 {
		t.Fatalf("first right Y border = %d, want coded edge 92", got)
	}
	bottomRow := top + fb.Img.CodedHeight + fb.border - 1
	if got := yPlane[bottomRow*stride+left+fb.Img.CodedWidth+fb.border-1]; got != 93 {
		t.Fatalf("far bottom Y border = %d, want coded edge 93", got)
	}

	uPlane := fb.buf[fb.uPlaneOff:fb.vPlaneOff]
	uvBorder := (fb.border + 1) >> 1
	uvRow0 := uvBorder * fb.Img.UStride
	if got := uPlane[uvRow0+uvBorder+uvWidth]; got != 61 {
		t.Fatalf("first U coded padding = %d, want preserved 61", got)
	}
	if got := uPlane[uvRow0+uvBorder+codedUVWidth]; got != 62 {
		t.Fatalf("first right U border = %d, want coded edge 62", got)
	}
	uvBottomRow := uvBorder + codedUVHeight + uvBorder - 1
	if got := uPlane[uvBottomRow*fb.Img.UStride+uvBorder+codedUVWidth+uvBorder-1]; got != 63 {
		t.Fatalf("far bottom U border = %d, want coded edge 63", got)
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

func BenchmarkExtendBorders(b *testing.B) {
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "smoke32", width: 32, height: 32},
		{name: "sd640x360", width: 640, height: 360},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			fb, err := NewFrameBuffer(tc.width, tc.height, 32, 32)
			if err != nil {
				b.Fatalf("NewFrameBuffer returned error: %v", err)
			}
			fillBenchmarkFrameBuffer(fb)

			b.ReportAllocs()
			b.SetBytes(int64(len(fb.buf)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				fb.ExtendBorders()
			}
		})
	}
}

func fillBenchmarkFrameBuffer(fb *FrameBuffer) {
	for y := 0; y < fb.Img.Height; y++ {
		for x := 0; x < fb.Img.Width; x++ {
			fb.Img.Y[y*fb.Img.YStride+x] = byte(x + y)
		}
	}
	uvWidth := (fb.Img.Width + 1) >> 1
	uvHeight := (fb.Img.Height + 1) >> 1
	for y := range uvHeight {
		for x := range uvWidth {
			fb.Img.U[y*fb.Img.UStride+x] = byte(85 + x + y)
			fb.Img.V[y*fb.Img.VStride+x] = byte(170 + x + y)
		}
	}
}
