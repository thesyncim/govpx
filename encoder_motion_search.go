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
	best := center
	bestWalkCost := centerWalkCost
	start := center
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
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				mvRow := row * interFrameMVFullPixelStep
				mvCol := col * interFrameMVFullPixelStep
				cost := ctx.fullPelCostLimited(mvRow, mvCol, bestWalkCost, refRow8, refCol8, qIndex)
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
	return interFrameNstepSearchResult{mv: best, cost: s.cost(best), num00: num00}
}

func (s *fullPelMotionSearch) refine(start vp8enc.MotionVector, searchRange int) (vp8enc.MotionVector, int) {
	neighbors := [...]vp8enc.MotionVector{
		{Row: -1},
		{Col: -1},
		{Col: 1},
		{Row: 1},
	}
	best := start
	bestWalkCost := s.walkCost(best, maxInt())
	bounds := s.bounds
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	qIndex := s.qIndex
	for range searchRange {
		bestSite := -1
		br := int(best.Row) >> 3
		bc := int(best.Col) >> 3
		for j, step := range neighbors {
			row := br + int(step.Row)
			col := bc + int(step.Col)
			if !bounds.containsFullPelStrict(row, col) {
				continue
			}
			mvRow := row * interFrameMVFullPixelStep
			mvCol := col * interFrameMVFullPixelStep
			cost := ctx.fullPelCostLimited(mvRow, mvCol, bestWalkCost, refRow8, refCol8, qIndex)
			if cost < bestWalkCost {
				bestWalkCost = cost
				bestSite = j
			}
		}
		if bestSite < 0 {
			break
		}
		best = vp8enc.MotionVector{
			Row: int16(int(best.Row) + int(neighbors[bestSite].Row)*interFrameMVFullPixelStep),
			Col: int16(int(best.Col) + int(neighbors[bestSite].Col)*interFrameMVFullPixelStep),
		}
	}
	return best, s.cost(best)
}

func (s *fullPelMotionSearch) exhaustive(best vp8enc.MotionVector, bestWalkCost int) (vp8enc.MotionVector, int) {
	centerRow := int(s.bestRefMV.Row) & ^7
	centerCol := int(s.bestRefMV.Col) & ^7
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	bestMVRow := int(best.Row)
	bestMVCol := int(best.Col)
	for row := centerRow - interFrameMVSearchRange; row <= centerRow+interFrameMVSearchRange; row += interFrameMVFullPixelStep {
		for col := centerCol - interFrameMVSearchRange; col <= centerCol+interFrameMVSearchRange; col += interFrameMVFullPixelStep {
			if row == bestMVRow && col == bestMVCol {
				continue
			}
			cost := ctx.fullPelCostLimited(row, col, bestWalkCost, refRow8, refCol8, s.qIndex)
			if cost < bestWalkCost {
				bestMVRow = row
				bestMVCol = col
				bestWalkCost = cost
			}
		}
	}
	best = vp8enc.MotionVector{Row: int16(bestMVRow), Col: int16(bestMVCol)}
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

	br := int(best.Row) >> 3
	bc := int(best.Col) >> 3
	bestSite := -1
	ctx := &s.ctx
	refRow8 := s.refRow8
	refCol8 := s.refCol8
	bounds := s.bounds
	qIndex := s.qIndex
	bestMVRow := int(best.Row)
	bestMVCol := int(best.Col)
	for i, step := range hex {
		row := br + int(step.Row)
		col := bc + int(step.Col)
		if !bounds.containsFullPel(row, col) {
			continue
		}
		mvRow := row * interFrameMVFullPixelStep
		mvCol := col * interFrameMVFullPixelStep
		cost := ctx.fullPelCostLimited(mvRow, mvCol, bestCost, refRow8, refCol8, qIndex)
		if cost < bestCost {
			bestMVRow = mvRow
			bestMVCol = mvCol
			bestCost = cost
			bestSite = i
		}
	}
	if bestSite >= 0 {
		br = bestMVRow >> 3
		bc = bestMVCol >> 3
		k := bestSite
		for j := 1; j < 127; j++ {
			bestSite = -1
			for i, step := range nextCheckpoints[k] {
				row := br + int(step.Row)
				col := bc + int(step.Col)
				if !bounds.containsFullPel(row, col) {
					continue
				}
				mvRow := row * interFrameMVFullPixelStep
				mvCol := col * interFrameMVFullPixelStep
				cost := ctx.fullPelCostLimited(mvRow, mvCol, bestCost, refRow8, refCol8, qIndex)
				if cost < bestCost {
					bestMVRow = mvRow
					bestMVCol = mvCol
					bestCost = cost
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br = bestMVRow >> 3
			bc = bestMVCol >> 3
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	br = bestMVRow >> 3
	bc = bestMVCol >> 3
	for range 8 {
		bestSite = -1
		for i, step := range neighbors {
			row := br + int(step.Row)
			col := bc + int(step.Col)
			if !bounds.containsFullPel(row, col) {
				continue
			}
			mvRow := row * interFrameMVFullPixelStep
			mvCol := col * interFrameMVFullPixelStep
			cost := ctx.fullPelCostLimited(mvRow, mvCol, bestCost, refRow8, refCol8, qIndex)
			if cost < bestCost {
				bestMVRow = mvRow
				bestMVCol = mvCol
				bestCost = cost
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br = bestMVRow >> 3
		bc = bestMVCol >> 3
	}
	return vp8enc.MotionVector{Row: int16(bestMVRow), Col: int16(bestMVCol)}, bestCost
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
