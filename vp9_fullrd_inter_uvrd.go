package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_uvrd.go ports the GENUINE inter super_block_uvrd
// (vp9/encoder/vp9_rdopt.c:1420) → txfm_rd_in_plane (vp9_rdopt.c:854) →
// block_rd_txfm (vp9_rdopt.c:699) over the U and V planes of an inter block,
// as a standalone, verified producer mirroring the Y producer
// vp9FullRDInterSuperBlockYRD (vp9_fullrd_inter_yrd.go) and the keyframe UV
// pickVP9KeyframeUvModeRD (vp9_encoder_key_modes.go).
//
// libvpx super_block_uvrd (verbatim, vp9_rdopt.c:1420-1466):
//
//	const TX_SIZE uv_tx_size = get_uv_tx_size(mi, &xd->plane[1]);
//	if (ref_best_rd < 0) is_cost_valid = 0;
//	if (is_inter_block(mi) && is_cost_valid)
//	  for (plane = 1; plane < MAX_MB_PLANE; ++plane) vp9_subtract_plane(...);
//	*rate = 0; *distortion = 0; *sse = 0; *skippable = 1;
//	for (plane = 1; plane < MAX_MB_PLANE; ++plane) {
//	  txfm_rd_in_plane(cpi, x, &pnrate, &pndist, &pnskip, &pnsse, ref_best_rd,
//	                   plane, bsize, uv_tx_size, ...);
//	  if (pnrate == INT_MAX) { is_cost_valid = 0; break; }
//	  *rate += pnrate; *distortion += pndist; *sse += pnsse;
//	  *skippable &= pnskip;
//	}
//	if (!is_cost_valid) { *rate = INT_MAX; ... return 0; }
//	return 1;
//
// Unlike the chroma keyframe scorer (which uses the intra predictor + fast
// quantizer), this producer assumes the INTER chroma predictor for *mi is
// already on the recon planes (predictVP9InterBlock builds all three planes by
// default) and runs the full-RD txfm_rd_in_plane path: the REGULAR quantizer
// vpx_quantize_b (sf->use_quant_fp == 0 on the full-RD path,
// vp9_speed_features.c:954) followed by the verbatim vp9_optimize_b trellis
// (encoder.VP9OptimizeB), exactly like the Y producer. The chroma coef-token
// model is fc.CoefProbs[uv_tx_size][PLANE_TYPE_UV=1][is_inter=1].
//
// This is the bounded inter UV tx-RD PRODUCER the holistic full-RD inter port
// needs for the per-mode this_rd assembly (vp9_fullrd_inter_thisrd.go). It is
// NOT wired into pickVP9InterModeWithOrder's production score (that is gated
// behind vp9InterUseDeepRDPartition, which is off in production).
//
// libvpx ground truth (vpxenc-vp9 + TEMPORARY fprintf in handle_inter_mode
// after super_block_uvrd, reverted): seed {0,2,0,0,2} (CBR 1200 kbps cpu0
// realtime, kf=999, fps 30) frame 1 SB0 64x64 root NEWMV ref=LAST mv=(12,4)
// filt=EIGHTTAP_SMOOTH at qindex=145 yields, for the chroma planes:
//
//	rate_uv=780630 dist_uv=1533408 sseuv=4672816 skippable_uv=0
//
// (uv_tx_size == TX_16X16 for a 64x64 luma block at 4:2:0: the 32x32-luma chroma
// plane caps at a 32x32 chroma region whose max tx is TX_16X16 via
// uv_txsize_lookup.)

// vp9FullRDInterUVRDResult is the super_block_uvrd output: the accumulated
// (rate, distortion, sse, skippable) across the U and V planes, plus Valid which
// maps libvpx's is_cost_valid (return value 0 → invalid, the whole UV-RD reset
// to INT_MAX).
type vp9FullRDInterUVRDResult struct {
	UvTxSize   common.TxSize
	Rate       int
	Distortion uint64
	SSE        uint64
	Skippable  bool
	Valid      bool
}

// vp9FullRDInterSuperBlockUVRD runs the genuine inter super_block_uvrd for the
// U and V planes of an inter block. The inter chroma predictor for (mode,
// refFrame, mv, filter) is built into the recon planes by this function
// (predictVP9InterBlock builds all three planes), matching libvpx, which forms
// x->plane[1..2].src_diff via vp9_subtract_plane against the predictor in
// pd->dst before super_block_uvrd.
//
// refBestRD is libvpx's ref_best_rd argument (in handle_inter_mode this is
// ref_best_rd - rdcosty, the budget left after the Y plane); it feeds each
// txfm_rd_in_plane's block_rd_txfm early-exit. A negative budget (libvpx
// ref_best_rd < 0) makes the whole UV-RD invalid (Valid=false).
func (e *VP9Encoder) vp9FullRDInterSuperBlockUVRD(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter, yTxSize common.TxSize, rdmult int,
	refBestRDValid bool, refBestRD uint64,
) vp9FullRDInterUVRDResult {
	if inter == nil || inter.dq == nil || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes || yTxSize >= common.TxSizes {
		return vp9FullRDInterUVRDResult{}
	}
	// libvpx: super_block_yrd sets xd->mi[0]->tx_size (vp9_rdopt.c:870), and
	// super_block_uvrd's get_uv_tx_size(mi, &pd[1]) caps the chroma tx at this
	// luma tx. The caller threads the Y producer's selected tx_size in as
	// mi.TxSize so the chroma uv_tx_size matches libvpx.
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       yTxSize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame:     [2]int8{refFrame, vp9dec.NoRefFrame},
		Mv:           [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return vp9FullRDInterUVRDResult{}
	}
	return e.vp9FullRDInterSuperBlockUVRDForMi(inter, miRows, miCols, miRow,
		miCol, bsize, &mi, rdmult, refBestRDValid, refBestRD)
}

// vp9FullRDInterSuperBlockUVRDForMi is the post-prediction core: it assumes the
// inter chroma predictor for *mi is already on the recon planes and runs the
// super_block_uvrd plane loop.
func (e *VP9Encoder) vp9FullRDInterSuperBlockUVRDForMi(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, rdmult int, refBestRDValid bool, refBestRD uint64,
) vp9FullRDInterUVRDResult {
	// libvpx vp9_rdopt.c:1431 — if (ref_best_rd < 0) is_cost_valid = 0. The
	// caller passes ref_best_rd - rdcosty; a negative budget invalidates UV-RD.
	if !refBestRDValid {
		return vp9FullRDInterUVRDResult{}
	}
	// libvpx vp9_rdopt.c:1425 — uv_tx_size = get_uv_tx_size(mi, &xd->plane[1]).
	uvTxSize := vp9dec.GetUvTxSize(bsize, mi.TxSize, &e.planes[1])

	var rate int
	var dist uint64
	var sse uint64
	skippable := true
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		c := e.vp9FullRDInterUVPlaneTxCandidate(inter, miRows, miCols, miRow,
			miCol, bsize, plane, uvTxSize, rdmult, refBestRDValid, refBestRD)
		if !c.Valid {
			// libvpx super_block_uvrd: pnrate == INT_MAX -> is_cost_valid = 0,
			// the whole UV-RD reset to INT_MAX (return value 0).
			return vp9FullRDInterUVRDResult{UvTxSize: uvTxSize}
		}
		// libvpx vp9_rdopt.c:1451-1454.
		rate += c.Rate
		dist += c.Dist
		sse += c.SSE
		skippable = skippable && c.Skip
	}
	return vp9FullRDInterUVRDResult{
		UvTxSize:   uvTxSize,
		Rate:       rate,
		Distortion: dist,
		SSE:        sse,
		Skippable:  skippable,
		Valid:      true,
	}
}

// vp9FullRDInterUVPlaneTxCandidate is the per-plane txfm_rd_in_plane
// (vp9_rdopt.c:854-889) producer for one inter chroma plane at uv_tx_size. It
// is the chroma sibling of the Y producer's vp9FullRDInterYPlaneTxCandidate:
// for each transform unit it runs block_rd_txfm's inter pixel-domain path with
// the REGULAR quantizer + vp9_optimize_b trellis, accumulating
// (rate, dist, sse, skippable) with the two block_rd_txfm early-exits
// (vp9_rdopt.c:820-824,846-849). Returns Valid=false on early-exit (mapping
// txfm_rd_in_plane's exit_early → rate=INT_MAX, vp9_rdopt.c:878-883).
func (e *VP9Encoder) vp9FullRDInterUVPlaneTxCandidate(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, plane int,
	txSize common.TxSize, rdmult int, refBestRDValid bool, refBestRD uint64,
) encoder.FullRDTxCandidate {
	if plane < 1 || plane >= vp9dec.MaxMbPlane {
		return encoder.FullRDTxCandidate{}
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return encoder.FullRDTxCandidate{}
	}
	planeData, stride := e.vp9EncoderReconPlane(plane)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
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
	dequant := inter.dq.Uv[segID]
	// libvpx full-RD block_rd_txfm uses the segment qindex with the REGULAR
	// quantizer (vp9_xform_quant → vpx_quantize_b, vp9_encodemb.c:537).
	qindex := inter.baseQindex

	// vp9_get_entropy_contexts (vp9_rdopt.c:872) seeds t_above/t_left from
	// pd->above_context/pd->left_context; govpx reads them via the plane context
	// cache and updates per block to (eob > 0). For SB corners libvpx
	// initialises to zero, matching the zero-initialised local arrays.
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
			// vp9_xform_quant + vp9_optimize_b (trellis) + inverse-add into recon
			// with the REGULAR quantizer at the segment qindex.
			hasResidue := e.prepareVP9InterUVTxResidueFullRD(inter, pd, plane,
				txSize, miRow, miCol, rr, cc, dequant, qindex, initCtx,
				coeffs, qcoeffs)

			// sse = sum_squares(src_diff) * 16 (vp9_rdopt.c:757-765).
			blockSSE := encoder.ResidualSSE(e.residueScratch[:bs*bs]) * 16
			sse += blockSSE

			// dist = pixel_sse(src, recon) * 16 (vp9_rdopt.c:681-689). When
			// eob==0 the predictor stays in recon, reducing to pixel_sse(src,pred).
			blockDist, distOK := vp9FullRDInterTxBlockPixelSSE(src, srcStride,
				srcW, srcH, planeData, stride, baseX+cc*4, baseY+rr*4, bs)
			if !distOK {
				return encoder.FullRDTxCandidate{}
			}
			blockDist *= 16
			dist += blockDist

			// block_rd_txfm zero-rate early-exit (vp9_rdopt.c:820-824): rd =
			// RDCOST(rdmult, rddiv, 0, dist); if this_rd + rd > best_rd → exit.
			if refBestRDValid {
				rdZeroRate := encoder.RDCost(rdmult, encoder.RDDivBits, 0, blockDist)
				if thisRD+rdZeroRate > refBestRD {
					return encoder.FullRDTxCandidate{}
				}
			}

			// cost_coeffs over the trellis-optimised qcoeff/dqcoeff with the same
			// coeff_ctx (vp9_rdopt.c:826), chroma plane (PLANE_TYPE_UV=1), inter.
			blockRate := e.vp9InterCoeffBlockRateCostQ(txSize, 1, dequant,
				coeffs, qcoeffs, initCtx)
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

			if refBestRDValid && thisRD > refBestRD {
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

// prepareVP9InterUVTxResidueFullRD is the chroma sibling of
// prepareVP9InterTxResidueFullRD: it builds the inter chroma transform-unit
// residual (src - predictor), forward-transforms it (DCT_DCT — chroma never
// uses ADST), quantizes with the REGULAR quantizer at the segment qindex, runs
// the verbatim vp9_optimize_b trellis, and inverse-adds the dequantized
// residual into the recon plane. Returns true when eob > 0.
func (e *VP9Encoder) prepareVP9InterUVTxResidueFullRD(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol, blockRow4x4, blockCol4x4 int, dequant [2]int16, qindex int,
	coeffCtx int, out, qOut []int16,
) bool {
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	scan := common.DefaultScanOrders[txSize]
	// coef_probs[tx_size][plane_type=1 (UV)][ref=1 (inter)] — the token_costs
	// slab vp9_optimize_b indexes (vp9_encodemb.c:103-104).
	coefModel := &e.fc.CoefProbs[txSize][1][1]
	// libvpx gates vp9_optimize_b on do_trellis_opt (vp9_rdopt.c:797-802); RT
	// speed >= 1 (cpu4) disables the trellis (DISABLE_TRELLIS_OPT,
	// vp9_speed_features.c:488). nil closure skips it. cpu0 keeps it enabled.
	var trellis func(coeff, qcoeff, dqcoeff []int16, eob int) int
	if e.vp9DoTrellisOptInterY(txSize) {
		trellis = func(coeff, qcoeff, dqcoeff []int16, eob int) int {
			return encoder.VP9OptimizeB(1 /*plane UV*/, 1 /*ref inter*/, txSize,
				coeffCtx, coeff, qcoeff, dqcoeff, eob, dequant,
				scan.Scan, scan.Neighbors, coefModel,
				int64(e.cbRdmult), uint(e.rc.rddiv), int(e.opts.Sharpness),
				0 /*segment_id*/, &e.modeScratch)
		}
	}
	// useLp32x32RD=true: super_block_uvrd runs inside the full-RD mode-selection
	// path where rd_pick_sb_modes forces x->use_lp32x32fdct=1
	// (vp9_encodeframe.c:1994); chroma tops out at TX_16X16 so this is moot for
	// the 64x64 case but kept for parity with the Y producer.
	return e.quantizeVP9TxResidualWithQTrellis(dst, stride, txSize, common.DctDct,
		dequant, qindex, out, qOut, inter.lossless,
		false /*useFastQuant*/, true /*useLp32x32RD*/, trellis)
}
