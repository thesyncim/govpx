package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_intra_sb.go ports the GENUINE larger-block (bsize >=
// BLOCK_8X8) INTRA rate-distortion search that libvpx runs inside the intra
// branch of vp9_rd_pick_inter_mode_sb (vp9/encoder/vp9_rdopt.c:3781-3867), as a
// standalone, verified producer.
//
// For an inter frame, when a non-split 8x8/16x16/32x32/64x64 partition reaches
// the ref_frame == INTRA_FRAME arm, libvpx (vp9_rdopt.c, verbatim):
//
//   - loops this_mode over DC_PRED..TM_PRED (the outer for-loop at
//     vp9_rdopt.c:3735 over the mode-list, INTRA_FRAME candidates first);
//   - for each Y mode runs super_block_yrd (vp9_rdopt.c:3839) — the SAME
//     transform-RD machinery as the inter path: choose_tx_size_from_rd
//     (vp9_rdopt.c:1039) over the block's tx-size range, each candidate
//     txfm_rd_in_plane → block_rd_txfm (vp9_rdopt.c:736-743) which for an intra
//     block calls vp9_encode_block_intra (vp9/encoder/vp9_encodemb.c:796): per
//     transform unit it predicts (vp9_predict_intra_block) from the
//     reconstructed neighbours, subtracts the source, forward-transforms,
//     quantizes with the REGULAR quantizer (vpx_quantize_b, NOT the fast fp
//     quantizer — sf->use_quant_fp == 0 on the full-RD path,
//     vp9_speed_features.c:954), runs the vp9_optimize_b trellis
//     (vp9_encodemb.c:863-867, do_trellis_opt → ENABLE_TRELLIS_OPT for the RT
//     full-RD mode-selection path), and inverse-adds into the SAME recon buffer
//     so later transform units predict from the freshly reconstructed samples;
//   - derives uv_tx = uv_txsize_lookup[bsize][mi->tx_size][...]
//     (vp9_rdopt.c:3846) from the Y tx-size super_block_yrd just chose;
//   - picks the chroma mode via choose_intra_uv_mode (vp9_rdopt.c:1531) →
//     rd_pick_intra_sbuv_mode (vp9_rdopt.c:1468): loops uv_mode DC_PRED..TM_PRED,
//     each running super_block_uvrd (vp9_rdopt.c:1491) over the U+V planes at
//     uv_tx, scoring this_rd = RDCOST(rate_uv_tokenonly + intra_uv_mode_cost,
//     dist_uv) and keeping the min-RD chroma mode (vp9_rdopt.c:1494-1507);
//   - costs the Y mode with the INTER-FRAME mbmode cost
//     cpi->mbmode_cost[mi->mode] = cost_tokens(fc->y_mode_prob[1])
//     (vp9_rdopt.c:3864; table vp9_rd.c:103 — the LITERAL size-group index 1,
//     NOT size_group_lookup[bsize]; reused here via
//     vp9FullRDInterIntraYModeCosts, the prior govpx fix in
//     pickVP9InterIntraModeCore / vp9_fullrd_intra.go);
//   - costs the chroma mode with intra_uv_mode_cost[INTER_FRAME][y_mode][uv_mode]
//     = cost_tokens(fc->uv_mode_prob[y_mode]) (vp9_rdopt.c:1496, folded into
//     rate_uv_intra; reused via vp9FullRDIntraUVModeCosts);
//   - forms rate2 = rate_y + mbmode_cost[mode] + rate_uv_intra[uv_tx]
//     (vp9_rdopt.c:3864), distortion2 = distortion_y + distortion_uv
//     (vp9_rdopt.c:3867), and (the caller's final RD) this_rd = RDCOST(x->rdmult,
//     x->rddiv, rate2, distortion2) (vp9_rdopt.c:3929, with the !disable_skip
//     skip-flag bit added — for the chosen no-skip intra case rate2 += skip0).
//
// This producer reproduces the per-Y-mode {Y-RD, uv_mode, rate_y, dist_y,
// rate_uv, dist_uv, mbmode_cost+uv_mode_cost, rd} byte-exactly and returns the
// min-RD intra mode. It reuses the existing exported full-RD primitives WITHOUT
// editing them: the choose_tx_size_from_rd selector (encoder.FullRDChooseTxSize),
// the trellis (encoder.VP9OptimizeB), the intra cost-coeffs walkers
// (vp9KeyframeCoeffBlockRateCostQ / vp9KeyframeUvCoeffBlockRateCostQ, is_inter=0),
// the per-tx-block intra prediction + residue (predictVP9KeyframeTx /
// gatherVP9TxResidual / quantizeVP9TxResidualWithQTrellis), and the intra-mode
// rate tables (vp9FullRDInterIntraYModeCosts / vp9FullRDIntraUVModeCosts). The
// per-tx-block intra-prediction + transform-RD inner loop is REPLICATED here
// (rather than editing the keyframe scorer) because the inter-frame intra branch
// runs the trellis + entropy-context threading + best_rd early-exit that the
// keyframe model scorer omits.
//
// It is NOT wired into the production mode loop (the parent wires it later); the
// flag-off path stays byte-identical.

// vp9FullRDInterIntraYModeOrder is the INTRA_FRAME subsequence of libvpx's
// vp9_mode_order[MAX_MODES] (vp9/encoder/vp9_rdopt.c:91-131), in the exact order
// rd_pick_inter_mode_sb visits Y intra modes: DC_PRED (index 3), TM_PRED (16),
// H_PRED (23), V_PRED (24), D135_PRED (25), D207_PRED (26), D153_PRED (27),
// D63_PRED (28), D117_PRED (29), D45_PRED (30). The producer iterates this
// sequence so the per-uv_tx choose_intra_uv_mode cache and the RD tie-breaking
// resolve to the same Y mode libvpx's loop would.
var vp9FullRDInterIntraYModeOrder = [common.IntraModes]common.PredictionMode{
	common.DcPred,
	common.TmPred,
	common.HPred,
	common.VPred,
	common.D135Pred,
	common.D207Pred,
	common.D153Pred,
	common.D63Pred,
	common.D117Pred,
	common.D45Pred,
}

// vp9FullRDInterIntraSBResult is the committed larger-block intra decision the
// intra branch of vp9_rd_pick_inter_mode_sb forms for an inter frame.
//
//   - YMode / UvMode: mi->mode and mi->uv_mode (vp9_rdopt.c:3813,3862).
//   - RateY / DistY: super_block_yrd's rate_y (the tokenonly transform rate at
//     the chosen tx_size) and distortion_y (vp9_rdopt.c:3839).
//   - RateUV / DistUV: rate_uv_tokenonly[uv_tx] and dist_uv[uv_tx] — the chroma
//     TOKENONLY rate (NO uv-mode bits) and distortion (vp9_rdopt.c:3859-3860).
//   - ModeCost: mbmode_cost[mi->mode] + intra_uv_mode_cost (the full mode
//     signalling: Y mbmode bits + chroma uv_mode bits). rate2's mode component
//     (vp9_rdopt.c:3864) is rate_y + ModeCost + rate_uv_tokenonly.
//   - IntraCostPenalty: the per-frame intra penalty rate2 adds for oblique modes
//     (this_mode != DC_PRED && this_mode != TM_PRED), vp9_rdopt.c:3865-3866 =
//     vp9_get_intra_cost_penalty(bsize, base_qindex, y_dc_delta_q) (vp9_rd.c:778).
//     0 for DC_PRED / TM_PRED.
//   - Rate2 / Distortion2: rate2 / distortion2 (vp9_rdopt.c:3864-3867), the
//     coefficient+mode rate (incl. IntraCostPenalty) and distortion BEFORE the
//     ref/skip bits the caller adds (this branch has no comp/ref cost for
//     INTRA_FRAME beyond ref_costs_single[INTRA_FRAME], added by the caller at
//     :3893).
//   - RD: the intra-mode RDCOST = RDCOST(x->rdmult, x->rddiv, Rate2, Distortion2)
//     — the value the per-Y-mode / per-chroma-mode search minimises. (The
//     caller's final this_rd additionally folds ref_costs_single[INTRA_FRAME] +
//     the skip-flag bit; this producer reports the pre-ref/pre-skip-bit RD —
//     identical to the value libvpx computes at vp9_rdopt.c:3929 for the chosen
//     no-skip intra mode minus the skip0 bit, which does not reorder the intra
//     mode selection.)
//   - TxSize / UvTxSize: mi->tx_size (super_block_yrd) and uv_tx
//     (uv_txsize_lookup).
//   - Skippable: skippable_y && skip_uv (vp9_rdopt.c:3861).
type vp9FullRDInterIntraSBResult struct {
	YMode            common.PredictionMode
	UvMode           common.PredictionMode
	TxSize           common.TxSize
	UvTxSize         common.TxSize
	RateY            int
	DistY            uint64
	RateUV           int
	DistUV           uint64
	ModeCost         int
	IntraCostPenalty int
	Rate2            int
	Distortion2      uint64
	RD               uint64
	Skippable        bool
	Valid            bool
}

// vp9FullRDInterIntraSB runs the genuine larger-block intra RD search for an
// inter-frame block at bsize in {8x8,16x16,32x32,64x64}. It returns the min-RD
// intra {y_mode, uv_mode, rate, distortion, rd}.
//
// inter carries the live frame context the picker is using (inter.selectFc ==
// the frame's entropy/coef probs, inter.dq == the dequant tables, inter.img ==
// the source). rdmult is x->rdmult (e.cbRdmult on the live path); rddiv is the
// libvpx constant RD_DIV_BITS (encoder.RDDivBits). refBestRD is best_rd, the
// running budget super_block_yrd's early-exit consumes (^uint64(0) when the
// picker has no budget yet).
func (e *VP9Encoder) vp9FullRDInterIntraSB(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, rdmult int, refBestRD uint64,
) (vp9FullRDInterIntraSBResult, bool) {
	if inter == nil || inter.dq == nil || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return vp9FullRDInterIntraSBResult{}, false
	}
	// A keyframe-like state wraps the source + dequant + header so the
	// per-tx-block intra prediction / residue helpers (which were written for
	// the keyframe path) can run against the inter frame's source and the
	// recon planes the inter picker has been filling. The qindex/dequant come
	// from the inter state (segment 0 base qindex), exactly as the inter Y/UV
	// producers read them.
	keyLike := e.vp9InterIntraKeyframeState(inter)
	if keyLike.hdr == nil || keyLike.img == nil || keyLike.dq == nil {
		return vp9FullRDInterIntraSBResult{}, false
	}

	// cpi->mbmode_cost[mi->mode] = cost_tokens(fc->y_mode_prob[1])
	// (vp9_rdopt.c:3864; table vp9_rd.c:103). The literal size-group index 1.
	var yModeCost [common.IntraModes]int
	vp9FullRDInterIntraYModeCosts(yModeCost[:], &inter.selectFc)

	// libvpx restricts the intra Y-mode set via the speed feature
	// mode_skip_mask[INTRA_FRAME] |= ~intra_y_mode_mask[max_txsize_lookup[bsize]]
	// (vp9_rdopt.c:3623-3624): a Y mode whose bit is clear in the mask is masked
	// out of the mode loop (the `mode_skip_mask[ref_frame] & (1<<this_mode)`
	// continue at vp9_rdopt.c:3698) and never reaches the intra arm. For cpu4
	// realtime this is INTRA_DC_H_V at every tx size except TX_32X32 (INTRA_DC)
	// (vp9_speed_features.c:563-567); cpu0 leaves it at INTRA_ALL. Mirror the
	// gate here so the producer evaluates exactly the Y modes libvpx does.
	maxTxSize := common.MaxTxsizeLookup[bsize]
	yModeMask := sfIntraAll
	if int(maxTxSize) < len(e.sf.IntraYModeMask) && e.sf.IntraYModeMask[maxTxSize] != 0 {
		yModeMask = e.sf.IntraYModeMask[maxTxSize]
	}

	// intra_cost_penalty = vp9_get_intra_cost_penalty(bsize, base_qindex,
	// y_dc_delta_q) (vp9_rdopt.c:3487-3488, applied at :3865-3866 for oblique
	// modes). qdelta is cm->y_dc_delta_q; for this realtime path the segment
	// qindex feeds vp9_dc_quant exactly as the nonrd intra fallback
	// (vp9_pick_inter_mode_nonrd_intra.go:114).
	segQIndex := vp9dec.GetSegmentQindex(&keyLike.hdr.Seg,
		vp9EncoderMiSegmentID(nil), int(keyLike.hdr.Quant.BaseQindex))
	intraCostPenalty := encoder.IntraCostPenalty(segQIndex,
		int(keyLike.hdr.Quant.YDcDeltaQ), bsize,
		e.noiseEstimate.Enabled, e.noiseEstimate.ExtractLevel())

	// rate_uv_intra[uv_tx] / rate_uv_tokenonly[uv_tx] / dist_uv[uv_tx] /
	// skip_uv[uv_tx] / mode_uv[uv_tx] are memoised per uv_tx in libvpx
	// (vp9_rdopt.c:3480-3483,3529) so choose_intra_uv_mode runs at most once per
	// uv_tx across the whole intra-mode loop. The producer mirrors that with a
	// per-uv_tx cache keyed by TX_SIZE.
	var uvCache [common.TxSizes]vp9FullRDInterIntraUVChoice
	var uvCacheValid [common.TxSizes]bool

	best := vp9FullRDInterIntraSBResult{}
	bestSet := false
	bestRD := refBestRD

	// Iterate the Y modes in libvpx's vp9_mode_order INTRA_FRAME sequence
	// (vp9_rdopt.c:91-131), not numeric order. The order is load-bearing for
	// two reasons that both reduce to "the first mode_order entry wins": (1) the
	// per-uv_tx choose_intra_uv_mode cache (vp9_rdopt.c:3851) is populated by the
	// FIRST Y mode reaching a given uv_tx, and intra_uv_mode_cost is keyed on
	// THAT Y mode (xd->mi[0]->mode at populate time); (2) RD ties between modes
	// resolve to the earlier mode_order entry (this_rd < best_rd is strict, so
	// the first-seen of equal-RD modes survives). Walking numeric order would
	// diverge on both for blocks whose winner is not DC.
	for _, mode := range vp9FullRDInterIntraYModeOrder {
		// intra_y_mode_mask gate (vp9_rdopt.c:3623-3624 + :3698): a Y mode whose
		// bit is clear is masked out of the loop and never evaluated.
		if yModeMask&(1<<uint(mode)) == 0 {
			continue
		}
		// super_block_yrd on the intra residual for this Y mode
		// (vp9_rdopt.c:3839). best_rd tightens to the running min RD so the
		// transform-RD early-exit fires as libvpx's does.
		yRD, ok := e.vp9FullRDInterIntraSuperBlockYRD(inter, &keyLike, tile,
			miRows, miCols, miRow, miCol, bsize, mode, rdmult, bestRD)
		if !ok || !yRD.Valid {
			// rate_y == INT_MAX -> continue (vp9_rdopt.c:3844).
			continue
		}

		// uv_tx = uv_txsize_lookup[bsize][mi->tx_size][...] (vp9_rdopt.c:3846).
		uvTx := vp9dec.GetUvTxSize(bsize, yRD.TxSize, &e.planes[1])
		if uvTx >= common.TxSizes {
			continue
		}

		// choose_intra_uv_mode (vp9_rdopt.c:3851-3855), memoised per uv_tx.
		if !uvCacheValid[uvTx] {
			choice, uvOK := e.vp9FullRDInterIntraChooseUVMode(inter, &keyLike,
				tile, miRows, miCols, miRow, miCol, bsize, mode, uvTx, rdmult)
			if !uvOK {
				continue
			}
			uvCache[uvTx] = choice
			uvCacheValid[uvTx] = true
		}
		uv := uvCache[uvTx]
		if !uv.Valid {
			continue
		}

		// rate_uv = rate_uv_tokenonly[uv_tx]; distortion_uv = dist_uv[uv_tx];
		// skippable = skippable && skip_uv[uv_tx] (vp9_rdopt.c:3859-3861).
		rateUV := uv.RateTokenOnly
		distUV := uv.Dist
		skippable := yRD.Skippable && uv.Skippable

		// rate2 = rate_y + mbmode_cost[mode] + rate_uv_intra[uv_tx]
		// (vp9_rdopt.c:3864). rate_uv_intra is the FULL chroma rate (tokenonly +
		// uv_mode bits); mbmode_cost is the Y mbmode bits.
		modeCost := yModeCost[mode] + uv.ModeCost
		rate2 := yRD.Rate + modeCost + rateUV
		// intra_cost_penalty for oblique modes (vp9_rdopt.c:3865-3866).
		penalty := 0
		if mode != common.DcPred && mode != common.TmPred {
			penalty = intraCostPenalty
			rate2 += penalty
		}
		dist2 := yRD.Distortion + distUV

		// this_rd = RDCOST(x->rdmult, x->rddiv, rate2, distortion2)
		// (vp9_rdopt.c:3929). This is the intra-mode RD the per-mode search
		// minimises (the ref/skip-flag bits the caller folds in afterwards do not
		// reorder the intra Y/chroma-mode selection, which is driven by this RD).
		rd := encoder.RDCost(rdmult, encoder.RDDivBits, rate2, dist2)

		cand := vp9FullRDInterIntraSBResult{
			YMode:            mode,
			UvMode:           uv.Mode,
			TxSize:           yRD.TxSize,
			UvTxSize:         uvTx,
			RateY:            yRD.Rate,
			DistY:            yRD.Distortion,
			RateUV:           rateUV,
			DistUV:           distUV,
			ModeCost:         modeCost,
			IntraCostPenalty: penalty,
			Rate2:            rate2,
			Distortion2:      dist2,
			RD:               rd,
			Skippable:        skippable,
			Valid:            true,
		}
		if !bestSet || cand.RD < best.RD {
			best = cand
			bestSet = true
			if rd < bestRD {
				bestRD = rd
			}
		}
	}
	return best, bestSet
}

// vp9FullRDInterIntraUVChoice is the per-uv_tx output of choose_intra_uv_mode
// (rd_pick_intra_sbuv_mode), memoised by the intra-mode loop.
//
//   - Mode: x->e_mbd.mi[0]->uv_mode = mode_selected (vp9_rdopt.c:1510).
//   - RateTokenOnly: *rate_tokenonly (the chroma transform tokenonly rate,
//     vp9_rdopt.c:1503) — what handle_inter_mode assigns to rate_uv.
//   - RateFull: *rate = rate_tokenonly + intra_uv_mode_cost (vp9_rdopt.c:1502)
//     — rate_uv_intra[uv_tx].
//   - ModeCost: RateFull - RateTokenOnly = intra_uv_mode_cost[INTER][y][uv].
//   - Dist / Skippable: *distortion / *skippable (vp9_rdopt.c:1504-1505).
type vp9FullRDInterIntraUVChoice struct {
	Mode          common.PredictionMode
	RateTokenOnly int
	RateFull      int
	ModeCost      int
	Dist          uint64
	Skippable     bool
	Valid         bool
}

// vp9FullRDInterIntraChooseUVMode ports rd_pick_intra_sbuv_mode
// (vp9/encoder/vp9_rdopt.c:1468-1512) for the inter-frame intra branch: it loops
// uv_mode DC_PRED..TM_PRED, runs super_block_uvrd at uv_tx for each, scores
// this_rd = RDCOST(rate_uv_tokenonly + intra_uv_mode_cost, dist_uv), and returns
// the min-RD chroma mode. yMode is mi->mode (the Y mode), used to select the
// intra_uv_mode_cost row (cost_tokens(fc->uv_mode_prob[y_mode])).
func (e *VP9Encoder) vp9FullRDInterIntraChooseUVMode(inter *vp9InterEncodeState,
	keyLike *vp9KeyframeEncodeState, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	yMode common.PredictionMode, uvTx common.TxSize, rdmult int,
) (vp9FullRDInterIntraUVChoice, bool) {
	// intra_uv_mode_cost[INTER_FRAME][y_mode][uv_mode] = cost_tokens(
	// fc->uv_mode_prob[y_mode]) (vp9_rdopt.c:1496; table vp9_rd.c:107-108).
	var uvModeCost [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(uvModeCost[:], vp9FullRDInterFrame, yMode,
		&inter.selectFc)

	// intra_uv_mode_mask gate (rd_pick_intra_sbuv_mode, vp9_rdopt.c:1481): a UV
	// mode whose bit is clear in intra_uv_mode_mask[uv_tx] is skipped. For cpu4
	// realtime this is INTRA_DC at every uv_tx (vp9_speed_features.c:565), so the
	// chroma search collapses to DC_PRED exactly like the use_uv_intra_rd_estimate
	// rd_sbuv_dcpred path (vp9_rdopt.c:1515-1531, which super_block_uvrd's DC at
	// INT64_MAX — identical here since DC is the first mode and is seeded with the
	// INT64_MAX best_rd). cpu0 leaves it at INTRA_ALL.
	uvMask := sfIntraAll
	if int(uvTx) < len(e.sf.IntraUvModeMask) && e.sf.IntraUvModeMask[uvTx] != 0 {
		uvMask = e.sf.IntraUvModeMask[uvTx]
	}

	best := vp9FullRDInterIntraUVChoice{}
	bestSet := false
	bestRD := ^uint64(0) // best_rd = INT64_MAX (vp9_rdopt.c:1476)

	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if uvMask&(1<<uint(mode)) == 0 {
			continue
		}
		// super_block_uvrd over U+V at uv_tx (vp9_rdopt.c:1491). The tokenonly
		// transform rate + distortion + skippable, with the best_rd early-exit.
		tokenRate, dist, skippable, ok := e.vp9FullRDInterIntraUVRD(inter,
			keyLike, tile, miRows, miCols, miRow, miCol, bsize, mode, uvTx,
			rdmult, bestRD)
		if !ok {
			// super_block_uvrd returned 0 (is_cost_valid == 0) -> continue.
			continue
		}
		// this_rate = this_rate_tokenonly + intra_uv_mode_cost (vp9_rdopt.c:1494).
		thisRate := tokenRate + uvModeCost[mode]
		// this_rd = RDCOST(x->rdmult, x->rddiv, this_rate, this_distortion).
		thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, thisRate, dist)
		if !bestSet || thisRD < bestRD {
			bestSet = true
			bestRD = thisRD
			best = vp9FullRDInterIntraUVChoice{
				Mode:          mode,
				RateTokenOnly: tokenRate,
				RateFull:      thisRate,
				ModeCost:      uvModeCost[mode],
				Dist:          dist,
				Skippable:     skippable,
				Valid:         true,
			}
		}
	}
	return best, bestSet
}

// vp9FullRDInterIntraUVRD ports super_block_uvrd (vp9/encoder/vp9_rdopt.c:1420)
// for an INTRA block at the given uv_tx: it accumulates the per-plane
// txfm_rd_in_plane (rate_tokenonly, distortion, skippable) over the U and V
// planes, with the is_cost_valid (INT_MAX -> invalid) short-circuit. Because the
// block is intra, libvpx does NOT vp9_subtract_plane up front (vp9_rdopt.c:1438
// is gated on is_inter_block); the chroma intra prediction happens per
// transform unit inside block_rd_txfm (vp9_encode_block_intra). The producer
// mirrors that via the per-tx-block intra residue helper.
func (e *VP9Encoder) vp9FullRDInterIntraUVRD(inter *vp9InterEncodeState,
	keyLike *vp9KeyframeEncodeState, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	uvMode common.PredictionMode, uvTx common.TxSize, rdmult int, refBestRD uint64,
) (rate int, distortion uint64, skippable bool, ok bool) {
	skippable = true
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		c := e.vp9FullRDInterIntraPlaneTxCandidate(inter, keyLike, tile,
			miRows, miCols, miRow, miCol, bsize, plane, uvMode, uvTx, rdmult,
			refBestRD)
		if !c.Valid {
			// pnrate == INT_MAX -> is_cost_valid = 0 (vp9_rdopt.c:1450).
			return 0, 0, false, false
		}
		rate += c.Rate
		distortion += c.Dist
		skippable = skippable && c.Skip
	}
	return rate, distortion, skippable, true
}

// vp9FullRDInterIntraSuperBlockYRD ports super_block_yrd
// (vp9/encoder/vp9_rdopt.c:1025) → choose_tx_size_from_rd (vp9_rdopt.c:907) for
// the INTRA Y plane of an inter-frame block at the given Y mode. It is the intra
// sibling of vp9FullRDInterSuperBlockYRDForMi: same choose_tx_size_from_rd
// machinery (per-tx-size candidate production, best_rd tightening, the verbatim
// FullRDChooseTxSize selector) but each per-tx-size candidate predicts intra
// per transform unit instead of reusing a single inter predictor.
func (e *VP9Encoder) vp9FullRDInterIntraSuperBlockYRD(inter *vp9InterEncodeState,
	keyLike *vp9KeyframeEncodeState, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, rdmult int, refBestRD uint64,
) (vp9FullRDInterYRDResult, bool) {
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return vp9FullRDInterYRDResult{}, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	if len(planeData) == 0 || stride <= 0 {
		return vp9FullRDInterYRDResult{}, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return vp9FullRDInterYRDResult{}, false
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 || restoreW*restoreH > len(e.blockScratch) {
		return vp9FullRDInterYRDResult{}, false
	}
	// Snapshot the SB Y-plane recon so each tx-size candidate runs on a pristine
	// baseline (the inverse-add writes the reconstruction into recon during a
	// pass). libvpx does the same via recon_buf[n] in choose_tx_size_from_rd
	// (vp9_rdopt.c:929-940).
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	maxTx := common.MaxTxsizeLookup[bsize]
	// choose_tx_size_from_rd start/end (vp9_rdopt.c:946-955), TX_MODE_SELECT
	// branch. inter.txMode is the frame's cm->tx_mode.
	startTx := int(maxTx)
	endTx := startTx
	if inter.txMode == common.TxModeSelect && !inter.lossless {
		endTx = max(startTx-e.sf.TxSizeSearchDepth, 0)
		if bsize > common.Block32x32 {
			endTx = min(endTx+1, startTx)
		}
	} else {
		chosen := maxTx
		if inter.txMode < common.TxModes {
			chosen = min(common.TxModeToBiggestTxSize[inter.txMode], maxTx)
		}
		if inter.lossless {
			chosen = common.Tx4x4
		}
		startTx = int(chosen)
		endTx = int(chosen)
	}
	if startTx < endTx || startTx >= int(common.TxSizes) || endTx < 0 {
		encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, saved)
		return vp9FullRDInterYRDResult{}, false
	}

	// Neighbours for skip / tx-size signalling context.
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	s0 := encoder.VP9CostBit(skipProb, 0)
	s1 := encoder.VP9CostBit(skipProb, 1)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	txSizeCostRow := encoder.FullRDTxSizeCostRow(txProbs, maxTx)

	var cand [common.TxSizes]encoder.FullRDTxCandidate
	bestRD := refBestRD
	for n := startTx; n >= endTx; n-- {
		tx := common.TxSize(n)
		// Restore the predictor baseline before this tx size's pass.
		encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, saved)
		c := e.vp9FullRDInterIntraPlaneTxCandidate(inter, keyLike, tile,
			miRows, miCols, miRow, miCol, bsize, 0 /*plane Y*/, mode, tx,
			rdmult, bestRD)
		cand[n] = c
		if c.Valid {
			if rd1 := vp9FullRDInterIntraRD1(cand, txSizeCostRow, n, rdmult,
				s0, s1); rd1 < bestRD {
				bestRD = rd1
			}
		}
	}
	// Restore the baseline so the caller's recon state is unperturbed.
	encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
		restoreW, restoreH, saved)

	res := encoder.FullRDChooseTxSize(cand, txSizeCostRow, maxTx,
		startTx, endTx, rdmult, encoder.RDDivBits, s0, s1,
		false /*isInter — intra*/, inter.lossless,
		e.sf.TxSizeSearchBreakout != 0,
		inter.txMode == common.TxModeSelect, refBestRD)

	// libvpx super_block_yrd returns *rate = r[best_tx][...] which is INT_MAX
	// when the selected tx candidate early-exited (all candidates exceeded
	// best_rd). The intra branch then does `if (rate_y == INT_MAX) continue;`
	// (vp9_rdopt.c:3844). Map that to !ok so the caller skips the mode.
	if res.TxSize < 0 || int(res.TxSize) >= int(common.TxSizes) ||
		!cand[res.TxSize].Valid {
		return vp9FullRDInterYRDResult{}, false
	}

	return vp9FullRDInterYRDResult{
		TxSize:     res.TxSize,
		Rate:       res.Rate,
		Distortion: res.Dist,
		Skippable:  res.Skip,
		SSE:        res.SSE,
		BestRD:     res.BestRDCost,
		Valid:      true,
		Cand:       cand,
		Start:      startTx,
		End:        endTx,
	}, true
}

// vp9FullRDInterIntraPlaneTxCandidate is the per-tx-size txfm_rd_in_plane
// (vp9_rdopt.c:854-889) → block_rd_txfm (vp9_rdopt.c:699-852) producer for one
// INTRA plane (Y or chroma) at txSize, mirroring the inter producer
// vp9FullRDInterYPlaneTxCandidate / vp9FullRDInterUVPlaneTxCandidate but for the
// intra (!is_inter_block) branch of block_rd_txfm (vp9_rdopt.c:736-768):
//
//   - vp9_encode_block_intra (vp9_encodemb.c:796): predict the transform unit
//     from reconstructed neighbours, subtract source, forward-transform,
//     quantize (regular vpx_quantize_b), run the vp9_optimize_b trellis, and
//     inverse-add into the recon buffer in place (so later units predict from
//     the reconstructed samples — vp9_rdopt.c does NOT restore the recon between
//     transform units within one tx-size pass).
//   - dist = pixel_sse(src, recon) * 16 (vp9_rdopt.c:766-768).
//   - sse = sum_squares(src_diff) * 16 (vp9_rdopt.c:757-765).
//   - rate += cost_coeffs (vp9_rdopt.c:826), is_inter=0 token model.
//   - the two block_rd_txfm early-exits on the accumulated this_rd
//     (vp9_rdopt.c:820-824,846-849); Valid=false maps txfm_rd_in_plane's
//     exit_early -> rate=INT_MAX (vp9_rdopt.c:878-883).
//
// entropy-context threading (t_above[c]/t_left[r] = !!eob) feeds the per-block
// coeff_ctx exactly as block_rd_txfm (vp9_rdopt.c:709-710,827-828).
func (e *VP9Encoder) vp9FullRDInterIntraPlaneTxCandidate(inter *vp9InterEncodeState,
	keyLike *vp9KeyframeEncodeState, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, plane int,
	mode common.PredictionMode, txSize common.TxSize, rdmult int,
	refBestRD uint64,
) encoder.FullRDTxCandidate {
	if plane < 0 || plane >= vp9dec.MaxMbPlane || int(mode) >= common.IntraModes {
		return encoder.FullRDTxCandidate{}
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return encoder.FullRDTxCandidate{}
	}
	planeData, stride := e.vp9EncoderReconPlane(plane)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(keyLike.img, plane)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return encoder.FullRDTxCandidate{}
	}
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
		bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if maxEob > len(e.coefScratch) || bs*bs > len(e.residueScratch) {
		return encoder.FullRDTxCandidate{}
	}
	segID := vp9EncoderMiSegmentID(nil)
	var dequant [2]int16
	if plane == 0 {
		dequant = inter.dq.Y[segID]
	} else {
		dequant = inter.dq.Uv[segID]
	}
	// libvpx full-RD block_rd_txfm uses the segment qindex with the REGULAR
	// quantizer (vp9_xform_quant → vpx_quantize_b, vp9_encodemb.c:537).
	qindex := vp9dec.GetSegmentQindex(&keyLike.hdr.Seg, segID,
		int(keyLike.hdr.Quant.BaseQindex))

	// vp9_get_entropy_contexts (vp9_rdopt.c:872) seeds t_above/t_left from
	// pd->above_context/pd->left_context; mirror the inter producer.
	var aboveCtx [16]uint8
	var leftCtx [16]uint8
	aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
	leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
	if aboveLen > len(aboveCtx) || leftLen > len(leftCtx) {
		return encoder.FullRDTxCandidate{}
	}
	if len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if plane < len(aboveOffsets) && plane < len(leftOffsets) {
			if off := aboveOffsets[plane]; off >= 0 && off+aboveLen <= len(pd.AboveContext) {
				copy(aboveCtx[:aboveLen], pd.AboveContext[off:off+aboveLen])
			}
			if off := leftOffsets[plane]; off >= 0 && off+leftLen <= len(pd.LeftContext) {
				copy(leftCtx[:leftLen], pd.LeftContext[off:off+leftLen])
			}
		}
	}

	var rate int
	var dist uint64
	var sse uint64
	skippable := true
	thisRD := uint64(0)
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			coeffs := e.coefScratch[:maxEob]
			qcoeffs := e.qCoefScratch[:maxEob]
			for i := range coeffs {
				coeffs[i] = 0
				qcoeffs[i] = 0
			}
			// coeff_ctx = combine_entropy_contexts(t_left[r], t_above[c])
			// (vp9_rdopt.c:709-710) feeds BOTH vp9_optimize_b and cost_coeffs.
			initCtx := vp9dec.GetEntropyContext(txSize,
				aboveCtx[cc:cc+step], leftCtx[rr:rr+step])
			// vp9_encode_block_intra: intra-predict the tx unit, subtract,
			// transform, quantize (regular), trellis, inverse-add in place.
			hasResidue := e.prepareVP9InterIntraTxResidueFullRD(keyLike, pd,
				plane, mode, txSize, tile, miRows, miCols, miRow, miCol, bsize,
				rr, cc, dequant, qindex, initCtx, coeffs, qcoeffs)

			// sse = sum_squares(src_diff) * 16 (vp9_rdopt.c:757-765). The diff
			// was just written by gatherVP9TxResidual.
			blockSSE := encoder.ResidualSSE(e.residueScratch[:bs*bs]) * 16
			sse += blockSSE

			// dist = pixel_sse(src, recon) * 16 (vp9_rdopt.c:766-768). When
			// eob==0 the predictor stays in recon, reducing to pixel_sse(src,pred).
			blockDist, distOK := vp9FullRDInterTxBlockPixelSSE(src, srcStride,
				srcW, srcH, planeData, stride, baseX+cc*4, baseY+rr*4, bs)
			if !distOK {
				return encoder.FullRDTxCandidate{}
			}
			blockDist *= 16
			dist += blockDist

			// block_rd_txfm zero-rate early-exit (vp9_rdopt.c:820-824).
			if refBestRD != rdCostMaxLocal {
				rdZeroRate := encoder.RDCost(rdmult, encoder.RDDivBits, 0, blockDist)
				if thisRD+rdZeroRate > refBestRD {
					return encoder.FullRDTxCandidate{}
				}
			}

			// cost_coeffs over the trellis-optimised qcoeff/dqcoeff with the same
			// coeff_ctx (vp9_rdopt.c:826), is_inter=0 token model. Y uses the
			// per-mode ADST/DCT scan (intra_mode_to_tx_type); chroma is DCT_DCT.
			var blockRate int
			if plane == 0 {
				blockRate = e.vp9KeyframeCoeffBlockRateCostQ(txSize, mode,
					inter.lossless, dequant, coeffs, qcoeffs, initCtx)
			} else {
				blockRate = e.vp9KeyframeUvCoeffBlockRateCostQ(txSize, dequant,
					coeffs, qcoeffs, initCtx)
			}
			rate += blockRate

			// rd = VPXMIN(RDCOST(rate,dist), RDCOST(0,sse)) accumulated into
			// this_rd, with a second early-exit (vp9_rdopt.c:829-849).
			rdCoded := encoder.RDCost(rdmult, encoder.RDDivBits, blockRate, blockDist)
			rdZero := encoder.RDCost(rdmult, encoder.RDDivBits, 0, blockSSE)
			thisRD += min(rdCoded, rdZero)

			hasCtx := uint8(0)
			if hasResidue {
				hasCtx = 1
				skippable = false
			}
			for i := 0; i < step && cc+i < aboveLen; i++ {
				aboveCtx[cc+i] = hasCtx
			}
			for i := 0; i < step && rr+i < leftLen; i++ {
				leftCtx[rr+i] = hasCtx
			}

			if refBestRD != rdCostMaxLocal && thisRD > refBestRD {
				return encoder.FullRDTxCandidate{}
			}
		}
	}
	return encoder.FullRDTxCandidate{
		Valid: true,
		Rate:  rate,
		Dist:  dist,
		Skip:  skippable,
		SSE:   sse,
	}
}

// prepareVP9InterIntraTxResidueFullRD is the intra per-tx-block residue builder
// for the inter-frame intra branch. It mirrors the keyframe
// prepareVP9KeyframeTxResidueWithQ (intra-predict → subtract → forward DCT/ADST
// → regular quantize → inverse-add into recon) but additionally runs the
// verbatim vp9_optimize_b trellis (encoder.VP9OptimizeB) between the quantizer
// and the inverse transform / cost_coeffs, exactly as block_rd_txfm does for the
// RT full-RD mode-selection path (vp9_rdopt.c:793, do_trellis_opt ->
// ENABLE_TRELLIS_OPT; vp9_encodemb.c:863-867 for the intra encode). The trellis
// uses the intra (is_inter=0) coef model and ref=0 plane_rd_mult. Returns true
// when eob > 0.
//
// This is REPLICATED here (rather than calling the keyframe helper) because the
// keyframe helper runs the fast no-trellis quantize path; editing it would touch
// a shared production codepath.
func (e *VP9Encoder) prepareVP9InterIntraTxResidueFullRD(keyLike *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int, dequant [2]int16,
	qindex int, coeffCtx int, out, qOut []int16,
) bool {
	dst, stride, x0, y0, ok := e.predictVP9KeyframeTx(keyLike.hdr, pd, plane, mode,
		txSize, tile, miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	// libvpx tx_type: Y plane uses intra_mode_to_tx_type[mode] for tx != 32x32
	// (and != lossless); chroma is always DCT_DCT (vp9_encode_block_intra /
	// get_tx_type, vp9_encodemb.c:826-837).
	txType := common.DctDct
	if plane == 0 && txSize != common.Tx32x32 && !keyLike.lossless {
		txType = common.IntraModeToTxType[mode]
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(keyLike.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	scan := common.ScanOrders[txSize][txType]
	if keyLike.lossless {
		scan = common.DefaultScanOrders[txSize]
	}
	planeType := 0
	if plane != 0 {
		planeType = 1
	}
	// coef_probs[tx_size][plane_type][ref=0 (intra)] — the token_costs slab
	// vp9_optimize_b indexes (vp9_encodemb.c:103-104).
	coefModel := &e.fc.CoefProbs[txSize][planeType][0]
	// libvpx gates vp9_optimize_b on do_trellis_opt (vp9_rdopt.c:797-802 for the
	// inter-frame intra block path too). RT speed >= 1 (cpu4) disables it
	// (DISABLE_TRELLIS_OPT, vp9_speed_features.c:488); cpu0 keeps it enabled. A
	// nil closure skips the trellis and keeps the raw quantizer output.
	var trellis func(coeff, qcoeff, dqcoeff []int16, eob int) int
	if e.vp9DoTrellisOptInterY(txSize) {
		trellis = func(coeff, qcoeff, dqcoeff []int16, eob int) int {
			// e.modeScratch is the ENTROPY_CONTEXT token_cache[1024]
			// (vp9_encodemb.c:72); the cost_coeffs call after this re-clears it.
			return encoder.VP9OptimizeB(plane, 0 /*ref intra*/, txSize, coeffCtx,
				coeff, qcoeff, dqcoeff, eob, dequant, scan.Scan, scan.Neighbors,
				coefModel, int64(e.cbRdmult), uint(e.rc.rddiv), int(e.opts.Sharpness),
				0 /*segment_id*/, &e.modeScratch)
		}
	}
	// useLp32x32RD=true: super_block_yrd runs inside the full-RD mode-selection
	// path where rd_pick_sb_modes forces x->use_lp32x32fdct=1
	// (vp9_encodeframe.c:1994).
	return e.quantizeVP9TxResidualWithQTrellis(dst, stride, txSize, txType,
		dequant, qindex, out, qOut, keyLike.lossless,
		false /*useFastQuant*/, true /*useLp32x32RD*/, trellis)
}

// vp9FullRDInterIntraRD1 recomputes rd[m][1] for an already-produced INTRA
// candidate m, used by the Y-RD sweep to tighten best_rd for the next tx size's
// early-exit threshold. It is the intra sibling of vp9FullRDInterRD1: it OMITS
// the inter-only sse floor (vp9_rdopt.c:988-991 is gated on is_inter_block), so
// the intra branch's rd[n][1] is exactly the skippable / non-skip RDCOST without
// the RDCOST(s1, sse) minimum the inter path applies.
func vp9FullRDInterIntraRD1(cand [common.TxSizes]encoder.FullRDTxCandidate,
	txSizeCostRow [common.TxSizes]int, m, rdmult, s0, s1 int,
) uint64 {
	if m < 0 || m >= int(common.TxSizes) {
		return rdCostMaxLocal
	}
	c := cand[m]
	if !c.Valid {
		return rdCostMaxLocal
	}
	// vp9_rdopt.c:975-986 — skippable / non-skip rd[n][1] (intra: is_inter==0).
	if c.Skip {
		// rd[n][1] = RDCOST(s1 + r_tx_size, sse) (vp9_rdopt.c:981).
		return encoder.RDCost(rdmult, encoder.RDDivBits, s1+txSizeCostRow[m], c.SSE)
	}
	// rd[n][1] = RDCOST(r[n][1] + s0, d[n]); r[n][1] = r[n][0] + r_tx_size.
	r1 := c.Rate + txSizeCostRow[m]
	return encoder.RDCost(rdmult, encoder.RDDivBits, r1+s0, c.Dist)
}
