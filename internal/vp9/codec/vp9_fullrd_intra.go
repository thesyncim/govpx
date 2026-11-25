package codec

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
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

// vp9InterZcoeffBlk holds the per-4x4-block (raster, blockIdx-by-step^2)
// zcoeff_blk decision for a committed inter leaf's Y plane: when an entry is
// true the writer forces that transform unit's eob to 0 (no tokens), mirroring
// libvpx encode_block (vp9/encoder/vp9_encodemb.c:580-588) consuming the
// x->zcoeff_blk[tx_size][block] flag the full-RD mode search committed.
type vp9InterZcoeffBlk struct {
	valid bool
	// flags is indexed by the Y-plane 4x4 raster index (rr*full4x4W+cc), the
	// same layout the writer's residual loop walks. Only the top-left 4x4 of
	// each transform unit is consulted.
	flags [256]bool
}

// vp9ComputeInterLeafZcoeffBlk reproduces libvpx block_rd_txfm's per-transform
// -block x->zcoeff_blk decision (vp9/encoder/vp9_rdopt.c:826-837) for the
// COMMITTED inter Y leaf at (miRow, miCol) and tx_size txSize. It is the
// commit-time replay of the same per-block rd1/rd2 the full-RD super_block_yrd
// producer (vp9FullRDInterYPlaneTxCandidate) already computes during the mode
// search:
//
//	rd1 = RDCOST(rdmult, rddiv, rate, dist)   // cost of coding the residual
//	rd2 = RDCOST(rdmult, rddiv, 0,    sse)    // cost of skipping it
//	zcoeff_blk[tx][block] = !eob || (sharpness == 0 && rd1 > rd2 && !lossless)
//
// libvpx commits zcoeff_blk into ctx during the search (vp9_rdopt.c:835, 4008)
// and replays it at encode time (update_state vp9_encodeframe.c:1846 →
// encode_block vp9_encodemb.c:580). govpx's writer instead recomputes it here
// from the SAME predictor the committed mode produced (already on the recon
// plane after predictVP9InterBlock), running the REGULAR quantizer
// (vp9_xform_quant) at the segment qindex exactly as block_rd_txfm. The
// predictor rect is snapshotted and restored so the writer's subsequent
// vp9_quantize_fp tokenize pass starts from the same predictor libvpx's
// encode_superblock rebuilds.
//
// Gated by the caller behind the deep full-RD use-partition flag; production
// (flag off) never calls it.
func (e *VP9Encoder) vp9ComputeInterLeafZcoeffBlk(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, txSize common.TxSize,
	segID uint8,
) (vp9InterZcoeffBlk, bool) {
	var out vp9InterZcoeffBlk
	if inter == nil || inter.dq == nil || inter.lossless {
		// lossless never zero-forces (the !xd->lossless guard); treat as no-op.
		return out, false
	}
	if e.opts.Sharpness != 0 {
		// libvpx's RD term `x->sharpness == 0` gates the rd1>rd2 force; with
		// sharpness != 0 the only zcoeff is !eob, which the FP-quant tokenize
		// already yields (eob==0 => no tokens), so the bitmap is a no-op.
		return out, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return out, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return out, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return out, false
	}
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
		bsize, pd, planeBsize)
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if maxEob > len(e.coefScratch) || bs*bs > len(e.residueScratch) {
		return out, false
	}
	if full4x4W*int(common.Num4x4BlocksHighLookup[planeBsize]) > len(out.flags) {
		return out, false
	}
	dequant := inter.dq.Y[segID]
	qindex := e.vp9SegmentQIndex(inter, segID)
	// rdmult mirrors the value the full-RD mode search committed the leaf with:
	// baseRdmult = e.rc.rdmult (the per-frame rd->RDMULT,
	// vp9_encoder_inter_modes.go:1799), falling back to
	// ComputeRDMultBasedOnQindex when rc.rdmult is unset. e.cbRdmult is reset to
	// 0 at the frame boundary (vp9_encoder_rd.go:141) so it cannot be used at
	// write time. TPL is off on the realtime cpu4 path, so no per-SB delta
	// applies. libvpx ref: x->rdmult feeding block_rd_txfm's RDCOST.
	rdmult := e.rc.rdmult
	if rdmult <= 0 {
		rdmult = encoder.ComputeRDMultBasedOnQindex(
			e.vp9EncoderModeDecisionQIndex(), encoder.RDFrameInter)
	}
	if rdmult <= 0 {
		rdmult = 1
	}
	// block_rd_txfm distortion domain (vp9_encodeframe.c:2041-2048); cpu4 forces
	// transform domain. The zcoeff rd1/rd2 must use the same domain block_rd_txfm
	// did when it set x->zcoeff_blk.
	useTxDomain := vp9InterUseDeepRDTxDomainDistortion &&
		e.vp9InterUseTransformDomainDistortion(inter, miRows, miCols, miRow, miCol,
			bsize)

	// Snapshot the predictor rect so the regular-quant inverse-add below can be
	// undone before the FP-quant tokenize pass (which re-adds the residual of
	// the surviving blocks).
	restoreW := full4x4W * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 || restoreW*restoreH > len(e.blockScratch) {
		return out, false
	}
	predictor := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(predictor[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	// Entropy contexts seed t_above/t_left exactly as vp9_get_entropy_contexts
	// (vp9_rdopt.c:872), the same seeding the producer uses.
	var aboveCtx [16]uint8
	var leftCtx [16]uint8
	aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
	leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
	if aboveLen <= len(aboveCtx) && leftLen <= len(leftCtx) &&
		len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if off := aboveOffsets[0]; off >= 0 && off+aboveLen <= len(pd.AboveContext) {
			copy(aboveCtx[:aboveLen], pd.AboveContext[off:off+aboveLen])
		}
		if off := leftOffsets[0]; off >= 0 && off+leftLen <= len(pd.LeftContext) {
			copy(leftCtx[:leftLen], pd.LeftContext[off:off+leftLen])
		}
	}

	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			coeffs := e.coefScratch[:maxEob]
			qcoeffs := e.qCoefScratch[:maxEob]
			for i := range coeffs {
				coeffs[i] = 0
				qcoeffs[i] = 0
			}
			initCtx := vp9dec.GetEntropyContextFull(txSize,
				aboveCtx[cc:cc+step], leftCtx[rr:rr+step])
			// Regular quantizer (vp9_xform_quant), segment qindex, inverse-add
			// into recon — exactly block_rd_txfm (vp9_rdopt.c:792-795).
			eob := e.prepareVP9InterTxResidueFullRD(inter, pd, txSize,
				miRow, miCol, rr, cc, dequant, qindex, initCtx, coeffs, qcoeffs)
			hasResidue := eob > 0

			// rd1/rd2 must consume the SAME distortion domain block_rd_txfm used
			// when it set zcoeff_blk. For cpu4 block_tx_domain==1 (transform
			// domain); otherwise pixel domain. Mirror the Y-producer choice so the
			// rd1>rd2 boundary matches libvpx (over-zeroing otherwise, e.g.
			// {0,1,1,0,1} frame-1 mi(7,5) blk0).
			var blockDist uint64
			var blockSSE uint64
			if useTxDomain && hasResidue {
				blockDist = encoder.TransformBlockError(e.txCoeffScratch[:maxEob],
					e.dqCoeffScratch[:maxEob], txSize)
				blockSSE = encoder.TransformBlockEnergy(e.txCoeffScratch[:maxEob],
					txSize)
			} else {
				bd, distOK := vp9FullRDInterTxBlockPixelSSE(src, srcStride,
					srcW, srcH, planeData, stride, baseX+cc*4, baseY+rr*4, bs)
				if !distOK {
					// Restore the predictor and bail; caller falls back to no-op.
					encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
						restoreW, restoreH, predictor)
					return vp9InterZcoeffBlk{}, false
				}
				blockDist = bd * 16
				blockSSE = encoder.ResidualSSE(e.residueScratch[:bs*bs]) * 16
			}
			// cost_coeffs MUST use the INITIAL (pre-compressed-header-update)
			// coef probs the full-RD search built its token cost tables from —
			// libvpx fills x->token_costs from cm->fc.coef_probs at frame start
			// (fill_token_costs in vp9_initialize_rd_consts) and the zcoeff
			// decision (block_rd_txfm) consumes those. The compressed-header
			// write later applies vp9_cond_prob_diff_update deltas into e.fc, so
			// reading e.fc here would use the post-delta probs and flip the
			// rd1>rd2 boundary on the write pass. inter.selectFc is the snapshot
			// of e.fc at frame start (vp9_encoder_frame.go:449), unchanged by the
			// header update, so it gives the search-time probs in BOTH the count
			// pre-pass and the write pass.
			blockRate := e.vp9InterCoeffBlockRateCostQFcWithCosts(&inter.selectFc,
				e.vp9CoeffTokenCostTable(txSize, 0, 1), txSize, 0,
				dequant, coeffs, qcoeffs, initCtx, eob, true)

			rd1 := encoder.RDCost(rdmult, encoder.RDDivBits, blockRate, blockDist)
			rd2 := encoder.RDCost(rdmult, encoder.RDDivBits, 0, blockSSE)
			// vp9_rdopt.c:835 — zcoeff_blk = !eob || (sharpness==0 && rd1>rd2 &&
			// !lossless). sharpness==0 and !lossless already gated above.
			zb := !hasResidue || rd1 > rd2
			idx := rr*full4x4W + cc
			if idx >= 0 && idx < len(out.flags) {
				out.flags[idx] = zb
			}

			hasCtx := uint8(0)
			if hasResidue {
				hasCtx = 1
			}
			for i := 0; i < step && cc+i < aboveLen; i++ {
				aboveCtx[cc+i] = hasCtx
			}
			for i := 0; i < step && rr+i < leftLen; i++ {
				leftCtx[rr+i] = hasCtx
			}
		}
	}

	encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
		restoreW, restoreH, predictor)
	out.valid = true
	return out, true
}

// vp9FullRDIntraUVFrameType selects which UV-mode probability family feeds
// intra_uv_mode_cost. libvpx indexes cpi->intra_uv_mode_cost by
// cpi->common.frame_type (KEY_FRAME=0 / INTER_FRAME=1); the KEY_FRAME slot is
// built from vp9_kf_uv_mode_prob and the INTER_FRAME slot from fc->uv_mode_prob
// (vp9/encoder/vp9_rd.c:104-108).
type vp9FullRDIntraUVFrameType int

const (
	vp9FullRDKeyFrame   vp9FullRDIntraUVFrameType = 0
	vp9FullRDInterFrame vp9FullRDIntraUVFrameType = 1
)

// vp9FullRDKeyframeYModeCosts fills costs[mode] with the keyframe Y-mode bit
// cost y_mode_costs[above][left], i.e.
//
//	vp9_cost_tokens(costs, vp9_kf_y_mode_prob[above][left], vp9_intra_mode_tree)
//
// This is the bmode_costs row that rd_pick_intra_sby_mode adds to the
// transform rate: this_rate = this_rate_tokenonly + bmode_costs[mode]
// (libvpx vp9/encoder/vp9_rdopt.c:1379, 1398; table built in vp9_rd.c:97-100).
//
// `above` / `left` are the neighbour intra modes from vp9_above_block_mode /
// vp9_left_block_mode (clamped to DC_PRED for inter / off-frame neighbours by
// the caller). costs must have room for common.IntraModes entries.
func vp9FullRDKeyframeYModeCosts(costs []int, above, left common.PredictionMode) {
	a := vp9FullRDClampIntraMode(above)
	l := vp9FullRDClampIntraMode(left)
	probs := tables.KfYModeProb[a][l]
	encoder.VP9CostTokens(costs, probs[:], common.IntraModeTree[:])
}

// vp9FullRDInterIntraYModeCosts fills costs[mode] with the inter-frame
// mbmode_cost used by the intra branch of vp9_rd_pick_inter_mode_sb:
//
//	vp9_cost_tokens(costs, fc->y_mode_prob[1], vp9_intra_mode_tree)
//
// (libvpx vp9/encoder/vp9_rd.c:103; consumed at vp9_rdopt.c:3864 as
// cpi->mbmode_cost[mi->mode]). The size-group index is the literal constant 1
// — NOT size_group_lookup[bsize]. costs must have room for common.IntraModes
// entries; fc is the frame context whose YModeProb mirrors fc->y_mode_prob
// (indexed [BLOCK_SIZE_GROUPS][9]).
func vp9FullRDInterIntraYModeCosts(costs []int, fc *vp9dec.FrameContext) {
	const mbmodeSizeGroup = 1 // libvpx vp9_rd.c:103 fc->y_mode_prob[1]
	row := fc.YModeProb[mbmodeSizeGroup]
	encoder.VP9CostTokens(costs, row[:], common.IntraModeTree[:])
}

// vp9FullRDIntraUVModeCosts fills costs[mode] with the UV intra bit cost
// intra_uv_mode_cost[frame_type][yMode][mode]:
//
//	KEY_FRAME:   vp9_cost_tokens(costs, vp9_kf_uv_mode_prob[yMode], tree)
//	INTER_FRAME: vp9_cost_tokens(costs, fc->uv_mode_prob[yMode], tree)
//
// (libvpx vp9/encoder/vp9_rd.c:104-108; consumed at vp9_rdopt.c:1496 inside
// rd_pick_intra_sbuv_mode as
// cpi->intra_uv_mode_cost[frame_type][xd->mi[0]->mode][mode]). costs must have
// room for common.IntraModes entries. `fc` carries fc->uv_mode_prob via its
// UvModeProb field; it is only consulted for the INTER_FRAME family.
func vp9FullRDIntraUVModeCosts(costs []int, frameType vp9FullRDIntraUVFrameType,
	yMode common.PredictionMode, fc *vp9dec.FrameContext,
) {
	y := vp9FullRDClampIntraMode(yMode)
	if frameType == vp9FullRDKeyFrame {
		probs := tables.KfUvModeProb[y]
		encoder.VP9CostTokens(costs, probs[:], common.IntraModeTree[:])
		return
	}
	row := fc.UvModeProb[y]
	encoder.VP9CostTokens(costs, row[:], common.IntraModeTree[:])
}

// vp9FullRDIntraModeRD expands libvpx's RDCOST(x->rdmult, x->rddiv, this_rate,
// this_distortion) for the intra-mode decision, where this_rate is the sum of
// the transform token-only rate and the intra-mode bit cost:
//
//	this_rate = this_rate_tokenonly + bmode_costs[mode];      // Y picker
//	this_rd   = RDCOST(x->rdmult, x->rddiv, this_rate, this_distortion);
//
// (libvpx vp9/encoder/vp9_rdopt.c:1398-1399 for the keyframe Y picker;
// vp9_rdopt.c:1494-1495 for the UV picker; the inter-frame intra branch builds
// rate2 = rate_y + cpi->mbmode_cost[mi->mode] + rate_uv_intra[uv_tx] at
// vp9_rdopt.c:3864 then scores it through the same RDCOST). rddiv is the
// libvpx constant RD_DIV_BITS (encoder.RDDivBits = 7).
//
// modeCost is bmode_costs[mode] (or mbmode_cost[mode]); tokenRate is the
// transform-domain rate (rate_tokenonly); distortion is the transform-domain
// distortion. Returns (rate, rdcost) so the caller can both track the winning
// rate and compare RD values, exactly as the libvpx pickers do.
func vp9FullRDIntraModeRD(rdmult, modeCost, tokenRate int, distortion uint64) (rate int, rd uint64) {
	rate = tokenRate + modeCost
	rd = encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion)
	return rate, rd
}

// vp9FullRDClampIntraMode mirrors the implicit invariant that the intra
// rate-cost tables are indexed by an intra prediction mode in
// [DC_PRED, TM_PRED]. libvpx never calls these with an out-of-range mode (the
// neighbour-mode helpers clamp inter/off-frame neighbours to DC_PRED); this
// guard keeps the Go table indexing panic-free for defensive callers and tests.
func vp9FullRDClampIntraMode(mode common.PredictionMode) common.PredictionMode {
	if mode < common.DcPred || int(mode) >= common.IntraModes {
		return common.DcPred
	}
	return mode
}

// vp9_fullrd_partition_cost.go ports the partition-type RATE COST and the
// PARTITION_NONE/HORZ/VERT/SPLIT RDCOST comparison used by libvpx's
// full-RD square-partition recursion (rd_pick_partition in
// vp9/encoder/vp9_encodeframe.c). It is kept in its own file so the
// shared full-RD inter/key partition pickers are not edited.
//
// KEY LIBVPX FACT (the divergence this file fixes):
//
// The RD search accumulates the partition-token rate via the precomputed
// table cpi->partition_cost[pl][PARTITION_X], built once per frame in
// vp9/encoder/vp9_rd.c:430-432:
//
//	for (i = 0; i < PARTITION_CONTEXTS; ++i)
//	  vp9_cost_tokens(cpi->partition_cost[i], get_partition_probs(xd, i),
//	                  vp9_partition_tree);
//
// vp9_cost_tokens walks the FULL 4-leaf vp9_partition_tree
// (vp9/common/vp9_entropymode.c:262):
//
//	{ -PARTITION_NONE, 2, -PARTITION_HORZ, 4, -PARTITION_VERT,
//	  -PARTITION_SPLIT }
//
// so partition_cost[pl][type] is the UNCONDITIONAL tree cost, indexed only
// by (pl, partition type). rd_pick_partition adds it verbatim at
// vp9_encodeframe.c:3826 (NONE), :3969 (SPLIT), :4035 (HORZ), :4085
// (VERT) regardless of frame-edge conditions.
//
// The hasRows/hasCols-clamped form (one or two tree bits) is a property of
// the BITSTREAM WRITER (write_partition in vp9_bitstream.c), NOT of the RD
// rate cost. Mixing the writer's clamped form into the RD accumulation
// understates the rate of HORZ/VERT/SPLIT at frame edges and changes the
// PARTITION_NONE vs split RDCOST comparison.

// RDPartitionCost returns the full-RD partition-token rate cost for the
// supplied partition-plane context and partition type, mirroring
// cpi->partition_cost[ctx][partition].
//
// It is the verbatim vp9_cost_tokens(vp9_partition_tree) cost: it does NOT
// clamp on frame-edge (hasRows/hasCols) conditions. Use this — not the
// writer's PartitionRateCost — inside the full-RD partition RDCOST
// accumulation.
//
// libvpx: vp9/encoder/vp9_rd.c:430-432 (table build),
// vp9/encoder/vp9_cost.c:56 (vp9_cost_tokens),
// vp9/common/vp9_entropymode.c:262 (vp9_partition_tree).
func RDPartitionCost(
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	ctx int, partition common.PartitionType,
) int {
	if partitionProbs == nil || ctx < 0 || ctx >= common.PartitionContexts {
		return 0
	}
	if partition < common.PartitionNone || partition >= common.PartitionTypes {
		return 0
	}
	var costs [common.PartitionTypes]int
	probs := partitionProbs[ctx]
	encoder.VP9CostTokens(costs[:], probs[:], common.PartitionTree[:])
	return costs[partition]
}

// VP9RDCost expands libvpx's RDCOST macro (vp9/encoder/vp9_rd.h:29-30):
//
//	RDCOST(RM, DM, R, D) =
//	  ROUND_POWER_OF_TWO((int64_t)R * RM, VP9_PROB_COST_SHIFT) + (D << DM)
//
// with DM == RDDIV_BITS == 7 and VP9_PROB_COST_SHIFT == 9. Provided so the
// full-RD partition comparison can be pinned against libvpx values without
// reaching into the shared encoder helpers.
func VP9RDCost(rdmult int, rate int, distortion uint64) uint64 {
	return encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion)
}

// vp9RDPartitionBetter reports whether candidate RDCOST `cand` should
// replace the running `best`, mirroring rd_pick_partition's
// "this_rdc.rdcost < best_rdc.rdcost" / "sum_rdc.rdcost < best_rdc.rdcost"
// strict-less comparison (vp9_encodeframe.c:3829, :3973). The first
// candidate (bestSet == false) always wins.
//
// libvpx breaks ties by keeping the earlier (PARTITION_NONE-first) winner,
// i.e. it does NOT replace best on equal rdcost.
func vp9RDPartitionBetter(bestSet bool, best, cand uint64) bool {
	if !bestSet {
		return true
	}
	return cand < best
}
