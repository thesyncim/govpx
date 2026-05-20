package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

const (
	vp9MVMax = (1 << (10 + 1 + 2)) - 1
)

type vp9FullpelPatternCandidate struct {
	row int
	col int
}

var vp9MVSADComponentCosts = func() [vp9MVMax + 1]int {
	var costs [vp9MVMax + 1]int
	for i := 1; i <= vp9MVMax; i++ {
		// libvpx: vp9_encoder.c cal_nmvsadcosts uses
		//   (int)(256 * (2 * (log2f(8 * i) + .6))).
		logv := float64(float32(math.Log2(float64(8 * i))))
		costs[i] = int(256 * (2 * (logv + .6)))
	}
	return costs
}()

var vp9BigDiaPatternCandidates = [vp9MaxMvSearchSteps][8]vp9FullpelPatternCandidate{
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

var vp9BigDiaPatternCandidateCounts = [vp9MaxMvSearchSteps]int{
	4, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

func vp9MVSADComponentCost(v int) int {
	if v < 0 {
		v = -v
	}
	if v > vp9MVMax {
		v = vp9MVMax
	}
	return vp9MVSADComponentCosts[v]
}

func (limits *vp9MvLimits) setFullpelSearchRange(ref vp9dec.MV) {
	if limits == nil {
		return
	}
	colMin := (int(ref.Col) >> 3) - vp9MaxFullPelVal
	if int(ref.Col)&7 != 0 {
		colMin++
	}
	rowMin := (int(ref.Row) >> 3) - vp9MaxFullPelVal
	if int(ref.Row)&7 != 0 {
		rowMin++
	}
	colMax := (int(ref.Col) >> 3) + vp9MaxFullPelVal
	rowMax := (int(ref.Row) >> 3) + vp9MaxFullPelVal

	colMin = max(colMin, (vp9MvLow>>3)+1)
	rowMin = max(rowMin, (vp9MvLow>>3)+1)
	colMax = min(colMax, (vp9MvUpp>>3)-1)
	rowMax = min(rowMax, (vp9MvUpp>>3)-1)

	if limits.ColMin < colMin {
		limits.ColMin = colMin
	}
	if limits.ColMax > colMax {
		limits.ColMax = colMax
	}
	if limits.RowMin < rowMin {
		limits.RowMin = rowMin
	}
	if limits.RowMax > rowMax {
		limits.RowMax = rowMax
	}
}

func (limits *vp9MvLimits) inFullpelRange(row, col int) bool {
	if limits == nil {
		return true
	}
	return col >= limits.ColMin && col <= limits.ColMax &&
		row >= limits.RowMin && row <= limits.RowMax
}

func (limits *vp9MvLimits) fullpelBoundsOK(row, col, searchRange int) bool {
	if limits == nil {
		return true
	}
	return row-searchRange >= limits.RowMin &&
		row+searchRange <= limits.RowMax &&
		col-searchRange >= limits.ColMin &&
		col+searchRange <= limits.ColMax
}

func (limits *vp9MvLimits) clampFullpel(row, col int) (int, int) {
	if limits == nil {
		return row, col
	}
	if row < limits.RowMin {
		row = limits.RowMin
	} else if row > limits.RowMax {
		row = limits.RowMax
	}
	if col < limits.ColMin {
		col = limits.ColMin
	} else if col > limits.ColMax {
		col = limits.ColMax
	}
	return row, col
}

func vp9FastDiamondPatternSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *vp9MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	searchParam := max(max(vp9MaxMvSearchSteps-2, stepParam), 0)
	if searchParam >= vp9MaxMvSearchSteps {
		searchParam = vp9MaxMvSearchSteps - 1
	}
	searchParamToSteps := [...]int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	bestInitS := searchParamToSteps[searchParam]

	br, bc := limits.clampFullpel(startDy, startDx)
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
		if !limits.fullpelBoundsOK(br, bc, 1<<s) &&
			!limits.inFullpelRange(row, col) {
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
		numCandidates := vp9BigDiaPatternCandidateCounts[s]
		for i := range numCandidates {
			c := vp9BigDiaPatternCandidates[s][i]
			checkBetter(s, i, br+c.row, bc+c.col, &bestSite)
		}
		if bestSite != -1 {
			c := vp9BigDiaPatternCandidates[s][bestSite]
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
				c := vp9BigDiaPatternCandidates[s][site]
				checkBetter(s, site, br+c.row, bc+c.col, &bestSite)
			}
			if bestSite != -1 {
				k = bestSite
				c := vp9BigDiaPatternCandidates[s][k]
				br += c.row
				bc += c.col
			}
		}
	}
	return bc, br, bestSad, bestScore
}

func vp9NStepDiamondSearchSAD(startDx, startDy int,
	startSad, startScore uint64, stepParam int, limits *vp9MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	searchParam := max(min(stepParam, vp9MaxMvSearchSteps-1), 0)
	br, bc := limits.clampFullpel(startDy, startDx)
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

	furtherSteps := vp9MaxMvSearchSteps - 1 - searchParam
	for n := 0; n <= furtherSteps; n++ {
		candDx, candDy, candSad, candScore := vp9NStepDiamondOnceSAD(
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
		for site, c := range vp9NStepRefineCandidates {
			row := bestDy + c.row
			col := bestDx + c.col
			if !limits.inFullpelRange(row, col) {
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
		c := vp9NStepRefineCandidates[bestSite]
		bestDy += c.row
		bestDx += c.col
	}

	return bestDx, bestDy, bestSad, bestScore
}

func vp9NStepDiamondOnceSAD(startDx, startDy int,
	startSad, startScore uint64, searchParam int, limits *vp9MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	scoreMv func(dx, dy int, sad uint64) uint64,
) (int, int, uint64, uint64) {
	br, bc := startDy, startDx
	bestSad := startSad
	bestScore := startScore
	firstStep := 1 << (vp9MaxMvSearchSteps - 1 - searchParam)
	for step := firstStep; step >= 1; step >>= 1 {
		bestSite := -1
		for i, c := range vp9NStepDiamondCandidates {
			row := br + c.row*step
			col := bc + c.col*step
			if !limits.inFullpelRange(row, col) {
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
			c := vp9NStepDiamondCandidates[bestSite]
			br += c.row * step
			bc += c.col * step
		}
	}
	return bc, br, bestSad, bestScore
}

var vp9NStepDiamondCandidates = [...]vp9FullpelPatternCandidate{
	{-1, 0},
	{1, 0},
	{0, -1},
	{0, 1},
	{-1, -1},
	{-1, 1},
	{1, -1},
	{1, 1},
}

var vp9NStepRefineCandidates = [...]vp9FullpelPatternCandidate{
	{-1, 0},
	{0, -1},
	{0, 1},
	{1, 0},
	{-1, -1},
	{-1, 1},
	{1, -1},
	{1, 1},
}

func (e *VP9Encoder) refineVP9InterSubpelMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, best vp9dec.MV, bestSad, bestScore uint64,
	refMv vp9dec.MV, refMvValid bool, nonrdSubpelTree bool,
) (vp9dec.MV, uint64) {
	// SPEED_FEATURES.mv.subpel_force_stop scales the min step:
	// HALFPEL (sf 4), QUARTERPEL (2), EIGHTHPEL (1 with HP / 2 without).
	// SPEED_FEATURES.mv.subpel_search_method caps the iteration depth.
	//
	// libvpx: vp9_mcomp.c — the tree-pruned variants halve the step until
	// it reaches forcestop and the more pruned methods stop after one or
	// two iterations. vp9InterSubpelMinStep already honors
	// SPEED_FEATURES.mv.subpel_force_stop and returns >4 when the walker
	// is disabled entirely (FULL_PEL).
	allowHP := inter != nil && inter.allowHP
	minStep := e.vp9InterSubpelMinStep(allowHP)
	if minStep > 4 {
		return best, bestScore
	}
	maxIters := e.vp9InterSubpelIters()
	mvCost := func(mv vp9dec.MV) uint64 {
		if !refMvValid {
			return 0
		}
		errorPerBit := e.vp9MVErrorPerBit(e.vp9EncoderModeDecisionQIndex())
		return vp9SubpelMVErrorCost(vp9InterModeCostFrameContext(inter), mv,
			refMv, allowHP, errorPerBit)
	}
	useSubpelTree := nonrdSubpelTree || e.sf.Mv.SubpelSearchMethod == SubpelTree
	if useSubpelTree {
		if variance, ok := e.vp9InterPredictionSubpelVariance(inter, miRow,
			miCol, bsize, refFrame, best); ok {
			bestScore = variance + mvCost(best)
		}
	} else {
		if dist, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, refFrame, best,
			vp9dec.InterpEighttap); ok {
			bestScore = dist + mvCost(best)
		} else {
			bestScore = bestSad + mvCost(best)
		}
	}
	if !useSubpelTree {
		bestScore = bestSad + mvCost(best)
		iters := 0
		for step := int16(4); step >= minStep; step >>= 1 {
			if iters >= maxIters {
				break
			}
			improved := true
			for improved {
				if iters >= maxIters {
					break
				}
				improved = false
				center := best
				for row := center.Row - step; row <= center.Row+step; row += step {
					for col := center.Col - step; col <= center.Col+step; col += step {
						cand := vp9dec.MV{Row: row, Col: col}
						vp9dec.ClampMvRef(&cand, miRows, miCols, miRow, miCol, bsize)
						vp9dec.LowerMvPrecision(&cand, allowHP)
						if cand == best {
							continue
						}
						sad, ok := e.vp9InterPredictionSAD(inter, miRows, miCols,
							miRow, miCol, bsize, common.NewMv, refFrame, cand,
							vp9dec.InterpEighttap, ^uint64(0))
						if !ok {
							continue
						}
						score := sad + mvCost(cand)
						if score >= bestScore {
							continue
						}
						best = cand
						bestScore = score
						bestSad = sad
						improved = true
					}
				}
				iters++
			}
		}
		return best, bestScore
	}
	return e.refineVP9InterSubpelMvTree(inter, miRows, miCols, miRow, miCol,
		bsize, refFrame, best, bestScore, refMv, allowHP, mvCost)
}

func (e *VP9Encoder) refineVP9InterSubpelMvTree(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, best vp9dec.MV, bestScore uint64, refMv vp9dec.MV, allowHP bool,
	mvCost func(vp9dec.MV) uint64,
) (vp9dec.MV, uint64) {
	// Verbatim shape of libvpx vp9_find_best_sub_pixel_tree:
	// vp9_mcomp.c:721-925. MVs are already in 1/8-pel units here.
	umvLimits := vp9EncoderMvLimits(miRows, miCols, miRow, miCol, bsize)
	var subpelLimits vp9MvLimits
	vp9SetSubpelMvSearchRange(&subpelLimits, &umvLimits, &refMv)

	round := 3 - int(e.sf.Mv.SubpelForceStop)
	if !(allowHP && vp9UseMvHP(refMv)) && round == 3 {
		round = 2
	}
	if round <= 0 {
		return best, bestScore
	}

	scoreAt := func(row, col int) (uint64, bool) {
		if col < subpelLimits.ColMin || col > subpelLimits.ColMax ||
			row < subpelLimits.RowMin || row > subpelLimits.RowMax {
			return 0, false
		}
		cand := vp9dec.MV{Row: int16(row), Col: int16(col)}
		dist, ok := e.vp9InterPredictionSubpelVariance(inter, miRow, miCol,
			bsize, refFrame, cand)
		if !ok {
			return 0, false
		}
		mvRate := mvCost(cand)
		score := dist + mvRate
		return score, true
	}
	checkBetter := func(row, col int) bool {
		score, ok := scoreAt(row, col)
		if !ok || score >= bestScore {
			return false
		}
		bestScore = score
		best.Row = int16(row)
		best.Col = int16(col)
		return true
	}

	br := int(best.Row)
	bc := int(best.Col)
	searchSteps := [...]struct {
		row int
		col int
	}{
		{0, -4}, {0, 4}, {-4, 0}, {4, 0},
		{0, -2}, {0, 2}, {-2, 0}, {2, 0},
		{0, -1}, {0, 1}, {-1, 0}, {1, 0},
	}
	hstep := 4
	for iter := 0; iter < round; iter++ {
		base := iter * 4
		bestIdx := -1
		costArray := [5]uint64{
			math.MaxUint64, math.MaxUint64, math.MaxUint64,
			math.MaxUint64, math.MaxUint64,
		}
		tr, tc := br, bc
		for idx := range 4 {
			tr = br + searchSteps[base+idx].row
			tc = bc + searchSteps[base+idx].col
			if score, ok := scoreAt(tr, tc); ok {
				costArray[idx] = score
				if score < bestScore {
					bestIdx = idx
					bestScore = score
				}
			}
		}

		kc := -hstep
		if costArray[1] < costArray[0] {
			kc = hstep
		}
		kr := -hstep
		if costArray[3] < costArray[2] {
			kr = hstep
		}
		tc = bc + kc
		tr = br + kr
		if score, ok := scoreAt(tr, tc); ok {
			costArray[4] = score
			if score < bestScore {
				bestIdx = 4
				bestScore = score
			}
		}

		switch {
		case bestIdx >= 0 && bestIdx < 4:
			br += searchSteps[base+bestIdx].row
			bc += searchSteps[base+bestIdx].col
		case bestIdx == 4:
			br = tr
			bc = tc
		}
		if bestIdx != -1 {
			best.Row = int16(br)
			best.Col = int16(bc)
		}

		if e.sf.Mv.SubpelSearchLevel > 0 && bestIdx != -1 {
			br0, bc0 := br, bc
			if tr == br && tc != bc {
				kc = bc - tc
				if e.sf.Mv.SubpelSearchLevel == 1 && checkBetter(br0, bc0+kc) {
					br, bc = int(best.Row), int(best.Col)
				}
			} else if tr != br && tc == bc {
				kr = br - tr
				if e.sf.Mv.SubpelSearchLevel == 1 && checkBetter(br0+kr, bc0) {
					br, bc = int(best.Row), int(best.Col)
				}
			}
			if e.sf.Mv.SubpelSearchLevel > 1 {
				if checkBetter(br0+kr, bc0) {
					br, bc = int(best.Row), int(best.Col)
				}
				if checkBetter(br0, bc0+kc) {
					br, bc = int(best.Row), int(best.Col)
				}
				if br0 != br || bc0 != bc {
					if checkBetter(br0+kr, bc0+kc) {
						br, bc = int(best.Row), int(best.Col)
					}
				}
			}
		}

		hstep >>= 1
	}
	return best, bestScore
}

func vp9EncoderMvLimits(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) vp9MvLimits {
	miW := int(common.Num8x8BlocksWideLookup[bsize])
	miH := int(common.Num8x8BlocksHighLookup[bsize])
	return vp9MvLimits{
		RowMin: -(((miRow + miH) * common.MiSize) + common.VP9InterpExtend),
		ColMin: -(((miCol + miW) * common.MiSize) + common.VP9InterpExtend),
		RowMax: (miRows-miRow)*common.MiSize + common.VP9InterpExtend,
		ColMax: (miCols-miCol)*common.MiSize + common.VP9InterpExtend,
	}
}

func vp9UseMvHP(ref vp9dec.MV) bool {
	const kMvRefThresh = 64
	row := int(ref.Row)
	if row < 0 {
		row = -row
	}
	col := int(ref.Col)
	if col < 0 {
		col = -col
	}
	return row < kMvRefThresh && col < kMvRefThresh
}

func (e *VP9Encoder) vp9MVErrorPerBit(qindex int) int {
	rdmult := e.activeRDMult(qindex)
	errorPerBit := rdmult >> 6
	if errorPerBit <= 0 {
		errorPerBit = 1
	}
	return errorPerBit
}

func vp9SubpelMVErrorCost(fc *vp9dec.FrameContext, mv, ref vp9dec.MV,
	allowHP bool, errorPerBit int,
) uint64 {
	if fc == nil || errorPerBit <= 0 {
		return 0
	}
	cost := encoder.MvCostWithHP(mv, ref, &fc.Nmvc, allowHP)
	return uint64((int64(cost)*int64(errorPerBit) + (1 << 13)) >> 14)
}

func (e *VP9Encoder) vp9InterPredictionSAD(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter, limit uint64,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, ok := encoder.VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !ok {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	// Motion-search SAD only consults luma; skip chroma reconstruction
	// to cut ~30% of convolve8 work per candidate. libvpx mirrors this
	// in nonrd_pickmode via vp9_build_inter_predictors_sby.
	// libvpx: vp9/encoder/vp9_pickmode.c:2336.
	if !e.predictVP9InterBlockLumaOnly(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return encoder.BlockSAD(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH, limit), true
}

// vp9NonrdUVVarianceSSE rebuilds the UV inter prediction (assuming the Y
// predictor has already been committed via vp9InterPredictionVarianceSSE)
// and returns (var_u, sse_u, var_v, sse_v). The realtime nonrd picker
// consumes these to drive encode_breakout_test's UV-plane skip check
// (vp9_pickmode.c:1014-1025).
//
// libvpx counterpart: vp9_pickmode.c:1009-1022 — xd->plane[1|2].pre[0] is
// pointed at the reference U/V buffer, vp9_build_inter_predictors_sbuv
// runs the chroma predictor, then cpi->fn_ptr[uv_bsize].vf returns
// (var_u, sse_u) / (var_v, sse_v).
func (e *VP9Encoder) vp9NonrdUVVarianceSSE(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (varU, sseU, varV, sseV uint64, ok bool) {
	if inter == nil || inter.img == nil {
		return 0, 0, 0, 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, 0, 0, 0, false
	}
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return 0, 0, 0, 0, false
		}
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		dst, dstStride := e.vp9EncoderReconPlane(plane)
		if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
			return 0, 0, 0, 0, false
		}
		blockW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		dstRows := len(dst) / dstStride
		if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
			!vp9VisibleBlockFits(x0, y0, blockW, blockH, dstStride, dstRows) {
			return 0, 0, 0, 0, false
		}
		variance, sse := encoder.BlockDiffVarianceSSE(src, srcStride, dst, dstStride,
			x0, y0, x0, y0, blockW, blockH)
		if plane == 1 {
			varU = variance
			sseU = sse
		} else {
			varV = variance
			sseV = sse
		}
	}
	return varU, sseU, varV, sseV, true
}

// vp9InterPredictionVarianceSSE runs the inter predictor for one
// (mode, ref, mv, filter) candidate and returns both the variance and the
// SSE between the source and the prediction. Mirrors libvpx's
// fn_ptr[bsize].vf call inside model_rd_for_sb_y (vp9_pickmode.c:661-666)
// which produces (var, sse). The realtime nonrd picker consumes both.
func (e *VP9Encoder) vp9InterPredictionVarianceSSE(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (variance, sse uint64, ok bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, vok := encoder.VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !vok {
		return 0, 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, 0, false
	}
	variance, sse = encoder.BlockDiffVarianceSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH)
	return variance, sse, true
}

func (e *VP9Encoder) vp9SubpelReferencePlane(refFrame int8,
	ref *vp9ReferenceFrame,
) (pixels []uint8, stride, originX, originY, width, height int, ok bool) {
	plane, planeStride, w, h := vp9ReferenceVisiblePlane(ref, 0)
	if len(plane) == 0 || planeStride <= 0 || w <= 0 || h <= 0 {
		return nil, 0, 0, 0, 0, 0, false
	}
	slot, slotOK := vp9EncoderReferenceSlot(refFrame)
	if slotOK && slot == vp9LastRefSlot {
		if !e.lastBorderedValid || e.lastBordered.W != w ||
			e.lastBordered.H != h {
			e.ensureLastBordered()
		}
		if e.lastBorderedValid && e.lastBordered.W == w &&
			e.lastBordered.H == h {
			return e.lastBordered.Pixels, e.lastBordered.Stride,
				e.lastBordered.OriginX(), e.lastBordered.OriginY(),
				w, h, true
		}
	}
	if !slotOK {
		return nil, 0, 0, 0, 0, 0, false
	}
	if !e.subpelRefBorderedValid || e.subpelRefBorderedSlot != slot ||
		e.subpelRefBordered.W != w || e.subpelRefBordered.H != h {
		common.YV12BuildBorderedPlane(&e.subpelRefBordered, plane,
			planeStride, w, h, common.VP9EncBorderInPixels)
		e.subpelRefBorderedSlot = slot
		e.subpelRefBorderedValid = true
	}
	return e.subpelRefBordered.Pixels, e.subpelRefBordered.Stride,
		e.subpelRefBordered.OriginX(), e.subpelRefBordered.OriginY(),
		w, h, true
}

func (e *VP9Encoder) vp9InterPredictionSubpelVariance(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, refFrame int8, mv vp9dec.MV,
) (uint64, bool) {
	if inter == nil || inter.img == nil || inter.ref == nil || !inter.ref.valid {
		return 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pre, preStride, preOriginX, preOriginY, preW, preH, ok :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(src) == 0 || len(pre) == 0 || srcStride <= 0 || preStride <= 0 {
		return 0, false
	}
	if !ok {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) {
		return 0, false
	}
	preX := x0 + (int(mv.Col) >> 3)
	preY := y0 + (int(mv.Row) >> 3)
	bufX := preOriginX + preX
	bufY := preOriginY + preY
	if bufX < 0 || bufY < 0 || bufX+blockW+1 > preStride ||
		bufY+blockH+1 > len(pre)/preStride ||
		preX < -preOriginX || preY < -preOriginY ||
		preX+blockW+1 > preW+preOriginX ||
		preY+blockH+1 > preH+preOriginY {
		return 0, false
	}
	srcOff := y0*srcStride + x0
	preOff := bufY*preStride + bufX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	var sse uint32
	var variance uint32
	switch bsize {
	case common.Block64x64:
		variance = vp9dsp.VpxSubPixelVariance64x64(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block64x32:
		variance = vp9dsp.VpxSubPixelVariance64x32(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block32x64:
		variance = vp9dsp.VpxSubPixelVariance32x64(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block32x32:
		variance = vp9dsp.VpxSubPixelVariance32x32(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block32x16:
		variance = vp9dsp.VpxSubPixelVariance32x16(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block16x32:
		variance = vp9dsp.VpxSubPixelVariance16x32(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block16x16:
		variance = vp9dsp.VpxSubPixelVariance16x16(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block16x8:
		variance = vp9dsp.VpxSubPixelVariance16x8(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block8x16:
		variance = vp9dsp.VpxSubPixelVariance8x16(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block8x8:
		variance = vp9dsp.VpxSubPixelVariance8x8(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block8x4:
		variance = vp9dsp.VpxSubPixelVariance8x4(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block4x8:
		variance = vp9dsp.VpxSubPixelVariance4x8(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	case common.Block4x4:
		variance = vp9dsp.VpxSubPixelVariance4x4(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse)
	default:
		return 0, false
	}
	return uint64(variance), true
}
