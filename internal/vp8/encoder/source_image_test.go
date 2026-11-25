package encoder

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
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

func TestCopySourceToFrameBufferAllocatesZero(t *testing.T) {
	const width, height = 64, 48
	src := testSourceImage(width, height, 10, 80, 160)
	var dst vp8common.FrameBuffer
	if err := dst.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	CopySourceToFrameBuffer(&dst, src)

	allocs := testing.AllocsPerRun(1000, func() {
		CopySourceToFrameBuffer(&dst, src)
	})
	if allocs != 0 {
		t.Fatalf("CopySourceToFrameBuffer allocs = %v, want 0", allocs)
	}
}

func TestCopySourceToFrameBufferActiveAllocatesZero(t *testing.T) {
	const width, height = 64, 48
	src := testSourceImage(width, height, 200, 100, 50)
	var dst vp8common.FrameBuffer
	if err := dst.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	activeMap := []uint8{
		1, 0, 1, 1,
		0, 1, 0, 1,
		1, 1, 0, 0,
	}
	CopySourceToFrameBufferActive(&dst, src, activeMap, 3, 4)

	allocs := testing.AllocsPerRun(1000, func() {
		CopySourceToFrameBufferActive(&dst, src, activeMap, 3, 4)
	})
	if allocs != 0 {
		t.Fatalf("CopySourceToFrameBufferActive allocs = %v, want 0", allocs)
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

func TestSourceImageFromImageUsesVisibleGeometry(t *testing.T) {
	img := vp8common.Image{
		Width:       17,
		Height:      15,
		CodedWidth:  32,
		CodedHeight: 16,
		YStride:     40,
		UStride:     20,
		VStride:     20,
		Y:           make([]byte, 40*16),
		U:           make([]byte, 20*8),
		V:           make([]byte, 20*8),
	}

	visible := SourceImageFromImage(&img)
	if visible.Width != 17 || visible.Height != 15 {
		t.Fatalf("visible geometry = %dx%d, want 17x15", visible.Width, visible.Height)
	}
	if visible.UVWidth != 9 || visible.UVHeight != 8 {
		t.Fatalf("visible UV geometry = %dx%d, want 9x8", visible.UVWidth, visible.UVHeight)
	}

	coded := CodedSourceImageFromImage(&img)
	if coded.Width != 32 || coded.Height != 16 {
		t.Fatalf("coded geometry = %dx%d, want 32x16", coded.Width, coded.Height)
	}
	if coded.UVWidth != 9 || coded.UVHeight != 8 {
		t.Fatalf("coded UV geometry = %dx%d, want visible-derived 9x8", coded.UVWidth, coded.UVHeight)
	}
}

func TestSourceImageMatchesReferenceVisibleSamplesOnly(t *testing.T) {
	ref := vp8common.Image{
		Width:   4,
		Height:  2,
		YStride: 8,
		UStride: 4,
		VStride: 4,
		Y:       make([]byte, 16),
		U:       make([]byte, 4),
		V:       make([]byte, 4),
	}
	src := SourceImage{
		Width:   4,
		Height:  2,
		YStride: 6,
		UStride: 3,
		VStride: 3,
		Y:       make([]byte, 12),
		U:       make([]byte, 3),
		V:       make([]byte, 3),
	}

	fillVisiblePlane(ref.Y, ref.YStride, src.Y, src.YStride, 4, 2, 10)
	fillVisiblePlane(ref.U, ref.UStride, src.U, src.UStride, 2, 1, 80)
	fillVisiblePlane(ref.V, ref.VStride, src.V, src.VStride, 2, 1, 120)
	src.Y[5] = 201
	ref.Y[7] = 202
	src.U[2] = 203
	ref.U[3] = 204
	if !SourceImageMatchesReference(src, &ref) {
		t.Fatalf("SourceImageMatchesReference = false, want true for matching visible samples")
	}

	src.V[1] ^= 1
	if SourceImageMatchesReference(src, &ref) {
		t.Fatalf("SourceImageMatchesReference = true after visible chroma mismatch")
	}
}

func TestSourceImageMatchesReferenceRejectsInvalidPlaneBounds(t *testing.T) {
	ref := vp8common.Image{
		Width:   4,
		Height:  2,
		YStride: 4,
		UStride: 2,
		VStride: 2,
		Y:       make([]byte, 8),
		U:       make([]byte, 2),
		V:       make([]byte, 2),
	}
	src := SourceImage{
		Width:   4,
		Height:  2,
		YStride: 3,
		UStride: 2,
		VStride: 2,
		Y:       make([]byte, 8),
		U:       make([]byte, 2),
		V:       make([]byte, 2),
	}
	if SourceImageMatchesReference(src, &ref) {
		t.Fatalf("SourceImageMatchesReference = true, want false for source stride smaller than visible width")
	}

	src.YStride = 4
	src.U = src.U[:1]
	if SourceImageMatchesReference(src, &ref) {
		t.Fatalf("SourceImageMatchesReference = true, want false for truncated source chroma plane")
	}

	src.U = make([]byte, 2)
	ref.V = ref.V[:1]
	if SourceImageMatchesReference(src, &ref) {
		t.Fatalf("SourceImageMatchesReference = true, want false for truncated reference chroma plane")
	}
}

func TestGatherClampedLumaBlockEdges(t *testing.T) {
	src := SourceImage{
		Width:   4,
		Height:  3,
		YStride: 4,
		Y:       make([]byte, 4*3),
	}
	for row := range src.Height {
		for col := range src.Width {
			src.Y[row*src.YStride+col] = byte(row*10 + col)
		}
	}

	tests := []struct {
		name  string
		baseY int
		baseX int
		want  []byte
	}{
		{
			name:  "bottom edge full x",
			baseY: 1,
			baseX: 0,
			want: []byte{
				10, 11, 12, 13,
				20, 21, 22, 23,
				20, 21, 22, 23,
				20, 21, 22, 23,
			},
		},
		{
			name:  "bottom right edge",
			baseY: 1,
			baseX: 2,
			want: []byte{
				12, 13, 13, 13,
				22, 23, 23, 23,
				22, 23, 23, 23,
				22, 23, 23, 23,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]byte, 4*4)
			GatherClampedLumaBlock(src, tt.baseY, tt.baseX, 4, 4, dst, 4)
			for i, want := range tt.want {
				if dst[i] != want {
					t.Fatalf("dst[%d] = %d, want %d (dst=%v)", i, dst[i], want, dst)
				}
			}
		})
	}
}

func fillVisiblePlane(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, base byte) {
	for y := range height {
		for x := range width {
			v := base + byte(y*width+x)
			dst[y*dstStride+x] = v
			src[y*srcStride+x] = v
		}
	}
}
