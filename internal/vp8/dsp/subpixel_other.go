//go:build !arm64

package dsp

// Pure-Go fallback for SixTapPredict16x16 on architectures without a
// NEON port. Returns false so the generic libvpx v1.16.0 scalar path
// in subpixel.go runs.

func sixTapPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	return false
}
