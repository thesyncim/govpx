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

type fullPelMotionSearch struct {
	ctx       fullPelSearchCtx
	bounds    interFrameFullPixelBounds
	mvProbs   *[2][vp8tables.MVPCount]uint8
	qIndex    int
	refRow8   int
	refCol8   int
	bestRefMV vp8enc.MotionVector
}

func newFullPelMotionSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, mvProbs *[2][vp8tables.MVPCount]uint8) fullPelMotionSearch {
	return fullPelMotionSearch{
		ctx:       newFullPelSearchCtx(src, ref, mbRow, mbCol),
		bounds:    bounds,
		mvProbs:   mvProbs,
		qIndex:    qIndex,
		refRow8:   int(bestRefMV.Row) >> 3,
		refCol8:   int(bestRefMV.Col) >> 3,
		bestRefMV: bestRefMV,
	}
}

func (s *fullPelMotionSearch) cost(mv vp8enc.MotionVector) int {
	return interMotionFullPixelSearchReturnCost(s.ctx.src, s.ctx.ref, s.ctx.mbRow, s.ctx.mbCol, mv, s.bestRefMV, s.qIndex, s.mvProbs)
}

func (s *fullPelMotionSearch) walkCost(mv vp8enc.MotionVector, limit int) int {
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	if limit == maxInt() {
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
	for range totalSteps {
		for range sitesPerStep {
			siteIndex := startIndex + i
			if siteIndex >= len(sites) {
				break
			}
			site := sites[siteIndex]
			row := bestRow + int(site.Row)
			col := bestCol + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				sad := ctx.fullPelSADFull(row, col)
				if sad < bestWalkCost {
					cost := sad + libvpxFullPelMVSADCost16FromDeltas(row, col, refRow8, refCol8, qIndex)
					if cost < bestWalkCost {
						bestWalkCost = cost
						bestSite = i
					}
				}
			}
			i++
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
	for range searchRange {
		bestSite := -1
		for j, step := range neighbors {
			row := bestRow + int(step.Row)
			col := bestCol + int(step.Col)
			if !bounds.containsFullPelStrict(row, col) {
				continue
			}
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
			continue
		}
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
					continue
				}
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
				continue
			}
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
