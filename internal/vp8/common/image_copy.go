package common

// VP8 frame copy helpers mirror libvpx v1.16.0 VP8 YV12_BUFFER_CONFIG image
// ownership: references copy visible/coded planes, while decoder refresh copies
// the full bordered backing planes.

// CopyExtendedImage copies the full bordered VP8 image planes.
func CopyExtendedImage(dst *Image, src *Image) {
	copy(dst.YFull, src.YFull)
	copy(dst.UFull, src.UFull)
	copy(dst.VFull, src.VFull)
}

// CopyImage copies the visible/coded VP8 image planes without borders.
func CopyImage(dst *Image, src *Image) {
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)
}

// CopyImageLuma copies the overlapping coded luma region of two VP8 images.
func CopyImageLuma(dst *Image, src *Image) {
	if dst == nil || src == nil {
		return
	}
	width := min(dst.CodedWidth, src.CodedWidth)
	height := min(dst.CodedHeight, src.CodedHeight)
	if min(width, height) <= 0 {
		return
	}
	if dst.YStride == src.YStride && width == dst.YStride {
		copy(dst.Y[:height*dst.YStride], src.Y[:height*src.YStride])
		return
	}
	for row := range height {
		copy(dst.Y[row*dst.YStride:row*dst.YStride+width], src.Y[row*src.YStride:row*src.YStride+width])
	}
}
