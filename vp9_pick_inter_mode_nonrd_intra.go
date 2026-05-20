package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func vp9NonrdAllowEncodeBreakout(lossless, sceneChangeDetected,
	highNumBlocksWithMotion bool,
) bool {
	return !lossless && !sceneChangeDetected && !highNumBlocksWithMotion
}

func vp9NonrdModeRdThreshold(base int, bestModeSkipTxfm, biasGolden bool,
	refFrame int8, framesSinceGolden uint16,
) int {
	modeRdThresh := base
	if bestModeSkipTxfm {
		modeRdThresh <<= 1
	}
	if biasGolden && refFrame == vp9dec.GoldenFrame && framesSinceGolden > 4 {
		modeRdThresh <<= 3
	}
	return modeRdThresh
}

func vp9NonrdForceLastReference(shortCircuitLowTempVar int,
	useNonrdPickMode, forceSkipLowTempVar bool,
) bool {
	return useNonrdPickMode && forceSkipLowTempVar &&
		(shortCircuitLowTempVar == 1 || shortCircuitLowTempVar == 3)
}

func vp9NonrdNormalizeSSE(sse uint64, bsize common.BlockSize) uint64 {
	if bsize < common.Block4x4 || bsize >= common.BlockSizes {
		return sse
	}
	return sse >> uint(common.NumPelsLog2Lookup[bsize])
}

func (e *VP9Encoder) vp9NonrdSourceVariance(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) (uint, bool) {
	if inter == nil || inter.img == nil ||
		bsize < common.Block4x4 || bsize >= common.BlockSizes {
		return 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	srcX := miCol * common.MiSize
	srcY := miRow * common.MiSize
	if srcX < 0 || srcY < 0 || srcX+blockW > srcW || srcY+blockH > srcH {
		return 0, false
	}
	return vp9SourceVariancePerPixel(src, srcStride, srcX, srcY,
		blockW, blockH, bsize), true
}

func vp9SourceVariancePerPixel(src []byte, srcStride, srcX, srcY, w, h int,
	bsize common.BlockSize,
) uint {
	return vp9SourceVarianceAreaPerPixel(src, srcStride, srcX, srcY, w, h)
}

func vp9NonrdScreenZeroLastBias(screen, sceneChangeDetected,
	highNumBlocksWithMotion bool, refFrame int8, mv vp9dec.MV,
	sourceVariance uint, sseY uint64,
) bool {
	return screen && (sceneChangeDetected || highNumBlocksWithMotion) &&
		refFrame == vp9dec.LastFrame && mv == (vp9dec.MV{}) &&
		sourceVariance == 0 && sseY > 0
}

func vp9NonrdIntraFallbackPrecheck(bestInterScore, interModeThresh uint64,
	forceSkipLowTempVar bool, bsize common.BlockSize,
	contentState vp9ContentStateSB, xSkip, sceneChangeDetected,
	screenFlat bool,
) bool {
	if screenFlat || sceneChangeDetected {
		return true
	}
	if xSkip {
		return false
	}
	if bestInterScore <= interModeThresh {
		return false
	}
	if forceSkipLowTempVar && bsize >= common.Block32x32 &&
		contentState != vp9ContentStateVeryHighSad {
		return false
	}
	return true
}

// vp9NonrdEstimateIntraFallback ports the intra-fallback section inside
// libvpx's vp9_pick_inter_mode (vp9_pickmode.c:2525-2648). It walks
// intra_mode_list (DC_PRED, V_PRED, H_PRED, TM_PRED) and computes a
// libvpx-faithful RDCOST per candidate via estimate_block_intra +
// block_yrd. Returns the winning intra decision when it strictly beats the
// supplied bestInterScore under the same rdmult/rddiv shape.
//
// Gating mirrors libvpx vp9_pickmode.c:2527-2534:
//
//	if (best_rdc.rdcost == INT64_MAX ||
//	    (cpi->oxcf.content == VP9E_CONTENT_SCREEN &&
//	     x->source_variance == 0) ||
//	    (scene_change_detected && perform_intra_pred) ||
//	    (... perform_intra_pred && !x->skip &&
//	     best_rdc.rdcost > inter_mode_thresh &&
//	     bsize <= cpi->sf.max_intra_bsize && ...)) {
//
// govpx carries x->variance_low from choose_partitioning so the
// force_skip_low_temp_var branch is evaluated here instead of treated as a
// picker-local heuristic. The scene-change / source-SAD content-state signals
// remain false unless their upstream libvpx state has been populated.
//
// libvpx: vp9_pickmode.c:1055-1096 (estimate_block_intra), vp9_pickmode.c:
// 1717-1720 (intra_cost_penalty + inter_mode_thresh), vp9_pickmode.c:2566
// (intra_mode_list loop), vp9_pickmode.c:2607-2647 (per-mode score +
// best-rdc update).
func (e *VP9Encoder) vp9NonrdEstimateIntraFallback(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, qindex int,
	above, left *vp9dec.NeighborMi,
	sourceVariance uint, bestInterScore uint64, forceSkipLowTempVar bool, xSkip bool,
	pickPred []byte, pickPredStride, pickPredOriginMiRow, pickPredOriginMiCol int,
	skipEncode bool,
) (vp9InterIntraDecision, bool) {
	if inter == nil || inter.img == nil {
		return vp9InterIntraDecision{}, false
	}
	// libvpx vp9_pickmode.c:1182 — assert(bsize >= BLOCK_8X8). The
	// intra-fallback section runs at the same bsize as the inter picker,
	// which the partition driver guarantees is >= BLOCK_8X8 in the
	// nonrd path (vp9_encodeframe.c::nonrd_pick_sb_modes).
	if bsize < common.Block8x8 || bsize >= common.BlockSizes {
		return vp9InterIntraDecision{}, false
	}
	// libvpx vp9_pickmode.c:2533 — bsize <= cpi->sf.max_intra_bsize gate.
	maxIntraBsize := e.sf.MaxIntraBsize
	if maxIntraBsize <= 0 || maxIntraBsize >= common.BlockSizes {
		maxIntraBsize = common.Block64x64
	}
	if bsize > maxIntraBsize {
		return vp9InterIntraDecision{}, false
	}
	contentState := vp9ContentStateInvalid
	if state, ok := e.vp9SourceSADContentState(inter.img, miRows, miCols,
		miRow, miCol); ok {
		contentState = state
	}

	// libvpx vp9_pickmode.c:1717-1720 — intra_cost_penalty seeds an
	// inter_mode_thresh = RDCOST(rdmult, rddiv, intra_cost_penalty, 0).
	// Intra-fallback runs only when best_rdc.rdcost > inter_mode_thresh
	// (vp9_pickmode.c:2532) — i.e. inter is not "already good enough" to
	// skip the intra sweep. govpx ports vp9_get_intra_cost_penalty
	// verbatim from vp9_rd.c:778-794.
	intraCostPenalty := vp9GetIntraCostPenalty(qindex, 0, bsize,
		e.noiseEstimate.enabled, e.noiseEstimate.extractLevel())
	rdmult := e.activeRDMult(qindex)
	interModeThresh := encoder.RDCost(rdmult, encoder.RDDivBits, intraCostPenalty, 0)
	screenFlat := e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) &&
		sourceVariance == 0
	if !vp9NonrdIntraFallbackPrecheck(bestInterScore, interModeThresh,
		forceSkipLowTempVar, bsize, contentState, xSkip, e.rc.highSourceSAD,
		screenFlat) {
		// libvpx: the gate at vp9_pickmode.c:2527-2534 also fires when
		// best_rdc.rdcost == INT64_MAX (no inter winner). The caller
		// invokes this helper only after an inter winner exists, so that
		// branch remains outside this helper.
		return vp9InterIntraDecision{}, false
	}

	// libvpx vp9_pickmode.c:2539-2541 — intra_tx_size selection.
	intraTxSize := common.MaxTxsizeLookup[bsize]
	// libvpx reads cpi->common.tx_mode here; govpx derives the same
	// biggest tx via TxModeToBiggestTxSize using the live frame tx_mode.
	frameTxMode := e.vp9EncoderFrameTxMode(false, false, inter.lossless)
	biggestTx := common.TxModeToBiggestTxSize[frameTxMode]
	if biggestTx < intraTxSize {
		intraTxSize = biggestTx
	}

	// libvpx vp9_rd.c:103 fills cpi->mbmode_cost from
	// fc->y_mode_prob[1], and the nonrd intra fallback consumes that table
	// directly at vp9_pickmode.c:2631.
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], vp9InterModeCostFrameContext(inter).YModeProb[1][:],
		common.IntraModeTree[:])

	// libvpx vp9_pickmode.c:1232-1234 — ref_frame_cost[INTRA_FRAME] =
	// vp9_cost_bit(intra_inter_p, 0). govpx ports the same via
	// vp9IntraInterRateCost with isInter=0.
	refRateIntra := vp9IntraInterRateCost(&inter.selectFc, above, left, 0)

	// libvpx vp9_pickmode.c:1718-1720 — skip-cost contribution. The
	// per-mode (rate, dist) tuple adds skip-on or skip-off depending on
	// whether the per-mode block_yrd flagged the candidate as
	// skippable.
	skipCtx := vp9dec.GetSkipContext(above, left)
	var skipProb uint8
	if skipCtx >= 0 && skipCtx < len(e.fc.SkipProbs) {
		skipProb = e.fc.SkipProbs[skipCtx]
	}
	skipBitOn := encoder.VP9CostBit(skipProb, 1)
	skipBitOff := encoder.VP9CostBit(skipProb, 0)

	// libvpx vp9_pickmode.c:2566 intra_mode_list loop.
	intraMaskBits := vp9KeyframeIntraModeMask(&e.sf, bsize)
	bestSet := false
	var best vp9InterIntraDecision

	// libvpx-faithful per-mode evaluation. Build the keyframe-like
	// state once (mirrors the same hdr-from-opts construction used by
	// pickVP9InterIntraModeCore at vp9_encoder.go:9747-9756).
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
	}
	keyLike := vp9KeyframeEncodeState{
		img:      inter.img,
		hdr:      &hdr,
		dq:       inter.dq,
		lossless: inter.lossless,
	}
	mi := vp9dec.NeighborMi{
		SbType: bsize,
		TxSize: intraTxSize,
	}
	dequantY := [2]int16{}
	if inter.dq != nil {
		dequantY = inter.dq.Y[0]
	}
	useSimpleIntraBlockYrd := e.sf.UseSimpleBlockYrd != 0 &&
		bsize < common.Block32x32

	for _, thisMode := range vp9NonrdIntraModeList {
		// libvpx vp9_pickmode.c:2578 — intra_y_mode_bsize_mask gate.
		if intraMaskBits&(1<<uint(thisMode)) == 0 {
			continue
		}
		// libvpx vp9_pickmode.c:2612-2614.
		if e.sf.RtIntraDcOnlyLowContent != 0 &&
			thisMode != common.DcPred &&
			contentState != vp9ContentStateVeryHighSad {
			continue
		}
		modeOffset := vp9ModeOffsetInterOrIntra(thisMode)
		if modeOffset < 0 {
			continue
		}
		modeIndex := vp9ModeIdxTable[vp9dec.IntraFrame][modeOffset]
		modeRdThresh := e.rdThresh.threshes[bsize][modeIndex]
		if vp9RDLessThanThresh(bestInterScore, modeRdThresh,
			e.rdThresh.threshFreqFact[bsize][modeIndex]) &&
			e.opts.ScreenContentMode != int8(VP9ScreenContentScreen) {
			continue
		}
		// libvpx vp9_pickmode.c:2607-2611 — compute_intra_yprediction,
		// model_rd_for_sb_y, then block_yrd. For speed-8 non-key blocks
		// below 32x32, block_yrd's use_simple_block_yrd branch returns
		// immediately after model_rd_for_sb_y with skippable=0
		// (vp9_pickmode.c:747-758), so do not run the transform RD kernel
		// in that case.
		mi.Mode = thisMode
		txYrd := min(intraTxSize, common.Tx16x16)
		mi.TxSize = txYrd
		var distortion uint64
		coeffRate := 0
		skippable := false
		var sse, variance uint64
		if useSimpleIntraBlockYrd {
			var ok bool
			if len(pickPred) != 0 && pickPredStride > 0 {
				// libvpx: compute_intra_yprediction reads and writes the live
				// pd->dst surface that reuse_inter_pred_sby maintains for this
				// SB. When x->skip_encode is set, libvpx takes the intra
				// predictor reference edges from the source plane instead.
				if skipEncode {
					src, srcStride, _, _ := vp9EncoderSourcePlane(inter.img, 0)
					sse, variance, ok = e.vp9NoReferenceIntraResidualStatsScratchRefNoRestore(
						&keyLike, thisMode, intraTxSize, tile, miRows, miCols,
						miRow, miCol, bsize, pickPred, pickPredStride,
						pickPredOriginMiRow, pickPredOriginMiCol,
						src, srcStride, 0, 0)
				} else {
					sse, variance, ok = e.vp9NoReferenceIntraResidualStatsScratchNoRestore(
						&keyLike, thisMode, intraTxSize, tile, miRows, miCols,
						miRow, miCol, bsize, pickPred, pickPredStride,
						pickPredOriginMiRow, pickPredOriginMiCol)
				}
			}
			if !ok {
				sse, variance, ok = e.vp9NoReferenceIntraResidualStatsNoRestore(&keyLike,
					thisMode, intraTxSize, tile, miRows, miCols, miRow, miCol, bsize)
			}
			if !ok {
				continue
			}
			rateY, distY, _, _ := encoder.ModelRdForSbY(bsize, qindex, dequantY,
				variance, sse, 1)
			coeffRate = rateY
			distortion = uint64(distY)
		} else {
			var ok bool
			distortion, coeffRate, skippable, ok = e.scoreVP9KeyframeModeTransformRD(
				&keyLike, thisMode, tile, miRows, miCols, miRow, miCol, bsize, &mi)
			if !ok {
				continue
			}
		}

		// libvpx vp9_pickmode.c:2615-2621 — skip-cost vs non-skip path.
		// govpx mirrors: skippable picks skip_on with rate=0 (no coeff
		// rate), else add coeff_rate + skip_off. The simple block_yrd
		// branch above forces skippable=false, exactly as libvpx does.
		var rate int
		if skippable {
			rate = skipBitOn
		} else {
			rate = coeffRate + skipBitOff
		}

		// libvpx vp9_pickmode.c:2631-2633 — final rate = mbmode_cost +
		// ref_frame_cost[INTRA_FRAME] + intra_cost_penalty + (coeff
		// rate + skip-bit).
		rate += yModeCosts[thisMode]
		rate += refRateIntra
		rate += intraCostPenalty

		// libvpx vp9_pickmode.c:2634-2635 — this_rdc.rdcost =
		// RDCOST(x->rdmult, x->rddiv, this_rdc.rate, this_rdc.dist).
		score := encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion)
		if !bestSet || score < best.score {
			best = vp9InterIntraDecision{
				mode:   thisMode,
				uvMode: thisMode,
				txSize: intraTxSize,
				rate:   rate,
				score:  score,
			}
			bestSet = true
		}
	}
	// Note: libvpx's non-luma walk (vp9_pickmode.c:2622-2630) only fires
	// for VP9E_CONTENT_SCREEN with color_sensitivity set, which govpx
	// does not yet surface; the Y-only path here is libvpx-faithful for
	// all other configurations.
	if !bestSet || best.score >= bestInterScore {
		return vp9InterIntraDecision{}, false
	}
	return best, true
}

// vp9NonrdIntraModeList mirrors libvpx's intra_mode_list (vp9_pickmode.c:
// 1105-1106) — the realtime nonrd intra-fallback walks {DC_PRED, V_PRED,
// H_PRED, TM_PRED} in that order.
var vp9NonrdIntraModeList = [4]common.PredictionMode{
	common.DcPred,
	common.VPred,
	common.HPred,
	common.TmPred,
}

// vp9GetIntraCostPenalty ports vp9_get_intra_cost_penalty (vp9_rd.c:
// 778-795) verbatim. The reduction factor halves the penalty for
// BLOCK_16X16 and quarters it for BLOCK_8X8 / smaller unless the live noise
// estimate is kHigh.
//
// libvpx:
//
//	int vp9_get_intra_cost_penalty(const VP9_COMP *const cpi, BLOCK_SIZE bsize,
//	                               int qindex, int qdelta) {
//	  int reduction_fac =
//	      (bsize <= BLOCK_16X16) ? ((bsize <= BLOCK_8X8) ? 4 : 2) : 0;
//	  if (cpi->noise_estimate.enabled && cpi->noise_estimate.level == kHigh)
//	    reduction_fac = 0;
//	  return (20 * vp9_dc_quant(qindex, qdelta, VPX_BITS_8)) >> reduction_fac;
//	}
func vp9GetIntraCostPenalty(qindex, qdelta int, bsize common.BlockSize,
	noiseEstimateEnabled bool, noiseLevel vp9NoiseLevel,
) int {
	reductionFac := 0
	if bsize <= common.Block16x16 {
		if bsize <= common.Block8x8 {
			reductionFac = 4
		} else {
			reductionFac = 2
		}
	}
	if noiseEstimateEnabled && noiseLevel == vp9NoiseLevelHigh {
		reductionFac = 0
	}
	dcQ := int(vp9dec.VpxDcQuant(qindex, qdelta, vp9dec.BitDepth8))
	return (20 * dcQ) >> reductionFac
}

func (e *VP9Encoder) vp9NewmvDiffBiasNoiseInputs() (bool, bool) {
	if e == nil || !e.noiseEstimate.enabled {
		return false, false
	}
	return true, e.noiseEstimate.extractLevel() >= vp9NoiseLevelMedium
}

func vp9NewmvDiffBiasLowvarInput(contentState vp9ContentStateSB) bool {
	return contentState == vp9ContentStateLowVarHighSumdiff
}

// vp9NeighborIsInter mirrors libvpx's is_inter_block(MODE_INFO *mi) helper.
//
// libvpx: vp9_blockd.h is_inter_block — ref_frame[0] > INTRA_FRAME.
func vp9NeighborIsInter(mi *vp9dec.NeighborMi) bool {
	if mi == nil {
		return false
	}
	return mi.RefFrame[0] > vp9dec.IntraFrame
}
