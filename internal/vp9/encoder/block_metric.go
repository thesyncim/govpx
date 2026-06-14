package encoder

import vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"

func planeRectFits(buf []byte, stride, x, y, w, h int) bool {
	if len(buf) == 0 || stride <= 0 || x < 0 || y < 0 || w <= 0 || h <= 0 {
		return false
	}
	if x > stride || w > stride-x {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if h-1 > maxInt-y {
		return false
	}
	lastRow := y + h - 1
	if lastRow > maxInt/stride {
		return false
	}
	rowStart := lastRow * stride
	if w > maxInt-x {
		return false
	}
	rowEndWidth := x + w
	return rowEndWidth <= len(buf) && rowStart <= len(buf)-rowEndWidth
}

// BlockDiffStats is the raw per-pixel error accumulation for a prediction
// block.
type BlockDiffStats struct {
	Sum   int64
	SSE   uint64
	Count uint64
}

// BlockSAD returns the sum of absolute differences for a source/reference
// rectangle. Size-specialized VP9 DSP kernels are used when available.
func BlockSAD(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int, limit uint64,
) uint64 {
	// libvpx's sad_function pointers (cpi->fn_ptr[bsize].sdf) compute the
	// full block SAD with no early-termination; see vpx_dsp/sad.c SAD().
	// The caller compares the returned SAD against best_sad afterwards.
	srcOff := srcY*srcStride + srcX
	refOff := refY*refStride + refX
	return BlockSADOffsets(src, srcOff, srcStride, ref, refOff, refStride,
		w, h, limit)
}

// BlockSADOffsets is BlockSAD with precomputed slice offsets.
func BlockSADOffsets(src []byte, srcOff, srcStride int,
	ref []byte, refOff, refStride int, w, h int, limit uint64,
) uint64 {
	if sad, ok := BlockSADNoLimitOffsets(src, srcOff, srcStride,
		ref, refOff, refStride, w, h); ok {
		return uint64(sad)
	}
	var sad uint64
	for y := range h {
		srcRow := src[srcOff+y*srcStride:]
		refRow := ref[refOff+y*refStride:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += uint64(diff)
		}
		if sad >= limit {
			return sad
		}
	}
	return sad
}

// BlockSADSkipRowsOffsets mirrors libvpx's vpx_sad_skip_* kernels: compute the
// SAD over every other row, starting at the supplied offsets, and scale it by 2.
func BlockSADSkipRowsOffsets(src []byte, srcOff, srcStride int,
	ref []byte, refOff, refStride int, w, h int, limit uint64,
) uint64 {
	var sad uint64
	rows := h / 2
	for y := range rows {
		srcRow := src[srcOff+y*2*srcStride:]
		refRow := ref[refOff+y*2*refStride:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += uint64(diff)
		}
		if sad*2 >= limit {
			return sad * 2
		}
	}
	return sad * 2
}

// BlockSSE returns the sum of squared errors for a source/reference rectangle.
func BlockSSE(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) uint64 {
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			sse += uint64(diff * diff)
		}
	}
	return sse
}

// BlockDiffVariance returns the variance of per-pixel differences for a
// source/reference rectangle.
func BlockDiffVariance(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) uint64 {
	variance, _ := BlockDiffVarianceSSE(src, srcStride, ref, refStride,
		srcX, srcY, refX, refY, w, h)
	return variance
}

// BlockDiffVarianceSSE returns variance and SSE for a source/reference
// rectangle in the same pass.
func BlockDiffVarianceSSE(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) (uint64, uint64) {
	stats := blockDiffStats(src, srcStride, ref, refStride, srcX, srcY,
		refX, refY, w, h)
	return blockDiffVarianceFromStats(stats), stats.SSE
}

// BlockDiffVarianceSSEClampedSource returns variance and SSE for a full
// prediction block while extending source reads at the visible frame edge.
// libvpx's VP9 encoder scores edge blocks through YV12 source buffers whose
// invisible padding has already been filled from the visible edge; callers
// that only have an image-visible source slice can use this helper to get the
// same arithmetic without materializing a padded copy.
func BlockDiffVarianceSSEClampedSource(src []byte, srcStride, srcW, srcH int,
	ref []byte, refStride int, srcX, srcY, refX, refY, w, h int,
) (variance, sse uint64, ok bool) {
	stats, ok := BlockDiffStatsClampedSource(src, srcStride, srcW, srcH,
		ref, refStride, srcX, srcY, refX, refY, w, h)
	if !ok {
		return 0, 0, false
	}
	variance = blockDiffVarianceFromStats(stats)
	return variance, stats.SSE, true
}

// BlockDiffStatsClampedSource accumulates full-block prediction residuals
// while extending source reads at the visible frame edge.
func BlockDiffStatsClampedSource(src []byte, srcStride, srcW, srcH int,
	ref []byte, refStride int, srcX, srcY, refX, refY, w, h int,
) (BlockDiffStats, bool) {
	maxInt := int(^uint(0) >> 1)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 ||
		srcW <= 0 || srcH <= 0 || w <= 0 || h <= 0 ||
		srcX < 0 || srcY < 0 || refX < 0 || refY < 0 ||
		srcX >= srcW || srcY >= srcH ||
		w-1 > maxInt-srcX || h-1 > maxInt-srcY || h > maxInt/w ||
		srcW > srcStride || !planeRectFits(src, srcStride, 0, 0, srcW, srcH) ||
		!planeRectFits(ref, refStride, refX, refY, w, h) {
		return BlockDiffStats{}, false
	}
	if w <= srcW && h <= srcH && srcX <= srcW-w && srcY <= srcH-h {
		return blockDiffStats(src, srcStride, ref, refStride,
			srcX, srcY, refX, refY, w, h), true
	}

	var stats BlockDiffStats
	stats.Count = uint64(w * h)
	for y := range h {
		sy := srcY + y
		if sy >= srcH {
			sy = srcH - 1
		}
		srcRow := src[sy*srcStride:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			sx := srcX + x
			if sx >= srcW {
				sx = srcW - 1
			}
			diff := int64(int(srcRow[sx]) - int(refRow[x]))
			stats.Sum += diff
			stats.SSE += uint64(diff * diff)
		}
	}
	return stats, true
}

func blockDiffStats(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) BlockDiffStats {
	var stats BlockDiffStats
	if w <= 0 || h <= 0 {
		return stats
	}
	stats.Count = uint64(w * h)
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int64(int(srcRow[x]) - int(refRow[x]))
			stats.Sum += diff
			stats.SSE += uint64(diff * diff)
		}
	}
	return stats
}

func blockDiffVarianceFromStats(stats BlockDiffStats) uint64 {
	if stats.Count == 0 {
		return 0
	}
	meanSquares := uint64((stats.Sum * stats.Sum) / int64(stats.Count))
	if stats.SSE <= meanSquares {
		return 0
	}
	return stats.SSE - meanSquares
}

// BlockSourceVariance128 returns the variance of source samples around 128.
func BlockSourceVariance128(src []byte, srcStride int, srcX, srcY, w, h int) uint64 {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		for x := range w {
			diff := int64(srcRow[x]) - 128
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0
	}
	return sse - meanSquares
}

// SourceVarianceAreaPerPixel returns BlockSourceVariance128 normalized by
// rectangle area with libvpx-style rounding.
func SourceVarianceAreaPerPixel(src []byte, srcStride int, srcX, srcY, w, h int) uint {
	if w <= 0 || h <= 0 {
		return 0
	}
	variance := BlockSourceVariance128(src, srcStride, srcX, srcY, w, h)
	pixels := uint64(w * h)
	return uint((variance + (pixels >> 1)) / pixels)
}

// InterSkipFilterSearch reports whether source variance gates this block out
// of multi-interp-filter RD search.
func InterSkipFilterSearch(srcVariance uint, threshold uint) bool {
	return threshold > 0 && srcVariance < threshold
}

// BlockSADNoLimitOffsets dispatches to a size-specialized SAD kernel when the
// block dimensions match a VP9 search block.
func BlockSADNoLimitOffsets(src []byte, srcOff, srcStride int,
	ref []byte, refOff, refStride int, w, h int,
) (uint32, bool) {
	switch {
	case w == 64 && h == 64:
		return vp9dsp.VpxSad64x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 64 && h == 32:
		return vp9dsp.VpxSad64x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 64:
		return vp9dsp.VpxSad32x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 32:
		return vp9dsp.VpxSad32x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 16:
		return vp9dsp.VpxSad32x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 32:
		return vp9dsp.VpxSad16x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 16:
		return vp9dsp.VpxSad16x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 8:
		return vp9dsp.VpxSad16x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 16:
		return vp9dsp.VpxSad8x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 8:
		return vp9dsp.VpxSad8x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 4:
		return vp9dsp.VpxSad8x4(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 8:
		return vp9dsp.VpxSad4x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 4:
		return vp9dsp.VpxSad4x4(src, srcOff, srcStride, ref, refOff, refStride), true
	default:
		return 0, false
	}
}

// VisibleInterScoreBlock clips an inter scoring rectangle to the visible
// source and reference extents.
func VisibleInterScoreBlock(x0, y0, blockW, blockH int,
	srcW, srcH, refW, refH int,
) (int, int, bool) {
	if x0 < 0 || y0 < 0 || blockW <= 0 || blockH <= 0 ||
		x0 >= srcW || y0 >= srcH || x0 >= refW || y0 >= refH {
		return 0, 0, false
	}
	scoreW := min(blockW, srcW-x0)
	scoreW = min(scoreW, refW-x0)
	scoreH := min(blockH, srcH-y0)
	scoreH = min(scoreH, refH-y0)
	return scoreW, scoreH, scoreW > 0 && scoreH > 0
}
