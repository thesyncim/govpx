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
	sadAt func(dx, dy int) (uint64, bool), sadPerBit, refFullDy, refFullDx int,
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

	// libvpx vp9_full_pixel_search:2916-2919 — full_pixel_diamond(...,
	// step_param=cpi->mv_step_param(0), MAX_MVSEARCH_STEPS-1-step_param,
	// do_refine=1, ...).
	const stepParam = 0
	furtherSteps := encoder.MaxMvSearchSteps - 1 - stepParam
	res := encoder.FullPixelDiamond(mvpRow, mvpCol, startMvSad, stepParam,
		sadPerBit, furtherSteps, true, refRow, refCol, mvLimits, sadAtRC, varAt)

	bestDx, bestDy = res.BestCol, res.BestRow
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
