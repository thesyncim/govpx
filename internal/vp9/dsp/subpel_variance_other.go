package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Size-specialized subpixel variance dispatchers share Go SIMD and scalar
// implementations through the pixel package.

func subPixelVariance64x64(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(64, 64, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance64x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(64, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance32x64(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(32, 64, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance32x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(32, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance32x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(32, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance16x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(16, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance16x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(16, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance16x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(16, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance8x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(8, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance8x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(8, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance8x4(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(8, 4, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance4x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(4, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance4x4(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVarianceGo(4, 4, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}

func subPixelVarianceGo(w, h int, src []byte, so, ss, x, y int, ref []byte, ro, rs int, sse *uint32) uint32 {
	if !pixel.Enabled {
		return subPixelVarianceScalar(w, h, src, so, ss, x, y, ref, ro, rs, sse)
	}
	sum, se := pixel.SubpelStats(src[so:], ss, ref[ro:], rs, w, h, vp9BilinearFilters[x], vp9BilinearFilters[y])
	*sse = se
	return se - uint32(int64(sum)*int64(sum)/int64(w*h))
}
