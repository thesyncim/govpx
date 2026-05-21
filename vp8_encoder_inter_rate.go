package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) interMacroblockSkipRate(skip bool) int {
	return vp8enc.InterMacroblockSkipRate(e.probSkipFalse, skip)
}

func (e *VP8Encoder) interIntraReferenceRate() int {
	if e.threadedHelperRowsActive {
		return 0
	}
	return vp8enc.BoolBitCost(e.refProbIntra, 0)
}

func (e *VP8Encoder) interInterReferenceRate(refRate int) int {
	if e.threadedHelperRowsActive {
		return 0
	}
	return vp8enc.BoolBitCost(e.refProbIntra, 1) + refRate
}

// interIntraMacroblockModeRate models libvpx vp8_calc_ref_frame_costs for the
// intra-coded ref-frame branch: skip-bit + intra/inter selector with the
// previous-frame prob_intra_coded.
func (e *VP8Encoder) interIntraMacroblockModeRate() int {
	return e.interMacroblockSkipRate(false) + e.interIntraReferenceRate()
}

func (e *VP8Encoder) interMotionModeRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return e.interIntraReferenceRate()
	}
	return e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, e.interReferenceFrameRate(mode.RefFrame))
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, vp8enc.RDNewMVBitCostWeight)
}

func (e *VP8Encoder) fastInterMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, vp8enc.FastNewMVBitCostWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return e.interIntraReferenceRate()
	}
	signBias := e.interFrameSignBias()
	return e.interInterReferenceRate(refRate) +
		vp8enc.InterPredictionModeRate(mode.Mode, vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame, signBias)) +
		vp8enc.InterMotionModeVectorCost(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, &e.modeProbs.MV, e.currentMotionVectorCostTables(), &e.subMVRefProbs, newMVWeight, signBias)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndModeContext(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, refRate int, modeCounts vp8enc.InterModeCounts, bestRefMV vp8enc.MotionVector, newMVWeight int) int {
	return e.interMotionModeRateWithReferenceRateAndModeContextAndCosts(mode, left, above, refRate, modeCounts, bestRefMV, e.currentMotionVectorCostTables(), newMVWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndModeContextAndCosts(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, refRate int, modeCounts vp8enc.InterModeCounts, bestRefMV vp8enc.MotionVector, mvCosts *vp8enc.MotionVectorCostTables, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return e.interIntraReferenceRate()
	}
	return e.interInterReferenceRate(refRate) +
		vp8enc.InterPredictionModeRate(mode.Mode, modeCounts) +
		vp8enc.InterMotionModeVectorCostWithBestRef(mode, left, above, bestRefMV, &e.modeProbs.MV, mvCosts, &e.subMVRefProbs, newMVWeight)
}

// interReferenceFrameRate ports libvpx vp8_calc_ref_frame_costs (bitstream.c):
// the LAST/GOLDEN/ALTREF tree uses the previous-frame prob_last_coded and
// prob_gf_coded, NOT a per-frame static 128.
func (e *VP8Encoder) interReferenceFrameRate(refFrame vp8common.MVReferenceFrame) int {
	if e.threadedHelperRowsActive {
		return 0
	}
	return vp8enc.InterReferenceFrameRate(refFrame, e.refProbLast, e.refProbGolden)
}

func (e *VP8Encoder) interReferenceFrameRateForReference(ref interAnalysisReference) int {
	if e.threadedHelperRowsActive {
		return 0
	}
	if ref.RefRateSet {
		return ref.RefRate
	}
	return e.interReferenceFrameRate(ref.Frame)
}

func (e *VP8Encoder) interReferenceFrameRatesForFlags(flags EncodeFlags) (last int, golden int, alt int) {
	probLast := e.refProbLast
	probGolden := e.refProbGolden
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	temporalSingleRef := e.interReferenceFrameRatesUseTemporalSingleRefSpecialCase()
	switch {
	case lastEnabled && !goldenEnabled && !altEnabled:
		probLast = 255
		probGolden = 128
	case temporalSingleRef && !lastEnabled && goldenEnabled && !altEnabled:
		probLast = 1
		probGolden = 255
	case temporalSingleRef && !lastEnabled && !goldenEnabled && altEnabled:
		probLast = 1
		probGolden = 1
	}
	return vp8enc.InterReferenceFrameRate(vp8common.LastFrame, probLast, probGolden),
		vp8enc.InterReferenceFrameRate(vp8common.GoldenFrame, probLast, probGolden),
		vp8enc.InterReferenceFrameRate(vp8common.AltRefFrame, probLast, probGolden)
}

func (e *VP8Encoder) interReferenceFrameRatesUseTemporalSingleRefSpecialCase() bool {
	if !e.opts.TemporalScalability.Enabled {
		return false
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	return ok && pattern.Layers > 1
}

func macroblockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvCol := int(mv.Col)
	mvRow := int(mv.Row)
	refBaseY := baseY + (mvRow >> 3)
	refBaseX := baseX + (mvCol >> 3)
	if (mvCol|mvRow)&7 == 0 &&
		uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SAD16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
		}
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, maxInt())
}

func macroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	// Uint range collapses (base >= 0) and (base+16 <= dim) into one
	// compare per dimension (works when dim >= 16; smaller dims fall
	// through to the per-pixel clamped path).
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if sse, ok := macroblockSubpixelSSEBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return sse
			}
		}
	}
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SSE16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
		}
	}

	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}

func macroblockLumaMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if variance, sse, ok := macroblockSubpixelVarianceBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return variance, sse
			}
		}
	}
	// Uint range collapses (baseY/X >= 0) and (baseY/X+16 <= dim) into
	// one compare each, matching the pattern in macroblockLumaSSE.
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
			return sse - ((sum * sum) >> 8), sse
		}
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

// macroblockSADLimited dispatches the limit-aware 16x16 SAD between the
// full-pel bordered-reference SIMD kernel, the sub-pel six-tap predict path,
// and the scalar fallback for invalid buffers / non-UMV callers.
func macroblockSADLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvCol := int(mv.Col)
	mvRow := int(mv.Row)
	refBaseY := baseY + (mvRow >> 3)
	refBaseX := baseX + (mvCol >> 3)
	if (mvCol|mvRow)&7 == 0 &&
		uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SAD16x16LimitPtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride, limit)
		}
	}
	return macroblockSADLimitedSlow(src, ref, baseY, baseX, refBaseY, refBaseX, mvCol, mvRow, limit)
}

func macroblockSADLimitedSlow(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, mvCol int, mvRow int, limit int) int {
	xOffset := mvCol & 7
	yOffset := mvRow & 7
	srcInBounds := baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width
	if xOffset|yOffset != 0 {
		if srcInBounds {
			if sad, ok := macroblockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset, limit); ok {
				return sad
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if sad, ok := macroblockSubpixelSADBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16, limit); ok {
				return sad
			}
		}
	}
	if srcInBounds &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride, limit)
	}

	srcY0 := src.Y
	refY0 := ref.Y
	srcStride := src.YStride
	refStride := ref.YStride
	srcH := src.Height
	srcW := src.Width
	refH := ref.CodedHeight
	refW := ref.CodedWidth
	var srcXs [16]int
	var refXs [16]int
	for col := range 16 {
		srcXs[col] = vp8enc.ClampEncodeCoord(baseX+col, srcW)
		refXs[col] = vp8enc.ClampEncodeCoord(refBaseX+col, refW)
	}
	sad := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, srcH)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, refH)
		srcRow := srcY * srcStride
		refRow := refY * refStride
		for col := range 16 {
			diff := int(srcY0[srcRow+srcXs[col]]) - int(refY0[refRow+refXs[col]])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

func splitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
			if sad, ok := splitBlockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset); ok {
				return sad
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, width, height, srcScratch[:], 16)
			if sad, ok := splitBlockSubpixelSADBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, srcScratch[:], 16); ok {
				return sad
			}
		}
	}
	if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock, ok := refFullPelYSlice(ref, refBaseY, refBaseX, width, height)
		if ok {
			switch {
			case width == 16 && height == 8:
				return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 16:
				return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 8:
				return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 4 && height == 4:
				return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
			}
		}
	}

	// libvpx allocates every reference (cm->yv12_fb[]) and lookahead source
	// buffer with 16-aligned width/height via vp8_yv12_alloc_frame_buffer
	// (vp8/common/alloccommon.c:56-65 rounds (width|height) up to a
	// multiple of 16 before calling vp8_yv12_alloc_frame_buffer, and
	// vp8/encoder/lookahead.c:68-70 does the same), so for an odd-axis
	// frame y_crop_height == y_height == coded height (e.g. 368 for a
	// visible 360 input). The post-loop-filter publication path
	// (vp8/encoder/onyx_if.c:3212 vp8_yv12_extend_frame_borders applied to
	// cm->frame_to_show) therefore extends from coded-height-1 (not from
	// visible-height-1), and the coded-but-invisible MB-padded rows/cols
	// retain the LF-modified reconstruction. The SPLITMV picker's NEW4X4
	// SAD walk on padded-edge MBs must read from that region the same way
	// libvpx does — i.e. clamp reference coords to CodedHeight/CodedWidth,
	// not to the visible extent. The source side, by contrast, comes from
	// vp8_copy_and_extend_frame (vp8/common/extend.c:14-72, called from
	// lookahead.c:139) which is a visible-edge extend, so source coords
	// remain clamped to visible Width/Height.
	refClampHeight := ref.CodedHeight
	refClampWidth := ref.CodedWidth
	sad := 0
	for row := range height {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, refClampHeight)
		for col := range width {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, refClampWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}

func splitBlockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, bool) {
	return splitBlockSubpixelSADBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func splitBlockSubpixelSADBlock(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+height+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+width+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+(height+4)*ref.YStride+width+5 > len(ref.YFull) {
		return 0, false
	}
	// libvpx allocates every reference (cm->yv12_fb[]) with 16-aligned
	// width/height via vp8_yv12_alloc_frame_buffer (alloccommon.c:56-65
	// rounds up before allocating), so y_crop_height == y_height == coded
	// height for the reference buffer. The post-LF
	// vp8_yv12_extend_frame_borders therefore extends from coded-height-1
	// (not from visible-height-1), leaving the coded-but-invisible MB
	// padding populated with the live LF reconstruction. The SixTap path
	// here must read that same reconstruction directly through ref.YFull
	// — only fall to the scratch-clamp when the read window would actually
	// spill past the coded plane (into the symmetric ExtendBorders region,
	// which mirrors libvpx's bottom/right replication from coded-edge-1).
	clampH := ref.CodedHeight
	clampW := ref.CodedWidth
	useScratch := refBaseY-2 < 0 || refBaseX-2 < 0 ||
		refBaseY+height+3 > clampH || refBaseX+width+3 > clampW
	var pred [16 * 16]byte
	switch {
	case width == 16 && height == 8:
		if useScratch {
			var scratch [(8 + 5) * (16 + 5)]byte
			gatherCodedClampedRefBlock(ref, refBaseY-2, refBaseX-2, 16+5, 8+5, scratch[:], 16+5)
			dsp.SixTapPredict16x8(scratch[:], 16+5, xOffset, yOffset, pred[:], 16)
		} else {
			dsp.SixTapPredict16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
		}
		return dsp.SAD16x8(srcBlock, srcStride, pred[:], 16), true
	case width == 8 && height == 16:
		if useScratch {
			var scratch [(16 + 5) * (8 + 5)]byte
			gatherCodedClampedRefBlock(ref, refBaseY-2, refBaseX-2, 8+5, 16+5, scratch[:], 8+5)
			dsp.SixTapPredict8x16(scratch[:], 8+5, xOffset, yOffset, pred[:], 8)
		} else {
			dsp.SixTapPredict8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		}
		return dsp.SAD8x16(srcBlock, srcStride, pred[:], 8), true
	case width == 8 && height == 8:
		if useScratch {
			var scratch [(8 + 5) * (8 + 5)]byte
			gatherCodedClampedRefBlock(ref, refBaseY-2, refBaseX-2, 8+5, 8+5, scratch[:], 8+5)
			dsp.SixTapPredict8x8(scratch[:], 8+5, xOffset, yOffset, pred[:], 8)
		} else {
			dsp.SixTapPredict8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		}
		return dsp.SAD8x8(srcBlock, srcStride, pred[:], 8), true
	case width == 4 && height == 4:
		if useScratch {
			var scratch [(4 + 5) * (4 + 5)]byte
			gatherCodedClampedRefBlock(ref, refBaseY-2, refBaseX-2, 4+5, 4+5, scratch[:], 4+5)
			dsp.SixTapPredict4x4(scratch[:], 4+5, xOffset, yOffset, pred[:], 4)
		} else {
			dsp.SixTapPredict4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 4)
		}
		return dsp.SAD4x4(srcBlock, srcStride, pred[:], 4), true
	default:
		return 0, false
	}
}

func macroblockSubpixelSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, bool) {
	return macroblockSubpixelSSEBlock(ref, refBaseY, refBaseX, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func macroblockSubpixelSSEBlock(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SSE16x16(srcBlock, srcStride, pred[:], 16), true
}

func macroblockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int, limit int) (int, bool) {
	return macroblockSubpixelSADBlock(ref, refBaseY, refBaseX, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride, limit)
}

func macroblockSubpixelSADBlock(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, srcBlock []byte, srcStride int, limit int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SAD16x16Limit(srcBlock, srcStride, pred[:], 16, limit), true
}

func splitBlockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, int, bool) {
	return splitBlockSubpixelVarianceBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func splitBlockSubpixelVarianceBlock(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+height+1 > ref.CodedHeight+ref.YBorder ||
		refBaseX+width+1 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+height*ref.YStride+width+1 > len(ref.YFull) {
		return 0, 0, false
	}
	// libvpx allocates the reference frame with 16-aligned dimensions
	// (alloccommon.c:56-65 rounds (width,height) up before calling
	// vp8_yv12_alloc_frame_buffer), so y_crop_height == y_height == coded
	// height for the reference buffer. The post-LF
	// vp8_yv12_extend_frame_borders therefore extends from coded-edge-1
	// (yv12extend.c:105-117), leaving rows/cols [Visible, Coded) populated
	// with the live LF reconstruction. The bilinear subpel-variance must
	// read the same coded reconstruction libvpx sees — clamp the scratch
	// path to the coded extent, not to the visible edge.
	useScratch := refBaseY < 0 || refBaseX < 0 ||
		refBaseY+height+1 > ref.CodedHeight || refBaseX+width+1 > ref.CodedWidth
	switch {
	case width == 16 && height == 8:
		if useScratch {
			var scratch [(8 + 1) * (16 + 1)]byte
			gatherCodedClampedRefBlock(ref, refBaseY, refBaseX, 16+1, 8+1, scratch[:], 16+1)
			variance, sse := dsp.SubpelVariance16x8(scratch[:], 16+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	case width == 8 && height == 16:
		if useScratch {
			var scratch [(16 + 1) * (8 + 1)]byte
			gatherCodedClampedRefBlock(ref, refBaseY, refBaseX, 8+1, 16+1, scratch[:], 8+1)
			variance, sse := dsp.SubpelVariance8x16(scratch[:], 8+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	case width == 8 && height == 8:
		if useScratch {
			var scratch [(8 + 1) * (8 + 1)]byte
			gatherCodedClampedRefBlock(ref, refBaseY, refBaseX, 8+1, 8+1, scratch[:], 8+1)
			variance, sse := dsp.SubpelVariance8x8(scratch[:], 8+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	case width == 4 && height == 4:
		if useScratch {
			var scratch [(4 + 1) * (4 + 1)]byte
			gatherCodedClampedRefBlock(ref, refBaseY, refBaseX, 4+1, 4+1, scratch[:], 4+1)
			variance, sse := dsp.SubpelVariance4x4(scratch[:], 4+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	default:
		return 0, 0, false
	}
}

func macroblockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, int, bool) {
	return macroblockSubpixelVarianceBlock(ref, refBaseY, refBaseX, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func macroblockSubpixelVarianceBlock(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+17 > ref.CodedHeight+ref.YBorder ||
		refBaseX+17 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+16*ref.YStride+17 > len(ref.YFull) {
		return 0, 0, false
	}
	variance, sse := dsp.SubpelVariance16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
	return variance, sse, true
}
