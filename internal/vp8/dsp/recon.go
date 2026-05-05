package dsp

// Ported from libvpx v1.16.0 vp8/common/reconinter.c copy helpers.

func Copy4x4(src []byte, srcStride int, dst []byte, dstStride int) {
	copyBlock(src, srcStride, dst, dstStride, 4, 4)
}

func Copy8x4(src []byte, srcStride int, dst []byte, dstStride int) {
	copyBlock(src, srcStride, dst, dstStride, 8, 4)
}

func Copy8x8(src []byte, srcStride int, dst []byte, dstStride int) {
	copyBlock(src, srcStride, dst, dstStride, 8, 8)
}

func Copy16x16(src []byte, srcStride int, dst []byte, dstStride int) {
	copyBlock(src, srcStride, dst, dstStride, 16, 16)
}

func AddResidual4x4(dst []byte, dstStride int, residual *[16]int16) {
	for y := 0; y < 4; y++ {
		row := y * dstStride
		coeff := y * 4
		dst[row+0] = ClipPixelAdd(dst[row+0], int(residual[coeff+0]))
		dst[row+1] = ClipPixelAdd(dst[row+1], int(residual[coeff+1]))
		dst[row+2] = ClipPixelAdd(dst[row+2], int(residual[coeff+2]))
		dst[row+3] = ClipPixelAdd(dst[row+3], int(residual[coeff+3]))
	}
}

func copyBlock(src []byte, srcStride int, dst []byte, dstStride int, width int, height int) {
	for y := 0; y < height; y++ {
		copy(dst[y*dstStride:y*dstStride+width], src[y*srcStride:y*srcStride+width])
	}
}
