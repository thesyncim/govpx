package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func (e *VP9Encoder) pickVP9KeyframeMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, txMode common.TxMode,
) common.PredictionMode {
	if key == nil || mi == nil {
		return common.DcPred
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	yModeProbs := vp9dec.GetYModeProbs(mi, above, left, 0)
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], yModeProbs, common.IntraModeTree[:])
	qindex := e.vp9EncoderModeDecisionQIndex()
	// Apply libvpx's per-SB TPL rdmult scaling.  The base rdmult is the
	// keyframe Lagrange multiplier encoder.KeyframeRDMul(qindex); TPL biases
	// it via get_rdmult_delta clamped to [orig/2, orig*3/2] before
	// running the per-mode RD search.
	// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248
	rdmult := encoder.KeyframeRDMul(qindex)
	if e.tpl.Enabled && bsize < common.BlockSizes {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, rdmult)
	}
	// Prime cb_rdmult so the UV intra and inter-intra scorers downstream
	// see the same TPL-biased multiplier instead of re-deriving it.
	// Inline save/restore (no defer) preserves the alloc-parity gate.
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	// The candidate set follows libvpx's keyframe hybrid picker. The RD arm
	// (`vp9_rd_pick_intra_mode_sb` -> `rd_pick_intra_sby_mode`) walks
	// DC_PRED..TM_PRED unconditionally; there is no
	// `intra_y_mode_(_bsize)_mask` gate on the keyframe Y picker. The non-RD
	// arm (`vp9_pick_intra_mode`) walks DC_PRED..H_PRED only. libvpx selects
	// the non-RD arm when `sf.use_nonrd_pick_mode` is active and either
	// `sf.nonrd_keyframe` is set or the block is at least BLOCK_16X16.
	// govpx mirrors that with useVP9KeyframeNonRDIntraMode. The previous
	// mask-based fallback walked only the {DC, V, H} subset on GOOD speed=1
	// because the configurator did not populate `IntraYModeBsizeMask` for the
	// GOOD path, which violated libvpx parity for cpu_used 0..4 GOOD-mode
	// keyframes (see vp9OptionsSeedsDeferred regression_vp9_options_e03af0a9).
	//
	// libvpx: vp9/encoder/vp9_rdopt.c:1383 (rd_pick_intra_sby_mode loop)
	// libvpx: vp9/encoder/vp9_pickmode.c:1199 (vp9_pick_intra_mode loop)
	// libvpx: vp9/encoder/vp9_encodeframe.c:4350-4365 (hybrid dispatch
	// between the two pickers)
	if e.useVP9KeyframeNonRDIntraMode(bsize) {
		bestMode := common.DcPred
		bestScore, ok := e.scoreVP9KeyframeModeNonRD(key, bestMode,
			yModeCosts[bestMode], rdmult, tile, miRows, miCols, miRow, miCol,
			bsize, mi)
		if !ok {
			e.cbRdmult = prevCbRdmult
			return bestMode
		}
		for mode := common.DcPred + 1; mode <= common.HPred; mode++ {
			score, ok := e.scoreVP9KeyframeModeNonRD(key, mode,
				yModeCosts[mode], rdmult, tile, miRows, miCols, miRow, miCol,
				bsize, mi)
			if ok && score < bestScore {
				bestScore = score
				bestMode = mode
			}
		}
		e.cbRdmult = prevCbRdmult
		return bestMode
	}
	bestMode := common.DcPred
	bestScore, ok := e.scoreVP9KeyframeModeRD(key, bestMode,
		yModeCosts[bestMode], rdmult, tile, miRows, miCols, miRow, miCol,
		bsize, mi, txMode)
	if !ok {
		e.cbRdmult = prevCbRdmult
		return bestMode
	}
	bestTx := mi.TxSize
	for mode := common.DcPred + 1; mode <= common.TmPred; mode++ {
		score, ok := e.scoreVP9KeyframeModeRD(key, mode, yModeCosts[mode],
			rdmult, tile, miRows, miCols, miRow, miCol, bsize, mi, txMode)
		if ok && score < bestScore {
			bestScore = score
			bestMode = mode
			bestTx = mi.TxSize
		}
	}
	mi.TxSize = bestTx
	e.cbRdmult = prevCbRdmult
	return bestMode
}

func (e *VP9Encoder) pickVP9KeyframeYModeRD(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, txMode common.TxMode,
	rdmult int, refBestRD uint64,
) (vp9KeyframeIntraRD, bool) {
	if key == nil || mi == nil || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return vp9KeyframeIntraRD{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	yModeProbs := vp9dec.GetYModeProbs(mi, above, left, 0)
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], yModeProbs, common.IntraModeTree[:])

	best := vp9KeyframeIntraRD{mode: common.DcPred}
	bestTx := mi.TxSize
	bestScore := refBestRD
	bestValid := false
	origMode := mi.Mode
	origTx := mi.TxSize
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if e.sf.UseNonrdPickMode != 0 {
			if vp9ConditionalSkipIntra(mode, best.mode) {
				continue
			}
			if bestValid && best.skippable {
				break
			}
		}
		mi.Mode = mode
		var cand vp9KeyframeIntraRD
		if e.sf.TxSizeSearchMethod == UseFullRD && e.vp9KeyframeRDRefinementEnabled() {
			txRD, ok := e.chooseVP9KeyframeModeTxRDWithBest(key, mode,
				rdmult, tile, miRows, miCols, miRow, miCol, bsize, mi,
				txMode, bestScore)
			if !ok {
				continue
			}
			cand = vp9KeyframeIntraRD{
				mode:          mode,
				rate:          yModeCosts[mode] + txRD.rate,
				rateTokenOnly: txRD.rateTokenOnly,
				distortion:    txRD.distortion,
				skippable:     txRD.skippable,
			}
		} else {
			distortion, coeffRate, skippable, ok := e.scoreVP9KeyframeModeTransformRD(
				key, mode, tile, miRows, miCols, miRow, miCol, bsize, mi)
			if !ok {
				continue
			}
			cand = vp9KeyframeIntraRD{
				mode:          mode,
				rate:          yModeCosts[mode] + coeffRate,
				rateTokenOnly: coeffRate,
				distortion:    distortion,
				skippable:     skippable,
			}
		}
		score := encoder.RDCost(rdmult, encoder.RDDivBits, cand.rate, cand.distortion)
		if !bestValid || score < bestScore {
			best = cand
			bestTx = mi.TxSize
			bestScore = score
			bestValid = true
		}
	}
	if !bestValid {
		mi.Mode = origMode
		mi.TxSize = origTx
		return vp9KeyframeIntraRD{}, false
	}
	mi.Mode = best.mode
	mi.TxSize = bestTx
	return best, true
}

func (e *VP9Encoder) useVP9KeyframeNonRDIntraMode(bsize common.BlockSize) bool {
	return e.sf.UseNonrdPickMode != 0 &&
		(e.sf.NonrdKeyframe != 0 || bsize >= common.Block16x16)
}

// pickVP9KeyframeSub8x8YMode ports libvpx's rd_pick_intra_sub_8x8_y_mode
// (vp9/encoder/vp9_rdopt.c:1299-1360) plus the per-subblock walker
// rd_pick_intra4x4block (vp9_rdopt.c:1061-1297). For BLOCK_4X4 / BLOCK_4X8 /
// BLOCK_8X4 keyframe partitions, the libvpx Y-mode picker walks the 2x2
// grid of 4x4 raster sub-blocks (stepped by num_4x4_blocks_{wide,high})
// and runs an independent DC_PRED..TM_PRED RD scan per sub-block. The
// chosen per-subblock mode lands in mic->bmi[i].as_mode (replicated
// across the num_4x4_blocks_{wide,high} cells the decision covers); the
// final mic->mode = mic->bmi[3].as_mode so write_modes_b / coef_sb pick
// up the per-block mode for sub-8x8 partitions via get_y_mode.
//
// The previous govpx behaviour reused pickVP9KeyframeMode for sub-8x8
// blocks, which left all Bmi[].AsMode entries at the default DC_PRED
// regardless of the picked block-level mode — divergent from libvpx
// whenever the per-subblock RD picker selects a non-DC mode for any
// 4x4 raster cell.
func (e *VP9Encoder) pickVP9KeyframeSub8x8YMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, refBestRD uint64,
) (vp9KeyframeIntraRD, bool) {
	if key == nil || mi == nil {
		return vp9KeyframeIntraRD{}, false
	}
	if bsize >= common.Block8x8 {
		return vp9KeyframeIntraRD{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// Mirror libvpx vp9_encodeframe.c:4245-4248 — TPL bias the rdmult so
	// the per-subblock RD compares under the same multiplier as the
	// 8x8+ keyframe picker.
	rdmult := encoder.KeyframeRDMul(qindex)
	bwMi := int(common.Num8x8BlocksWideLookup[bsize])
	bhMi := int(common.Num8x8BlocksHighLookup[bsize])
	if e.tpl.Enabled {
		rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, rdmult)
	}
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	// Pin tx_size at TX_4X4 for the sub-8x8 picker, matching libvpx
	// rd_pick_intra4x4block:1088 — `xd->mi[0]->tx_size = TX_4X4`.
	mi.TxSize = common.Tx4x4
	pd := &e.planes[0]
	var aboveCtx, leftCtx [2]uint8
	if len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if off := aboveOffsets[0]; off >= 0 && off+2 <= len(pd.AboveContext) {
			copy(aboveCtx[:], pd.AboveContext[off:off+2])
		}
		if off := leftOffsets[0]; off >= 0 && off+2 <= len(pd.LeftContext) {
			copy(leftCtx[:], pd.LeftContext[off:off+2])
		}
	}
	var total vp9KeyframeIntraRD
	totalRD := uint64(0)
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			i := idy*2 + idx
			// libvpx vp9_rdopt.c:1325-1330 — for keyframe blocks the
			// per-subblock bmode_costs row is keyed by (above_block_mode,
			// left_block_mode). govpx's GetYModeProbs encapsulates the same
			// kf_y_mode_prob[A][L] lookup, fed into VP9CostTokens to expand
			// per-mode rates.
			probs := vp9dec.GetYModeProbs(mi, above, left, i)
			var bmodeCosts [common.IntraModes]int
			encoder.VP9CostTokens(bmodeCosts[:], probs, common.IntraModeTree[:])
			remainingRD := uint64(^uint64(0))
			if refBestRD != ^uint64(0) {
				if totalRD >= refBestRD {
					e.cbRdmult = prevCbRdmult
					return vp9KeyframeIntraRD{}, false
				}
				remainingRD = refBestRD - totalRD
			}
			bestMode, rd, ok := e.pickVP9Sub4x4IntraBlockMode(key, tile, miRows, miCols,
				miRow, miCol, bsize, mi, idy, idx, bmodeCosts[:], rdmult,
				aboveCtx[idx:idx+num4x4W], leftCtx[idy:idy+num4x4H], remainingRD)
			if !ok {
				e.cbRdmult = prevCbRdmult
				return vp9KeyframeIntraRD{}, false
			}
			thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, rd.rate, rd.distortion)
			if remainingRD != ^uint64(0) && thisRD >= remainingRD {
				e.cbRdmult = prevCbRdmult
				return vp9KeyframeIntraRD{}, false
			}
			totalRD += thisRD
			// Replicate best_mode into the bmi cells the sub-block
			// decision covers (libvpx vp9_rdopt.c:1344-1348).
			mi.Bmi[i].AsMode = bestMode
			total.rate += rd.rate
			total.rateTokenOnly += rd.rateTokenOnly
			total.distortion += rd.distortion
			for j := 1; j < num4x4H; j++ {
				mi.Bmi[i+j*2].AsMode = bestMode
			}
			for j := 1; j < num4x4W; j++ {
				mi.Bmi[i+j].AsMode = bestMode
			}
			if refBestRD != ^uint64(0) && totalRD >= refBestRD {
				e.cbRdmult = prevCbRdmult
				return vp9KeyframeIntraRD{}, false
			}
		}
	}
	// libvpx vp9_rdopt.c:1357 — `mic->mode = mic->bmi[3].as_mode` so
	// downstream consumers (write_mb_modes_kf coef_sb get_y_mode) read
	// the per-subblock mode through Bmi[] while leaving mi.Mode as the
	// bottom-right subblock's pick.
	mi.Mode = mi.Bmi[3].AsMode
	e.cbRdmult = prevCbRdmult
	total.mode = mi.Mode
	return total, true
}

// pickVP9Sub4x4IntraBlockMode ports libvpx's rd_pick_intra4x4block
// (vp9/encoder/vp9_rdopt.c:1061-1297). For one 4x4-grid raster sub-block
// at (idy,idx) inside a BLOCK_4X4 / 4X8 / 8X4 partition, it scans
// DC_PRED..TM_PRED, scoring each candidate via the same RD primitives
// the keyframe block picker uses (predict at TX_4X4 then quantise + RD
// cost the Hadamard-domain residue) and returns the lowest-RD mode. The
// best mode's prediction is left on the recon plane so subsequent
// sub-blocks see the correct intra-pred neighbours; the recon outside
// the {num_4x4_blocks_wide_lookup, num_4x4_blocks_high_lookup} footprint
// of this sub-block is preserved via a snapshot/restore mirroring
// libvpx's `best_dst[]` save (vp9_rdopt.c:1081-1085 + 1280-1294).
func (e *VP9Encoder) pickVP9Sub4x4IntraBlockMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
	idy, idx int, bmodeCosts []int, rdmult int, aboveCtx, leftCtx []uint8,
	rdThresh uint64,
) (common.PredictionMode, vp9KeyframeIntraRD, bool) {
	pd := &e.planes[0]
	planeData, stride := e.vp9EncoderReconPlane(0)
	if len(planeData) == 0 || stride <= 0 {
		return common.DcPred, vp9KeyframeIntraRD{}, false
	}
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	// Snapshot the sub-block's recon rect so per-candidate predictions
	// can be undone before re-trying the next mode. Footprint matches
	// libvpx vp9_rdopt.c:1081 — `uint8_t best_dst[8 * 8]` covering
	// num_4x4_blocks_{wide,high}*4 pixels.
	baseX := miCol*common.MiSize + idx*4
	baseY := miRow*common.MiSize + idy*4
	rectW := num4x4W * 4
	rectH := num4x4H * 4
	rows := len(planeData) / stride
	if baseX < 0 || baseY < 0 || baseX+rectW > stride || baseY+rectH > rows {
		return common.DcPred, vp9KeyframeIntraRD{}, false
	}
	rectPixels := rectW * rectH
	if rectPixels*2 > len(e.blockScratch) {
		return common.DcPred, vp9KeyframeIntraRD{}, false
	}
	saved := e.blockScratch[:rectPixels]
	bestDst := e.blockScratch[rectPixels : 2*rectPixels]
	for y := range rectH {
		copy(saved[y*rectW:(y+1)*rectW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+rectW])
	}
	segID := vp9EncoderMiSegmentID(mi)
	dequant := key.dq.Y[segID]
	// Mode-mask gate mirrors libvpx vp9_rdopt.c:1207 —
	// `if (!(cpi->sf.intra_y_mode_mask[TX_4X4] & (1 << mode))) continue;`.
	mask := sfIntraAll
	if int(common.Tx4x4) < len(e.sf.IntraYModeMask) && e.sf.IntraYModeMask[common.Tx4x4] != 0 {
		mask = e.sf.IntraYModeMask[common.Tx4x4]
	}
	// libvpx vp9_rdopt.c:1148-1149, 1237-1238 — coeff_ctx =
	// combine_entropy_contexts(tempa[idx], templ[idy]) per 4x4
	// sub-block, with tempa/templ rebooted from the SB context arrays
	// at the start of each candidate mode. Since this helper operates
	// on one (idy,idx) sub-block of the 8x8 (or 4x8/8x4) partition,
	// govpx threads tempa/templ across the inner per-block grid.
	qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, segID,
		int(key.hdr.Quant.BaseQindex))
	maxEob := vp9dec.MaxEobForTxSize(common.Tx4x4)
	bestMode := common.DcPred
	bestRD := rdThresh
	bestValid := false
	bestBlockRD := vp9KeyframeIntraRD{}
	var ta, tl, bestA, bestL [2]uint8
	for i := 0; i < num4x4W && i < len(aboveCtx) && i < len(ta); i++ {
		ta[i] = aboveCtx[i]
	}
	for i := 0; i < num4x4H && i < len(leftCtx) && i < len(tl); i++ {
		tl[i] = leftCtx[i]
	}
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if mask&(1<<mode) == 0 {
			continue
		}
		if e.sf.ModeSearchSkipFlags&FlagSkipIntraDirMismatch != 0 &&
			vp9ConditionalSkipIntra(mode, bestMode) {
			continue
		}
		// Restore the saved recon so this candidate's prediction starts
		// from the same neighbour state as the previous candidate
		// (libvpx vp9_rdopt.c:1108-1109 — `memcpy(tempa,...);
		// memcpy(templ,...);` before each per-mode pass).
		vp9RestorePlaneRect(planeData, stride, baseX, baseY, rectW, rectH, saved)
		rate := bmodeCosts[mode]
		var totalDistortion uint64
		var totalCoeffRate int
		valid := true
		// libvpx vp9_rdopt.c:1108-1109 — per-candidate tempa/templ
		// refresh; govpx tracks the per-sub-block residue flag with a
		// local 2-cell strip per axis (the libvpx 8x8 partition only
		// holds 2 num_4x4_blocks in either direction).
		var tempa, templ [2]uint8
		copy(tempa[:], ta[:])
		copy(templ[:], tl[:])
		for jy := 0; jy < num4x4H && valid; jy++ {
			for jx := 0; jx < num4x4W && valid; jx++ {
				src, srcStride, _, _ := vp9EncoderSourcePlane(key.img, 0)
				if len(src) == 0 || srcStride <= 0 {
					valid = false
					break
				}
				coeffs := e.coefScratch[:maxEob]
				qcoeffs := e.qCoefScratch[:maxEob]
				for i := range coeffs {
					coeffs[i] = 0
					qcoeffs[i] = 0
				}
				// libvpx vp9_rdopt.c:1124-1167 — predict_intra +
				// subtract + fdct/fht4x4 + quantize_b + cost_coeffs.
				// govpx folds these into prepareVP9KeyframeTxResidueWithQ,
				// then scores the transform-domain reconstruction error
				// against txCoeffScratch/dqCoeffScratch just like
				// vp9_block_error(coeff, dqcoeff, 16, &unused) >> 2 at
				// vp9_rdopt.c:1261-1263.
				hasResidue := e.prepareVP9KeyframeTxResidueWithQ(key, pd, 0, mode,
					common.Tx4x4, tile, miRows, miCols, miRow, miCol, bsize,
					idy+jy, idx+jx, dequant, qindex, coeffs, qcoeffs)
				totalDistortion += vp9TransformBlockErrorShifted(
					e.txCoeffScratch[:maxEob], e.dqCoeffScratch[:maxEob])
				// libvpx vp9_rdopt.c:1148-1149 + 1156-1157 — coeff_ctx
				// = combine_entropy_contexts(tempa[idx], templ[idy]);
				// ratey += cost_coeffs(... coeff_ctx ...). govpx
				// dispatches to vp9KeyframeCoeffBlockRateCostQ which
				// reads v = qcoeff[rc] directly (vp9_rdopt.c:392,405).
				initCtx := vp9dec.GetEntropyContext(common.Tx4x4,
					tempa[jx:jx+1], templ[jy:jy+1])
				totalCoeffRate += e.vp9KeyframeCoeffBlockRateCostQ(
					common.Tx4x4, mode, key.lossless, dequant, coeffs, qcoeffs, initCtx)
				// libvpx vp9_rdopt.c:1162, 1244 — tempa[idx] =
				// templ[idy] = (eobs[block] > 0) ? 1 : 0.
				eobFlag := uint8(0)
				if hasResidue {
					eobFlag = 1
				}
				tempa[jx] = eobFlag
				templ[jy] = eobFlag
				if encoder.RDCost(rdmult, encoder.RDDivBits, totalCoeffRate,
					totalDistortion) >= bestRD {
					valid = false
					break
				}
			}
		}
		if !valid {
			continue
		}
		rate += totalCoeffRate
		thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, rate, totalDistortion)
		if thisRD < bestRD {
			bestRD = thisRD
			bestMode = mode
			bestBlockRD = vp9KeyframeIntraRD{
				mode:          mode,
				rate:          rate,
				rateTokenOnly: totalCoeffRate,
				distortion:    totalDistortion,
			}
			bestValid = true
			for y := range rectH {
				copy(bestDst[y*rectW:(y+1)*rectW],
					planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+rectW])
			}
			copy(bestA[:], tempa[:])
			copy(bestL[:], templ[:])
		}
	}
	// Leave the best mode's reconstructed pixels on the recon plane (libvpx
	// vp9_rdopt.c:1292-1294 copies best_dst, which already includes the
	// inverse-transform add for the winning mode). This is important for
	// the following 4x4 sub-block's intra neighbours.
	if bestValid {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY, rectW, rectH, bestDst)
		for i := 0; i < num4x4W && i < len(aboveCtx) && i < len(bestA); i++ {
			aboveCtx[i] = bestA[i]
		}
		for i := 0; i < num4x4H && i < len(leftCtx) && i < len(bestL); i++ {
			leftCtx[i] = bestL[i]
		}
	} else {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY, rectW, rectH, saved)
	}
	return bestMode, bestBlockRD, bestValid
}

func vp9ConditionalSkipIntra(mode, bestMode common.PredictionMode) bool {
	switch mode {
	case common.D117Pred:
		return bestMode != common.VPred && bestMode != common.D135Pred
	case common.D63Pred:
		return bestMode != common.VPred && bestMode != common.D45Pred
	case common.D207Pred:
		return bestMode != common.HPred && bestMode != common.D45Pred
	case common.D153Pred:
		return bestMode != common.HPred && bestMode != common.D135Pred
	default:
		return false
	}
}

// vp9KeyframeIntraModeMask returns the libvpx `intra_y_mode_bsize_mask`
// entry the nonrd inter-frame intra picker consults. The keyframe Y-mode
// picker itself does NOT consult this mask — libvpx's keyframe RD path
// (`rd_pick_intra_sby_mode`, vp9_rdopt.c:1383) walks all 10 modes
// unconditionally, and the nonrd keyframe path (`vp9_pick_intra_mode`,
// vp9_pickmode.c:1199) walks DC..H_PRED unconditionally; govpx mirrors
// that dispatch via `e.sf.NonrdKeyframe` inside pickVP9KeyframeMode. This
// helper survives for the nonrd inter-frame intra picker
// (vp9_pickmode.c:2578) which the govpx nonrd picker still TODO-defers
// inside the consumers file, and for the audit test pinning the
// configurator-populated narrow mask semantics.
//
// libvpx: vp9/encoder/vp9_pickmode.c:2578 — `(1 << this_mode) &
// cpi->sf.intra_y_mode_bsize_mask[bsize]`.
func vp9KeyframeIntraModeMask(sf *SpeedFeatures, bsize common.BlockSize) int {
	if sf == nil || int(bsize) >= len(sf.IntraYModeBsizeMask) {
		return sfIntraDCHV
	}
	mask := sf.IntraYModeBsizeMask[bsize]
	if mask == 0 {
		return sfIntraDCHV
	}
	return mask
}

type vp9KeyframeTxRDResult struct {
	txSize        common.TxSize
	rate          int
	rateTokenOnly int
	distortion    uint64
	skippable     bool
}

func (e *VP9Encoder) chooseVP9KeyframeModeTxRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rdmult int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, txMode common.TxMode,
) (vp9KeyframeTxRDResult, bool) {
	return e.chooseVP9KeyframeModeTxRDWithBest(key, mode, rdmult, tile,
		miRows, miCols, miRow, miCol, bsize, mi, txMode, ^uint64(0))
}

func (e *VP9Encoder) chooseVP9KeyframeModeTxRDWithBest(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rdmult int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, txMode common.TxMode, refBestRD uint64,
) (vp9KeyframeTxRDResult, bool) {
	if key == nil || key.dq == nil || mi == nil || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return vp9KeyframeTxRDResult{}, false
	}
	maxTx := common.MaxTxsizeLookup[bsize]
	startTx := int(maxTx)
	endTx := startTx
	if txMode == common.TxModeSelect && !key.lossless &&
		e.sf.TxSizeSearchMethod == UseFullRD {
		endTx = max(startTx-e.sf.TxSizeSearchDepth, 0)
		if bsize > common.Block32x32 {
			endTx = min(endTx+1, startTx)
		}
	} else {
		chosen := maxTx
		if txMode < common.TxModes {
			chosen = min(common.TxModeToBiggestTxSize[txMode], maxTx)
		}
		if key.lossless {
			chosen = common.Tx4x4
		}
		startTx = int(chosen)
		endTx = int(chosen)
	}
	if startTx < endTx {
		return vp9KeyframeTxRDResult{}, false
	}

	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	skip0 := encoder.VP9CostBit(skipProb, 0)
	skip1 := encoder.VP9CostBit(skipProb, 1)

	origTx := mi.TxSize
	best := vp9KeyframeTxRDResult{txSize: common.TxSize(startTx)}
	bestScore := uint64(^uint64(0))
	bestValid := false
	prevScore := uint64(0)
	prevValid := false
	txRefBestRD := refBestRD
	for n := startTx; n >= endTx; n-- {
		tx := common.TxSize(n)
		mi.TxSize = tx
		distortion, coeffRate, skippable, ok := e.scoreVP9KeyframeModeTransformRDWithBest(
			key, mode, tile, miRows, miCols, miRow, miCol, bsize, mi,
			txRefBestRD)
		if !ok {
			if e.sf.TxSizeSearchBreakout != 0 {
				break
			}
			continue
		}
		txRate := 0
		if txMode == common.TxModeSelect {
			txRate = vp9TxSizeRateCost(txProbs, tx, maxTx)
		}
		rate := coeffRate + txRate
		scoreRate := rate + skip0
		if skippable {
			scoreRate = txRate + skip1
		}
		score := encoder.RDCost(rdmult, encoder.RDDivBits, scoreRate, distortion)
		if !bestValid || score < bestScore {
			best = vp9KeyframeTxRDResult{
				txSize:        tx,
				rate:          rate,
				rateTokenOnly: coeffRate,
				distortion:    distortion,
				skippable:     skippable,
			}
			bestScore = score
			bestValid = true
		}
		if score < txRefBestRD {
			txRefBestRD = score
		}
		if e.sf.TxSizeSearchBreakout != 0 &&
			((n < startTx && prevValid && score > prevScore) || skippable) {
			break
		}
		prevScore = score
		prevValid = true
	}
	mi.TxSize = origTx
	if !bestValid {
		return vp9KeyframeTxRDResult{}, false
	}
	mi.TxSize = best.txSize
	return best, true
}

// scoreVP9KeyframeModeRD computes the Lagrangian RD cost of a keyframe mode
// using an explicit rdmult.  The picker computes rdmult once per SB —
// optionally adjusted by the TPL per-SB delta (libvpx: vp9_encodeframe.c:4245
// -4248 wiring x->cb_rdmult from get_rdmult_delta) — and feeds it into every
// candidate score so all candidates are compared under the same multiplier.
func (e *VP9Encoder) scoreVP9KeyframeModeRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rate, rdmult int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, txMode common.TxMode,
) (uint64, bool) {
	if mi == nil {
		return 0, false
	}
	if e.sf.TxSizeSearchMethod != UseFullRD || !e.vp9KeyframeRDRefinementEnabled() {
		distortion, coeffRate, skippable, ok := e.scoreVP9KeyframeModeTransformRD(
			key, mode, tile, miRows, miCols, miRow, miCol, bsize, mi)
		if !ok {
			return 0, false
		}
		var left *vp9dec.NeighborMi
		if miCol > tile.MiColStart {
			left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
		}
		above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
		skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
		if skippable {
			rate += encoder.VP9CostBit(skipProb, 1)
		} else {
			rate += coeffRate + encoder.VP9CostBit(skipProb, 0)
		}
		return encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion), true
	}
	txRD, ok := e.chooseVP9KeyframeModeTxRD(key, mode, rdmult, tile,
		miRows, miCols, miRow, miCol, bsize, mi, txMode)
	if !ok {
		return 0, false
	}
	rate += txRD.rate
	return encoder.RDCost(rdmult, encoder.RDDivBits, rate, txRD.distortion), true
}

// scoreVP9KeyframeModeNonRD mirrors libvpx's realtime keyframe
// vp9_pick_intra_mode scorer. It predicts each transform unit, scores the
// residual with block_yrd's Hadamard + quantize_fp proxy, and compares the
// resulting RD tuple under the caller-supplied rdmult.
func (e *VP9Encoder) scoreVP9KeyframeModeNonRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rate, rdmult int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) (uint64, bool) {
	if key == nil || key.hdr == nil || key.img == nil || key.dq == nil || mi == nil {
		return 0, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return 0, false
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
		return 0, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	txSize := clampVP9TxSizeForBlock(mi.TxSize, bsize)
	yrdTxSize := min(txSize, common.Tx16x16)
	txBlockStep := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	segID := vp9EncoderMiSegmentID(mi)
	dequant := key.dq.Y[segID]
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	if max4x4W <= 0 || max4x4H <= 0 {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
		return 0, false
	}

	var coeffRate int
	var distortion uint64
	skippable := false
	visited := false
	for rr := 0; rr < max4x4H; rr += txBlockStep {
		for cc := 0; cc < max4x4W; cc += txBlockStep {
			_, _, x0, y0, ok := e.predictVP9KeyframeTx(key.hdr, pd, 0, mode,
				txSize, tile, miRows, miCols, miRow, miCol, bsize, rr, cc)
			if !ok {
				vp9RestorePlaneRect(planeData, stride, baseX, baseY,
					restoreW, restoreH, saved)
				return 0, false
			}
			yrdSrc := src
			yrdSrcStride := srcStride
			yrdSrcX := x0
			yrdSrcY := y0
			// vpxenc scores keyframe edge blocks against the extended source
			// border; mirror that here so partial bottom/right TUs stay valid.
			if x0 < 0 || y0 < 0 || x0+bs > srcW || y0+bs > srcH {
				if bs*bs > len(e.modeScratch) {
					vp9RestorePlaneRect(planeData, stride, baseX, baseY,
						restoreW, restoreH, saved)
					return 0, false
				}
				yrdSrc = e.modeScratch[:bs*bs]
				yrdSrcStride = bs
				yrdSrcX = 0
				yrdSrcY = 0
				for yy := range bs {
					sy := vp9ClampSourceCoord(y0+yy, srcH)
					srcRow := src[sy*srcStride:]
					dstRow := yrdSrc[yy*bs:]
					for xx := range bs {
						sx := vp9ClampSourceCoord(x0+xx, srcW)
						dstRow[xx] = srcRow[sx]
					}
				}
			}
			yrd := encoder.BlockYrd(yrdSrc, yrdSrcStride, yrdSrcX, yrdSrcY,
				planeData, stride, x0, y0, bs, bs, yrdTxSize, dequant,
				encoder.BlockYrdUnknownSSE, e.vp9BlockYrdScratch[:])
			if !yrd.Valid {
				vp9RestorePlaneRect(planeData, stride, baseX, baseY,
					restoreW, restoreH, saved)
				return 0, false
			}
			coeffRate += yrd.Rate
			distortion += uint64(yrd.Dist)
			skippable = yrd.Skippable
			visited = true
		}
	}
	vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
	if !visited {
		return 0, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	if skippable {
		rate += encoder.VP9CostBit(skipProb, 1)
	} else {
		rate += coeffRate + encoder.VP9CostBit(skipProb, 0)
	}
	return encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion), true
}

// scoreVP9KeyframeModeTransformRD is a libvpx-faithful port of
// txfm_rd_in_plane (vp9/encoder/vp9_rdopt.c:854-889) → block_rd_txfm
// (vp9_rdopt.c:699-852) → cost_coeffs (vp9_rdopt.c:358-459) for the
// keyframe Y-plane RD pick. libvpx's rd_pick_intra_sby_mode
// (vp9_rdopt.c:1383) calls super_block_yrd (vp9_rdopt.c:1025) which
// dispatches to txfm_rd_in_plane (vp9_rdopt.c:1042) — this drives the
// keyframe-Y-mode RD scorer.
//
// The libvpx-faithful flow per 4x4 step (with `step = 1 << tx_size`):
//
//   - block_rd_txfm runs the real forward DCT/ADST (vp9_xform_quant),
//     QuantizeB / QuantizeFP, and inverse-transform-add into the recon
//     buffer (vp9_rdopt.c:519-690).
//   - distortion = pixel_sse(src, recon) * 16 (vp9_rdopt.c:689).
//   - rate = cost_coeffs(... coeff_ctx ...) with coeff_ctx =
//     combine_entropy_contexts(t_above[c], t_left[r]) (vp9_rdopt.c:709-710).
//   - After each block, args.t_above[c..c+w]/t_left[r..r+h] are updated
//     to `eob > 0` so subsequent blocks see the proper entropy context
//     (libvpx vp9_encodemb.c:set_entropy_context_b drives the
//     non-RD update; for the RD path block_rd_txfm at vp9_rdopt.c:786-792
//     writes args.t_above/args.t_left = (eob > 0) under the
//     trellis_opt_tx_rd path — without trellis it stays at the
//     vp9_get_entropy_contexts initial values, but the inner-block
//     coeff_ctx still observes the per-block residue presence).
//
// The previous govpx implementation called the SATD-of-qcoeff proxy
// (the libvpx vp9_pickmode.c:830-853 block_yrd nonrd estimator) for
// the keyframe RD path. That was incorrect: libvpx keyframe at
// cpu_used=0..4 GOOD/REALTIME runs through the FULL_RD pick
// (sf->tx_size_search_method == USE_FULL_RD, sf->use_nonrd_pick_mode
// == 0; vp9_speed_features.c:942, 997) and dispatches to
// super_block_yrd, NOT block_yrd. The SATD proxy underestimated the
// larger-tx coef rate, so the picker biased toward smaller tx_size on
// textured residuals — the residual measured under
// TestVP9OracleRuntimeControlDeferredSeedsRemainReproducible was +989-2298B/frame
// across seeds #0/#1/#2/#4/#6/#7 (the RuntimeControls regression).
func (e *VP9Encoder) scoreVP9KeyframeModeTransformRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) (distortion uint64, coeffRate int, skippable bool, ok bool) {
	return e.scoreVP9KeyframeModeTransformRDWithBest(key, mode, tile,
		miRows, miCols, miRow, miCol, bsize, mi, ^uint64(0))
}

func (e *VP9Encoder) scoreVP9KeyframeModeTransformRDWithBest(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, refBestRD uint64,
) (distortion uint64, coeffRate int, skippable bool, ok bool) {
	if key == nil || key.hdr == nil || key.img == nil || key.dq == nil || mi == nil {
		return 0, 0, false, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, 0, false, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, 0, false, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return 0, 0, false, false
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
		return 0, 0, false, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	txSize := clampVP9TxSizeForBlock(mi.TxSize, bsize)
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	segID := vp9EncoderMiSegmentID(mi)
	dequant := key.dq.Y[segID]
	qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, segID,
		int(key.hdr.Quant.BaseQindex))
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if maxEob > len(e.coefScratch) {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, saved)
		return 0, 0, false, false
	}
	// libvpx vp9_rdopt.c:872 — args.t_above/args.t_left initialised by
	// vp9_get_entropy_contexts from pd->above_context/pd->left_context.
	// govpx mirrors via the plane context cache; the entropy-context
	// arrays are then updated per block to !!eob (vp9_rdopt.c:786-792).
	var aboveCtx [16]uint8
	var leftCtx [16]uint8
	aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
	leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
	if aboveLen > len(aboveCtx) || leftLen > len(leftCtx) {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, saved)
		return 0, 0, false, false
	}
	// vp9EncoderPlaneContextOffsets() does a modulo by len(pd.LeftContext)
	// and would panic when the plane LeftContext slab is not yet
	// allocated (some test fixtures bypass the encoder init path). Skip
	// the context cache copy in that case and start with the
	// vp9_get_entropy_contexts default of zeroes (libvpx's
	// vp9_rd.c:547-583 initial state for a fresh SB), which matches the
	// libvpx-faithful coeff_ctx at SB corners.
	if len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if off := aboveOffsets[0]; off >= 0 && off+aboveLen <= len(pd.AboveContext) {
			copy(aboveCtx[:aboveLen], pd.AboveContext[off:off+aboveLen])
		}
		if off := leftOffsets[0]; off >= 0 && off+leftLen <= len(pd.LeftContext) {
			copy(leftCtx[:leftLen], pd.LeftContext[off:off+leftLen])
		}
	}
	skippable = true
	useTxDomainDistortion := e.vp9KeyframeUseTransformDomainDistortion(key,
		miRows, miCols, miRow, miCol, bsize)
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			coeffs := e.coefScratch[:maxEob]
			qcoeffs := e.qCoefScratch[:maxEob]
			for i := range coeffs {
				coeffs[i] = 0
				qcoeffs[i] = 0
			}
			// libvpx vp9_rdopt.c:699-768 — block_rd_txfm dispatches to
			// vp9_xform_quant + inv_txfm_add. govpx's
			// prepareVP9KeyframeTxResidueWithQ chains predictVP9KeyframeTx
			// (intra prediction) → gatherVP9TxResidual (src - pred) →
			// quantizeVP9TxResidualWithQ (forward DCT/ADST + QuantizeB*WithQ
			// emitting qcoeff for cost_coeffs (vp9_rdopt.c:367) +
			// InverseTransformBlock). The dst recon is updated in
			// place, so subsequent intra predictions for later blocks
			// in the SB see the libvpx-correct reconstructed neighbour
			// samples — mirroring vp9_rdopt.c:683-687 which copies the
			// recon into out_recon for downstream blocks.
			hasResidue := e.prepareVP9KeyframeTxResidueWithQ(key, pd, 0, mode, txSize,
				tile, miRows, miCols, miRow, miCol, bsize, rr, cc, dequant,
				qindex, coeffs, qcoeffs)

			var blockDist uint64
			var blockSSE uint64
			if useTxDomainDistortion && hasResidue {
				blockDist = vp9TransformBlockError(e.txCoeffScratch[:maxEob],
					e.dqCoeffScratch[:maxEob], txSize)
				blockSSE = vp9TransformBlockEnergy(e.txCoeffScratch[:maxEob], txSize)
			} else {
				// libvpx vp9_rdopt.c:766-768 — dist = pixel_sse(src,recon)*16
				// when transform-domain distortion is disabled, or when eob=0
				// makes dist_block fall through to the pixel path.
				bs := 4 << uint(txSize)
				txX := baseX + cc*4
				txY := baseY + rr*4
				dist, distOK := vp9PlaneRectSSEClamped(src, srcStride, srcW,
					srcH, planeData, stride, txX, txY, bs, bs)
				if !distOK {
					vp9RestorePlaneRect(planeData, stride, baseX, baseY,
						restoreW, restoreH, saved)
					return 0, 0, false, false
				}
				blockDist = dist * 16
				blockSSE = vp9ResidualSSE(e.residueScratch[:bs*bs]) * 16
			}
			distortion += blockDist

			// libvpx vp9_rdopt.c:709-710 — coeff_ctx =
			// combine_entropy_contexts(t_above[c], t_left[r]).
			// govpx's vp9dec.GetEntropyContext returns
			// (above != 0) + (left != 0) which matches.
			initCtx := vp9dec.GetEntropyContext(txSize,
				aboveCtx[cc:cc+step], leftCtx[rr:rr+step])
			// libvpx vp9_rdopt.c:826 — rate = rate_block(...) =
			// cost_coeffs(...). The keyframe-Y intra is_inter=0 path
			// reads x->token_costs[tx_size][PLANE_TYPE_Y][0]; govpx
			// dispatches to vp9KeyframeCoeffBlockRateCostQ which
			// indexes fc.CoefProbs[txSize][0][0] (planeType=0,
			// is_inter=0) and walks the per-token entropy tree reading
			// v = qcoeff[rc] directly (vp9_rdopt.c:392,405).
			blockRate := e.vp9KeyframeCoeffBlockRateCostQ(txSize, mode,
				key.lossless, dequant, coeffs, qcoeffs, initCtx)
			coeffRate += blockRate

			// libvpx block_rd_txfm accumulates an early-exit RD using
			// min(coded_rd, zero_rd) per transform block (vp9_rdopt.c:831-846).
			// The final returned tuple still uses coded rate/distortion; this
			// accumulator only decides whether later modes/tx sizes survive.
			if refBestRD != ^uint64(0) {
				rdCoded := encoder.RDCost(e.cbRdmult, encoder.RDDivBits, blockRate, blockDist)
				rdZero := encoder.RDCost(e.cbRdmult, encoder.RDDivBits, 0, blockSSE)
				blockRD := min(rdZero, rdCoded)
				if blockRD > refBestRD {
					vp9RestorePlaneRect(planeData, stride, baseX, baseY,
						restoreW, restoreH, saved)
					return 0, 0, false, false
				}
				refBestRD -= blockRD
			}

			// libvpx vp9_rdopt.c:786-792 — after the block,
			// t_above[c..c+w]/t_left[r..r+h] = (eob > 0). govpx
			// mirrors via the hasResidue flag (prepareVP9KeyframeTxResidue
			// returns true exactly when eob > 0 from the encoder
			// quantize pass).
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
		}
	}
	vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
	return distortion, coeffRate, skippable, true
}

func vp9RestorePlaneRect(data []byte, stride, x0, y0, w, h int, saved []byte) {
	for y := range h {
		copy(data[(y0+y)*stride+x0:(y0+y)*stride+x0+w],
			saved[y*w:(y+1)*w])
	}
}

func vp9TransformBlockErrorShifted(coeffs, dqcoeffs []int16) uint64 {
	return vp9TransformBlockError(coeffs, dqcoeffs, common.Tx4x4)
}

func vp9TransformBlockEnergy(coeffs []int16, txSize common.TxSize) uint64 {
	var energy uint64
	for _, coeff := range coeffs {
		v := int64(coeff)
		energy += uint64(v * v)
	}
	if txSize != common.Tx32x32 {
		energy >>= 2
	}
	return energy
}

func vp9ResidualSSE(residue []int16) uint64 {
	var sse uint64
	for _, diff := range residue {
		v := int64(diff)
		sse += uint64(v * v)
	}
	return sse
}

func vp9TransformBlockError(coeffs, dqcoeffs []int16, txSize common.TxSize) uint64 {
	n := min(len(coeffs), len(dqcoeffs))
	var err uint64
	for i := range n {
		diff := int64(coeffs[i]) - int64(dqcoeffs[i])
		err += uint64(diff * diff)
	}
	if txSize != common.Tx32x32 {
		err >>= 2
	}
	return err
}

func (e *VP9Encoder) vp9KeyframeUseTransformDomainDistortion(key *vp9KeyframeEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) bool {
	if e == nil || e.sf.AllowTxfmDomainDistortion == 0 {
		return false
	}
	if e.sf.TxDomainThresh <= 0 {
		return true
	}
	if key == nil || key.img == nil || bsize >= common.BlockSizes {
		return false
	}
	src, stride, width, height := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, width, height) {
		return false
	}
	variance := encoder.BlockSourceVariance128(src, stride, x0, y0, blockW, blockH)
	scaled := float64(variance*256) /
		float64(uint64(1)<<uint(common.NumPelsLog2Lookup[bsize]))
	return math.Log(scaled+1.0) >= e.sf.TxDomainThresh
}

func (e *VP9Encoder) scoreVP9KeyframePlanePrediction(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode, plane int,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (uint64, bool) {
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	planeData, stride := e.vp9EncoderReconPlane(plane)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	if baseX >= stride || baseY >= rows {
		return 0, false
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 {
		return 0, false
	}
	if restoreW*restoreH > len(e.blockScratch) {
		return 0, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}
	// Later intra transforms can reference earlier transforms in the same
	// block; seed those references from source while scoring, then restore.
	restoreRecon := func() {
		for y := 0; y < restoreH; y++ {
			copy(planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW],
				saved[y*restoreW:(y+1)*restoreW])
		}
	}

	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	var distortion uint64
	ok := true
scoreLoop:
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			score, scoreOK := e.scoreVP9KeyframeTxPrediction(key, pd, mode, plane,
				txSize, tile, miRows, miCols, miRow, miCol, bsize, rr, cc)
			if !scoreOK {
				ok = false
				break scoreLoop
			}
			distortion += score
			txX := baseX + cc*4
			txY := baseY + rr*4
			vp9CopySourceRectClamped(planeData, stride, src, srcStride,
				srcW, srcH, txX, txY, bs, bs)
		}
	}
	restoreRecon()
	if !ok {
		return 0, false
	}
	return distortion, true
}

func vp9IntraPredictWidth4x4(bsize, planeBsize common.BlockSize,
	pd *vp9dec.MacroblockdPlane,
) int {
	w := int(common.Num4x4BlocksWideLookup[planeBsize])
	if pd == nil || bsize >= common.Block8x8 {
		return w
	}
	sub8W := max(2>>pd.SubsamplingX, 1)
	if sub8W > w {
		return sub8W
	}
	return w
}

// pickVP9KeyframeUvMode ports libvpx's rd_pick_intra_sbuv_mode
// (vp9/encoder/vp9_rdopt.c:1468-1512) verbatim. For each UV intra mode
// in DC_PRED..TM_PRED that passes the sf.intra_uv_mode_mask gate keyed on
// max_txsize_lookup[bsize], it invokes super_block_uvrd to score the
// (rate_tokenonly, distortion, skippable) tuple at the per-Y-mode keyed
// UV mode bit cost intra_uv_mode_cost[KEY_FRAME][Y_mode][uv_mode], then
// tracks the minimum-RD UV mode under the keyframe rdmult.
//
// libvpx (vp9_rdopt.c:1468-1512):
//
//	static int64_t rd_pick_intra_sbuv_mode(VP9_COMP *cpi, MACROBLOCK *x,
//	                                       PICK_MODE_CONTEXT *ctx, ...) {
//	  ...
//	  for (mode = DC_PRED; mode <= TM_PRED; ++mode) {
//	    if (!(cpi->sf.intra_uv_mode_mask[max_tx_size] & (1 << mode))) continue;
//	    xd->mi[0]->uv_mode = mode;
//	    if (!super_block_uvrd(cpi, x, &this_rate_tokenonly, &this_distortion,
//	                          &s, &this_sse, bsize, best_rd))
//	      continue;
//	    this_rate = this_rate_tokenonly +
//	        cpi->intra_uv_mode_cost[KEY_FRAME][xd->mi[0]->mode][mode];
//	    this_rd = RDCOST(x->rdmult, x->rddiv, this_rate, this_distortion);
//	    if (this_rd < best_rd) { ... mode_selected = mode; ... }
//	  }
//	  xd->mi[0]->uv_mode = mode_selected;
//	  return best_rd;
//	}
//
// govpx specifics:
//   - super_block_uvrd is ported as vp9SuperBlockUvRD below, walking the
//     two chroma planes at uv_tx_size = GetUvTxSize(bsize, mi.TxSize, pd)
//     and accumulating per-plane rate/distortion/sse/skippable via the
//     same Hadamard+quantize_fp+SATD substrate the Y picker uses (the
//     realtime nonrd substrate; the keyframe pipe runs through the
//     realtime tokenizer downstream).
//   - intra_uv_mode_cost[KEY_FRAME][Y_mode][uv_mode] is realized via
//     vp9_cost_tokens(kf_uv_mode_prob[Y_mode], vp9_intra_mode_tree).
//   - rdmult mirrors encoder.KeyframeRDMul(qindex) and the TPL bias applied
//     upstream by pickVP9KeyframeMode (preserved via e.cbRdmult).
func (e *VP9Encoder) pickVP9KeyframeUvMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) common.PredictionMode {
	rd, ok := e.pickVP9KeyframeUvModeRD(key, tile, miRows, miCols,
		miRow, miCol, bsize, mi)
	if !ok {
		return common.DcPred
	}
	return rd.mode
}

func (e *VP9Encoder) pickVP9KeyframeUvModeRD(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) (vp9KeyframeIntraRD, bool) {
	if key == nil || mi == nil {
		return vp9KeyframeIntraRD{}, false
	}
	// libvpx vp9_rdopt.c:1540-1546 — rd_pick_intra_sbuv_mode receives the
	// max(bsize, BLOCK_8X8) for sub-8x8 partitions because chroma is
	// subsampled and the smallest legal UV block is 8x8 luma == 4x4 UV.
	uvBsize := max(bsize, common.Block8x8)
	if uvBsize >= common.BlockSizes {
		return vp9KeyframeIntraRD{}, false
	}
	maxTxSize := common.MaxTxsizeLookup[uvBsize]
	// libvpx vp9_rdopt.c:1482 — intra_uv_mode_mask[max_tx_size] gate. Fall
	// back to DC if the speed-features mask is uninitialised or zeroed
	// (defensive; the configurator seeds it to sfIntraAll for the keyframe
	// best-quality path at vp9_speed_features.go:906).
	uvMask := e.sf.IntraUvModeMask[maxTxSize]
	if uvMask == 0 {
		uvMask = sfIntraAll
	}
	yMode := mi.Mode
	if int(yMode) < 0 || int(yMode) >= common.IntraModes {
		yMode = common.DcPred
	}
	// libvpx vp9_rd.c:104-106 — cost_tokens(intra_uv_mode_cost[KEY][Y_mode],
	// kf_uv_mode_prob[Y_mode], vp9_intra_mode_tree). The keyframe path uses
	// vp9_kf_uv_mode_prob (KfUvModeProb) keyed on the chosen Y mode.
	uvProbs := tables.KfUvModeProb[yMode]
	var uvModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(uvModeCosts[:], uvProbs[:], common.IntraModeTree[:])

	// libvpx vp9_rdopt.c:1480 — memset(x->skip_txfm, SKIP_TXFM_NONE, ...).
	// govpx tracks per-tx skip state inside the per-plane scorer; the
	// global x->skip_txfm reset is implicit (no carry across UV modes).

	qindex := e.vp9EncoderModeDecisionQIndex()
	rdmult := e.cbRdmult
	if rdmult <= 0 {
		// Re-derive when the caller hasn't primed cb_rdmult (e.g. tests).
		rdmult = encoder.KeyframeRDMul(qindex)
	}

	best := vp9KeyframeIntraRD{mode: common.DcPred}
	bestRD := uint64(^uint64(0))
	bestValid := false
	useTxDomainDistortion := e.vp9KeyframeUseTransformDomainDistortion(key,
		miRows, miCols, miRow, miCol, bsize)
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if uvMask&(1<<uint(mode)) == 0 {
			continue
		}
		coeffRate, distortion, skippable, ok := e.scoreVP9KeyframeUvModeTransformRD(
			key, mode, uvBsize, tile, miRows, miCols, miRow, miCol, mi,
			useTxDomainDistortion)
		if !ok {
			continue
		}
		thisRate := coeffRate + uvModeCosts[mode]
		thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, thisRate, distortion)
		if !bestValid || thisRD < bestRD {
			bestRD = thisRD
			best = vp9KeyframeIntraRD{
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
		return vp9KeyframeIntraRD{}, false
	}
	return best, true
}

// scoreVP9KeyframeUvModeTransformRD ports libvpx's super_block_uvrd
// (vp9/encoder/vp9_rdopt.c:1418-1466) for the keyframe intra path.
// Iterates planes 1..2 and accumulates per-plane (rate, distortion, sse)
// via the same Hadamard+quantize_fp+SATD substrate the Y picker uses.
// Each plane's transform size is uv_tx_size = GetUvTxSize(bsize, mi.TxSize, pd).
//
// libvpx (verbatim):
//
//	if (ref_best_rd < 0) is_cost_valid = 0;
//	... (subtract per-plane src diff for inter) ...
//	*rate = 0; *distortion = 0; *sse = 0; *skippable = 1;
//	for (plane = 1; plane < MAX_MB_PLANE; ++plane) {
//	  txfm_rd_in_plane(cpi, x, &pnrate, &pndist, &pnskip, &pnsse, ref_best_rd,
//	                   plane, bsize, uv_tx_size, ...);
//	  if (pnrate == INT_MAX) { is_cost_valid = 0; break; }
//	  *rate += pnrate; *distortion += pndist;
//	  *sse += pnsse; *skippable &= pnskip;
//	}
//
// govpx differences:
//   - sse is not surfaced because the keyframe picker uses (rate, dist) under
//     RDCOST only; libvpx threads sse through for the skip-vs-non-skip
//     override which lives in the inter-frame path (rd_pick_inter_mode_sb)
//     not the intra picker.
//   - skippable is surfaced for completeness but the caller does not
//     fold it into rate; libvpx's rd_pick_intra_sbuv_mode does not add a
//     skip-bit cost either (that lives in the caller that writes the
//     block — write_mb_modes_kf / write_modes_b does not emit a UV-only
//     skip flag separately from the Y-block skip).
func (e *VP9Encoder) scoreVP9KeyframeUvModeTransformRD(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, bsize common.BlockSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, mi *vp9dec.NeighborMi,
	useTxDomainDistortion bool,
) (coeffRate int, distortion uint64, skippable bool, ok bool) {
	if key == nil || key.hdr == nil || key.img == nil || key.dq == nil || mi == nil {
		return 0, 0, false, false
	}
	skippable = true
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		pnRate, pnDist, pnSkip, planeOK := e.scoreVP9KeyframeUvPlaneRD(
			key, pd, mode, plane, bsize, tile, miRows, miCols, miRow, miCol, mi,
			useTxDomainDistortion)
		if !planeOK {
			// libvpx super_block_uvrd: pnrate == INT_MAX -> is_cost_valid = 0.
			return 0, 0, false, false
		}
		coeffRate += pnRate
		distortion += pnDist
		skippable = skippable && pnSkip
	}
	return coeffRate, distortion, skippable, true
}

// scoreVP9KeyframeUvPlaneRD ports the per-plane txfm_rd_in_plane
// (vp9/encoder/vp9_rdopt.c:854-889) for a single chroma plane. Mirrors
// the Y-plane scoreVP9KeyframeModeTransformRD verbatim — predict +
// quantize via prepareVP9KeyframeTxResidue (which itself ports
// vp9_encode_block_intra), then dist = pixel_sse(src, recon) * 16 and
// rate = cost_coeffs(coeff_ctx). The chroma tx_size derives from
// uv_tx_size = GetUvTxSize(bsize, mi.TxSize, pd) per libvpx
// vp9_rdopt.c:1425.
func (e *VP9Encoder) scoreVP9KeyframeUvPlaneRD(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode, plane int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, mi *vp9dec.NeighborMi,
	useTxDomainDistortion bool,
) (rate int, distortion uint64, skippable bool, ok bool) {
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, 0, false, false
	}
	planeData, stride := e.vp9EncoderReconPlane(plane)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, 0, false, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	if baseX >= stride || baseY >= rows {
		return 0, 0, false, false
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
		return 0, 0, false, false
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}

	// libvpx vp9_blockd.h get_uv_tx_size — caps the chroma tx_size at the
	// luma mi tx_size but never exceeds the chroma plane's own max.
	uvTxSize := vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(uvTxSize)
	segID := vp9EncoderMiSegmentID(mi)
	dequant := key.dq.Uv[segID]
	qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, segID,
		int(key.hdr.Quant.BaseQindex))
	maxEob := vp9dec.MaxEobForTxSize(uvTxSize)
	if maxEob > len(e.coefScratch) {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, saved)
		return 0, 0, false, false
	}
	// libvpx vp9_rdopt.c:872 — args.t_above/args.t_left initialised by
	// vp9_get_entropy_contexts from pd->above_context/pd->left_context.
	// Mirror the Y picker exactly: copy any seeded chroma plane context
	// into local arrays, then update per block via the !!eob rule
	// (vp9_rdopt.c:786-792). For SB corners libvpx's
	// vp9_get_entropy_contexts initialises to zero (vp9_rd.c:547-583)
	// which matches our zero-initialised local arrays.
	var aboveCtx [16]uint8
	var leftCtx [16]uint8
	aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
	leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
	if aboveLen > len(aboveCtx) || leftLen > len(leftCtx) {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY,
			restoreW, restoreH, saved)
		return 0, 0, false, false
	}
	if len(pd.AboveContext) > 0 && len(pd.LeftContext) > 0 {
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		// aboveOffsets/leftOffsets index by plane.
		if plane >= 0 && plane < len(aboveOffsets) && plane < len(leftOffsets) {
			if off := aboveOffsets[plane]; off >= 0 && off+aboveLen <= len(pd.AboveContext) {
				copy(aboveCtx[:aboveLen], pd.AboveContext[off:off+aboveLen])
			}
			if off := leftOffsets[plane]; off >= 0 && off+leftLen <= len(pd.LeftContext) {
				copy(leftCtx[:leftLen], pd.LeftContext[off:off+leftLen])
			}
		}
	}
	skippable = true
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			coeffs := e.coefScratch[:maxEob]
			qcoeffs := e.qCoefScratch[:maxEob]
			for i := range coeffs {
				coeffs[i] = 0
				qcoeffs[i] = 0
			}
			// libvpx vp9_rdopt.c:699-768 — block_rd_txfm dispatches to
			// vp9_xform_quant + inv_txfm_add. govpx's
			// prepareVP9KeyframeTxResidueWithQ chains predictVP9KeyframeTx
			// (intra prediction) → gatherVP9TxResidual (src - pred) →
			// quantizeVP9TxResidualWithQ (forward DCT/ADST + QuantizeB*WithQ
			// emitting qcoeff for cost_coeffs (vp9_rdopt.c:367) +
			// InverseTransformBlock). The dst recon is updated in place
			// so subsequent intra predictions for later blocks in the
			// SB see libvpx-correct neighbour samples.
			hasResidue := e.prepareVP9KeyframeTxResidueWithQ(key, pd, plane, mode, uvTxSize,
				tile, miRows, miCols, miRow, miCol, bsize, rr, cc, dequant,
				qindex, coeffs, qcoeffs)

			if useTxDomainDistortion && hasResidue {
				distortion += vp9TransformBlockError(e.txCoeffScratch[:maxEob],
					e.dqCoeffScratch[:maxEob], uvTxSize)
			} else {
				// libvpx vp9_rdopt.c:766-768 — dist = pixel_sse(src,recon)*16
				// when transform-domain distortion is disabled, or when eob=0
				// makes dist_block fall through to the pixel path.
				bs := 4 << uint(uvTxSize)
				txX := baseX + cc*4
				txY := baseY + rr*4
				dist, distOK := vp9PlaneRectSSEClamped(src, srcStride, srcW,
					srcH, planeData, stride, txX, txY, bs, bs)
				if !distOK {
					vp9RestorePlaneRect(planeData, stride, baseX, baseY,
						restoreW, restoreH, saved)
					return 0, 0, false, false
				}
				distortion += dist * 16
			}

			// libvpx vp9_rdopt.c:709-710 — coeff_ctx =
			// combine_entropy_contexts(t_above[c], t_left[r]).
			initCtx := vp9dec.GetEntropyContext(uvTxSize,
				aboveCtx[cc:cc+step], leftCtx[rr:rr+step])
			// libvpx vp9_rdopt.c:826 — rate = rate_block(...) =
			// cost_coeffs(...). For chroma planes the cost_coeffs reads
			// x->token_costs[tx_size][PLANE_TYPE_UV=1][is_inter=0]; the
			// vp9KeyframeUvCoeffBlockRateCostQ helper indexes
			// fc.CoefProbs[txSize][planeType][is_inter] internally and
			// reads v = qcoeff[rc] (vp9_rdopt.c:438) directly.
			rate += e.vp9KeyframeUvCoeffBlockRateCostQ(uvTxSize, dequant,
				coeffs, qcoeffs, initCtx)

			// libvpx vp9_rdopt.c:786-792 — t_above[c..]/t_left[r..] = !!eob.
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
		}
	}
	vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
	return rate, distortion, skippable, true
}

func (e *VP9Encoder) scoreVP9KeyframeTxPrediction(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode,
	plane int, txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
) (uint64, bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := buffers.Align(int(key.hdr.Width), 8)
	alignedHeight := buffers.Align(int(key.hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if bs*bs > len(e.modeScratch) || x0+bs > stride || y0+bs > rows {
		return 0, false
	}

	bounds := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := blockRow4x4 != 0 || miRow > 0
	if upAvailable {
		edges.Above = planeData[(y0-1)*stride+x0:]
		if leftAvailable {
			edges.AboveLeft = planeData[(y0-1)*stride+x0-1]
		}
	}
	planeBlock4x4W := vp9IntraPredictWidth4x4(bsize, planeBsize, pd)
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W

	pred := e.modeScratch[:bs*bs]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            pred,
		DstStride:      bs,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          edges,
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)

	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	score := vp9PredictionSSEClamped(src, srcStride, srcW, srcH,
		pred, bs, x0, y0, bs)
	return score, true
}

// pickVP9KeyframeBlockTxSize is a verbatim port of libvpx's
// choose_tx_size_from_rd (vp9/encoder/vp9_rdopt.c:907-1023) specialised for
// the keyframe Y-plane RD pick. libvpx's vp9_rd_pick_intra_mode_sb
// (vp9_rdopt.c:3221-3271) calls rd_pick_intra_sby_mode which, for each
// candidate Y mode, invokes super_block_yrd (vp9_rdopt.c:1025-1042) which in
// turn dispatches to choose_tx_size_from_rd for cm->tx_mode == TX_MODE_SELECT
// (the case the keyframe write_mb_modes_kf bitstream emits when tx_mode is
// TX_MODE_SELECT). govpx already picks the Y mode upstream via
// pickVP9KeyframeMode using a Tx16x16-capped score; this helper layers the
// per-block Tx32x32/Tx16x16/Tx8x8/Tx4x4 RD pick on top so mi.TxSize matches
// libvpx's choose_tx_size_from_rd output. The Y-plane only — libvpx UV
// tx_size is derived from mi->tx_size via get_uv_tx_size (which the
// keyframe-source write path already does via vp9dec.GetUvTxSize).
//
// libvpx (vp9_rdopt.c:946-955) sets start_tx/end_tx as:
//
//	if (cm->tx_mode == TX_MODE_SELECT) {
//	  start_tx = max_tx_size;
//	  end_tx = VPXMAX(start_tx - cpi->sf.tx_size_search_depth, 0);
//	  if (bs > BLOCK_32X32) end_tx = VPXMIN(end_tx + 1, start_tx);
//	}
//
// and loops `for (n = start_tx; n >= end_tx; n--)`. Each candidate's rate
// includes the tx_size signalling cost cpi->tx_size_cost[..][..][n] (libvpx
// vp9_rdopt.c:958); govpx mirrors this via vp9TxSizeRateCost using the
// fc.TxProbs row keyed on (max_tx_size, tx_size_ctx).
//
// Distortion is measured as libvpx's block_rd_txfm (vp9_rdopt.c:766-768):
// `dist = pixel_sse(src, dst) * 16` where `dst` is the post-encoded recon
// (the same recon the loop-filter SSE picker later consumes). Rate is the
// coefficient-block cost via vp9InterCoeffBlockRateCost reusing
// fc.CoefProbs[txSize][0] (planeType=0 for Y; is_inter=0 for keyframe).
func (e *VP9Encoder) pickVP9KeyframeBlockTxSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, txMode common.TxMode,
) {
	if key == nil || key.hdr == nil || key.img == nil || key.dq == nil || mi == nil {
		return
	}
	if txMode != common.TxModeSelect || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return
	}
	if key.lossless {
		// libvpx vp9_rdopt.c:1035 — lossless dispatches to
		// choose_largest_tx_size which pins tx_size at the cap; here that
		// reduces to Tx4x4 because the keyframe Y residue is forced to
		// Tx4x4 elsewhere when lossless is set.
		return
	}
	// libvpx vp9_rdopt.c:1035 — when sf.tx_size_search_method == USE_LARGESTALL
	// super_block_yrd dispatches to choose_largest_tx_size, NOT
	// choose_tx_size_from_rd. govpx leaves mi.TxSize at the
	// MaxTxsizeLookup[bsize] preload (the existing keyframe baseMi /
	// clampVP9TxSizeForBlock pin), matching libvpx's choose_largest_tx_size
	// output `mi->tx_size = VPXMIN(max_tx_size, tx_mode_to_biggest_tx_size)`.
	if e.sf.TxSizeSearchMethod != UseFullRD {
		return
	}
	mode := mi.Mode
	if int(mode) >= common.IntraModes {
		mode = common.DcPred
	}
	rdmult := encoder.KeyframeRDMul(e.vp9EncoderModeDecisionQIndex())
	bwMi := int(common.Num8x8BlocksWideLookup[bsize])
	bhMi := int(common.Num8x8BlocksHighLookup[bsize])
	if e.tpl.Enabled {
		rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, rdmult)
	}
	if e.vp9KeyframeRDRefinementEnabled() {
		if txRD, ok := e.chooseVP9KeyframeModeTxRD(key, mode, rdmult, tile,
			miRows, miCols, miRow, miCol, bsize, mi, txMode); ok {
			mi.TxSize = txRD.txSize
			return
		}
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	if len(planeData) == 0 || stride <= 0 {
		return
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return
	}
	maxTx := common.MaxTxsizeLookup[bsize]
	// libvpx vp9_rdopt.c:946-954 — TX_MODE_SELECT start_tx/end_tx range.
	startTx := int(maxTx)
	endTx := max(startTx-e.sf.TxSizeSearchDepth, 0)
	if bsize > common.Block32x32 {
		// VPXMIN(end_tx + 1, start_tx) (vp9_rdopt.c:949).
		newEnd := min(endTx+1, startTx)
		endTx = newEnd
	}
	if startTx <= endTx && startTx == endTx {
		// Only one candidate; nothing to RD-pick.
		mi.TxSize = common.TxSize(startTx)
		return
	}
	if startTx < endTx {
		return
	}
	// Snapshot the SB Y-plane recon so each TX candidate can run on a
	// pristine baseline. libvpx accomplishes the same via per-candidate
	// recon_buf[n][64*64] in choose_tx_size_from_rd (vp9_rdopt.c:929-940).
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	rows := len(planeData) / stride
	if baseX >= stride || baseY >= rows {
		return
	}
	restoreW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	restoreH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if baseX+restoreW > stride {
		restoreW = stride - baseX
	}
	if baseY+restoreH > rows {
		restoreH = rows - baseY
	}
	if restoreW <= 0 || restoreH <= 0 ||
		restoreW*restoreH > len(e.blockScratch) {
		return
	}
	saved := e.blockScratch[:restoreW*restoreH]
	for y := 0; y < restoreH; y++ {
		copy(saved[y*restoreW:(y+1)*restoreW],
			planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
	}
	// Tx-size signalling cost. libvpx vp9_rdopt.c:927+958 derive
	// tx_size_ctx from get_tx_size_context and rate from
	// cpi->tx_size_cost[max_tx-1][ctx][n]. govpx mirrors via
	// vp9TxSizeRateCost on the fc.TxProbs row.
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, vp9EncoderMiSegmentID(mi),
		int(key.hdr.Quant.BaseQindex))
	dequant := key.dq.Y[vp9EncoderMiSegmentID(mi)]

	bestTx := common.TxSize(startTx)
	bestScore := uint64(^uint64(0))
	bestValid := false
	prevScore := uint64(0)
	prevValid := false
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	useTxDomainDistortion := e.vp9KeyframeUseTransformDomainDistortion(key,
		miRows, miCols, miRow, miCol, bsize)
	for n := startTx; n >= endTx; n-- {
		tx := common.TxSize(n)
		// libvpx vp9_rdopt.c:1004-1009 — restore recon and run
		// txfm_rd_in_plane for this tx candidate. govpx restores by
		// blitting `saved` back over the SB rect.
		for y := 0; y < restoreH; y++ {
			copy(planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW],
				saved[y*restoreW:(y+1)*restoreW])
		}
		step := 1 << uint(tx)
		bs := 4 << uint(tx)
		coeffBlockSlots := vp9dec.MaxEobForTxSize(tx)
		if coeffBlockSlots > len(e.coefScratch) {
			continue
		}
		var rate int
		var distortion uint64
		valid := true
		for rr := 0; rr < max4x4H && valid; rr += step {
			for cc := 0; cc < max4x4W && valid; cc += step {
				mode := vp9dec.GetYMode(mi, rr*int(common.Num4x4BlocksWideLookup[planeBsize])+cc)
				coeffs := e.coefScratch[:coeffBlockSlots]
				qcoeffs := e.qCoefScratch[:coeffBlockSlots]
				for i := range coeffs {
					coeffs[i] = 0
					qcoeffs[i] = 0
				}
				hasResidue := e.prepareVP9KeyframeTxResidueWithQ(key, pd, 0, mode, tx, tile,
					miRows, miCols, miRow, miCol, bsize, rr, cc, dequant,
					qindex, coeffs, qcoeffs)
				if !hasResidue {
					// No residue: the prediction matched src exactly (or
					// quantization zeroed everything). libvpx's
					// block_rd_txfm still computes dist via pixel_sse —
					// which is 0 here — and rate via cost_coeffs on the
					// zero-coeff EOB. Mirror by leaving rate/dist 0 for
					// this 4x4 step.
				}
				if useTxDomainDistortion && hasResidue {
					distortion += vp9TransformBlockError(e.txCoeffScratch[:coeffBlockSlots],
						e.dqCoeffScratch[:coeffBlockSlots], tx)
				} else {
					// libvpx vp9_rdopt.c:766-768 — dist = pixel_sse(src,dst)*16
					// when transform-domain distortion is disabled, or when eob=0
					// makes dist_block fall through to the pixel path.
					txX := baseX + cc*4
					txY := baseY + rr*4
					if dist, ok := vp9PlaneRectSSEClamped(src, srcStride, srcW,
						srcH, planeData, stride, txX, txY, bs, bs); ok {
						distortion += dist * 16
					} else {
						valid = false
						break
					}
				}
				// libvpx vp9_rdopt.c:826 — rate = rate_block(...) =
				// cost_coeffs(...). govpx uses
				// vp9KeyframeCoeffBlockRateCostQ as the cost_coeffs port,
				// reading v = qcoeff[rc] directly (vp9_rdopt.c:392,405);
				// keyframe is_inter=0 so the [0] is_inter index of
				// fc.CoefProbs is the libvpx-faithful path. initCtx=0
				// matches the per-tx_size choose_tx_size_from_rd loop
				// which doesn't thread per-block above/left contexts
				// across the inner TX_SIZE candidates (libvpx
				// vp9_rdopt.c:854-872 — txfm_rd_in_plane resets
				// args.t_above/t_left from vp9_get_entropy_contexts each
				// outer iteration; the per-block coeff_ctx is computed
				// inside block_rd_txfm and is locally 0 at the SB
				// corner with no above/left residue).
				rate += e.vp9KeyframeCoeffBlockRateCostQ(tx, mode,
					key.lossless, dequant, coeffs, qcoeffs, 0)
			}
		}
		if !valid {
			continue
		}
		// libvpx vp9_rdopt.c:958+985 — r[n][1] = r[n][0] + r_tx_size,
		// then rd[n][1] = RDCOST(rate + s0, dist) (the !skip branch).
		// govpx folds s0/s1 (the skip-flag costs) into the existing
		// keyframe writer downstream; for the TX_MODE_SELECT inner pick
		// we only need rate + r_tx_size to compare candidates under the
		// same skip context. This mirrors libvpx since the skip cost is
		// independent of tx_size when the block has residue (the
		// dominant case during keyframe TX_MODE_SELECT pick).
		rate += vp9TxSizeRateCost(txProbs, tx, maxTx)
		score := e.vp9ModeDecisionScore(distortion, rate, qindex)
		if !bestValid || score < bestScore {
			bestScore = score
			bestTx = tx
			bestValid = true
		}
		// libvpx tx_size_search_breakout (vp9_rdopt.c:994-997) — break the
		// search loop when smaller-tx fails to improve over the previous
		// larger-tx score. govpx initializes sf.tx_size_search_breakout = 1
		// in the best-quality init (vp9_speed_features.go:944), so the
		// libvpx-default behaviour applies. Without the breakout, the loop
		// continues testing smaller tx candidates, which biases the picker
		// toward smaller tx_size on textured residuals because the
		// SATD-based rate proxy underestimates the larger-tx coef rate.
		if e.sf.TxSizeSearchBreakout != 0 && n < startTx && prevValid &&
			score > prevScore {
			break
		}
		prevScore = score
		prevValid = true
	}
	// Restore the recon snapshot. The subsequent
	// prepareVP9KeyframeBlockResidue call will re-encode with the chosen tx.
	for y := 0; y < restoreH; y++ {
		copy(planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW],
			saved[y*restoreW:(y+1)*restoreW])
	}
	if bestValid {
		mi.TxSize = bestTx
	}
}

// vp9KeyframeCoeffBlockRateCost is a verbatim port of libvpx's cost_coeffs
// (vp9/encoder/vp9_rdopt.c:358-459) specialised for the keyframe Y-plane
// path. libvpx walks the per-token entropy tree against
// x->token_costs[tx_size][type][is_inter_block(mi)] where type=PLANE_TYPE_Y
// (=0) and is_inter=0 for an intra/keyframe block. govpx mirrors by
// reading the matching fc.CoefProbs[txSize][planeType=0][ref=0] slab and
// invoking encoder.CoeffTokenRateCost for the unconstrained pareto8 tail —
// the same pareto-tree walk vp9_cost_tokens (vp9/encoder/vp9_cost.c)
// drives in fill_token_costs (vp9/encoder/vp9_rd.c:135-152). The
// per-coefficient energy class fed into the next coef-context lookup
// mirrors libvpx's token_cache[rc] = vp9_pt_energy_class[token]
// (vp9_rdopt.c:397, 429, 442; pt_energy_class table is in
// vp9/common/vp9_entropy.c:95). initCtx is libvpx's coeff_ctx
// (vp9_rdopt.c:709 combine_entropy_contexts(t_above, t_left)) that
// block_rd_txfm threads into the cost_coeffs(... pt ...) `pt` parameter
// (vp9_rdopt.c:695). Callers compute initCtx via
// vp9dec.GetEntropyContext on the per-block above/left context cache.
func (e *VP9Encoder) vp9KeyframeCoeffBlockRateCost(txSize common.TxSize,
	mode common.PredictionMode, lossless bool, dequant [2]int16,
	coeffs []int16, initCtx int,
) int {
	return e.vp9KeyframeCoeffBlockRateCostQ(txSize, mode, lossless, dequant,
		coeffs, nil, initCtx)
}

// vp9KeyframeCoeffBlockRateCostQ mirrors vp9KeyframeCoeffBlockRateCost and
// consumes signed qcoeff[] directly when non-nil instead of recovering q
// from int16-wrapped dqcoeff via vp9CoeffTokenAbsVal. libvpx
// vp9_rdopt.c:367,392,405 reads qcoeff[rc] (the unwrapped quantized
// magnitude); govpx's recovery
//
//	|q| = (2*|dqcoeff| + dequant - 1) / dequant  // Tx32x32
//	|q| = |dqcoeff| / dequant                    // otherwise
//
// is exact only when dqcoeff fits in int16. For Tx32x32 dq=1828 the
// dqcoeff = q*dq/2 cast wraps once |q| >= 36; for non-32x32 the cast
// wraps once |q*dq| > 32767. Passing qcoeff sidesteps the wrap.
func (e *VP9Encoder) vp9KeyframeCoeffBlockRateCostQ(txSize common.TxSize,
	mode common.PredictionMode, lossless bool, dequant [2]int16,
	coeffs, qcoeffs []int16, initCtx int,
) int {
	if txSize >= common.TxSizes {
		return 0
	}
	if int(mode) >= common.IntraModes {
		mode = common.DcPred
	}
	scanOrder := common.GetScan(txSize, 0, 0, lossless, mode)
	return e.vp9KeyframeCoeffBlockRateCostPlaneQ(txSize, 0, scanOrder,
		dequant, coeffs, qcoeffs, initCtx)
}

// vp9KeyframeUvCoeffBlockRateCostQ is the qcoeff-emitting sibling of
// vp9KeyframeUvCoeffBlockRateCost. libvpx vp9_rdopt.c:367,438 reads
// qcoeff[rc] directly for chroma planes too.
func (e *VP9Encoder) vp9KeyframeUvCoeffBlockRateCostQ(txSize common.TxSize,
	dequant [2]int16, coeffs, qcoeffs []int16, initCtx int,
) int {
	if txSize >= common.TxSizes {
		return 0
	}
	return e.vp9KeyframeCoeffBlockRateCostPlaneQ(txSize, 1,
		common.DefaultScanOrders[txSize], dequant, coeffs, qcoeffs, initCtx)
}

// vp9KeyframeCoeffBlockRateCostPlane is the shared cost_coeffs walker
// parameterised on planeType (0 = PLANE_TYPE_Y, 1 = PLANE_TYPE_UV) so
// the chroma RD pick consumes the libvpx-faithful chroma coef-token
// model fc.CoefProbs[txSize][planeType][is_inter=0].
func (e *VP9Encoder) vp9KeyframeCoeffBlockRateCostPlane(txSize common.TxSize,
	planeType int, scanOrder common.ScanOrder, dequant [2]int16,
	coeffs []int16, initCtx int,
) int {
	return e.vp9KeyframeCoeffBlockRateCostPlaneQ(txSize, planeType, scanOrder,
		dequant, coeffs, nil, initCtx)
}

// vp9KeyframeCoeffBlockRateCostPlaneQ mirrors libvpx's cost_coeffs
// (vp9_rdopt.c:358-459). When qcoeffs is non-nil the per-coefficient
// magnitude is read directly from qcoeffs[raster] — matching libvpx's
// p->qcoeff dereference — and the legacy vp9CoeffTokenAbsVal recovery
// from int16-wrapped dqcoeff is bypassed.
func (e *VP9Encoder) vp9KeyframeCoeffBlockRateCostPlaneQ(txSize common.TxSize,
	planeType int, scanOrder common.ScanOrder, dequant [2]int16,
	coeffs, qcoeffs []int16, initCtx int,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txSize >= common.TxSizes || dequant[0] == 0 || dequant[1] == 0 ||
		len(coeffs) < maxEob || len(e.modeScratch) < maxEob ||
		initCtx < 0 || initCtx > 2 || planeType < 0 || planeType > 1 {
		return 0
	}
	if qcoeffs != nil && len(qcoeffs) < maxEob {
		qcoeffs = nil
	}
	scan := scanOrder.Scan
	neighbors := scanOrder.Neighbors
	if len(scan) < maxEob || len(neighbors) < common.MaxNeighbors*maxEob {
		return 0
	}
	for i := range e.modeScratch[:maxEob] {
		e.modeScratch[i] = 0
	}
	eob := vp9CoeffBlockEOB(scan, maxEob, coeffs, qcoeffs)
	// libvpx vp9_rdopt.c:369 — x->token_costs[tx_size][type][is_inter].
	// type = planeType (0 = Y, 1 = UV); is_inter = 0 for keyframe/intra.
	coefModel := &e.fc.CoefProbs[txSize][planeType][0]
	if e.sf.UseFastCoefCosting != 0 {
		return e.vp9CoeffBlockRateCostFastQ(txSize, coefModel, scanOrder,
			dequant, coeffs, qcoeffs, initCtx)
	}
	return e.vp9CoeffBlockRateCostSlowQ(txSize, coefModel, scanOrder,
		dequant, coeffs, qcoeffs, initCtx, eob)
}

var vp9CoeffCostBandCounts = [common.TxSizes][8]int{
	{1, 2, 3, 4, 3, 16 - 13, 0},
	{1, 2, 3, 4, 11, 64 - 21, 0},
	{1, 2, 3, 4, 11, 256 - 21, 0},
	{1, 2, 3, 4, 11, 1024 - 21, 0},
}

// vp9CoeffBlockRateCostSlowQ ports the non-fast arm of libvpx cost_coeffs
// (vp9/encoder/vp9_rdopt.c:419-459). The second token-cost index is
// `!previous_token`, which skips the EOB branch immediately after a zero
// coefficient; charging the full tree there overstates sparse residuals and
// can move RD mode decisions.
func (e *VP9Encoder) vp9CoeffBlockRateCostSlowQ(txSize common.TxSize,
	coefModel *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8,
	scanOrder common.ScanOrder, dequant [2]int16, coeffs, qcoeffs []int16,
	initCtx int, eob int,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txSize >= common.TxSizes || coefModel == nil || dequant[0] == 0 ||
		dequant[1] == 0 || len(coeffs) < maxEob || initCtx < 0 ||
		initCtx > 2 {
		return 0
	}
	if qcoeffs != nil && len(qcoeffs) < maxEob {
		qcoeffs = nil
	}
	scan := scanOrder.Scan
	neighbors := scanOrder.Neighbors
	if len(scan) < maxEob || len(neighbors) < common.MaxNeighbors*maxEob {
		return 0
	}
	if eob <= 0 {
		return encoder.CoeffTreeTokenCost((*coefModel)[0][initCtx][:], false,
			encoder.EobToken)
	}
	if eob > maxEob {
		eob = maxEob
	}

	dcAbs, dcSign := vp9CoeffMagnitudeAndSign(qcoeffs, 0, coeffs[0],
		dequant[0], txSize == common.Tx32x32)
	prevToken, extraCost := encoder.CoeffTokenExtraCost(dcAbs, dcSign)
	rate := extraCost + encoder.CoeffTreeTokenCost(
		(*coefModel)[0][initCtx][:], false, prevToken)
	e.modeScratch[0] = encoder.PtEnergyClass[prevToken]

	band := 1
	bandLeft := vp9CoeffCostBandCounts[txSize][band]
	for c := 1; c < eob; c++ {
		if band >= vp9dec.CoefBands {
			return rate
		}
		raster := int(scan[c])
		dqv := dequant[1]
		absVal, sign := vp9CoeffMagnitudeAndSign(qcoeffs, raster,
			coeffs[raster], dqv, txSize == common.Tx32x32)
		token, extra := encoder.CoeffTokenExtraCost(absVal, sign)
		pt := vp9dec.GetCoefContext(neighbors, &e.modeScratch, c)
		rate += extra + encoder.CoeffTreeTokenCost(
			(*coefModel)[band][pt][:], prevToken == encoder.ZeroToken, token)
		e.modeScratch[raster] = encoder.PtEnergyClass[token]
		if bandLeft > 0 {
			bandLeft--
			if bandLeft == 0 {
				band++
				if band < len(vp9CoeffCostBandCounts[txSize]) {
					bandLeft = vp9CoeffCostBandCounts[txSize][band]
				}
			}
		}
		prevToken = token
	}
	if bandLeft != 0 && band < vp9dec.CoefBands {
		pt := vp9dec.GetCoefContext(neighbors, &e.modeScratch, eob)
		rate += encoder.CoeffTreeTokenCost((*coefModel)[band][pt][:], false,
			encoder.EobToken)
	}
	return rate
}

// vp9CoeffBlockRateCostFastQ ports the use_fast_coef_costing arm of libvpx's
// cost_coeffs (vp9/encoder/vp9_rdopt.c:387-416). The fast arm advances through
// band_counts rather than recomputing get_coef_context() for every non-zero AC
// coefficient, and indexes the token-cost slab with [!prev_token][!prev_token].
// govpx computes the same token costs on demand from fc.CoefProbs instead of
// caching x->token_costs.
func (e *VP9Encoder) vp9CoeffBlockRateCostFastQ(txSize common.TxSize,
	coefModel *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8,
	scanOrder common.ScanOrder, dequant [2]int16, coeffs, qcoeffs []int16,
	initCtx int,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txSize >= common.TxSizes || coefModel == nil || dequant[0] == 0 ||
		dequant[1] == 0 || len(coeffs) < maxEob || initCtx < 0 ||
		initCtx > 2 {
		return 0
	}
	if qcoeffs != nil && len(qcoeffs) < maxEob {
		qcoeffs = nil
	}
	scan := scanOrder.Scan
	if len(scan) < maxEob {
		return 0
	}
	eob := vp9CoeffBlockEOB(scan, maxEob, coeffs, qcoeffs)
	if eob == 0 {
		return encoder.CoeffTreeTokenCost((*coefModel)[0][initCtx][:], false,
			encoder.EobToken)
	}

	rate := 0
	dcAbs, dcSign := vp9CoeffMagnitudeAndSign(qcoeffs, 0, coeffs[0],
		dequant[0], txSize == common.Tx32x32)
	prevToken, extraCost := encoder.CoeffTokenExtraCost(dcAbs, dcSign)
	rate += extraCost
	rate += encoder.CoeffTreeTokenCost((*coefModel)[0][initCtx][:], false,
		prevToken)

	bandIdx := 1
	bandLeft := vp9CoeffCostBandCounts[txSize][bandIdx]
	for c := 1; c < eob; c++ {
		raster := int(scan[c])
		absVal, sign := vp9CoeffMagnitudeAndSign(qcoeffs, raster,
			coeffs[raster], dequant[1], txSize == common.Tx32x32)
		token, extra := encoder.CoeffTokenExtraCost(absVal, sign)
		ctx := 0
		skipEOB := false
		if prevToken == encoder.ZeroToken {
			ctx = 1
			skipEOB = true
		}
		rate += extra
		rate += encoder.CoeffTreeTokenCost((*coefModel)[bandIdx][ctx][:],
			skipEOB, token)
		prevToken = token
		bandLeft--
		if bandLeft == 0 {
			bandIdx++
			if bandIdx >= len(vp9CoeffCostBandCounts[txSize]) {
				break
			}
			bandLeft = vp9CoeffCostBandCounts[txSize][bandIdx]
		}
	}
	if bandLeft != 0 {
		ctx := 0
		if prevToken == encoder.ZeroToken {
			ctx = 1
		}
		rate += encoder.CoeffTreeTokenCost((*coefModel)[bandIdx][ctx][:], false,
			encoder.EobToken)
	}
	return rate
}
