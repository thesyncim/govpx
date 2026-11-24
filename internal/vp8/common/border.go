package common

import "encoding/binary"

// Ported from libvpx v1.16.0 vpx_scale/generic/yv12extend.c.

// ExtendBorders replicates coded edge samples into the frame border.
func (fb *FrameBuffer) ExtendBorders() {
	if fb == nil {
		return
	}
	extendPlane(
		fb.buf[fb.yPlaneOff:fb.uPlaneOff],
		fb.Img.YStride,
		fb.Img.CodedWidth,
		fb.Img.CodedHeight,
		fb.border,
		fb.border,
		fb.border,
		fb.border,
	)

	uvBorder := (fb.border + 1) >> 1
	codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
	codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
	extendPlane(
		fb.buf[fb.uPlaneOff:fb.vPlaneOff],
		fb.Img.UStride,
		codedUVWidth,
		codedUVHeight,
		uvBorder,
		uvBorder,
		uvBorder,
		uvBorder,
	)
	extendPlane(
		fb.buf[fb.vPlaneOff:],
		fb.Img.VStride,
		codedUVWidth,
		codedUVHeight,
		uvBorder,
		uvBorder,
		uvBorder,
		uvBorder,
	)
}

func extendPlane(plane []byte, stride int, width int, height int, left int, right int, top int, bottom int) {
	if width <= 0 || height <= 0 {
		return
	}

	for y := range height {
		row := plane[(top+y)*stride:]
		first := row[left]
		last := row[left+width-1]
		if left > 0 {
			fillPlaneBorder(row[:left], first)
		}
		if right > 0 {
			fillPlaneBorder(row[left+width:left+width+right], last)
		}
	}

	rowWidth := left + width + right
	firstRow := plane[top*stride : top*stride+rowWidth]
	for y := range top {
		copy(plane[y*stride:y*stride+rowWidth], firstRow)
	}

	lastRowStart := (top + height - 1) * stride
	lastRow := plane[lastRowStart : lastRowStart+rowWidth]
	for y := range bottom {
		dstStart := (top + height + y) * stride
		copy(plane[dstStart:dstStart+rowWidth], lastRow)
	}
}

func fillPlaneBorder(buf []byte, value byte) {
	switch len(buf) {
	case 16:
		_ = buf[15]
		word := uint64(value) * 0x0101010101010101
		binary.LittleEndian.PutUint64(buf[0:8], word)
		binary.LittleEndian.PutUint64(buf[8:16], word)
	case 32:
		_ = buf[31]
		word := uint64(value) * 0x0101010101010101
		binary.LittleEndian.PutUint64(buf[0:8], word)
		binary.LittleEndian.PutUint64(buf[8:16], word)
		binary.LittleEndian.PutUint64(buf[16:24], word)
		binary.LittleEndian.PutUint64(buf[24:32], word)
	default:
		buf[0] = value
		for n := 1; n < len(buf); n *= 2 {
			copy(buf[n:], buf[:n])
		}
	}
}
