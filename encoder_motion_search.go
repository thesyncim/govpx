package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type interFrameNstepSearchResult struct {
	mv    vp8enc.MotionVector
	cost  int
	num00 int
}

type interFrameMotionSearchStats struct {
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
	if batch {
		s.fullPelBatchCalls++
	}
}

func (s *interFrameMotionSearchStats) recordFullPelBoundsRejects(count int) {
	if s == nil || count <= 0 {
		return
	}
	s.fullPelBoundsRejects += count
}

func (s *interFrameMotionSearchStats) recordFullPelEarlyBreak() {
	if s == nil {
		return
	}
	s.fullPelEarlyBreaks++
}

func (s *interFrameMotionSearchStats) recordSubpelCandidate() {
	if s == nil {
		return
	}
	s.subpelCandidates++
}

func (s *interFrameMotionSearchStats) recordSubpelVariance() {
	if s == nil {
		return
	}
	s.subpelVarianceCalls++
}

func (s *interFrameMotionSearchStats) recordSubpelCacheHit() {
	if s == nil {
		return
	}
	s.subpelCacheHits++
}

func (s *interFrameMotionSearchStats) recordSubpelBoundsReject() {
	if s == nil {
		return
	}
	s.subpelBoundsRejects++
}

func (s *interFrameMotionSearchStats) recordSubpelEarlyBreak() {
	if s == nil {
		return
	}
	s.subpelEarlyBreaks++
}

type fullPelMotionSearch struct {
	ctx       fullPelSearchCtx
	bounds    interFrameFullPixelBounds
	mvProbs   *[2][vp8tables.MVPCount]uint8
	qIndex    int
	refRow8   int
	refCol8   int
	bestRefMV vp8enc.MotionVector
	stats     *interFrameMotionSearchStats
}

func newFullPelMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, mvProbs *[2][vp8tables.MVPCount]uint8, stats *interFrameMotionSearchStats) fullPelMotionSearch {
	return fullPelMotionSearch{
		ctx:       newFullPelSearchCtx(src, ref, mbRow, mbCol),
		bounds:    bounds,
		mvProbs:   mvProbs,
		qIndex:    qIndex,
		refRow8:   int(bestRefMV.Row) >> 3,
		refCol8:   int(bestRefMV.Col) >> 3,
		bestRefMV: bestRefMV,
		stats:     stats,
	}
}

func (s *fullPelMotionSearch) cost(mv vp8enc.MotionVector) int {
	s.stats.recordFullPelSAD(1, false)
	return interMotionFullPixelSearchReturnCost(s.ctx.src, s.ctx.ref, s.ctx.mbRow, s.ctx.mbCol, mv, s.bestRefMV, s.qIndex, s.mvProbs)
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
	stepParam := search.fullPixelSearchParam
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}

	result := s.searchSites(center, centerWalkCost, sites, sitesPerStep, stepParam)
	best := result.mv
	bestCost := result.cost
	n := result.num00
	num00 := 0
	doRefine := search.fullPixelFinalRefine
	if n > search.fullPixelFurtherSteps {
		doRefine = false
	}
	for n < search.fullPixelFurtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := s.searchSites(center, centerWalkCost, sites, sitesPerStep, stepParam+n)
		num00 = candidate.num00
		if search.fullPixelFinalRefine && num00 > search.fullPixelFurtherSteps-n {
			doRefine = false
		}
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	if doRefine {
		best, bestCost = s.refine(best, 8)
	}
	return best, bestCost
}

func (s *fullPelMotionSearch) searchSites(center vp8enc.MotionVector, centerWalkCost int, sites []vp8enc.MotionVector, sitesPerStep int, searchParam int) interFrameNstepSearchResult {
	if sitesPerStep <= 0 || len(sites) < 1+sitesPerStep {
		return interFrameNstepSearchResult{mv: center, cost: s.cost(center)}
	}
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
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
				site0 := sites[siteIndex]
				site1 := sites[siteIndex+1]
				site2 := sites[siteIndex+2]
				site3 := sites[siteIndex+3]
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
	return interFrameNstepSearchResult{mv: best, cost: s.cost(best), num00: num00}
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
			bestRow += int(neighbors[bestSite].Row)
			bestCol += int(neighbors[bestSite].Col)
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
		bestRow += int(neighbors[bestSite].Row)
		bestCol += int(neighbors[bestSite].Col)
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

func (s *fullPelMotionSearch) hex(best vp8enc.MotionVector, bestCost int) (vp8enc.MotionVector, int) {
	hex := [...]vp8enc.MotionVector{
		{Row: -1, Col: -2},
		{Row: 1, Col: -2},
		{Row: 2, Col: 0},
		{Row: 1, Col: 2},
		{Row: -1, Col: 2},
		{Row: -2, Col: 0},
	}
	nextCheckpoints := [...][3]vp8enc.MotionVector{
		{{Row: -2, Col: 0}, {Row: -1, Col: -2}, {Row: 1, Col: -2}},
		{{Row: -1, Col: -2}, {Row: 1, Col: -2}, {Row: 2, Col: 0}},
		{{Row: 1, Col: -2}, {Row: 2, Col: 0}, {Row: 1, Col: 2}},
		{{Row: 2, Col: 0}, {Row: 1, Col: 2}, {Row: -1, Col: 2}},
		{{Row: 1, Col: 2}, {Row: -1, Col: 2}, {Row: -2, Col: 0}},
		{{Row: -1, Col: 2}, {Row: -2, Col: 0}, {Row: -1, Col: -2}},
	}
	neighbors := [...]vp8enc.MotionVector{
		{Row: 0, Col: -1},
		{Row: -1, Col: 0},
		{Row: 1, Col: 0},
		{Row: 0, Col: 1},
	}

	bestRow := int(best.Row) >> 3
	bestCol := int(best.Col) >> 3
	bestSite := -1
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	bounds := s.bounds
	qIndex := s.qIndex
	nextRow := bestRow
	nextCol := bestCol
	for i, step := range hex {
		row := bestRow + int(step.Row)
		col := bestCol + int(step.Col)
		if !bounds.containsFullPel(row, col) {
			s.stats.recordFullPelBoundsRejects(1)
			continue
		}
		s.stats.recordFullPelSAD(1, false)
		sad := ctx.fullPelSADFull(row, col)
		if sad < bestCost {
			cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
			if cost < bestCost {
				nextRow = row
				nextCol = col
				bestCost = cost
				bestSite = i
			}
		}
	}
	if bestSite >= 0 {
		bestRow = nextRow
		bestCol = nextCol
		k := bestSite
		for j := 1; j < 127; j++ {
			bestSite = -1
			nextRow = bestRow
			nextCol = bestCol
			for i, step := range nextCheckpoints[k] {
				row := bestRow + int(step.Row)
				col := bestCol + int(step.Col)
				if !bounds.containsFullPel(row, col) {
					s.stats.recordFullPelBoundsRejects(1)
					continue
				}
				s.stats.recordFullPelSAD(1, false)
				sad := ctx.fullPelSADFull(row, col)
				if sad < bestCost {
					cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
					if cost < bestCost {
						nextRow = row
						nextCol = col
						bestCost = cost
						bestSite = i
					}
				}
			}
			if bestSite < 0 {
				s.stats.recordFullPelEarlyBreak()
				break
			}
			bestRow = nextRow
			bestCol = nextCol
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	for range 8 {
		bestSite = -1
		nextRow = bestRow
		nextCol = bestCol
		for i, step := range neighbors {
			row := bestRow + int(step.Row)
			col := bestCol + int(step.Col)
			if !bounds.containsFullPel(row, col) {
				s.stats.recordFullPelBoundsRejects(1)
				continue
			}
			s.stats.recordFullPelSAD(1, false)
			sad := ctx.fullPelSADFull(row, col)
			if sad < bestCost {
				cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
				if cost < bestCost {
					nextRow = row
					nextCol = col
					bestCost = cost
					bestSite = i
				}
			}
		}
		if bestSite < 0 {
			s.stats.recordFullPelEarlyBreak()
			break
		}
		bestRow = nextRow
		bestCol = nextCol
	}
	return vp8enc.MotionVector{Row: int16(bestRow * interFrameMVFullPixelStep), Col: int16(bestCol * interFrameMVFullPixelStep)}, bestCost
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
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(length)}
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
