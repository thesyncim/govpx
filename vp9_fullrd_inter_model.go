package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

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

	for plane := 0; plane < vp9dec.MaxMbPlane; plane++ {
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
		for idy := 0; idy < bh; idy++ {
			for idx := 0; idx < bw; idx++ {
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
