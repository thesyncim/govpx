package codec

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
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

// vp9_fullrd_inter_model.go ports libvpx's build_inter_pred_model_rd_earlyterm
// (vp9/encoder/vp9_rdopt.c:165-282), the per-plane variance MODEL RD that
// handle_inter_mode's SWITCHABLE filter loop scores each interp filter with
// BEFORE the genuine txfm_rd_in_plane. The model is what selects best_filter and
// drives the use_rd_breakout early-outs (vp9_rdopt.c:3103-3108, :3180-3187):
// libvpx prunes a mode (returns INT64_MAX) when the model rd/2 exceeds
// ref_best_rd — WITHOUT ever computing that mode's genuine RD. That pruning is
// load-bearing for byte-exactness: e.g. {0,1,1,0,1} frame-1 SB0 mi(0,0) NEARMV
// is dropped by the :3103 model breakout (model_rd/2=15.1M > ref_best_rd=5.1M)
// even though its genuine RD (2.6M) is the lowest — so NEWMV (8,14) wins.
//
// build_inter_pred_model_rd_earlyterm, verbatim (vp9_rdopt.c:165-282):
//
//	for (i = 0; i < MAX_MB_PLANE; ++i) {
//	  const BLOCK_SIZE bs = get_plane_block_size(bsize, pd);          // :194
//	  const TX_SIZE max_tx_size = max_txsize_lookup[bs];              // :195
//	  const BLOCK_SIZE unit_size = txsize_to_bsize[max_tx_size];      // :196
//	  int bw = 1 << (b_width_log2_lookup[bs]  - b_width_log2_lookup[unit_size]);
//	  int bh = 1 << (b_height_log2_lookup[bs] - b_width_log2_lookup[unit_size]);
//	  ... for each (idy,idx) unit: var = vf(src,dst,&sse); sum_sse += sse;  // :217-249
//	  qstep = pd->dequant[1] >> dequant_shift(=3);                    // :252
//	  nlog2 = num_pels_log2_lookup[bs];                               // :253
//	  if (simple_model_rd_from_var) { ... } else
//	    vp9_model_rd_from_var_lapndz(sum_sse, nlog2, qstep, &rate, &dist); // :266
//	  rate_sum += rate; dist_sum += dist;                            // :267-269
//	}
//	*out_rate_sum = rate_sum;
//	*out_dist_sum = dist_sum << VP9_DIST_SCALE_LOG2;                  // :279
//	*skip_sse_sb  = total_sse << VP9_DIST_SCALE_LOG2;                 // :277
//	*skip_txfm_sb = skip_flag;                                        // :276
//
// (The skip_txfm / low_err_skip per-unit bookkeeping at :229-247 only affects
// skip_txfm_sb and x->skip_txfm[]; the realtime full-RD path always re-runs the
// genuine yrd/uvrd when !skip_txfm_sb, and for these seeds skip_flag is 0, so
// the model's job here is the {rate_sum, dist_sum} estimate + the skip flag.)
//
// dequant_shift is 3 (non-highbitdepth, vp9_rdopt.c:182-186). VP9_DIST_SCALE_LOG2
// is 4 (vp9_rd.h:48). cpi->sf.simple_model_rd_from_var is 0 at cpu_used<=4
// (set at speed>=5, vp9_speed_features.c:614), so the lapndz path is used.

// vp9ModelRDForInterSBResult is build_inter_pred_model_rd_earlyterm's output.
type vp9ModelRDForInterSBResult struct {
	RateSum  int    // out_rate_sum
	DistSum  uint64 // out_dist_sum (already << VP9_DIST_SCALE_LOG2)
	SkipSSE  uint64 // skip_sse_sb (total_sse << VP9_DIST_SCALE_LOG2)
	SkipFlag bool   // skip_txfm_sb (always false here; see vp9ModelRDForInterSBForMi)
	Valid    bool
}

// vp9ModelRDDistScaleLog2 is libvpx VP9_DIST_SCALE_LOG2 (vp9_rd.h:48).
const vp9ModelRDDistScaleLog2 = 4

// vp9ModelRDForInterSB builds the inter predictor for (mode, refFrame, mv,
// filter) into the recon planes and computes the per-plane variance MODEL RD
// (build_inter_pred_model_rd_earlyterm). do_earlyterm/best_rd implement the
// per-plane early-exit at vp9_rdopt.c:270-274: when do_earlyterm and the running
// RDCOST(rate_sum, dist_sum<<scale) >= best_rd it returns Valid=false (the C
// function returns 1 / "skip the filter").
func (e *VP9Encoder) vp9ModelRDForInterSB(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter, rdmult int, doEarlyterm bool, bestRD uint64,
) vp9ModelRDForInterSBResult {
	if inter == nil || inter.dq == nil || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return vp9ModelRDForInterSBResult{}
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame:     [2]int8{refFrame, vp9dec.NoRefFrame},
		Mv:           [2]vp9dec.MV{mv},
	}
	// libvpx builds the predictor per plane inside the loop via
	// vp9_build_inter_predictors_sbp; govpx's predictVP9InterBlock builds all
	// three planes once into the recon planes (idempotent across calls).
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return vp9ModelRDForInterSBResult{}
	}
	return e.vp9ModelRDForInterSBForMi(inter, miRows, miCols, miRow, miCol, bsize,
		rdmult, doEarlyterm, bestRD)
}

// vp9ModelRDForInterSBForMi is the post-prediction core: assumes the inter
// predictor for the candidate is already on the recon planes and accumulates the
// per-plane lapndz model RD.
func (e *VP9Encoder) vp9ModelRDForInterSBForMi(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	rdmult int, doEarlyterm bool, bestRD uint64,
) vp9ModelRDForInterSBResult {
	segID := vp9EncoderMiSegmentID(nil)
	rateSum := int64(0)
	distSum := int64(0)
	totalSSE := uint64(0)
	// libvpx skip_flag (vp9_rdopt.c:180,239-245,276) starts 1 and clears to 0 the
	// moment any unit is not "low_err_skip". It only feeds skip_txfm_sb, which
	// gates whether handle_inter_mode re-runs the genuine yrd/uvrd. The realtime
	// full-RD callers here always want the genuine RD (skip_txfm_sb == 0 path),
	// and for the high-variance seeds it targets skip_flag is 0; SkipFlag is not
	// consumed by vp9HandleInterMode. Report false (not all-skippable) — the
	// conservative value — rather than implement the per-unit low_err_skip
	// bookkeeping, which would only matter for an all-flat block this path never
	// commits.
	skipFlag := false

	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return vp9ModelRDForInterSBResult{}
		}
		planeData, stride := e.vp9EncoderReconPlane(plane)
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
			return vp9ModelRDForInterSBResult{}
		}
		baseX := (miCol * common.MiSize) >> pd.SubsamplingX
		baseY := (miRow * common.MiSize) >> pd.SubsamplingY

		// libvpx vp9_rdopt.c:195-208: unit_size = txsize_to_bsize[max_tx_size]
		// and the unit tiling. bw/bh are in unit_size steps; note bh uses
		// b_width_log2_lookup[unit_size] (a square unit, so width==height log2).
		maxTx := common.MaxTxsizeLookup[planeBsize]
		unitSize := common.TxsizeToBsize[maxTx]
		unitWLog2 := int(common.BWidthLog2Lookup[unitSize])
		bw := 1 << uint(int(common.BWidthLog2Lookup[planeBsize])-unitWLog2)
		bh := 1 << uint(int(common.BHeightLog2Lookup[planeBsize])-unitWLog2)
		// unit side in pixels: a square unit at max_tx_size is (4 << max_tx_size).
		unitPx := 4 << uint(maxTx)

		// Clamp the measured region to the visible plane (libvpx vf reads the
		// full unit_size block; for partial SB-edge blocks the source/predictor
		// planes carry the extended border, so the full unit applies — but guard
		// against running past the buffer for safety).
		var sumSSE uint64
		for idy := range bh {
			for idx := range bw {
				ux := baseX + idx*unitPx
				uy := baseY + idy*unitPx
				w := unitPx
				h := unitPx
				if ux+w > stride {
					w = stride - ux
				}
				if uy+h > len(planeData)/stride {
					h = len(planeData)/stride - uy
				}
				if w <= 0 || h <= 0 {
					return vp9ModelRDForInterSBResult{}
				}
				_, sse, ok := encoder.BlockDiffVarianceSSEClampedSource(
					src, srcStride, srcW, srcH, planeData, stride,
					ux, uy, ux, uy, w, h)
				if !ok {
					return vp9ModelRDForInterSBResult{}
				}
				sumSSE += sse
			}
		}
		totalSSE += sumSSE

		var dequant [2]int16
		if plane == 0 {
			dequant = inter.dq.Y[segID]
		} else {
			dequant = inter.dq.Uv[segID]
		}
		qstep := uint32(dequant[1]) >> 3 // dequant_shift == 3 (vp9_rdopt.c:186)
		nlog2 := uint(common.NumPelsLog2Lookup[planeBsize])

		// cpi->sf.simple_model_rd_from_var == 0 at cpu_used<=4 → lapndz path.
		rate, dist := encoder.ModelRDFromVarLapndz(uint32(sumSSE), nlog2, qstep)
		rateSum += int64(rate)
		distSum += dist

		if doEarlyterm {
			// libvpx vp9_rdopt.c:270-273.
			if encoder.RDCost(rdmult, encoder.RDDivBits, int(rateSum),
				uint64(distSum)<<vp9ModelRDDistScaleLog2) >= bestRD {
				return vp9ModelRDForInterSBResult{}
			}
		}
	}

	return vp9ModelRDForInterSBResult{
		RateSum:  int(rateSum),
		DistSum:  uint64(distSum) << vp9ModelRDDistScaleLog2,
		SkipSSE:  totalSSE << vp9ModelRDDistScaleLog2,
		SkipFlag: skipFlag,
		Valid:    true,
	}
}

// vp9_fullrd_inter_sub8x8_intra.go ports the INTRA evaluation that libvpx's
// vp9_rd_pick_inter_mode_sub8x8 runs for the INTRA_FRAME reference (ref_index 5
// in vp9_ref_order, i.e. evaluated AFTER every inter ref). The two pieces are:
//
//   - rd_pick_intra_sub_8x8_y_mode (vp9/encoder/vp9_rdopt.c:1299-1360): the
//     per-4x4 sub-block intra Y mode search across the 8x8 (driving
//     rd_pick_intra4x4block per sub-block of size 4x4 / 8x4 / 4x8). On an inter
//     frame the per-sub-block bmode_costs row is the FIXED cpi->mbmode_cost
//     (= cost_tokens(fc->y_mode_prob[1]), vp9_rd.c:103) — NOT the keyframe
//     context-keyed cpi->y_mode_costs[A][L] (that override only runs when
//     cpi->common.frame_type == KEY_FRAME, vp9_rdopt.c:1325-1330).
//   - choose_intra_uv_mode (vp9_rdopt.c:1531-1549) → rd_pick_intra_sbuv_mode
//     (vp9_rdopt.c:1468-1512) over max(bsize, BLOCK_8X8) at TX_4X4, with the
//     inter-frame UV cost intra_uv_mode_cost[INTER_FRAME][y_mode][uv_mode]
//     (= cost_tokens(fc->uv_mode_prob[y_mode]), vp9_rd.c:107-108).
//
// The wrapper (rdPickInterModeSub8x8) calls rdPickInterSub8x8IntraYMode after
// the inter ref loop, then sums Y + UV rate/dist + intra_cost_penalty + the
// INTRA ref-frame signalling cost + the no-skip flag, applies RDCOST, and
// commits the intra mode when it beats the running best (mi->mode = the chosen
// intra mode, ref_frame[0] = INTRA_FRAME, mi->interp_filter = SWITCHABLE_FILTERS,
// mi->mv[0] = 0). GATED behind vp9InterUseDeepRDSub8x8 with the rest of the
// wrapper; production keeps the model stand-in.

// vp9Sub8x8IntraYResult is rd_pick_intra_sub_8x8_y_mode's output: the committed
// per-sub-block bmi quartet + the running rate/dist + the post-coding plane[0]
// entropy context (t_above[2]/t_left[2]) for the next-sibling stamp.
type vp9Sub8x8IntraYResult struct {
	bmi        [4]vp9dec.Bmi
	mode       common.PredictionMode // mic->mode = bmi[3].as_mode
	rate       int                   // *rate  = cost (incl. mbmode_cost per sub-block)
	rateY      int                   // *rate_y = tot_rate_y (token-only)
	distortion uint64                // *distortion = total_distortion
	segEntropy vp9Sub8x8SegmentEntropy
	rd         uint64 // RDCOST(rdmult, rddiv, cost, total_distortion)
	valid      bool
}

// rdPickInterSub8x8IntraYMode ports rd_pick_intra_sub_8x8_y_mode for the
// inter-frame case (vp9_rdopt.c:1299-1360). bsize is the sub-8x8 shape
// (BLOCK_4X4 / 8X4 / 4X8); the footprint is the 8x8 at (miRow, miCol).
// bestRD is the running best_rd (the inter best so far) used as the per-sub-block
// early-exit budget. Returns valid=false when total_rd reaches best_rd (the
// libvpx INT64_MAX early-exit at vp9_rdopt.c:1337,1350).
func (e *VP9Encoder) rdPickInterSub8x8IntraYMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, rdmult int, bestRD uint64,
) (vp9Sub8x8IntraYResult, bool) {
	if inter == nil || mi == nil || bsize >= common.Block8x8 {
		return vp9Sub8x8IntraYResult{}, false
	}
	keyLike := e.vp9InterIntraKeyframeState(inter)
	if keyLike.dq == nil || keyLike.img == nil {
		return vp9Sub8x8IntraYResult{}, false
	}

	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])

	// libvpx vp9_rdopt.c:1318 + 1316-1330 — bmode_costs = cpi->mbmode_cost for
	// inter frames (the frame_type==KEY_FRAME override at :1325-1330 does NOT
	// fire on an inter frame). mbmode_cost = cost_tokens(fc->y_mode_prob[1]).
	var bmodeCosts [common.IntraModes]int
	vp9FullRDInterIntraYModeCosts(bmodeCosts[:], &inter.selectFc)

	// rd_pick_intra4x4block pins mi->tx_size = TX_4X4 (vp9_rdopt.c:1088).
	mi.TxSize = common.Tx4x4

	// Seed t_above[2]/t_left[2] from the live plane[0] entropy context (the
	// sibling-stamped seed), then thread it across the per-sub-block grid exactly
	// as libvpx passes xd->plane[0].above_context + idx / left_context + idy into
	// rd_pick_intra4x4block (vp9_rdopt.c:1334) and that function writes the winning
	// tempa/templ back through those pointers (vp9_rdopt.c:1280-1282).
	var seed vp9Sub8x8SegmentEntropy
	e.vp9Sub8x8SeedEntropy(&seed, miRow, miCol)
	aboveCtx := seed.above
	leftCtx := seed.left

	// e.cbRdmult / pickVP9Sub4x4IntraBlockMode path read the dequant from
	// key.dq.Y[segID]; rdmult feeds the per-sub-block RDCOST. Mirror the keyframe
	// driver's cbRdmult prime so any downstream cbRdmult read is the inter rdmult.
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	defer func() { e.cbRdmult = prevCbRdmult }()

	var total vp9Sub8x8IntraYResult
	totalRD := uint64(0)
	// libvpx vp9_rdopt.c:1319-1320 — for (idy=0; idy<2; idy+=num_4x4_high)
	// for (idx=0; idx<2; idx+=num_4x4_wide).
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			i := idy*2 + idx
			// best_rd - total_rd is the per-sub-block budget (vp9_rdopt.c:1335).
			remainingRD := ^uint64(0)
			if bestRD != ^uint64(0) {
				if totalRD >= bestRD {
					return vp9Sub8x8IntraYResult{}, false
				}
				remainingRD = bestRD - totalRD
			}
			bestMode, rd, ok := e.pickVP9Sub4x4IntraBlockMode(&keyLike, tile,
				miRows, miCols, miRow, miCol, bsize, mi, idy, idx, bmodeCosts[:],
				rdmult, aboveCtx[idx:idx+num4x4W], leftCtx[idy:idy+num4x4H],
				remainingRD)
			if !ok {
				return vp9Sub8x8IntraYResult{}, false
			}
			thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, rd.rate, rd.distortion)
			// libvpx vp9_rdopt.c:1337 — if (this_rd >= best_rd - total_rd) return
			// INT64_MAX.
			if remainingRD != ^uint64(0) && thisRD >= remainingRD {
				return vp9Sub8x8IntraYResult{}, false
			}
			totalRD += thisRD
			total.rate += rd.rate
			total.rateY += rd.rateTokenOnly
			total.distortion += rd.distortion
			// libvpx vp9_rdopt.c:1344-1348 — replicate best_mode into the bmi cells
			// the sub-block covers.
			mi.Bmi[i].AsMode = bestMode
			for j := 1; j < num4x4H; j++ {
				mi.Bmi[i+j*2].AsMode = bestMode
			}
			for j := 1; j < num4x4W; j++ {
				mi.Bmi[i+j].AsMode = bestMode
			}
			// libvpx vp9_rdopt.c:1350 — if (total_rd >= best_rd) return INT64_MAX.
			if bestRD != ^uint64(0) && totalRD >= bestRD {
				return vp9Sub8x8IntraYResult{}, false
			}
		}
	}
	// libvpx vp9_rdopt.c:1357 — mic->mode = mic->bmi[3].as_mode.
	mi.Mode = mi.Bmi[3].AsMode
	total.mode = mi.Mode
	total.bmi = mi.Bmi
	total.segEntropy = vp9Sub8x8SegmentEntropy{above: aboveCtx, left: leftCtx}
	total.rd = encoder.RDCost(rdmult, encoder.RDDivBits, total.rate, total.distortion)
	total.valid = true
	return total, true
}

// vp9Sub8x8IntraUVResult is choose_intra_uv_mode's output: the chosen chroma
// mode + its rate/dist for the 8x8 (BLOCK_8X8) chroma extent.
type vp9Sub8x8IntraUVResult struct {
	mode          common.PredictionMode
	rate          int    // rate_uv_intra (this_rate = tokenonly + uv mode cost)
	rateTokenOnly int    // rate_uv (rate_uv_tokenonly)
	distortion    uint64 // dist_uv
	skippable     bool
}

// vp9Sub8x8IntraUVRD ports choose_intra_uv_mode (vp9_rdopt.c:1531-1549) →
// rd_pick_intra_sbuv_mode (vp9_rdopt.c:1468-1512) for the inter-frame sub-8x8
// intra path: a full super_block_uvrd search over BLOCK_8X8 at TX_4X4 with the
// inter-frame UV cost intra_uv_mode_cost[INTER_FRAME][yMode][mode]. yMode is the
// chosen Y mode (mi->mode after rd_pick_intra_sub_8x8_y_mode) which the UV cost
// table is keyed on (vp9_rdopt.c:1496 cpi->intra_uv_mode_cost[...][xd->mi[0]->mode]).
func (e *VP9Encoder) vp9Sub8x8IntraUVRD(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	yMode common.PredictionMode, rdmult int,
) (vp9Sub8x8IntraUVResult, bool) {
	if inter == nil {
		return vp9Sub8x8IntraUVResult{}, false
	}
	keyLike := e.vp9InterIntraKeyframeState(inter)
	if keyLike.dq == nil || keyLike.img == nil {
		return vp9Sub8x8IntraUVResult{}, false
	}
	if int(yMode) < 0 || int(yMode) >= common.IntraModes {
		yMode = common.DcPred
	}
	// libvpx vp9_rdopt.c:1540/1545 — rd_pick_intra_sbuv_mode receives
	// max(bsize, BLOCK_8X8); for a sub-8x8 partition that is BLOCK_8X8.
	const uvBsize = common.Block8x8
	maxTxSize := common.MaxTxsizeLookup[uvBsize]
	uvMask := e.sf.IntraUvModeMask[maxTxSize]
	if uvMask == 0 {
		uvMask = sfIntraAll
	}
	// libvpx vp9_rd.c:107-108 — intra_uv_mode_cost[INTER_FRAME][y_mode] =
	// cost_tokens(fc->uv_mode_prob[y_mode]).
	var uvModeCosts [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(uvModeCosts[:], vp9FullRDInterFrame, yMode,
		&inter.selectFc)

	// The UV transform predictor needs an mi whose Mode (Y), TxSize and SbType
	// drive get_uv_tx_size / the chroma neighbour context. mi->sb_type stays the
	// sub-8x8 shape for the predictor; uv_tx_size from BLOCK_8X8 + TX_4X4 = TX_4X4.
	mi := vp9dec.NeighborMi{
		SbType:   uvBsize,
		TxSize:   common.Tx4x4,
		Mode:     yMode,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
	}

	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	defer func() { e.cbRdmult = prevCbRdmult }()

	useTxDomainDistortion := e.vp9KeyframeUseTransformDomainDistortion(&keyLike,
		miRows, miCols, miRow, miCol, uvBsize)

	best := vp9Sub8x8IntraUVResult{mode: common.DcPred}
	bestRD := ^uint64(0)
	bestValid := false
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if uvMask&(1<<uint(mode)) == 0 {
			continue
		}
		bestRDForMode := bestRD
		bestRDValid := bestValid
		coeffRate, distortion, skippable, ok := e.scoreVP9KeyframeUvModeTransformRD(
			&keyLike, mode, uvBsize, tile, miRows, miCols, miRow, miCol, &mi,
			useTxDomainDistortion, rdmult, uvModeCosts[mode], bestRDForMode,
			bestRDValid)
		if !ok {
			continue
		}
		thisRate := coeffRate + uvModeCosts[mode]
		thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, thisRate, distortion)
		if !bestValid || thisRD < bestRD {
			bestRD = thisRD
			best = vp9Sub8x8IntraUVResult{
				mode:          mode,
				rate:          thisRate,
				rateTokenOnly: coeffRate,
				distortion:    distortion,
				skippable:     skippable,
			}
			bestValid = true
		}
	}
	if !bestValid {
		return vp9Sub8x8IntraUVResult{}, false
	}
	return best, true
}

// vp9_fullrd_inter_sub8x8_segment.go ports encode_inter_mb_segment
// (vp9/encoder/vp9_rdopt.c:1608-1734): the per-label residual RD for the
// sub-8x8 joint search. For label i it builds the inter predictor over the
// label's pw×ph footprint with mi.bmi[i].as_mv[0], subtracts the source, and
// walks the footprint in 4x4 transform units running fdct4x4 → vpx_quantize_b →
// vp9_block_error → cost_coeffs, accumulating thisrate/thisdistortion/thissse
// with the running this_rd early-exit. Distortion and sse are reported >> 2
// (vp9_rdopt.c:1729-1731).

// vp9Sub8x8SegmentEntropy holds the t_above[2]/t_left[2] entropy context the
// sub-8x8 segment carries from label to label (vp9_rdopt.c:2103,2120-2121,
// 2372-2373). It is the 8x8 block's 2-wide/2-high above/left context.
type vp9Sub8x8SegmentEntropy struct {
	above [2]uint8
	left  [2]uint8
}

// vp9Sub8x8SegmentRD is encode_inter_mb_segment's output for one label.
type vp9Sub8x8SegmentRD struct {
	byrate int    // *labelyrate (cost_coeffs sum)
	dist   uint64 // *distortion (thisdistortion >> 2)
	sse    uint64 // *sse (thissse >> 2)
	rdcost uint64 // RDCOST(rdmult, rddiv, labelyrate, distortion)
	eob    int    // p->eobs[block] (the label's first 4x4 eob, for the probe)
	anyEob bool   // any 4x4 unit in the label had eob > 0 (→ not skippable)
}

// encodeInterMbSegment ports encode_inter_mb_segment (vp9_rdopt.c:1608-1734)
// for the single-reference Y plane. mi.bmi[block].as_mv[0] is the label MV
// (already written by set_and_cost_bmi_mvs). ent is updated in place. The
// returned rdcost is RDCOST(labelyrate, distortion); the caller adds the
// mode/MV-rate RDCOST and the early-exit budget is bestYrd.
func (e *VP9Encoder) encodeInterMbSegment(inter *vp9InterEncodeState,
	in vp9Sub8x8Input, ent *vp9Sub8x8SegmentEntropy, bsize common.BlockSize,
	block int, refFrame int8, mi *vp9dec.NeighborMi, bestYrd uint64,
) (vp9Sub8x8SegmentRD, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return vp9Sub8x8SegmentRD{}, false
	}
	// plane_bsize width/height = label footprint pw×ph (vp9_rdopt.c:1618-1620).
	width := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	height := int(common.Num4x4BlocksHighLookup[bsize]) * 4

	// Sub-block raster position inside the 8x8 (vp9_rdopt.c:1636-1638):
	// b_width_log2_lookup[BLOCK_8X8] == 1 → h = 4*(i>>1), w = 4*(i&1).
	x0Block := in.miCol*common.MiSize + 4*(block&1)
	y0Block := in.miRow*common.MiSize + 4*(block>>1)

	pre, preStride, preOriginX, preOriginY, _, _, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if !refOK || len(pre) == 0 || preStride <= 0 {
		return vp9Sub8x8SegmentRD{}, false
	}
	mv := mi.Bmi[block].AsMv[0]
	filterIdx := int(mi.InterpFilter)
	if filterIdx < 0 || filterIdx >= int(vp9dec.InterpSwitchable) {
		return vp9Sub8x8SegmentRD{}, false
	}
	predBuf := e.vp9Sub8x8PredScratch(width * height)
	if predBuf == nil {
		return vp9Sub8x8SegmentRD{}, false
	}
	// vp9_build_inter_predictor with MV_PRECISION_Q3 (vp9_reconinter.c:
	// mv_q4 = mv*2; subpel = mv_q4 & SUBPEL_MASK(15); integer = mv_q4 >> 4).
	// encode_inter_mb_segment calls it directly with the raw (unclamped) MV.
	mvQ4Col := int(mv.Col) * 2
	mvQ4Row := int(mv.Row) * 2
	preX := x0Block + (mvQ4Col >> vp9dec.SubpelBitsConst)
	preY := y0Block + (mvQ4Row >> vp9dec.SubpelBitsConst)
	bufX := preOriginX + preX
	bufY := preOriginY + preY
	preRows := len(pre) / preStride
	if bufX < 0 || bufY < 0 || bufX+width+1 > preStride || bufY+height+1 > preRows {
		return vp9Sub8x8SegmentRD{}, false
	}
	preOff := bufY*preStride + bufX
	subpelX := mvQ4Col & (vp9dec.SubpelShifts - 1)
	subpelY := mvQ4Row & (vp9dec.SubpelShifts - 1)
	vp9dec.InterPredictor(pre, preStride, predBuf, width, subpelX, subpelY,
		tables.FilterKernels[filterIdx], vp9dec.SubpelShifts, vp9dec.SubpelShifts,
		width, height, 0, preOff)

	dequant := inter.dq.Y[0]
	qindex := inter.baseQindex
	scan := common.DefaultScanOrders[common.Tx4x4]

	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	var diff [16]int16

	var thisrate int
	var thisdistortion uint64
	var thissse uint64
	firstEob := 0
	anyEob := false

	w4 := width / 4
	h4 := height / 4
	// k accumulates exactly as libvpx (vp9_rdopt.c:1689-1699).
	k := block
	for idy := range h4 {
		for idx := range w4 {
			k += idy*2 + idx
			col := k & 1
			row := k >> 1
			// combine_entropy_contexts(ta[k&1], tl[k>>1]) for TX_4X4
			// (vp9_rdopt.c:1700): (above!=0) + (left!=0).
			coeffCtx := vp9dec.GetEntropyContextFull(common.Tx4x4,
				ent.above[col:col+1], ent.left[row:row+1])

			ux := x0Block + 4*idx
			uy := y0Block + 4*idy
			if !vp9Sub8x8GatherResidual(src, srcStride, srcW, srcH, predBuf, width,
				ux, uy, 4*idx, 4*idy, diff[:]) {
				return vp9Sub8x8SegmentRD{}, false
			}
			encoder.ForwardDCT4x4Into(diff[:], 4, coeff[:])
			eob := encoder.QuantizeBWithQScanOrder(coeff[:], qindex, dequant,
				scan, qcoeff[:], dqcoeff[:])

			// vp9_block_error: error = sum(coeff-dqcoeff)^2, ssz = sum(coeff^2).
			thisdistortion += encoder.BlockErrorFP(coeff[:], dqcoeff[:])
			thissse += vp9Sub8x8SumSquares(coeff[:])

			thisrate += e.vp9InterCoeffBlockRateCostQEOB(common.Tx4x4, 0, dequant,
				coeff[:], qcoeff[:], coeffCtx, eob, true)

			hasCtx := uint8(0)
			if eob > 0 {
				hasCtx = 1
				anyEob = true
			}
			ent.above[col] = hasCtx
			ent.left[row] = hasCtx
			if idy == 0 && idx == 0 {
				firstEob = eob
			}

			// rd1 = RDCOST(rate, dist>>2); rd2 = RDCOST(0, sse>>2);
			// rd = min(rd1, rd2); if rd >= best_yrd return INT64_MAX.
			rd1 := encoder.RDCost(in.rdmult, encoder.RDDivBits, thisrate,
				thisdistortion>>2)
			rd2 := encoder.RDCost(in.rdmult, encoder.RDDivBits, 0, thissse>>2)
			rd := min(rd2, rd1)
			if bestYrd != rdCostMaxLocal && rd >= bestYrd {
				return vp9Sub8x8SegmentRD{}, false
			}
		}
	}

	dist := thisdistortion >> 2
	sse := thissse >> 2
	return vp9Sub8x8SegmentRD{
		byrate: thisrate,
		dist:   dist,
		sse:    sse,
		rdcost: encoder.RDCost(in.rdmult, encoder.RDDivBits, thisrate, dist),
		eob:    firstEob,
		anyEob: anyEob,
	}, true
}

// vp9Sub8x8SumSquares returns sum(coeff[i]^2) (the ssz term of vp9_block_error,
// vp9_rdopt.c:327).
func vp9Sub8x8SumSquares(coeff []int16) uint64 {
	var s uint64
	for _, c := range coeff[:min(16, len(coeff))] {
		s += uint64(int64(c) * int64(c))
	}
	return s
}

// vp9Sub8x8GatherResidual writes the (src - predictor) 4x4 residual for one
// transform unit into diff (raster, stride 4).
func vp9Sub8x8GatherResidual(src []byte, srcStride, srcW, srcH int,
	predBuf []byte, predStride, srcX, srcY, predX, predY int, diff []int16,
) bool {
	if srcX < 0 || srcY < 0 || srcX+4 > srcW || srcY+4 > srcH {
		return false
	}
	if predX < 0 || predY < 0 || predX+4 > predStride ||
		(predY+4)*predStride > len(predBuf) {
		return false
	}
	for y := range 4 {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		predRow := predBuf[(predY+y)*predStride+predX:]
		dRow := diff[y*4:]
		for x := range 4 {
			dRow[x] = int16(int(srcRow[x]) - int(predRow[x]))
		}
	}
	return true
}

// vp9Sub8x8PredScratch returns a reusable byte scratch buffer for the label
// predictor.
func (e *VP9Encoder) vp9Sub8x8PredScratch(n int) []byte {
	if n <= 0 {
		return nil
	}
	if cap(e.sub8x8PredScratch) < n {
		e.sub8x8PredScratch = make([]byte, n)
	}
	return e.sub8x8PredScratch[:n]
}

// vp9Sub8x8SeedEntropy seeds the t_above[2]/t_left[2] segment entropy context
// from the 8x8 block's plane[0] above/left context (vp9_rdopt.c:2120-2121).
func (e *VP9Encoder) vp9Sub8x8SeedEntropy(ent *vp9Sub8x8SegmentEntropy,
	miRow, miCol int,
) {
	pd := &e.planes[0]
	aboveOff, leftOff := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	if ao := aboveOff[0]; ao >= 0 && ao+2 <= len(pd.AboveContext) {
		ent.above[0] = pd.AboveContext[ao]
		ent.above[1] = pd.AboveContext[ao+1]
	}
	if lo := leftOff[0]; lo >= 0 && lo+2 <= len(pd.LeftContext) {
		ent.left[0] = pd.LeftContext[lo]
		ent.left[1] = pd.LeftContext[lo+1]
	}
}

// vp9Sub8x8StampEntropy writes the committed 8x8 block's segment entropy context
// back into the plane[0] above/left context. This is the entropy-context half of
// libvpx's encode_sb / encode_superblock → vp9_foreach_transformed_block →
// vp9_set_contexts (vp9/encoder/vp9_encodeframe.c:4163-4166, the split children
// with pc_tree->index != 3): after a sub-8x8 leaf is committed during the SPLIT
// recursion, its plane entropy context is stamped so the next sibling 8x8's
// rd_pick_best_sub8x8_mode seed (memcpy(t_above, pd->above_context),
// vp9_rdopt.c:2120-2121) reads it. ent.above[0..1]/left[0..1] are the running
// t_above[2]/t_left[2] at segment end (vp9_rdopt.c:2398-2399).
func (e *VP9Encoder) vp9Sub8x8StampEntropy(ent *vp9Sub8x8SegmentEntropy,
	miRow, miCol int,
) {
	pd := &e.planes[0]
	aboveOff, leftOff := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	if ao := aboveOff[0]; ao >= 0 && ao+2 <= len(pd.AboveContext) {
		pd.AboveContext[ao] = ent.above[0]
		pd.AboveContext[ao+1] = ent.above[1]
	}
	if lo := leftOff[0]; lo >= 0 && lo+2 <= len(pd.LeftContext) {
		pd.LeftContext[lo] = ent.left[0]
		pd.LeftContext[lo+1] = ent.left[1]
	}
}
