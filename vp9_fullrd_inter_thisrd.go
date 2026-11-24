package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_thisrd.go assembles the GENUINE per-candidate this_rd the
// way libvpx's vp9_rd_pick_inter_mode_sb does (vp9/encoder/vp9_rdopt.c:3445),
// from the real Y-RD (super_block_yrd, vp9_fullrd_inter_yrd.go) + UV-RD
// (super_block_uvrd, vp9_fullrd_inter_uvrd.go) + the mode/MV/filter/ref rate +
// the skip-vs-non-skip decision. It is the verifiable per-mode RD component the
// holistic full-RD inter port needs.
//
// It replaces, ONLY on the vp9InterUseDeepRDPartition-on branch, the model-RD
// approximation vp9InterModeScore that the production mode loop scores each
// candidate with. When the deep flag is off (production) the genuine assembly
// is never invoked, so production byte-parity is untouched.
//
// libvpx assembly, verbatim (single-reference inter candidate):
//
// handle_inter_mode (vp9_rdopt.c:2811):
//   - rate2 += {discounted} rate_mv          (NEWMV, :2936-2941)
//   - rate2 += {discounted} cost_mv_ref       (:2970-2977)
//   - rate2 += rs (switchable filter rate)    (:3164)
//   - super_block_yrd → rate_y, distortion_y, psse  (:3176)
//   - rate2 += rate_y; distortion += distortion_y   (:3186-3187)
//   - rdcosty = VPXMIN(RDCOST(rate2, distortion), RDCOST(0, psse))  (:3189-3190)
//   - super_block_uvrd(ref_best_rd - rdcosty) → rate_uv, distortion_uv, sseuv (:3192)
//   - psse += sseuv; rate2 += rate_uv; distortion += distortion_uv;
//     skippable = skippable_y && skippable_uv  (:3200-3203)
//
// vp9_rd_pick_inter_mode_sb caller (:3888-3929):
//   - rate2 += ref_costs_single[ref_frame]    (:3893)
//   - skip-vs-non-skip pick (:3896-3930):
//       if (skippable) { rate2 -= rate_y+rate_uv; rate2 += skip_cost1; }
//       else if (inter && !lossless && !sharpness) {
//         if (RDCOST(rate_y+rate_uv+skip0, dist2) < RDCOST(skip1, total_sse))
//           rate2 += skip0;
//         else { rate2 += skip1; dist2 = total_sse; rate2 -= rate_y+rate_uv; }
//       } else rate2 += skip0;
//       this_rd = RDCOST(x->rdmult, x->rddiv, rate2, dist2);
//
// (rd_variance_adjustment / film-grain bias at :3932-3963 only fire when
// recon != NULL, which is the VOD content==FILM path; the realtime full-RD
// path passes recon == NULL, so they are omitted here.)
//
// libvpx ground truth (vpxenc-vp9 + TEMPORARY fprintf, reverted): seed
// {0,2,0,0,2} (CBR 1200 kbps cpu0 realtime, kf=999, fps 30) frame 1 SB0 64x64
// root NEWMV ref=LAST mv=(12,4) filt=EIGHTTAP_SMOOTH at rdmult=139158 rddiv=7:
//
//	rate_y=5464856 dist_y=5496832   (super_block_yrd, TX_16X16)
//	rate_uv=780630 dist_uv=1533408 sseuv=4672816  (super_block_uvrd, uv_tx=TX_16X16)
//	rate2_pre=2132 (rs=1069, discount=1, ref_cost=461)
//	rate2=6248102 dist2=7030240 total_sse=88782496  (skip2=0, no-skip chosen)
//	this_rd=2598060912

// vp9FullRDInterThisRDResult is the per-candidate genuine RD decomposition.
type vp9FullRDInterThisRDResult struct {
	ThisRD     uint64
	Rate       int    // rate2 after the skip pick (libvpx rd_cost->rate)
	Distortion uint64 // distortion2 after the skip pick (libvpx rd_cost->dist)
	RateY      int
	RateUV     int
	DistY      uint64
	DistUV     uint64
	SSE        uint64 // total_sse = psse_y + sse_uv
	TxSize     common.TxSize
	UvTxSize   common.TxSize
	Skippable  bool
	Skip2      bool // this_skip2 (the skip-flag forced by the skip-pick)
	Valid      bool
}

// vp9FullRDInterThisRDInput carries the per-SB picker context the genuine
// this_rd assembly needs that is not derivable from the candidate alone.
type vp9FullRDInterThisRDInput struct {
	tile     vp9dec.TileBounds
	miRows   int
	miCols   int
	miRow    int
	miCol    int
	bsize    common.BlockSize
	refFrame int8
	// interModeCtx is mode_context[ref_frame] (the inter-mode-tree probability
	// context); refRate is ref_costs_single[ref_frame]; switchableCtx is the
	// switchable-interp probability context. above/left are the signalling
	// neighbours for the skip-flag probability.
	interModeCtx  int
	refRate       int
	switchableCtx int
	above         *vp9dec.NeighborMi
	left          *vp9dec.NeighborMi
	rdmult        int
	refBestRD     uint64
	refBestRDInf  bool // ref_best_rd == INT64_MAX (no budget yet)
}

// vp9FullRDInterThisRD computes the genuine per-candidate this_rd for one
// single-reference inter candidate, mirroring handle_inter_mode + the
// vp9_rd_pick_inter_mode_sb skip-pick verbatim.
func (e *VP9Encoder) vp9FullRDInterThisRD(inter *vp9InterEncodeState,
	in vp9FullRDInterThisRDInput, mode common.PredictionMode, mv, refMv vp9dec.MV,
	filter vp9dec.InterpFilter,
) vp9FullRDInterThisRDResult {
	if inter == nil || inter.dq == nil || in.bsize < common.Block8x8 ||
		in.bsize >= common.BlockSizes {
		return vp9FullRDInterThisRDResult{}
	}
	rddiv := encoder.RDDivBits

	// ref_best_rd: handle_inter_mode passes it through to super_block_yrd. When
	// the picker has no budget yet (best_rd == INT64_MAX) the producers run
	// without the early-exit (refBestRD = ^uint64(0)).
	yRefBest := in.refBestRD
	if in.refBestRDInf {
		yRefBest = ^uint64(0)
	}

	// --- mode + MV rate (with NEWMV discount), then the switchable filter rate.
	// libvpx vp9_rdopt.c:2936-2941, :2970-2977 (discount), :3164 (rs).
	discount := e.vp9FullRDInterDiscountNewMv(inter, in, mode, mv)
	modeMvRate := encoder.InterModeMvRateWithDiscount(&inter.selectFc,
		in.interModeCtx, mode, mv, refMv, inter.allowHP, discount)
	filterRate := vp9InterInterpFilterRateCost(inter, &inter.selectFc,
		in.switchableCtx, filter)
	preRate := modeMvRate + filterRate

	// --- Y plane: super_block_yrd (the genuine producer).
	yRD := e.vp9FullRDInterSuperBlockYRD(inter, in.miRows, in.miCols, in.miRow,
		in.miCol, in.bsize, mode, in.refFrame, mv, filter, in.rdmult, yRefBest)
	if !yRD.Valid {
		return vp9FullRDInterThisRDResult{}
	}
	rateY := yRD.Rate
	distY := yRD.Distortion
	psseY := yRD.SSE

	// libvpx :3189-3190 — rdcosty = VPXMIN(RDCOST(rate2, distortion),
	//                                       RDCOST(0, psse)). rate2 here is
	// (preRate + rate_y); distortion is distortion_y; psse is psse_y. ref_cost
	// is NOT yet added (the caller adds it after handle_inter_mode returns).
	rate2YOnly := preRate + rateY
	rdcosty := encoder.RDCost(in.rdmult, rddiv, rate2YOnly, distY)
	if floor := encoder.RDCost(in.rdmult, rddiv, 0, psseY); floor < rdcosty {
		rdcosty = floor
	}

	// --- UV planes: super_block_uvrd with budget ref_best_rd - rdcosty.
	// libvpx :3192-3193. The predictor for the chroma planes was just built by
	// the Y producer's predictVP9InterBlock; re-run it via the standalone UV
	// entry (it rebuilds the full 3-plane predictor, idempotent here).
	uvRefBestValid := true
	var uvRefBest uint64
	if in.refBestRDInf {
		// ref_best_rd == INT64_MAX: ref_best_rd - rdcosty is still huge and
		// always >= 0, so UV runs without an effective early-exit budget.
		uvRefBest = ^uint64(0)
	} else if rdcosty > in.refBestRD {
		// ref_best_rd - rdcosty < 0 → super_block_uvrd's is_cost_valid = 0.
		uvRefBestValid = false
	} else {
		uvRefBest = in.refBestRD - rdcosty
	}
	uvRD := e.vp9FullRDInterSuperBlockUVRD(inter, in.miRows, in.miCols, in.miRow,
		in.miCol, in.bsize, mode, in.refFrame, mv, filter, yRD.TxSize, in.rdmult,
		uvRefBestValid, uvRefBest)
	if !uvRD.Valid {
		return vp9FullRDInterThisRDResult{}
	}
	rateUV := uvRD.Rate
	distUV := uvRD.Distortion
	sseUV := uvRD.SSE

	// --- accumulate (libvpx :3186-3203 + caller :3893).
	rate2 := preRate + rateY + rateUV + in.refRate
	dist2 := distY + distUV
	totalSSE := psseY + sseUV
	skippable := yRD.Skippable && uvRD.Skippable

	// --- skip-vs-non-skip pick (libvpx :3896-3930). disable_skip is never set
	// on this path (skip_txfm_sb == 0 forces the !skip_txfm_sb branch in
	// handle_inter_mode, so disable_skip stays 0), so the caller's
	// !disable_skip branch always runs.
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(in.above, in.left)]
	skip0 := encoder.VP9CostBit(skipProb, 0)
	skip1 := encoder.VP9CostBit(skipProb, 1)
	skip2 := false
	if skippable {
		// Back out the coefficient coding costs, cost the skip-mb case.
		rate2 -= rateY + rateUV
		rate2 += skip1
	} else if in.refFrame > vp9dec.IntraFrame && !inter.lossless &&
		e.opts.Sharpness == 0 {
		noSkip := encoder.RDCost(in.rdmult, rddiv, rateY+rateUV+skip0, dist2)
		skip := encoder.RDCost(in.rdmult, rddiv, skip1, totalSSE)
		if noSkip < skip {
			rate2 += skip0
		} else {
			rate2 += skip1
			dist2 = totalSSE
			rate2 -= rateY + rateUV
			skip2 = true
		}
	} else {
		rate2 += skip0
	}

	thisRD := encoder.RDCost(in.rdmult, rddiv, rate2, dist2)

	return vp9FullRDInterThisRDResult{
		ThisRD:     thisRD,
		Rate:       rate2,
		Distortion: dist2,
		RateY:      rateY,
		RateUV:     rateUV,
		DistY:      distY,
		DistUV:     distUV,
		SSE:        totalSSE,
		TxSize:     yRD.TxSize,
		UvTxSize:   uvRD.UvTxSize,
		Skippable:  skippable,
		Skip2:      skip2,
		Valid:      true,
	}
}

// vp9FullRDInterDiscountNewMv evaluates libvpx's discount_newmv_test for a
// single-reference candidate (vp9/encoder/vp9_rdopt.c:2798-2807). It reads the
// NEARESTMV / NEARMV candidate MVs for the same reference (mode_mv[...][ref]).
func (e *VP9Encoder) vp9FullRDInterDiscountNewMv(inter *vp9InterEncodeState,
	in vp9FullRDInterThisRDInput, mode common.PredictionMode, mv vp9dec.MV,
) bool {
	if mode != common.NewMv {
		return false
	}
	nearest, nearestOK := e.vp9EncoderInterModeCandidateMv(in.tile, in.miRows,
		in.miCols, in.miRow, in.miCol, in.bsize, common.NearestMv, in.refFrame,
		inter.allowHP, inter.refSignBias)
	near, nearOK := e.vp9EncoderInterModeCandidateMv(in.tile, in.miRows,
		in.miCols, in.miRow, in.miCol, in.bsize, common.NearMv, in.refFrame,
		inter.allowHP, inter.refSignBias)
	return encoder.DiscountNewMvTest(inter.isSrcFrameAltRef, mode, mv,
		nearest, nearestOK, near, nearOK)
}
