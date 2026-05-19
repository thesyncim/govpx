package common

import "unsafe"

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
	alignedWidth := Align(width, 8)
	alignedHeight := Align(height, 8)
	yStride := Align(alignedWidth+2*border, 32)
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
		yOrigin = Align(yOrigin, byteAlignment)
		uvOrigin = Align(uvOrigin, byteAlignment)
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
	layout.YOrigin = AlignOffsetForSlice(yFull, layout.YOrigin, byteAlignment)
	layout.UOrigin = AlignOffsetForSlice(uFull, layout.UOrigin, byteAlignment)
	layout.VOrigin = AlignOffsetForSlice(vFull, layout.VOrigin, byteAlignment)
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

// ByteSliceAligned reports whether buf starts on an align-byte boundary.
func ByteSliceAligned(buf []byte, align int) bool {
	if align <= 1 || len(buf) == 0 {
		return true
	}
	return uintptr(unsafe.Pointer(&buf[0]))%uintptr(align) == 0
}

// AlignmentPadding returns the prefix needed to align buf to align bytes.
func AlignmentPadding(buf []byte, align int) int {
	if align <= 1 || len(buf) == 0 {
		return 0
	}
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	rem := ptr % uintptr(align)
	if rem == 0 {
		return 0
	}
	return int(uintptr(align) - rem)
}

// AlignOffsetForSlice returns off rounded up so &buf[off] has align-byte
// alignment.
func AlignOffsetForSlice(buf []byte, off, align int) int {
	if align <= 1 || len(buf) == 0 {
		return off
	}
	ptr := uintptr(unsafe.Pointer(&buf[0])) + uintptr(off)
	rem := ptr % uintptr(align)
	if rem == 0 {
		return off
	}
	return off + int(uintptr(align)-rem)
}

// Align rounds v up to an align-byte boundary.
func Align(v, align int) int {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}
