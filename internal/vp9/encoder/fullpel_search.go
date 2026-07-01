package encoder

type fullpelPatternCandidate struct {
	row int
	col int
}

// PatternSAD4Func compares four full-pel candidates in candidate order.
type PatternSAD4Func func(dx0, dy0, dx1, dy1, dx2, dy2, dx3, dy3 int) (sad0, sad1, sad2, sad3 uint64, ok bool)

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
	{{-256, -256}, {0, -512}, {256, -256}, {512, 0}, {256, 256}, {0, 512}, {-256, 256}, {-512, 0}},
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

var squarePatternCandidates = [MaxMvSearchSteps][8]fullpelPatternCandidate{
	{{-1, -1}, {0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}},
	{{-2, -2}, {0, -2}, {2, -2}, {2, 0}, {2, 2}, {0, 2}, {-2, 2}, {-2, 0}},
	{{-4, -4}, {0, -4}, {4, -4}, {4, 0}, {4, 4}, {0, 4}, {-4, 4}, {-4, 0}},
	{{-8, -8}, {0, -8}, {8, -8}, {8, 0}, {8, 8}, {0, 8}, {-8, 8}, {-8, 0}},
	{{-16, -16}, {0, -16}, {16, -16}, {16, 0}, {16, 16}, {0, 16}, {-16, 16}, {-16, 0}},
	{{-32, -32}, {0, -32}, {32, -32}, {32, 0}, {32, 32}, {0, 32}, {-32, 32}, {-32, 0}},
	{{-64, -64}, {0, -64}, {64, -64}, {64, 0}, {64, 64}, {0, 64}, {-64, 64}, {-64, 0}},
	{{-128, -128}, {0, -128}, {128, -128}, {128, 0}, {128, 128}, {0, 128}, {-128, 128}, {-128, 0}},
	{{-256, -256}, {0, -256}, {256, -256}, {256, 0}, {256, 256}, {0, 256}, {-256, 256}, {-256, 0}},
	{{-512, -512}, {0, -512}, {512, -512}, {512, 0}, {512, 512}, {0, 512}, {-512, 512}, {-512, 0}},
	{{-1024, -1024}, {0, -1024}, {1024, -1024}, {1024, 0}, {1024, 1024}, {0, 1024}, {-1024, 1024}, {-1024, 0}},
}

var squarePatternCandidateCounts = [MaxMvSearchSteps]int{
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

func FastDiamondPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return FastDiamondPatternSearchSADWithBatch(startDx, startDy, startSad,
		startScore, stepParam, limits, sadAt, nil, scoreMv)
}

func FastDiamondPatternSearchSADWithBatch(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool), sadAt4 PatternSAD4Func,
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return fastPatternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, limits, sadAt, sadAt4, scoreMv,
		&bigDiamondPatternCandidateCounts, &bigDiamondPatternCandidates)
}

func FastHexPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return FastHexPatternSearchSADWithBatch(startDx, startDy, startSad,
		startScore, stepParam, limits, sadAt, nil, scoreMv)
}

func FastHexPatternSearchSADWithBatch(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool), sadAt4 PatternSAD4Func,
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return fastPatternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, limits, sadAt, sadAt4, scoreMv,
		&hexPatternCandidateCounts, &hexPatternCandidates)
}

func BigDiamondPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return patternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, true, limits, sadAt, scoreMv,
		&bigDiamondPatternCandidateCounts, &bigDiamondPatternCandidates)
}

func HexPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return patternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, true, limits, sadAt, scoreMv,
		&hexPatternCandidateCounts, &hexPatternCandidates)
}

func SquarePatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return patternSearchSAD(startDx, startDy, startSad, startScore,
		stepParam, true, limits, sadAt, scoreMv,
		&squarePatternCandidateCounts, &squarePatternCandidates)
}

func fastPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool), sadAt4 PatternSAD4Func,
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
	checkBetter4 := func(s int, sites [4]int, rows [4]int, cols [4]int,
		bestSite *int,
	) bool {
		if sadAt4 == nil {
			return false
		}
		searchRange := 1 << s
		boundsOK := limits.FullpelBoundsOK(br, bc, searchRange)
		for i := 0; i < 4; i++ {
			if rows[i] == br && cols[i] == bc {
				return false
			}
			if !boundsOK && !limits.InFullpelRange(rows[i], cols[i]) {
				return false
			}
		}
		sad0, sad1, sad2, sad3, ok := sadAt4(cols[0], rows[0], cols[1],
			rows[1], cols[2], rows[2], cols[3], rows[3])
		if !ok {
			return false
		}
		sads := [4]uint64{sad0, sad1, sad2, sad3}
		for i, sad := range sads {
			if sad >= bestScore {
				continue
			}
			score := scoreMv(cols[i], rows[i], sad)
			if score >= bestScore {
				continue
			}
			bestSad = sad
			bestScore = score
			*bestSite = sites[i]
		}
		return true
	}
	checkCandidateList := func(s int, count int, bestSite *int) {
		i := 0
		for i+4 <= count {
			var sites, rows, cols [4]int
			for j := 0; j < 4; j++ {
				site := i + j
				c := candidates[s][site]
				sites[j] = site
				rows[j] = br + c.row
				cols[j] = bc + c.col
			}
			if checkBetter4(s, sites, rows, cols, bestSite) {
				i += 4
				continue
			}
			for j := 0; j < 4; j++ {
				checkBetter(s, sites[j], rows[j], cols[j], bestSite)
			}
			i += 4
		}
		for ; i < count; i++ {
			c := candidates[s][i]
			checkBetter(s, i, br+c.row, bc+c.col, bestSite)
		}
	}

	k := -1
	for s := bestInitS; s >= 0; s-- {
		bestSite := -1
		numCandidates := candidateCounts[s]
		checkCandidateList(s, numCandidates, &bestSite)
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

func patternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, doInitSearch bool,
	limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
	candidateCounts *[MaxMvSearchSteps]int,
	candidates *[MaxMvSearchSteps][8]fullpelPatternCandidate,
) (int, int, uint64, uint64) {
	searchParam := max(min(stepParam, MaxMvSearchSteps-1), 0)
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

	k := -1
	checkBetter := func(s, site, row, col int, bestSite *int) {
		if row == br && col == bc {
			return
		}
		if !limits.FullpelBoundsOK(br, bc, 1<<s) &&
			!limits.InFullpelRange(row, col) {
			return
		}
		sad, ok := sadAt(col, row)
		if !ok || sad >= bestScore {
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

	if doInitSearch {
		s := bestInitS
		bestInitS = -1
		for t := 0; t <= s; t++ {
			bestSite := -1
			numCandidates := candidateCounts[t]
			for i := range numCandidates {
				c := candidates[t][i]
				checkBetter(t, i, br+c.row, bc+c.col, &bestSite)
			}
			if bestSite != -1 {
				bestInitS = t
				k = bestSite
			}
		}
		if bestInitS != -1 {
			c := candidates[bestInitS][k]
			br += c.row
			bc += c.col
		}
	}

	if bestInitS == -1 {
		return bc, br, bestSad, bestScore
	}

	bestSite := -1
	for s := bestInitS; s >= 0; s-- {
		numCandidates := candidateCounts[s]
		if !doInitSearch || s != bestInitS {
			bestSite = -1
			for i := range numCandidates {
				c := candidates[s][i]
				checkBetter(s, i, br+c.row, bc+c.col, &bestSite)
			}
			if bestSite == -1 {
				continue
			}
			c := candidates[s][bestSite]
			br += c.row
			bc += c.col
			k = bestSite
		}

		for {
			next := [3]int{k - 1, k, k + 1}
			if next[0] < 0 {
				next[0] = numCandidates - 1
			}
			if next[2] == numCandidates {
				next[2] = 0
			}
			bestSite = -1
			for i, site := range next {
				c := candidates[s][site]
				checkBetter(s, i, br+c.row, bc+c.col, &bestSite)
			}
			if bestSite == -1 {
				break
			}
			k = next[bestSite]
			c := candidates[s][k]
			br += c.row
			bc += c.col
		}
	}

	return bc, br, bestSad, bestScore
}

func NStepDiamondSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	return NStepDiamondSearchSADWithBatch(startDx, startDy, startSad, startScore,
		stepParam, limits, sadAt, nil, scoreMv)
}

func NStepDiamondSearchSADWithBatch(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool), sadAt4 PatternSAD4Func,
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
			limits, sadAt, sadAt4, scoreMv)
		if candScore < bestScore {
			bestDx = candDx
			bestDy = candDy
			bestSad = candSad
			bestScore = candScore
		}
	}

	for range 8 {
		bestSite, candSad, candScore := checkNStepCandidates(bestDx, bestDy,
			bestScore, limits, nStepRefineCandidates[:], sadAt, sadAt4, scoreMv)
		if bestSite == -1 {
			break
		}
		bestSad = candSad
		bestScore = candScore
		c := nStepRefineCandidates[bestSite]
		bestDy += c.row
		bestDx += c.col
	}

	return bestDx, bestDy, bestSad, bestScore
}

func nStepDiamondOnceSAD(startDx, startDy int,
	startSad, startScore uint64, searchParam int, limits *MvLimits,
	sadAt func(dx, dy int) (uint64, bool), sadAt4 PatternSAD4Func,
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	br, bc := startDy, startDx
	bestSad := startSad
	bestScore := startScore
	firstStep := 1 << (MaxMvSearchSteps - 1 - searchParam)
	for step := firstStep; step >= 1; step >>= 1 {
		var stepped [len(nStepDiamondCandidates)]fullpelPatternCandidate
		for i, c := range nStepDiamondCandidates {
			stepped[i] = fullpelPatternCandidate{
				row: c.row * step,
				col: c.col * step,
			}
		}
		bestSite, candSad, candScore := checkNStepCandidates(bc, br, bestScore,
			limits, stepped[:], sadAt, sadAt4, scoreMv)
		if bestSite != -1 {
			bestSad = candSad
			bestScore = candScore
			c := nStepDiamondCandidates[bestSite]
			br += c.row * step
			bc += c.col * step
		}
	}
	return bc, br, bestSad, bestScore
}

func checkNStepCandidates(centerDx, centerDy int, initialBestScore uint64,
	limits *MvLimits, candidates []fullpelPatternCandidate,
	sadAt func(dx, dy int) (uint64, bool), sadAt4 PatternSAD4Func,
	scoreMv func(dx, dy int, sad uint64) uint64,
) (bestSite int, bestSad, bestScore uint64) {
	bestSite = -1
	bestScore = initialBestScore
	checkOne := func(site, dx, dy int) {
		if !limits.InFullpelRange(dy, dx) {
			return
		}
		sad, ok := sadAt(dx, dy)
		if !ok || sad >= bestScore {
			return
		}
		score := scoreMv(dx, dy, sad)
		if score < bestScore {
			bestScore = score
			bestSad = sad
			bestSite = site
		}
	}
	i := 0
	for i+4 <= len(candidates) {
		var dxs, dys [4]int
		allIn := sadAt4 != nil
		for j := 0; j < 4; j++ {
			c := candidates[i+j]
			dxs[j] = centerDx + c.col
			dys[j] = centerDy + c.row
			allIn = allIn && limits.InFullpelRange(dys[j], dxs[j])
		}
		if allIn {
			sad0, sad1, sad2, sad3, ok := sadAt4(dxs[0], dys[0], dxs[1],
				dys[1], dxs[2], dys[2], dxs[3], dys[3])
			if ok {
				sads := [4]uint64{sad0, sad1, sad2, sad3}
				for j, sad := range sads {
					if sad >= bestScore {
						continue
					}
					score := scoreMv(dxs[j], dys[j], sad)
					if score < bestScore {
						bestScore = score
						bestSad = sad
						bestSite = i + j
					}
				}
				i += 4
				continue
			}
		}
		for j := 0; j < 4; j++ {
			checkOne(i+j, dxs[j], dys[j])
		}
		i += 4
	}
	for ; i < len(candidates); i++ {
		c := candidates[i]
		checkOne(i, centerDx+c.col, centerDy+c.row)
	}
	return bestSite, bestSad, bestScore
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
