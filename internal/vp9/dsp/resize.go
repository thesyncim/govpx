package dsp

import "image"

// polyphasePhases is the phase count of the
// libvpx-style polyphase resampler. Mirrors libvpx's
// SUBPEL_SHIFTS / vp9_resize.c phase resolution.
const polyphasePhases = 16

// polyphaseTaps is the per-phase filter length.
// libvpx vp9_resize.c uses a symmetric polyphase resampler whose
// kernel is 8 taps wide. The govpx implementation picks 8 taps too:
// a 5-tap kernel is not sufficient to span libvpx's resize impulse
// response at the 16-phase sub-pixel resolution, and an 8-tap kernel
// matches the rest of govpx's VP9 sub-pixel infrastructure.
const polyphaseTaps = 8

// polyphaseShift is the rounding shift applied
// after summing the 8-tap, 16-phase polyphase output. Matches the
// 128-scaled coefficient table below: each phase row sums to 128,
// so a 7-bit shift restores the 8-bit output range.
const polyphaseShift = 7

// polyphaseFilters is the 16-phase, 8-tap
// downscale polyphase filter bank derived from libvpx vp9_resize.c's
// 0.625 (i.e. "filteredinterp_filters625") preset. Every row sums to
// 128. Phase 0 is the identity row (centered on tap index 3); higher
// phases shift the sample location toward the next source pixel.
//
// The libvpx 0.625 preset is a band-limited Lanczos approximation
// tuned for two-axis downscale ratios in [0.5, 0.75]. govpx uses it
// for every downscale ratio. This trades a small per-ratio quality
// loss against avoiding a five-table arena that varies with the
// caller's exact downscale ratio; in practice the multi-resolution
// pyramid stays in the 0.5-0.75 zone where this preset was tuned.
var polyphaseFilters = [polyphasePhases][polyphaseTaps]int16{
	{0, 0, 0, 128, 0, 0, 0, 0},
	{-1, 2, -6, 127, 9, -3, 1, -1},
	{-2, 5, -12, 124, 18, -7, 3, -1},
	{-2, 7, -16, 119, 28, -11, 5, -2},
	{-3, 8, -19, 114, 38, -14, 7, -3},
	{-3, 9, -22, 107, 49, -17, 8, -3},
	{-4, 10, -23, 99, 60, -20, 10, -4},
	{-4, 11, -23, 90, 70, -22, 10, -4},
	{-4, 11, -23, 80, 80, -23, 11, -4},
	{-4, 10, -22, 70, 90, -23, 11, -4},
	{-4, 10, -20, 60, 99, -23, 10, -4},
	{-3, 8, -17, 49, 107, -22, 9, -3},
	{-3, 7, -14, 38, 114, -19, 8, -3},
	{-2, 5, -11, 28, 119, -16, 7, -2},
	{-1, 3, -7, 18, 124, -12, 5, -2},
	{-1, 1, -3, 9, 127, -6, 2, -1},
}

// polyphaseTap plans a 1-D polyphase walk: for each output
// pixel at coordinate `outPos`, compute the source center as
// `(outPos + 0.5) * srcLen / dstLen - 0.5` in 16-phase fixed point,
// then return (integer source index of tap 0, phase index 0..15).
//
// The kernel is centered at tap index 3 (the row entry holding the
// largest coefficient at phase 0), so the leftmost tap sits at
// (center - 3) in source coordinates. The caller clamps that index
// against the source plane edges (replicate-edge sampling).
func polyphaseTap(outPos, dstLen, srcLen int) (firstSrc int, phase int) {
	// Fixed-point ratio with 16-phase resolution: each source pixel
	// is divided into 16 phases. The "+8" before the multiply is the
	// 0.5 source-pixel offset (in 16ths) that aligns the kernel
	// center between samples, matching libvpx's 0.5-shift convention
	// in vp9_resize.c.
	//
	// numerator = (outPos * srcLen * 16) + (srcLen * 8) - (dstLen * 8)
	// pos       = numerator / dstLen
	//
	// Then phase = pos & 15, center = pos >> 4. The leftmost tap is
	// center - 3 because the kernel is centered at tap index 3.
	numerator := int64(outPos)*int64(srcLen)*int64(polyphasePhases) +
		int64(srcLen)*int64(polyphasePhases>>1) -
		int64(dstLen)*int64(polyphasePhases>>1)
	pos := numerator / int64(dstLen)
	// Round phase toward the canonical sub-pixel slot.
	if numerator < 0 && numerator%int64(dstLen) != 0 {
		// Truncation toward zero would land us one phase off for
		// negative source-center positions (small output edge).
		pos -= 1
	}
	phase = int(pos & (polyphasePhases - 1))
	center := int(pos >> 4)
	firstSrc = center - 3
	return firstSrc, phase
}

// PolyphaseFilterPlane resamples one 8-bit plane
// from (srcWidth, srcHeight) into (dstWidth, dstHeight) using the
// 8-tap 16-phase polyphase filter. Edges are handled by replicate
// padding (libvpx's resize replicates the boundary samples too).
//
// The pass uses a row-major two-pass approach: a horizontal pass
// produces an intermediate plane of width dstWidth × height srcHeight
// in the caller-supplied scratch slab, then a vertical pass walks the
// intermediate into the destination. The scratch slab must be at
// least dstWidth * srcHeight int32 entries (the horizontal output
// keeps 7 fractional bits so the vertical pass sums them at full
// precision before the second shift).
func PolyphaseFilterPlane(dst []byte, dstStride int,
	dstWidth, dstHeight int,
	src []byte, srcStride int,
	srcWidth, srcHeight int,
	scratch []int32,
) {
	if dstWidth <= 0 || dstHeight <= 0 || srcWidth <= 0 || srcHeight <= 0 ||
		len(scratch) < dstWidth*srcHeight {
		return
	}

	// Horizontal pass: resample every source row from srcWidth to
	// dstWidth. The output is a signed int32 (we keep the 128-scaled
	// taps unrounded so the second pass can sum at full precision).
	for y := range srcHeight {
		srcRow := src[y*srcStride:]
		dstRow := scratch[y*dstWidth:]
		for x := range dstWidth {
			firstSrc, phase := polyphaseTap(x, dstWidth, srcWidth)
			taps := &polyphaseFilters[phase]
			var acc int32
			for t := range polyphaseTaps {
				idx := firstSrc + t
				if idx < 0 {
					idx = 0
				} else if idx >= srcWidth {
					idx = srcWidth - 1
				}
				acc += int32(taps[t]) * int32(srcRow[idx])
			}
			dstRow[x] = acc
		}
	}

	// Vertical pass: resample every column of the intermediate plane
	// from srcHeight to dstHeight, applying the polyphase filter and
	// the combined two-pass rounding shift. The intermediate samples
	// are 128-scaled; the combined shift is 2 * shift = 14 bits.
	const combinedShift = 2 * polyphaseShift
	const round = 1 << (combinedShift - 1)
	for y := range dstHeight {
		firstSrc, phase := polyphaseTap(y, dstHeight, srcHeight)
		taps := &polyphaseFilters[phase]
		dstRow := dst[y*dstStride:]
		for x := range dstWidth {
			var acc int64
			for t := range polyphaseTaps {
				idx := firstSrc + t
				if idx < 0 {
					idx = 0
				} else if idx >= srcHeight {
					idx = srcHeight - 1
				}
				acc += int64(taps[t]) * int64(scratch[idx*dstWidth+x])
			}
			out := (acc + int64(round)) >> combinedShift
			if out < 0 {
				out = 0
			} else if out > 255 {
				out = 255
			}
			dstRow[x] = byte(out)
		}
	}
}

// PolyphaseDownscaleI420 downscales src into dst at
// the destination's declared visible width/height using the libvpx-
// aligned 8-tap 16-phase polyphase filter. Scratch carries the
// horizontal-pass intermediate; one slab is reused across the three
// planes. The caller-supplied scratch must hold at least
// dstWidth*srcHeight int32 entries — the luma horizontal pass is the
// largest, so chroma reuses the same slab.
func PolyphaseDownscaleI420(dst *image.YCbCr, src *image.YCbCr,
	dstWidth, dstHeight int, scratch []int32,
) {
	if dst == nil || src == nil {
		return
	}
	srcWidth := src.Rect.Dx()
	srcHeight := src.Rect.Dy()
	if srcWidth <= 0 || srcHeight <= 0 || dstWidth <= 0 || dstHeight <= 0 {
		return
	}

	PolyphaseFilterPlane(dst.Y, dst.YStride,
		dstWidth, dstHeight,
		src.Y, src.YStride, srcWidth, srcHeight,
		scratch)

	srcChromaW := (srcWidth + 1) >> 1
	srcChromaH := (srcHeight + 1) >> 1
	dstChromaW := (dstWidth + 1) >> 1
	dstChromaH := (dstHeight + 1) >> 1

	PolyphaseFilterPlane(dst.Cb, dst.CStride,
		dstChromaW, dstChromaH,
		src.Cb, src.CStride, srcChromaW, srcChromaH,
		scratch)
	PolyphaseFilterPlane(dst.Cr, dst.CStride,
		dstChromaW, dstChromaH,
		src.Cr, src.CStride, srcChromaW, srcChromaH,
		scratch)
}

// PolyphaseScratchSize returns the int32 entries the
// scratch slab must hold to satisfy a given (dstWidth, srcHeight)
// luma pair. Chroma reuses the same slab because the I420 chroma
// plane shrinks by 2x in each axis and so cannot exceed the luma
// scratch footprint.
func PolyphaseScratchSize(dstWidth, srcHeight int) int {
	if dstWidth <= 0 || srcHeight <= 0 {
		return 0
	}
	return dstWidth * srcHeight
}
