package common

import "github.com/thesyncim/govpx/internal/vpx/buffers"

// FrameLayout describes the padded VP9 frame buffer layout for 4:2:0, 4:2:2,
// 4:4:0, or 4:4:4 / RGB planes.
type FrameLayout struct {
	YStride      int
	UVStride     int
	YWidth       int
	YHeight      int
	UVWidth      int
	UVHeight     int
	YOrigin      int
	UVOrigin     int
	UOrigin      int
	VOrigin      int
	YFullLen     int
	UVFullLen    int
	SubsamplingX uint8
	SubsamplingY uint8
}

// NewFrameLayout returns the default internal VP9 4:2:0 frame buffer layout.
func NewFrameLayout(width, height int) FrameLayout {
	return NewDecoderFrameLayoutSubsampled(width, height, 0, 1, 1)
}

// NewFrameLayoutSubsampled returns the internal VP9 frame buffer layout for
// explicit (ssX, ssY) chroma subsampling shifts.
func NewFrameLayoutSubsampled(width, height int, ssX, ssY uint8) FrameLayout {
	return NewDecoderFrameLayoutSubsampled(width, height, 0, ssX, ssY)
}

// NewDecoderFrameLayout returns the padded VP9 4:2:0 decoder frame buffer layout.
func NewDecoderFrameLayout(width, height, byteAlignment int) FrameLayout {
	return NewDecoderFrameLayoutSubsampled(width, height, byteAlignment, 1, 1)
}

// NewDecoderFrameLayoutSubsampled returns the padded VP9 decoder frame buffer
// layout for explicit (ssX, ssY) chroma subsampling shifts.
func NewDecoderFrameLayoutSubsampled(width, height, byteAlignment int,
	ssX, ssY uint8,
) FrameLayout {
	const border = 32 // VP9_DEC_BORDER_IN_PIXELS in libvpx vpx_scale/yv12config.h.
	if ssX > 1 {
		ssX = 1
	}
	if ssY > 1 {
		ssY = 1
	}
	alignedWidth := buffers.Align(width, 8)
	alignedHeight := buffers.Align(height, 8)
	yStride := buffers.Align(alignedWidth+2*border, 32)
	uvWidth := alignedWidth >> ssX
	uvHeight := alignedHeight >> ssY
	uvBorderX := border >> ssX
	uvBorderY := border >> ssY
	uvStride := yStride >> ssX
	yOrigin := border*yStride + border
	uvOrigin := uvBorderY*uvStride + uvBorderX
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
		YStride:      yStride,
		UVStride:     uvStride,
		YWidth:       alignedWidth,
		YHeight:      alignedHeight,
		UVWidth:      uvWidth,
		UVHeight:     uvHeight,
		YOrigin:      yOrigin,
		UVOrigin:     uvOrigin,
		UOrigin:      uOrigin,
		VOrigin:      vOrigin,
		YFullLen:     yStride*(alignedHeight+2*border) + extraAlignment,
		UVFullLen:    uvStride*(uvHeight+2*uvBorderY) + extraAlignment,
		SubsamplingX: ssX,
		SubsamplingY: ssY,
	}
}

// NewDecoderFrameLayoutForPlanes returns a VP9 4:2:0 frame buffer layout
// adjusted to the actual backing slice addresses.
func NewDecoderFrameLayoutForPlanes(width, height, byteAlignment int,
	yFull, uFull, vFull []byte,
) FrameLayout {
	return NewDecoderFrameLayoutForPlanesSubsampled(width, height, byteAlignment,
		1, 1, yFull, uFull, vFull)
}

// NewDecoderFrameLayoutForPlanesSubsampled returns a VP9 frame buffer layout
// for explicit (ssX, ssY) subsampling adjusted to the actual backing slice
// addresses.
func NewDecoderFrameLayoutForPlanesSubsampled(width, height, byteAlignment int,
	ssX, ssY uint8, yFull, uFull, vFull []byte,
) FrameLayout {
	layout := NewDecoderFrameLayoutSubsampled(width, height, byteAlignment, ssX, ssY)
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
