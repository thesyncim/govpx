package encoder

type fullpelPatternCandidate struct {
	row int
	col int
}

var bigDiamondPatternCandidates = [MaxMvSearchSteps][8]fullpelPatternCandidate{
	{{0, -1}, {1, 0}, {0, 1}, {-1, 0}},
	{{-1, -1}, {0, -2}, {1, -1}, {2, 0}, {1, 1}, {0, 2}, {-1, 1}, {-2, 0}},
	{{-2, -2}, {0, -4}, {2, -2}, {4, 0}, {2, 2}, {0, 4}, {-2, 2}, {-4, 0}},
	{{-4, -4}, {0, -8}, {4, -4}, {8, 0}, {4, 4}, {0, 8}, {-4, 4}, {-8, 0}},
	{{-8, -8}, {0, -16}, {8, -8}, {16, 0}, {8, 8}, {0, 16}, {-8, 8}, {-16, 0}},
	{{-16, -16}, {0, -32}, {16, -16}, {32, 0}, {16, 16}, {0, 32}, {-16, 16}, {-32, 0}},
	{{-32, -32}, {0, -64}, {32, -32}, {64, 0}, {32, 32}, {0, 64}, {-32, 32}, {-64, 0}},
	{{-64, -64}, {0, -128}, {64, -64}, {128, 0}, {64, 64}, {0, 128}, {-64, 64}, {-128, 0}},
	{{-128, -128}, {0, -256}, {128, -128}, {256, 0}, {128, 128}, {0, 256}, {-128, 128}, {-256, 0}},
	{{-256, -256}, {0, -512}, {256, -512}, {512, 0}, {256, 256}, {0, 512}, {-256, 256}, {-512, 0}},
	{{-512, -512}, {0, -1024}, {512, -512}, {1024, 0}, {512, 512}, {0, 1024}, {-512, 512}, {-1024, 0}},
}

var bigDiamondPatternCandidateCounts = [MaxMvSearchSteps]int{
	4, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

var hexPatternCandidates = [MaxMvSearchSteps][8]fullpelPatternCandidate{
	{{-1, -1}, {0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}},
	{{-1, -2}, {1, -2}, {2, 0}, {1, 2}, {-1, 2}, {-2, 0}},
	{{-2, -4}, {2, -4}, {4, 0}, {2, 4}, {-2, 4}, {-4, 0}},
	{{-4, -8}, {4, -8}, {8, 0}, {4, 8}, {-4, 8}, {-8, 0}},
	{{-8, -16}, {8, -16}, {16, 0}, {8, 16}, {-8, 16}, {-16, 0}},
	{{-16, -32}, {16, -32}, {32, 0}, {16, 32}, {-16, 32}, {-32, 0}},
	{{-32, -64}, {32, -64}, {64, 0}, {32, 64}, {-32, 64}, {-64, 0}},
	{{-64, -128}, {64, -128}, {128, 0}, {64, 128}, {-64, 128}, {-128, 0}},
	{{-128, -256}, {128, -256}, {256, 0}, {128, 256}, {-128, 256}, {-256, 0}},
	{{-256, -512}, {256, -512}, {512, 0}, {256, 512}, {-256, 512}, {-512, 0}},
	{{-512, -1024}, {512, -1024}, {1024, 0}, {512, 1024}, {-512, 1024}, {-1024, 0}},
}

var hexPatternCandidateCounts = [MaxMvSearchSteps]int{
	8, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
}

func FastDiamondPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return fastPatternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, limits, sadAt, scoreMv,
		&bigDiamondPatternCandidateCounts, &bigDiamondPatternCandidates)
}

func FastHexPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return fastPatternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, limits, sadAt, scoreMv,
		&hexPatternCandidateCounts, &hexPatternCandidates)
}

func fastPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
	candidateCounts *[MaxMvSearchSteps]int,
	candidates *[MaxMvSearchSteps][8]fullpelPatternCandidate,
) (int, int, uint64, uint64) {
	searchParam := max(max(MaxMvSearchSteps-2, stepParam), 0)
	if searchParam >= MaxMvSearchSteps {
		searchParam = MaxMvSearchSteps - 1
	}
	searchParamToSteps := [...]int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	bestInitS := searchParamToSteps[searchParam]

	br, bc := limits.ClampFullpel(startDy, startDx)
	bestSad := startSad
	bestScore := startScore
	if br != startDy || bc != startDx {
		if sad, ok := sadAt(bc, br); ok {
			bestSad = sad
			bestScore = scoreMv(bc, br, sad)
		}
	}

	checkBetter := func(s, site, row, col int, bestSite *int) {
		if row == br && col == bc {
			return
		}
		if !limits.FullpelBoundsOK(br, bc, 1<<s) &&
			!limits.InFullpelRange(row, col) {
			return
		}
		sad, ok := sadAt(col, row)
		if !ok {
			return
		}
		if sad >= bestScore {
			return
		}
		score := scoreMv(col, row, sad)
		if score >= bestScore {
			return
		}
		bestSad = sad
		bestScore = score
		*bestSite = site
	}

	k := -1
	for s := bestInitS; s >= 0; s-- {
		bestSite := -1
		numCandidates := candidateCounts[s]
		for i := range numCandidates {
			c := candidates[s][i]
			checkBetter(s, i, br+c.row, bc+c.col, &bestSite)
		}
		if bestSite != -1 {
			c := candidates[s][bestSite]
			br += c.row
			bc += c.col
			k = bestSite
		}
		for bestSite != -1 {
			next := [3]int{
				k - 1,
				k,
				k + 1,
			}
			if next[0] < 0 {
				next[0] = numCandidates - 1
			}
			if next[2] == numCandidates {
				next[2] = 0
			}
			bestSite = -1
			for _, site := range next {
				c := candidates[s][site]
				checkBetter(s, site, br+c.row, bc+c.col, &bestSite)
			}
			if bestSite != -1 {
				k = bestSite
				c := candidates[s][k]
				br += c.row
				bc += c.col
			}
		}
	}
	return bc, br, bestSad, bestScore
}

func NStepDiamondSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	searchParam := max(min(stepParam, MaxMvSearchSteps-1), 0)
	br, bc := limits.ClampFullpel(startDy, startDx)
	bestSad := startSad
	bestScore := startScore
	if br != startDy || bc != startDx {
		if sad, ok := sadAt(bc, br); ok {
			bestSad = sad
			bestScore = scoreMv(bc, br, sad)
		}
	}
	searchStartSad := bestSad
	searchStartScore := bestScore
	bestDx, bestDy := bc, br

	furtherSteps := MaxMvSearchSteps - 1 - searchParam
	for n := 0; n <= furtherSteps; n++ {
		candDx, candDy, candSad, candScore := nStepDiamondOnceSAD(
			bc, br, searchStartSad, searchStartScore, searchParam+n,
			limits, sadAt, scoreMv)
		if candScore < bestScore {
			bestDx = candDx
			bestDy = candDy
			bestSad = candSad
			bestScore = candScore
		}
	}

	for range 8 {
		bestSite := -1
		for site, c := range nStepRefineCandidates {
			row := bestDy + c.row
			col := bestDx + c.col
			if !limits.InFullpelRange(row, col) {
				continue
			}
			sad, ok := sadAt(col, row)
			if !ok || sad >= bestScore {
				continue
			}
			score := scoreMv(col, row, sad)
			if score < bestScore {
				bestSad = sad
				bestScore = score
				bestSite = site
			}
		}
		if bestSite == -1 {
			break
		}
		c := nStepRefineCandidates[bestSite]
		bestDy += c.row
		bestDx += c.col
	}

	return bestDx, bestDy, bestSad, bestScore
}

func nStepDiamondOnceSAD(startDx, startDy int,
	startSad, startScore uint64, searchParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	br, bc := startDy, startDx
	bestSad := startSad
	bestScore := startScore
	firstStep := 1 << (MaxMvSearchSteps - 1 - searchParam)
	for step := firstStep; step >= 1; step >>= 1 {
		bestSite := -1
		for i, c := range nStepDiamondCandidates {
			row := br + c.row*step
			col := bc + c.col*step
			if !limits.InFullpelRange(row, col) {
				continue
			}
			sad, ok := sadAt(col, row)
			if !ok || sad >= bestScore {
				continue
			}
			score := scoreMv(col, row, sad)
			if score < bestScore {
				bestSad = sad
				bestScore = score
				bestSite = i
			}
		}
		if bestSite != -1 {
			c := nStepDiamondCandidates[bestSite]
			br += c.row * step
			bc += c.col * step
		}
	}
	return bc, br, bestSad, bestScore
}

var nStepDiamondCandidates = [...]fullpelPatternCandidate{
	{-1, 0},
	{1, 0},
	{0, -1},
	{0, 1},
	{-1, -1},
	{-1, 1},
	{1, -1},
	{1, 1},
}

var nStepRefineCandidates = [...]fullpelPatternCandidate{
	{-1, 0},
	{0, -1},
	{0, 1},
	{1, 0},
	{-1, -1},
	{-1, 1},
	{1, -1},
	{1, 1},
}
