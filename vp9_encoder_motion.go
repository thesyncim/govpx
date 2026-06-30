package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

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
	// libvpx costs subpel MVs with x->nmvcost (vp9_mcomp.c mv_err_cost ->
	// mv_cost). That table is vpx_calloc'd to zero and only (re)built by
	// vp9_build_nmv_cost_table on non-intra frames satisfying the
	// fill_mode_costs gate (vp9_rd.c:439-443). For the full-RD path
	// (!use_nonrd_pick_mode) that gate fires on every non-intra frame so the
	// table is always populated; only the nonrd path can leave it at the
	// vpx_calloc'd zero state, the state reached on the first inter frame after
	// two adjacent keyframes where neither keyframe builds the table and the
	// first inter frame (current_video_frame&7 != 1) does not either. In that
	// nonrd-unbuilt case the MV-entropy cost is exactly zero; the full-RD path
	// keeps the live cost FrameContext so its subpel scoring is unchanged.
	mvCostFc, mvCostBuilt := vp9InterMvCostFrameContext(inter)
	mvCost := func(mv vp9dec.MV) uint64 {
		if !refMvValid {
			return 0
		}
		errorPerBit := e.vp9MVErrorPerBit(e.vp9EncoderModeDecisionQIndex())
		if nonrdSubpelTree {
			if !mvCostBuilt {
				return 0
			}
			return encoder.SubpelMVErrorCost(mvCostFc, mv, refMv, allowHP,
				errorPerBit)
		}
		return encoder.SubpelMVErrorCost(vp9InterModeCostFrameContext(inter), mv,
			refMv, allowHP, errorPerBit)
	}
	useSubpelTree := nonrdSubpelTree || e.vp9InterSubpelSearchUsesTree()
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
	umvLimits := encoder.EncoderMvLimits(miRows, miCols, miRow, miCol, bsize)
	var subpelLimits encoder.MvLimits
	encoder.SetSubpelMvSearchRange(&subpelLimits, &umvLimits, &refMv)

	round := 3 - int(e.sf.Mv.SubpelForceStop)
	if !(allowHP && encoder.UseMvHP(refMv)) && round == 3 {
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

func (e *VP9Encoder) vp9MVErrorPerBit(qindex int) int {
	rdmult := e.activeRDMult(qindex)
	errorPerBit := rdmult >> 6
	if errorPerBit <= 0 {
		errorPerBit = 1
	}
	return errorPerBit
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
		if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
			!encoder.VisibleBlockFits(x0, y0, blockW, blockH, dstStride, dstRows) {
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
//
// libvpx model_rd_for_sb_y always scores from vp9_build_inter_predictors_sby
// (search_filter_ref uses the same builder). Motion search keeps the bordered
// subpel variance substrate via vp9InterPredictionSubpelVariance.
func (e *VP9Encoder) vp9InterPredictionVarianceSSE(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (variance, sse uint64, ok bool) {
	return e.vp9InterPredictionVarianceSSEOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mode, refFrame, mv, filter, false)
}

// vp9InterPredictionVarianceSSEForFilterSearch mirrors libvpx
// search_filter_ref's vp9_build_inter_predictors_sby + vf path.
func (e *VP9Encoder) vp9InterPredictionVarianceSSEForFilterSearch(
	inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (variance, sse uint64, ok bool) {
	return e.vp9InterPredictionVarianceSSEOpts(inter, miRows, miCols, miRow, miCol,
		bsize, mode, refFrame, mv, filter, true)
}

func (e *VP9Encoder) vp9InterPredictionVarianceSSEOpts(
	inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter, _ bool,
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
	if !e.predictVP9InterBlockLumaOnly(inter, miRows, miCols, miRow, miCol,
		bsize, &mi) {
		return 0, 0, false
	}
	return encoder.BlockDiffVarianceSSEClampedSource(src, srcStride, srcW, srcH,
		dst, dstStride, x0, y0, x0, y0, blockW, blockH)
}

func (e *VP9Encoder) vp9InterPredictionBorderedConvolveVarianceSSE(
	inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
	refFrame int8, mv vp9dec.MV, filter vp9dec.InterpFilter,
	src []byte, srcStride int, dst []byte, dstStride int,
	x0, y0, scoreW, scoreH int,
) (variance, sse uint64, ok bool) {
	filterIdx := int(filter)
	if filterIdx < 0 || filterIdx >= int(vp9dec.InterpSwitchable) {
		return 0, 0, false
	}
	pre, preStride, preOriginX, preOriginY, preW, preH, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(pre) == 0 || preStride <= 0 || !refOK {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	preX := x0 + (int(mv.Col) >> 3)
	preY := y0 + (int(mv.Row) >> 3)
	bufX := preOriginX + preX
	bufY := preOriginY + preY
	if bufX < 0 || bufY < 0 || bufX+blockW+1 > preStride ||
		bufY+blockH+1 > len(pre)/preStride ||
		preX < -preOriginX || preY < -preOriginY ||
		preX+blockW+1 > preW+preOriginX ||
		preY+blockH+1 > preH+preOriginY {
		return 0, 0, false
	}
	if x0+scoreW > dstStride || y0+scoreH > len(dst)/dstStride {
		return 0, 0, false
	}
	preOff := bufY*preStride + bufX
	subpelX := int(mv.Col) & 7
	subpelY := int(mv.Row) & 7
	dstOff := y0*dstStride + x0
	vp9dec.InterPredictor(pre, preStride, dst[dstOff:], dstStride,
		subpelX, subpelY, tables.FilterKernels[filterIdx],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, scoreW, scoreH, 0, preOff)
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
	slot, slotOK := e.vp9ReferenceSlotForFrame(refFrame)
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
	if !e.subpelRefBorderedValid[slot] ||
		e.subpelRefBordered[slot].W != w ||
		e.subpelRefBordered[slot].H != h {
		common.YV12BuildBorderedPlane(&e.subpelRefBordered[slot], plane,
			planeStride, w, h, common.VP9EncBorderInPixels)
		e.subpelRefBorderedValid[slot] = true
	}
	return e.subpelRefBordered[slot].Pixels, e.subpelRefBordered[slot].Stride,
		e.subpelRefBordered[slot].OriginX(), e.subpelRefBordered[slot].OriginY(),
		w, h, true
}

func (e *VP9Encoder) vp9InterPredictionSubpelVariance(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, refFrame int8, mv vp9dec.MV,
) (uint64, bool) {
	variance, _, ok := e.vp9InterPredictionBorderedSubpelVarianceSSE(
		inter, miRow, miCol, bsize, refFrame, mv)
	return variance, ok
}

func (e *VP9Encoder) vp9InterPredictionBorderedSubpelVarianceSSE(
	inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, refFrame int8, mv vp9dec.MV,
) (variance, sse uint64, ok bool) {
	if inter == nil || inter.img == nil || inter.ref == nil || !inter.ref.valid {
		return 0, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pre, preStride, preOriginX, preOriginY, preW, preH, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(src) == 0 || len(pre) == 0 || srcStride <= 0 || preStride <= 0 {
		return 0, 0, false
	}
	if !refOK {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) {
		return 0, 0, false
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
		return 0, 0, false
	}
	srcOff := y0*srcStride + x0
	preOff := bufY*preStride + bufX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset == 0 && yOffset == 0 {
		variance, sse := encoder.BlockDiffVarianceSSE(src, srcStride, pre, preStride,
			x0, y0, bufX, bufY, blockW, blockH)
		return variance, sse, true
	}
	var sse32 uint32
	var variance32 uint32
	switch bsize {
	case common.Block64x64:
		variance32 = vp9dsp.VpxSubPixelVariance64x64(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block64x32:
		variance32 = vp9dsp.VpxSubPixelVariance64x32(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block32x64:
		variance32 = vp9dsp.VpxSubPixelVariance32x64(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block32x32:
		variance32 = vp9dsp.VpxSubPixelVariance32x32(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block32x16:
		variance32 = vp9dsp.VpxSubPixelVariance32x16(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block16x32:
		variance32 = vp9dsp.VpxSubPixelVariance16x32(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block16x16:
		variance32 = vp9dsp.VpxSubPixelVariance16x16(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block16x8:
		variance32 = vp9dsp.VpxSubPixelVariance16x8(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block8x16:
		variance32 = vp9dsp.VpxSubPixelVariance8x16(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block8x8:
		variance32 = vp9dsp.VpxSubPixelVariance8x8(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block8x4:
		variance32 = vp9dsp.VpxSubPixelVariance8x4(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block4x8:
		variance32 = vp9dsp.VpxSubPixelVariance4x8(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	case common.Block4x4:
		variance32 = vp9dsp.VpxSubPixelVariance4x4(pre, preOff, preStride,
			xOffset, yOffset, src, srcOff, srcStride, &sse32)
	default:
		return 0, 0, false
	}
	return uint64(variance32), uint64(sse32), true
}
