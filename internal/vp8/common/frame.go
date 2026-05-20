package common

import (
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// Ported frame-buffer layout concepts from libvpx v1.16.0
// vp8/common/alloccommon.c and vpx_scale/yv12config.c.

// Image is the internal planar 8-bit 4:2:0 image view used by VP8 frame
// buffers. Width and Height are visible dimensions; CodedWidth and CodedHeight
// cover the macroblock-padded reconstruction area.
type Image struct {
	Y           []byte
	U           []byte
	V           []byte
	YFull       []byte
	UFull       []byte
	VFull       []byte
	Width       int
	Height      int
	CodedWidth  int
	CodedHeight int
	YStride     int
	UStride     int
	VStride     int
	YOrigin     int
	UOrigin     int
	VOrigin     int
	YBorder     int
	UVBorder    int
}

// FrameBuffer owns one bordered VP8 frame. Allocation happens in NewFrameBuffer
// or Resize; per-frame reuse and border extension do not allocate.
type FrameBuffer struct {
	buf       []byte
	Img       Image
	yPlaneOff int
	uPlaneOff int
	vPlaneOff int
	yOff      int
	uOff      int
	vOff      int
	yRows     int
	uRows     int
	vRows     int
	border    int
	align     int
}

func NewFrameBuffer(width int, height int, border int, align int) (*FrameBuffer, error) {
	var fb FrameBuffer
	if err := fb.Resize(width, height, border, align); err != nil {
		return nil, err
	}
	return &fb, nil
}

func (fb *FrameBuffer) Resize(width int, height int, border int, align int) error {
	layout, err := computeLayout(width, height, border, align)
	if err != nil {
		return err
	}
	if cap(fb.buf) < layout.total || fb.align != layout.align {
		fb.buf = buffers.NewAligned(layout.total, layout.align)
	} else {
		fb.buf = fb.buf[:layout.total]
	}

	fb.yPlaneOff = layout.yPlaneOff
	fb.uPlaneOff = layout.uPlaneOff
	fb.vPlaneOff = layout.vPlaneOff
	fb.yOff = layout.yOff
	fb.uOff = layout.uOff
	fb.vOff = layout.vOff
	fb.yRows = layout.yRows
	fb.uRows = layout.uRows
	fb.vRows = layout.vRows
	fb.border = border
	fb.align = layout.align

	fb.Img.Width = width
	fb.Img.Height = height
	fb.Img.CodedWidth = layout.codedWidth
	fb.Img.CodedHeight = layout.codedHeight
	fb.Img.YStride = layout.yStride
	fb.Img.UStride = layout.uStride
	fb.Img.VStride = layout.vStride
	fb.Img.YFull = fb.buf[layout.yPlaneOff:layout.uPlaneOff]
	fb.Img.UFull = fb.buf[layout.uPlaneOff:layout.vPlaneOff]
	fb.Img.VFull = fb.buf[layout.vPlaneOff:]
	fb.Img.YOrigin = layout.yOff
	fb.Img.UOrigin = layout.uOff
	fb.Img.VOrigin = layout.vOff
	fb.Img.YBorder = border
	fb.Img.UVBorder = layout.uvBorder
	fb.Img.Y = fb.buf[layout.yPlaneOff+layout.yOff : layout.yPlaneOff+layout.yOff+planeLen(layout.yStride, layout.codedHeight, layout.codedWidth)]
	fb.Img.U = fb.buf[layout.uPlaneOff+layout.uOff : layout.uPlaneOff+layout.uOff+planeLen(layout.uStride, layout.uvHeight, layout.uvWidth)]
	fb.Img.V = fb.buf[layout.vPlaneOff+layout.vOff : layout.vPlaneOff+layout.vOff+planeLen(layout.vStride, layout.uvHeight, layout.uvWidth)]
	return nil
}

func (fb *FrameBuffer) Reset() {
	if fb == nil {
		return
	}
	for i := range fb.buf {
		fb.buf[i] = 0
	}
}

func (fb *FrameBuffer) BufferLen() int {
	if fb == nil {
		return 0
	}
	return len(fb.buf)
}

func (fb *FrameBuffer) Border() int {
	if fb == nil {
		return 0
	}
	return fb.border
}

type frameLayout struct {
	yStride int
	uStride int
	vStride int

	codedWidth  int
	codedHeight int
	uvWidth     int
	uvHeight    int
	uvBorder    int

	yRows int
	uRows int
	vRows int

	yPlaneOff int
	uPlaneOff int
	vPlaneOff int

	yOff int
	uOff int
	vOff int

	total int
	align int
}

func computeLayout(width int, height int, border int, align int) (frameLayout, error) {
	if width <= 0 || height <= 0 || width > maxFrameDimension || height > maxFrameDimension || border < 0 {
		return frameLayout{}, ErrInvalidFrameSize
	}
	if align <= 0 {
		align = 1
	}
	codedWidth := buffers.RoundUp(width, 16)
	codedHeight := buffers.RoundUp(height, 16)
	uvWidth := (codedWidth + 1) >> 1
	uvHeight := (codedHeight + 1) >> 1
	uvBorder := (border + 1) >> 1

	yStride := buffers.RoundUp(codedWidth+border*2, align)
	uStride := buffers.RoundUp(uvWidth+uvBorder*2, align)
	vStride := uStride
	yRows := codedHeight + border*2
	uRows := uvHeight + uvBorder*2
	vRows := uRows

	yPlaneSize := yStride * yRows
	uPlaneSize := uStride * uRows
	vPlaneSize := vStride * vRows
	uPlaneOff := yPlaneSize
	vPlaneOff := uPlaneOff + uPlaneSize
	total := vPlaneOff + vPlaneSize

	return frameLayout{
		yStride:     yStride,
		uStride:     uStride,
		vStride:     vStride,
		codedWidth:  codedWidth,
		codedHeight: codedHeight,
		uvWidth:     uvWidth,
		uvHeight:    uvHeight,
		uvBorder:    uvBorder,
		yRows:       yRows,
		uRows:       uRows,
		vRows:       vRows,
		uPlaneOff:   uPlaneOff,
		vPlaneOff:   vPlaneOff,
		yOff:        border*yStride + border,
		uOff:        uvBorder*uStride + uvBorder,
		vOff:        uvBorder*vStride + uvBorder,
		total:       total,
		align:       align,
	}, nil
}

const maxFrameDimension = 16383

func planeLen(stride int, rows int, visibleWidth int) int {
	if rows <= 0 {
		return 0
	}
	return stride*(rows-1) + visibleWidth
}
