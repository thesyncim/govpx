//go:build arm64 && !purego

package dsp

import "unsafe"

// VP9 ARMv8 NEON sub-pixel variance kernels. Each public
// VpxSubPixelVariance{W}x{H} call routes through subPixelVarianceSimd
// which:
//   1. Validates the read-window for src/ref (subpel reads a (h+1)xW
//      window from src so the bilinear tap pair stays in-bounds).
//   2. Runs the bilinear first pass (horizontal blend) into a stack
//      scratch buffer using subpelVarFilter{4,8,16}NEON or the
//      16-chunks variant for wider blocks.
//   3. Runs the bilinear second pass (vertical blend) on the scratch
//      to produce a tightly packed WxH uint8 block.
//   4. Calls the existing varianceBlock NEON kernel on the temp block
//      vs the reference.
//
// libvpx scales the bilinear filter from {128 - 16k, 16k} (FILTER_BITS=7)
// down to {8 - k, k} with shift=3 so the entire pipeline fits in uint8.
// The two filter constants are packed into uint64 byte-lane duplicates
// by the caller.

//go:noescape
func subpelVarFilter4NEON(src *byte, srcStride int, dst *byte, pixelStep int, height int, f0 uint64, f1 uint64)

//go:noescape
func subpelVarFilter8NEON(src *byte, srcStride int, dst *byte, pixelStep int, height int, f0 uint64, f1 uint64)

//go:noescape
func subpelVarFilter16NEON(src *byte, srcStride int, dst *byte, pixelStep int, height int, f0 uint64, f1 uint64)

//go:noescape
func subpelVarFilter16ChunksNEON(src *byte, srcStride int, dst *byte, pixelStep int, width int, height int, f0 uint64, f1 uint64)

//go:noescape
func subpelVarAvg8NEON(src *byte, srcStride int, dst *byte, pixelStep int, height int)

//go:noescape
func subpelVarAvg16NEON(src *byte, srcStride int, dst *byte, pixelStep int, height int)

//go:noescape
func subpelVarAvg16ChunksNEON(src *byte, srcStride int, dst *byte, pixelStep int, width int, height int)

// subpelHalfFilter maps libvpx-scale weight (0/16/32/48/64/80/96/112/128)
// to the 0..8 byte-lane scale used by the NEON kernels.
func subpelHalfFilter(filterIdx int) (uint64, uint64) {
	// vp9BilinearFilters[idx][0] is 128 - idx*16; [1] is idx*16. Divide
	// both by 16 to fit in [0, 8].
	return uint64(uint8(vp9BilinearFilters[filterIdx][0] >> 4)),
		uint64(uint8(vp9BilinearFilters[filterIdx][1] >> 4))
}

// subpelVarWindowOK validates the src read-window covers (w+1)x(h+1).
// The horizontal tap looks at src[x+1] and the vertical tap reads
// the row immediately below the last bilinear output, so the safe
// window is (h+1) rows of (w+1) bytes.
func subpelVarWindowOK(buf []uint8, off, stride, w, h int) bool {
	if off < 0 || stride < 0 {
		return false
	}
	limit := off + h*stride + w + 1
	return limit >= off && limit <= len(buf)
}

// runFirstPass runs the horizontal bilinear pre-filter for the given
// width into dst (tightly packed). xOffset must be in [0, 7].
func runFirstPass(src *byte, srcStride int, dst *byte, w, h, xOffset int) {
	f0, f1 := subpelHalfFilter(xOffset)
	switch w {
	case 4:
		subpelVarFilter4NEON(src, srcStride, dst, 1, h, f0, f1)
	case 8:
		subpelVarFilter8NEON(src, srcStride, dst, 1, h, f0, f1)
	case 16:
		subpelVarFilter16NEON(src, srcStride, dst, 1, h, f0, f1)
	default:
		// 32, 64 — repeat the 16-wide body across `w / 16` chunks per row.
		subpelVarFilter16ChunksNEON(src, srcStride, dst, 1, w, h, f0, f1)
	}
}

// runSecondPass runs the vertical bilinear pre-filter on a tightly
// packed (h+1)xw uint8 buffer. yOffset must be in [0, 7].
func runSecondPass(src *byte, dst *byte, w, h, yOffset int) {
	f0, f1 := subpelHalfFilter(yOffset)
	// srcStride for second pass equals the tight width.
	switch w {
	case 4:
		subpelVarFilter4NEON(src, 4, dst, 4, h, f0, f1)
	case 8:
		subpelVarFilter8NEON(src, 8, dst, 8, h, f0, f1)
	case 16:
		subpelVarFilter16NEON(src, 16, dst, 16, h, f0, f1)
	default:
		subpelVarFilter16ChunksNEON(src, w, dst, w, w, h, f0, f1)
	}
}

// runVerticalPassFromSource runs the vertical bilinear pre-filter directly on
// the source plane when the horizontal tap is identity.
func runVerticalPassFromSource(src *byte, srcStride int, dst *byte, w, h, yOffset int) {
	f0, f1 := subpelHalfFilter(yOffset)
	switch w {
	case 4:
		subpelVarFilter4NEON(src, srcStride, dst, srcStride, h, f0, f1)
	case 8:
		subpelVarFilter8NEON(src, srcStride, dst, srcStride, h, f0, f1)
	case 16:
		subpelVarFilter16NEON(src, srcStride, dst, srcStride, h, f0, f1)
	default:
		subpelVarFilter16ChunksNEON(src, srcStride, dst, srcStride, w, h, f0, f1)
	}
}

// runAveragePass runs libvpx's half-pel rounded-average fast path for
// filter offset 4: (a + b + 1) >> 1. The NEON kernel mirrors
// var_filter_block2d_avg from subpel_variance_neon.c.
func runAveragePass(src *byte, srcStride int, dst *byte, w, h, pixelStep int) bool {
	switch w {
	case 8:
		subpelVarAvg8NEON(src, srcStride, dst, pixelStep, h)
	case 16:
		subpelVarAvg16NEON(src, srcStride, dst, pixelStep, h)
	case 32, 64:
		subpelVarAvg16ChunksNEON(src, srcStride, dst, pixelStep, w, h)
	default:
		return false
	}
	return true
}

// finalVarianceFromBlock runs the appropriate NEON variance kernel on
// the temp block (tightly packed at stride=w) against the reference.
func finalVarianceFromBlock(temp []byte, w, h int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	var sum int32
	var s uint32
	tempPtr := unsafe.SliceData(temp)
	refPtr := unsafe.SliceData(ref[refOff:])
	switch w {
	case 4:
		varianceBlock4xNNEON(tempPtr, 4, refPtr, refStride, h, &sum, &s)
	case 8:
		varianceBlock8xNNEON(tempPtr, 8, refPtr, refStride, h, &sum, &s)
	default:
		variance16xNKernel(tempPtr, w, refPtr, refStride, w, h, &sum, &s)
	}
	*sse = s
	return finalVariance(sum, s, w, h)
}

func finalVariance8xNFromBlock(temp []byte, h int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	var sum int32
	var s uint32
	varianceBlock8xNNEON(unsafe.SliceData(temp), 8,
		unsafe.SliceData(ref[refOff:]), refStride, h, &sum, &s)
	*sse = s
	return finalVariance(sum, s, 8, h)
}

func finalVariance16xNFromBlock(temp []byte, h int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	var sum int32
	var s uint32
	variance16xNKernel(unsafe.SliceData(temp), 16,
		unsafe.SliceData(ref[refOff:]), refStride, 16, h, &sum, &s)
	*sse = s
	return finalVariance(sum, s, 16, h)
}

// subPixelVarianceSimd is the common NEON dispatch. Returns the
// (variance, ok) pair; falls back to scalar on out-of-window or
// 4x4-with-odd-height (varianceBlock4xN requires even height — both
// 4x4 and 4x8 are fine, but guard anyway).
func subPixelVarianceSimd(w, h int,
	src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) (uint32, bool) {
	if xOffset < 0 || xOffset > 7 || yOffset < 0 || yOffset > 7 {
		return 0, false
	}
	if w != 4 && w != 8 && w != 16 && w != 32 && w != 64 {
		return 0, false
	}
	if w == 4 && (h&1) != 0 {
		return 0, false
	}
	if xOffset == 0 && yOffset == 0 {
		if !varWindowOK(src, srcOff, srcStride, w, h) ||
			!varWindowOK(ref, refOff, refStride, w, h) {
			return 0, false
		}
		stats := varianceStatsStandard(w, h, src, srcOff, srcStride, ref, refOff, refStride)
		*sse = stats.SSE
		return stats.Variance, true
	}
	if !subpelVarWindowOK(src, srcOff, srcStride, w, h) ||
		!varWindowOK(ref, refOff, refStride, w, h) {
		return 0, false
	}

	if xOffset == 0 || yOffset == 0 {
		return subPixelVarianceSimdOnePassScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, sse), true
	}
	return subPixelVarianceSimdTwoPassScratch(w, h,
		src, srcOff, srcStride, xOffset, yOffset,
		ref, refOff, refStride, sse), true
}

func subPixelVarianceSimdOnePassScratch(w, h int,
	src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	switch {
	case w <= 8 && h <= 16:
		var tmp [8 * 16]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], nil, sse)
	case w <= 16 && h <= 32:
		var tmp [16 * 32]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], nil, sse)
	case w <= 32:
		var tmp [32 * 64]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], nil, sse)
	case h <= 32:
		var tmp [64 * 32]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], nil, sse)
	default:
		var tmp [64 * 64]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], nil, sse)
	}
}

func subPixelVarianceSimdTwoPassScratch(w, h int,
	src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	switch {
	case w <= 8 && h <= 16:
		var tmp [8 * 16]byte
		var fdata [8 * 17]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], fdata[:w*(h+1)], sse)
	case w <= 16 && h <= 32:
		var tmp [16 * 32]byte
		var fdata [16 * 33]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], fdata[:w*(h+1)], sse)
	case w <= 32:
		var tmp [32 * 64]byte
		var fdata [32 * 65]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], fdata[:w*(h+1)], sse)
	case h <= 32:
		var tmp [64 * 32]byte
		var fdata [64 * 33]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], fdata[:w*(h+1)], sse)
	default:
		var tmp [64 * 64]byte
		var fdata [64 * 65]byte
		return subPixelVarianceSimdWithScratch(w, h,
			src, srcOff, srcStride, xOffset, yOffset,
			ref, refOff, refStride, tmp[:w*h], fdata[:w*(h+1)], sse)
	}
}

func subPixelVarianceSimdWithScratch(w, h int,
	src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, temp, fdata []byte, sse *uint32,
) uint32 {
	srcPtr := unsafe.SliceData(src[srcOff:])
	if yOffset == 0 && xOffset != 0 {
		if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), w, h, 1) {
			runFirstPass(srcPtr, srcStride, unsafe.SliceData(temp), w, h, xOffset)
		}
		return finalVarianceFromBlock(temp, w, h, ref, refOff, refStride, sse)
	}
	if xOffset == 0 {
		if yOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), w, h, srcStride) {
			runVerticalPassFromSource(srcPtr, srcStride, unsafe.SliceData(temp), w, h, yOffset)
		}
		return finalVarianceFromBlock(temp, w, h, ref, refOff, refStride, sse)
	}

	if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
		unsafe.SliceData(fdata), w, h+1, 1) {
		runFirstPass(srcPtr, srcStride, unsafe.SliceData(fdata), w, h+1, xOffset)
	}
	if yOffset != 4 || !runAveragePass(unsafe.SliceData(fdata), w,
		unsafe.SliceData(temp), w, h, w) {
		runSecondPass(unsafe.SliceData(fdata), unsafe.SliceData(temp), w, h, yOffset)
	}

	return finalVarianceFromBlock(temp, w, h, ref, refOff, refStride, sse)
}

func subPixelVarianceSimd8x8(src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) (uint32, bool) {
	if xOffset < 0 || xOffset > 7 || yOffset < 0 || yOffset > 7 {
		return 0, false
	}
	if xOffset == 0 && yOffset == 0 {
		if v, ok := varianceSimd8xN(src, srcOff, srcStride, ref, refOff, refStride, 8, sse); ok {
			return v, true
		}
		return 0, false
	}
	if !subpelVarWindowOK(src, srcOff, srcStride, 8, 8) ||
		!varWindowOK(ref, refOff, refStride, 8, 8) {
		return 0, false
	}

	srcPtr := unsafe.SliceData(src[srcOff:])
	var tmp [8 * 8]byte
	temp := tmp[:]
	if yOffset == 0 {
		if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), 8, 8, 1) {
			runFirstPass(srcPtr, srcStride, unsafe.SliceData(temp), 8, 8, xOffset)
		}
		return finalVariance8xNFromBlock(temp, 8, ref, refOff, refStride, sse), true
	}
	if xOffset == 0 {
		if yOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), 8, 8, srcStride) {
			runVerticalPassFromSource(srcPtr, srcStride, unsafe.SliceData(temp), 8, 8, yOffset)
		}
		return finalVariance8xNFromBlock(temp, 8, ref, refOff, refStride, sse), true
	}

	var fdata [8 * 9]byte
	if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
		unsafe.SliceData(fdata[:]), 8, 9, 1) {
		runFirstPass(srcPtr, srcStride, unsafe.SliceData(fdata[:]), 8, 9, xOffset)
	}
	if yOffset != 4 || !runAveragePass(unsafe.SliceData(fdata[:]), 8,
		unsafe.SliceData(temp), 8, 8, 8) {
		runSecondPass(unsafe.SliceData(fdata[:]), unsafe.SliceData(temp), 8, 8, yOffset)
	}
	return finalVariance8xNFromBlock(temp, 8, ref, refOff, refStride, sse), true
}

func subPixelVarianceSimd16x16(src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) (uint32, bool) {
	if xOffset < 0 || xOffset > 7 || yOffset < 0 || yOffset > 7 {
		return 0, false
	}
	if xOffset == 0 && yOffset == 0 {
		if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 16, 16, sse); ok {
			return v, true
		}
		return 0, false
	}
	if !subpelVarWindowOK(src, srcOff, srcStride, 16, 16) ||
		!varWindowOK(ref, refOff, refStride, 16, 16) {
		return 0, false
	}

	srcPtr := unsafe.SliceData(src[srcOff:])
	var tmp [16 * 16]byte
	temp := tmp[:]
	if yOffset == 0 {
		if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), 16, 16, 1) {
			runFirstPass(srcPtr, srcStride, unsafe.SliceData(temp), 16, 16, xOffset)
		}
		return finalVariance16xNFromBlock(temp, 16, ref, refOff, refStride, sse), true
	}
	if xOffset == 0 {
		if yOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), 16, 16, srcStride) {
			runVerticalPassFromSource(srcPtr, srcStride, unsafe.SliceData(temp), 16, 16, yOffset)
		}
		return finalVariance16xNFromBlock(temp, 16, ref, refOff, refStride, sse), true
	}

	var fdata [16 * 17]byte
	if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
		unsafe.SliceData(fdata[:]), 16, 17, 1) {
		runFirstPass(srcPtr, srcStride, unsafe.SliceData(fdata[:]), 16, 17, xOffset)
	}
	if yOffset != 4 || !runAveragePass(unsafe.SliceData(fdata[:]), 16,
		unsafe.SliceData(temp), 16, 16, 16) {
		runSecondPass(unsafe.SliceData(fdata[:]), unsafe.SliceData(temp), 16, 16, yOffset)
	}
	return finalVariance16xNFromBlock(temp, 16, ref, refOff, refStride, sse), true
}

func subPixelVarianceSimd32x32(src []uint8, srcOff, srcStride, xOffset, yOffset int,
	ref []uint8, refOff, refStride int, sse *uint32,
) (uint32, bool) {
	if xOffset < 0 || xOffset > 7 || yOffset < 0 || yOffset > 7 {
		return 0, false
	}
	if xOffset == 0 && yOffset == 0 {
		if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 32, 32, sse); ok {
			return v, true
		}
		return 0, false
	}
	if !subpelVarWindowOK(src, srcOff, srcStride, 32, 32) ||
		!varWindowOK(ref, refOff, refStride, 32, 32) {
		return 0, false
	}

	srcPtr := unsafe.SliceData(src[srcOff:])
	var tmp [32 * 32]byte
	temp := tmp[:]
	if yOffset == 0 {
		if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), 32, 32, 1) {
			runFirstPass(srcPtr, srcStride, unsafe.SliceData(temp), 32, 32, xOffset)
		}
		return finalVarianceFromBlock(temp, 32, 32, ref, refOff, refStride, sse), true
	}
	if xOffset == 0 {
		if yOffset != 4 || !runAveragePass(srcPtr, srcStride,
			unsafe.SliceData(temp), 32, 32, srcStride) {
			runVerticalPassFromSource(srcPtr, srcStride, unsafe.SliceData(temp), 32, 32, yOffset)
		}
		return finalVarianceFromBlock(temp, 32, 32, ref, refOff, refStride, sse), true
	}

	var fdata [32 * 33]byte
	if xOffset != 4 || !runAveragePass(srcPtr, srcStride,
		unsafe.SliceData(fdata[:]), 32, 33, 1) {
		runFirstPass(srcPtr, srcStride, unsafe.SliceData(fdata[:]), 32, 33, xOffset)
	}
	if yOffset != 4 || !runAveragePass(unsafe.SliceData(fdata[:]), 32,
		unsafe.SliceData(temp), 32, 32, 32) {
		runSecondPass(unsafe.SliceData(fdata[:]), unsafe.SliceData(temp), 32, 32, yOffset)
	}
	return finalVarianceFromBlock(temp, 32, 32, ref, refOff, refStride, sse), true
}

// Size-specialised dispatchers. Each tries the NEON path first.

func subPixelVariance64x64(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(64, 64, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(64, 64, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance64x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(64, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(64, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance32x64(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(32, 64, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(32, 64, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance32x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd32x32(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(32, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance32x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(32, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(32, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance16x32(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(16, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(16, 32, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance16x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd16x16(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(16, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance16x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(16, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(16, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance8x16(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(8, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(8, 16, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance8x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd8x8(src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(8, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance8x4(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(8, 4, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(8, 4, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance4x8(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(4, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(4, 8, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
func subPixelVariance4x4(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := subPixelVarianceSimd(4, 4, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse); ok {
		return v
	}
	return subPixelVarianceScalar(4, 4, src, srcOff, srcStride, xOffset, yOffset, ref, refOff, refStride, sse)
}
