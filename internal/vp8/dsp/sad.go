package dsp

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c scalar SAD primitives.

func SAD16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 16, 16)
}

func SAD16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	return sadBlockLimit(src, srcStride, ref, refStride, 16, 16, limit)
}

func SAD16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 16, 8)
}

func SAD8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 8, 16)
}

func SAD8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 8, 8)
}

func SAD4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 4, 4)
}

func sadBlock(src []byte, srcStride int, ref []byte, refStride int, width int, height int) int {
	if width == 16 && height == 16 {
		return sadBlock16x16(src, srcStride, ref, refStride)
	}
	if width == 16 && height == 8 {
		return sadBlock16x8(src, srcStride, ref, refStride)
	}
	if width == 8 && height == 16 {
		return sadBlock8x16(src, srcStride, ref, refStride)
	}
	if width == 8 && height == 8 {
		return sadBlock8x8(src, srcStride, ref, refStride)
	}
	if width == 4 && height == 4 {
		return sadBlock4x4(src, srcStride, ref, refStride)
	}
	sad := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := 0; x < width; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func sadBlockLimit(src []byte, srcStride int, ref []byte, refStride int, width int, height int, limit int) int {
	if width == 16 && height == 16 {
		return sadBlock16x16Limit(src, srcStride, ref, refStride, limit)
	}
	sad := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := 0; x < width; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}
