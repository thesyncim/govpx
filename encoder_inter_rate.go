package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func interMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return interMotionModeVectorCostWithNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, libvpxRDNewMVBitCostWeight)
}

func interMotionModeVectorCostWithNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int) int {
	return interMotionModeVectorCostWithNewMVWeightAndSignBias(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, newMVWeight, defaultInterFrameSignBias())
}

func interMotionModeVectorCostWithNewMVWeightAndSignBias(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int, signBias [vp8common.MaxRefFrames]bool) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame, mbRow, mbCol, mbRows, mbCols, signBias)
	if mode.Mode == vp8common.SplitMV {
		return splitMotionModeVectorCost(mode, left, above, best, mvProbs)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	return interNewMVVectorCost(mode.MV, best, mvProbs, newMVWeight)
}

func interMotionModeVectorCostWithBestRefMV(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, bestRefMV vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	if mode.Mode == vp8common.SplitMV {
		return splitMotionModeVectorCost(mode, left, above, bestRefMV, mvProbs)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	return interNewMVVectorCost(mode.MV, bestRefMV, mvProbs, newMVWeight)
}

func interMacroblockSkipRate(skip bool) int {
	return interMacroblockSkipRateWithProb(128, skip)
}

func interMacroblockSkipRateWithProb(prob uint8, skip bool) int {
	if prob == 0 {
		prob = 128
	}
	if skip {
		return boolBitCost(prob, 1)
	}
	return boolBitCost(prob, 0)
}

func (e *VP8Encoder) interMacroblockSkipRate(skip bool) int {
	return interMacroblockSkipRateWithProb(e.probSkipFalse, skip)
}

// interIntraMacroblockModeRate models libvpx vp8_calc_ref_frame_costs for the
// intra-coded ref-frame branch: skip-bit + intra/inter selector with the
// previous-frame prob_intra_coded.
func (e *VP8Encoder) interIntraMacroblockModeRate() int {
	return e.interMacroblockSkipRate(false) + boolBitCost(e.refProbIntra, 0)
}

func (e *VP8Encoder) interMotionModeRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	return e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, e.interReferenceFrameRate(mode.RefFrame))
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxRDNewMVBitCostWeight)
}

func (e *VP8Encoder) fastInterMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxFastNewMVBitCostWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	signBias := e.interFrameSignBias()
	return boolBitCost(e.refProbIntra, 1) +
		refRate +
		interPredictionModeRate(mode.Mode, vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame, signBias)) +
		interMotionModeVectorCostWithNewMVWeightAndSignBias(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, &e.modeProbs.MV, newMVWeight, signBias)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndModeContext(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, refRate int, modeCounts vp8enc.InterModeCounts, bestRefMV vp8enc.MotionVector, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	return boolBitCost(e.refProbIntra, 1) +
		refRate +
		interPredictionModeRate(mode.Mode, modeCounts) +
		interMotionModeVectorCostWithBestRefMV(mode, left, above, bestRefMV, &e.modeProbs.MV, newMVWeight)
}

// interReferenceFrameRate ports libvpx vp8_calc_ref_frame_costs (bitstream.c):
// the LAST/GOLDEN/ALTREF tree uses the previous-frame prob_last_coded and
// prob_gf_coded, NOT a per-frame static 128.
func (e *VP8Encoder) interReferenceFrameRate(refFrame vp8common.MVReferenceFrame) int {
	return interReferenceFrameRateWithProbs(refFrame, e.refProbLast, e.refProbGolden)
}

func (e *VP8Encoder) interReferenceFrameRateForReference(ref interAnalysisReference) int {
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
	return interReferenceFrameRateWithProbs(vp8common.LastFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.GoldenFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.AltRefFrame, probLast, probGolden)
}

func (e *VP8Encoder) interReferenceFrameRatesUseTemporalSingleRefSpecialCase() bool {
	if !e.opts.TemporalScalability.Enabled {
		return false
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	return ok && pattern.Layers > 1
}

func interReferenceFrameRateWithProbs(refFrame vp8common.MVReferenceFrame, probLast uint8, probGolden uint8) int {
	switch refFrame {
	case vp8common.LastFrame:
		return boolBitCost(probLast, 0)
	case vp8common.GoldenFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 0)
	case vp8common.AltRefFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 1)
	default:
		return 1 << 30
	}
}

func interPredictionModeRate(mode vp8common.MBPredictionMode, counts vp8enc.InterModeCounts) int {
	probs := vp8tables.InterModeContexts
	switch mode {
	case vp8common.ZeroMV:
		return boolBitCost(probs[counts.Intra][0], 0)
	case vp8common.NearestMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 0)
	case vp8common.NearMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 0)
	case vp8common.NewMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 0)
	case vp8common.SplitMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 1)
	default:
		return 1 << 30
	}
}

func splitMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mode.Partition >= vp8tables.NumMBSplits {
		return 1 << 30
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	cost := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(vp8tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := analysisSplitLeftMV(mode, left, block)
		aboveMV := analysisSplitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return maxInt() / 4
		}
		cost += splitSubMotionLabelRate(bMode)
		if bMode == vp8common.New4x4 {
			delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
			cost += splitMotionVectorCost(delta, mvProbs)
		}
	}
	return cost
}

var libvpxDefaultSubMVRefProbs = [3]uint8{180, 162, 25}

func splitSubMotionLabelRate(mode vp8common.BPredictionMode) int {
	// libvpx RD uses x->inter_bmode_costs, built from fc.sub_mv_ref_prob,
	// while bitstream writing uses context-specific sub-MV probabilities.
	return splitSubMotionLabelCostWithProbs(mode, libvpxDefaultSubMVRefProbs)
}

func splitSubMotionLabelCostWithProbs(mode vp8common.BPredictionMode, probs [3]uint8) int {
	// Single unsigned-range check; the prior two-branch form left the
	// function 2 cost-points over the inliner budget.
	if uint(mode-vp8common.Left4x4) > uint(vp8common.New4x4-vp8common.Left4x4) {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.SubMVRefTree[:], probs[:], int(mode))
}

func splitSubMotionLabelMatchesMV(mode vp8common.BPredictionMode, target vp8enc.MotionVector, left vp8enc.MotionVector, above vp8enc.MotionVector) bool {
	switch mode {
	case vp8common.Left4x4:
		return target == left
	case vp8common.Above4x4:
		return above != left && target == above
	case vp8common.Zero4x4:
		return target.IsZero()
	case vp8common.New4x4:
		return true
	default:
		return false
	}
}

func mbSplitPartitionRate(partition uint8) int {
	if partition >= vp8tables.NumMBSplits {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.MBSplitTree[:], vp8tables.MBSplitProbs[:], int(partition))
}

func analysisSplitLeftMV(cur *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block&3 == 0 {
		if left == nil {
			return vp8enc.MotionVector{}
		}
		if left.Mode == vp8common.SplitMV {
			return left.BlockMV[block+3]
		}
		return left.MV
	}
	return cur.BlockMV[block-1]
}

func analysisSplitAboveMV(cur *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block>>2 == 0 {
		if above == nil {
			return vp8enc.MotionVector{}
		}
		if above.Mode == vp8common.SplitMV {
			return above.BlockMV[block+12]
		}
		return above.MV
	}
	return cur.BlockMV[block-4]
}

func interNewMVVectorCost(mv vp8enc.MotionVector, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, weight int) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, best, mvProbs, weight)
}

func splitMotionVectorCost(mv vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, mvProbs, 102)
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
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SSE16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
		}
	}

	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
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
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
			return sse - ((sum * sum) >> 8), sse
		}
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
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
	if xOffset|yOffset != 0 && srcInBounds {
		if sad, ok := macroblockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset, limit); ok {
			return sad
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
		srcXs[col] = clampEncodeCoord(baseX+col, srcW)
		refXs[col] = clampEncodeCoord(refBaseX+col, refW)
	}
	sad := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, srcH)
		refY := clampEncodeCoord(refBaseY+row, refH)
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
		if baseY >= 0 && baseX >= 0 &&
			baseY+height <= src.Height && baseX+width <= src.Width {
			if sad, ok := splitBlockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset); ok {
				return sad
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+height <= src.Height && baseX+width <= src.Width {
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

	sad := 0
	for row := range height {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range width {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}

func splitBlockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, bool) {
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
	var pred [16 * 16]byte
	switch {
	case width == 16 && height == 8:
		dsp.SixTapPredict16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
		return dsp.SAD16x8(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16), true
	case width == 8 && height == 16:
		dsp.SixTapPredict8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		return dsp.SAD8x16(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 8), true
	case width == 8 && height == 8:
		dsp.SixTapPredict8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		return dsp.SAD8x8(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 8), true
	case width == 4 && height == 4:
		dsp.SixTapPredict4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 4)
		return dsp.SAD4x4(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 4), true
	default:
		return 0, false
	}
}

func macroblockSubpixelSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, bool) {
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
	return dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16), true
}

func macroblockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int, limit int) (int, bool) {
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
	return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16, limit), true
}

func splitBlockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, int, bool) {
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
	srcBlock := src.Y[baseY*src.YStride+baseX:]
	switch {
	case width == 16 && height == 8:
		variance, sse := dsp.SubpelVariance16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	case width == 8 && height == 16:
		variance, sse := dsp.SubpelVariance8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	case width == 8 && height == 8:
		variance, sse := dsp.SubpelVariance8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	case width == 4 && height == 4:
		variance, sse := dsp.SubpelVariance4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	default:
		return 0, 0, false
	}
}

func macroblockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, int, bool) {
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
	variance, sse := dsp.SubpelVariance16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
	return variance, sse, true
}

func macroblockChromaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	if baseY >= 0 && baseX >= 0 &&
		baseY+8 <= uvHeight && baseX+8 <= uvWidth &&
		baseY+8 <= refUVHeight && baseX+8 <= refUVWidth {
		srcUOffset := baseY*src.UStride + baseX
		refUOffset := baseY*ref.UStride + baseX
		srcVOffset := baseY*src.VStride + baseX
		refVOffset := baseY*ref.VStride + baseX
		return dsp.SSE8x8PtrFast(&src.U[srcUOffset], src.UStride, &ref.U[refUOffset], ref.UStride) +
			dsp.SSE8x8PtrFast(&src.V[srcVOffset], src.VStride, &ref.V[refVOffset], ref.VStride)
	}

	sse := 0
	for row := range 8 {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		refY := clampEncodeCoord(baseY+row, refUVHeight)
		for col := range 8 {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			refX := clampEncodeCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

func macroblockLumaVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		baseY+16 <= ref.CodedHeight && baseX+16 <= ref.CodedWidth {
		// R15-C: fused (sum, sse) read collapses Variance16x16 + SSE16x16
		// into one SIMD pass (variance = sse - sum*sum/256).
		sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[baseY*ref.YStride+baseX], ref.YStride)
		return sse - ((sum * sum) >> 8), sse
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}
