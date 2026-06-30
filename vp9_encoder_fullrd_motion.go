package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9FullRDFullPelMv runs the verbatim full-RD full-pixel motion search for a
// single-reference NEWMV candidate: full_pixel_diamond (vp9_mcomp.c:2486) with
// step_param = cpi->mv_step_param == 0 (the no-recode realtime value;
// set_mv_search_params @ vp9_encoder.c:3728 is never called when
// recode_loop == DISALLOW_RECODE), variance-rescoring each diamond candidate
// and the final via vp9_get_mvpred_var (vp9_mcomp.c:1454, variance not SAD).
//
// Returns the best full-pel MV (bestDx, bestDy in full-pel), the SAD at that
// point (for the subpel-refine fallback), and ok.
func (e *VP9Encoder) vp9FullRDFullPelMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, refFrame int8,
	opts vp9InterMvSearchOptions, mvLimits *encoder.MvLimits,
	sadAt func(dx, dy int) (uint64, bool),
	sadAt4 func(dx0, dy0, dx1, dy1, dx2, dy2, dx3, dy3 int) (uint64, uint64, uint64, uint64, bool),
	sadPerBit, refFullDy, refFullDx int,
) (bestDx, bestDy int, bestSad uint64, ok bool) {
	// libvpx: mvp_full = pred_mv[best_predmv_idx]; mvp_full >>= 3;
	// clamp_mv(mvp_full, mv_limits). opts.seed is the mvp_full seed in 1/8-pel.
	mvpRow, mvpCol := 0, 0
	if opts.seedValid {
		mvpRow = int(opts.seed.Row) >> 3
		mvpCol = int(opts.seed.Col) >> 3
	}
	mvpRow, mvpCol = mvLimits.ClampFullpel(mvpRow, mvpCol)

	// libvpx: ref_mv (center_mv) is the NEAREST candidate in 1/8-pel; the
	// diamond does >>3 internally for fcenter_mv.
	refRow := int(opts.refMv.Row)
	refCol := int(opts.refMv.Col)

	// libvpx full_pixel_diamond:2509-2515 — start_mv_sad = sdf(mvp_full) +
	// mvsad_err_cost(mvp_full, ref_mv>>3). govpx scores full pels directly, so
	// the even/odd-row downsampled split (a re-search heuristic that does not
	// change site selection for a deterministic SAD source) is folded into the
	// single full-block SAD.
	startSad, sok := sadAt(mvpCol, mvpRow)
	if !sok {
		return 0, 0, 0, false
	}
	startMvSad := startSad + uint64(encoder.FullPelMVSADCost(mvpRow, mvpCol,
		refRow>>3, refCol>>3, sadPerBit))

	// libvpx: vp9_get_mvpred_var(x, mv, ref_mv, fn_ptr, 1) = vfp->vf(src,
	// pred@mv) + mv_err_cost(mv*8, ref_mv, mvjointcost, mvcost, errorperbit).
	allowHP := inter != nil && inter.allowHP
	errorPerBit := e.vp9MVErrorPerBit(e.vp9EncoderModeDecisionQIndex())
	fc := vp9InterModeCostFrameContext(inter)
	varAt := func(row, col int) uint64 {
		intMv := vp9dec.MV{Row: int16(row * 8), Col: int16(col * 8)}
		variance, _, vok := e.vp9InterPredictionVarianceSSE(inter, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, refFrame, intMv,
			vp9dec.InterpEighttap)
		if !vok {
			// libvpx returns INT_MAX-equivalent for unreadable offsets; use a
			// large value so the diamond never selects it.
			return ^uint64(0) >> 1
		}
		cost := encoder.SubpelMVErrorCost(fc, intMv, opts.refMv, allowHP,
			errorPerBit)
		return variance + cost
	}

	sadAtRC := func(row, col int) (uint64, bool) { return sadAt(col, row) }
	var sadAt4RC encoder.DiamondSAD4Func
	if sadAt4 != nil {
		sadAt4RC = func(row0, col0, row1, col1, row2, col2, row3, col3 int,
		) (uint64, uint64, uint64, uint64, bool) {
			return sadAt4(col0, row0, col1, row1, col2, row2, col3, row3)
		}
	}

	// libvpx single_motion_search step_param (vp9_rdopt.c:2613-2638):
	//   - cpi->mv_step_param == 0 on the no-recode RT path: set_mv_search_params
	//     (vp9_encoder.c:3858) is reached only inside the RESIZE_DYNAMIC branch,
	//     so mv_step_param keeps its 0 init for both cpu0 and cpu4 (verified by
	//     fprintf ground truth).
	//   - auto_mv_step_size (cpu4 RT, off for cpu0) averages it with
	//     init_search_range(max_mv_context[ref]).
	//   - adaptive_motion_search (cpu4 RT, off for cpu0) bumps it to the per-bsize
	//     boffset (== 6 for BLOCK_8X8) and adds the tlevel<5 term.
	// Flag OFF (production + cpu0 {0,2,0,0,2} pins): step_param stays 0 and the
	// NSTEP full_pixel_diamond runs exactly as before.
	stepParam := 0
	searchMethod := e.sf.Mv.SearchMethod
	if vp9InterUseDeepRDUsePartition {
		const mvStepParam = 0 // no-recode RT runtime cpi->mv_step_param
		stepParam = encoder.FullRdSingleMotionStepParam(mvStepParam,
			opts.maxMvContext, e.sf.Mv.AutoMvStepSize != 0,
			e.vp9HeaderScratch.ShowFrame)
		// common.B{Width,Height}Log2Lookup are already in 4x4-block units
		// (BLOCK_8X8 == 1, BLOCK_64X64 == 4), matching libvpx b_*_log2_lookup
		// directly (the "pixels" doc comment is a misnomer).
		bwl := int(common.BWidthLog2Lookup[bsize])
		bhl := int(common.BHeightLog2Lookup[bsize])
		stepParam = encoder.FullRdSingleMotionStepParamAdaptive(stepParam,
			e.sf.AdaptiveMotionSearch != 0, bsize == common.Block64x64,
			int(common.BWidthLog2Lookup[common.Block64x64]), bwl, bhl,
			int(opts.predSad))
	} else {
		// Flag OFF: the production/cpu0 path used the fixed NSTEP diamond
		// regardless of the SF method. Preserve that exactly.
		searchMethod = SearchMethodNStep
	}

	switch searchMethod {
	case SearchMethodFastHex, SearchMethodFastDiamond, SearchMethodHex,
		SearchMethodBigDia, SearchMethodSquare:
		// libvpx vp9_full_pixel_search (vp9_mcomp.c:2898-2913): the pattern
		// searches (FAST_HEX/FAST_DIAMOND/HEX/BIGDIA/SQUARE) run SAD-based hex/
		// diamond patterns over the source vs reference; the final MV is the
		// pattern result (the trailing vp9_get_mvpred_var at :2948 only rescores
		// the returned var, never the MV). startScore is the SAD+mvsad cost at
		// mvp_full so the pattern's running-best compare uses the same baseline.
		startScore := startSad + uint64(encoder.FullPelMVSADCost(mvpRow, mvpCol,
			refRow>>3, refCol>>3, sadPerBit))
		scoreMv := func(dx, dy int, sad uint64) uint64 {
			return sad + uint64(encoder.FullPelMVSADCost(dy, dx,
				refRow>>3, refCol>>3, sadPerBit))
		}
		switch searchMethod {
		case SearchMethodFastHex:
			bestDx, bestDy, _, _ = encoder.FastHexPatternSearchSAD(mvpCol, mvpRow,
				startSad, startScore, stepParam, mvLimits, sadAt, scoreMv)
		case SearchMethodFastDiamond:
			bestDx, bestDy, _, _ = encoder.FastDiamondPatternSearchSAD(mvpCol,
				mvpRow, startSad, startScore, stepParam, mvLimits, sadAt, scoreMv)
		case SearchMethodHex:
			bestDx, bestDy, _, _ = encoder.HexPatternSearchSAD(mvpCol, mvpRow,
				startSad, startScore, stepParam, mvLimits, sadAt, scoreMv)
		case SearchMethodBigDia:
			bestDx, bestDy, _, _ = encoder.BigDiamondPatternSearchSAD(mvpCol,
				mvpRow, startSad, startScore, stepParam, mvLimits, sadAt, scoreMv)
		case SearchMethodSquare:
			bestDx, bestDy, _, _ = encoder.SquarePatternSearchSAD(mvpCol, mvpRow,
				startSad, startScore, stepParam, mvLimits, sadAt, scoreMv)
		}
	default:
		// NSTEP / MESH (cpu0): libvpx vp9_full_pixel_search:2916-2919 —
		// full_pixel_diamond(..., step_param, MAX_MVSEARCH_STEPS-1-step_param,
		// do_refine=1, ...) with the variance-rescoring diamond.
		furtherSteps := encoder.MaxMvSearchSteps - 1 - stepParam
		res := encoder.FullPixelDiamondWithBatch(mvpRow, mvpCol, startMvSad,
			stepParam, sadPerBit, furtherSteps, true, refRow, refCol, mvLimits,
			sadAtRC, sadAt4RC, varAt)
		bestDx, bestDy = res.BestCol, res.BestRow
	}
	if sad, sadOK := sadAt(bestDx, bestDy); sadOK {
		bestSad = sad
	} else {
		bestSad = startSad
	}
	// Pin the full-pel MV (pre-subpel) for the frame-1 SB0 (0,0) parity test.
	// Zero-cost in non-trace builds: recordVP9FullRDFirstInterMv is a no-op
	// stub there (vp9_oracle_trace_flag.go), so this compiles out.
	e.recordVP9FullRDFirstInterMv(e.frameIndex, miRow, miCol, refFrame,
		bestDy, bestDx)
	return bestDx, bestDy, bestSad, true
}
