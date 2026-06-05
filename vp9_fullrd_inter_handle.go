package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_handle.go ports the SWITCHABLE interp-filter loop and the
// ref_best_rd early breakouts of libvpx handle_inter_mode (vp9/encoder/
// vp9_rdopt.c:2811-3218) for a single-reference inter candidate. It is the
// mode-pre-filtering mechanism that selects the interp filter by the variance
// MODEL RD (vp9ModelRDForInterSB) and prunes a mode (returns pruned=true, the
// caller's `continue` at :3881) WITHOUT running the genuine RD, when:
//
//   - the rate-only RD already exceeds ref_best_rd and the mode is not NEARESTMV
//     (vp9_rdopt.c:2994-2996), OR
//   - (use_rd_breakout) the i==0 model rd/2 exceeds ref_best_rd
//     (vp9_rdopt.c:3103-3108), OR
//   - (use_rd_breakout) the post-loop model rd/2 exceeds ref_best_rd
//     (vp9_rdopt.c:3155-3162).
//
// When NOT pruned it runs the GENUINE per-mode RD for the model-selected filter
// via vp9FullRDInterThisRD (super_block_yrd + super_block_uvrd + the skip pick),
// threaded with the same ref_best_rd budget so the genuine producers' txfm-RD
// early-exit fires exactly as libvpx (vp9_rdopt.c:846-849, :3214, :3227). That
// genuine yrd early-exit is what prunes NEWMV at {0,1,1,0,1} mi(1,1).
//
// This faithfully mirrors libvpx's order: the model picks ONE filter, then the
// genuine RD is computed once for that filter — UNLIKE the model-score consider
// path which scores every filter's genuine RD and keeps the lowest. The model
// breakout is load-bearing for byte-exactness (e.g. {0,1,1,0,1} mi(0,0) NEARMV
// is dropped by the :3103 model breakout even though its genuine RD is lowest).
//
// Filter-loop speed-feature configuration for the realtime cpu_used<=4 path
// (set_rt_speed_feature_framesize_independent, vp9_speed_features.c:452-660):
//   - cb_pred_filter_search == 0  -> pred_filter_search == 0, enable_interp_search == 1
//   - adaptive_interp_filter_search == 0, early_term_interp_search_plane_rd == 0
//     (those are GOOD-quality-only, set_good_speed_feature)
//   - simple_model_rd_from_var == 0 (set at speed>=5) -> lapndz model path
//   - disable_filter_search_var_thresh == 100 (speed 3) -> EIGHTTAP-only when the
//     source-block luma variance is below the threshold (vp9_rdopt.c:3015-3016).
//   - cm->interp_filter == SWITCHABLE on these clips -> rs/rs_rd are charged.

// vp9HandleInterModeResult is the genuine per-candidate decision handle_inter_mode
// produces for the model-selected filter, or Pruned (the libvpx INT64_MAX path).
type vp9HandleInterModeResult struct {
	Pruned bool
	Filter vp9dec.InterpFilter
	RD     vp9FullRDInterThisRDResult
}

// vp9HandleInterMode runs the handle_inter_mode filter loop + breakouts for one
// single-reference inter candidate at the given MV, then (if not pruned) the
// genuine RD for the model-selected filter. refBestRD is the running best_rd
// (ref_best_rd); refBestRDInf marks ref_best_rd == INT64_MAX (no budget yet, so
// the use_rd_breakout model breakouts are disabled, matching libvpx's
// `ref_best_rd < INT64_MAX` guard).
func (e *VP9Encoder) vp9HandleInterMode(inter *vp9InterEncodeState,
	in vp9FullRDInterThisRDInput, mode common.PredictionMode, mv, refMv vp9dec.MV,
	src []byte, srcStride, x0, y0, scoreW, scoreH int,
	refBestRD uint64, refBestRDInf bool,
) vp9HandleInterModeResult {
	rddiv := encoder.RDDivBits

	// --- *rate2 at the :2994 breakout: cost_mv_ref + MV-bit-cost (NEWMV
	// discounted), NO interp-filter rate yet (vp9_rdopt.c:2936-2977).
	discount := e.vp9FullRDInterDiscountNewMv(inter, in, mode, mv)
	rateOnly := encoder.InterModeMvRateWithDiscount(&inter.selectFc,
		in.interModeCtx, mode, mv, refMv, inter.allowHP, discount)

	// libvpx vp9_rdopt.c:2994-2996 — rate-only RD breakout, NEARESTMV exempt.
	if !refBestRDInf {
		if encoder.RDCost(in.rdmult, rddiv, rateOnly, 0) > refBestRD &&
			mode != common.NearestMv {
			return vp9HandleInterModeResult{Pruned: true}
		}
	}

	useRDBreakout := e.vp9FullRDUseRDBreakout()
	switchable := vp9InterFrameInterpFilter(inter) == vp9dec.InterpSwitchable

	// libvpx vp9_rdopt.c:3043 intpel_mv = !mv_has_subpel(&mi->mv[0].as_mv), and
	// mv_has_subpel (vp9_rdopt.c:1824) masks (row|col) & 0x0F — NOT & 7. So an MV
	// like (0,24) (col & 0x0F == 8) counts as SUBPEL here even though its 1/8-pel
	// fraction (col & 7) is 0; the filter loop then re-runs the model per filter
	// instead of the intpel reuse. This is a libvpx quirk ported verbatim.
	intpelMV := !vp9MvHasSubpel0F(mv)

	// libvpx vp9_rdopt.c:3015-3016 — source-block luma variance gate:
	// if (x->source_variance < disable_filter_search_var_thresh) best_filter =
	// EIGHTTAP and the SWITCHABLE filter loop is SKIPPED (tmp_rd stays INT64_MAX,
	// so the post-loop takes the `else` branch at :3133-3145).
	varThreshFired := false
	if e.sf.DisableFilterSearchVarThresh > 0 && scoreW > 0 && scoreH > 0 &&
		switchable {
		sourceVariance := encoder.SourceVarianceAreaPerPixel(src, srcStride,
			x0, y0, scoreW, scoreH)
		if encoder.InterSkipFilterSearch(sourceVariance,
			e.sf.DisableFilterSearchVarThresh) {
			varThreshFired = true
		}
	}

	bestFilter := vp9dec.InterpEighttap
	tmpRD := ^uint64(0) // libvpx tmp_rd (best_rd from the loop, with rs)

	if !varThreshFired {
		// --- interp-filter loop (vp9_rdopt.c:3022-3115): select best_filter by
		// the variance MODEL RD. Mirrors the cm->interp_filter == SWITCHABLE arm.
		filters := vp9InterInterpFilterCandidates(inter)
		bestModelRD := ^uint64(0)
		var tmpRateSum int
		var tmpDistSum uint64
		haveTmp := false
		bestSet := false
		for fi, filter := range filters {
			rs := vp9InterInterpFilterRateCost(inter, &inter.selectFc,
				in.switchableCtx, filter)
			rsRD := encoder.RDCost(in.rdmult, rddiv, rs, 0)

			var rd uint64
			if fi > 0 && intpelMV && haveTmp {
				// libvpx vp9_rdopt.c:3035-3041 — integer-pel MV: the prediction
				// is identical across filters, so reuse the i==0 model {rate,dist}.
				rd = encoder.RDCost(in.rdmult, rddiv, tmpRateSum, tmpDistSum)
			} else {
				m := e.vp9ModelRDForInterSB(inter, in.miRows, in.miCols, in.miRow,
					in.miCol, in.bsize, mode, in.refFrame, mv, filter, in.rdmult,
					false, 0)
				if !m.Valid {
					continue
				}
				rd = encoder.RDCost(in.rdmult, rddiv, m.RateSum, m.DistSum)
				if fi == 0 && intpelMV {
					tmpRateSum = m.RateSum
					tmpDistSum = m.DistSum
					haveTmp = true
				}
			}
			if switchable {
				rd += rsRD // vp9_rdopt.c:3078 (filter_cache[i]=rd; if SWITCHABLE rd+=rs_rd)
			}

			// libvpx vp9_rdopt.c:3103-3108 — i==0 model rd/2 breakout.
			if fi == 0 && useRDBreakout && !refBestRDInf {
				if rd/2 > refBestRD {
					return vp9HandleInterModeResult{Pruned: true}
				}
			}

			if !bestSet || rd < bestModelRD {
				bestModelRD = rd
				bestFilter = filter
				tmpRD = bestModelRD
				bestSet = true
			}
		}
		if !bestSet {
			return vp9HandleInterModeResult{Pruned: true}
		}
	}

	// --- post-loop rd (vp9_rdopt.c:3119-3145).
	rs := 0
	if switchable {
		rs = vp9InterInterpFilterRateCost(inter, &inter.selectFc,
			in.switchableCtx, bestFilter)
	}
	var postRD uint64
	if tmpRD != ^uint64(0) {
		// vp9_rdopt.c:3132 — rd = tmp_rd + RDCOST(rs, 0). tmp_rd already folds
		// best_filter's rs (the loop added rs_rd), so this charges rs twice; ported
		// verbatim.
		postRD = tmpRD + encoder.RDCost(in.rdmult, rddiv, rs, 0)
	} else {
		// vp9_rdopt.c:3133-3142 — the var-thresh / bilinear case: run the model
		// directly (do_earlyterm=0) and rd = RDCOST(rs + tmp_rate, tmp_dist).
		m := e.vp9ModelRDForInterSB(inter, in.miRows, in.miCols, in.miRow,
			in.miCol, in.bsize, mode, in.refFrame, mv, bestFilter, in.rdmult,
			false, 0)
		if !m.Valid {
			return vp9HandleInterModeResult{Pruned: true}
		}
		postRD = encoder.RDCost(in.rdmult, rddiv, rs+m.RateSum, m.DistSum)
	}

	// libvpx vp9_rdopt.c:3155-3162 — post-loop model rd/2 breakout.
	if useRDBreakout && !refBestRDInf {
		if postRD/2 > refBestRD {
			return vp9HandleInterModeResult{Pruned: true}
		}
	}

	// --- genuine RD for the model-selected filter, threaded with the budget.
	gin := in
	gin.refBestRD = refBestRD
	gin.refBestRDInf = refBestRDInf
	grd := e.vp9FullRDInterThisRD(inter, gin, mode, mv, refMv, bestFilter)
	if !grd.Valid {
		// super_block_yrd / super_block_uvrd early-exited past the budget
		// (vp9_rdopt.c:3214-3218, :3227-3233) -> handle_inter_mode returns
		// INT64_MAX -> caller `continue`s.
		return vp9HandleInterModeResult{Pruned: true}
	}
	return vp9HandleInterModeResult{Filter: bestFilter, RD: grd}
}

// vp9FullRDUseRDBreakout mirrors cpi->sf.use_rd_breakout for the realtime
// full-RD path: set at speed>=1 (vp9_speed_features.c:495), default 0 at speed 0
// (the framesize-independent baseline, vp9_speed_features.c:988). cpu_used maps
// to speed; the cpu0 SEARCH seed has use_rd_breakout == 0 (so only the rate-only
// :2994 breakout + the genuine txfm-RD early-exit prune), cpu4 VAR_BASED has it
// set (so the MODEL rd/2 breakouts also fire — the mechanism that drops NEARMV
// at {0,1,1,0,1} mi(0,0)).
func (e *VP9Encoder) vp9FullRDUseRDBreakout() bool {
	return e.sf.UseRdBreakout != 0
}

// vp9MvHasSubpel0F is the handle_inter_mode mv_has_subpel (vp9_rdopt.c:1824-1826):
//
//	static INLINE int mv_has_subpel(const MV *mv) {
//	  return (mv->row & 0x0F) || (mv->col & 0x0F);
//	}
//
// Distinct from vp9MvHasSubpel (mv & 7, the true 1/8-pel fraction): this masks
// 4 bits, so any MV component not a multiple of 16 (2 full pixels) counts as
// "subpel" for the intpel_mv interp-filter-loop reuse gate.
func vp9MvHasSubpel0F(mv vp9dec.MV) bool {
	return (mv.Row&0x0F) != 0 || (mv.Col&0x0F) != 0
}
