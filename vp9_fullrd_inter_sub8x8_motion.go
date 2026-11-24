package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_sub8x8_motion.go ports the per-sub-block single-reference
// NEWMV motion search inside rd_pick_best_sub8x8_mode (vp9_rdopt.c:2168-2261):
// full_pixel_diamond (vp9_full_pixel_search, NSTEP) over the EIGHTTAP-predicted
// reference at the sub-block offset, then find_fractional_mv_step (SUBPEL_TREE)
// subpel refinement. The realtime full-RD path uses step_param =
// cpi->mv_step_param == 0 (auto_mv_step_size == 0 for RT,
// vp9_speed_features.c:938), sadpb = x->sadperbit4, and bsize = the sub-8x8
// partition (BLOCK_4X4/4X8/8X4) as the fn_ptr index.
//
// The mvp_full seed (bsi->mvp) is best_ref_mv for block 0; for block>0 it is the
// previous block's MV (block 2 uses block 0's MV) — vp9_rdopt.c:2185-2196.

// vp9Sub8x8NewMvSearch runs the NEWMV full-pixel + subpel motion search for one
// sub-block label and returns the 1/8-pel MV. Mirrors vp9_rdopt.c:2169-2261.
func (e *VP9Encoder) vp9Sub8x8NewMvSearch(inter *vp9InterEncodeState,
	in vp9Sub8x8Input, bsize common.BlockSize, block int, refFrame int8,
	mi *vp9dec.NeighborMi, bestRefMv vp9dec.MV,
) (vp9dec.MV, bool) {
	// bsi->mvp: block 0 → best_ref_mv; block 1 → bmi[0]; block 2 → bmi[0];
	// block 3 → bmi[2] (vp9_rdopt.c:2185-2189).
	mvp := bestRefMv
	if block > 0 {
		mvp = mi.Bmi[block-1].AsMv[0]
		if block == 2 {
			mvp = mi.Bmi[block-2].AsMv[0]
		}
	}

	// Sub-block footprint + pixel position inside the 8x8.
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := in.miCol*common.MiSize + 4*(block&1)
	y0 := in.miRow*common.MiSize + 4*(block>>1)

	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pre, preStride, preOriginX, preOriginY, preW, preH, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(src) == 0 || srcStride <= 0 || !refOK || len(pre) == 0 || preStride <= 0 {
		return vp9dec.MV{}, false
	}
	if x0+blockW > srcW || y0+blockH > srcH {
		return vp9dec.MV{}, false
	}
	srcOff := y0*srcStride + x0
	preRows := len(pre) / preStride

	// mv_limits: set_offsets(BLOCK_4X4) box, then vp9_set_mv_search_range from
	// best_ref_mv (vp9_rdopt.c:2223).
	mvLimits := encoder.EncoderMvLimits(in.miRows, in.miCols, in.miRow, in.miCol,
		bsize)
	mvLimits.SetFullpelSearchRange(bestRefMv)

	// mvp_full = bsi->mvp >> 3, clamped to mv_limits (vp9_rdopt.c:2208-2209 +
	// the diamond's internal clamp_mv).
	mvpRow := int(mvp.Row) >> 3
	mvpCol := int(mvp.Col) >> 3
	mvpRow, mvpCol = mvLimits.ClampFullpel(mvpRow, mvpCol)

	sadpb := encoder.SADPerBit4(e.vp9EncoderModeDecisionQIndex())
	refRow := int(bestRefMv.Row)
	refCol := int(bestRefMv.Col)

	// Full-pel SAD over the sub-block footprint at offset (dx, dy).
	sadAt := func(row, col int) (uint64, bool) {
		if !mvLimits.InFullpelRange(row, col) {
			return 0, false
		}
		bufX := preOriginX + x0 + col
		bufY := preOriginY + y0 + row
		if bufX < 0 || bufY < 0 || bufX+blockW > preStride || bufY+blockH > preRows {
			return 0, false
		}
		preOff := bufY*preStride + bufX
		return encoder.BlockSADOffsets(src, srcOff, srcStride, pre, preOff,
			preStride, blockW, blockH, ^uint64(0)), true
	}

	allowHP := inter.allowHP
	errorPerBit := e.vp9MVErrorPerBit(e.vp9EncoderModeDecisionQIndex())
	fc := &inter.selectFc
	// vp9_get_mvpred_var: vf(src, pred@mv) + mv_err_cost(mv*8, ref_mv) at full
	// pel (vp9_mcomp.c:1454).
	varAt := func(row, col int) uint64 {
		intMv := vp9dec.MV{Row: int16(row * 8), Col: int16(col * 8)}
		variance, ok := e.vp9Sub8x8SubpelVariance(inter, x0, y0, bsize, refFrame,
			intMv, src, srcStride, srcOff, pre, preStride, preOriginX, preOriginY,
			preW, preH, preRows)
		if !ok {
			return math.MaxUint64 >> 1
		}
		cost := encoder.SubpelMVErrorCost(fc, intMv, bestRefMv, allowHP,
			errorPerBit)
		return variance + cost
	}

	// start_mv_sad = sdf(mvp_full) + mvsad_err_cost(mvp_full, ref_mv>>3)
	// (full_pixel_diamond, vp9_mcomp.c:2509-2515).
	startSad, sok := sadAt(mvpRow, mvpCol)
	if !sok {
		return vp9dec.MV{}, false
	}
	startMvSad := startSad + uint64(encoder.FullPelMVSADCost(mvpRow, mvpCol,
		refRow>>3, refCol>>3, sadpb))

	const stepParam = 0
	furtherSteps := encoder.MaxMvSearchSteps - 1 - stepParam
	res := encoder.FullPixelDiamond(mvpRow, mvpCol, startMvSad, stepParam, sadpb,
		furtherSteps, true, refRow, refCol, &mvLimits, sadAt, varAt)

	bestRow, bestCol := res.BestRow, res.BestCol
	bestSad, _ := sadAt(bestRow, bestCol)
	mv := vp9dec.MV{Row: int16(bestRow * 8), Col: int16(bestCol * 8)}

	// find_fractional_mv_step (SUBPEL_TREE) refinement (vp9_rdopt.c:2252-2258).
	mv = e.vp9Sub8x8SubpelRefine(inter, in, x0, y0, bsize, refFrame, mv, bestSad,
		bestRefMv, src, srcStride, srcOff, pre, preStride, preOriginX, preOriginY,
		preW, preH, preRows)
	return mv, true
}

// vp9Sub8x8SubpelVariance returns the subpel variance of the sub-block at pixel
// (x0, y0) for MV mv (1/8-pel) against the bordered reference, mirroring the
// vfp->svf used by find_fractional_mv_step / vp9_get_mvpred_var.
func (e *VP9Encoder) vp9Sub8x8SubpelVariance(inter *vp9InterEncodeState,
	x0, y0 int, bsize common.BlockSize, refFrame int8, mv vp9dec.MV,
	src []byte, srcStride, srcOff int, pre []byte, preStride, preOriginX,
	preOriginY, preW, preH, preRows int,
) (uint64, bool) {
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	preX := x0 + (int(mv.Col) >> 3)
	preY := y0 + (int(mv.Row) >> 3)
	bufX := preOriginX + preX
	bufY := preOriginY + preY
	if bufX < 0 || bufY < 0 || bufX+blockW+1 > preStride ||
		bufY+blockH+1 > preRows ||
		preX < -preOriginX || preY < -preOriginY ||
		preX+blockW+1 > preW+preOriginX || preY+blockH+1 > preH+preOriginY {
		return 0, false
	}
	preOff := bufY*preStride + bufX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	var sse32 uint32
	var variance32 uint32
	switch bsize {
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
		return 0, false
	}
	_ = variance32
	return uint64(variance32), true
}

// vp9Sub8x8SubpelRefine ports find_fractional_mv_step's SUBPEL_TREE walk
// (vp9_mcomp.c:721-925, vp9_find_best_sub_pixel_tree) for one sub-block. MVs are
// in 1/8-pel. subpel_force_stop == EIGHTH_PEL, subpel_search_level == 2.
func (e *VP9Encoder) vp9Sub8x8SubpelRefine(inter *vp9InterEncodeState,
	in vp9Sub8x8Input, x0, y0 int, bsize common.BlockSize, refFrame int8,
	best vp9dec.MV, bestSad uint64, bestRefMv vp9dec.MV, src []byte,
	srcStride, srcOff int, pre []byte, preStride, preOriginX, preOriginY,
	preW, preH, preRows int,
) vp9dec.MV {
	allowHP := inter.allowHP
	errorPerBit := e.vp9MVErrorPerBit(e.vp9EncoderModeDecisionQIndex())
	fc := &inter.selectFc
	mvCost := func(mv vp9dec.MV) uint64 {
		return encoder.SubpelMVErrorCost(fc, mv, bestRefMv, allowHP, errorPerBit)
	}

	// set_subpel_mv_search_range from the block's mv_limits box (the BLOCK_4X4
	// set_offsets box, vp9_rdopt.c via vp9_mcomp.c:51-67).
	umvLimits := encoder.EncoderMvLimits(in.miRows, in.miCols, in.miRow,
		in.miCol, bsize)
	var subpelLimits encoder.MvLimits
	encoder.SetSubpelMvSearchRange(&subpelLimits, &umvLimits, &bestRefMv)

	// round = 3 - subpel_force_stop; EIGHTH_PEL → 3. Halved when HP unusable.
	round := 3 - int(e.sf.Mv.SubpelForceStop)
	if !(allowHP && encoder.UseMvHP(bestRefMv)) && round == 3 {
		round = 2
	}
	if round <= 0 {
		return best
	}

	bestScore := uint64(math.MaxUint64)
	if v, ok := e.vp9Sub8x8SubpelVariance(inter, x0, y0, bsize, refFrame, best,
		src, srcStride, srcOff, pre, preStride, preOriginX, preOriginY, preW,
		preH, preRows); ok {
		bestScore = v + mvCost(best)
	}

	scoreAt := func(row, col int) (uint64, bool) {
		if col < subpelLimits.ColMin || col > subpelLimits.ColMax ||
			row < subpelLimits.RowMin || row > subpelLimits.RowMax {
			return 0, false
		}
		cand := vp9dec.MV{Row: int16(row), Col: int16(col)}
		v, ok := e.vp9Sub8x8SubpelVariance(inter, x0, y0, bsize, refFrame, cand,
			src, srcStride, srcOff, pre, preStride, preOriginX, preOriginY, preW,
			preH, preRows)
		if !ok {
			return 0, false
		}
		return v + mvCost(cand), true
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
	searchSteps := [...]struct{ row, col int }{
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
	return best
}
