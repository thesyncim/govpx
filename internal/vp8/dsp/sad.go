package dsp

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c scalar SAD primitives.

func SAD16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 16, 16)
}

func SAD16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	return sadBlock16x16Limit(src, srcStride, ref, refStride, limit)
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
	return sadBlockScalarFallback(src, srcStride, ref, refStride, width, height)
}

func sadBlockScalarFallback(src []byte, srcStride int, ref []byte, refStride int, width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	_ = src[(height-1)*srcStride+(width-1)]
	_ = ref[(height-1)*refStride+(width-1)]
	sad := 0
	for y := range height {
		srcRow := src[y*srcStride : y*srcStride+width]
		refRow := ref[y*refStride : y*refStride+width]
		for x := range width {
			diff := int(srcRow[x]) - int(refRow[x])
			mask := diff >> signShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}
