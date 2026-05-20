package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9InterIntraDecision struct {
	mode   common.PredictionMode
	uvMode common.PredictionMode
	txSize common.TxSize
	rate   int
	score  uint64
}

func (e *VP9Encoder) pickVP9InterIntraMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize, interScore uint64,
) (vp9InterIntraDecision, bool) {
	if inter == nil {
		return vp9InterIntraDecision{}, false
	}
	if interScore < 1<<60 &&
		!e.vp9InterIntraResidualLooksSceneCut(inter, miRow, miCol, bsize) {
		return vp9InterIntraDecision{}, false
	}
	decision, ok := e.pickVP9InterIntraModeCore(inter, tile, miRows, miCols,
		miRow, miCol, bsize, txSize,
		func(above, left *vp9dec.NeighborMi) int {
			return encoder.IntraInterRateCost(&inter.selectFc, above, left, 0)
		})
	if !ok {
		return vp9InterIntraDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	qindex := e.vp9EncoderModeDecisionQIndex()
	interAdjusted := interScore + e.vp9ModeDecisionRateScore(
		encoder.IntraInterRateCost(&inter.selectFc, above, left, 1), qindex)
	if decision.score >= interAdjusted {
		return vp9InterIntraDecision{}, false
	}
	return decision, true
}

func (e *VP9Encoder) vp9InterIntraResidualLooksSceneCut(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	if bsize >= common.BlockSizes {
		return false
	}
	sse, activity, ok := e.vp9InterTxResidualStats(inter, miRow, miCol, bsize)
	if !ok {
		return false
	}
	pixels := uint64(common.Num4x4BlocksWideLookup[bsize]) *
		uint64(common.Num4x4BlocksHighLookup[bsize]) * 16
	return sse >= pixels*64*64 && activity <= pixels*64
}

func (e *VP9Encoder) pickVP9ForcedInterIntraMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize,
) (vp9InterIntraDecision, bool) {
	return e.pickVP9InterIntraModeCore(inter, tile, miRows, miCols,
		miRow, miCol, bsize, txSize,
		func(*vp9dec.NeighborMi, *vp9dec.NeighborMi) int { return 0 })
}

var vp9NoReferenceIntraModes = [...]common.PredictionMode{
	common.DcPred,
	common.VPred,
	common.HPred,
	common.TmPred,
}

func vp9NoReferenceIntraModeCount(bsize common.BlockSize, screenContentMode int8) int {
	// Mirrors the realtime VP9 intra_y_mode_bsize_mask used when inter refs
	// are disabled: non-screen content only keeps DC for blocks above 16x16.
	if screenContentMode != 1 && bsize > common.Block16x16 {
		return 1
	}
	return 3
}

func (e *VP9Encoder) vp9InterIntraKeyframeState(inter *vp9InterEncodeState) vp9KeyframeEncodeState {
	hdr := &e.vp9InterIntraHdr
	*hdr = vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
		Quant:  e.vp9HeaderScratch.Quant,
		Seg:    e.vp9HeaderScratch.Seg,
	}
	if e.vp9HeaderScratch.Width != 0 {
		hdr.Width = e.vp9HeaderScratch.Width
	}
	if e.vp9HeaderScratch.Height != 0 {
		hdr.Height = e.vp9HeaderScratch.Height
	}
	if inter != nil {
		hdr.Quant.BaseQindex = int16(inter.baseQindex)
		hdr.Quant.Lossless = inter.lossless
		return vp9KeyframeEncodeState{
			img:      inter.img,
			hdr:      hdr,
			dq:       inter.dq,
			lossless: inter.lossless,
			counts:   inter.counts,
		}
	}
	hdr.Quant.BaseQindex = int16(e.vp9EncoderModeDecisionQIndex())
	return vp9KeyframeEncodeState{hdr: hdr}
}

func (e *VP9Encoder) pickVP9NoReferenceIntraMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, maxTx common.TxSize, segmentID uint8,
) (vp9InterIntraDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterIntraDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	keyLike := e.vp9InterIntraKeyframeState(inter)

	sg := common.SizeGroupLookup[bsize]
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], inter.selectFc.YModeProb[sg][:],
		common.IntraModeTree[:])
	qindex := e.vp9EncoderModeDecisionQIndex()
	rateBase := encoder.IntraInterRateCost(&inter.selectFc, above, left, 0)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	modeCount := vp9NoReferenceIntraModeCount(bsize, e.opts.ScreenContentMode)

	bestSet := false
	var best vp9InterIntraDecision
	for i := range modeCount {
		mode := vp9NoReferenceIntraModes[i]
		txSize, txOK := e.pickVP9NoReferenceIntraTxSize(&keyLike, tile,
			miRows, miCols, miRow, miCol, bsize, maxTx, mode)
		if !txOK {
			continue
		}
		mi := vp9dec.NeighborMi{
			SbType:    bsize,
			SegmentID: segmentID,
			TxSize:    txSize,
		}
		distortion, coeffRate, skippable, scoreOK := e.scoreVP9KeyframeModeTransformRD(
			&keyLike, mode, tile, miRows, miCols, miRow, miCol, bsize, &mi)
		if !scoreOK {
			continue
		}
		rate := rateBase + yModeCosts[mode]
		if skippable {
			rate += encoder.VP9CostBit(skipProb, 1)
		} else {
			rate += coeffRate + encoder.VP9CostBit(skipProb, 0)
		}
		cand := vp9InterIntraDecision{
			mode:   mode,
			uvMode: mode,
			txSize: txSize,
			rate:   rate,
			score:  e.vp9ModeDecisionScore(distortion, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}
	return best, bestSet
}

func (e *VP9Encoder) pickVP9NoReferenceIntraTxSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, maxTx common.TxSize, mode common.PredictionMode,
) (common.TxSize, bool) {
	maxTx = min(clampVP9TxSizeForBlock(maxTx, bsize), common.Tx16x16)
	if maxTx <= common.Tx4x4 {
		return maxTx, true
	}
	predTx := common.MaxTxsizeLookup[bsize]
	sse, variance, ok := e.vp9NoReferenceIntraResidualStats(key, mode, predTx,
		tile, miRows, miCols, miRow, miCol, bsize)
	if !ok {
		return maxTx, false
	}
	if sse > variance<<2 {
		return maxTx, true
	}
	return common.Tx8x8, true
}

func (e *VP9Encoder) vp9NoReferenceIntraResidualStats(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (sse uint64, variance uint64, ok bool) {
	return e.vp9NoReferenceIntraResidualStatsWithRestore(key, mode, txSize,
		tile, miRows, miCols, miRow, miCol, bsize, true)
}

func (e *VP9Encoder) vp9NoReferenceIntraResidualStatsNoRestore(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (sse uint64, variance uint64, ok bool) {
	return e.vp9NoReferenceIntraResidualStatsWithRestore(key, mode, txSize,
		tile, miRows, miCols, miRow, miCol, bsize, false)
}

// vp9NoReferenceIntraResidualStatsScratchNoRestore mirrors the realtime
// nonrd path where vp9_pick_inter_mode scores intra fallback against the
// live prediction buffer (`pd->dst`) instead of the final reconstruction
// plane. With ML_BASED_PARTITION that buffer is x->est_pred, populated by
// get_estimated_pred before nonrd_pick_partition enters the leaf picker.
func (e *VP9Encoder) vp9NoReferenceIntraResidualStatsScratchNoRestore(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	scratch []byte, scratchStride, originMiRow, originMiCol int,
) (sse uint64, variance uint64, ok bool) {
	return e.vp9NoReferenceIntraResidualStatsScratchRefNoRestore(key, mode,
		txSize, tile, miRows, miCols, miRow, miCol, bsize,
		scratch, scratchStride, originMiRow, originMiCol,
		scratch, scratchStride, originMiRow, originMiCol)
}

func (e *VP9Encoder) vp9NoReferenceIntraResidualStatsScratchRefNoRestore(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	scratch []byte, scratchStride, originMiRow, originMiCol int,
	ref []byte, refStride, refOriginMiRow, refOriginMiCol int,
) (sse uint64, variance uint64, ok bool) {
	if key == nil || key.hdr == nil || key.img == nil || int(mode) >= common.IntraModes {
		return 0, 0, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || srcStride <= 0 || len(scratch) == 0 || scratchStride <= 0 ||
		len(ref) == 0 || refStride <= 0 {
		return 0, 0, false
	}
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	originX := originMiCol * common.MiSize
	originY := originMiRow * common.MiSize
	refOriginX := refOriginMiCol * common.MiSize
	refOriginY := refOriginMiRow * common.MiSize
	var sum int64
	var count uint64
	predOK := true
residualLoop:
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			dst, dstStride, x0, y0, ok := e.predictVP9KeyframeTxGeneric(
				key.hdr, pd, 0, mode, txSize, tile, miRows, miCols,
				miRow, miCol, bsize, rr, cc,
				scratch, scratchStride, ref, refStride,
				originX, originY, refOriginX, refOriginY)
			if !ok {
				predOK = false
				break residualLoop
			}
			copyW := bs
			copyH := bs
			if x0 >= srcW || y0 >= srcH {
				continue
			}
			if x0+copyW > srcW {
				copyW = srcW - x0
			}
			if y0+copyH > srcH {
				copyH = srcH - y0
			}
			for y := 0; y < copyH; y++ {
				srcRow := src[(y0+y)*srcStride+x0:]
				dstRow := dst[y*dstStride:]
				for x := 0; x < copyW; x++ {
					diff := int(srcRow[x]) - int(dstRow[x])
					sse += uint64(diff * diff)
					sum += int64(diff)
					count++
				}
			}
		}
	}
	if !predOK {
		return 0, 0, false
	}
	if count == 0 {
		return 0, 0, false
	}
	meanSquare := uint64((sum * sum) / int64(count))
	if sse >= meanSquare {
		return sse, sse - meanSquare, true
	}
	return sse, meanSquare - sse, true
}

func (e *VP9Encoder) vp9NoReferenceIntraResidualStatsWithRestore(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, restore bool,
) (sse uint64, variance uint64, ok bool) {
	if key == nil || key.hdr == nil || key.img == nil || int(mode) >= common.IntraModes {
		return 0, 0, false
	}
	pd := &e.planes[0]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, 0, false
	}
	planeData, stride := e.vp9EncoderReconPlane(0)
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(planeData) == 0 || stride <= 0 || len(src) == 0 || srcStride <= 0 {
		return 0, 0, false
	}
	rows := len(planeData) / stride
	baseX := miCol * common.MiSize
	baseY := miRow * common.MiSize
	if baseX >= stride || baseY >= rows {
		return 0, 0, false
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
		return 0, 0, false
	}
	var saved []byte
	if restore {
		if restoreW*restoreH > len(e.blockScratch) {
			return 0, 0, false
		}
		saved = e.blockScratch[:restoreW*restoreH]
		for y := 0; y < restoreH; y++ {
			copy(saved[y*restoreW:(y+1)*restoreW],
				planeData[(baseY+y)*stride+baseX:(baseY+y)*stride+baseX+restoreW])
		}
	}

	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	var sum int64
	var count uint64
	predOK := true
residualLoop:
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			dst, dstStride, x0, y0, ok := e.predictVP9KeyframeTx(
				key.hdr, pd, 0, mode, txSize, tile, miRows, miCols,
				miRow, miCol, bsize, rr, cc)
			if !ok {
				predOK = false
				break residualLoop
			}
			copyW := bs
			copyH := bs
			if x0 >= srcW || y0 >= srcH {
				continue
			}
			if x0+copyW > srcW {
				copyW = srcW - x0
			}
			if y0+copyH > srcH {
				copyH = srcH - y0
			}
			for y := 0; y < copyH; y++ {
				srcRow := src[(y0+y)*srcStride+x0:]
				dstRow := dst[y*dstStride:]
				for x := 0; x < copyW; x++ {
					diff := int(srcRow[x]) - int(dstRow[x])
					sse += uint64(diff * diff)
					sum += int64(diff)
					count++
				}
			}
		}
	}
	if restore {
		vp9RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
	}
	if !predOK {
		return 0, 0, false
	}
	if count == 0 {
		return 0, 0, false
	}
	meanSquare := uint64((sum * sum) / int64(count))
	if sse >= meanSquare {
		return sse, sse - meanSquare, true
	}
	return sse, meanSquare - sse, true
}

func (e *VP9Encoder) pickVP9InterIntraModeCore(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txSize common.TxSize,
	intraInterRate func(above, left *vp9dec.NeighborMi) int,
) (vp9InterIntraDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterIntraDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	qindex := e.vp9EncoderModeDecisionQIndex()
	rateBase := 0
	if intraInterRate != nil {
		rateBase = intraInterRate(above, left)
	}

	keyLike := e.vp9InterIntraKeyframeState(inter)
	sg := common.SizeGroupLookup[bsize]
	var yModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(yModeCosts[:], inter.selectFc.YModeProb[sg][:],
		common.IntraModeTree[:])

	bestSet := false
	var best vp9InterIntraDecision
	tryMode := func(mode common.PredictionMode) {
		yDist, ok := e.scoreVP9KeyframePlanePrediction(&keyLike, &e.planes[0],
			mode, 0, txSize, tile, miRows, miCols, miRow, miCol, bsize)
		if !ok {
			return
		}
		uvMode, uvDist, uvRate, ok := e.pickVP9InterIntraUvMode(&keyLike,
			&inter.selectFc, mode, tile, miRows, miCols, miRow, miCol, bsize, txSize)
		if !ok {
			return
		}
		rate := rateBase + yModeCosts[mode] + uvRate
		cand := vp9InterIntraDecision{
			mode:   mode,
			uvMode: uvMode,
			txSize: txSize,
			rate:   rate,
			score:  e.vp9ModeDecisionScore(yDist+uvDist, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	tryMode(common.DcPred)
	for mode := common.DcPred + 1; mode <= common.TmPred; mode++ {
		tryMode(mode)
	}
	return best, bestSet
}

func (e *VP9Encoder) pickVP9InterIntraUvMode(key *vp9KeyframeEncodeState,
	fc *vp9dec.FrameContext, yMode common.PredictionMode, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, txSize common.TxSize,
) (common.PredictionMode, uint64, int, bool) {
	if key == nil || fc == nil || yMode < common.DcPred || int(yMode) >= common.IntraModes {
		return common.DcPred, 0, 0, false
	}
	var uvModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(uvModeCosts[:], fc.UvModeProb[yMode][:],
		common.IntraModeTree[:])
	bestSet := false
	bestMode := common.DcPred
	var bestDist uint64
	bestRate := 0
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		var dist uint64
		ok := true
		for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
			pd := &e.planes[plane]
			planeTx := txSize
			if plane > 0 {
				planeTx = vp9dec.GetUvTxSize(bsize, txSize, pd)
			}
			score, scoreOK := e.scoreVP9KeyframePlanePrediction(key, pd, mode,
				plane, planeTx, tile, miRows, miCols, miRow, miCol, bsize)
			if !scoreOK {
				ok = false
				break
			}
			dist += score
		}
		if !ok {
			continue
		}
		rate := uvModeCosts[mode]
		if !bestSet || dist < bestDist || (dist == bestDist && rate < bestRate) {
			bestSet = true
			bestMode = mode
			bestDist = dist
			bestRate = rate
		}
	}
	return bestMode, bestDist, bestRate, bestSet
}

func (e *VP9Encoder) prepareVP9InterIntraBlockResidue(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) bool {
	if inter == nil {
		return false
	}
	keyLike := e.vp9InterIntraKeyframeState(inter)
	return e.prepareVP9KeyframeBlockResidue(&keyLike, tile, miRows, miCols,
		miRow, miCol, bsize, mi, uvMode)
}

type vp9InterModeDecision struct {
	intra          bool
	refFrame       int8
	secondRefFrame int8
	refSlot        int
	secondRefSlot  int
	isCompound     bool
	mode           common.PredictionMode
	mv             [2]vp9dec.MV
	bmi            [4]vp9dec.Bmi
	interpFilter   vp9dec.InterpFilter
	txSize         common.TxSize
	uvMode         common.PredictionMode
	rate           int
	distortion     uint64
	score          uint64
}

type vp9KeyframeModeDecision struct {
	mode   common.PredictionMode
	bmi    [4]vp9dec.Bmi
	txSize common.TxSize
	uvMode common.PredictionMode
}

// vp9LeafKeyframeDecisionEntry stores the intra-mode choices selected during
// the keyframe count pre-pass so the bitstream write pass can emit with the
// same Y mode, sub-8x8 BMI quartet, UV mode, and tx size after probability
// updates. Coefficients are intentionally not cached; each pass rebuilds
// residue against its own entropy contexts.
type vp9LeafKeyframeDecisionEntry struct {
	version  uint32
	bsize    common.BlockSize
	decision vp9KeyframeModeDecision
	valid    bool
}

type vp9KeyframePartitionDecisionEntry struct {
	version uint32
	root    common.BlockSize
	target  common.BlockSize
	valid   bool
}

// vp9LeafInterDecisionEntry stores one cached leaf-write inter-mode decision
// keyed by (version, bsize). The cache mirrors libvpx's mi_grid_visible
// per-block storage; entries are populated by the count pre-pass at
// pickVP9InterReferenceMode and consumed by the bitstream write pass to skip
// the redundant picker invocation. The version stamp guards against stale
// entries spanning multiple frames; the bsize discriminator guards against
// callers that re-enter the leaf-write site at a different block size than
// the prior visit.
//
// libvpx: vp9/encoder/vp9_encodeframe.c encode_b stores the picker decision
// into mi[0]->mbmi; vp9/encoder/vp9_bitstream.c::write_modes_b reads it back
// for emission without recomputation.
type vp9LeafInterDecisionEntry struct {
	version  uint32
	bsize    common.BlockSize
	decision vp9InterModeDecision
	valid    bool
}

func (e *VP9Encoder) pickVP9InterReferenceMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if inter == nil {
		return vp9InterModeDecision{}, false
	}
	// SPEED_FEATURES.use_nonrd_pick_mode (cpu_used >= 5 in libvpx realtime)
	// routes the inter-mode picker through the verbatim nonrd port at
	// vp9_pick_inter_mode_nonrd.go. The nonrd entry walks the libvpx
	// ref_mode_set[] schedule, prunes the per-mode interp-filter loop, and
	// applies aggressive early termination — collapsing the per-block work
	// from ~36 (3 refs × 4 modes × 3 filters) candidate evaluations to ~12.
	//
	// libvpx merges single-ref + compound candidates into a single loop
	// (vp9_pickmode.c:2050 — idx < num_inter_modes + comp_modes). govpx
	// keeps them separate: the nonrd entry handles single-ref; the
	// existing compound branch below handles compound. The schedule order
	// matches libvpx because nonrd visits all single-ref candidates first
	// (idx 0..num_inter_modes-1) and compound is appended at the tail.
	//
	// libvpx: vp9_pickmode.c:1696 vp9_pick_inter_mode.
	// libvpx: vp9_speed_features.h:447 sf->use_nonrd_pick_mode.
	useNonrd := e.vp9InterUsesNonrdPickmode()
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	// libvpx restricts usable_ref_frame at speed >= 8 to LAST_FRAME for
	// the steady-state inter-block hot path: frames_since_golden > 120
	// or low last_sb_high_content triggers
	// `usable_ref_frame = LAST_FRAME` and skips GOLDEN/ALTREF
	// reference-mode picking entirely. Additionally
	// sf.short_circuit_low_temp_var (3 at speed 8 CBR non-screen) short-
	// circuits non-LAST refs on low-temporal-variance blocks via
	// force_skip_low_temp_var. govpx caches libvpx's per-SB variance_low
	// map from choose_partitioning and applies the LAST-only fan from that
	// exact signal when it exists; before the cache is available it keeps the
	// historical LAST-only fallback for the threaded warm path. Frames that
	// explicitly mask out LAST (e.g. EncodeNoReferenceLast for altref-only
	// inter) must keep the full ref set so a fallback ref can still be picked.
	// libvpx: vp9/encoder/vp9_pickmode.c:1962-1985 (usable_ref_frame),
	// vp9_speed_features.c:774 (ShortCircuitLowTempVar = 3 at speed 8
	// CBR non-screen).
	refFramesAll := [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame}
	refFrames := refFramesAll[:]
	forceSkipLowTempVar, forceSkipLowTempKnown :=
		e.vp9VarPartForceSkipLowTempVarOK(miCols, miRow, miCol, bsize)
	if !forceSkipLowTempKnown && e.sf.ShortCircuitLowTempVar >= 1 {
		forceSkipLowTempVar = true
	}
	if vp9NonrdForceLastReference(e.sf.ShortCircuitLowTempVar,
		e.sf.UseNonrdPickMode != 0, forceSkipLowTempVar) {
		if _, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame); ok {
			refFrames = refFramesAll[:1]
		}
	}
	// SPEED_FEATURES.use_altref_onepass = 0 (cpu_used >= 5 in realtime) drops
	// ALTREF from the reference-frame fan. vp9InterReferenceFramesEnabled
	// returns {LAST, GOLDEN, ALTREF} or {LAST, GOLDEN} depending on the field.
	//
	// libvpx: vp9_speed_features.c:586 sf->use_altref_onepass = 0.
	refFrameSet := refFrames
	if len(refFrameSet) == len(refFramesAll) {
		// Defer to the speed-feature helper when we haven't already
		// pruned to LAST-only above (it honors use_altref_onepass).
		refFrameSet = e.vp9InterReferenceFramesEnabled()
		hasEnabledRef := false
		for _, refFrame := range refFrameSet {
			if _, ok := e.vp9InterReferenceSlot(inter, refFrame); ok {
				hasEnabledRef = true
				break
			}
		}
		if !hasEnabledRef {
			refFrameSet = refFramesAll[:]
		}
	}
	sourceAltRefOverlay := e.vp9OnePassVBRSourceAltRefOverlay(inter)
	if sourceAltRefOverlay {
		if _, ok := e.vp9InterReferenceSlot(inter, vp9dec.AltrefFrame); ok {
			refFrameSet = refFramesAll[2:3]
		}
	}
	// libvpx's one-pass ARF group path promotes usable_ref_frame to ALTREF
	// for VBR+lag ARNR frames (vp9_pickmode.c:1918-1939).
	filteredAltRefGroup := e.opts.AutoAltRef && e.sf.UseAltrefOnepass != 0 &&
		e.opts.ARNRMaxFrames > 1 && e.rc.altRefGFGroup
	if filteredAltRefGroup {
		if _, ok := e.vp9InterReferenceSlot(inter, vp9dec.AltrefFrame); ok {
			refFrameSet = refFramesAll[2:3]
		}
	}
	bestSet := false
	var best vp9InterModeDecision
	// useNonrd: route the speed-feature-selected realtime path through
	// the libvpx-shaped ref_mode_set[] loop in vp9_pick_inter_mode_nonrd.go.
	// When the low-temporal-variance shortcut above has reduced the usable
	// set to LAST-only, temporarily narrow inter.refMask so the nonrd picker
	// sees the same usable_ref_frame scope libvpx would.
	//
	// libvpx: vp9_pickmode.c:1696 vp9_pick_inter_mode.
	if useNonrd && len(refFrameSet) > 0 {
		savedRefMask := inter.refMask
		if len(refFrameSet) != len(refFramesAll) {
			var narrowed uint8
			for _, refFrame := range refFrameSet {
				narrowed |= 1 << uint(refFrame)
			}
			inter.refMask &= narrowed
		}
		decision, ok := e.pickVP9InterReferenceModeNonRD(inter, tile,
			miRows, miCols, miRow, miCol, bsize)
		inter.refMask = savedRefMask
		if ok {
			best = decision
			bestSet = true
		}
	} else {
		for _, refFrame := range refFrameSet {
			refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
			if !ok {
				continue
			}
			inter.ref = &e.refFrames[refSlot]
			refRate := encoder.SingleRefModeRateCost(&inter.selectFc, above, left,
				inter.referenceMode, inter.compoundRefs, refFrame)
			decision, ok := e.pickVP9InterMode(inter, tile, miRows, miCols,
				miRow, miCol, bsize, refFrame, refRate)
			if !ok {
				continue
			}
			decision.refFrame = refFrame
			decision.secondRefFrame = vp9dec.NoRefFrame
			decision.refSlot = refSlot
			if !bestSet || decision.score < best.score ||
				(decision.score == best.score && decision.rate < best.rate) {
				best = decision
				bestSet = true
			}
		}
	}
	// SPEED_FEATURES.use_compound_nonrd_pickmode gates the compound branch
	// when UseNonrdPickMode is on (cpu_used >= 7 in libvpx realtime). The
	// nonrd_pickmode entry skips compound entirely when the feature is 0.
	//
	// libvpx: vp9/encoder/vp9_speed_features.c:469 / 656 / 665,
	// vp9/encoder/vp9_pickmode.c:1989.
	if !e.vp9InterCompoundEnabled() {
		return best, bestSet
	}
	if sourceAltRefOverlay {
		return best, bestSet
	}
	if inter.compoundAllowed && inter.referenceMode != vp9dec.SingleReference {
		for _, varRef := range inter.compoundRefs.CompVarRef {
			refFrame, refSlot, secondRefFrame, secondRefSlot, ok :=
				e.vp9CompoundReferencePair(inter, varRef)
			if !ok {
				continue
			}
			refRate, ok := encoder.CompoundRefRateCost(&inter.selectFc, above, left,
				inter.referenceMode, inter.compoundRefs, inter.refSignBias,
				[2]int8{refFrame, secondRefFrame})
			if !ok {
				continue
			}
			decision, ok := e.pickVP9CompoundInterMode(inter, tile, miRows, miCols,
				miRow, miCol, bsize, [2]int8{refFrame, secondRefFrame},
				[2]int{refSlot, secondRefSlot}, refRate)
			if !ok {
				continue
			}
			if !bestSet || decision.score < best.score ||
				(decision.score == best.score && decision.rate < best.rate) {
				best = decision
				bestSet = true
			}
		}
	}
	return best, bestSet
}

func (e *VP9Encoder) firstVP9InterReference(inter *vp9InterEncodeState) (int8, int, bool) {
	for _, refFrame := range [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame} {
		if refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame); ok {
			return refFrame, refSlot, true
		}
	}
	return 0, 0, false
}

func (e *VP9Encoder) vp9InterReferenceSlot(inter *vp9InterEncodeState, refFrame int8) (int, bool) {
	if inter == nil || inter.refMask&(1<<uint(refFrame)) == 0 {
		return 0, false
	}
	slot, ok := vp9EncoderReferenceSlot(refFrame)
	if !ok {
		return 0, false
	}
	if !e.refFrames[slot].valid {
		return 0, false
	}
	return slot, true
}

func (e *VP9Encoder) vp9CompoundReferencePair(inter *vp9InterEncodeState,
	varRef int8,
) (int8, int, int8, int, bool) {
	if inter == nil {
		return 0, 0, 0, 0, false
	}
	fixedRef := inter.compoundRefs.CompFixedRef
	fixedSlot, ok := e.vp9InterReferenceSlot(inter, fixedRef)
	if !ok {
		return 0, 0, 0, 0, false
	}
	varSlot, ok := e.vp9InterReferenceSlot(inter, varRef)
	if !ok {
		return 0, 0, 0, 0, false
	}
	idx := int(inter.refSignBias[fixedRef])
	if idx == 0 {
		return fixedRef, fixedSlot, varRef, varSlot, true
	}
	return varRef, varSlot, fixedRef, fixedSlot, true
}

func (e *VP9Encoder) pickVP9CompoundInterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame [2]int8, refSlot [2]int, refRate int,
) (vp9InterModeDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterModeDecision{}, false
	}
	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248 — per-SB cb_rdmult
	// priming (see pickVP9InterMode for the long-form comment).  The
	// compound picker shares the same TPL delta lookup as the single-ref
	// picker because libvpx routes both through rd_pick_sb_modes.
	prevCbRdmult := e.cbRdmult
	baseRdmult := e.rc.rdmult
	if baseRdmult <= 0 {
		baseRdmult = encoder.ComputeRDMultBasedOnQindex(qindex, encoder.RDFrameInter)
	}
	if bsize < common.BlockSizes && e.tpl.enabled {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		baseRdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, baseRdmult)
	}
	if baseRdmult <= 0 {
		baseRdmult = 1
	}
	e.cbRdmult = baseRdmult
	// SPEED_FEATURES.inter_mode_mask gates inter modes for compound refs too.
	// libvpx: vp9_pickmode.c:2150 — applied to every mode candidate.
	interModeMask := e.vp9InterModeMaskFor(bsize)
	modeAllowed := func(mode common.PredictionMode) bool {
		return interModeMask&(1<<uint(mode)) != 0
	}
	bestSet := false
	var best vp9InterModeDecision
	consider := func(mode common.PredictionMode, mv, refMv [2]vp9dec.MV,
		filter vp9dec.InterpFilter, distortion uint64,
	) {
		rate := refRate +
			encoder.InterModeRateCostN(&inter.selectFc, interModeCtx, mode,
				mv, refMv, 2, inter.allowHP) +
			vp9InterInterpFilterRateCost(inter, &inter.selectFc, switchableCtx, filter)
		cand := vp9InterModeDecision{
			refFrame:       refFrame[0],
			secondRefFrame: refFrame[1],
			refSlot:        refSlot[0],
			secondRefSlot:  refSlot[1],
			isCompound:     true,
			mode:           mode,
			mv:             mv,
			interpFilter:   filter,
			rate:           rate,
			distortion:     distortion,
			score:          e.vp9InterModeScore(distortion, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	if modeAllowed(common.ZeroMv) {
		e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
			refFrame, refSlot, common.ZeroMv, [2]vp9dec.MV{},
			[2]vp9dec.MV{}, consider)
	}

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		if !modeAllowed(mode) {
			continue
		}
		var mv [2]vp9dec.MV
		ok := true
		for ref := range 2 {
			mv[ref], ok = e.vp9EncoderInterModeCandidateMv(tile,
				miRows, miCols, miRow, miCol, bsize, mode,
				refFrame[ref], inter.allowHP, inter.refSignBias)
			if !ok {
				break
			}
		}
		if ok {
			e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
				refFrame, refSlot, mode, mv, [2]vp9dec.MV{}, consider)
		}
	}

	if modeAllowed(common.NewMv) {
		var newMv, newRefMv [2]vp9dec.MV
		newOK := true
		newHasMotion := false
		for ref := range 2 {
			inter.ref = &e.refFrames[refSlot[ref]]
			newMv[ref], _, newOK = e.pickVP9InterMvAllowZero(inter, miRows, miCols,
				miRow, miCol, bsize, refFrame[ref], vp9InterMvSearchOptions{})
			if !newOK {
				break
			}
			if newMv[ref] != (vp9dec.MV{}) {
				newHasMotion = true
			}
			newRefMv[ref], _ = e.vp9EncoderInterModeCandidateMv(tile,
				miRows, miCols, miRow, miCol, bsize, common.NewMv,
				refFrame[ref], inter.allowHP, inter.refSignBias)
		}
		if newOK && newHasMotion {
			e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
				refFrame, refSlot, common.NewMv, newMv, newRefMv, consider)
		}
	}
	e.cbRdmult = prevCbRdmult
	return best, bestSet
}

func (e *VP9Encoder) evalVP9CompoundMode(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame [2]int8, refSlot [2]int, mode common.PredictionMode,
	mv, refMv [2]vp9dec.MV,
	consider func(common.PredictionMode, [2]vp9dec.MV, [2]vp9dec.MV,
		vp9dec.InterpFilter, uint64),
) {
	filters := vp9InterInterpFilterCandidates(inter)
	if !vp9AnyMvHasSubpel(mv) {
		distortion, ok := e.vp9CompoundPredictionDistortion(inter, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, refSlot, mv,
			filters[0])
		if ok {
			for _, filter := range filters {
				consider(mode, mv, refMv, filter, distortion)
			}
		}
		return
	}
	for _, filter := range filters {
		distortion, ok := e.vp9CompoundPredictionDistortion(inter, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, refSlot, mv, filter)
		if ok {
			consider(mode, mv, refMv, filter, distortion)
		}
	}
}

func (e *VP9Encoder) pickVP9InterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8, refRate int,
) (vp9InterModeDecision, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid ||
		refFrame <= vp9dec.IntraFrame {
		return vp9InterModeDecision{}, false
	}
	if bsize < common.Block8x8 {
		return e.pickVP9Sub8InterMode(inter, tile, miRows, miCols,
			miRow, miCol, bsize, refFrame, refRate)
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(inter.ref, 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return vp9InterModeDecision{}, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	scoreW, scoreH, ok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, refW, refH)
	if !ok {
		return vp9InterModeDecision{}, false
	}

	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// libvpx: vp9/encoder/vp9_encodeframe.c:4245-4248 — every SB's
	// rd_pick_sb_modes call seeds x->cb_rdmult from get_rdmult_delta so
	// the per-mode RDCOST consumes a TPL-biased multiplier rather than
	// the bare per-frame rd.RDMULT.  Inline save/restore (no defer) to
	// preserve the alloc-parity gate; the TPL lookup is short-circuited
	// when no slab is populated so this stays cheap.
	prevCbRdmult := e.cbRdmult
	baseRdmult := e.rc.rdmult
	if baseRdmult <= 0 {
		baseRdmult = encoder.ComputeRDMultBasedOnQindex(qindex, encoder.RDFrameInter)
	}
	if bsize < common.BlockSizes && e.tpl.enabled {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		baseRdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, baseRdmult)
	}
	if baseRdmult <= 0 {
		baseRdmult = 1
	}
	e.cbRdmult = baseRdmult
	useResidualScore := e.vp9InterPreferVarianceRoot(inter, miRows, miCols,
		miRow, miCol, bsize)
	// SPEED_FEATURES.inter_mode_mask gates which inter modes the picker
	// evaluates per block size. At higher cpu_used libvpx drops NEARMV/NEWMV
	// on large blocks (INTER_NEAREST_NEW_ZERO). Reading the per-bsize mask
	// here verbatim matches libvpx's pickmode gate.
	// libvpx: vp9_pickmode.c:2150 — if (!(cpi->sf.inter_mode_mask[bsize] & (1 << this_mode))) continue;
	interModeMask := e.vp9InterModeMaskFor(bsize)
	sourceAltRefOverlay := e.vp9OnePassVBRSourceAltRefOverlay(inter) &&
		refFrame == vp9dec.AltrefFrame
	modeAllowed := func(mode common.PredictionMode) bool {
		if interModeMask&(1<<uint(mode)) == 0 {
			return false
		}
		if !sourceAltRefOverlay {
			return true
		}
		return mode == common.ZeroMv ||
			mode == common.NearestMv ||
			mode == common.NearMv
	}
	bestSet := false
	var best vp9InterModeDecision
	consider := func(mode common.PredictionMode, mv, refMv vp9dec.MV,
		filter vp9dec.InterpFilter, distortion uint64,
	) {
		rate := refRate +
			encoder.InterModeRateCost(&inter.selectFc, interModeCtx, mode,
				mv, refMv, inter.allowHP) +
			vp9InterInterpFilterRateCost(inter, &inter.selectFc, switchableCtx, filter)
		cand := vp9InterModeDecision{
			mode:         mode,
			mv:           [2]vp9dec.MV{mv},
			interpFilter: filter,
			rate:         rate,
			distortion:   distortion,
			score:        e.vp9InterModeScore(distortion, rate, qindex),
		}
		if useResidualScore && refFrame == vp9dec.LastFrame {
			if rdDist, rdRate, ok := e.scoreVP9InterModeResidual(inter, miRows,
				miCols, miRow, miCol, bsize, mode, refFrame, mv, filter); ok {
				cand.distortion = rdDist
				cand.rate = rate + rdRate
				cand.score = e.vp9InterModeScore(cand.distortion, cand.rate, qindex)
			}
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	zeroDistortion := vp9BlockSSE(src, srcStride, ref, refStride,
		x0, y0, x0, y0, scoreW, scoreH)
	allFilters := vp9InterInterpFilterCandidates(inter)
	// libvpx: vp9/encoder/vp9_speed_features.c — sf->disable_filter_search_var_thresh
	// prunes non-EIGHTTAP filters when source variance falls below the
	// threshold.  Mirror libvpx's source-only luma variance, not the
	// zero-motion reference error: a flat source block should skip extra
	// filter search even when the current reference is a poor predictor.
	if e.sf.DisableFilterSearchVarThresh > 0 && scoreW > 0 && scoreH > 0 &&
		len(allFilters) > 1 {
		sourceVariance := vp9SourceVarianceAreaPerPixel(src, srcStride,
			x0, y0, scoreW, scoreH)
		if e.vp9InterSkipFilterSearch(sourceVariance) {
			allFilters = allFilters[:1]
		}
	}

	// libvpx: vp9_pickmode.c:1731-1880 — realtime (nonrd) per-mode filter
	// selection.  filter_ref starts as cm->interp_filter and is overwritten
	// from above/left inter neighbours when default_interp_filter != BILINEAR.
	// pred_filter_search is (cm->interp_filter == SWITCHABLE), refined by a
	// chessboard pattern when sf.cb_pred_filter_search is set.
	//
	// In the realtime path (sf.use_nonrd_pick_mode == 1), the per-mode
	// candidate evaluation at vp9_pickmode.c:2318-2330 either:
	//   (a) sweeps {EIGHTTAP, EIGHTTAP_SMOOTH} via search_filter_ref when
	//       the MV is subpel AND pred_filter_search AND
	//       (this_mode == NEWMV || filter_ref == SWITCHABLE), OR
	//   (b) locks to filter = (filter_ref == SWITCHABLE) ? EIGHTTAP : filter_ref.
	//
	// govpx's slow (full RD) path keeps the libvpx vp9_rdopt.c three-filter
	// sweep over {EIGHTTAP, EIGHTTAP_SMOOTH, EIGHTTAP_SHARP}.
	useNonrd := e.sf.UseNonrdPickMode == 1
	frameInterp := vp9InterFrameInterpFilter(inter)
	filterRef := vp9NonrdFilterRef(frameInterp, e.sf.DefaultInterpFilter,
		above, left)
	predFilterSearch := vp9NonrdPredFilterSearch(frameInterp,
		e.sf.CbPredFilterSearch, miRow, miCol, bsize, e.frameIndex)
	// pickFilters returns the per-mode filter list following libvpx's
	// vp9_pick_inter_mode realtime gate.  In the slow path (useNonrd ==
	// false) it returns allFilters (the libvpx vp9_rd_pick_inter_mode_sb
	// three-filter sweep).
	pickFilters := func(mode common.PredictionMode, mv vp9dec.MV,
		refIsLast bool,
	) []vp9dec.InterpFilter {
		if !useNonrd {
			return allFilters
		}
		// libvpx: vp9_pickmode.c:2318-2330.  The realtime filter search
		// fires only when (a) the MV has subpel bits, (b) pred_filter_search
		// is on, (c) this_mode == NEWMV or filter_ref == SWITCHABLE, and
		// (d) ref_frame is LAST (or one of the special GOLDEN cases — SVC
		// or VBR — which govpx does not surface to this picker yet).
		if vp9MvHasSubpel(mv) && predFilterSearch && refIsLast &&
			(mode == common.NewMv || filterRef == vp9dec.InterpSwitchable) {
			return vp9NonrdSwitchableInterpFilterOrder[:]
		}
		// libvpx: vp9_pickmode.c:2330 — single-filter fallback.
		if filterRef == vp9dec.InterpSwitchable {
			return vp9EighttapInterpFilterOrder[:]
		}
		switch filterRef {
		case vp9dec.InterpEighttap:
			return vp9EighttapInterpFilterOrder[:]
		case vp9dec.InterpEighttapSmooth:
			return vp9SmoothInterpFilterOrder[:]
		case vp9dec.InterpEighttapSharp:
			return vp9SharpInterpFilterOrder[:]
		case vp9dec.InterpBilinear:
			return vp9BilinearInterpFilterOrder[:]
		default:
			return vp9EighttapInterpFilterOrder[:]
		}
	}
	refIsLast := refFrame == vp9dec.LastFrame
	if modeAllowed(common.ZeroMv) {
		for _, filter := range pickFilters(common.ZeroMv, vp9dec.MV{}, refIsLast) {
			consider(common.ZeroMv, vp9dec.MV{}, vp9dec.MV{}, filter,
				zeroDistortion)
		}
	}

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		if !modeAllowed(mode) {
			continue
		}
		mv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, inter.allowHP,
			inter.refSignBias)
		if !ok {
			continue
		}
		if sourceAltRefOverlay && mv != (vp9dec.MV{}) {
			continue
		}
		filters := pickFilters(mode, mv, refIsLast)
		if !vp9MvHasSubpel(mv) {
			distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
				miRow, miCol, bsize, mode, refFrame, mv, filters[0],
			)
			if ok {
				for _, filter := range filters {
					consider(mode, mv, mv, filter, distortion)
				}
			}
			continue
		}
		for _, filter := range filters {
			distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
				miRow, miCol, bsize, mode, refFrame, mv, filter)
			if ok {
				consider(mode, mv, mv, filter, distortion)
			}
		}
	}

	if modeAllowed(common.NewMv) {
		refMv, refMvOK := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, refFrame, inter.allowHP,
			inter.refSignBias)
		mvOpts := vp9InterMvSearchOptions{
			refMv:      refMv,
			refMvValid: refMvOK,
		}
		if seed, ok := e.vp9InterMvPredSearchSeed(inter, tile, miRows, miCols,
			miRow, miCol, bsize, refFrame); ok {
			mvOpts.seed = seed
			mvOpts.seedValid = true
		}
		if mv, _, ok := e.pickVP9InterMvWithOptions(inter, miRows, miCols,
			miRow, miCol, bsize, refFrame, mvOpts); ok {
			filters := pickFilters(common.NewMv, mv, refIsLast)
			if !vp9MvHasSubpel(mv) {
				distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
					miRow, miCol, bsize, common.NewMv, refFrame, mv,
					filters[0])
				if ok {
					for _, filter := range filters {
						consider(common.NewMv, mv, refMv, filter, distortion)
					}
				}
			} else {
				for _, filter := range filters {
					distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
						miRow, miCol, bsize, common.NewMv, refFrame, mv, filter,
					)
					if ok {
						consider(common.NewMv, mv, refMv, filter, distortion)
					}
				}
			}
		}
	}
	e.cbRdmult = prevCbRdmult
	if !bestSet {
		return vp9InterModeDecision{}, false
	}
	return best, true
}

func (e *VP9Encoder) pickVP9Sub8InterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8, refRate int,
) (vp9InterModeDecision, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid ||
		bsize >= common.Block8x8 {
		return vp9InterModeDecision{}, false
	}
	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()
	interModeMask := e.vp9InterModeMaskFor(bsize)
	sourceAltRefOverlay := e.vp9OnePassVBRSourceAltRefOverlay(inter) &&
		refFrame == vp9dec.AltrefFrame
	modeAllowed := func(mode common.PredictionMode) bool {
		if interModeMask&(1<<uint(mode)) == 0 {
			return false
		}
		if !sourceAltRefOverlay {
			return true
		}
		return mode == common.ZeroMv ||
			mode == common.NearestMv ||
			mode == common.NearMv
	}

	filters := vp9InterInterpFilterCandidates(inter)
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	bestSet := false
	var best vp9InterModeDecision
	for _, mode := range [...]common.PredictionMode{
		common.ZeroMv,
		common.NearestMv,
		common.NearMv,
	} {
		if !modeAllowed(mode) {
			continue
		}
		base := vp9dec.NeighborMi{
			SbType: bsize,
			RefFrame: [2]int8{
				refFrame,
				vp9dec.NoRefFrame,
			},
		}
		if !e.fillVP9Sub8InterBmi(&base, tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, inter.allowHP,
			inter.refSignBias) {
			continue
		}
		if sourceAltRefOverlay {
			nonZero := false
			for i := range base.Bmi {
				if base.Bmi[i].AsMv[0] != (vp9dec.MV{}) {
					nonZero = true
					break
				}
			}
			if nonZero {
				continue
			}
		}
		modeRate := 0
		for idy := 0; idy < 2; idy += num4x4H {
			for idx := 0; idx < 2; idx += num4x4W {
				j := idy*2 + idx
				modeRate += encoder.InterModeRateCost(&inter.selectFc,
					interModeCtx, base.Bmi[j].AsMode,
					base.Bmi[j].AsMv[0], vp9dec.MV{}, inter.allowHP)
			}
		}
		for _, filter := range filters {
			candMi := base
			candMi.InterpFilter = uint8(filter)
			distortion, ok := e.vp9InterPredictionDistortionForMi(inter,
				miRows, miCols, miRow, miCol, bsize, &candMi)
			if !ok {
				continue
			}
			rate := refRate + modeRate +
				vp9InterInterpFilterRateCost(inter, &inter.selectFc,
					switchableCtx, filter)
			cand := vp9InterModeDecision{
				refFrame:       refFrame,
				secondRefFrame: vp9dec.NoRefFrame,
				mode:           candMi.Mode,
				mv:             candMi.Mv,
				bmi:            candMi.Bmi,
				interpFilter:   filter,
				rate:           rate,
				distortion:     distortion,
				score:          e.vp9InterModeScore(distortion, rate, qindex),
			}
			if e.sf.UseNonrdPickMode == 0 {
				if rdDist, rdRate, hasResidue, ok := e.scoreVP9InterTxCandidate(
					inter, miRows, miCols, miRow, miCol, bsize,
					common.Tx4x4); ok {
					if !hasResidue {
						rdRate = 0
					}
					cand.distortion = rdDist
					cand.rate = rate + rdRate
					cand.score = e.vp9InterModeScore(cand.distortion,
						cand.rate, qindex)
				}
			}
			if !bestSet || cand.score < best.score ||
				(cand.score == best.score && cand.rate < best.rate) {
				best = cand
				bestSet = true
			}
		}
	}
	return best, bestSet
}

func (e *VP9Encoder) fillVP9Sub8InterBmi(mi *vp9dec.NeighborMi,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mode common.PredictionMode, refFrame int8,
	allowHP bool, signBias [vp9dec.MaxRefFrames]uint8,
) bool {
	if mi == nil || bsize >= common.Block8x8 {
		return false
	}
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			j := idy*2 + idx
			mv := vp9dec.MV{}
			switch mode {
			case common.ZeroMv:
			case common.NearestMv, common.NearMv:
				mv = e.vp9AppendSub8x8MvsForIdx(mi, tile, miRows, miCols,
					miRow, miCol, bsize, mode, j, 0, refFrame, signBias)
				vp9dec.LowerMvPrecision(&mv, allowHP)
			default:
				return false
			}
			mi.Bmi[j].AsMode = mode
			mi.Bmi[j].AsMv[0] = mv
			if num4x4H == 2 {
				mi.Bmi[j+2] = mi.Bmi[j]
			}
			if num4x4W == 2 {
				mi.Bmi[j+1] = mi.Bmi[j]
			}
		}
	}
	mi.Mode = mi.Bmi[3].AsMode
	mi.Mv = mi.Bmi[3].AsMv
	return true
}

func vp9Sub8InterModeValid(mode common.PredictionMode) bool {
	return mode >= common.NearestMv && mode <= common.NewMv
}

func vp9Sub8InterBmiValid(mi *vp9dec.NeighborMi) bool {
	if mi == nil {
		return false
	}
	for i := range mi.Bmi {
		if !vp9Sub8InterModeValid(mi.Bmi[i].AsMode) {
			return false
		}
	}
	return true
}

func (e *VP9Encoder) ensureVP9Sub8InterBmiForWrite(mi *vp9dec.NeighborMi,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, inter *vp9InterEncodeState,
) bool {
	if mi == nil {
		return false
	}
	if bsize >= common.Block8x8 || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return true
	}
	if vp9Sub8InterBmiValid(mi) {
		mi.Mode = mi.Bmi[3].AsMode
		mi.Mv = mi.Bmi[3].AsMv
		return true
	}
	mode := mi.Mode
	if !vp9Sub8InterModeValid(mode) || mode == common.NewMv {
		mode = common.ZeroMv
	}
	refFrame := mi.RefFrame[0]
	signBias := [vp9dec.MaxRefFrames]uint8{}
	allowHP := true
	if inter != nil {
		signBias = inter.refSignBias
		allowHP = inter.allowHP
	}
	return e.fillVP9Sub8InterBmi(mi, tile, miRows, miCols, miRow, miCol,
		bsize, mode, refFrame, allowHP, signBias)
}

func (e *VP9Encoder) vp9InterMvPredSearchSeed(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
) (vp9dec.MV, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid ||
		bsize < common.Block8x8 {
		return vp9dec.MV{}, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	refBuf, refStride, refOriginX, refOriginY, _, _, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(src) == 0 || len(refBuf) == 0 || srcStride <= 0 ||
		refStride <= 0 || !refOK {
		return vp9dec.MV{}, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH {
		return vp9dec.MV{}, false
	}
	refRows := len(refBuf) / refStride
	var candidates [vp9MvPredMaxCandidates]vp9MvPredInputCandidate
	refList, refCount := vp9dec.FindInterMvRefsFields(e.miGrid,
		e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize,
		common.NearMv, refFrame, inter.refSignBias, -1)
	if refCount >= 1 {
		candidates[0] = vp9MvPredInputCandidate{mv: refList[0], valid: true}
	}
	if refCount >= 2 {
		candidates[1] = vp9MvPredInputCandidate{mv: refList[1], valid: true}
	}
	if predMv, ok := e.vp9VarPartSBPredMv(miCols, miRow, miCol, refFrame); ok {
		candidates[2] = vp9MvPredInputCandidate{mv: predMv, valid: true}
	}
	maxPartitionSize := e.sf.DefaultMaxPartitionSize
	if maxPartitionSize == 0 {
		maxPartitionSize = common.Block64x64
	}
	result := vp9MvPredScanCandidates(candidates[:],
		vp9MvPredNumCandidates(bsize, maxPartitionSize),
		src, srcStride, x0, y0,
		refBuf, refStride, x0, y0, refOriginX, refOriginY, refRows,
		blockW, blockH)
	if result.bestIndex < 0 || result.bestIndex >= len(candidates) ||
		!candidates[result.bestIndex].valid {
		return vp9dec.MV{}, false
	}
	return candidates[result.bestIndex].mv, true
}

func vp9VisibleInterScoreBlock(x0, y0, blockW, blockH int,
	srcW, srcH, refW, refH int,
) (int, int, bool) {
	if x0 < 0 || y0 < 0 || blockW <= 0 || blockH <= 0 ||
		x0 >= srcW || y0 >= srcH || x0 >= refW || y0 >= refH {
		return 0, 0, false
	}
	scoreW := min(blockW, srcW-x0)
	scoreW = min(scoreW, refW-x0)
	scoreH := min(blockH, srcH-y0)
	scoreH = min(scoreH, refH-y0)
	return scoreW, scoreH, scoreW > 0 && scoreH > 0
}

type vp9InterMvSearchOptions struct {
	seed            vp9dec.MV
	seedValid       bool
	refMv           vp9dec.MV
	refMvValid      bool
	nonrdSubpelTree bool
	useMvPart       bool
}

func (e *VP9Encoder) pickVP9InterMvWithOptions(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
	opts vp9InterMvSearchOptions,
) (vp9dec.MV, uint64, bool) {
	mv, score, ok := e.pickVP9InterMvAllowZero(inter, miRows, miCols,
		miRow, miCol, bsize, refFrame, opts)
	if !ok || mv == (vp9dec.MV{}) {
		return vp9dec.MV{}, score, false
	}
	return mv, score, true
}

// scoreVP9InterModeResidual gives flat-root LAST candidates a small non-RD
// residual model analogous to libvpx vp9_pick_inter_mode's model/block Y RD
// pass. Prediction SSE alone overvalues tiny subpel NEWMV gains on flat deltas.
func (e *VP9Encoder) scoreVP9InterModeResidual(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (uint64, int, bool) {
	if inter == nil || inter.dq == nil {
		return 0, 0, false
	}
	txSize := clampVP9TxSizeForBlock(common.Tx16x16, bsize)
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       txSize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, 0, false
	}
	distortion, rate, hasResidue, ok := e.scoreVP9InterTxCandidate(inter,
		miRows, miCols, miRow, miCol, bsize, txSize)
	if !ok {
		return 0, 0, false
	}
	if !hasResidue {
		rate = 0
	}
	return distortion, rate, true
}

func (e *VP9Encoder) pickVP9InterMvAllowZero(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
	opts vp9InterMvSearchOptions,
) (vp9dec.MV, uint64, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return vp9dec.MV{}, 0, false
	}
	if bsize < common.Block8x8 {
		return vp9dec.MV{}, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refOriginX, refOriginY, _, _, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return vp9dec.MV{}, 0, false
	}
	if !refOK {
		return vp9dec.MV{}, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH {
		return vp9dec.MV{}, 0, false
	}
	srcOff := y0*srcStride + x0
	refRows := len(ref) / refStride
	refFullDx, refFullDy := 0, 0
	refMvForRange := vp9dec.MV{}
	if opts.refMvValid {
		refMvForRange = opts.refMv
		refFullDx = int(opts.refMv.Col) >> 3
		refFullDy = int(opts.refMv.Row) >> 3
	}
	mvLimits := vp9EncoderMvLimits(miRows, miCols, miRow, miCol, bsize)
	mvLimits.setFullpelSearchRange(refMvForRange)
	sadAt := func(dx, dy int) (uint64, bool) {
		if !mvLimits.inFullpelRange(dy, dx) {
			return 0, false
		}
		refX := x0 + dx
		refY := y0 + dy
		bufX := refOriginX + refX
		bufY := refOriginY + refY
		if bufX < 0 || bufY < 0 || bufX+blockW > refStride ||
			bufY+blockH > refRows {
			return 0, false
		}
		refOff := bufY*refStride + bufX
		return vp9BlockSADOffsets(src, srcOff, srcStride, ref, refOff,
			refStride, blockW, blockH, ^uint64(0)), true
	}

	sadPerBit := vp9SADPerBit16(e.vp9EncoderModeDecisionQIndex())
	scoreMv := func(dx, dy int, sad uint64) uint64 {
		return sad + uint64(vp9FullPelMVSADCost(dy, dx,
			refFullDy, refFullDx, sadPerBit))
	}
	bestSad, ok := sadAt(0, 0)
	if !ok {
		return vp9dec.MV{}, 0, false
	}
	bestScore := scoreMv(0, 0, bestSad)
	bestDx, bestDy := 0, 0
	searchCenterDx, searchCenterDy := 0, 0
	searchFromSeed := false
	seededStart := false
	eval := func(dx, dy int) bool {
		if dx == bestDx && dy == bestDy {
			return false
		}
		sad, ok := sadAt(dx, dy)
		if !ok {
			return false
		}
		score := scoreMv(dx, dy, sad)
		if score < bestScore {
			bestScore = score
			bestSad = sad
			bestDx = dx
			bestDy = dy
			return true
		}
		return false
	}
	if opts.seedValid {
		seedDx := int(opts.seed.Col) >> 3
		seedDy := int(opts.seed.Row) >> 3
		seedDy, seedDx = mvLimits.clampFullpel(seedDy, seedDx)
		if sad, ok := sadAt(seedDx, seedDy); ok {
			bestSad = sad
			bestScore = scoreMv(seedDx, seedDy, bestSad)
			bestDx = seedDx
			bestDy = seedDy
			seededStart = true
			searchCenterDx = seedDx
			searchCenterDy = seedDy
			searchFromSeed = true
		}
	}

	if !(opts.useMvPart && seededStart) {
		// MV-hint biasing: when a multi-resolution lower-resolution layer
		// has supplied a scaled MV hint for this SB, evaluate it as an
		// extra candidate before the (0,0)-centered fan. The hint can
		// land outside the local 16-pixel radius (libvpx-style cross-
		// resolution motion correlation regularly produces hints that
		// exceed the realtime search radius); when that happens the
		// search radius widens to encompass the hint so the refinement
		// step can still walk a local fan around the winning candidate.
		// When no hint is installed this branch is a nil-check.
		//
		// libvpx: SPEED_FEATURES.mv.search_method picks the
		// fast-diamond / bigdia / NSTEP dispatcher (vp9_mcomp.c:2875). At
		// cpu_used=8 the configurator pins FAST_DIAMOND, which caps the
		// effective search radius to a 4-pel fan. Read that field here
		// instead of always running the full 16-pel search.
		searchRadius := e.vp9InterSearchRadius()
		if refFrame == vp9dec.LastFrame {
			if hintDx, hintDy, ok := e.vp9MVHintCandidatePixelOffset(miRow, miCol); ok {
				if !seededStart && eval(hintDx, hintDy) && searchFromSeed {
					searchCenterDx = hintDx
					searchCenterDy = hintDy
				}
				// Widen the search radius so the refinement loop can
				// walk a small fan around the hint when it wins.
				absDx := hintDx
				if absDx < 0 {
					absDx = -absDx
				}
				absDy := hintDy
				if absDy < 0 {
					absDy = -absDy
				}
				if absDx > searchRadius {
					searchRadius = absDx
				}
				if absDy > searchRadius {
					searchRadius = absDy
				}
			}
		}

		scanMinDx, scanMaxDx := -searchRadius, searchRadius
		scanMinDy, scanMaxDy := -searchRadius, searchRadius
		if searchFromSeed {
			scanMinDx = searchCenterDx - searchRadius
			scanMaxDx = searchCenterDx + searchRadius
			scanMinDy = searchCenterDy - searchRadius
			scanMaxDy = searchCenterDy + searchRadius
		}
		if e.sf.Mv.SearchMethod == SearchMethodFastDiamond {
			bestDx, bestDy, bestSad, bestScore = vp9FastDiamondPatternSearchSAD(
				bestDx, bestDy, bestSad, bestScore, e.sf.Mv.FullpelSearchStepParam,
				&mvLimits, sadAt, scoreMv)
		} else if e.sf.Mv.SearchMethod == SearchMethodNStep ||
			e.sf.Mv.SearchMethod == SearchMethodMesh {
			bestDx, bestDy, bestSad, bestScore = vp9NStepDiamondSearchSAD(
				bestDx, bestDy, bestSad, bestScore, e.sf.Mv.FullpelSearchStepParam,
				&mvLimits, sadAt, scoreMv)
		} else {
			// Coarse fan for non-FAST_DIAMOND methods. We size the coarse
			// step so the fan covers +/-searchRadius without exceeding it.
			coarseStep := max(e.vp9InterSearchCoarseStep(), 1)
			for dy := scanMinDy; dy <= scanMaxDy; dy += coarseStep {
				for dx := scanMinDx; dx <= scanMaxDx; dx += coarseStep {
					eval(dx, dy)
				}
			}
			for step := coarseStep >> 1; step >= 1; step >>= 1 {
				improved := true
				for improved {
					improved = false
					centerDx, centerDy := bestDx, bestDy
					for dy := centerDy - step; dy <= centerDy+step; dy += step {
						for dx := centerDx - step; dx <= centerDx+step; dx += step {
							if dx < scanMinDx || dx > scanMaxDx ||
								dy < scanMinDy || dy > scanMaxDy {
								continue
							}
							if eval(dx, dy) {
								improved = true
							}
						}
					}
				}
			}
		}
	}
	mv := vp9dec.MV{Row: int16(bestDy * 8), Col: int16(bestDx * 8)}
	vp9dec.ClampMvRef(&mv, miRows, miCols, miRow, miCol, bsize)
	vp9dec.LowerMvPrecision(&mv, inter.allowHP)
	// SPEED_FEATURES.mv.subpel_force_stop == FULL_PEL — libvpx skips
	// vp9_find_best_sub_pixel_tree* entirely. govpx mirrors that gate here.
	//
	// libvpx: vp9_mcomp.c — find_best_sub_pixel_tree_pruned_more returns
	// early when forcestop == FULL_PEL.
	if e.vp9InterSubpelEnabled() {
		mv, bestScore = e.refineVP9InterSubpelMv(inter, miRows, miCols,
			miRow, miCol, bsize, refFrame, mv, bestSad, bestScore,
			opts.refMv, opts.refMvValid, opts.nonrdSubpelTree)
	}
	return mv, bestScore, true
}

func vp9SADPerBit16(qindex int) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	q := vp9ConvertQIndexToQ(qindex)
	return int(0.0418*q + 2.4107)
}

func vp9FullPelMVSADCost(mvRow, mvCol, refRow, refCol, sadPerBit int) int {
	row := mvRow - refRow
	col := mvCol - refCol
	jointCost := 300
	if row == 0 && col == 0 {
		jointCost = 600
	}
	cost := jointCost + vp9MVSADComponentCost(row) + vp9MVSADComponentCost(col)
	// libvpx: mvsad_err_cost rounds by VP9_PROB_COST_SHIFT (9).
	return (cost*sadPerBit + 256) >> 9
}
