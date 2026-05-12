package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, searchCenter vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src, ref, mbRow, mbCol, block, width, height, searchCenter, bestRefMV, qIndex, 0, true, &vp8tables.DefaultMVContext)
}

func selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, searchCenter vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, stepParam int, fullSearchFallback bool, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	centerRow := int(searchCenter.Row) & ^7
	centerCol := int(searchCenter.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := interFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	best = bounds.clampEighth(best)
	centerWalkCost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	centerReturnCost, ok := interMotionSplitBlockFullPixelReturnCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, mvProbs)
	if !ok {
		centerReturnCost = maxInt()
	}
	best, bestReturnCost := nstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, centerWalkCost, centerReturnCost, bestRefMV, qIndex, mvProbs, bounds, stepParam)
	if fullSearchFallback && splitMotionFullSearchFallbackNeeded(bestReturnCost, width, height) {
		candidate := fullSearchInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, bounds.clampEighth(vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}), bestRefMV, qIndex, mvProbs, bounds, interFrameFullPixelSearchRadius)
		if candidate.returnCost < bestReturnCost {
			best = candidate.mv
			bestReturnCost = candidate.returnCost
		}
	}
	return best, bestReturnCost
}

type splitFullPixelSearchResult struct {
	mv         vp8enc.MotionVector
	walkCost   int
	returnCost int
	num00      int
}

func nstepInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, centerWalkCost int, centerReturnCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8, bounds interFrameFullPixelBounds, stepParam int) (vp8enc.MotionVector, int) {
	stepParam = min(max(stepParam, 0), interFrameMaxMVSearchSteps-1)
	furtherSteps := (interFrameMaxMVSearchSteps - 1) - stepParam
	result := diamondNstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, center, centerWalkCost, centerReturnCost, bestRefMV, qIndex, mvProbs, bounds, stepParam)
	best := result.mv
	bestCost := result.returnCost
	n := result.num00
	num00 := 0
	for n < furtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := diamondNstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, center, centerWalkCost, centerReturnCost, bestRefMV, qIndex, mvProbs, bounds, stepParam+n)
		num00 = candidate.num00
		if candidate.returnCost < bestCost {
			best = candidate.mv
			bestCost = candidate.returnCost
		}
	}
	return best, bestCost
}

func diamondNstepInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, centerWalkCost int, centerReturnCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8, bounds interFrameFullPixelBounds, searchParam int) splitFullPixelSearchResult {
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
	returnCost := centerReturnCost
	if best != center {
		var ok bool
		returnCost, ok = interMotionSplitBlockFullPixelReturnCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, mvProbs)
		if !ok {
			returnCost = maxInt()
		}
	}
	return splitFullPixelSearchResult{mv: best, walkCost: bestWalkCost, returnCost: returnCost, num00: num00}
}

func splitMotionFullSearchFallbackNeeded(returnCost int, width int, height int) bool {
	shift := splitMotionSegmentationSSEShift(width, height)
	return (returnCost >> shift) > interFrameSplitMVFullSearchThreshold
}

func interMotionSplitBlockFullPixelReturnCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, bool) {
	variance, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, int(mv.Row)/2, int(mv.Col)/2)
	if !ok {
		return maxInt(), false
	}
	return variance + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, mvProbs), true
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

func fullSearchInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8, bounds interFrameFullPixelBounds, distance int) splitFullPixelSearchResult {
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
	best := center
	bestWalkCost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	for row := rowMin; row < rowMax; row++ {
		for col := colMin; col < colMax; col++ {
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, bestRefMV, qIndex)
			if cost < bestWalkCost {
				best = mv
				bestWalkCost = cost
			}
		}
	}
	returnCost, ok := interMotionSplitBlockFullPixelReturnCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, mvProbs)
	if !ok {
		returnCost = maxInt()
	}
	return splitFullPixelSearchResult{mv: best, walkCost: bestWalkCost, returnCost: returnCost}
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
	bestDist, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, br, bc)
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
	dist, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, row, col)
	if !ok {
		return maxInt(), false
	}
	mv := vp8enc.MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
	// Iterative subpel candidate cost: libvpx CHECK_BETTER uses the MVC
	// macro (1/4-pel signed index built from `(mv>>1) - (ref>>1)`), not
	// mv_err_cost.
	return dist + interMotionSubpelCandidateVectorCost(mv, bestRefMV, qIndex, mvProbs), true
}

func splitBlockSubpixelVarianceForQuarterMV(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, row int, col int) (int, bool) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	if baseY < 0 || baseX < 0 || baseY+height > src.Height || baseX+width > src.Width {
		return 0, false
	}
	refBaseY := baseY + (row >> 2)
	refBaseX := baseX + (col >> 2)
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	variance, _, ok := splitBlockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset)
	return variance, ok
}
