package govpx

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func selectInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearch(src, ref, mbRow, mbCol, 0, 0, bestRefMV, qIndex, defaultInterAnalysisSearchConfig(), mvProbs)
}

func selectInterFrameMotionVectorWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearchStart(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, interFrameSearchStart{}, mvProbs)
}

// selectInterFrameMotionVectorWithSearchStart mirrors libvpx pickinter.c's
// fast NEWMV path: integer-pel search followed by unconditional acceptance of
// the fractional refinement (find_fractional_mv_step). libvpx uses bilinear
// variance during the subpel search and trusts that result; second-guessing
// it with a 6-tap SSE recompute biases us toward integer-pel even when the
// bilinear-best candidate scores lower distortion AND lower MV-rate, which
// is the realtime-cbr cpu0/4/8 NEWMV mv_row divergence at frame=2 mb=(0,3),
// (2,3) on the 64x64 panning fixture.
func selectInterFrameMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearchStartAndStats(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs, nil)
}

func selectInterFrameMotionVectorWithSearchStartAndStats(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8, stats *interFrameMotionSearchStats) (vp8enc.MotionVector, int) {
	var mvCosts vp8enc.MotionVectorCostTables
	var mvCostPtr *vp8enc.MotionVectorCostTables
	if mvProbs != nil {
		mvCosts.Build(mvProbs)
		mvCostPtr = &mvCosts
	}
	return interFrameMotionVectorSearch{
		src:       src,
		ref:       ref,
		mbRow:     mbRow,
		mbCol:     mbCol,
		mbRows:    mbRows,
		mbCols:    mbCols,
		bestRefMV: bestRefMV,
		qIndex:    qIndex,
		search:    search,
		start:     start,
		mvProbs:   mvProbs,
		mvCosts:   mvCostPtr,
		stats:     stats,
	}.selectFast().motionCost()
}

func selectRDInterFrameMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectRDInterFrameMotionVectorWithSearchStartAndStats(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs, nil)
}

func selectRDInterFrameMotionVectorWithSearchStartAndStats(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8, stats *interFrameMotionSearchStats) (vp8enc.MotionVector, int) {
	var mvCosts vp8enc.MotionVectorCostTables
	var mvCostPtr *vp8enc.MotionVectorCostTables
	if mvProbs != nil {
		mvCosts.Build(mvProbs)
		mvCostPtr = &mvCosts
	}
	return interFrameMotionVectorSearch{
		src:       src,
		ref:       ref,
		mbRow:     mbRow,
		mbCol:     mbCol,
		mbRows:    mbRows,
		mbCols:    mbCols,
		bestRefMV: bestRefMV,
		qIndex:    qIndex,
		search:    search,
		start:     start,
		mvProbs:   mvProbs,
		mvCosts:   mvCostPtr,
		stats:     stats,
	}.selectRD().motionCost()
}

type interFrameMotionVectorSearch struct {
	src       vp8enc.SourceImage
	ref       *vp8common.Image
	mbRow     int
	mbCol     int
	mbRows    int
	mbCols    int
	bestRefMV vp8enc.MotionVector
	qIndex    int
	search    interAnalysisSearchConfig
	start     interFrameSearchStart
	mvProbs   *[2][vp8tables.MVPCount]uint8
	mvCosts   *vp8enc.MotionVectorCostTables
	stats     *interFrameMotionSearchStats
}

type interFrameMotionVectorSearchResult struct {
	mv        vp8enc.MotionVector
	cost      int
	variance  int
	sse       int
	haveError bool
}

func (r interFrameMotionVectorSearchResult) motionCost() (vp8enc.MotionVector, int) {
	return r.mv, r.cost
}

func (s interFrameMotionVectorSearch) selectFast() interFrameMotionVectorSearchResult {
	best, bestCost := s.fullPixel()
	if bestCost == 0 {
		return interFrameMotionVectorSearchResult{mv: best, cost: bestCost, haveError: true}
	}
	subpel := s.subpixel(best)
	if refined, refinedCost, variance, sse, ok := subpel.refine(); ok {
		return interFrameMotionVectorSearchResult{mv: refined, cost: refinedCost, variance: variance, sse: sse, haveError: true}
	}
	return interFrameMotionVectorSearchResult{mv: best, cost: bestCost}
}

func (s interFrameMotionVectorSearch) selectRD() interFrameMotionVectorSearchResult {
	best, bestCost := s.fullPixel()
	subpel := s.subpixel(best)
	if refined, refinedCost, variance, sse, ok := subpel.refine(); ok {
		return interFrameMotionVectorSearchResult{mv: refined, cost: refinedCost, variance: variance, sse: sse, haveError: true}
	}
	return interFrameMotionVectorSearchResult{mv: best, cost: bestCost}
}

func (s interFrameMotionVectorSearch) fullPixel() (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsAndStats(s.src, s.ref, s.mbRow, s.mbCol, s.mbRows, s.mbCols, s.bestRefMV, s.qIndex, s.search, s.start, s.mvProbs, s.stats)
}

func (s interFrameMotionVectorSearch) subpixel(best vp8enc.MotionVector) interFrameSubpixelSearch {
	return interFrameSubpixelSearch{
		src:       s.src,
		ref:       s.ref,
		mbRow:     s.mbRow,
		mbCol:     s.mbCol,
		best:      best,
		bestRefMV: s.bestRefMV,
		qIndex:    s.qIndex,
		search:    s.search,
		mvProbs:   s.mvProbs,
		mvCosts:   s.mvCosts,
		stats:     s.stats,
	}
}

// selectInterFrameFullPixelMotionVector centers the integer-pel search at
// bestRefMV (libvpx pickinter.c uses `mvp_full = bestRefMV >> 3`) and charges
// the candidate's MV-cost against bestRefMV instead of (0,0). Standalone
// callers keep the exhaustive sweep for existing coverage; encoder mode
// decision uses libvpx's NSTEP/hex speed-feature paths.
func selectInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearch(src, ref, mbRow, mbCol, 0, 0, bestRefMV, qIndex, defaultInterAnalysisSearchConfig())
}

func selectInterFrameFullPixelMotionVectorWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStart(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, interFrameSearchStart{})
}

func selectInterFrameFullPixelMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &vp8tables.DefaultMVContext)
}

func selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsAndStats(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs, nil)
}

func selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsAndStats(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8, stats *interFrameMotionSearchStats) (vp8enc.MotionVector, int) {
	searchStart := bestRefMV
	if start.ok && search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		searchStart = start.mv
		search = search.adjustedForImprovedMVStart(start)
	}
	centerRow := int(searchStart.Row) & ^7
	centerCol := int(searchStart.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	bounds := interFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	if search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		best = bounds.clampEighth(best)
	}
	searcher := newFullPelMotionSearch(src, ref, mbRow, mbCol, bestRefMV, qIndex, bounds, mvProbs, stats)
	bestWalkCost := searcher.walkCost(best, maxInt())
	if bestWalkCost == 0 {
		return best, searcher.cost(best)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchNstep {
		return searcher.nstep(best, bestWalkCost, search)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchDiamond {
		return searcher.diamond(best, bestWalkCost, search)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchHex {
		return searcher.hex(best, bestWalkCost)
	}
	return searcher.exhaustive(best, bestWalkCost)
}

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
	fillImprovedInterFrameCurrentMVSlot(&slots[0], src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above, signBias)
	fillImprovedInterFrameCurrentMVSlot(&slots[1], src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left, signBias)
	fillImprovedInterFrameCurrentMVSlot(&slots[2], src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft, signBias)
	if e.lastFrameInterModesValid && len(e.lastFrameInterModes) >= mbRows*mbCols && mbRows > 0 && mbCols > 0 {
		slotCount = 8
		fillImprovedInterFrameLastMVSlot(&slots[3], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
		fillImprovedInterFrameLastMVSlot(&slots[4], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
		fillImprovedInterFrameLastMVSlot(&slots[5], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
		fillImprovedInterFrameLastMVSlot(&slots[6], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
		fillImprovedInterFrameLastMVSlot(&slots[7], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
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

func fillImprovedInterFrameCurrentMVSlot(slot *improvedInterFrameMVSlot, src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode, signBias [vp8common.MaxRefFrames]bool) {
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

func fillImprovedInterFrameLastMVSlot(slot *improvedInterFrameMVSlot, src vp8enc.SourceImage, img *vp8common.Image, modes []vp8enc.InterFrameMacroblockMode, modeBias []bool, srcMbRow int, srcMbCol int, mbRows int, mbCols int, refMbRow int, refMbCol int) {
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

func selectInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(src, ref, mbRow, mbCol, block, width, height, bestRefMV, bestRefMV, qIndex)
}

func selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, searchCenter vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src, ref, mbRow, mbCol, block, width, height, searchCenter, bestRefMV, qIndex, 0, true)
}

func selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, searchCenter vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, stepParam int, fullSearchFallback bool) (vp8enc.MotionVector, int) {
	centerRow := int(searchCenter.Row) & ^7
	centerCol := int(searchCenter.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := interFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	best = bounds.clampEighth(best)
	bestCost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	best, bestCost = nstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestCost, bestRefMV, qIndex, bounds, stepParam)
	if fullSearchFallback && splitMotionFullSearchFallbackNeeded(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex) {
		best, bestCost = fullSearchInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, bounds.clampEighth(vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}), best, bestCost, bestRefMV, qIndex, bounds, interFrameFullPixelSearchRadius)
	}
	return best, bestCost
}

func nstepInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, stepParam int) (vp8enc.MotionVector, int) {
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}
	furtherSteps := (interFrameMaxMVSearchSteps - 1) - stepParam
	result := diamondNstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam)
	best := result.mv
	bestCost := result.cost
	n := result.num00
	num00 := 0
	for n < furtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := diamondNstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam+n)
		num00 = candidate.num00
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	return best, bestCost
}

func diamondNstepInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchParam int) interFrameNstepSearchResult {
	sites := &interFrameNstepSites
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
	best := center
	bestWalkCost := centerWalkCost
	start := center
	startIndex := searchParam * 8
	totalSteps := (len(sites) / 8) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	for range totalSteps {
		for range 8 {
			site := sites[startIndex+i]
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
				cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, bestRefMV, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = i
				}
			}
			i++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			best = vp8enc.MotionVector{
				Row: int16(int(best.Row) + int(site.Row)*interFrameMVFullPixelStep),
				Col: int16(int(best.Col) + int(site.Col)*interFrameMVFullPixelStep),
			}
			lastSite = bestSite
		} else if best == start {
			num00++
		}
	}
	return interFrameNstepSearchResult{mv: best, cost: bestWalkCost, num00: num00}
}

func splitMotionFullSearchFallbackNeeded(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) bool {
	shift := splitMotionSegmentationSSEShift(width, height)
	cost, ok := interMotionSplitBlockFullPixelVarianceCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	return ok && (cost>>shift) > interFrameSplitMVFullSearchThreshold
}

func interMotionSplitBlockFullPixelVarianceCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) (int, bool) {
	variance, _, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, int(mv.Row)/2, int(mv.Col)/2)
	if !ok {
		return maxInt(), false
	}
	return variance + interMotionSplitBlockSearchVectorCost(mv, bestRefMV, qIndex), true
}

func splitMotionSegmentationSSEShift(width int, height int) int {
	switch {
	case width == 16 && height == 8:
		return 3
	case width == 8 && height == 16:
		return 3
	case width == 8 && height == 8:
		return 2
	default:
		return 0
	}
}

func fullSearchInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, best vp8enc.MotionVector, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, distance int) (vp8enc.MotionVector, int) {
	refRow := int(center.Row) >> 3
	refCol := int(center.Col) >> 3
	rowMin := refRow - distance
	rowMax := refRow + distance
	colMin := refCol - distance
	colMax := refCol + distance
	if rowMin < bounds.rowMin {
		rowMin = bounds.rowMin
	}
	if rowMax > bounds.rowMax {
		rowMax = bounds.rowMax
	}
	if colMin < bounds.colMin {
		colMin = bounds.colMin
	}
	if colMax > bounds.colMax {
		colMax = bounds.colMax
	}
	for row := rowMin; row < rowMax; row++ {
		for col := colMin; col < colMax; col++ {
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, bestRefMV, qIndex)
			if cost < bestCost {
				best = mv
				bestCost = cost
			}
		}
	}
	return best, bestCost
}

func refineInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	switch search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return stepInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, true, mvProbs)
	case interAnalysisFractionalSearchHalf:
		return stepInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, false, mvProbs)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, false
	default:
		return iterativeInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, mvProbs)
	}
}

func stepInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, quarter bool, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	bestRow := (int(best.Row) >> 3) * 4
	bestCol := (int(best.Col) >> 3) * 4
	bestCost, ok := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, bestRow, bestCol, bestRefMV, qIndex, mvProbs)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost, bestRow, bestCol = stepInterFrameSplitBlockSubpixelDirectionalSearch(src, ref, mbRow, mbCol, block, width, height, bestRow, bestCol, 2, bestCost, bestRefMV, qIndex, mvProbs)
	if quarter {
		bestCost, bestRow, bestCol = stepInterFrameSplitBlockSubpixelDirectionalSearch(src, ref, mbRow, mbCol, block, width, height, bestRow, bestCol, 1, bestCost, bestRefMV, qIndex, mvProbs)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func stepInterFrameSplitBlockSubpixelDirectionalSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, startRow int, startCol int, step int, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow, startCol-step, bestRefMV, qIndex, mvProbs)
	rightCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow, startCol+step, bestRefMV, qIndex, mvProbs)
	upCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow-step, startCol, bestRefMV, qIndex, mvProbs)
	downCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow+step, startCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, leftCost, startRow, startCol-step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, rightCost, startRow, startCol+step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, upCost, startRow-step, startCol)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, downCost, startRow+step, startCol)

	diagRow := startRow - step
	if upCost >= downCost {
		diagRow = startRow + step
	}
	diagCol := startCol - step
	if leftCost >= rightCost {
		diagCol = startCol + step
	}
	diagCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, diagRow, diagCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, diagCost, diagRow, diagCol)
	return bestCost, bestRow, bestCol
}

func iterativeInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	bestDist, _, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCost(bestMV, bestRefMV, qIndex, mvProbs)
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := interFrameSubpelSearchBoundsFor(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	cand := func(r, c int) int {
		if !bounds.contains(r, c) {
			return maxInt()
		}
		cost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, r, c, bestRefMV, qIndex, mvProbs)
		return cost
	}

	for range 3 {
		leftCost := cand(tr, tc-2)
		rightCost := cand(tr, tc+2)
		upCost := cand(tr-2, tc)
		downCost := cand(tr+2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftCost := cand(tr, tc-1)
		rightCost := cand(tr, tc+1)
		upCost := cand(tr-1, tc)
		downCost := cand(tr+1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func splitBlockSubpixelMotionSearchCandidateCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, row int, col int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, bool) {
	dist, _, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, row, col)
	if !ok {
		return maxInt(), false
	}
	mv := vp8enc.MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
	// Iterative subpel candidate cost: libvpx CHECK_BETTER uses the MVC
	// macro (1/4-pel signed index built from `(mv>>1) - (ref>>1)`), not
	// mv_err_cost.
	return dist + interMotionSubpelCandidateVectorCost(mv, bestRefMV, qIndex, mvProbs), true
}

func splitBlockSubpixelVarianceForQuarterMV(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, row int, col int) (int, int, bool) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	if baseY < 0 || baseX < 0 || baseY+height > src.Height || baseX+width > src.Width {
		return 0, 0, false
	}
	refBaseY := baseY + (row >> 2)
	refBaseX := baseX + (col >> 2)
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	return splitBlockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset)
}

type interFrameFullPixelBounds struct {
	rowMin int
	rowMax int
	colMin int
	colMax int
}

func interFrameFullPixelSearchBounds(bestRefMV vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) interFrameFullPixelBounds {
	bounds := interFrameFullPixelBounds{
		rowMin: ((int(bestRefMV.Row) + 7) >> 3) - interFrameMaxFullPelVal,
		rowMax: (int(bestRefMV.Row) >> 3) + interFrameMaxFullPelVal,
		colMin: ((int(bestRefMV.Col) + 7) >> 3) - interFrameMaxFullPelVal,
		colMax: (int(bestRefMV.Col) >> 3) + interFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := interFrameUMVBorderPixels - 16
		rowMin := -((mbRow * 16) + umv)
		rowMax := ((mbRows - 1 - mbRow) * 16) + umv
		if bounds.rowMin < rowMin {
			bounds.rowMin = rowMin
		}
		if bounds.rowMax > rowMax {
			bounds.rowMax = rowMax
		}
	}
	if mbCols > 0 {
		umv := interFrameUMVBorderPixels - 16
		colMin := -((mbCol * 16) + umv)
		colMax := ((mbCols - 1 - mbCol) * 16) + umv
		if bounds.colMin < colMin {
			bounds.colMin = colMin
		}
		if bounds.colMax > colMax {
			bounds.colMax = colMax
		}
	}
	return bounds
}

func interFrameUMVFullPixelInRange(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if mbRows <= 0 || mbCols <= 0 {
		return true
	}
	umv := interFrameUMVBorderPixels - 16
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	rowMin := -((mbRow * 16) + umv)
	rowMax := ((mbRows - 1 - mbRow) * 16) + umv
	colMin := -((mbCol * 16) + umv)
	colMax := ((mbCols - 1 - mbCol) * 16) + umv
	return row >= rowMin && row <= rowMax && col >= colMin && col <= colMax
}

func clampInterMotionVectorToModeEdges(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) vp8enc.MotionVector {
	if mbRows <= 0 || mbCols <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return vp8enc.MotionVector{
		Row: int16(clampInterMotionVectorComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterMotionVectorComponent(int(mv.Col), left, right)),
	}
}

func clampInterMotionVectorComponent(v int, lowEdge int, highEdge int) int {
	if v < lowEdge-(16<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(16<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func (b interFrameFullPixelBounds) containsFullPel(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

func (b interFrameFullPixelBounds) containsFullPelStrict(row int, col int) bool {
	return row > b.rowMin && row < b.rowMax && col > b.colMin && col < b.colMax
}

func (b interFrameFullPixelBounds) clampEighth(mv vp8enc.MotionVector) vp8enc.MotionVector {
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	if row < b.rowMin {
		row = b.rowMin
	} else if row > b.rowMax {
		row = b.rowMax
	}
	if col < b.colMin {
		col = b.colMin
	} else if col > b.colMax {
		col = b.colMax
	}
	return vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
}

type interFrameSubpixelSearch struct {
	src       vp8enc.SourceImage
	ref       *vp8common.Image
	mbRow     int
	mbCol     int
	best      vp8enc.MotionVector
	bestRefMV vp8enc.MotionVector
	qIndex    int
	search    interAnalysisSearchConfig
	mvProbs   *[2][vp8tables.MVPCount]uint8
	mvCosts   *vp8enc.MotionVectorCostTables
	stats     *interFrameMotionSearchStats
}

type subpelCandidateEval struct {
	cost     int
	variance int
	sse      int
	ok       bool
}

func (s *interFrameSubpixelSearch) refine() (vp8enc.MotionVector, int, int, int, bool) {
	switch s.search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return s.step(true)
	case interAnalysisFractionalSearchHalf:
		return s.step(false)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, 0, 0, false
	default:
		return s.iterative()
	}
}

func (s *interFrameSubpixelSearch) step(quarter bool) (vp8enc.MotionVector, int, int, int, bool) {
	if int(s.best.Row)&7 != 0 || int(s.best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	bestRow := (int(s.best.Row) >> 3) * 4
	bestCol := (int(s.best.Col) >> 3) * 4
	subCtx, subCtxOK := newSubpelSearchCtx(s.src, s.ref, s.mbRow, s.mbCol)
	if !subCtxOK {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	errorPerBit := libvpxErrorPerBit(s.qIndex)
	refRow4 := int(s.bestRefMV.Row) >> 1
	refCol4 := int(s.bestRefMV.Col) >> 1
	bestEval := s.candidateEval(&subCtx, bestRow, bestCol, refRow4, refCol4, errorPerBit)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	bestEval, bestRow, bestCol = s.directionalStep(&subCtx, bestRow, bestCol, 2, bestEval, refRow4, refCol4, errorPerBit)
	if quarter {
		bestEval, bestRow, bestCol = s.directionalStep(&subCtx, bestRow, bestCol, 1, bestEval, refRow4, refCol4, errorPerBit)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, s.bestRefMV) {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	return finalMV, bestEval.cost, bestEval.variance, bestEval.sse, true
}

func (s *interFrameSubpixelSearch) directionalStep(subCtx *subpelSearchCtx, startRow int, startCol int, step int, bestEval subpelCandidateEval, refRow4 int, refCol4 int, errorPerBit int) (subpelCandidateEval, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftEval := s.candidateEval(subCtx, startRow, startCol-step, refRow4, refCol4, errorPerBit)
	rightEval := s.candidateEval(subCtx, startRow, startCol+step, refRow4, refCol4, errorPerBit)
	upEval := s.candidateEval(subCtx, startRow-step, startCol, refRow4, refCol4, errorPerBit)
	downEval := s.candidateEval(subCtx, startRow+step, startCol, refRow4, refCol4, errorPerBit)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, leftEval, startRow, startCol-step)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, rightEval, startRow, startCol+step)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, upEval, startRow-step, startCol)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, downEval, startRow+step, startCol)

	diagRow := startRow - step
	if upEval.cost >= downEval.cost {
		diagRow = startRow + step
	}
	diagCol := startCol - step
	if leftEval.cost >= rightEval.cost {
		diagCol = startCol + step
	}
	diagEval := s.candidateEval(subCtx, diagRow, diagCol, refRow4, refCol4, errorPerBit)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, diagEval, diagRow, diagCol)
	return bestEval, bestRow, bestCol
}

func (s *interFrameSubpixelSearch) candidateEval(subCtx *subpelSearchCtx, row int, col int, refRow4 int, refCol4 int, errorPerBit int) subpelCandidateEval {
	s.stats.recordSubpelCandidate()
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		s.stats.recordSubpelBoundsReject()
		return subpelCandidateEval{cost: maxInt()}
	}
	s.stats.recordSubpelVariance()
	return subpelCandidateEval{
		cost:     dist + s.motionCost(row, col, refRow4, refCol4, errorPerBit),
		variance: dist,
		sse:      sse,
		ok:       true,
	}
}

func (s *interFrameSubpixelSearch) motionCost(row int, col int, refRow4 int, refCol4 int, errorPerBit int) int {
	if s.mvProbs == nil || s.mvCosts == nil {
		return 0
	}
	return s.mvCosts.SubpelSearchCostFromQuarterDeltas(row, col, refRow4, refCol4, errorPerBit)
}

func interFrameSubpixelMotionVectorInRange(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector) bool {
	maxFullPelEighths := interFrameMaxFullPelVal << 3
	rowDelta := int(mv.Row) - int(bestRefMV.Row)
	colDelta := int(mv.Col) - int(bestRefMV.Col)
	if rowDelta < 0 {
		rowDelta = -rowDelta
	}
	if colDelta < 0 {
		colDelta = -colDelta
	}
	return rowDelta <= maxFullPelEighths && colDelta <= maxFullPelEighths
}

// interFrameSubpelSearchBounds mirrors the minc/maxc/minr/maxr clamps libvpx
// computes at the head of vp8_find_best_sub_pixel_step_iteratively (and
// _step). The bounds are the intersection of the UMV window (in 1/4-pel:
// x->mv_col_min*4, x->mv_col_max*4) and a per-component window of size
// `(1 << mvlong_width) - 1` 1/4-pel sites around the 1/4-pel-aligned ref_mv
// (`ref_mv->as_mv.col >> 1`).  CHECK_BETTER's IFMVCV macro short-circuits any
// candidate outside this rectangle to UINT_MAX, which the govpx iter searches
// previously skipped — letting the iter chase variance gradients past the
// UMV edge into the replicated border, where the synthetic SPLITMV fixture
// finds an artificially low residual at large offsets and commits a wildly
// drifted MV.
type interFrameSubpelSearchBounds struct {
	rowMin int
	rowMax int
	colMin int
	colMax int
}

const subpelMVQuarterPelLongLimit = (1 << 10) - 1 // libvpx mvlong_width = 10.

func interFrameSubpelSearchBoundsFor(bestRefMV vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) interFrameSubpelSearchBounds {
	// libvpx mv_col_min / mv_col_max are in integer-pel; *4 converts to 1/4-pel.
	// The UMV window: -(mb_col*16 + (UMV_BORDER - 16)) ... ((mb_cols-1-mb_col)*16 + (UMV_BORDER - 16)).
	umv := interFrameUMVBorderPixels - 16
	rowMinIPel := -((mbRow * 16) + umv)
	rowMaxIPel := ((mbRows - 1 - mbRow) * 16) + umv
	colMinIPel := -((mbCol * 16) + umv)
	colMaxIPel := ((mbCols - 1 - mbCol) * 16) + umv

	rrQuarter := int(bestRefMV.Row) >> 1
	rcQuarter := int(bestRefMV.Col) >> 1
	rowMin := max(rrQuarter-subpelMVQuarterPelLongLimit, rowMinIPel*4)
	rowMax := min(rrQuarter+subpelMVQuarterPelLongLimit, rowMaxIPel*4)
	colMin := max(rcQuarter-subpelMVQuarterPelLongLimit, colMinIPel*4)
	colMax := min(rcQuarter+subpelMVQuarterPelLongLimit, colMaxIPel*4)
	return interFrameSubpelSearchBounds{rowMin: rowMin, rowMax: rowMax, colMin: colMin, colMax: colMax}
}

func (b interFrameSubpelSearchBounds) contains(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

// subpelSearchCtx hoists the per-MB invariants for the iterative sub-pel
// refinement out of the inner candidate-cost call. The 13-step
// half-then-quarter walk fires up to 7 candidate-cost calls per ring × 6
// rings = 42 candidates per MB, each of which previously paid the full
// macroblockSubpixelVariance prologue (slice-header bounds, ref bound
// checks). R15-B precomputes the source row pointer + ref limit
// thresholds once and folds them into a tight inline test.
type subpelSearchCtx struct {
	srcRowPtr  []byte // = src.Y[baseY*src.YStride+baseX:]
	srcYStride int
	refYFull   []byte
	refYStride int
	refYOrigin int
	refYBorder int
	refCodedH  int
	refCodedW  int
	baseY      int
	baseX      int
}

func newSubpelSearchCtx(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (subpelSearchCtx, bool) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY < 0 || baseX < 0 || baseY+16 > src.Height || baseX+16 > src.Width {
		return subpelSearchCtx{}, false
	}
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return subpelSearchCtx{}, false
	}
	return subpelSearchCtx{
		srcRowPtr:  src.Y[baseY*src.YStride+baseX:],
		srcYStride: src.YStride,
		refYFull:   ref.YFull,
		refYStride: ref.YStride,
		refYOrigin: ref.YOrigin,
		refYBorder: ref.YBorder,
		refCodedH:  ref.CodedHeight,
		refCodedW:  ref.CodedWidth,
		baseY:      baseY,
		baseX:      baseX,
	}, true
}

// subpelVarianceForQuarterMV computes the picker's quarter-pel variance
// without the per-call macroblockSubpixelVariance prologue.
//
// Caller passes (row, col) in quarter-pel units (signed); the function
// derives the integer-pel offset and the 1/8-pel sub-pel offset from
// those bits, mirroring libvpx's quarter-pel indexing exactly.
func (c *subpelSearchCtx) subpelVarianceForQuarterMV(row int, col int) (int, int, bool) {
	refBaseY := c.baseY + (row >> 2)
	refBaseX := c.baseX + (col >> 2)
	if refBaseY < -c.refYBorder || refBaseX < -c.refYBorder ||
		refBaseY+17 > c.refCodedH+c.refYBorder ||
		refBaseX+17 > c.refCodedW+c.refYBorder {
		return 0, 0, false
	}
	start := c.refYOrigin + refBaseY*c.refYStride + refBaseX
	if start < 0 || start+16*c.refYStride+17 > len(c.refYFull) {
		return 0, 0, false
	}
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	variance, sse := dsp.SubpelVariance16x16(c.refYFull[start:], c.refYStride, xOffset, yOffset, c.srcRowPtr, c.srcYStride)
	return variance, sse, true
}

// iterative performs the libvpx half- then
// quarter-pel refinement (vp8_find_best_sub_pixel_step_iteratively) anchored
// to bestRefMV: candidate MVs farther from bestRefMV than MAX_FULL_PEL_VAL
// (in 1/8-pel) get rejected with INT_MAX and the cost is charged against the
// ref-MV, not (0,0).
func (s *interFrameSubpixelSearch) iterative() (vp8enc.MotionVector, int, int, int, bool) {
	if int(s.best.Row)&7 != 0 || int(s.best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	br := (int(s.best.Row) >> 3) * 4
	bc := (int(s.best.Col) >> 3) * 4
	tr := br
	tc := bc
	subCtx, subCtxOK := newSubpelSearchCtx(s.src, s.ref, s.mbRow, s.mbCol)
	if !subCtxOK {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	mbRows := (s.src.Height + 15) >> 4
	mbCols := (s.src.Width + 15) >> 4
	bounds := interFrameSubpelSearchBoundsFor(s.bestRefMV, s.mbRow, s.mbCol, mbRows, mbCols)
	// R15-B: hoist errorPerBit + motion-vector probabilities into the
	// closure capture so each candidate-cost call collapses to a
	// SubpelVariance + LUT lookup.
	errorPerBit := libvpxErrorPerBit(s.qIndex)
	refRow4 := int(s.bestRefMV.Row) >> 1
	refCol4 := int(s.bestRefMV.Col) >> 1
	var cachedRows [48]int
	var cachedCols [48]int
	var cachedEval [48]subpelCandidateEval
	cachedCount := 0
	cand := func(r, c int) subpelCandidateEval {
		for i := range cachedCount {
			if cachedRows[i] == r && cachedCols[i] == c {
				s.stats.recordSubpelCandidate()
				s.stats.recordSubpelCacheHit()
				return cachedEval[i]
			}
		}
		eval := subpelCandidateEval{cost: maxInt()}
		if !bounds.contains(r, c) {
			s.stats.recordSubpelCandidate()
			s.stats.recordSubpelBoundsReject()
		} else {
			eval = s.candidateEval(&subCtx, r, c, refRow4, refCol4, errorPerBit)
		}
		if cachedCount < len(cachedRows) {
			cachedRows[cachedCount] = r
			cachedCols[cachedCount] = c
			cachedEval[cachedCount] = eval
			cachedCount++
		}
		return eval
	}
	bestEval := cand(br, bc)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}

	for range 3 {
		leftEval := cand(tr, tc-2)
		rightEval := cand(tr, tc+2)
		upEval := cand(tr-2, tc)
		downEval := cand(tr+2, tc)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, leftEval, tr, tc-2)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, rightEval, tr, tc+2)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, upEval, tr-2, tc)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, downEval, tr+2, tc)

		diagRow := tr - 2
		if upEval.cost >= downEval.cost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftEval.cost >= rightEval.cost {
			diagCol = tc + 2
		}
		diagEval := cand(diagRow, diagCol)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, diagEval, diagRow, diagCol)

		if tr == br && tc == bc {
			s.stats.recordSubpelEarlyBreak()
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftEval := cand(tr, tc-1)
		rightEval := cand(tr, tc+1)
		upEval := cand(tr-1, tc)
		downEval := cand(tr+1, tc)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, leftEval, tr, tc-1)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, rightEval, tr, tc+1)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, upEval, tr-1, tc)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, downEval, tr+1, tc)

		diagRow := tr - 1
		if upEval.cost >= downEval.cost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftEval.cost >= rightEval.cost {
			diagCol = tc + 1
		}
		diagEval := cand(diagRow, diagCol)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, diagEval, diagRow, diagCol)

		if tr == br && tc == bc {
			s.stats.recordSubpelEarlyBreak()
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, s.bestRefMV) {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	return finalMV, bestEval.cost, bestEval.variance, bestEval.sse, true
}

func updateSubpixelSearchBest(bestCost int, bestRow int, bestCol int, candidateCost int, candidateRow int, candidateCol int) (int, int, int) {
	if candidateCost < bestCost {
		return candidateCost, candidateRow, candidateCol
	}
	return bestCost, bestRow, bestCol
}

func updateSubpixelSearchBestEval(best subpelCandidateEval, bestRow int, bestCol int, candidate subpelCandidateEval, candidateRow int, candidateCol int) (subpelCandidateEval, int, int) {
	if candidate.cost < best.cost {
		return candidate, candidateRow, candidateCol
	}
	return best, bestRow, bestCol
}

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSplitBlockSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return splitBlockSAD(src, ref, mbRow, mbCol, block, width, height, mv) + interMotionSplitBlockSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSearchCostLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return interMotionSearchCostLimitedSADPerBit(src, ref, mbRow, mbCol, mv, limit, bestRefMV, libvpxSADPerBit16(qIndex))
}

// interMotionSearchCostLimitedSADPerBit takes sadPerBit pre-bound so a
// hot caller can hoist the LUT lookup out of its inner loop. Behaviour
// matches interMotionSearchCostLimited; macroblockSADLimited's own hot
// path covers the full-pel-in-bounds case.
func interMotionSearchCostLimitedSADPerBit(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int, bestRefMV vp8enc.MotionVector, sadPerBit int) int {
	mvCost := vp8enc.MotionVectorSADCost(mv, bestRefMV, sadPerBit)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, sadLimit) + mvCost
}

// fullPelSearchCtx hoists the per-MB invariants for the diamond / n-step /
// refine / hex / exhaustive full-pel search kernels out of the per-site inner
// loop. Like libvpx's mcomp.c, candidate SAD reads from the bordered reference
// plane (`base_pre + d->offset + row*stride + col`), so legal UMV edge
// candidates stay on the same SIMD path as interior candidates.
type fullPelSearchCtx struct {
	src        vp8enc.SourceImage
	ref        *vp8common.Image
	mbRow      int
	mbCol      int
	baseY      int
	baseX      int
	srcRowPtr  []byte // = src.Y[baseY*src.YStride+baseX : ]
	srcRowPtrP *byte  // = unsafe.SliceData(srcRowPtr) — hot SAD bypass
	srcYStride int
	refYFullP  *byte
	refYStride int
	refYOrigin int
	refYBorder int
	refRowH    uint // = uint(ref.CodedHeight + 2*ref.YBorder - 16)
	refRowW    uint // = uint(ref.CodedWidth + 2*ref.YBorder - 16)
}

func newFullPelSearchCtx(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) fullPelSearchCtx {
	baseY := mbRow * 16
	baseX := mbCol * 16
	srcRowPtr := src.Y[baseY*src.YStride+baseX:]
	refYFull := ref.YFull
	refYFullP := unsafe.SliceData(refYFull)
	refRowH := uint(ref.CodedHeight + 2*ref.YBorder - 16)
	refRowW := uint(ref.CodedWidth + 2*ref.YBorder - 16)
	if !refFullPelBufferOK(ref, 16, 16) {
		refYFullP = nil
		refRowH = 0
		refRowW = 0
	}
	return fullPelSearchCtx{
		src:        src,
		ref:        ref,
		mbRow:      mbRow,
		mbCol:      mbCol,
		baseY:      baseY,
		baseX:      baseX,
		srcRowPtr:  srcRowPtr,
		srcRowPtrP: unsafe.SliceData(srcRowPtr),
		srcYStride: src.YStride,
		refYFullP:  refYFullP,
		refYStride: ref.YStride,
		refYOrigin: ref.YOrigin,
		refYBorder: ref.YBorder,
		refRowH:    refRowH,
		refRowW:    refRowW,
	}
}

func (c *fullPelSearchCtx) fullPelCostFull(row int, col int, refRow8 int, refCol8 int, qIndex int) int {
	return c.fullPelSADFull(row, col) + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
}

func (c *fullPelSearchCtx) fullPelSADFull(row int, col int) int {
	refBaseY := c.baseY + row
	refBaseX := c.baseX + col
	if c.refYFullP != nil &&
		uint(refBaseY+c.refYBorder) <= c.refRowH &&
		uint(refBaseX+c.refYBorder) <= c.refRowW {
		refPtr := (*byte)(unsafe.Add(unsafe.Pointer(c.refYFullP), c.refYOrigin+refBaseY*c.refYStride+refBaseX))
		return dsp.SAD16x16PtrFast(c.srcRowPtrP, c.srcYStride, refPtr, c.refYStride)
	}
	return c.fullPelCostLimitedSlow(col*interFrameMVFullPixelStep, row*interFrameMVFullPixelStep, refBaseY, refBaseX, maxInt())
}

func (c *fullPelSearchCtx) fullPelSADFull4(row0 int, col0 int, row1 int, col1 int, row2 int, col2 int, row3 int, col3 int, out *[4]uint32) bool {
	refBaseY0 := c.baseY + row0
	refBaseX0 := c.baseX + col0
	refBaseY1 := c.baseY + row1
	refBaseX1 := c.baseX + col1
	refBaseY2 := c.baseY + row2
	refBaseX2 := c.baseX + col2
	refBaseY3 := c.baseY + row3
	refBaseX3 := c.baseX + col3
	if c.refYFullP == nil ||
		uint(refBaseY0+c.refYBorder) > c.refRowH || uint(refBaseX0+c.refYBorder) > c.refRowW ||
		uint(refBaseY1+c.refYBorder) > c.refRowH || uint(refBaseX1+c.refYBorder) > c.refRowW ||
		uint(refBaseY2+c.refYBorder) > c.refRowH || uint(refBaseX2+c.refYBorder) > c.refRowW ||
		uint(refBaseY3+c.refYBorder) > c.refRowH || uint(refBaseX3+c.refYBorder) > c.refRowW {
		return false
	}
	base := unsafe.Pointer(c.refYFullP)
	refPtr0 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY0*c.refYStride+refBaseX0))
	refPtr1 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY1*c.refYStride+refBaseX1))
	refPtr2 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY2*c.refYStride+refBaseX2))
	refPtr3 := (*byte)(unsafe.Add(base, c.refYOrigin+refBaseY3*c.refYStride+refBaseX3))
	dsp.SAD16x16x4PtrFast(c.srcRowPtrP, c.srcYStride, refPtr0, refPtr1, refPtr2, refPtr3, c.refYStride, out)
	return true
}

func (c *fullPelSearchCtx) fullPelCostLimited(mvRow int, mvCol int, limit int, refRow8 int, refCol8 int, qIndex int) int {
	mvCost := libvpxFullPelMVSADCost16FromDeltas(mvRow>>3, mvCol>>3, refRow8, refCol8, qIndex)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	refBaseY := c.baseY + (mvRow >> 3)
	refBaseX := c.baseX + (mvCol >> 3)
	if c.refYFullP != nil &&
		uint(refBaseY+c.refYBorder) <= c.refRowH &&
		uint(refBaseX+c.refYBorder) <= c.refRowW {
		refPtr := (*byte)(unsafe.Add(unsafe.Pointer(c.refYFullP), c.refYOrigin+refBaseY*c.refYStride+refBaseX))
		return dsp.SAD16x16LimitPtrFast(c.srcRowPtrP, c.srcYStride, refPtr, c.refYStride, sadLimit) + mvCost
	}
	return c.fullPelCostLimitedSlow(mvCol, mvRow, refBaseY, refBaseX, sadLimit) + mvCost
}

func (c *fullPelSearchCtx) fullPelCostLimitedSlow(mvCol int, mvRow int, refBaseY int, refBaseX int, sadLimit int) int {
	return macroblockSADLimitedSlow(c.src, c.ref, c.baseY, c.baseX, refBaseY, refBaseX, mvCol, mvRow, sadLimit)
}

func refFullPelBufferOK(ref *vp8common.Image, width int, height int) bool {
	if ref == nil || width <= 0 || height <= 0 || len(ref.YFull) == 0 ||
		ref.YOrigin < 0 || ref.YBorder < 0 ||
		ref.CodedWidth+2*ref.YBorder < width ||
		ref.CodedHeight+2*ref.YBorder < height ||
		ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return false
	}
	if ref.YOrigin-ref.YBorder*ref.YStride-ref.YBorder < 0 {
		return false
	}
	maxRow := ref.CodedHeight + ref.YBorder - 1
	maxColEnd := ref.CodedWidth + ref.YBorder
	return ref.YOrigin+maxRow*ref.YStride+maxColEnd <= len(ref.YFull)
}

func refFullPelYOffset(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int) (int, bool) {
	if !refFullPelBufferOK(ref, width, height) {
		return 0, false
	}
	if uint(refBaseY+ref.YBorder) > uint(ref.CodedHeight+2*ref.YBorder-height) ||
		uint(refBaseX+ref.YBorder) > uint(ref.CodedWidth+2*ref.YBorder-width) {
		return 0, false
	}
	off := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if off < 0 || off+(height-1)*ref.YStride+width > len(ref.YFull) {
		return 0, false
	}
	return off, true
}

func refFullPelYPtr(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int) (*byte, bool) {
	off, ok := refFullPelYOffset(ref, refBaseY, refBaseX, width, height)
	if !ok {
		return nil, false
	}
	return (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(ref.YFull)), off)), true
}

func refFullPelYSlice(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int) ([]byte, bool) {
	off, ok := refFullPelYOffset(ref, refBaseY, refBaseX, width, height)
	if !ok {
		return nil, false
	}
	return ref.YFull[off:], true
}

func interMotionFullPixelSearchReturnCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	variance, _ := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	return variance + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, mvProbs)
}

// interMotionSearchVectorCost charges full-pel MV bits against bestRefMV like
// libvpx mvsad_err_cost — picking against (0,0) inflates the cost of motion
// far from a strong predictor and biases NEWMV away from correct candidates.
func interMotionSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit16(qIndex))
}

func interMotionSplitBlockSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit4(qIndex))
}

// interMotionSearchErrorVectorCost charges sub-pel MV bits against bestRefMV
// (libvpx find_best_sub_pixel_step_iteratively in mcomp.c).
func interMotionSearchErrorVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorErrorCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}

// interMotionSubpelCandidateVectorCost charges the sub-pel MV bits like the
// MVC macro inside libvpx's vp8_find_best_sub_pixel_step{_iteratively}: the
// 1/4-pel index is built from (mv>>1) - (ref>>1) — i.e. each operand is
// arithmetic-shifted to 1/4-pel before subtraction — and the lookup is
// signed (no clamp-to-zero). This matches the CHECK_BETTER candidate cost
// shape exactly when bestRefMV is fractional in 1/8-pel, which the
// mv_err_cost / vp8_mv_bit_cost variants used for the central cost do not.
// See MotionVectorSubpelSearchCost for the full derivation.
func interMotionSubpelCandidateVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorSubpelSearchCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}
