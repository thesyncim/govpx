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
	if xOffset == 0 && yOffset == 0 {
		return varianceScalar(w, h, src, srcOff, srcStride, ref, refOff, refStride, sse)
	}
	var sumAcc, sse0, sse1 int
	if yOffset == 0 {
		fx := vp9BilinearFilters[xOffset]
		fx0, fx1 := int(fx[0]), int(fx[1])
		si, ri := srcOff, refOff
		for range h {
			s := src[si : si+w+1]
			r := ref[ri : ri+w]
			x := 0
			for ; x+3 < w; x += 4 {
				s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
				d0 := ((s0*fx0 + s1*fx1 + 64) >> 7) - int(r[x])
				d1 := ((s1*fx0 + s2*fx1 + 64) >> 7) - int(r[x+1])
				d2 := ((s2*fx0 + s3*fx1 + 64) >> 7) - int(r[x+2])
				d3 := ((s3*fx0 + s4*fx1 + 64) >> 7) - int(r[x+3])
				sumAcc += d0 + d1 + d2 + d3
				sse0 += d0*d0 + d2*d2
				sse1 += d1*d1 + d3*d3
			}
			for ; x < w; x++ {
				d := ((int(s[x])*fx0 + int(s[x+1])*fx1 + 64) >> 7) - int(r[x])
				sumAcc += d
				sse0 += d * d
			}
			si += srcStride
			ri += refStride
		}
		totalSSE := uint32(sse0 + sse1)
		*sse = totalSSE
		return totalSSE - uint32((int64(sumAcc)*int64(sumAcc))/int64(w*h))
	}
	if xOffset == 0 {
		fy := vp9BilinearFilters[yOffset]
		fy0, fy1 := int(fy[0]), int(fy[1])
		si, ri := srcOff, refOff
		for range h {
			s0 := src[si : si+w]
			s1 := src[si+srcStride : si+srcStride+w]
			r := ref[ri : ri+w]
			x := 0
			for ; x+3 < w; x += 4 {
				d0 := ((int(s0[x])*fy0 + int(s1[x])*fy1 + 64) >> 7) - int(r[x])
				d1 := ((int(s0[x+1])*fy0 + int(s1[x+1])*fy1 + 64) >> 7) - int(r[x+1])
				d2 := ((int(s0[x+2])*fy0 + int(s1[x+2])*fy1 + 64) >> 7) - int(r[x+2])
				d3 := ((int(s0[x+3])*fy0 + int(s1[x+3])*fy1 + 64) >> 7) - int(r[x+3])
				sumAcc += d0 + d1 + d2 + d3
				sse0 += d0*d0 + d2*d2
				sse1 += d1*d1 + d3*d3
			}
			for ; x < w; x++ {
				d := ((int(s0[x])*fy0 + int(s1[x])*fy1 + 64) >> 7) - int(r[x])
				sumAcc += d
				sse0 += d * d
			}
			si += srcStride
			ri += refStride
		}
		totalSSE := uint32(sse0 + sse1)
		*sse = totalSSE
		return totalSSE - uint32((int64(sumAcc)*int64(sumAcc))/int64(w*h))
	}
	fx := vp9BilinearFilters[xOffset]
	fy := vp9BilinearFilters[yOffset]
	fx0, fx1 := int(fx[0]), int(fx[1])
	fy0, fy1 := int(fy[0]), int(fy[1])
	var prevBuf [64]uint16
	prev := prevBuf[:w]
	sFirst := src[srcOff : srcOff+w+1]
	for x := range w {
		prev[x] = uint16((int(sFirst[x])*fx0 + int(sFirst[x+1])*fx1 + 64) >> 7)
	}
	si, ri := srcOff+srcStride, refOff
	for range h {
		s := src[si : si+w+1]
		r := ref[ri : ri+w]
		x := 0
		for ; x+3 < w; x += 4 {
			s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
			b0 := (s0*fx0 + s1*fx1 + 64) >> 7
			b1 := (s1*fx0 + s2*fx1 + 64) >> 7
			b2 := (s2*fx0 + s3*fx1 + 64) >> 7
			b3 := (s3*fx0 + s4*fx1 + 64) >> 7
			a0, a1, a2, a3 := int(prev[x]), int(prev[x+1]), int(prev[x+2]), int(prev[x+3])
			prev[x], prev[x+1], prev[x+2], prev[x+3] = uint16(b0), uint16(b1), uint16(b2), uint16(b3)
			d0 := ((a0*fy0 + b0*fy1 + 64) >> 7) - int(r[x])
			d1 := ((a1*fy0 + b1*fy1 + 64) >> 7) - int(r[x+1])
			d2 := ((a2*fy0 + b2*fy1 + 64) >> 7) - int(r[x+2])
			d3 := ((a3*fy0 + b3*fy1 + 64) >> 7) - int(r[x+3])
			sumAcc += d0 + d1 + d2 + d3
			sse0 += d0*d0 + d2*d2
			sse1 += d1*d1 + d3*d3
		}
		for ; x < w; x++ {
			b := (int(s[x])*fx0 + int(s[x+1])*fx1 + 64) >> 7
			a := int(prev[x])
			prev[x] = uint16(b)
			d := ((a*fy0 + b*fy1 + 64) >> 7) - int(r[x])
			sumAcc += d
			sse0 += d * d
		}
		si += srcStride
		ri += refStride
	}
	totalSSE := uint32(sse0 + sse1)
	*sse = totalSSE
	return totalSSE - uint32((int64(sumAcc)*int64(sumAcc))/int64(w*h))
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
