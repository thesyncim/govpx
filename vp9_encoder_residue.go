package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9PlaneRectSSEClamped returns the SSE between src and dst rectangles of
// size (w,h) starting at (x0,y0) in BOTH planes (src has dim srcW x srcH,
// dst has dim dstStride x dstRows). Clamps both src and dst coords to
// extents to mirror libvpx's pixel_sse which uses sum_squares_visible
// semantics. Returns false if dst access would be out-of-bounds at (x0,y0).
func vp9PlaneRectSSEClamped(src []byte, srcStride, srcW, srcH int,
	dst []byte, dstStride, x0, y0, w, h int,
) (uint64, bool) {
	if len(src) == 0 || srcStride <= 0 || len(dst) == 0 || dstStride <= 0 ||
		w <= 0 || h <= 0 {
		return 0, false
	}
	dstRows := len(dst) / dstStride
	if x0 < 0 || y0 < 0 || x0 >= dstStride || y0 >= dstRows {
		return 0, false
	}
	var sse uint64
	for y := range h {
		sy := y0 + y
		if sy >= srcH {
			sy = srcH - 1
		}
		dy := y0 + y
		if dy >= dstRows {
			dy = dstRows - 1
		}
		srcRow := src[sy*srcStride:]
		dstRow := dst[dy*dstStride:]
		for x := range w {
			sx := x0 + x
			if sx >= srcW {
				sx = srcW - 1
			}
			dx := x0 + x
			if dx >= dstStride {
				dx = dstStride - 1
			}
			diff := int(srcRow[sx]) - int(dstRow[dx])
			sse += uint64(diff * diff)
		}
	}
	return sse, true
}

func (e *VP9Encoder) prepareVP9KeyframeBlockResidue(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) bool {
	hasResidue := false
	segID := vp9EncoderMiSegmentID(mi)
	sc := e.vp9BlockCoeffScratch()
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - max4x4W) >> txSize) * blockStep
		blockIdx := 0
		dequant := key.dq.Y[segID]
		if plane > 0 {
			dequant = key.dq.Uv[segID]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				blockIdx4x4 := rr*full4x4W + cc
				if blockIdx4x4 >= 0 && blockIdx4x4 < len(sc.blockEOBs[plane]) {
					sc.blockEOBs[plane][blockIdx4x4] = 0
				}
				coeffBase, maxEob, coeffOK := vp9BlockCoeffOffset(planeBsize,
					rr, cc, txSize)
				if !coeffOK {
					continue
				}
				coeffs := e.coefScratch[:maxEob]
				qindex := vp9dec.GetSegmentQindex(&key.hdr.Seg, segID,
					int(key.hdr.Quant.BaseQindex))
				qcoeffs := sc.blockQCoeffs[plane][coeffBase : coeffBase+maxEob]
				if ok, eob := e.prepareVP9KeyframeTxResidueWithQEOB(key, pd, plane, mode,
					txSize, tile, miRows, miCols, miRow, miCol, bsize, rr, cc,
					dequant, qindex, coeffs, qcoeffs); ok {
					if blockIdx4x4 >= 0 && blockIdx4x4 < len(sc.blockEOBs[plane]) {
						sc.blockEOBs[plane][blockIdx4x4] = int16(eob)
					}
					hasResidue = true
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	return hasResidue
}

func (e *VP9Encoder) prepareVP9InterBlockResidue(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds, mi *vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams, forcedRefFrame int8, forcedRef bool,
	txMode common.TxMode, deferFinalInter bool,
) (vp9InterModeDecision, common.PredictionMode, bool, bool) {
	e.resetVP9ProducerTokens()
	interDecision, ok := e.prepareVP9InterPredictionBlock(inter, miRows, miCols,
		miRow, miCol, bsize, tile, mi, seg, forcedRefFrame, forcedRef)
	if !ok {
		return vp9InterModeDecision{}, common.DcPred, false, false
	}
	if interDecision.intra {
		mi.Mode = interDecision.mode
		// Committed intra-block mv sentinel. The NONRD picker
		// (vp9/encoder/vp9_pickmode.c:2644-2645) parks mv[0]/mv[1] at INVALID_MV;
		// the FULL-RD picker (vp9/encoder/vp9_rdopt.c:3990, the
		// `if (ref_frame == INTRA_FRAME) mi->mv[0].as_int = 0;` best-mode commit)
		// parks mv[0] at 0 ("required for left and above block mv"). Both keep the
		// NEWMV-diff-bias neighbour check byte-exact (INVALID_MV is rejected,
		// mv==0 is a valid zero candidate), but the committed value differs and is
		// read back by the neighbour MV scan, so the path must match: cpu4 full-RD
		// commits 0, cpu>=5 nonrd commits INVALID_MV.
		mi.Mv = e.vp9InterIntraCommitMv()
		mi.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
		mi.InterpFilter = uint8(vp9dec.SwitchableFilters)
		if interDecision.txSize < common.TxSizes {
			mi.TxSize = interDecision.txSize
		}
		uvMode := interDecision.uvMode
		if uvMode < common.DcPred || int(uvMode) >= common.IntraModes {
			uvMode = interDecision.mode
		}
		return interDecision, uvMode, e.prepareVP9InterIntraBlockResidue(inter, tile,
			miRows, miCols, miRow, miCol, bsize, mi, uvMode), false
	}
	if e.opts.AQMode == VP9AQComplexity {
		mi.TxSize = e.pickVP9InterTxSize(inter, tile, miRows, miCols, miRow, miCol,
			bsize, mi.TxSize, mi.SegmentID)
		projectedRate := interDecision.rate
		if _, coeffRate, hasTxResidue, ok := e.scoreVP9InterTxCandidate(inter,
			miRows, miCols, miRow, miCol, bsize, mi.TxSize, true); ok && hasTxResidue {
			projectedRate += coeffRate
		}
		e.applyVP9ComplexityAQSegment(inter, miRow, miCol, bsize, mi,
			projectedRate)
	}
	if !forcedRef && e.vp9StaticThresholdBreakout(inter, miRows, miCols,
		miRow, miCol, bsize, mi) {
		e.vp9AccumulateBlockFilterDiff(inter, interDecision.score, true)
		return interDecision, common.DcPred, false, false
	}
	if e.opts.AQMode != VP9AQComplexity {
		// libvpx vp9/encoder/vp9_encodeframe.c:6100 encode_superblock reads the
		// tx_size the full-RD mode search committed into mi->tx_size
		// (vp9_rd_pick_inter_mode_sb -> super_block_yrd choose_tx_size_from_rd,
		// vp9_rdopt.c). It does NOT re-derive the tx_size at encode time. On the
		// deep full-RD use-partition path the committed leaf decision already
		// carries that choose_tx_size_from_rd tx_size (interDecision.txSize); the
		// realtime pickVP9InterTxSize heuristic that the nonrd path uses would
		// override it with a different size (e.g. mi(1,3) frame-1 of {0,1,1,0,1}:
		// full-RD commits TX_8X8 but the realtime picker returns TX_4X4), which
		// flips the per-block tx_size field AND the residual token decomposition.
		// Gate on the deep flag so production (flag off) keeps the nonrd picker
		// byte-for-byte.
		if e.vp9InterUsesNonrdPickmode() && interDecision.txSize < common.TxSizes {
			// Realtime nonrd follows the same committed-mbmi rule: libvpx's
			// vp9_pick_inter_mode stores best_pickmode.best_tx_size into
			// mi->tx_size and encode_superblock reads it back. Reusing the
			// picker result also keeps the count pass and tile pass on the
			// same decision without rerunning the residual-stat tx picker.
			mi.TxSize = clampVP9TxSizeForBlock(interDecision.txSize, bsize)
		} else if vp9InterUseDeepRDUsePartition && interDecision.txSize < common.TxSizes {
			mi.TxSize = interDecision.txSize
		} else {
			mi.TxSize = e.pickVP9InterTxSize(inter, tile, miRows, miCols, miRow, miCol,
				bsize, mi.TxSize, mi.SegmentID)
		}
	}
	// libvpx routes the realtime nonrd inter frame purely through
	// vp9_pick_inter_mode (vp9_encodeframe.c::nonrd_pick_sb_modes:4422-4435),
	// whose own intra fallback (vp9_pickmode.c:2527-2648) is the only intra
	// evaluation. There is no second intra re-decode at residue/encode time in
	// that path, so the picker's committed inter/intra decision is final. Only
	// the full-RD path (vp9_rd_pick_inter_mode_sb) evaluates intra alongside the
	// inter modes, which this secondary picker models. Gating on !useNonrd keeps
	// the nonrd leaf decision untouched here.
	if !inter.lossless && !forcedRef && !e.vp9InterUsesNonrdPickmode() {
		if intra, ok := e.pickVP9InterIntraMode(inter, tile, miRows, miCols,
			miRow, miCol, bsize, mi.TxSize, interDecision.score); ok {
			mi.Mode = intra.mode
			// libvpx vp9_pickmode.c:2644-2645 — intra winner mv sentinel.
			mi.Mv = [2]vp9dec.MV{vp9dec.InvalidMV, vp9dec.InvalidMV}
			mi.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
			mi.InterpFilter = uint8(vp9dec.SwitchableFilters)
			interDecision.intra = true
			interDecision.mode = intra.mode
			interDecision.rate = intra.rate
			interDecision.score = intra.score
			e.vp9AccumulateBlockFilterDiff(inter, intra.score, true)
			return interDecision, intra.uvMode, e.prepareVP9InterIntraBlockResidue(inter, tile,
				miRows, miCols, miRow, miCol, bsize, mi, intra.uvMode), false
		}
	}
	e.applyVP9DenoiserToInterBlock(inter, miRows, miCols, miRow, miCol,
		bsize, interDecision)
	// libvpx vp9/encoder/vp9_rdopt.c:4149,4173 commits mi->skip = best_skip2 ||
	// best_mode_skippable; encode_superblock then leaves mi->skip set and
	// vp9_encode_sb/tokenize emit no residual for the whole block (the skip bit
	// codes it). On the deep full-RD use-partition path the committed decision
	// already carries that skip flag; honour it directly instead of re-deriving
	// skip from the re-quantized residual (which would code chroma/Y residual
	// for a block libvpx codes skip — e.g. {0,1,1,0,1} frame-1 mi(2,0)/mi(5,3)).
	// The predictor is already on the recon plane (predictVP9InterBlock), so a
	// skip block's reconstruction is the predictor, matching libvpx. Gate on the
	// deep flag so production keeps deriving skip from the residue.
	if vp9InterUseDeepRDUsePartition && interDecision.skip {
		e.vp9AccumulateBlockFilterDiff(inter, interDecision.score, false)
		return interDecision, common.DcPred, false, false
	}
	if deferFinalInter {
		return interDecision, common.DcPred, false, true
	}
	hasResidue := e.prepareVP9FinalInterBlockResidue(inter, miRows, miCols,
		miRow, miCol, bsize, mi, interDecision, forcedRef, txMode)
	return interDecision, common.DcPred, hasResidue, false
}

func (e *VP9Encoder) prepareVP9FinalInterBlockResidue(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi, interDecision vp9InterModeDecision, forcedRef bool,
	txMode common.TxMode,
) bool {
	hasResidue := false
	segID := vp9EncoderMiSegmentID(mi)
	// libvpx encode_block (vp9/encoder/vp9_encodemb.c:580) forces the Y-plane
	// transform unit's eob to 0 when the full-RD mode search marked it in
	// x->zcoeff_blk[tx_size][block] (rd1 > rd2: coding the residual costs more
	// than skipping it). On the deep full-RD use-partition path the committed
	// leaf's predictor is already on the recon plane, so recompute that
	// per-block decision here and apply it to the FP-quant tokenize pass; the
	// nonrd path (flag off) keeps coding every nonzero block as before.
	var zcoeff vp9InterZcoeffBlk
	if vp9InterUseDeepRDUsePartition && !inter.lossless {
		zcoeff, _ = e.vp9ComputeInterLeafZcoeffBlk(inter, miRows, miCols,
			miRow, miCol, bsize, mi.TxSize, uint8(segID))
	}
	sc := e.vp9BlockCoeffScratch()
	producerTxStable := txMode == common.TxModeSelect ||
		mi.TxSize == min(common.TxModeToBiggestTxSize[txMode], common.MaxTxsizeLookup[bsize])
	stageTokens := e.canStageVP9ProducerTokens(inter, bsize, forcedRef) &&
		producerTxStable && e.beginVP9ProducerTokens(miRow, miCol, bsize, mi.TxSize)
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		dequant := inter.dq.Y[segID]
		if plane > 0 {
			dequant = inter.dq.Uv[segID]
		}
		if txSize >= common.TxSizes {
			stageTokens = false
			continue
		}
		step := 1 << uint(txSize)
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		if maxEob > vp9EncoderTxCoeffSlots ||
			dequant[0] == 0 || dequant[1] == 0 ||
			(inter.lossless && txSize != common.Tx4x4) {
			stageTokens = false
			continue
		}
		fpTables := e.vp9QuantFPTablesForPlaneSegment(plane, segID, dequant)
		scanOrder := &common.ScanOrders[txSize][common.DctDct]
		if inter.lossless {
			scanOrder = &common.DefaultScanOrders[txSize]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				blockIdx4x4 := rr*full4x4W + cc
				if blockIdx4x4 >= 0 && blockIdx4x4 < len(sc.blockEOBs[plane]) {
					sc.blockEOBs[plane][blockIdx4x4] = 0
				}
				coeffBase, compactMaxEob, coeffOK := vp9BlockCoeffOffset(planeBsize,
					rr, cc, txSize)
				if !coeffOK || compactMaxEob != maxEob {
					stageTokens = false
					continue
				}
				qcoeffs := sc.blockQCoeffs[plane][coeffBase : coeffBase+maxEob]
				// libvpx zcoeff_blk zero-forcing is luma-only (plane == 0,
				// vp9_encodemb.c:580). A forced block keeps eob 0 (no tokens)
				// and leaves the predictor in recon (no inverse-add), so skip
				// the FP quantize/tokenize for it entirely.
				if plane == 0 && zcoeff.valid {
					if idx := blockIdx4x4; idx >= 0 &&
						idx < len(zcoeff.flags) && zcoeff.flags[idx] {
						if stageTokens && !e.stageVP9ProducerBlock(plane, txSize, rr, cc,
							dequant, scanOrder, qcoeffs, 0, inter.counts) {
							e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
							stageTokens = false
						}
						continue
					}
				}
				if e.vp9InterSkipTxfmACDCLuma(inter, interDecision,
					plane, segID) {
					if stageTokens && !e.stageVP9ProducerBlock(plane, txSize, rr, cc,
						dequant, scanOrder, qcoeffs, 0, inter.counts) {
						e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
						stageTokens = false
					}
					continue
				}
				coeffs := e.coefScratch[:maxEob]
				txHasResidue, eob := e.prepareVP9InterTxResidueDCTFPPrechecked(inter, pd, plane,
					txSize, miRow, miCol, rr, cc, maxEob, scanOrder, dequant,
					fpTables, coeffs, qcoeffs)
				if blockIdx4x4 >= 0 && blockIdx4x4 < len(sc.blockEOBs[plane]) {
					sc.blockEOBs[plane][blockIdx4x4] = int16(eob)
				}
				if txHasResidue {
					hasResidue = true
				}
				if stageTokens && !e.stageVP9ProducerBlock(plane, txSize, rr, cc,
					dequant, scanOrder, qcoeffs, eob, inter.counts) {
					e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
					stageTokens = false
				}
			}
		}
	}
	if stageTokens {
		e.finishVP9ProducerTokens(hasResidue)
	} else {
		e.abortVP9ProducerTokens()
	}
	e.vp9AccumulateBlockFilterDiff(inter, interDecision.score, false)
	return hasResidue
}

func (e *VP9Encoder) vp9InterSkipTxfmACDCLuma(inter *vp9InterEncodeState,
	decision vp9InterModeDecision, plane, segID int,
) bool {
	return e != nil && inter != nil &&
		e.vp9InterUsesNonrdPickmode() &&
		e.sf.UseQuantFp != 0 &&
		plane == 0 && segID == 0 && !inter.lossless &&
		decision.skipTxfm == encoder.SkipTxfmAcDc
}

func (e *VP9Encoder) vp9StaticThresholdBreakout(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) bool {
	threshold := e.opts.StaticThreshold
	if threshold <= 0 || inter == nil || mi == nil || inter.dq == nil ||
		inter.lossless || bsize < common.Block8x8 {
		return false
	}
	refFrame := mi.RefFrame[0]
	if refFrame <= vp9dec.IntraFrame || refFrame >= vp9dec.MaxRefFrames {
		return false
	}
	if e.opts.SpatialScalability.Enabled && refFrame == vp9dec.GoldenFrame {
		return false
	}
	mv := mi.Mv[0]
	if mv.Row < -64 || mv.Row > 64 || mv.Col < -64 || mv.Col > 64 {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pred, predStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
		return false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	predH := 0
	if predStride > 0 {
		predH = len(pred) / predStride
	}
	if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
		!encoder.VisibleBlockFits(x0, y0, blockW, blockH, predStride, predH) {
		return false
	}
	segID := vp9EncoderMiSegmentID(mi)
	threshAC, threshDC := vp9StaticThresholds(threshold, inter.dq.Y[segID],
		bsize)
	varY, sseY := encoder.BlockDiffVarianceSSE(src, srcStride, pred, predStride,
		x0, y0, x0, y0, blockW, blockH)
	if varY > threshAC || sseY-varY > threshDC {
		return false
	}
	return e.vp9StaticThresholdChromaBreakout(inter, miRow, miCol, bsize,
		threshAC, threshDC)
}

func (e *VP9Encoder) vp9StaticThresholdChromaBreakout(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, threshAC, threshDC uint64,
) bool {
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return false
		}
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		pred, predStride := e.vp9EncoderReconPlane(plane)
		if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
			return false
		}
		blockW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		predH := len(pred) / predStride
		if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
			!encoder.VisibleBlockFits(x0, y0, blockW, blockH, predStride, predH) {
			return false
		}
		variance, sse := encoder.BlockDiffVarianceSSE(src, srcStride, pred,
			predStride, x0, y0, x0, y0, blockW, blockH)
		if (variance<<2) > threshAC || sse-variance > threshDC {
			return false
		}
	}
	return true
}

func vp9StaticThresholds(threshold int, yDequant [2]int16,
	bsize common.BlockSize,
) (uint64, uint64) {
	const maxThresh = uint64(36000)
	minThresh := maxThresh
	if threshold < int(maxThresh>>4) {
		minThresh = uint64(threshold) << 4
	}
	yAC := int(yDequant[1])
	threshAC := uint64(yAC*yAC) >> 3
	if threshAC < minThresh {
		threshAC = minThresh
	} else if threshAC > maxThresh {
		threshAC = maxThresh
	}
	shift := 8 - int(common.BWidthLog2Lookup[bsize]+common.BHeightLog2Lookup[bsize])
	if shift > 0 {
		threshAC >>= uint(shift)
	}
	yDC := int(yDequant[0])
	return threshAC, uint64(yDC*yDC) >> 6
}

func (e *VP9Encoder) prepareVP9InterPredictionBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds, mi *vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams, forcedRefFrame int8, forcedRef bool,
) (vp9InterModeDecision, bool) {
	e.vp9ClearBlockFilterRDScores()
	if mi == nil {
		return vp9InterModeDecision{}, false
	}
	mi.Mode = common.ZeroMv
	mi.Mv = [2]vp9dec.MV{}
	mi.RefFrame = [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}
	mi.InterpFilter = uint8(vp9dec.InterpEighttap)
	var picked vp9InterModeDecision
	pickedValid := false
	if forcedRef {
		refSlot, ok := e.vp9InterReferenceSlot(inter, forcedRefFrame)
		if !ok {
			return vp9InterModeDecision{}, false
		}
		inter.ref = &e.refFrames[refSlot]
		mi.RefFrame = [2]int8{forcedRefFrame, vp9dec.NoRefFrame}
		if decision, ok := e.pickVP9InterMode(inter, tile, miRows, miCols,
			miRow, miCol, bsize, forcedRefFrame, 0, vp9FullRDRefState{},
			0, false, nil); ok {
			picked = decision
			picked.refFrame = forcedRefFrame
			picked.secondRefFrame = vp9dec.NoRefFrame
			picked.refSlot = refSlot
			mi.Mode = decision.mode
			mi.Mv = decision.mv
			mi.Bmi = decision.bmi
			mi.InterpFilter = uint8(decision.interpFilter)
			pickedValid = true
		}
	} else if cached, cok := e.vp9LookupDeepInterRDDecisionForWrite(miRows, miCols,
		miRow, miCol, bsize); cok {
		// SEARCH->WRITE replay (vp9InterUseDeepRDPartition only): the
		// depth-first full-RD partition search (pickVP9InterPartitionRD)
		// already committed this leaf's decision into the deep cache as it
		// filled the mi grid. Replay it verbatim so the writer emits exactly
		// what the search chose, instead of re-running pickVP9InterReferenceMode
		// with a different x->pred_mv / interp-filter context than the search
		// ran (the bug that committed garbage MVs to the deep recursion's
		// leaves). libvpx replays the cached mbmi at write_modes_b without
		// re-picking (vp9/encoder/vp9_bitstream.c).
		picked = cached
		mi.Mode = cached.mode
		mi.Mv = cached.mv
		mi.Bmi = cached.bmi
		mi.RefFrame = [2]int8{cached.refFrame, cached.secondRefFrame}
		mi.InterpFilter = uint8(cached.interpFilter)
		if cached.txSize < common.TxSizes {
			mi.TxSize = cached.txSize
		}
		if !cached.intra {
			inter.ref = &e.refFrames[cached.refSlot]
		}
		pickedValid = true
	} else if cached, ok := e.lookupVP9LeafInterDecision(miRow, miCol, bsize); ok {
		// libvpx: vp9/encoder/vp9_bitstream.c::write_modes_b reads the
		// stored picker decision from mi[0]->mbmi without re-invoking
		// the picker. The cache populated by the prior count pre-pass
		// supplies the same decision for this leaf-write call site.
		picked = cached
		mi.Mode = cached.mode
		mi.Mv = cached.mv
		mi.Bmi = cached.bmi
		mi.RefFrame = [2]int8{cached.refFrame, cached.secondRefFrame}
		mi.InterpFilter = uint8(cached.interpFilter)
		if cached.txSize < common.TxSizes {
			mi.TxSize = cached.txSize
		}
		if !cached.intra {
			inter.ref = &e.refFrames[cached.refSlot]
		}
		pickedValid = true
	} else if decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, bsize); ok {
		picked = decision
		mi.Mode = decision.mode
		mi.Mv = decision.mv
		mi.Bmi = decision.bmi
		mi.RefFrame = [2]int8{decision.refFrame, decision.secondRefFrame}
		mi.InterpFilter = uint8(decision.interpFilter)
		if decision.txSize < common.TxSizes {
			mi.TxSize = decision.txSize
		}
		if !decision.intra {
			inter.ref = &e.refFrames[decision.refSlot]
		}
		pickedValid = true
	} else if refFrame, refSlot, ok := e.firstVP9InterReference(inter); ok {
		mi.RefFrame[0] = refFrame
		inter.ref = &e.refFrames[refSlot]
	} else {
		return vp9InterModeDecision{}, false
	}
	if pickedValid && picked.intra {
		mi.Mode = picked.mode
		// libvpx vp9_pickmode.c:2644-2645 — intra winner mv sentinel.
		mi.Mv = [2]vp9dec.MV{vp9dec.InvalidMV, vp9dec.InvalidMV}
		mi.RefFrame = [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}
		mi.InterpFilter = uint8(vp9dec.SwitchableFilters)
		if picked.txSize < common.TxSizes {
			mi.TxSize = picked.txSize
		}
		return picked, true
	}
	if pickedValid && picked.lumaPredReady {
		if !e.predictVP9InterBlockChromaOnly(inter, miRows, miCols, miRow, miCol,
			bsize, mi) {
			return vp9InterModeDecision{}, false
		}
	} else {
		if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi) {
			return vp9InterModeDecision{}, false
		}
	}
	return picked, true
}

func (e *VP9Encoder) prepareVP9InterSkipPrediction(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, forcedRefFrame int8, forcedRef bool,
) bool {
	if inter == nil || mi == nil {
		return false
	}
	refFrame := mi.RefFrame[0]
	if forcedRef {
		refFrame = forcedRefFrame
	}
	refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
	if !ok && !forcedRef {
		refFrame, refSlot, ok = e.firstVP9InterReference(inter)
	}
	if !ok {
		return false
	}
	mi.Mode = common.ZeroMv
	mi.Mv = [2]vp9dec.MV{}
	mi.RefFrame = [2]int8{refFrame, vp9dec.NoRefFrame}
	mi.InterpFilter = uint8(vp9dec.InterpEighttap)
	inter.ref = &e.refFrames[refSlot]
	return e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi)
}

func (e *VP9Encoder) pickVP9InterTxSize(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, maxTx common.TxSize, segmentID uint8,
) common.TxSize {
	if inter == nil || inter.dq == nil || bsize >= common.BlockSizes {
		return maxTx
	}
	maxTx = clampVP9TxSizeForBlock(maxTx, bsize)
	if maxTx < common.Tx8x8 {
		return maxTx
	}
	if miRow+int(common.Num8x8BlocksHighLookup[bsize]) > miRows ||
		miCol+int(common.Num8x8BlocksWideLookup[bsize]) > miCols {
		return maxTx
	}
	sse, activity, ok := e.vp9InterTxResidualStats(inter, miRow, miCol, bsize)
	pixels := uint64(common.Num4x4BlocksWideLookup[bsize]) *
		uint64(common.Num4x4BlocksHighLookup[bsize]) * 16
	if !ok {
		return maxTx
	}
	// limitTx mirrors libvpx vp9/encoder/vp9_pickmode.c:370-373
	// (calculate_tx_size) — under CYCLIC_REFRESH_AQ the encoder lifts the
	// inter Tx16x16 cap when source variance or residual variance is zero
	// (var_thresh = 1 for inter, i.e. is_intra=0). Outside CYCLIC_REFRESH_AQ
	// limit_tx stays 1 and the libvpx Tx16x16 ceiling applies.
	limitTx := e.vp9InterCalculateTxLimitTx(inter, miRow, miCol, bsize, sse)
	// libvpx vp9_pickmode.c:380-388 — the boosted-segment Tx8x8 force and
	// screen-content Tx4x4 force apply once the picker has produced a
	// candidate tx_size. acThr mirrors model_rd_for_sb_y at vp9_pickmode.c:
	// 658 (`ac_thr = p->quant_thred[1] >> 6`); quant_thred[1] is computed
	// as zbin[1]^2 at vp9_quantize.c:265 with zbin[1] =
	// ROUND_POWER_OF_TWO(qzbin_factor * ac_quant, 7) (vp9_quantize.c:211).
	acThr := e.vp9InterCalculateTxAcThr(inter, segmentID)
	// residualVar derived from the same sse/sum-of-differences as
	// vp9InterCalculateTxLimitTx so the screen-content force uses the
	// same variance the libvpx model_rd_for_sb_y feeds calculate_tx_size
	// (vp9_pickmode.c:668). residualVar == 0 retains the prior limit_tx
	// semantics; the full uint64 value now feeds the (var >> 5) > ac_thr
	// screen-content Tx4x4 force at vp9_pickmode.c:386-388.
	sourceVar, residualVar, _ := e.vp9InterTxSourceAndResidualVar(inter, miRow,
		miCol, bsize, sse)
	if e.vp9InterUsesNonrdPickmode() {
		return e.vp9InterCalculateTxSize(bsize, vp9InterFrameTxMode(inter), sse,
			residualVar, sourceVar, acThr, segmentID)
	}
	useTxRDSearch := vp9InterFrameTxMode(inter) == common.TxModeSelect &&
		e.sf.TxSizeSearchMethod == UseTx8x8
	if !useTxRDSearch && maxTx == common.Tx8x8 &&
		sse > pixels*512 && activity > pixels*128 {
		return e.vp9InterTxApplyForces(maxTx, bsize, residualVar, acThr,
			limitTx, segmentID)
	}
	if !useTxRDSearch && (sse <= pixels*512 || activity <= pixels*16) {
		// libvpx vp9_pickmode.c:371-384: in the CYCLIC_REFRESH_AQ flat
		// region (limit_tx=0) the Tx16x16 ceiling is dropped, so the
		// picker can return maxTx (up to Tx32x32) directly without
		// running the score-based RDO. For limit_tx=1 the libvpx
		// Tx16x16 cap still applies.
		if !limitTx {
			return e.vp9InterTxApplyForces(maxTx, bsize, residualVar,
				acThr, limitTx, segmentID)
		}
		// The realtime oracle keeps smooth changed inter blocks below
		// 32x32, while still allowing textured residuals to use the
		// scored Tx32 path below.
		tx := min(maxTx, common.Tx16x16)
		return e.vp9InterTxApplyForces(tx, bsize, residualVar, acThr,
			limitTx, segmentID)
	}
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, bsize)
	if !ok {
		return maxTx
	}
	defer e.releaseVP9PartitionReconSnapshot(reconSnap)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	txCtx := vp9dec.GetTxSizeContext(above, left, maxTx)
	txProbs := vp9TxProbsRow(&e.fc.TxProbs, maxTx, txCtx)
	qindex := e.vp9EncoderModeDecisionQIndex()

	bestTx := maxTx
	bestScore := uint64(^uint64(0))
	bestRate := int(^uint(0) >> 1)
	minTx := max(maxTx-1, common.Tx4x4)
	if useTxRDSearch {
		minTx = vp9InterTxRDSearchMin(maxTx, e.sf.TxSizeSearchDepth, bsize)
	}
	for txi := int(maxTx); txi >= int(minTx); txi-- {
		tx := common.TxSize(txi)
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		distortion, coeffRate, hasResidue, ok := e.scoreVP9InterTxCandidate(inter,
			miRows, miCols, miRow, miCol, bsize, tx, false)
		if !ok {
			continue
		}
		rate := 0
		if hasResidue {
			rate = coeffRate + encoder.TxSizeRateCost(txProbs, tx, maxTx)
		}
		score := e.vp9ModeDecisionScore(distortion, rate, qindex)
		if score < bestScore || (score == bestScore && rate < bestRate) {
			bestScore = score
			bestRate = rate
			bestTx = tx
		}
	}
	e.restoreVP9PartitionReconSnapshot(reconSnap)
	return e.vp9InterTxApplyForces(bestTx, bsize, residualVar, acThr,
		limitTx, segmentID)
}

func vp9InterTxRDSearchMin(maxTx common.TxSize, depth int,
	bsize common.BlockSize,
) common.TxSize {
	endTx := max(int(maxTx)-depth, int(common.Tx4x4))
	if bsize > common.Block32x32 {
		endTx = min(endTx+1, int(maxTx))
	}
	return common.TxSize(endTx)
}

// vp9InterTxApplyForces folds in the libvpx-verbatim boosted-segment
// Tx8x8 force from vp9/encoder/vp9_pickmode.c:380-384 (inside
// calculate_tx_size) plus the VP9E_CONTENT_SCREEN Tx4x4 force at
// vp9_pickmode.c:386-388 on top of govpx's score-based picker output.
// libvpx evaluates:
//
//	if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && limit_tx &&
//	    cyclic_refresh_segment_id_boosted(xd->mi[0]->segment_id))
//	  tx_size = TX_8X8;
//	else if (tx_size > TX_16X16 && limit_tx)
//	  tx_size = TX_16X16;
//	// For screen-content force 4X4 tx_size over 8X8, for large variance.
//	if (cpi->oxcf.content == VP9E_CONTENT_SCREEN && tx_size == TX_8X8 &&
//	    bsize <= BLOCK_16X16 && ((var >> 5) > (unsigned int)ac_thr))
//	  tx_size = TX_4X4;
//
// residualVar mirrors the libvpx `var` passed to calculate_tx_size by
// model_rd_for_sb_y at vp9_pickmode.c:668 — the same residual variance
// computed in vp9InterTxSourceAndResidualVar via
// sse - ((sum*sum) >> (bw+bh+4)).
func (e *VP9Encoder) vp9InterTxApplyForces(tx common.TxSize, bsize common.BlockSize,
	residualVar uint64, acThr int64, limitTx bool, segmentID uint8,
) common.TxSize {
	if e == nil {
		return tx
	}
	return encoder.ApplyTxSizeForces(encoder.TxSizeForcesArgs{
		TxSize:          tx,
		BSize:           bsize,
		VarY:            residualVar,
		ACThreshold:     acThr,
		LimitTx:         limitTx,
		CyclicRefreshAQ: e.opts.AQMode == VP9AQCyclicRefresh,
		SegmentID:       segmentID,
		ScreenContent:   e.opts.ScreenContentMode == int8(VP9ScreenContentScreen),
	})
}

// vp9InterCalculateTxAcThr ports libvpx's
// `ac_thr = p->quant_thred[1] >> 6` (vp9/encoder/vp9_pickmode.c:658) and
// `quant_thred[1] = zbin[1]^2` (vp9/encoder/vp9_quantize.c:265) with
// zbin[1] = ROUND_POWER_OF_TWO(qzbin_factor * ac_quant, 7)
// (vp9/encoder/vp9_quantize.c:211). ac_quant is dequant[1] for the Y
// plane at the segment qindex.
//
// The ac_thr_factor scaling at vp9_pickmode.c:494/497 is independent of
// the per-block segment id and feeds the abs(sum) >> (bw+bh) check that
// only fires at speed >= 8 / norm_sum < 5. govpx does not yet thread
// the per-block norm_sum into the picker; the factor defaults to 1
// outside that gate, so the ac_thr returned here matches libvpx for
// every speed < 8 path and approximates libvpx for the speed=8 path
// where norm_sum >= 5 (the textured-residual majority).
func (e *VP9Encoder) vp9InterCalculateTxAcThr(inter *vp9InterEncodeState,
	segmentID uint8,
) int64 {
	if e == nil || inter == nil || inter.dq == nil ||
		int(segmentID) >= len(inter.dq.Y) {
		return 0
	}
	_, acThr := encoder.ModelRdQuantThresholds(e.vp9SegmentQIndex(inter, segmentID),
		inter.dq.Y[segmentID])
	return acThr
}

// vp9InterCalculateTxLimitTx is a verbatim port of the limit_tx
// computation from libvpx vp9/encoder/vp9_pickmode.c:370-373 inside
// calculate_tx_size, specialised for the inter path (is_intra=0).
// libvpx evaluates:
//
//	int limit_tx = 1;
//	if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ &&
//	    (source_variance == 0 || var < var_thresh))
//	  limit_tx = 0;
//
// where var_thresh = is_intra ? ac_thr : 1, so for inter we have
// var_thresh = 1 and the only way the predicate fires is when either
// source_variance or var equals zero.
//
// govpx computes the residual variance from sse and sum of differences
// as libvpx does (var = sse - (sum*sum) >> (bw+bh+4)). When the
// residual is constant the variance is zero and limit_tx flips to 0;
// otherwise the libvpx Tx16x16 cap stays in place.
func (e *VP9Encoder) vp9InterCalculateTxLimitTx(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, sse uint64,
) bool {
	if e == nil || inter == nil || bsize >= common.BlockSizes {
		return true
	}
	if e.opts.AQMode != VP9AQCyclicRefresh {
		// libvpx vp9_pickmode.c:371 — limit_tx defaults to 1 outside
		// CYCLIC_REFRESH_AQ.
		return true
	}
	srcVar, residVar, ok := e.vp9InterTxSourceAndResidualVar(inter, miRow,
		miCol, bsize, sse)
	if !ok {
		return true
	}
	// var_thresh = 1 for is_intra=0; var < 1 ⇔ var == 0 since var is
	// unsigned. source_variance == 0 || var == 0 toggles limit_tx to 0.
	if srcVar == 0 || residVar == 0 {
		return false
	}
	return true
}

// vp9InterTxSourceAndResidualVar returns the libvpx source_variance
// (block luma variance about its mean) and the residual variance
// computed as `sse - ((sum_diff*sum_diff) >> (bw+bh+4))`. The bw/bh
// shift mirrors libvpx vp9_pickmode.c:481 / vpx_dsp variance.c
// variance(), which divides sum_sqr by the pixel count (4<<bw * 4<<bh
// = 16 << (bw+bh)) using a fixed right-shift. residualVar equals the
// libvpx `var` value model_rd_for_sb_y passes into calculate_tx_size
// at vp9_pickmode.c:668 — both the
// `cyclic_refresh limit_tx` predicate (var < var_thresh) and the
// screen-content Tx4x4 force (`(var >> 5) > ac_thr`) consume it.
func (e *VP9Encoder) vp9InterTxSourceAndResidualVar(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, sse uint64,
) (sourceVar uint64, residualVar uint64, ok bool) {
	if inter == nil {
		return 0, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pred, predStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	predRows := len(pred) / predStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > predStride || y0+blockH > predRows {
		return 0, 0, false
	}
	var srcSum int64
	var srcSse uint64
	var diffSum int64
	for y := range blockH {
		srcRow := src[(y0+y)*srcStride:]
		predRow := pred[(y0+y)*predStride:]
		for x := range blockW {
			s := int(srcRow[x0+x])
			p := int(predRow[x0+x])
			srcSum += int64(s)
			srcSse += uint64(s * s)
			diffSum += int64(s - p)
		}
	}
	n := int64(blockW * blockH)
	if n <= 0 {
		return 0, 0, false
	}
	// libvpx source_variance in vp9_block.h:120 is the variance about the
	// block mean: sum(x*x) - (sum(x))^2 / N. Computed in floor-divide form
	// so values exactly equal to the unbiased variance for byte input.
	srcMeanSqr := uint64((srcSum * srcSum) / n)
	if srcSse > srcMeanSqr {
		sourceVar = srcSse - srcMeanSqr
	}
	// residual variance: sse - (sum*sum) >> (bw+bh+4). bw,bh from libvpx
	// b_{width,height}_log2_lookup give blockW=4<<bw, blockH=4<<bh.
	bwLog2 := int(common.BWidthLog2Lookup[bsize])
	bhLog2 := int(common.BHeightLog2Lookup[bsize])
	shift := uint(bwLog2 + bhLog2 + 4)
	sumSqr := uint64((diffSum * diffSum) >> shift)
	if sse > sumSqr {
		residualVar = sse - sumSqr
	}
	return sourceVar, residualVar, true
}

// vp9InterCalculateTxSize is a verbatim port of libvpx
// vp9/encoder/vp9_pickmode.c:363-393 (calculate_tx_size) specialised
// for the inter path (is_intra=0, var_thresh = 1). Currently used as
// a reference oracle by the limit_tx-aware post-pass in
// pickVP9InterTxSize and by future select_tx_mode rewiring; the
// govpx inter picker still drives its score-based RDO on top of
// libvpx's limit_tx semantics so it can preserve byte parity against
// the established heuristic baseline while exposing the libvpx
// CYCLIC_REFRESH_AQ var=0 escape.
//
// libvpx reference (vp9_pickmode.c:363-393):
//
//	static TX_SIZE calculate_tx_size(VP9_COMP *const cpi, BLOCK_SIZE bsize,
//	                                 MACROBLOCKD *const xd, unsigned int var,
//	                                 unsigned int sse, int64_t ac_thr,
//	                                 unsigned int source_variance, int is_intra) {
//	  TX_SIZE tx_size;
//	  unsigned int var_thresh = is_intra ? (unsigned int)ac_thr : 1;
//	  int limit_tx = 1;
//	  if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ &&
//	      (source_variance == 0 || var < var_thresh))
//	    limit_tx = 0;
//	  if (cpi->common.tx_mode == TX_MODE_SELECT) {
//	    if (sse > (var << 2))
//	      tx_size = VPXMIN(max_txsize_lookup[bsize],
//	                       tx_mode_to_biggest_tx_size[cpi->common.tx_mode]);
//	    else
//	      tx_size = TX_8X8;
//	    if (cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && limit_tx &&
//	        cyclic_refresh_segment_id_boosted(xd->mi[0]->segment_id))
//	      tx_size = TX_8X8;
//	    else if (tx_size > TX_16X16 && limit_tx)
//	      tx_size = TX_16X16;
//	    if (cpi->oxcf.content == VP9E_CONTENT_SCREEN && tx_size == TX_8X8 &&
//	        bsize <= BLOCK_16X16 && ((var >> 5) > (unsigned int)ac_thr))
//	      tx_size = TX_4X4;
//	  } else {
//	    tx_size = VPXMIN(max_txsize_lookup[bsize],
//	                     tx_mode_to_biggest_tx_size[cpi->common.tx_mode]);
//	  }
//	  return tx_size;
//	}
func (e *VP9Encoder) vp9InterCalculateTxSize(bsize common.BlockSize,
	txMode common.TxMode, sse, residualVar, sourceVar uint64, acThr int64,
	segmentID uint8,
) common.TxSize {
	return encoder.CalculateTxSize(encoder.CalculateTxSizeArgs{
		BSize:           bsize,
		TxMode:          txMode,
		VarY:            residualVar,
		SSEY:            sse,
		ACThreshold:     acThr,
		SourceVariance:  sourceVar,
		CyclicRefreshAQ: e.opts.AQMode == VP9AQCyclicRefresh,
		SegmentID:       segmentID,
		ScreenContent:   e.opts.ScreenContentMode == int8(VP9ScreenContentScreen),
	})
}

func (e *VP9Encoder) vp9InterTxResidualStats(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) (sse, activity uint64, ok bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	pred, predStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(pred) == 0 || srcStride <= 0 || predStride <= 0 {
		return 0, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	predRows := len(pred) / predStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > predStride || y0+blockH > predRows {
		return 0, 0, false
	}
	var prevDiffs [64]int16
	for y := range blockH {
		srcRow := src[(y0+y)*srcStride:]
		predRow := pred[(y0+y)*predStride:]
		leftDiff := 0
		for x := range blockW {
			diff := int(srcRow[x0+x]) - int(predRow[x0+x])
			sse += uint64(diff * diff)
			if x > 0 {
				activity += uint64(vp9AbsInt(diff - leftDiff))
			}
			if y > 0 {
				activity += uint64(vp9AbsInt(diff - int(prevDiffs[x])))
			}
			leftDiff = diff
			prevDiffs[x] = int16(diff)
		}
	}
	return sse, activity, true
}

func vp9AbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (e *VP9Encoder) scoreVP9InterTxCandidate(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	lumaTx common.TxSize, includeChroma bool,
) (distortion uint64, rate int, hasResidue bool, ok bool) {
	if inter == nil || inter.dq == nil {
		return 0, 0, false, false
	}
	planeLimit := 1
	if includeChroma {
		planeLimit = vp9dec.MaxMbPlane
	}
	aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	var aboveCtx [vp9dec.MaxMbPlane][16]uint8
	var leftCtx [vp9dec.MaxMbPlane][16]uint8
	var aboveLen [vp9dec.MaxMbPlane]int
	var leftLen [vp9dec.MaxMbPlane]int
	for plane := 0; plane < planeLimit; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		aboveLen[plane] = int(common.Num4x4BlocksWideLookup[planeBsize])
		leftLen[plane] = int(common.Num4x4BlocksHighLookup[planeBsize])
		if aboveLen[plane] > len(aboveCtx[plane]) || leftLen[plane] > len(leftCtx[plane]) {
			return 0, 0, false, false
		}
		if off := aboveOffsets[plane]; off >= 0 && off+aboveLen[plane] <= len(pd.AboveContext) {
			copy(aboveCtx[plane][:aboveLen[plane]], pd.AboveContext[off:off+aboveLen[plane]])
		}
		if off := leftOffsets[plane]; off >= 0 && off+leftLen[plane] <= len(pd.LeftContext) {
			copy(leftCtx[plane][:leftLen[plane]], pd.LeftContext[off:off+leftLen[plane]])
		}
	}

	for plane := 0; plane < planeLimit; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		txSize := lumaTx
		dequant := inter.dq.Y[0]
		planeType := 0
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, lumaTx, pd)
			dequant = inter.dq.Uv[0]
			planeType = 1
		}
		max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		if txSize >= common.TxSizes {
			return 0, 0, false, false
		}
		step := 1 << uint(txSize)
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		if maxEob > len(e.coefScratch) || maxEob > len(e.qCoefScratch) ||
			dequant[0] == 0 || dequant[1] == 0 ||
			(inter.lossless && txSize != common.Tx4x4) {
			return 0, 0, false, false
		}
		fpTables := e.vp9QuantFPTablesForPlaneSegment(plane, 0, dequant)
		scanOrder := &common.ScanOrders[txSize][common.DctDct]
		if inter.lossless {
			scanOrder = &common.DefaultScanOrders[txSize]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				coeffs := e.coefScratch[:maxEob]
				qcoeffs := e.qCoefScratch[:maxEob]
				clear(coeffs)
				clear(qcoeffs)
				// libvpx vp9_rdopt.c:367,405 — cost_coeffs reads
				// v = qcoeff[rc] (p->qcoeff). prepareVP9InterTxResidueWithQ
				// emits qcoeff alongside dqcoeff so the cost path
				// consumes the libvpx-equivalent magnitude verbatim
				// instead of recovering q from int16-wrapped dqcoeff.
				hasTxResidue, eob := e.prepareVP9InterTxResidueDCTFPPrechecked(inter, pd,
					plane, txSize, miRow, miCol, rr, cc, maxEob, scanOrder,
					dequant, fpTables, coeffs, qcoeffs)
				txDist, distOK := e.scoreVP9InterTxReconstruction(inter, pd, plane,
					txSize, miRow, miCol, rr, cc)
				if !distOK {
					return 0, 0, false, false
				}
				distortion += txDist

				initCtx := vp9dec.GetEntropyContextFull(txSize,
					aboveCtx[plane][cc:cc+step], leftCtx[plane][rr:rr+step])
				rate += e.vp9InterCoeffBlockRateCostQEOB(txSize, planeType,
					dequant, coeffs, qcoeffs, initCtx, eob, true)
				hasCtx := uint8(0)
				if hasTxResidue {
					hasCtx = 1
					hasResidue = true
				}
				for i := range step {
					aboveCtx[plane][cc+i] = hasCtx
					leftCtx[plane][rr+i] = hasCtx
				}
			}
		}
	}
	return distortion, rate, hasResidue, true
}

// stampVP9InterLeafTxContext walks a committed inter block's per-tx transform
// units (all planes) and writes (eob>0) into the GLOBAL plane entropy context
// pd->above_context/left_context. This is the commit-time half of
// scoreVP9InterTxCandidate (which writes only local copies) and mirrors libvpx
// vp9_set_contexts (vp9/common/vp9_blockd.h) invoked per block by
// vp9_foreach_transformed_block inside encode_b. The predictor for the committed
// mode must already be in pd->dst (predictVP9InterBlock). When the block is coded
// skip, libvpx's encode_b resets the context to zero (reset_skip_context); this
// reproduces that by stamping 0 across the footprint.
func (e *VP9Encoder) stampVP9InterLeafTxContext(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, lumaTx common.TxSize,
	skip bool,
) {
	if inter == nil || inter.dq == nil {
		return
	}
	aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		txSize := lumaTx
		dequant := inter.dq.Y[0]
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, lumaTx, pd)
			dequant = inter.dq.Uv[0]
		}
		aboveLen := int(common.Num4x4BlocksWideLookup[planeBsize])
		leftLen := int(common.Num4x4BlocksHighLookup[planeBsize])
		ao := aboveOffsets[plane]
		lo := leftOffsets[plane]
		max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		if txSize >= common.TxSizes {
			continue
		}
		step := 1 << uint(txSize)
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		if maxEob > len(e.coefScratch) || maxEob > len(e.qCoefScratch) ||
			dequant[0] == 0 || dequant[1] == 0 ||
			(inter.lossless && txSize != common.Tx4x4) {
			continue
		}
		fpTables := e.vp9QuantFPTablesForPlaneSegment(plane, 0, dequant)
		scanOrder := &common.ScanOrders[txSize][common.DctDct]
		if inter.lossless {
			scanOrder = &common.DefaultScanOrders[txSize]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				hasCtx := uint8(0)
				if !skip {
					coeffs := e.coefScratch[:maxEob]
					qcoeffs := e.qCoefScratch[:maxEob]
					clear(coeffs)
					clear(qcoeffs)
					if ok, _ := e.prepareVP9InterTxResidueDCTFPPrechecked(inter, pd,
						plane, txSize, miRow, miCol, rr, cc, maxEob, scanOrder,
						dequant, fpTables, coeffs, qcoeffs); ok {
						hasCtx = 1
					}
				}
				for i := 0; i < step && cc+i < aboveLen; i++ {
					if ao >= 0 && ao+cc+i < len(pd.AboveContext) {
						pd.AboveContext[ao+cc+i] = hasCtx
					}
				}
				for i := 0; i < step && rr+i < leftLen; i++ {
					if lo >= 0 && lo+rr+i < len(pd.LeftContext) {
						pd.LeftContext[lo+rr+i] = hasCtx
					}
				}
			}
		}
	}
}

func (e *VP9Encoder) scoreVP9InterTxReconstruction(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol, blockRow4x4, blockCol4x4 int,
) (uint64, bool) {
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		return 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	bs := 4 << uint(txSize)
	var distortion uint64
	for y := 0; y < bs && y0+y < srcH; y++ {
		srcRow := src[(y0+y)*srcStride:]
		dstRow := dst[y*stride:]
		for x := 0; x < bs && x0+x < srcW; x++ {
			diff := int(srcRow[x0+x]) - int(dstRow[x])
			distortion += uint64(diff * diff)
		}
	}
	return distortion, true
}

// vp9InterCoeffBlockRateCostQ mirrors libvpx's cost_coeffs is_inter=1
// path (vp9_rdopt.c:358-459). When qcoeffs is non-nil the per-coefficient
// magnitude is read directly from qcoeffs[raster] — see
// vp9KeyframeCoeffBlockRateCostPlaneQ for the rationale.
func (e *VP9Encoder) vp9InterCoeffBlockRateCostQ(txSize common.TxSize,
	planeType int, dequant [2]int16, coeffs, qcoeffs []int16, initCtx int,
) int {
	return e.vp9InterCoeffBlockRateCostQEOB(txSize, planeType, dequant,
		coeffs, qcoeffs, initCtx, 0, false)
}

func (e *VP9Encoder) vp9InterCoeffBlockRateCostQEOB(txSize common.TxSize,
	planeType int, dequant [2]int16, coeffs, qcoeffs []int16, initCtx int,
	eob int, eobKnown bool,
) int {
	return e.vp9InterCoeffBlockRateCostQFcWithCosts(&e.fc,
		e.vp9CoeffTokenCostTable(txSize, planeType, 1), txSize, planeType,
		dequant, coeffs, qcoeffs, initCtx, eob, eobKnown)
}

// vp9InterCoeffBlockRateCostQFc is vp9InterCoeffBlockRateCostQ with an explicit
// frame context, so the zcoeff_blk recompute can cost coefficients with the
// search-time (pre-compressed-header-update) coef probs (inter.selectFc) rather
// than the live e.fc that the header writer mutates between the count and write
// passes.
func (e *VP9Encoder) vp9InterCoeffBlockRateCostQFc(fc *vp9dec.FrameContext,
	txSize common.TxSize, planeType int, dequant [2]int16,
	coeffs, qcoeffs []int16, initCtx int,
) int {
	return e.vp9InterCoeffBlockRateCostQFcWithCosts(fc, nil, txSize, planeType,
		dequant, coeffs, qcoeffs, initCtx, 0, false)
}

func (e *VP9Encoder) vp9InterCoeffBlockRateCostQFcWithCosts(fc *vp9dec.FrameContext,
	costs *encoder.CoeffTreeTokenCostTable, txSize common.TxSize, planeType int,
	dequant [2]int16, coeffs, qcoeffs []int16, initCtx int,
	eob int, eobKnown bool,
) int {
	if fc == nil || txSize >= common.TxSizes || planeType < 0 || planeType > 1 {
		return 0
	}
	if e.sf.UseFastCoefCosting != 0 && qcoeffs != nil && costs != nil && eobKnown {
		return encoder.CoeffBlockRateCostFastKnownQCoeffTable(txSize, costs,
			common.DefaultScanOrders[txSize].Scan, qcoeffs, initCtx, eob)
	}
	return encoder.CoeffBlockRateCost(encoder.CoeffBlockRateCostInput{
		TxSize:     txSize,
		CoefModel:  &fc.CoefProbs[txSize][planeType][1],
		ScanOrder:  common.DefaultScanOrders[txSize],
		Dequant:    dequant,
		Coeffs:     coeffs,
		QCoeffs:    qcoeffs,
		InitCtx:    initCtx,
		Fast:       e.sf.UseFastCoefCosting != 0,
		TokenCache: &e.modeScratch,
		CostTable:  costs,
		EOB:        eob,
		EOBKnown:   eobKnown,
	})
}
