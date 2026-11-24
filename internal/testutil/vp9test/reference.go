package vp9test

import "image"

// ShiftedI420 builds an I420 source by sampling another I420 image with a
// clamped whole-pixel offset.
func ShiftedI420(width int, height int,
	y, u, v []byte, yStride, uStride, vStride int, dx, dy int,
) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	shiftPlane(img.Y, img.YStride, y, yStride, width, height, dx, dy)
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	shiftPlane(img.Cb, img.CStride, u, uStride, uvWidth, uvHeight, dx>>1, dy>>1)
	shiftPlane(img.Cr, img.CStride, v, vStride, uvWidth, uvHeight, dx>>1, dy>>1)
	return img
}

// SplitXShiftedI420 applies one clamped horizontal offset on the left half of
// each plane and another on the right half.
func SplitXShiftedI420(width int, height int,
	y, u, v []byte, yStride, uStride, vStride int, leftDx, rightDx int,
) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	splitXShiftPlane(img.Y, img.YStride, y, yStride, width, height, leftDx, rightDx)
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	splitXShiftPlane(img.Cb, img.CStride, u, uStride, uvWidth, uvHeight, leftDx>>1, rightDx>>1)
	splitXShiftPlane(img.Cr, img.CStride, v, vStride, uvWidth, uvHeight, leftDx>>1, rightDx>>1)
	return img
}

// SplitYShiftedI420 applies one clamped vertical offset on the top half of
// each plane and another on the bottom half.
func SplitYShiftedI420(width int, height int,
	y, u, v []byte, yStride, uStride, vStride int, topDy, bottomDy int,
) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	splitYShiftPlane(img.Y, img.YStride, y, yStride, width, height, topDy, bottomDy)
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	splitYShiftPlane(img.Cb, img.CStride, u, uStride, uvWidth, uvHeight, topDy>>1, bottomDy>>1)
	splitYShiftPlane(img.Cr, img.CStride, v, vStride, uvWidth, uvHeight, topDy>>1, bottomDy>>1)
	return img
}

// QuadrantShiftedI420 samples each image quadrant with a separate clamped
// whole-pixel offset.
func QuadrantShiftedI420(width int, height int,
	y, u, v []byte, yStride, uStride, vStride int,
	topLeft, topRight, bottomLeft, bottomRight image.Point,
) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	quadrantShiftPlane(img.Y, img.YStride, y, yStride, width, height,
		topLeft, topRight, bottomLeft, bottomRight)
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	uvTopLeft := image.Point{X: topLeft.X >> 1, Y: topLeft.Y >> 1}
	uvTopRight := image.Point{X: topRight.X >> 1, Y: topRight.Y >> 1}
	uvBottomLeft := image.Point{X: bottomLeft.X >> 1, Y: bottomLeft.Y >> 1}
	uvBottomRight := image.Point{X: bottomRight.X >> 1, Y: bottomRight.Y >> 1}
	quadrantShiftPlane(img.Cb, img.CStride, u, uStride, uvWidth, uvHeight,
		uvTopLeft, uvTopRight, uvBottomLeft, uvBottomRight)
	quadrantShiftPlane(img.Cr, img.CStride, v, vStride, uvWidth, uvHeight,
		uvTopLeft, uvTopRight, uvBottomLeft, uvBottomRight)
	return img
}

func shiftPlane(dst []byte, dstStride int, src []byte, srcStride, width, height, dx, dy int) {
	for yy := range height {
		dstRow := dst[yy*dstStride:]
		srcY := clampInt(yy+dy, 0, height-1)
		srcRow := src[srcY*srcStride:]
		for xx := range width {
			srcX := clampInt(xx+dx, 0, width-1)
			dstRow[xx] = srcRow[srcX]
		}
	}
}

func splitXShiftPlane(dst []byte, dstStride int, src []byte, srcStride, width, height, leftDx, rightDx int) {
	splitX := width / 2
	for yy := range height {
		dstRow := dst[yy*dstStride:]
		srcRow := src[yy*srcStride:]
		for xx := range width {
			dx := leftDx
			if xx >= splitX {
				dx = rightDx
			}
			srcX := clampInt(xx+dx, 0, width-1)
			dstRow[xx] = srcRow[srcX]
		}
	}
}

func splitYShiftPlane(dst []byte, dstStride int, src []byte, srcStride, width, height, topDy, bottomDy int) {
	splitY := height / 2
	for yy := range height {
		dy := topDy
		if yy >= splitY {
			dy = bottomDy
		}
		srcY := clampInt(yy+dy, 0, height-1)
		dstRow := dst[yy*dstStride:]
		srcRow := src[srcY*srcStride:]
		copy(dstRow[:width], srcRow[:width])
	}
}

func quadrantShiftPlane(dst []byte, dstStride int, src []byte, srcStride, width, height int,
	topLeft, topRight, bottomLeft, bottomRight image.Point,
) {
	splitX := width / 2
	splitY := height / 2
	for yy := range height {
		dstRow := dst[yy*dstStride:]
		for xx := range width {
			shift := topLeft
			if yy >= splitY {
				shift = bottomLeft
				if xx >= splitX {
					shift = bottomRight
				}
			} else if xx >= splitX {
				shift = topRight
			}
			srcX := clampInt(xx+shift.X, 0, width-1)
			srcY := clampInt(yy+shift.Y, 0, height-1)
			dstRow[xx] = src[srcY*srcStride+srcX]
		}
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
