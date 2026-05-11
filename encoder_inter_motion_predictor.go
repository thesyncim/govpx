package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type interFrameSearchStart struct {
	mv           vp8enc.MotionVector
	sr           int
	nearSADIndex int
	ok           bool
}

func (search interAnalysisSearchConfig) adjustedForImprovedMVStart(start interFrameSearchStart) interAnalysisSearchConfig {
	if !start.ok {
		return search
	}
	stepParam := start.sr + search.fullPixelSpeedAdjust
	if stepParam > search.fullPixelSearchParam {
		if stepParam >= interFrameMaxMVSearchSteps {
			stepParam = interFrameMaxMVSearchSteps - 1
		}
		search.fullPixelSearchParam = stepParam
		search.fullPixelFurtherSteps = libvpxInterFrameFurtherSteps(search.fullPixelSpeed, stepParam)
	}
	return search
}

type improvedInterFrameMVSlot struct {
	mv       vp8enc.MotionVector
	refFrame vp8common.MVReferenceFrame
	signBias bool
	sad      int
}

func (e *VP8Encoder) improvedInterFrameSearchStart(
	src vp8enc.SourceImage, refFrame vp8common.MVReferenceFrame,
	mbRow int, mbCol int, mbRows int, mbCols int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
) interFrameSearchStart {
	if e == nil || !search.improvedMVPrediction || refFrame == vp8common.IntraFrame {
		return interFrameSearchStart{}
	}
	var slots [8]improvedInterFrameMVSlot
	slotCount := 3
	signBias := e.interFrameSignBias()
	slots[0].fillCurrent(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above, signBias)
	slots[1].fillCurrent(src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left, signBias)
	slots[2].fillCurrent(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft, signBias)
	if e.lastFrameInterModesValid && len(e.lastFrameInterModes) >= mbRows*mbCols && mbRows > 0 && mbCols > 0 {
		slotCount = 8
		slots[3].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
		slots[4].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
		slots[5].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
		slots[6].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
		slots[7].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
	}
	biasImprovedInterFrameMVSlots(&slots, slotCount, refFrame, signBias, mbRow, mbCol, mbRows, mbCols)
	order := improvedInterFrameMVSlotOrder(slots, slotCount)
	for rank := 0; rank < slotCount; rank++ {
		slot := slots[order[rank]]
		if slot.refFrame == refFrame {
			sr := 2
			if rank < 3 {
				sr = 3
			}
			return interFrameSearchStart{mv: slot.mv, sr: sr, nearSADIndex: order[rank], ok: true}
		}
	}
	mv := improvedInterFrameMVMedian(slots, slotCount)
	return interFrameSearchStart{mv: mv, sr: 0, nearSADIndex: -1, ok: true}
}

func (slot *improvedInterFrameMVSlot) fillCurrent(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode, signBias [vp8common.MaxRefFrames]bool) {
	// Mirror libvpx's vp8_mv_pred neighbor table for the current frame: a nil
	// pointer (border MB) corresponds to libvpx's calloc-zeroed mode_info
	// sentinel row/column where ref_frame == INTRA_FRAME and mv == 0, and
	// vp8_cal_sad sets the matching near_sad entry to INT_MAX.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if mode == nil || refMbRow < 0 || refMbCol < 0 {
		return
	}
	slot.refFrame = convertInterFrameReference(mode)
	if slot.refFrame > vp8common.IntraFrame && slot.refFrame < vp8common.MaxRefFrames {
		slot.signBias = signBias[slot.refFrame]
	}
	if slot.refFrame == vp8common.IntraFrame {
		// libvpx leaves near_mvs[vcnt] at zero when the neighbor is intra; do
		// the same here regardless of any stale MV field on the mode entry.
		return
	}
	slot.mv = mode.MV
	slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func (slot *improvedInterFrameMVSlot) fillLast(src vp8enc.SourceImage, img *vp8common.Image, modes []vp8enc.InterFrameMacroblockMode, modeBias []bool, srcMbRow int, srcMbCol int, mbRows int, mbCols int, refMbRow int, refMbCol int) {
	// Mirror libvpx's vp8_mv_pred neighbor table for the previous frame:
	// out-of-range MB coordinates correspond to libvpx's lfmv/lf_ref_frame
	// sentinel rows (top/bottom) and columns (left/right) which are
	// calloc-zeroed and therefore report INTRA_FRAME with mv == 0, while
	// vp8_cal_sad sets the matching near_sad entry to INT_MAX.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if refMbRow < 0 || refMbCol < 0 || refMbRow >= mbRows || refMbCol >= mbCols {
		return
	}
	index := refMbRow*mbCols + refMbCol
	if index < 0 || index >= len(modes) {
		return
	}
	mode := &modes[index]
	slot.refFrame = convertInterFrameReference(mode)
	if index < len(modeBias) {
		slot.signBias = modeBias[index]
	}
	if slot.refFrame == vp8common.IntraFrame {
		// libvpx leaves near_mvs[vcnt] at zero for intra last-frame slots even
		// though it still increments vcnt; mirror that exactly.
		return
	}
	slot.mv = mode.MV
	slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func biasImprovedInterFrameMVSlots(slots *[8]improvedInterFrameMVSlot, count int, refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool, mbRow int, mbCol int, mbRows int, mbCols int) {
	if slots == nil || refFrame <= vp8common.IntraFrame || refFrame >= vp8common.MaxRefFrames {
		return
	}
	targetBias := signBias[refFrame]
	for i := 0; i < count && i < len(slots); i++ {
		slot := &slots[i]
		if slot.refFrame == vp8common.IntraFrame {
			continue
		}
		if slot.signBias != targetBias {
			slot.mv.Row = -slot.mv.Row
			slot.mv.Col = -slot.mv.Col
		}
		slot.mv = clampInterFrameModeMotionVector(slot.mv, mbRow, mbCol, mbRows, mbCols)
	}
}

func clampInterFrameModeMotionVector(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) vp8enc.MotionVector {
	if mbRows <= 0 || mbCols <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return vp8enc.MotionVector{
		Row: int16(clampInterFrameModeMotionVectorComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterFrameModeMotionVectorComponent(int(mv.Col), left, right)),
	}
}

func clampInterFrameModeMotionVectorComponent(v int, lowEdge int, highEdge int) int {
	if v < lowEdge-(16<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(16<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func improvedInterFrameMVSlotOrder(slots [8]improvedInterFrameMVSlot, count int) [8]int {
	var order [8]int
	for i := 0; i < count && i < len(order); i++ {
		order[i] = i
	}
	for i := 1; i < count && i < len(order); i++ {
		idx := order[i]
		sad := slots[idx].sad
		j := i - 1
		for ; j >= 0 && sad < slots[order[j]].sad; j-- {
			order[j+1] = order[j]
		}
		order[j+1] = idx
	}
	return order
}

func improvedInterFrameMVMedian(slots [8]improvedInterFrameMVSlot, count int) vp8enc.MotionVector {
	if count <= 0 {
		return vp8enc.MotionVector{}
	}
	var rows [8]int
	var cols [8]int
	for i := 0; i < count && i < len(slots); i++ {
		rows[i] = int(slots[i].mv.Row)
		cols[i] = int(slots[i].mv.Col)
	}
	insertionSortInts(rows[:count])
	insertionSortInts(cols[:count])
	return vp8enc.MotionVector{Row: int16(rows[count/2]), Col: int16(cols[count/2])}
}

func insertionSortInts(values []int) {
	for i := 1; i < len(values); i++ {
		v := values[i]
		j := i - 1
		for ; j >= 0 && v < values[j]; j-- {
			values[j+1] = values[j]
		}
		values[j+1] = v
	}
}
