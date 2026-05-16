//go:build (!amd64 && !arm64) || purego

package dsp

// Scalar size-specialized variance helpers. Architectures with SIMD
// support override these via variance_amd64.go / variance_arm64.go.

// varWindowOK, finalVariance, varianceSimd* are defined in the SIMD
// variants; the scalar build doesn't need them. The size-specialized
// helpers below shadow the SIMD-build versions when the platform isn't
// supported (or purego is requested).

func variance64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(64, 64, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(64, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(32, 64, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(32, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(32, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(16, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(16, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(16, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(8, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(8, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(8, 4, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(4, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return varianceScalar(4, 4, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
