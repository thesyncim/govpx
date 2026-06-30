package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_yrd.go ports the GENUINE inter super_block_yrd
// (vp9/encoder/vp9_rdopt.c:1025) → choose_tx_size_from_rd
// (vp9_rdopt.c:907) → txfm_rd_in_plane (vp9_rdopt.c:854) → block_rd_txfm
// (vp9_rdopt.c:699) → dist_block (vp9_rdopt.c:561) for the Y plane of an
// inter block, as a standalone, verified producer.
//
// Unlike the model/SSE-based vp9InterModeScore approximation (which never
// runs the tx-size sweep), this function runs the real per-tx-size
// transform-RD loop: for each tx_size n in [end_tx .. start_tx], over the
// block's 4x4/8x8/16x16/32x32 transform units, it predicts→subtracts→
// fdct→quantizes→inverse-adds and accumulates (rate, dist, skip, sse) the
// way libvpx's block_rd_txfm does, then feeds the per-tx-size arrays to the
// verbatim choose_tx_size_from_rd selector (encoder.FullRDChooseTxSize) to
// pick tx_size and the reported block rate/dist/skip/sse + best_rd.
//
// All RD primitives are reused verbatim: quantize (quantizeVP9TxResidualWithQ),
// fdct (ForwardHT*Into via the residue helper), cost_coeffs
// (vp9InterCoeffBlockRateCostQ → encoder.CoeffBlockRateCost),
// FullRDChooseTxSize, and RDCost/ComputeRDMult. No constants are re-derived.
//
// This is the bounded inter tx-RD PRODUCER the holistic full-RD inter port
// needs; it is NOT wired into pickVP9InterModeWithOrder yet (that flips
// decisions and is the next step).

// vp9FullRDInterYRDResult is the super_block_yrd output for the Y plane: the
// selected tx_size plus the rate/distortion/skip/sse choose_tx_size_from_rd
// reports back to its caller (vp9_rdopt.c:1006-1009), and BestRD, the
// internal best_rd choose_tx_size_from_rd arrives at (vp9_rdopt.c:1001),
// which is what super_block_yrd's caller folds into rdcosty.
type vp9FullRDInterYRDResult struct {
	TxSize     common.TxSize
	Rate       int
	Distortion uint64
	Skippable  bool
	SSE        uint64
	BestRD     uint64
	Valid      bool
	// Cand carries the raw per-tx-size txfm_rd_in_plane outputs (indexed by
	// TX_SIZE) the selector consumed, so callers/tests can pin the full
	// r[n][0]/d[n]/s[n]/sse[n] arrays, not just the selected tx_size's tuple.
	Cand  [common.TxSizes]encoder.FullRDTxCandidate
	Start int
	End   int
}

// vp9FullRDInterSuperBlockYRD runs the genuine inter super_block_yrd for the
// Y plane of an inter block, given an already-built encoder/inter context.
//
// Preconditions: the inter predictor for (mode, refFrame, mv, filter) is
// produced into the recon plane by this function (predictVP9InterBlockOpts);
// the caller supplies the same per-frame rdmult/skipProb/coef-probs libvpx
// uses (rdmult == x->rdmult, e.fc == the frame's entropy context).
//
// libvpx ground truth (vpxenc-vp9 + TEMPORARY fprintf in
// choose_tx_size_from_rd, reverted): seed {0,2,0,0,2} (CBR 1200 kbps cpu0
// realtime, kf=999, fps 30) frame 1 SB0 64x64 root NEWMV ref=LAST mv=(12,4)
// filt=EIGHTTAP_SMOOTH at qindex=145 (rdmult=139158, rddiv=7) yields
// tx_size=TX_16X16, best_rd=2188910183, with
//
//	n=TX_16X16: r0=5462472 r1=5464856 d=5496832 s=0 sse=84109680  (selected)
//	n=TX_32X32: r0=6466064 r1=6466285 d=5642240 s=0 sse=84109680
//
// (start_tx=TX_32X32, end_tx=TX_16X16 per the TX_MODE_SELECT depth-2 path.)
//
// This producer reproduces the SELECTED tx_size, best_rd, and the FULL
// per-tx-size table (incl. the TX_32X32 loser) byte-exactly. libvpx's
// super_block_yrd runs vp9_optimize_b (trellis coefficient optimization) on
// each transform block ONLY when do_trellis_opt is non-zero (vp9_rdopt.c:
// 797-802) — i.e. when sf.trellis_opt_tx_rd.method != DISABLE_TRELLIS_OPT. That
// holds for the cpu0 ({0,2,0,0,2}) realtime mode-selection path (speed 0 keeps
// the ENABLE_TRELLIS_OPT default, vp9_speed_features.c:975-976), but NOT for RT
// speed >= 1 (e.g. cpu4 {0,1,1,0,1}), where the speed feature sets
// DISABLE_TRELLIS_OPT (vp9_speed_features.c:485-488). The producer therefore
// gates the trellis on vp9DoTrellisOptInterY: for cpu0 it wires the verbatim
// port (encoder.VP9OptimizeB, internal/vp9/encoder/fullrd_trellis.go) into the
// txfm_rd_in_plane path so the optimized dqcoeff/eob feed both the distortion
// and the cost_coeffs rate (the TX_32X32 candidate matches libvpx exactly:
// r0=6466064 d=5642240; pre-trellis it was r0=6541317 d=5530544); for cpu4 the
// trellis is skipped and the raw quantizer output is kept verbatim. Trellis
// does not flip the winner for THIS block, so the selected tx_size + best_rd
// (TX_16X16, 2188910183) are unchanged.
func (e *VP9Encoder) vp9FullRDInterSuperBlockYRD(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter, rdmult int, refBestRD uint64,
) vp9FullRDInterYRDResult {
	if inter == nil || inter.dq == nil || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return vp9FullRDInterYRDResult{}
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame:     [2]int8{refFrame, vp9dec.NoRefFrame},
		Mv:           [2]vp9dec.MV{mv},
	}
	// Build the inter predictor for the whole block into the recon plane
	// once. libvpx forms x->plane[0].src_diff via vp9_subtract_plane before
	// super_block_yrd; the predictor in pd->dst is fixed across the tx
	// sweep (dist_block adds the inverse into a separate recon scratch, not
	// pd->dst — vp9_rdopt.c:635,664-689). govpx mirrors that by snapshotting
	// the predictor and restoring it between tx sizes.
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return vp9FullRDInterYRDResult{}
	}
	return e.vp9FullRDInterSuperBlockYRDForMi(inter, miRows, miCols, miRow,
		miCol, bsize, &mi, rdmult, refBestRD)
}

// vp9FullRDInterSuperBlockYRDForMi is the post-prediction core: it assumes the
// inter predictor for *mi is already on the recon plane and runs the
// choose_tx_size_from_rd loop for the Y plane.
func (e *VP9Encoder) vp9FullRDInterSuperBlockYRDForMi(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, rdmult int, refBestRD uint64,
) vp9FullRDInterYRDResult {
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return vp9FullRDInterYRDResult{}
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	if len(planeData) == 0 || stride <= 0 {
		return vp9FullRDInterYRDResult{}
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return vp9FullRDInterYRDResult{}
	}
	// Snapshot the predictor rect so each tx-size pass starts from the same
	// pd->dst predictor (vp9_rdopt.c choose_tx_size_from_rd re-runs
	// txfm_rd_in_plane per tx size against the fixed predictor).
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 || restoreW*restoreH > len(e.blockScratch) {
		return vp9FullRDInterYRDResult{}
	}
	predictor := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(predictor[y*restoreW:(y+1)*restoreW],
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
		return vp9FullRDInterYRDResult{}
	}

	// Neighbours for skip / tx-size signalling context. The producer must
	// read the same above/left mi the production picker installs.
	var left *vp9dec.NeighborMi
	if miCol > 0 {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	s0 := encoder.VP9CostBit(skipProb, 0)
	s1 := encoder.VP9CostBit(skipProb, 1)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	txSizeCostRow := encoder.FullRDTxSizeCostRow(txProbs, maxTx)

	// Per-tx-size candidate production. Each txfm_rd_in_plane call receives
	// the running best_rd (vp9_rdopt.c:963-967) so block_rd_txfm's early-exit
	// (vp9_rdopt.c:820-824,846-849) fires exactly as libvpx. best_rd seeds at
	// ref_best_rd and tightens to rd[n][1] as better tx sizes are found
	// (vp9_rdopt.c:924,999-1002). Candidates are produced for ALL tx sizes in
	// [end_tx..start_tx]; the authoritative tx_size_search_breakout is applied
	// by FullRDChooseTxSize, which stops at the same n libvpx's loop would —
	// any candidates it never inspects are harmless. Early-exit only marks a
	// losing tx size Valid=false (never selected), so the winning tx size
	// (whose rd[n][1] <= best_rd) is always fully produced.
	var cand [common.TxSizes]encoder.FullRDTxCandidate
	bestRD := refBestRD
	for n := startTx; n >= endTx; n-- {
		tx := common.TxSize(n)
		c := e.vp9FullRDInterYPlaneTxCandidate(inter, miRows, miCols, miRow,
			miCol, bsize, tx, rdmult, bestRD)
		cand[n] = c
		// Restore the predictor for the next tx size (the inverse-add wrote
		// the reconstruction into the recon plane during this pass).
		encoder.RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, predictor)
		// Tighten best_rd to rd[n][1] for the next tx size's threshold.
		if c.Valid {
			if rd1 := vp9FullRDInterRD1(cand, txSizeCostRow, n, rdmult, s0, s1,
				inter.lossless); rd1 < bestRD {
				bestRD = rd1
			}
		}
	}

	// Final selection via the verbatim choose_tx_size_from_rd selector.
	res := encoder.FullRDChooseTxSize(cand, txSizeCostRow, maxTx,
		startTx, endTx, rdmult, encoder.RDDivBits, s0, s1,
		true /*isInter*/, inter.lossless, e.sf.TxSizeSearchBreakout != 0,
		inter.txMode == common.TxModeSelect, refBestRD)

	// libvpx: if the selected tx reports rate==INT_MAX (the tx_size loop broke
	// on an early-exited largest tx, vp9_rdopt.c:1007), super_block_yrd's caller
	// treats *rate_y == INT_MAX as the whole-block prune (handle_inter_mode
	// :3214-3218 returns INT64_MAX). Map that to an invalid yrd result.
	if res.Rate == encoder.FullRDTxRateInvalid {
		return vp9FullRDInterYRDResult{}
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
	}
}

// vp9InterUseTransformDomainDistortion mirrors libvpx's x->block_tx_domain
// computation (vp9/encoder/vp9_encodeframe.c:2036-2049) for the inter Y plane.
// When sf.tx_domain_thresh and sf.trellis_opt_tx_rd.thresh are both <= 0 (the
// REALTIME speed >= 1 case, vp9_speed_features.c:486-489) the else branch
// returns sf.allow_txfm_domain_distortion directly. When a positive threshold
// is configured (GOOD-quality path), it gates on log_block_var >= thresh,
// reusing the same source-variance metric as the keyframe helper.
func (e *VP9Encoder) vp9InterUseTransformDomainDistortion(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) bool {
	if e == nil || e.sf.AllowTxfmDomainDistortion == 0 {
		return false
	}
	// libvpx vp9_encodeframe.c:2036 — only compute the log-variance gate when a
	// positive threshold is set; otherwise block_tx_domain = allow (== true
	// here, since AllowTxfmDomainDistortion != 0).
	if e.sf.TxDomainThresh <= 0 && e.sf.TrellisOptTxRd.Thresh <= 0 {
		return true
	}
	if inter == nil || inter.img == nil || bsize >= common.BlockSizes {
		return false
	}
	src, stride, width, height := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, width, height) {
		return false
	}
	variance := encoder.BlockSourceVariance128(src, stride, x0, y0, blockW, blockH)
	scaled := float64(variance*256) /
		float64(uint64(1)<<uint(common.NumPelsLog2Lookup[bsize]))
	return math.Log(scaled+1.0) >= e.sf.TxDomainThresh
}

const rdCostMaxLocal = ^uint64(0)

// vp9FullRDInterRD1 recomputes rd[m][1] for an already-produced candidate m,
// used by the breakout comparison `rd[n][1] > rd[n+1][1]` (vp9_rdopt.c:996).
func vp9FullRDInterRD1(cand [common.TxSizes]encoder.FullRDTxCandidate,
	txSizeCostRow [common.TxSizes]int, m, rdmult, s0, s1 int, lossless bool,
) uint64 {
	if m < 0 || m >= int(common.TxSizes) {
		return rdCostMaxLocal
	}
	c := cand[m]
	if !c.Valid {
		return rdCostMaxLocal
	}
	r1 := c.Rate + txSizeCostRow[m]
	var rd1 uint64
	if c.Skip {
		rd1 = encoder.RDCost(rdmult, encoder.RDDivBits, s1, c.SSE)
	} else {
		rd1 = encoder.RDCost(rdmult, encoder.RDDivBits, r1+s0, c.Dist)
	}
	if !lossless && !c.Skip && c.SSE != rdCostMaxLocal {
		floor := encoder.RDCost(rdmult, encoder.RDDivBits, s1, c.SSE)
		if floor < rd1 {
			rd1 = floor
		}
	}
	return rd1
}

// vp9FullRDInterYPlaneTxCandidate is the per-tx-size txfm_rd_in_plane
// (vp9_rdopt.c:854-889) producer for the inter Y plane. For the given tx_size
// it walks the block's 4x4-grid of transform units, and for each unit runs
// block_rd_txfm's inter pixel-domain path (vp9_rdopt.c:770-818 with
// skip_txfm==SKIP_TXFM_NONE, block_tx_domain==0):
//
//   - vp9_xform_quant + dist_block: predict (already on recon) - src →
//     src_diff, forward DCT, quantize_b, inverse-add into recon, then
//     dist = pixel_sse(src, recon) * 16 (vp9_rdopt.c:681-689) and
//     sse = sum_squares(src_diff) * 16 (vp9_rdopt.c:757-765).
//   - rate += cost_coeffs (vp9_rdopt.c:826).
//   - entropy context t_above[c]/t_left[r] = (eob > 0) (vp9_rdopt.c:827-828).
//   - this_rd accumulator early-exit: exits (→ rate=INT_MAX) when
//     this_rd > best_rd (vp9_rdopt.c:820-824,846-849).
//
// Returns a FullRDTxCandidate with Valid=false on early-exit (mapping
// txfm_rd_in_plane's exit_early → rate=INT_MAX path, vp9_rdopt.c:878-883).
func (e *VP9Encoder) vp9FullRDInterYPlaneTxCandidate(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	txSize common.TxSize, rdmult uint64OrInt, refBestRD uint64,
) encoder.FullRDTxCandidate {
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return encoder.FullRDTxCandidate{}
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return encoder.FullRDTxCandidate{}
	}
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
		bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if maxEob > len(e.coefScratch) || bs*bs > len(e.residueScratch) {
		return encoder.FullRDTxCandidate{}
	}
	dequant := inter.dq.Y[0]
	// libvpx full-RD block_rd_txfm calls vp9_xform_quant
	// (vp9/encoder/vp9_encodemb.c:489), which uses the REGULAR quantizer
	// vpx_quantize_b / vpx_quantize_b_32x32 (lines 537,542) — NOT the fast
	// vp9_quantize_fp — because sf->use_quant_fp == 0 on the full-RD path
	// (vp9_speed_features.c:954). The regular quantizer's zbin/round/quant
	// shift come from the segment qindex.
	qindex := inter.baseQindex

	// vp9_get_entropy_contexts (vp9_rdopt.c:872) seeds t_above/t_left from
	// pd->above_context/pd->left_context; govpx reads them via the plane
	// context cache and updates per block to (eob > 0).
	var aboveCtx [16]uint8
	var leftCtx [16]uint8
	aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
	leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
	if aboveLen > len(aboveCtx) || leftLen > len(leftCtx) {
		return encoder.FullRDTxCandidate{}
	}
	if len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if off := aboveOffsets[0]; off >= 0 && off+aboveLen <= len(pd.AboveContext) {
			copy(aboveCtx[:aboveLen], pd.AboveContext[off:off+aboveLen])
		}
		if off := leftOffsets[0]; off >= 0 && off+leftLen <= len(pd.LeftContext) {
			copy(leftCtx[:leftLen], pd.LeftContext[off:off+leftLen])
		}
	}

	// libvpx vp9/encoder/vp9_encodeframe.c:2036-2049 — x->block_tx_domain
	// selects transform-domain distortion (vp9_block_error on coeff/dqcoeff)
	// over pixel-domain (pixel_sse on recon) inside block_rd_txfm
	// (vp9_rdopt.c:571-600). For REALTIME speed >= 1 (cpu4) the speed feature
	// sets allow_txfm_domain_distortion=1 with tx_domain_thresh=0 and
	// trellis_opt_tx_rd.thresh=0 (vp9_speed_features.c:486-489), so the else
	// branch (line 2048) forces block_tx_domain = 1 unconditionally.
	//
	// Wired behind vp9InterUseDeepRDTxDomainDistortion (default OFF): the Y-RD
	// transform-domain dist matches libvpx per-tx exactly (mi(0,4) frame-1 of
	// {0,1,1,0,1}: TX_8X8 d=56334 sse=119666, TX_4X4 d=46797 sse=119991). It
	// MUST be enabled in lockstep with the matching UV-RD transform-domain path
	// (vp9FullRDInterUVPlaneTxCandidate, same flag): Y-only inverts the
	// NEARESTMV-vs-NEARMV this_rd tie at that leaf, while Y+UV together pick
	// NEARMV/TX_4X4 exactly as libvpx. The flag stays OFF by default so the
	// pixel-domain producers (and the cpu0 {0,2,0,0,2} YRD pins) are unchanged.
	useTxDomain := vp9InterUseDeepRDTxDomainDistortion &&
		e.vp9InterUseTransformDomainDistortion(inter, miRows, miCols, miRow, miCol,
			bsize)
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
			// coeff_ctx = combine_entropy_contexts(t_left[blk_row],
			// t_above[blk_col]) (vp9_rdopt.c:709-710) feeds BOTH vp9_optimize_b
			// (the trellis) and cost_coeffs, so it must be computed before the
			// transform/quant/trellis below.
			initCtx := vp9dec.GetEntropyContext(txSize,
				aboveCtx[cc:cc+step], leftCtx[rr:rr+step])
			// vp9_xform_quant + vp9_optimize_b (trellis) + inverse-add into
			// recon (vp9_rdopt.c:792-795) with the REGULAR quantizer (quantize_b)
			// at the segment qindex. gatherVP9TxResidual leaves the src-pred diff
			// in e.residueScratch; the forward DCT lands in e.txCoeffScratch and
			// the dequantized coeffs in e.dqCoeffScratch.
			hasResidue := e.prepareVP9InterTxResidueFullRD(inter, pd, txSize,
				miRow, miCol, rr, cc, dequant, qindex, initCtx, coeffs, qcoeffs)

			var blockDist uint64
			var blockSSE uint64
			if useTxDomain && hasResidue {
				// libvpx vp9_rdopt.c:571-600 (block_tx_domain && eob):
				// dist = vp9_block_error(coeff, dqcoeff) >> shift,
				// sse  = sum(coeff^2)              >> shift, with shift==2
				// for tx != 32x32. TransformBlockError / TransformBlockEnergy
				// fold in that shift.
				blockDist = encoder.TransformBlockError(e.txCoeffScratch[:maxEob],
					e.dqCoeffScratch[:maxEob], txSize)
				blockSSE = encoder.TransformBlockEnergy(e.txCoeffScratch[:maxEob],
					txSize)
			} else {
				// Pixel domain (block_tx_domain==0 or eob==0): dist =
				// pixel_sse(src, recon) * 16, sse = sum_squares(src_diff) * 16
				// (vp9_rdopt.c:601-690, 757-765). eob==0 leaves the predictor in
				// recon so dist reduces to pixel_sse(src, pred).
				bd, distOK := vp9FullRDInterTxBlockPixelSSE(src, srcStride,
					srcW, srcH, planeData, stride, baseX+cc*4, baseY+rr*4, bs)
				if !distOK {
					return encoder.FullRDTxCandidate{}
				}
				blockDist = bd * 16
				blockSSE = encoder.ResidualSSE(e.residueScratch[:bs*bs]) * 16
			}
			sse += blockSSE
			dist += blockDist

			// block_rd_txfm early-exit on accumulated zero-rate rd before the
			// rate is even charged (vp9_rdopt.c:820-824): rd = RDCOST(rdmult,
			// rddiv, 0, dist); if this_rd + rd > best_rd → exit_early.
			if refBestRD != rdCostMaxLocal {
				rdZeroRate := encoder.RDCost(int(rdmult), encoder.RDDivBits, 0, blockDist)
				if thisRD+rdZeroRate > refBestRD {
					return encoder.FullRDTxCandidate{}
				}
			}

			// cost_coeffs over the trellis-optimised qcoeff/dqcoeff with the
			// same coeff_ctx (vp9_rdopt.c:826).
			blockRate := e.vp9InterCoeffBlockRateCostQ(txSize, 0, dequant,
				coeffs, qcoeffs, initCtx)
			rate += blockRate

			// rd = VPXMIN(RDCOST(rate,dist), RDCOST(0,sse)) accumulated into
			// this_rd, with a second early-exit (vp9_rdopt.c:829-849).
			rdCoded := encoder.RDCost(int(rdmult), encoder.RDDivBits, blockRate, blockDist)
			rdZero := encoder.RDCost(int(rdmult), encoder.RDDivBits, 0, blockSSE)
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

// uint64OrInt is a tiny alias to keep the rdmult parameter typed as int while
// documenting that it carries the libvpx x->rdmult value. (Plain int.)
type uint64OrInt = int

// prepareVP9InterTxResidueFullRD is the full-RD sibling of
// prepareVP9InterTxResidueWithQ: it builds the inter Y-plane transform-unit
// residual (src - predictor), forward-transforms it, and quantizes with the
// REGULAR quantizer vpx_quantize_b / vpx_quantize_b_32x32 (vp9_xform_quant,
// vp9/encoder/vp9_encodemb.c:489-543) at the segment qindex, then inverse-adds
// the dequantized residual into the recon plane. Returns true when eob > 0.
//
// This differs from prepareVP9InterTxResidueWithQ (which uses the fast
// vp9_quantize_fp path, qindex 0) — full-RD super_block_yrd runs through
// vp9_xform_quant whose quantizer is the regular quantize_b (sf->use_quant_fp
// == 0 on the full-RD path, vp9_speed_features.c:954).
//
// After quantization it runs the verbatim vp9_optimize_b trellis
// (encoder.VP9OptimizeB) — block_rd_txfm calls vp9_optimize_b between
// vp9_xform_quant and dist_block for the RT full-RD mode-selection path
// (vp9_rdopt.c:793, do_trellis_opt → ENABLE_TRELLIS_OPT). coeffCtx is the
// combine_entropy_contexts value (== the cost_coeffs coeff_ctx); rdmult is
// x->rdmult. The trellis mutates qcoeff/dqcoeff so the inverse-add (recon) and
// the cost_coeffs rate consume the optimised coefficients.
func (e *VP9Encoder) prepareVP9InterTxResidueFullRD(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, txSize common.TxSize,
	miRow, miCol, blockRow4x4, blockCol4x4 int, dequant [2]int16, qindex int,
	coeffCtx int, out, qOut []int16,
) bool {
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, 0, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	scan := common.DefaultScanOrders[txSize]
	// coef_probs[tx_size][plane_type=0 (Y)][ref=1 (inter)] — the token_costs
	// slab vp9_optimize_b indexes (vp9_encodemb.c:103-104).
	coefModel := &e.fc.CoefProbs[txSize][0][1]
	// libvpx block_rd_txfm gates vp9_optimize_b on do_trellis_opt
	// (vp9_rdopt.c:797-802). For RT speed >= 1 (e.g. cpu4) the speed feature sets
	// trellis_opt_tx_rd.method = DISABLE_TRELLIS_OPT (vp9_speed_features.c:488),
	// so the trellis is SKIPPED and the raw quantizer eob/coeffs are kept; only
	// cpu0 (speed 0) keeps ENABLE_TRELLIS_OPT. A nil trellis closure skips it.
	var trellis func(coeff, qcoeff, dqcoeff []int16, eob int) int
	if e.vp9DoTrellisOptInterY(txSize) {
		trellis = func(coeff, qcoeff, dqcoeff []int16, eob int) int {
			// e.modeScratch is the ENTROPY_CONTEXT token_cache[1024]
			// (vp9_encodemb.c:72); reused by the cost_coeffs call after this,
			// which re-clears it.
			return encoder.VP9OptimizeB(0 /*plane Y*/, 1 /*ref inter*/, txSize,
				coeffCtx, coeff, qcoeff, dqcoeff, eob, dequant,
				scan.Scan, scan.Neighbors, coefModel,
				int64(e.cbRdmult), uint(e.rc.rddiv), int(e.opts.Sharpness),
				0 /*segment_id*/, &e.modeScratch)
		}
	}
	// useLp32x32RD=true: super_block_yrd runs inside the full-RD
	// mode-selection path, where rd_pick_sb_modes forces x->use_lp32x32fdct=1
	// (vp9_encodeframe.c:1994) so 32x32 uses vpx_fdct32x32_rd regardless of
	// the speed feature.
	return e.quantizeVP9TxResidualWithQTrellis(dst, stride, txSize, common.DctDct,
		dequant, qindex, out, qOut, inter.lossless,
		false /*useFastQuant*/, true /*useLp32x32RD*/, trellis) > 0
}

// vp9FullRDInterTxBlockPixelSSE returns pixel_sse(src, dst) (vp9_rdopt.c:523)
// for one transform block, with the visible-region clamp libvpx applies for
// edge blocks. For fully-visible tx blocks it is the plain bs*bs SSE.
func vp9FullRDInterTxBlockPixelSSE(src []byte, srcStride, srcW, srcH int,
	dst []byte, dstStride, x0, y0, bs int,
) (uint64, bool) {
	if len(src) == 0 || srcStride <= 0 || len(dst) == 0 || dstStride <= 0 ||
		bs <= 0 {
		return 0, false
	}
	dstRows := len(dst) / dstStride
	if x0 < 0 || y0 < 0 || x0 >= dstStride || y0 >= dstRows {
		return 0, false
	}
	// Visible width/height clamp: libvpx pixel_sse only sums the 4x4s that
	// lie within the frame (num_4x4_to_edge). For the no-clamp case this is
	// the full bs x bs block.
	w := bs
	h := bs
	if x0+w > srcW {
		w = srcW - x0
	}
	if y0+h > srcH {
		h = srcH - y0
	}
	if x0+w > dstStride {
		w = dstStride - x0
	}
	if y0+h > dstRows {
		h = dstRows - y0
	}
	if w <= 0 || h <= 0 {
		return 0, true
	}
	// libvpx clamps on 4x4 granularity; round the visible extent down to a
	// multiple of 4 to match num_4x4_to_edge (vp9_rdopt.c:534-537).
	w &^= 3
	h &^= 3
	if w <= 0 || h <= 0 {
		return 0, true
	}
	var sseSum uint64
	for y := 0; y < h; y++ {
		srcRow := src[(y0+y)*srcStride+x0:]
		dstRow := dst[(y0+y)*dstStride+x0:]
		for x := 0; x < w; x++ {
			diff := int(srcRow[x]) - int(dstRow[x])
			sseSum += uint64(diff * diff)
		}
	}
	return sseSum, true
}
