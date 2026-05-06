package dsp

// Ported from libvpx v1.16.0 vp8/encoder/variance.c scalar variance
// primitives.

func SSE16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 16, 16)
	return sse
}

func SSE8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 8, 8)
	return sse
}

func SSE4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 4, 4)
	return sse
}

func Variance16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 16, 16)
	return sse - (sum * sum >> 8)
}

func Variance8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 8, 8)
	return sse - (sum * sum >> 6)
}

func Variance4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 4, 4)
	return sse - (sum * sum >> 4)
}

func varianceBlock(src []byte, srcStride int, ref []byte, refStride int, width int, height int) (int, int) {
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := 0; x < width; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}
