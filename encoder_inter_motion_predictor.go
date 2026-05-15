package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type interFrameSearchStart struct {
	mv           vp8enc.MotionVector
	sr           int8
	nearSADIndex int8
	flags        uint8
}

const interFrameSearchStartOK uint8 = 1 << 0

func newInterFrameSearchStart(mv vp8enc.MotionVector, sr int, nearSADIndex int) interFrameSearchStart {
	return interFrameSearchStart{
		mv:           mv,
		sr:           int8(sr),
		nearSADIndex: int8(nearSADIndex),
		flags:        interFrameSearchStartOK,
	}
}

func (start interFrameSearchStart) ok() bool {
	return start.flags&interFrameSearchStartOK != 0
}

func (start interFrameSearchStart) searchRange() int {
	return int(start.sr)
}

func (start interFrameSearchStart) nearSADIndexInt() int {
	return int(start.nearSADIndex)
}

func (search interAnalysisSearchConfig) adjustedForImprovedMVStart(start interFrameSearchStart) interAnalysisSearchConfig {
	if !start.ok() {
		return search
	}
	stepParam := start.searchRange() + int(search.fullPixelSpeedAdjust)
	if stepParam > int(search.fullPixelSearchParam) {
		if stepParam >= interFrameMaxMVSearchSteps {
			stepParam = interFrameMaxMVSearchSteps - 1
		}
		search.fullPixelSearchParam = int8(stepParam)
		search.fullPixelFurtherSteps = int8(libvpxInterFrameFurtherSteps(int(search.fullPixelSpeed), stepParam))
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
	if !search.improvedMVPrediction || refFrame == vp8common.IntraFrame {
		return interFrameSearchStart{}
	}
	var slots [8]improvedInterFrameMVSlot
	slotCount := 3
	signBias := e.interFrameSignBias()
	slots[0].fillCurrent(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above, signBias)
	slots[1].fillCurrent(src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left, signBias)
	slots[2].fillCurrent(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft, signBias)
	lastModesAvailable := e.lastFrameInterModesValid && len(e.lastFrameInterModes) >= mbRows*mbCols
	includeLastSlots := (lastModesAvailable || e.lastCodedFrameType != vp8common.KeyFrame) && mbRows > 0 && mbCols > 0
	if includeLastSlots {
		slotCount = 8
		if lastModesAvailable {
			slots[3].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
			slots[4].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
			slots[5].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
			slots[6].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
			slots[7].fillLast(src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
		} else {
			slots[3].fillLastIntraSentinel(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
			slots[4].fillLastIntraSentinel(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
			slots[5].fillLastIntraSentinel(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
			slots[6].fillLastIntraSentinel(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
			slots[7].fillLastIntraSentinel(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
		}
	}
	biasImprovedInterFrameMVSlots(&slots, slotCount, refFrame, signBias, mbRow, mbCol, mbRows, mbCols)
	order := improvedInterFrameMVSlotOrder(slots, slotCount)
	// Both slots and order are length 8 (pow2). rank is bounded by
	// slotCount ≤ 8 and each order[rank] cell is [0,8) by construction
	// (the insertion sort writes only [0, slotCount) indices). Mask
	// with 7 to elide bounds checks on both indexed loads.
	for rank := 0; rank < slotCount; rank++ {
		slot := slots[order[rank&7]&7]
		if slot.refFrame == refFrame {
			sr := 2
			if rank < 3 {
				sr = 3
			}
			return newInterFrameSearchStart(slot.mv, sr, order[rank&7])
		}
	}
	mv := improvedInterFrameMVMedian(slots, slotCount)
	return newInterFrameSearchStart(mv, 0, -1)
}

func (slot *improvedInterFrameMVSlot) fillCurrent(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode, signBias [vp8common.MaxRefFrames]bool) {
	// Mirror libvpx's vp8_mv_pred neighbor table for the current frame: a
	// nil pointer (border MB) corresponds to libvpx's calloc-zeroed
	// mode_info sentinel row/column where ref_frame == INTRA_FRAME and
	// mv == 0, and vp8_cal_sad sets the matching near_sad entry to INT_MAX.
	// In-frame intra neighbors still receive a real near_sad value; they are
	// skipped later by the ref-frame match but still affect the matched
	// neighbor's SAD rank and therefore the search range.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if mode == nil || refMbRow < 0 || refMbCol < 0 {
		return
	}
	slot.refFrame = convertInterFrameReference(mode)
	// uint range check folds (refFrame > IntraFrame && < MaxRefFrames)
	// into one compare since IntraFrame==0 and MaxRefFrames is a small
	// positive constant.
	if uint(slot.refFrame)-1 < uint(vp8common.MaxRefFrames-1) {
		slot.signBias = signBias[slot.refFrame]
	}
	if slot.refFrame == vp8common.IntraFrame {
		// libvpx leaves near_mvs[vcnt] at zero when the neighbor is intra; do
		// the same here regardless of any stale MV field on the mode entry.
		slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
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
	// vp8_cal_sad sets the matching near_sad entry to INT_MAX. In-frame
	// previous-frame intra slots still get their real last-frame SAD and can
	// change the matched slot's rank.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if uint(refMbRow) >= uint(mbRows) || uint(refMbCol) >= uint(mbCols) {
		return
	}
	index := refMbRow*mbCols + refMbCol
	if uint(index) >= uint(len(modes)) {
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
		slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
		return
	}
	slot.mv = mode.MV
	slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func (slot *improvedInterFrameMVSlot) fillLastIntraSentinel(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, mbRows int, mbCols int, refMbRow int, refMbCol int) {
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if uint(refMbRow) >= uint(mbRows) || uint(refMbCol) >= uint(mbCols) {
		return
	}
	slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func biasImprovedInterFrameMVSlots(slots *[8]improvedInterFrameMVSlot, count int, refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool, mbRow int, mbCol int, mbRows int, mbCols int) {
	if slots == nil || refFrame <= vp8common.IntraFrame || refFrame >= vp8common.MaxRefFrames {
		return
	}
	targetBias := signBias[refFrame]
	limit := min(count, len(slots))
	for i := range limit {
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
	return min(max(v, lowEdge-(16<<3)), highEdge+(16<<3))
}

func improvedInterFrameMVSlotOrder(slots [8]improvedInterFrameMVSlot, count int) [8]int {
	var order [8]int
	limit := min(count, len(order))
	for i := range limit {
		order[i] = i
	}
	for i := 1; i < limit; i++ {
		// Every order[k] index is bounded to [0, limit) ⊂ [0, 8) by the
		// init loop above. j is bounded to [0, i) ⊂ [0, 8) and j+1 ≤ i
		// ≤ 8, so j+1 ∈ [1, 8]. AND-mask with 7 elides bounds checks
		// on the per-iter slots/order accesses without changing
		// semantics.
		idx := order[i&7]
		sad := slots[idx&7].sad
		j := i - 1
		for ; j >= 0 && sad < slots[order[j&7]&7].sad; j-- {
			order[(j+1)&7] = order[j&7]
		}
		order[(j+1)&7] = idx
	}
	return order
}

func improvedInterFrameMVMedian(slots [8]improvedInterFrameMVSlot, count int) vp8enc.MotionVector {
	if count <= 0 {
		return vp8enc.MotionVector{}
	}
	var rows [8]int
	var cols [8]int
	// limit ∈ [0, 8] by construction (min with array length); using
	// limit instead of count for both the sort range and the median
	// index avoids a per-call IsSliceInBounds on rows[:count]/
	// cols[:count] (count is unbounded from the compiler's view) and
	// also closes a latent panic when count > 8.
	limit := min(count, len(slots))
	for i := range limit {
		rows[i] = int(slots[i].mv.Row)
		cols[i] = int(slots[i].mv.Col)
	}
	insertionSortInts(rows[:limit])
	insertionSortInts(cols[:limit])
	return vp8enc.MotionVector{Row: int16(rows[limit/2]), Col: int16(cols[limit/2])}
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
