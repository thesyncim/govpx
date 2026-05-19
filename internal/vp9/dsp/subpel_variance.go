package dsp

// VP9 sub-pixel variance kernels. Ported from libvpx v1.16.0
// vpx_dsp/variance.c SUBPIX_VAR macro.
//
// The sub-pixel position is given by (xOffset, yOffset) in [0, 7].
// libvpx uses a 2-tap bilinear pre-filter (separate from VP8's, with
// a wider 128/128 weight range and 8 phases instead of VP8's 7):
//
//   bilinear_filters[8][2] = {
//     {128, 0}, {112, 16}, {96, 32}, {80, 48},
//     {64, 64}, {48, 80},  {32, 96}, {16, 112},
//   }
//
// First pass (horizontal) takes src[y*stride + x] and src[y*stride + x + 1],
// blends them by the x-filter, rounds by FilterBits (=7), keeping the
// uint16 result for full precision into the next pass. Second pass
// (vertical) takes the intermediate row and the row beneath, blends
// by the y-filter and rounds back down to uint8. The final block is
// then handed to the matching variance kernel against the reference.

// vp9BilinearFilters mirrors libvpx's bilinear_filters[8][2] in
// vpx_dsp/variance.c. The values are positive uint8 so we store them
// as int16 to interoperate with the existing variance helpers.
var vp9BilinearFilters = [8][2]int16{
	{128, 0},
	{112, 16},
	{96, 32},
	{80, 48},
	{64, 64},
	{48, 80},
	{32, 96},
	{16, 112},
}

// subpelFilterBits is the bilinear-filter rounding shift (libvpx
// FILTER_BITS = 7).
const subpelFilterBits = 7

// varFilterBlock2DBilFirstPass mirrors libvpx's
// var_filter_block2d_bil_first_pass: 2-tap blend along the row axis,
// producing uint16 output to preserve precision for pass 2.
func varFilterBlock2DBilFirstPass(src []uint8, srcOff, srcStride int,
	dst []uint16, height, width int, filter [2]int16,
) {
	f0 := int32(filter[0])
	f1 := int32(filter[1])
	for y := range height {
		srcRow := srcOff + y*srcStride
		dstRow := y * width
		for x := range width {
			v := int32(src[srcRow+x])*f0 + int32(src[srcRow+x+1])*f1
			dst[dstRow+x] = uint16(roundPowerOfTwo(v, subpelFilterBits))
		}
	}
}

// varFilterBlock2DBilSecondPass mirrors libvpx's
// var_filter_block2d_bil_second_pass. pixelStep is `width` for the
// vertical pass since libvpx packs intermediate rows tightly.
func varFilterBlock2DBilSecondPass(src []uint16, dst []uint8, dstOff,
	height, width int, filter [2]int16,
) {
	f0 := int32(filter[0])
	f1 := int32(filter[1])
	for y := range height {
		srcRow := y * width
		dstRow := dstOff + y*width
		for x := range width {
			v := int32(src[srcRow+x])*f0 + int32(src[srcRow+x+width])*f1
			dst[dstRow+x] = uint8(roundPowerOfTwo(v, subpelFilterBits))
		}
	}
}

// subPixelVarianceScalar is the parametric reference subpel variance
// helper used by VpxSubPixelVariance{W}x{H} fallbacks and tests.
func subPixelVarianceScalar(w, h int,
	src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	var fdata [65 * 64]uint16
	var temp [64 * 64]uint8
	fdataBlock := fdata[:(h+1)*w]
	tempBlock := temp[:h*w]
	varFilterBlock2DBilFirstPass(src, srcOff, srcStride, fdataBlock, h+1, w,
		vp9BilinearFilters[xOffset])
	varFilterBlock2DBilSecondPass(fdataBlock, tempBlock, 0, h, w,
		vp9BilinearFilters[yOffset])
	return varianceScalar(w, h, tempBlock, 0, w, ref, refOff, refStride, sse)
}

// VpxSubPixelVariance{W}x{H} mirror libvpx's vpx_sub_pixel_variance{W}x{H}.
// Each delegates to a size-specialized helper so per-arch SIMD backends
// can override the hot paths and the rest stays on the scalar reference.

func VpxSubPixelVariance64x64(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance64x64(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance64x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance64x32(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance32x64(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance32x64(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance32x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance32x32(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance32x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance32x16(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance16x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance16x32(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance16x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance16x16(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance16x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance16x8(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance8x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance8x16(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance8x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance8x8(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance8x4(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance8x4(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance4x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance4x8(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func VpxSubPixelVariance4x4(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return subPixelVariance4x4(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
