package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func libvpxInterReferenceSearchOrder(refs []interAnalysisReference, refCount int) [4]int {
	order := [4]int{-1, -1, -1, -1}
	searchSlot := 1
	for refIndex := 0; refIndex < refCount && refIndex < len(refs) && searchSlot < len(order); refIndex++ {
		if refs[refIndex].Img == nil {
			continue
		}
		switch refs[refIndex].Frame {
		case vp8common.LastFrame, vp8common.GoldenFrame, vp8common.AltRefFrame:
			order[searchSlot] = refIndex
			searchSlot++
		}
	}
	return order
}

func interReferenceBySearchSlot(refs []interAnalysisReference, searchOrder [4]int, refSlot int) (interAnalysisReference, int, bool) {
	if refSlot <= 0 || refSlot >= len(searchOrder) {
		return interAnalysisReference{}, 0, false
	}
	refIndex := searchOrder[refSlot]
	if refIndex < 0 || refIndex >= len(refs) || refs[refIndex].Img == nil {
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
	slot := 0
	if refFrame >= 0 && int(refFrame) < len(signBias) {
		slot = interModeSignBiasSlot(signBias[refFrame])
	}
	return slot
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
	state.nearest[slot] = nearest
	state.near[slot] = near
	state.best[slot] = best
	opp := 1 - slot
	state.nearest[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -nearest.Row, Col: -nearest.Col}, mbRow, mbCol, mbRows, mbCols)
	state.near[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -near.Row, Col: -near.Col}, mbRow, mbCol, mbRows, mbCols)
	state.best[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -best.Row, Col: -best.Col}, mbRow, mbCol, mbRows, mbCols)
	return state
}
