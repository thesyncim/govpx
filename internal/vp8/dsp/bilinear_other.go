//go:build (!arm64 && !amd64) || purego

package dsp

// Pure-Go fallback for BilinearPredict on architectures without a SIMD
// port. Returns false so the generic libvpx v1.16.0 scalar path in
// subpixel.go runs.

func bilinearPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	return false
}

func bilinearPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	return false
}
