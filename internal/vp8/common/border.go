package common

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
		// Memset-style fills using the doubling-copy idiom: write the
		// edge byte once and grow the filled region exponentially via
		// copy(), which dispatches to runtime memmove. Cuts the per-row
		// border population from N stores to log2(N) memmoves.
		if left > 0 {
			leftBuf := row[:left]
			leftBuf[0] = first
			for n := 1; n < left; n *= 2 {
				copy(leftBuf[n:], leftBuf[:n])
			}
		}
		if right > 0 {
			rightBuf := row[left+width : left+width+right]
			rightBuf[0] = last
			for n := 1; n < right; n *= 2 {
				copy(rightBuf[n:], rightBuf[:n])
			}
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
