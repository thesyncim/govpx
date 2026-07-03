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
		// libvpx rdopt.c:2086 (RD path, vp8_rd_pick_inter_mode NEWMV):
		//
		//   further_steps = (cpi->sf.max_step_search_steps - 1) - step_param;
		//
		// libvpx pickinter.c:1005-1008 (non-RD path, vp8_pick_inter_mode
		// NEWMV):
		//
		//   further_steps = (cpi->Speed >= 8)
		//       ? 0
		//       : (cpi->sf.max_step_search_steps - 1 - step_param);
		//
		// govpx's fullPixelFinalRefine flag is the RD-vs-picker selector
		// (set from interAnalysisUsesRDModeDecision in
		// interAnalysisSearchConfig). The RD path is independent of
		// cpi->Speed; only the pickinter path applies the Speed>=8 short-
		// circuit. Routing the raw cpi->Speed through
		// libvpxInterFrameFurtherSteps here would silently cap
		// further_steps to 0 in the BestQuality+cpu_used>=8 RD-path cohort
		// (sr>0 from improved_mv_pred), diverging frame-N MB modes from
		// libvpx vp8_rd_pick_inter_mode (fuzz seed
		// regression_option_grid_022b3ed5).
		if search.fullPixelFinalRefine {
			further := max(interFrameMaxMVSearchSteps-1-stepParam, 0)
			search.fullPixelFurtherSteps = int8(further)
		} else {
			search.fullPixelFurtherSteps = int8(libvpxInterFrameFurtherSteps(int(search.fullPixelSpeed), stepParam))
		}
	}
	return search
}

type improvedInterFrameMVSlot struct {
	mv       vp8enc.MotionVector
	refFrame vp8common.MVReferenceFrame
	signBias bool
	sad      int
}

// improvedInterFrameNearSADCache mirrors libvpx pickinter.c's saddone +
// near_sad[8] pair: vp8_cal_sad computes the neighbor SAD table once per
// macroblock and every reference frame's NEWMV vp8_mv_pred call reuses it.
// The SAD values depend only on the source MB, the reconstructed
// current-frame neighbors and the previous frame - never on the reference
// frame being searched - so the fast picker keeps one cache per MB in its
// loop context.
type improvedInterFrameNearSADCache struct {
	sad [8]int
	set bool
}

func currentNeighborNearSAD(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode) int {
	if mode == nil || refMbRow < 0 || refMbCol < 0 {
		return maxInt()
	}
	return macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func lastNeighborNearSAD(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, mbRows int, mbCols int, refMbRow int, refMbCol int) int {
	if uint(refMbRow) >= uint(mbRows) || uint(refMbCol) >= uint(mbCols) {
		return maxInt()
	}
	return macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func (e *VP8Encoder) improvedInterFrameSearchStart(
	src vp8enc.SourceImage, refFrame vp8common.MVReferenceFrame,
	mbRow int, mbCol int, mbRows int, mbCols int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	sadCache *improvedInterFrameNearSADCache,
) interFrameSearchStart {
	if !search.improvedMVPrediction || refFrame == vp8common.IntraFrame {
		return interFrameSearchStart{}
	}
	signBias := e.interFrameSignBias()
	mbCount := mbRows * mbCols
	lastRefsAvailable := e.lastFrameInterModesValid && len(e.lastFrameInterModeRefs) >= mbCount
	includeLastSlots := (lastRefsAvailable || e.lastCodedFrameType != vp8common.KeyFrame) && mbRows > 0 && mbCols > 0
	var localSADs improvedInterFrameNearSADCache
	cache := sadCache
	if cache == nil {
		cache = &localSADs
	}
	if !cache.set {
		cache.sad[0] = currentNeighborNearSAD(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above)
		cache.sad[1] = currentNeighborNearSAD(src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left)
		cache.sad[2] = currentNeighborNearSAD(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft)
		if includeLastSlots {
			cache.sad[3] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
			cache.sad[4] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
			cache.sad[5] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
			cache.sad[6] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
			cache.sad[7] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
		}
		cache.set = true
	}
	var slots [8]improvedInterFrameMVSlot
	slotCount := 3
	slots[0].fillCurrent(cache.sad[0], mbRow-1, mbCol, above, signBias)
	slots[1].fillCurrent(cache.sad[1], mbRow, mbCol-1, left, signBias)
	slots[2].fillCurrent(cache.sad[2], mbRow-1, mbCol-1, aboveLeft, signBias)
	if includeLastSlots {
		slotCount = 8
		if lastRefsAvailable {
			slots[3].fillLastRef(cache.sad[3], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow, mbCol)
			slots[4].fillLastRef(cache.sad[4], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow-1, mbCol)
			slots[5].fillLastRef(cache.sad[5], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow, mbCol-1)
			slots[6].fillLastRef(cache.sad[6], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow, mbCol+1)
			slots[7].fillLastRef(cache.sad[7], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow+1, mbCol)
		} else {
			slots[3] = improvedInterFrameMVSlot{sad: cache.sad[3]}
			slots[4] = improvedInterFrameMVSlot{sad: cache.sad[4]}
			slots[5] = improvedInterFrameMVSlot{sad: cache.sad[5]}
			slots[6] = improvedInterFrameMVSlot{sad: cache.sad[6]}
			slots[7] = improvedInterFrameMVSlot{sad: cache.sad[7]}
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

func (slot *improvedInterFrameMVSlot) fillCurrent(sad int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode, signBias [vp8common.MaxRefFrames]bool) {
	// Mirror libvpx's vp8_mv_pred neighbor table for the current frame: a
	// nil pointer (border MB) corresponds to libvpx's calloc-zeroed
	// mode_info sentinel row/column where ref_frame == INTRA_FRAME and
	// mv == 0, and vp8_cal_sad sets the matching near_sad entry to INT_MAX
	// (the caller passes that sentinel through sad). In-frame intra
	// neighbors still receive a real near_sad value; they are skipped later
	// by the ref-frame match but still affect the matched neighbor's SAD
	// rank and therefore the search range.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if mode == nil || refMbRow < 0 || refMbCol < 0 {
		return
	}
	slot.refFrame = vp8enc.ConvertInterFrameReference(mode)
	// uint range check folds (refFrame > IntraFrame && < MaxRefFrames)
	// into one compare since IntraFrame==0 and MaxRefFrames is a small
	// positive constant.
	if uint(slot.refFrame)-1 < uint(vp8common.MaxRefFrames-1) {
		slot.signBias = signBias[slot.refFrame]
	}
	slot.sad = sad
	if slot.refFrame == vp8common.IntraFrame {
		// libvpx leaves near_mvs[vcnt] at zero when the neighbor is intra; do
		// the same here regardless of any stale MV field on the mode entry.
		return
	}
	slot.mv = mode.MV
}

func (slot *improvedInterFrameMVSlot) fillLastRef(sad int, refs []vp8enc.InterFrameMVRef, mbRows int, mbCols int, refMbRow int, refMbCol int) {
	// Same previous-frame sentinel semantics as fillLast, but backed by the
	// compact lfmv/lf_ref_frame sidecar captured at frame refresh.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if uint(refMbRow) >= uint(mbRows) || uint(refMbCol) >= uint(mbCols) {
		return
	}
	index := refMbRow*mbCols + refMbCol
	if uint(index) >= uint(len(refs)) {
		return
	}
	ref := refs[index]
	slot.refFrame = ref.RefFrame
	slot.signBias = ref.SignBias
	slot.sad = sad
	if slot.refFrame == vp8common.IntraFrame {
		return
	}
	slot.mv = ref.MV
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
