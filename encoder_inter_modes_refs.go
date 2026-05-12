package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func interReferenceSearchOrder(refs []interAnalysisReference, refCount int) [4]int {
	order := [4]int{-1, -1, -1, -1}
	searchSlot := 1
	// Hoist the min(refCount, len(refs)) bound out of the loop condition.
	refLimit := min(refCount, len(refs))
	for refIndex := 0; refIndex < refLimit && searchSlot < len(order); refIndex++ {
		if refs[refIndex].Img == nil {
			continue
		}
		switch refs[refIndex].Frame {
		case vp8common.LastFrame, vp8common.GoldenFrame, vp8common.AltRefFrame:
			// searchSlot is in [1,4) by the loop condition above; AND-mask
			// with 3 elides the bounds check on the [4]int order array.
			order[searchSlot&3] = refIndex
			searchSlot++
		}
	}
	return order
}

func interReferenceBySearchSlot(refs []interAnalysisReference, searchOrder [4]int, refSlot int) (interAnalysisReference, int, bool) {
	// uint(refSlot-1) folds (refSlot <= 0) and (refSlot >= len) into one
	// branch: refSlot=0 → -1 wraps to huge, refSlot=len → len-1 >= len-1
	// (false on the upper bound for the original test); use len-1 as
	// inclusive max instead.
	if uint(refSlot-1) >= uint(len(searchOrder)-1) {
		return interAnalysisReference{}, 0, false
	}
	refIndex := searchOrder[refSlot]
	if uint(refIndex) >= uint(len(refs)) || refs[refIndex].Img == nil {
		return interAnalysisReference{}, 0, false
	}
	return refs[refIndex], refIndex, true
}

type interModeMVSlots struct {
	nearest [2]vp8enc.MotionVector
	near    [2]vp8enc.MotionVector
	best    [2]vp8enc.MotionVector
	counts  vp8enc.InterModeCounts
}

func interModeSignBiasSlot(bias bool) int {
	if bias {
		return 1
	}
	return 0
}

func interModeSignBiasSlotForReference(refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool) int {
	// Single uint range check folds the refFrame >= 0 and < len guards.
	if uint(refFrame) >= uint(len(signBias)) {
		return 0
	}
	return interModeSignBiasSlot(signBias[refFrame])
}

func (e *VP8Encoder) interModeMVSlots(
	refs []interAnalysisReference, refSearchOrder [4]int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	mbRow int, mbCol int, mbRows int, mbCols int,
) interModeMVSlots {
	var state interModeMVSlots
	baseRef, _, ok := interReferenceBySearchSlot(refs, refSearchOrder, 1)
	if !ok {
		return state
	}
	signBias := e.interFrameSignBias()
	slot := interModeSignBiasSlotForReference(baseRef.Frame, signBias)
	nearest, near := interAnalysisReferenceMotionPredictorsWithSignBias(baseRef.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, signBias)
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, baseRef.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	state.counts = vp8enc.InterFrameModeCounts(above, left, aboveLeft, baseRef.Frame, signBias)
	// slot from interModeSignBiasSlotForReference is 0 or 1; AND-mask
	// with 1 elides the bounds check on the [2]MotionVector slot arrays
	// for both the active slot and its opposite.
	state.nearest[slot&1] = nearest
	state.near[slot&1] = near
	state.best[slot&1] = best
	opp := (1 - slot) & 1
	state.nearest[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -nearest.Row, Col: -nearest.Col}, mbRow, mbCol, mbRows, mbCols)
	state.near[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -near.Row, Col: -near.Col}, mbRow, mbCol, mbRows, mbCols)
	state.best[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -best.Row, Col: -best.Col}, mbRow, mbCol, mbRows, mbCols)
	return state
}
