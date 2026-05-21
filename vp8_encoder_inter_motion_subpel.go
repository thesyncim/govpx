package govpx

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type interFrameSubpixelSearch struct {
	ref     *vp8common.Image
	mvProbs *[2][vp8tables.MVPCount]uint8
	mvCosts *vp8enc.MotionVectorCostTables
	src     vp8enc.SourceImage
	mbRow   int
	mbCol   int
	qIndex  int
	// errorPerBit, when non-zero, overrides the libvpx-default
	// vp8enc.ErrorPerBit(qIndex) used to scale motion-vector rate into the
	// fractional-search cost. TuneSSIM's per-MB vp8_activity_masking call in
	// libvpx (vp8/encoder/encodeframe.c) re-derives x->errorperbit from the
	// activity-adjusted x->rdmult before each MB's NEWMV search; mirroring
	// that here is what closes the realtime-cbr-cpu-3-64x64-segmented-tune-
	// ssim parity gap that remained after the activity probe matched libvpx
	// byte-for-byte.
	errorPerBit int
	best        vp8enc.MotionVector
	bestRefMV   vp8enc.MotionVector
	search      interAnalysisSearchConfig
}

// effectiveErrorPerBit returns the caller-supplied per-MB errorperbit when
// TuneSSIM activity masking is active, falling back to the libvpx default
// derived purely from qIndex. The default preserves byte parity for the
// PSNR-tuned path and for all callers that have not been threaded through
// to populate errorPerBit yet (the picker and split-MV dispatches that the
// segmented-tune-ssim fixture exercises do populate it).
func (s *interFrameSubpixelSearch) effectiveErrorPerBit() int {
	if s.errorPerBit > 0 {
		return s.errorPerBit
	}
	return vp8enc.ErrorPerBit(s.qIndex)
}

type subpelCandidateEval struct {
	cost     int
	variance int32
	sse      int32
	ok       bool
}

func makeSubpelCandidateEval(cost int, variance int, sse int) subpelCandidateEval {
	return subpelCandidateEval{
		cost:     cost,
		variance: int32(variance),
		sse:      int32(sse),
		ok:       true,
	}
}

func (s *interFrameSubpixelSearch) refine() (vp8enc.MotionVector, int, int32, int32, bool) {
	switch s.search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return s.stepNoStats(true)
	case interAnalysisFractionalSearchHalf:
		return s.stepNoStats(false)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, 0, 0, false
	default:
		return s.iterativeNoStats()
	}
}

func (s *interFrameSubpixelSearch) refineWithStats(stats *interFrameMotionSearchStats) (vp8enc.MotionVector, int, int32, int32, bool) {
	if stats == nil {
		return s.refine()
	}
	switch s.search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return s.stepWithStats(stats, true)
	case interAnalysisFractionalSearchHalf:
		return s.stepWithStats(stats, false)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, 0, 0, false
	default:
		return s.iterativeWithStats(stats)
	}
}

func (s *interFrameSubpixelSearch) stepWithStats(stats *interFrameMotionSearchStats, quarter bool) (vp8enc.MotionVector, int, int32, int32, bool) {
	if int(s.best.Row)&7 != 0 || int(s.best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	bestRow := (int(s.best.Row) >> 3) * 4
	bestCol := (int(s.best.Col) >> 3) * 4
	subCtx, subCtxOK := newSubpelSearchCtx(s.src, s.ref, s.mbRow, s.mbCol)
	if !subCtxOK {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	errorPerBit := s.effectiveErrorPerBit()
	bestEval := s.centerEvalWithStats(stats, &subCtx, bestRow, bestCol, errorPerBit)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	bestEval, bestRow, bestCol = s.directionalStepWithStats(stats, &subCtx, bestRow, bestCol, 2, bestEval, errorPerBit)
	if quarter {
		bestEval, bestRow, bestCol = s.directionalStepWithStats(stats, &subCtx, bestRow, bestCol, 1, bestEval, errorPerBit)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !vp8enc.InterFrameSubpixelMotionVectorInRange(finalMV, s.bestRefMV) {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	return finalMV, bestEval.cost, bestEval.variance, bestEval.sse, true
}

func (s *interFrameSubpixelSearch) stepNoStats(quarter bool) (vp8enc.MotionVector, int, int32, int32, bool) {
	if int(s.best.Row)&7 != 0 || int(s.best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	bestRow := (int(s.best.Row) >> 3) * 4
	bestCol := (int(s.best.Col) >> 3) * 4
	subCtx, subCtxOK := newSubpelSearchCtx(s.src, s.ref, s.mbRow, s.mbCol)
	if !subCtxOK {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	errorPerBit := s.effectiveErrorPerBit()
	bestEval := s.centerEvalNoStats(&subCtx, bestRow, bestCol, errorPerBit)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	bestEval, bestRow, bestCol = s.directionalStepNoStats(&subCtx, bestRow, bestCol, 2, bestEval, errorPerBit)
	if quarter {
		bestEval, bestRow, bestCol = s.directionalStepNoStats(&subCtx, bestRow, bestCol, 1, bestEval, errorPerBit)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !vp8enc.InterFrameSubpixelMotionVectorInRange(finalMV, s.bestRefMV) {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	return finalMV, bestEval.cost, bestEval.variance, bestEval.sse, true
}

func (s *interFrameSubpixelSearch) directionalStepWithStats(stats *interFrameMotionSearchStats, subCtx *subpelSearchCtx, startRow int, startCol int, step int, bestEval subpelCandidateEval, errorPerBit int) (subpelCandidateEval, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftEval := s.stepCandidateEvalWithStats(stats, subCtx, startRow, startCol-step, errorPerBit)
	rightEval := s.stepCandidateEvalWithStats(stats, subCtx, startRow, startCol+step, errorPerBit)
	upEval := s.stepCandidateEvalWithStats(stats, subCtx, startRow-step, startCol, errorPerBit)
	downEval := s.stepCandidateEvalWithStats(stats, subCtx, startRow+step, startCol, errorPerBit)
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
	diagEval := s.stepCandidateEvalWithStats(stats, subCtx, diagRow, diagCol, errorPerBit)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, diagEval, diagRow, diagCol)
	return bestEval, bestRow, bestCol
}

func (s *interFrameSubpixelSearch) directionalStepNoStats(subCtx *subpelSearchCtx, startRow int, startCol int, step int, bestEval subpelCandidateEval, errorPerBit int) (subpelCandidateEval, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftEval := s.stepCandidateEvalNoStats(subCtx, startRow, startCol-step, errorPerBit)
	rightEval := s.stepCandidateEvalNoStats(subCtx, startRow, startCol+step, errorPerBit)
	upEval := s.stepCandidateEvalNoStats(subCtx, startRow-step, startCol, errorPerBit)
	downEval := s.stepCandidateEvalNoStats(subCtx, startRow+step, startCol, errorPerBit)
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
	diagEval := s.stepCandidateEvalNoStats(subCtx, diagRow, diagCol, errorPerBit)
	bestEval, bestRow, bestCol = updateSubpixelSearchBestEval(bestEval, bestRow, bestCol, diagEval, diagRow, diagCol)
	return bestEval, bestRow, bestCol
}

func (s *interFrameSubpixelSearch) stepCandidateEvalWithStats(stats *interFrameMotionSearchStats, subCtx *subpelSearchCtx, row int, col int, errorPerBit int) subpelCandidateEval {
	stats.recordSubpelCandidate()
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		stats.recordSubpelBoundsReject()
		return subpelCandidateEval{cost: maxInt()}
	}
	stats.recordSubpelVariance()
	return makeSubpelCandidateEval(dist+s.centerMotionCost(row, col, errorPerBit), dist, sse)
}

func (s *interFrameSubpixelSearch) stepCandidateEvalNoStats(subCtx *subpelSearchCtx, row int, col int, errorPerBit int) subpelCandidateEval {
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		return subpelCandidateEval{cost: maxInt()}
	}
	return makeSubpelCandidateEval(dist+s.centerMotionCost(row, col, errorPerBit), dist, sse)
}

func (s *interFrameSubpixelSearch) candidateEvalWithStats(stats *interFrameMotionSearchStats, subCtx *subpelSearchCtx, row int, col int, refRow4 int, refCol4 int, errorPerBit int) subpelCandidateEval {
	stats.recordSubpelCandidate()
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		stats.recordSubpelBoundsReject()
		return subpelCandidateEval{cost: maxInt()}
	}
	stats.recordSubpelVariance()
	return makeSubpelCandidateEval(dist+s.motionCost(row, col, refRow4, refCol4, errorPerBit), dist, sse)
}

func (s *interFrameSubpixelSearch) candidateEvalNoStats(subCtx *subpelSearchCtx, row int, col int, refRow4 int, refCol4 int, errorPerBit int) subpelCandidateEval {
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		return subpelCandidateEval{cost: maxInt()}
	}
	return makeSubpelCandidateEval(dist+s.motionCost(row, col, refRow4, refCol4, errorPerBit), dist, sse)
}

func (s *interFrameSubpixelSearch) centerEvalWithStats(stats *interFrameMotionSearchStats, subCtx *subpelSearchCtx, row int, col int, errorPerBit int) subpelCandidateEval {
	stats.recordSubpelCandidate()
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		stats.recordSubpelBoundsReject()
		return subpelCandidateEval{cost: maxInt()}
	}
	stats.recordSubpelVariance()
	return makeSubpelCandidateEval(dist+s.centerMotionCost(row, col, errorPerBit), dist, sse)
}

func (s *interFrameSubpixelSearch) centerEvalNoStats(subCtx *subpelSearchCtx, row int, col int, errorPerBit int) subpelCandidateEval {
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		return subpelCandidateEval{cost: maxInt()}
	}
	return makeSubpelCandidateEval(dist+s.centerMotionCost(row, col, errorPerBit), dist, sse)
}

func (s *interFrameSubpixelSearch) motionCost(row int, col int, refRow4 int, refCol4 int, errorPerBit int) int {
	if s.mvProbs == nil || s.mvCosts == nil {
		return 0
	}
	return s.mvCosts.SubpelSearchCostFromQuarterDeltas(row, col, refRow4, refCol4, errorPerBit)
}

func (s *interFrameSubpixelSearch) centerMotionCost(row int, col int, errorPerBit int) int {
	if s.mvProbs == nil {
		return 0
	}
	mvRow8 := row * 2
	mvCol8 := col * 2
	if s.mvCosts != nil {
		return s.mvCosts.ErrorCostFromEighthDeltas(mvRow8, mvCol8, int(s.bestRefMV.Row), int(s.bestRefMV.Col), errorPerBit)
	}
	return vp8enc.MotionVectorErrorCost(vp8enc.MotionVector{Row: int16(mvRow8), Col: int16(mvCol8)}, s.bestRefMV, s.mvProbs, errorPerBit)
}

// subpelSearchCtx hoists the per-MB invariants for the iterative sub-pel
// refinement out of the inner candidate-cost call. The 13-step
// half-then-quarter walk fires up to 7 candidate-cost calls per ring × 6
// rings = 42 candidates per MB, each of which previously paid the full
// macroblockSubpixelVariance prologue (slice-header bounds, ref bound
// checks). R15-B precomputes the source row pointer + ref limit
// thresholds once and folds them into a tight inline test.
type subpelSearchCtx struct {
	srcRowPtr  *byte // = &src.Y[baseY*src.YStride+baseX]
	refYFull   []byte
	refYFullP  *byte
	srcYStride int
	refYStride int
	refYOrigin int
	refYBorder int
	refCodedH  int
	refCodedW  int
	baseY      int
	baseX      int
	srcScratch [16 * 16]byte
	srcPartial bool
}

func newSubpelSearchCtx(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (subpelSearchCtx, bool) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if src.Width <= 0 || src.Height <= 0 || baseY < 0 || baseX < 0 {
		return subpelSearchCtx{}, false
	}
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return subpelSearchCtx{}, false
	}
	ctx := subpelSearchCtx{
		refYFull:   ref.YFull,
		refYStride: ref.YStride,
		refYOrigin: ref.YOrigin,
		refYBorder: ref.YBorder,
		refCodedH:  ref.CodedHeight,
		refCodedW:  ref.CodedWidth,
		baseY:      baseY,
		baseX:      baseX,
	}
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		ctx.srcRowPtr = (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(src.Y)), baseY*src.YStride+baseX))
		ctx.srcYStride = src.YStride
		ctx.refYFullP = unsafe.SliceData(ctx.refYFull)
		return ctx, true
	}
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			ctx.srcScratch[row*16+col] = src.Y[srcY*src.YStride+srcX]
		}
	}
	ctx.srcYStride = 16
	ctx.srcPartial = true
	ctx.refYFullP = unsafe.SliceData(ctx.refYFull)
	return ctx, true
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
	srcRowPtr := c.srcRowPtr
	if c.srcPartial {
		srcRowPtr = unsafe.SliceData(c.srcScratch[:])
	}
	refPtr := (*byte)(unsafe.Add(unsafe.Pointer(c.refYFullP), start))
	variance, sse := dsp.SubpelVariance16x16PtrFast(refPtr, c.refYStride, xOffset, yOffset, srcRowPtr, c.srcYStride)
	return variance, sse, true
}

type subpelEvalCache struct {
	rows [64]int
	cols [64]int
	eval [64]subpelCandidateEval
	used uint64
}

func subpelEvalCacheIndex(row int, col int) uint {
	return uint((row*33)^col) & 63
}

func (c *subpelEvalCache) get(row int, col int) (subpelCandidateEval, bool) {
	index := subpelEvalCacheIndex(row, col)
	for range 64 {
		bit := uint64(1) << index
		if c.used&bit == 0 {
			return subpelCandidateEval{}, false
		}
		if c.rows[index] == row && c.cols[index] == col {
			return c.eval[index], true
		}
		index = (index + 1) & 63
	}
	return subpelCandidateEval{}, false
}

func (c *subpelEvalCache) put(row int, col int, eval subpelCandidateEval) {
	index := subpelEvalCacheIndex(row, col)
	for range 64 {
		bit := uint64(1) << index
		if c.used&bit == 0 || c.rows[index] == row && c.cols[index] == col {
			c.used |= bit
			c.rows[index] = row
			c.cols[index] = col
			c.eval[index] = eval
			return
		}
		index = (index + 1) & 63
	}
}

func (s *interFrameSubpixelSearch) cachedCandidateEvalWithStats(stats *interFrameMotionSearchStats, cache *subpelEvalCache, subCtx *subpelSearchCtx, bounds vp8enc.InterFrameSubpelSearchBounds, row int, col int, refRow4 int, refCol4 int, errorPerBit int) subpelCandidateEval {
	if eval, ok := cache.get(row, col); ok {
		stats.recordSubpelCandidate()
		stats.recordSubpelCacheHit()
		return eval
	}
	eval := subpelCandidateEval{cost: maxInt()}
	if !bounds.Contains(row, col) {
		stats.recordSubpelCandidate()
		stats.recordSubpelBoundsReject()
	} else {
		eval = s.candidateEvalWithStats(stats, subCtx, row, col, refRow4, refCol4, errorPerBit)
	}
	cache.put(row, col, eval)
	return eval
}

func (s *interFrameSubpixelSearch) cachedCandidateEvalNoStats(cache *subpelEvalCache, subCtx *subpelSearchCtx, bounds vp8enc.InterFrameSubpelSearchBounds, row int, col int, refRow4 int, refCol4 int, errorPerBit int) subpelCandidateEval {
	if eval, ok := cache.get(row, col); ok {
		return eval
	}
	eval := subpelCandidateEval{cost: maxInt()}
	if bounds.Contains(row, col) {
		eval = s.candidateEvalNoStats(subCtx, row, col, refRow4, refCol4, errorPerBit)
	}
	cache.put(row, col, eval)
	return eval
}

// iterative performs the libvpx half- then
// quarter-pel refinement (vp8_find_best_sub_pixel_step_iteratively) anchored
// to bestRefMV: candidate MVs farther from bestRefMV than MAX_FULL_PEL_VAL
// (in 1/8-pel) get rejected with INT_MAX and the cost is charged against the
// ref-MV, not (0,0).
func (s *interFrameSubpixelSearch) iterativeWithStats(stats *interFrameMotionSearchStats) (vp8enc.MotionVector, int, int32, int32, bool) {
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
	bounds := vp8enc.InterFrameSubpelSearchBoundsFor(s.bestRefMV, s.mbRow, s.mbCol, mbRows, mbCols)
	// R15-B: hoist errorPerBit + motion-vector probabilities into the
	// closure capture so each candidate-cost call collapses to a
	// SubpelVariance + LUT lookup.
	errorPerBit := s.effectiveErrorPerBit()
	refRow4 := int(s.bestRefMV.Row) >> 1
	refCol4 := int(s.bestRefMV.Col) >> 1
	var cache subpelEvalCache
	bestEval := s.centerEvalWithStats(stats, &subCtx, br, bc, errorPerBit)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}

	for range 3 {
		leftEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr, tc-2, refRow4, refCol4, errorPerBit)
		rightEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr, tc+2, refRow4, refCol4, errorPerBit)
		upEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr-2, tc, refRow4, refCol4, errorPerBit)
		downEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr+2, tc, refRow4, refCol4, errorPerBit)
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
		diagEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, diagRow, diagCol, refRow4, refCol4, errorPerBit)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, diagEval, diagRow, diagCol)

		if tr == br && tc == bc {
			stats.recordSubpelEarlyBreak()
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr, tc-1, refRow4, refCol4, errorPerBit)
		rightEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr, tc+1, refRow4, refCol4, errorPerBit)
		upEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr-1, tc, refRow4, refCol4, errorPerBit)
		downEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, tr+1, tc, refRow4, refCol4, errorPerBit)
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
		diagEval := s.cachedCandidateEvalWithStats(stats, &cache, &subCtx, bounds, diagRow, diagCol, refRow4, refCol4, errorPerBit)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, diagEval, diagRow, diagCol)

		if tr == br && tc == bc {
			stats.recordSubpelEarlyBreak()
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !vp8enc.InterFrameSubpixelMotionVectorInRange(finalMV, s.bestRefMV) {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	return finalMV, bestEval.cost, bestEval.variance, bestEval.sse, true
}

func (s *interFrameSubpixelSearch) iterativeNoStats() (vp8enc.MotionVector, int, int32, int32, bool) {
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
	bounds := vp8enc.InterFrameSubpelSearchBoundsFor(s.bestRefMV, s.mbRow, s.mbCol, mbRows, mbCols)
	errorPerBit := s.effectiveErrorPerBit()
	refRow4 := int(s.bestRefMV.Row) >> 1
	refCol4 := int(s.bestRefMV.Col) >> 1
	var cache subpelEvalCache
	bestEval := s.centerEvalNoStats(&subCtx, br, bc, errorPerBit)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}

	for range 3 {
		leftEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr, tc-2, refRow4, refCol4, errorPerBit)
		rightEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr, tc+2, refRow4, refCol4, errorPerBit)
		upEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr-2, tc, refRow4, refCol4, errorPerBit)
		downEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr+2, tc, refRow4, refCol4, errorPerBit)
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
		diagEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, diagRow, diagCol, refRow4, refCol4, errorPerBit)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, diagEval, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr, tc-1, refRow4, refCol4, errorPerBit)
		rightEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr, tc+1, refRow4, refCol4, errorPerBit)
		upEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr-1, tc, refRow4, refCol4, errorPerBit)
		downEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, tr+1, tc, refRow4, refCol4, errorPerBit)
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
		diagEval := s.cachedCandidateEvalNoStats(&cache, &subCtx, bounds, diagRow, diagCol, refRow4, refCol4, errorPerBit)
		bestEval, br, bc = updateSubpixelSearchBestEval(bestEval, br, bc, diagEval, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !vp8enc.InterFrameSubpixelMotionVectorInRange(finalMV, s.bestRefMV) {
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
