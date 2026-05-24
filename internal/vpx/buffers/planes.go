package buffers

// CopyPlane copies the visible width by height rectangle from src into dst.
// Both planes may have larger strides than the visible width.
func CopyPlane(dst []byte, dstStride int, src []byte, srcStride int, width int, height int) {
	for row := range height {
		copy(dst[row*dstStride:row*dstStride+width], src[row*srcStride:row*srcStride+width])
	}
}

// AveragePlaneInto replaces dst with the rounded average of dst and src for
// the visible width by height rectangle. It mirrors VPx compound prediction's
// byte average: (dst + src + 1) >> 1.
func AveragePlaneInto(dst []byte, dstStride int, src []byte, srcStride int, width int, height int) {
	for row := range height {
		dstRow := dst[row*dstStride:]
		srcRow := src[row*srcStride:]
		for col := range width {
			dstRow[col] = byte((int(dstRow[col]) + int(srcRow[col]) + 1) >> 1)
		}
	}
}
