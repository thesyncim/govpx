package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

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
	// errorPerBit, when non-zero, overrides libvpxErrorPerBit(qIndex) for
	// the sub-pel refinement. Populated by per-MB picker dispatches that
	// apply libvpx's vp8_activity_masking errorperbit adjustment when
	// TuneSSIM is active; otherwise zero (default keeps the libvpx
	// qIndex-only baseline that drives the PSNR-tuned path).
	errorPerBit int
	search      interAnalysisSearchConfig
	start       interFrameSearchStart
	mvProbs     *[2][vp8tables.MVPCount]uint8
	mvCosts     *vp8enc.MotionVectorCostTables
	stats       *interFrameMotionSearchStats
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
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsCostTablesAndStats(s.src, s.ref, s.mbRow, s.mbCol, s.mbRows, s.mbCols, s.bestRefMV, s.qIndex, s.errorPerBit, s.search, s.start, s.mvProbs, s.mvCosts, s.stats)
}

func (s interFrameMotionVectorSearch) subpixel(best vp8enc.MotionVector) interFrameSubpixelSearch {
	return interFrameSubpixelSearch{
		src:         s.src,
		ref:         s.ref,
		mbRow:       s.mbRow,
		mbCol:       s.mbCol,
		best:        best,
		bestRefMV:   s.bestRefMV,
		qIndex:      s.qIndex,
		errorPerBit: s.errorPerBit,
		search:      s.search,
		mvProbs:     s.mvProbs,
		mvCosts:     s.mvCosts,
		stats:       s.stats,
	}
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
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsCostTablesAndStats(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, 0, search, start, mvProbs, nil, stats)
}

func selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsCostTablesAndStats(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, errorPerBit int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, stats *interFrameMotionSearchStats) (vp8enc.MotionVector, int) {
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
	searcher := newFullPelMotionSearch(src, ref, mbRow, mbCol, bestRefMV, qIndex, bounds, mvProbs, mvCosts, errorPerBit, stats)
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
