//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for the SAD primitives on architectures without a SIMD
// port. Mirrors libvpx v1.16.0 vpx_dsp/sad.c semantics.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 16, 16)
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	return sadBlockLimitScalar(src, srcStride, ref, refStride, 16, 16, limit)
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 16, 8)
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 8, 16)
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 8, 8)
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 4, 4)
}

func sadBlockScalar(src []byte, srcStride int, ref []byte, refStride int, width, height int) int {
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

func sadBlockLimitScalar(src []byte, srcStride int, ref []byte, refStride int, width, height, limit int) int {
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
