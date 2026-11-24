package common

import "github.com/thesyncim/govpx/internal/vpx/buffers"

// FrameLayout describes the padded VP9 4:2:0 frame buffer layout.
type FrameLayout struct {
	YStride   int
	UVStride  int
	YWidth    int
	YHeight   int
	UVWidth   int
	UVHeight  int
	YOrigin   int
	UVOrigin  int
	UOrigin   int
	VOrigin   int
	YFullLen  int
	UVFullLen int
}

// NewFrameLayout returns the default internal VP9 frame buffer layout.
func NewFrameLayout(width, height int) FrameLayout {
	return NewDecoderFrameLayout(width, height, 0)
}

// NewDecoderFrameLayout returns the padded VP9 decoder frame buffer layout.
func NewDecoderFrameLayout(width, height, byteAlignment int) FrameLayout {
	const border = 32 // VP9_DEC_BORDER_IN_PIXELS in libvpx vpx_scale/yv12config.h.
	alignedWidth := buffers.Align(width, 8)
	alignedHeight := buffers.Align(height, 8)
	yStride := buffers.Align(alignedWidth+2*border, 32)
	uvWidth := alignedWidth >> 1
	uvHeight := alignedHeight >> 1
	uvStride := yStride >> 1
	uvBorder := border >> 1
	yOrigin := border*yStride + border
	uvOrigin := uvBorder*uvStride + uvBorder
	uOrigin := uvOrigin
	vOrigin := uvOrigin
	extraAlignment := 0
	if byteAlignment > 0 {
		yOrigin = buffers.Align(yOrigin, byteAlignment)
		uvOrigin = buffers.Align(uvOrigin, byteAlignment)
		extraAlignment = byteAlignment
		uOrigin = uvOrigin
		vOrigin = uvOrigin
	}
	return FrameLayout{
		YStride:   yStride,
		UVStride:  uvStride,
		YWidth:    alignedWidth,
		YHeight:   alignedHeight,
		UVWidth:   uvWidth,
		UVHeight:  uvHeight,
		YOrigin:   yOrigin,
		UVOrigin:  uvOrigin,
		UOrigin:   uOrigin,
		VOrigin:   vOrigin,
		YFullLen:  yStride*(alignedHeight+2*border) + extraAlignment,
		UVFullLen: uvStride*(uvHeight+2*uvBorder) + extraAlignment,
	}
}

// NewDecoderFrameLayoutForPlanes returns a VP9 frame buffer layout adjusted to
// the actual backing slice addresses.
func NewDecoderFrameLayoutForPlanes(width, height, byteAlignment int,
	yFull, uFull, vFull []byte,
) FrameLayout {
	layout := NewDecoderFrameLayout(width, height, byteAlignment)
	if byteAlignment <= 0 {
		return layout
	}
	layout.YOrigin = buffers.AlignOffsetForSlice(yFull, layout.YOrigin, byteAlignment)
	layout.UOrigin = buffers.AlignOffsetForSlice(uFull, layout.UOrigin, byteAlignment)
	layout.VOrigin = buffers.AlignOffsetForSlice(vFull, layout.VOrigin, byteAlignment)
	layout.UVOrigin = layout.UOrigin
	return layout
}

// DecoderFrameAlignment returns the effective VP9 decoder plane alignment.
func DecoderFrameAlignment(byteAlignment int) int {
	if byteAlignment > 0 {
		return byteAlignment
	}
	return 32
}
