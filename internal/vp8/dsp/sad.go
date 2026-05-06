package dsp

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c scalar SAD primitives.

func SAD16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlock(src, srcStride, ref, refStride, 16, 16)
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
