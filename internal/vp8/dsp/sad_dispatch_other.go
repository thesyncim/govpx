//go:build !arm64

package dsp

// Pure-Go fallback for the 16x16 SAD primitives on architectures
// without a NEON port. Mirrors libvpx v1.16.0 vpx_dsp/sad.c semantics.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	sad := 0
	for y := 0; y < 16; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := 0; x < 16; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	sad := 0
	for y := 0; y < 16; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := 0; x < 16; x++ {
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
