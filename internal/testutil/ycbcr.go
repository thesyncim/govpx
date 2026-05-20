package testutil

import "image"

// NewYCbCr returns a full-frame 4:2:0 image filled with one Y/Cb/Cr sample.
func NewYCbCr(width, height int, y, cb, cr byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := 0; yy < height; yy++ {
		row := img.Y[yy*img.YStride:]
		for xx := 0; xx < width; xx++ {
			row[xx] = y
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for yy := 0; yy < uvHeight; yy++ {
		cbRow := img.Cb[yy*img.CStride:]
		crRow := img.Cr[yy*img.CStride:]
		for xx := 0; xx < uvWidth; xx++ {
			cbRow[xx] = cb
			crRow[xx] = cr
		}
	}
	return img
}

// NewMotionYCbCr returns deterministic textured luma content with neutral
// chroma. The pattern is stable across tests but contains enough gradients
// and edges to exercise simple motion and variance paths.
func NewMotionYCbCr(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := 0; yy < height; yy++ {
		row := img.Y[yy*img.YStride:]
		for xx := 0; xx < width; xx++ {
			row[xx] = byte((xx*3 + yy*5 + (xx/8)*17 + (yy/8)*29) & 0xff)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for yy := 0; yy < uvHeight; yy++ {
		cbRow := img.Cb[yy*img.CStride:]
		crRow := img.Cr[yy*img.CStride:]
		for xx := 0; xx < uvWidth; xx++ {
			cbRow[xx] = 128
			crRow[xx] = 128
		}
	}
	return img
}

// ShiftYCbCrCopy shifts src by dy/dx pixels into a new image. Samples that
// would read outside the source repeat the nearest edge sample.
func ShiftYCbCrCopy(src *image.YCbCr, dy, dx int) *image.YCbCr {
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	out := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := 0; yy < height; yy++ {
		for xx := 0; xx < width; xx++ {
			sy := ClampCoord(yy-dy, height)
			sx := ClampCoord(xx-dx, width)
			out.Y[yy*out.YStride+xx] = src.Y[sy*src.YStride+sx]
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for yy := 0; yy < uvHeight; yy++ {
		for xx := 0; xx < uvWidth; xx++ {
			sy := ClampCoord(yy-dy/2, uvHeight)
			sx := ClampCoord(xx-dx/2, uvWidth)
			out.Cb[yy*out.CStride+xx] = src.Cb[sy*src.CStride+sx]
			out.Cr[yy*out.CStride+xx] = src.Cr[sy*src.CStride+sx]
		}
	}
	return out
}

// AppendYCbCrI420 appends the visible I420 planes from img, ignoring stride
// padding.
func AppendYCbCrI420(out []byte, img *image.YCbCr) []byte {
	width := img.Rect.Dx()
	height := img.Rect.Dy()
	return AppendI420Planes(out, width, height,
		img.Y, img.YStride,
		img.Cb, img.CStride,
		img.Cr, img.CStride)
}

// AppendI420Planes appends visible Y, U, and V samples from strided 4:2:0
// planes.
func AppendI420Planes(out []byte, width int, height int,
	y []byte, yStride int,
	u []byte, uStride int,
	v []byte, vStride int,
) []byte {
	for row := range height {
		start := row * yStride
		out = append(out, y[start:start+width]...)
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		start := row * uStride
		out = append(out, u[start:start+uvWidth]...)
	}
	for row := range uvHeight {
		start := row * vStride
		out = append(out, v[start:start+uvWidth]...)
	}
	return out
}

// ClampCoord clamps a sample coordinate into [0, limit).
func ClampCoord(v, limit int) int {
	switch {
	case limit <= 0:
		return 0
	case v < 0:
		return 0
	case v >= limit:
		return limit - 1
	default:
		return v
	}
}
