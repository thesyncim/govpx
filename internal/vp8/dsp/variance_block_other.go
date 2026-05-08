//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for the 16x16 variance block on architectures
// without a NEON port. Mirrors libvpx v1.16.0 vpx_dsp/variance.c
// semantics by deferring to varianceBlockGeneric16x16, which is the
// same scalar loop the size-parameterised varianceBlock uses.

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	return varianceBlockGeneric16x16(src, srcStride, ref, refStride)
}

func varianceBlockGeneric16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum := 0
	sse := 0
	for y := 0; y < 16; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := 0; x < 16; x++ {
			diff := int(srcRow[x]) - int(refRow[x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}
