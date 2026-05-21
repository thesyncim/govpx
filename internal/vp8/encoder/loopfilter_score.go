package encoder

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// Loop-filter score helpers mirror libvpx v1.16.0 VP8 picklpf.c and
// onyx_if.c scoring paths while staying beside encoder-owned frame types.

// LoopFilterFullPickerBias mirrors libvpx vp8/encoder/picklpf.c
// vp8cx_pick_filter_level's `Bias = (best_err >> (15 - (filt_mid / 8))) *
// filter_step;` followed by `if (section_intra_rating < 20) Bias = Bias *
// section_intra_rating / 20`. The shift amount is `15 - filt_mid/8`. For
// filt_mid in [0, 63] the shift ranges [8, 15].
//
// Critically, libvpx's twopass.section_intra_rating is in the cpi->twopass
// struct which is calloc'd; in one-pass / realtime / CBR encodes it is never
// written so it stays at 0. The unconditional VP8 guard
// `if (section_intra_rating < 20) Bias = Bias * section_intra_rating / 20;`
// then forces Bias = 0 every iteration of the full picker. VP9's analogue adds
// an `oxcf.pass == 2` predicate, but VP8 does not; the two-pass guard is
// implicit via the zero default.
func LoopFilterFullPickerBias(bestErr int, filtMid int, filterStep int, sectionIntraRating int) int {
	shift := max(15-(filtMid/8), 0)
	bias := (bestErr >> uint(shift)) * filterStep
	if sectionIntraRating < 20 {
		bias = bias * sectionIntraRating / 20
	}
	return bias
}

// CopyLoopFilterPartialLuma refreshes the luma plane window the partial-frame
// loop-filter trial reads. It mirrors libvpx's yv12_copy_partial_frame: copy
// from ((y_height >> 5) * 16) - 4 for rowCount MB rows plus the 4 luma context
// lines above, filling negative top-context rows from the visible top row.
func CopyLoopFilterPartialLuma(dst *vp8common.Image, src *vp8common.Image, startRow int, rowCount int) {
	if rowCount <= 0 {
		return
	}
	startY := startRow*16 - 4
	lineCount := rowCount*16 + 4
	if lineCount <= 0 {
		return
	}
	if src.YStride == dst.YStride && len(src.YFull) > 0 && len(dst.YFull) > 0 {
		// libvpx yv12_copy_partial_frame copies y_stride bytes from the
		// visible-origin row, preserving right-border/stride bytes used by
		// vp8_loop_filter_partial_frame.
		topOff := src.YOrigin
		srcOff := src.YOrigin + startY*src.YStride
		dstOff := dst.YOrigin + startY*dst.YStride
		for dstOff < dst.YOrigin && lineCount > 0 {
			// Uint range collapses (off<0) + (off+stride > len) into one
			// compare per buffer.
			if uint(topOff) > uint(len(src.YFull)-src.YStride) || uint(dstOff) > uint(len(dst.YFull)-dst.YStride) {
				return
			}
			copy(dst.YFull[dstOff:dstOff+dst.YStride], src.YFull[topOff:topOff+src.YStride])
			srcOff += src.YStride
			dstOff += dst.YStride
			lineCount--
		}
		n := lineCount * src.YStride
		// Uint range collapses (srcOff/dstOff >= 0) + (srcOff/dstOff+n <=
		// len) into one compare each on the two buffer dimensions. n is
		// guaranteed >= 0 by the lineCount > 0 guard and YStride > 0.
		if lineCount > 0 && uint(srcOff) <= uint(len(src.YFull)-n) && uint(dstOff) <= uint(len(dst.YFull)-n) {
			copy(dst.YFull[dstOff:dstOff+n], src.YFull[srcOff:srcOff+n])
		}
		return
	}
	width := min(dst.CodedWidth, src.CodedWidth)
	startVisibleY := max(startY, 0)
	endVisibleY := min(min(startY+lineCount, src.CodedHeight), dst.CodedHeight)
	if endVisibleY <= startVisibleY {
		return
	}
	if src.YStride == dst.YStride && width == src.YStride {
		copy(dst.Y[startVisibleY*dst.YStride:endVisibleY*dst.YStride], src.Y[startVisibleY*src.YStride:endVisibleY*src.YStride])
		return
	}
	for row := startVisibleY; row < endVisibleY; row++ {
		copy(dst.Y[row*dst.YStride:row*dst.YStride+width], src.Y[row*src.YStride:row*src.YStride+width])
	}
}

// CalcKeyFrameSSError ports libvpx vp8/encoder/onyx_if.c vp8_calc_ss_err over
// the Y plane: full-frame sum of squared 16x16 luma differences between the
// encoded source and the reconstructed frame.
func CalcKeyFrameSSError(src SourceImage, recon *vp8common.Image, rows int, cols int) int {
	if rows <= 0 || cols <= 0 {
		return 0
	}
	return LoopFilterLumaSSE(src, recon, rows, cols, false)
}

// LoopFilterLumaSSE returns the luma SSE used by VP8 loop-filter level picking.
func LoopFilterLumaSSE(src SourceImage, img *vp8common.Image, rows int, cols int, partial bool) int {
	startRow, rowCount := 0, rows
	if partial {
		startRow, rowCount = LoopFilterPartialFrameWindow(rows)
	}
	total := 0
	srcY := src.Y
	imgY := img.Y
	srcStride := src.YStride
	imgStride := img.YStride
	srcW := src.Width
	srcH := src.Height
	imgW := img.CodedWidth
	imgH := img.CodedHeight
	if cols > 0 && rows > 0 && cols*16 <= srcW && cols*16 <= imgW {
		height := rowCount * 16
		if startRow >= 0 && height > 0 && (startRow+rowCount)*16 <= srcH && (startRow+rowCount)*16 <= imgH {
			srcRowOff := startRow * 16 * srcStride
			imgRowOff := startRow * 16 * imgStride
			for mbCol := range cols {
				baseX := mbCol * 16
				total += dsp.SSE16xNPtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride, height)
			}
			return total
		}
	}
	colsAllAligned := cols > 0 && cols*16 <= srcW && cols*16 <= imgW
	for mbRow := startRow; mbRow < startRow+rowCount && mbRow < rows; mbRow++ {
		baseY := mbRow * 16
		if baseY+16 <= srcH && baseY+16 <= imgH {
			srcRowOff := baseY * srcStride
			imgRowOff := baseY * imgStride
			if colsAllAligned {
				for mbCol := range cols {
					baseX := mbCol * 16
					total += dsp.SSE16x16PtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride)
				}
				continue
			}
			for mbCol := range cols {
				baseX := mbCol * 16
				if baseX+16 <= srcW && baseX+16 <= imgW {
					total += dsp.SSE16x16PtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride)
					continue
				}
				total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
			}
			continue
		}
		for mbCol := range cols {
			baseX := mbCol * 16
			total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
		}
	}
	return total
}

func loopFilterLumaBlockSSE(src SourceImage, img *vp8common.Image, baseY int, baseX int) int {
	sse := 0
	for row := range 16 {
		srcY := ClampEncodeCoord(baseY+row, src.Height)
		imgY := ClampEncodeCoord(baseY+row, img.CodedHeight)
		for col := range 16 {
			srcX := ClampEncodeCoord(baseX+col, src.Width)
			imgX := ClampEncodeCoord(baseX+col, img.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(img.Y[imgY*img.YStride+imgX])
			sse += diff * diff
		}
	}
	return sse
}

// LoopFilterPartialFrameWindow returns the libvpx middle-slice macroblock
// window used by VP8's fast loop-filter picker.
func LoopFilterPartialFrameWindow(rows int) (int, int) {
	if rows <= 0 {
		return 0, 0
	}
	start := rows / 2
	count := rows / vp8common.PartialFrameFraction
	count = min(max(count, 1), rows-start)
	return start, count
}

// LoopFilterSearchStep returns the VP8 loop-filter search increment for a
// candidate level.
func LoopFilterSearchStep(level int) int {
	if level > 10 {
		return 2
	}
	return 1
}

// ClampLoopFilterPickLevel clamps a candidate picker level to the active search
// bounds.
func ClampLoopFilterPickLevel(level int, minLevel int, maxLevel int) int {
	return min(max(level, minLevel), maxLevel)
}

// LibvpxClampLoopFilterLevel clamps a filter level to libvpx VP8's quantizer
// dependent legal range.
func LibvpxClampLoopFilterLevel(qIndex int, level int) int {
	return min(max(level, LibvpxMinLoopFilterLevel(qIndex)), LibvpxMaxLoopFilterLevel(qIndex))
}

// LibvpxMinLoopFilterLevel returns libvpx VP8's minimum loop-filter level for
// qIndex.
func LibvpxMinLoopFilterLevel(qIndex int) int {
	if qIndex <= 6 {
		return 0
	}
	if qIndex <= 16 {
		return 1
	}
	return qIndex / 8
}

// LibvpxMaxLoopFilterLevel returns libvpx VP8's maximum loop-filter level for
// qIndex.
func LibvpxMaxLoopFilterLevel(qIndex int) int {
	_ = qIndex
	return vp8common.MaxLoopFilter
}

// LibvpxInitialLoopFilterLevel returns libvpx VP8's key-frame seed level for
// qIndex.
func LibvpxInitialLoopFilterLevel(qIndex int) int {
	if qIndex <= 0 {
		return 0
	}
	level := qIndex * 3 / 8
	if level > vp8common.MaxLoopFilter {
		return vp8common.MaxLoopFilter
	}
	return level
}
