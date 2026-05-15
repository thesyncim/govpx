package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type interFrameNstepSearchResult struct {
	cost  int
	mv    vp8enc.MotionVector
	num00 uint8
}

type interFrameMotionSearchStats struct {
	phase                *EncoderPhaseStats
	fullPelSADCalls      int
	fullPelSADCandidates int
	fullPelBatchCalls    int
	fullPelBoundsRejects int
	fullPelEarlyBreaks   int
	subpelCandidates     int
	subpelVarianceCalls  int
	subpelCacheHits      int
	subpelBoundsRejects  int
	subpelEarlyBreaks    int
}

func (s *interFrameMotionSearchStats) recordFullPelSAD(candidates int, batch bool) {
	if s == nil || candidates <= 0 {
		return
	}
	s.fullPelSADCalls++
	s.fullPelSADCandidates += candidates
	if phase := s.phase; phase != nil {
		phase.FullPelSADCalls++
		phase.FullPelSADCandidates += int64(candidates)
	}
	if batch {
		s.fullPelBatchCalls++
		if phase := s.phase; phase != nil {
			phase.FullPelBatchCalls++
		}
	}
}

func (s *interFrameMotionSearchStats) recordFullPelBoundsRejects(count int) {
	if s == nil || count <= 0 {
		return
	}
	s.fullPelBoundsRejects += count
	if phase := s.phase; phase != nil {
		phase.FullPelBoundsRejects += int64(count)
	}
}

func (s *interFrameMotionSearchStats) recordFullPelEarlyBreak() {
	if s == nil {
		return
	}
	s.fullPelEarlyBreaks++
	if phase := s.phase; phase != nil {
		phase.FullPelEarlyBreaks++
	}
}

func (s *interFrameMotionSearchStats) recordSubpelCandidate() {
	if s == nil {
		return
	}
	s.subpelCandidates++
	if phase := s.phase; phase != nil {
		phase.SubpelCandidates++
	}
}

func (s *interFrameMotionSearchStats) recordSubpelVariance() {
	if s == nil {
		return
	}
	s.subpelVarianceCalls++
	if phase := s.phase; phase != nil {
		phase.SubpelVarianceCalls++
	}
}

func (s *interFrameMotionSearchStats) recordSubpelCacheHit() {
	if s == nil {
		return
	}
	s.subpelCacheHits++
	if phase := s.phase; phase != nil {
		phase.SubpelCacheHits++
	}
}

func (s *interFrameMotionSearchStats) recordSubpelBoundsReject() {
	if s == nil {
		return
	}
	s.subpelBoundsRejects++
	if phase := s.phase; phase != nil {
		phase.SubpelBoundsRejects++
	}
}

func (s *interFrameMotionSearchStats) recordSubpelEarlyBreak() {
	if s == nil {
		return
	}
	s.subpelEarlyBreaks++
	if phase := s.phase; phase != nil {
		phase.SubpelEarlyBreaks++
	}
}

type fullPelMotionSearch struct {
	mvProbs     *[2][vp8tables.MVPCount]uint8
	mvCosts     *vp8enc.MotionVectorCostTables
	stats       *interFrameMotionSearchStats
	ctx         fullPelSearchCtx
	bounds      interFrameFullPixelBounds
	qIndex      int
	errorPerBit int
	refRow8     int
	refCol8     int
	bestRefMV   vp8enc.MotionVector
}

func newFullPelMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, errorPerBit int, stats *interFrameMotionSearchStats) fullPelMotionSearch {
	if errorPerBit <= 0 {
		errorPerBit = libvpxErrorPerBit(qIndex)
	}
	return fullPelMotionSearch{
		ctx:         newFullPelSearchCtx(src, ref, mbRow, mbCol),
		bounds:      bounds,
		mvProbs:     mvProbs,
		mvCosts:     mvCosts,
		qIndex:      qIndex,
		errorPerBit: errorPerBit,
		refRow8:     int(bestRefMV.Row) >> 3,
		refCol8:     int(bestRefMV.Col) >> 3,
		bestRefMV:   bestRefMV,
		stats:       stats,
	}
}

func (s *fullPelMotionSearch) cost(mv vp8enc.MotionVector) int {
	s.stats.recordFullPelSAD(1, false)
	return interMotionFullPixelSearchReturnCostWithErrorPerBitAndCostTables(s.ctx.src, s.ctx.ref, s.ctx.mbRow, s.ctx.mbCol, mv, s.bestRefMV, s.errorPerBit, s.mvProbs, s.mvCosts)
}

func (s *fullPelMotionSearch) walkCost(mv vp8enc.MotionVector, limit int) int {
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	if limit == maxInt() {
		s.stats.recordFullPelSAD(1, false)
		return s.ctx.fullPelCostFull(row, col, s.refRow8, s.refCol8, s.qIndex)
	}
	return s.ctx.fullPelCostLimited(int(mv.Row), int(mv.Col), limit, s.refRow8, s.refCol8, s.qIndex)
}

func (s *fullPelMotionSearch) nstep(center vp8enc.MotionVector, centerWalkCost int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	return s.steppedDiamond(center, centerWalkCost, search, interFrameNstepSites[:], 8)
}

func (s *fullPelMotionSearch) diamond(center vp8enc.MotionVector, centerWalkCost int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	return s.steppedDiamond(center, centerWalkCost, search, interFrameDiamondSites[:], 4)
}

func (s *fullPelMotionSearch) steppedDiamond(center vp8enc.MotionVector, centerWalkCost int, search interAnalysisSearchConfig, sites []vp8enc.MotionVector, sitesPerStep int) (vp8enc.MotionVector, int) {
	stepParam := min(max(int(search.fullPixelSearchParam), 0), interFrameMaxMVSearchSteps-1)

	result := s.searchSites(center, centerWalkCost, sites, sitesPerStep, stepParam)
	best := result.mv
	bestCost := result.cost
	n := int(result.num00)
	num00 := 0
	doRefine := search.fullPixelFinalRefine
	furtherSteps := int(search.fullPixelFurtherSteps)
	if n > furtherSteps {
		doRefine = false
	}
	for n < furtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := s.searchSites(center, centerWalkCost, sites, sitesPerStep, stepParam+n)
		num00 = int(candidate.num00)
		if search.fullPixelFinalRefine && num00 > furtherSteps-n {
			doRefine = false
		}
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	if doRefine {
		refined, refinedCost := s.refine(best, 8)
		if refinedCost < bestCost {
			best = refined
			bestCost = refinedCost
		}
	}
	return best, bestCost
}

func (s *fullPelMotionSearch) searchSites(center vp8enc.MotionVector, centerWalkCost int, sites []vp8enc.MotionVector, sitesPerStep int, searchParam int) interFrameNstepSearchResult {
	if sitesPerStep <= 0 || len(sites) < 1+sitesPerStep {
		return interFrameNstepSearchResult{mv: center, cost: s.cost(center)}
	}
	searchParam = min(max(searchParam, 0), interFrameMaxMVSearchSteps-1)
	bestRow := int(center.Row) >> 3
	bestCol := int(center.Col) >> 3
	bestWalkCost := centerWalkCost
	startRow := bestRow
	startCol := bestCol
	startIndex := searchParam * sitesPerStep
	totalSteps := (len(sites) / sitesPerStep) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	bounds := s.bounds
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	qIndex := s.qIndex
	var sad4 [4]uint32
	for range totalSteps {
		for stepSite := 0; stepSite < sitesPerStep; {
			siteIndex := startIndex + i
			if siteIndex >= len(sites) {
				break
			}
			if stepSite+4 <= sitesPerStep && siteIndex+3 < len(sites) {
				// Re-slice to a fixed-len 4 chunk so the compiler can
				// prove chunk[0..3] is in bounds without four separate
				// IsInBounds checks.
				chunk := sites[siteIndex : siteIndex+4 : siteIndex+4]
				site0 := chunk[0]
				site1 := chunk[1]
				site2 := chunk[2]
				site3 := chunk[3]
				row0 := bestRow + int(site0.Row)
				col0 := bestCol + int(site0.Col)
				row1 := bestRow + int(site1.Row)
				col1 := bestCol + int(site1.Col)
				row2 := bestRow + int(site2.Row)
				col2 := bestCol + int(site2.Col)
				row3 := bestRow + int(site3.Row)
				col3 := bestCol + int(site3.Col)
				if bounds.containsFullPelStrict(row0, col0) &&
					bounds.containsFullPelStrict(row1, col1) &&
					bounds.containsFullPelStrict(row2, col2) &&
					bounds.containsFullPelStrict(row3, col3) &&
					ctx.fullPelSADFull4(row0, col0, row1, col1, row2, col2, row3, col3, &sad4) {
					s.stats.recordFullPelSAD(4, true)
					sad := int(sad4[0])
					if sad < bestWalkCost {
						cost := sad + libvpxFullPelMVSADCost16FromDeltas(row0, col0, refRow8, refCol8, qIndex)
						if cost < bestWalkCost {
							bestWalkCost = cost
							bestSite = i
						}
					}
					sad = int(sad4[1])
					if sad < bestWalkCost {
						cost := sad + libvpxFullPelMVSADCost16FromDeltas(row1, col1, refRow8, refCol8, qIndex)
						if cost < bestWalkCost {
							bestWalkCost = cost
							bestSite = i + 1
						}
					}
					sad = int(sad4[2])
					if sad < bestWalkCost {
						cost := sad + libvpxFullPelMVSADCost16FromDeltas(row2, col2, refRow8, refCol8, qIndex)
						if cost < bestWalkCost {
							bestWalkCost = cost
							bestSite = i + 2
						}
					}
					sad = int(sad4[3])
					if sad < bestWalkCost {
						cost := sad + libvpxFullPelMVSADCost16FromDeltas(row3, col3, refRow8, refCol8, qIndex)
						if cost < bestWalkCost {
							bestWalkCost = cost
							bestSite = i + 3
						}
					}
					i += 4
					stepSite += 4
					continue
				}
			}
			site := sites[siteIndex]
			row := bestRow + int(site.Row)
			col := bestCol + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				s.stats.recordFullPelSAD(1, false)
				sad := ctx.fullPelSADFull(row, col)
				if sad < bestWalkCost {
					cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
					if cost < bestWalkCost {
						bestWalkCost = cost
						bestSite = i
					}
				}
			} else {
				s.stats.recordFullPelBoundsRejects(1)
			}
			i++
			stepSite++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			bestRow += int(site.Row)
			bestCol += int(site.Col)
			lastSite = bestSite
		} else if bestRow == startRow && bestCol == startCol {
			num00++
		}
	}
	best := vp8enc.MotionVector{Row: int16(bestRow * interFrameMVFullPixelStep), Col: int16(bestCol * interFrameMVFullPixelStep)}
	return interFrameNstepSearchResult{cost: s.cost(best), mv: best, num00: uint8(num00)}
}

func (s *fullPelMotionSearch) refine(start vp8enc.MotionVector, searchRange int) (vp8enc.MotionVector, int) {
	neighbors := [...]vp8enc.MotionVector{
		{Row: -1},
		{Col: -1},
		{Col: 1},
		{Row: 1},
	}
	bestRow := int(start.Row) >> 3
	bestCol := int(start.Col) >> 3
	bestWalkCost := s.ctx.fullPelCostFull(bestRow, bestCol, s.refRow8, s.refCol8, s.qIndex)
	bounds := s.bounds
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	qIndex := s.qIndex
	var sad4 [4]uint32
	for range searchRange {
		bestSite := -1
		if bounds.containsFullPelStrict(bestRow-1, bestCol) &&
			bounds.containsFullPelStrict(bestRow, bestCol-1) &&
			bounds.containsFullPelStrict(bestRow, bestCol+1) &&
			bounds.containsFullPelStrict(bestRow+1, bestCol) &&
			ctx.fullPelSADFull4(bestRow-1, bestCol, bestRow, bestCol-1, bestRow, bestCol+1, bestRow+1, bestCol, &sad4) {
			s.stats.recordFullPelSAD(4, true)
			sad := int(sad4[0])
			if sad < bestWalkCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(bestRow-1, bestCol, refRow8, refCol8, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = 0
				}
			}
			sad = int(sad4[1])
			if sad < bestWalkCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(bestRow, bestCol-1, refRow8, refCol8, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = 1
				}
			}
			sad = int(sad4[2])
			if sad < bestWalkCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(bestRow, bestCol+1, refRow8, refCol8, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = 2
				}
			}
			sad = int(sad4[3])
			if sad < bestWalkCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(bestRow+1, bestCol, refRow8, refCol8, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = 3
				}
			}
			if bestSite < 0 {
				s.stats.recordFullPelEarlyBreak()
				break
			}
			// neighbors has fixed len 4 (power of 2). bestSite is in
			// [0,3] after the `if bestSite < 0 { break }` guard above,
			// so `& 3` is a no-op functionally but lets the compiler
			// elide the bounds check on neighbors[bestSite].
			bestRow += int(neighbors[bestSite&3].Row)
			bestCol += int(neighbors[bestSite&3].Col)
			continue
		}
		for j, step := range neighbors {
			row := bestRow + int(step.Row)
			col := bestCol + int(step.Col)
			if !bounds.containsFullPelStrict(row, col) {
				s.stats.recordFullPelBoundsRejects(1)
				continue
			}
			s.stats.recordFullPelSAD(1, false)
			sad := ctx.fullPelSADFull(row, col)
			if sad < bestWalkCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = j
				}
			}
		}
		if bestSite < 0 {
			s.stats.recordFullPelEarlyBreak()
			break
		}
		// neighbors has fixed len 4 (power of 2); bestSite is in [0,3]
		// after the negative-bestSite break above, so `& 3` elides the
		// bounds check on neighbors[bestSite].
		bestRow += int(neighbors[bestSite&3].Row)
		bestCol += int(neighbors[bestSite&3].Col)
	}
	best := vp8enc.MotionVector{Row: int16(bestRow * interFrameMVFullPixelStep), Col: int16(bestCol * interFrameMVFullPixelStep)}
	return best, s.cost(best)
}

func (s *fullPelMotionSearch) exhaustive(best vp8enc.MotionVector, bestWalkCost int) (vp8enc.MotionVector, int) {
	centerRow := int(s.bestRefMV.Row) >> 3
	centerCol := int(s.bestRefMV.Col) >> 3
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	bestRow := int(best.Row) >> 3
	bestCol := int(best.Col) >> 3
	for row := centerRow - interFrameFullPixelSearchRadius; row <= centerRow+interFrameFullPixelSearchRadius; row++ {
		for col := centerCol - interFrameFullPixelSearchRadius; col <= centerCol+interFrameFullPixelSearchRadius; col++ {
			if row == bestRow && col == bestCol {
				continue
			}
			s.stats.recordFullPelSAD(1, false)
			sad := ctx.fullPelSADFull(row, col)
			if sad < bestWalkCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, s.qIndex)
				if cost < bestWalkCost {
					bestRow = row
					bestCol = col
					bestWalkCost = cost
				}
			}
		}
	}
	best = vp8enc.MotionVector{Row: int16(bestRow * interFrameMVFullPixelStep), Col: int16(bestCol * interFrameMVFullPixelStep)}
	return best, s.cost(best)
}

// hex runs the libvpx-style hex_search loop (initial 6-point ring +
// 3-point next-checkpoints walk + 4-neighbour final refine) via the
// super-kernel: stats are accumulated locally, MV cost is inlined
// against a pinned per-q table, and the ±2 and ±1 site neighbourhoods
// are batched through the x4 SAD primitive when the centre is interior.
func (s *fullPelMotionSearch) hex(best vp8enc.MotionVector, bestCost int) (vp8enc.MotionVector, int) {
	return hexSuperKernel(s, best, bestCost)
}

// interFrameNstepSites and interFrameDiamondSites are package-level
// pre-computed search-site arrays reused by every full-pel motion search.
var interFrameNstepSites = buildInterFrameNstepSearchSites()
var interFrameDiamondSites = buildInterFrameDiamondSearchSites()

func buildInterFrameNstepSearchSites() [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector {
	var sites [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector
	count := 1
	for length := 1 << (interFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(length)}
		count++
	}
	return sites
}

func buildInterFrameDiamondSearchSites() [1 + interFrameMaxMVSearchSteps*4]vp8enc.MotionVector {
	var sites [1 + interFrameMaxMVSearchSteps*4]vp8enc.MotionVector
	count := 1
	for length := 1 << (interFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(length)}
		count++
	}
	return sites
}
