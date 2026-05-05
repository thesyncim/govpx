package common

// ExtendBorders replicates visible edge samples into the frame border.
func (fb *FrameBuffer) ExtendBorders() {
	if fb == nil || fb.border == 0 {
		return
	}
	extendPlane(
		fb.buf[fb.yPlaneOff:fb.uPlaneOff],
		fb.Img.YStride,
		fb.Img.Width,
		fb.Img.Height,
		fb.border,
		fb.border,
		fb.border,
		fb.border,
	)

	uvBorder := (fb.border + 1) >> 1
	uvWidth := (fb.Img.Width + 1) >> 1
	uvHeight := (fb.Img.Height + 1) >> 1
	extendPlane(
		fb.buf[fb.uPlaneOff:fb.vPlaneOff],
		fb.Img.UStride,
		uvWidth,
		uvHeight,
		uvBorder,
		uvBorder,
		uvBorder,
		uvBorder,
	)
	extendPlane(
		fb.buf[fb.vPlaneOff:],
		fb.Img.VStride,
		uvWidth,
		uvHeight,
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

	for y := 0; y < height; y++ {
		row := plane[(top+y)*stride:]
		first := row[left]
		last := row[left+width-1]
		for x := 0; x < left; x++ {
			row[x] = first
		}
		for x := 0; x < right; x++ {
			row[left+width+x] = last
		}
	}

	rowWidth := left + width + right
	firstRow := plane[top*stride : top*stride+rowWidth]
	for y := 0; y < top; y++ {
		copy(plane[y*stride:y*stride+rowWidth], firstRow)
	}

	lastRowStart := (top + height - 1) * stride
	lastRow := plane[lastRowStart : lastRowStart+rowWidth]
	for y := 0; y < bottom; y++ {
		dstStart := (top + height + y) * stride
		copy(plane[dstStart:dstStart+rowWidth], lastRow)
	}
}
