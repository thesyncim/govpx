package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

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
	bestEval := s.centerEval(&subCtx, bestRow, bestCol, errorPerBit)
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

func (s *interFrameSubpixelSearch) centerEval(subCtx *subpelSearchCtx, row int, col int, errorPerBit int) subpelCandidateEval {
	s.stats.recordSubpelCandidate()
	dist, sse, ok := subCtx.subpelVarianceForQuarterMV(row, col)
	if !ok {
		s.stats.recordSubpelBoundsReject()
		return subpelCandidateEval{cost: maxInt()}
	}
	s.stats.recordSubpelVariance()
	return subpelCandidateEval{
		cost:     dist + s.centerMotionCost(row, col, errorPerBit),
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

func interFrameSubpixelMotionVectorInRange(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector) bool {
	maxFullPelEighths := interFrameMaxFullPelVal << 3
	rowDelta := int(mv.Row) - int(bestRefMV.Row)
	colDelta := int(mv.Col) - int(bestRefMV.Col)
	rMask := rowDelta >> mvKernelSignShift
	cMask := colDelta >> mvKernelSignShift
	rowDelta = (rowDelta ^ rMask) - rMask
	colDelta = (colDelta ^ cMask) - cMask
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
	bestEval := s.centerEval(&subCtx, br, bc, errorPerBit)
	if !bestEval.ok {
		return vp8enc.MotionVector{}, 0, 0, 0, false
	}
	if cachedCount < len(cachedRows) {
		cachedRows[cachedCount] = br
		cachedCols[cachedCount] = bc
		cachedEval[cachedCount] = bestEval
		cachedCount++
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
