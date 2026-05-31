package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func vp9InterEdgeBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) (common.BlockSize, bool) {
	if root != common.Block64x64 {
		return common.BlockInvalid, false
	}
	maxW := int(common.Num8x8BlocksWideLookup[root])
	maxH := int(common.Num8x8BlocksHighLookup[root])
	availW := min(miCols-miCol, maxW)
	availH := min(miRows-miRow, maxH)
	if availW >= maxW-1 && availH >= maxH-1 {
		return root, true
	}
	if (availW < maxW || availH < maxH) && availW >= 4 && availH >= 4 {
		return common.Block32x32, true
	}
	return common.BlockInvalid, false
}

func vp9KeyframeSourceBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) common.BlockSize {
	maxW := min(miCols-miCol, int(common.Num8x8BlocksWideLookup[root]))
	maxH := min(miRows-miRow, int(common.Num8x8BlocksHighLookup[root]))
	if maxW > 4 {
		maxW = 4
	}
	if maxH > 4 {
		maxH = 4
	}
	if maxW >= 3 && maxH >= 3 {
		return common.Block32x32
	}
	for _, bsize := range vp9StubBlockSizeOrder {
		if int(common.Num8x8BlocksWideLookup[bsize]) <= maxW &&
			int(common.Num8x8BlocksHighLookup[bsize]) <= maxH {
			return bsize
		}
	}
	return common.Block4x4
}

func (e *VP9Encoder) pickVP9KeyframeTexturePartitionBlockSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int, root common.BlockSize,
) (common.BlockSize, bool) {
	if key == nil || key.img == nil || key.dq == nil || root <= common.Block4x4 ||
		root >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	splitSize := common.SubsizeLookup[common.PartitionSplit][root]
	if splitSize >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[root])
	blockMiH := int(common.Num8x8BlocksHighLookup[root])
	if miCol+blockMiW > miCols || miRow+blockMiH > miRows {
		return common.BlockInvalid, false
	}
	src, stride, width, height := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return common.BlockInvalid, false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	blockW := int(common.Num4x4BlocksWideLookup[root]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[root]) * 4
	if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, width, height) {
		return common.BlockInvalid, false
	}
	variance := encoder.BlockSourceVariance128(src, stride, x0, y0, blockW, blockH)
	threshold := vp9KeyframeVariancePartitionThreshold(key.dq.Y[0][1], root)
	if threshold == 0 {
		return common.BlockInvalid, false
	}
	if variance > threshold {
		if root == common.Block8x8 {
			if sub8Size, ok := e.pickVP9KeyframeSub8x8RDPartitionBlockSize(key,
				tile, miRows, miCols, miRow, miCol); ok {
				return sub8Size, true
			}
		}
		return splitSize, true
	}
	return common.BlockInvalid, false
}

type vp9KeyframeIntraRD struct {
	mode          common.PredictionMode
	rate          int
	rateTokenOnly int
	distortion    uint64
	skippable     bool
}

type vp9KeyframePartitionRD struct {
	target     common.BlockSize
	partition  common.PartitionType
	rate       int
	distortion uint64
	score      uint64
}

// pickVP9KeyframeRDPartitionBlockSize mirrors libvpx's RD-row keyframe
// partition picker for the small fixed-Q realtime lane where speed features
// request VAR_BASED_PARTITION but use_nonrd_pick_mode is still false. In that
// row libvpx reaches rd_pick_partition, not choose_partitioning.
func (e *VP9Encoder) pickVP9KeyframeRDPartitionBlockSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
	txMode common.TxMode,
) (common.BlockSize, bool) {
	if !e.vp9KeyframeRDPartitionEnabled(key) || root < common.Block8x8 ||
		root >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	if partitionProbs == nil {
		probs := tables.KfPartitionProbs
		partitionProbs = &probs
	}
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, root)
	if !ok {
		return common.BlockInvalid, false
	}
	defer e.releaseVP9PartitionReconSnapshot(reconSnap)
	ctxSnap, ctxOK := e.snapshotVP9PartitionContexts(miRow, miCol, root)
	var miSaved [64]vp9dec.NeighborMi
	miRowsSaved, miColsSaved, miOK := e.snapshotVP9MiRect(miRows, miCols,
		miRow, miCol, int(common.Num8x8BlocksHighLookup[root]),
		int(common.Num8x8BlocksWideLookup[root]), miSaved[:])
	if !ctxOK || !miOK {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		return common.BlockInvalid, false
	}
	restoreBase := func() {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol,
			miRowsSaved, miColsSaved, miSaved[:])
		e.restoreVP9PartitionContexts(ctxSnap)
		e.restoreVP9PartitionReconSnapshotPixels(reconSnap)
	}

	rd, ok := e.scoreVP9KeyframeRDPartitionTree(key, tile, partitionProbs,
		miRows, miCols, miRow, miCol, root, txMode, ^uint64(0), true,
		key.counts != nil)
	restoreBase()
	if !ok {
		e.partitionReconScratchTop = reconSnap.top
		return common.BlockInvalid, false
	}
	e.partitionReconScratchTop = reconSnap.top
	return rd.target, true
}

// scoreVP9KeyframeRDPartitionTree compares NONE, SPLIT, HORZ, and VERT under
// the same state discipline as rd_pick_partition: every candidate starts from
// the caller's partition/reconstruction state, but a candidate may replay its
// chosen children so later blocks in that candidate see the right intra edges.
func (e *VP9Encoder) scoreVP9KeyframeRDPartitionTree(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
	txMode common.TxMode, bestRD uint64, apply, store bool,
) (vp9KeyframePartitionRD, bool) {
	if root < common.Block8x8 {
		return e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile, miRows, miCols,
			miRow, miCol, root, txMode, bestRD, apply, store)
	}
	horzSize, vertSize, splitSize, ok := encoder.InterRDPartitionSizes(root)
	if !ok {
		return e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile, miRows, miCols,
			miRow, miCol, root, txMode, bestRD, apply, store)
	}
	bsl := int(common.BWidthLog2Lookup[root])
	bs := (1 << uint(bsl)) / 4
	hasRows := miRow+bs < miRows
	hasCols := miCol+bs < miCols
	noneAllowed := hasRows && hasCols
	horzAllowed := hasCols
	vertAllowed := hasRows
	doSplit := root >= common.Block8x8
	if e.sf.AutoMinMaxPartitionSize != AutoMinMaxNotInUse {
		minSize := e.sf.DefaultMinPartitionSize
		maxSize := e.sf.DefaultMaxPartitionSize
		noneAllowed = noneAllowed && root <= maxSize
		horzAllowed = horzAllowed && ((root <= maxSize && root > minSize) || !hasRows)
		vertAllowed = vertAllowed && ((root <= maxSize && root > minSize) || !hasCols)
		doSplit = doSplit && root > minSize
	}
	if e.sf.UseSquarePartitionOnly != 0 &&
		(root > e.sf.UseSquareOnlyThreshHigh || root < e.sf.UseSquareOnlyThreshLow) {
		horzAllowed = horzAllowed && !hasRows
		vertAllowed = vertAllowed && !hasCols
	}

	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, root)
	if !ok {
		return vp9KeyframePartitionRD{}, false
	}
	defer e.releaseVP9PartitionReconSnapshot(reconSnap)
	ctxSnap, ctxOK := e.snapshotVP9PartitionContexts(miRow, miCol, root)
	var miSaved [64]vp9dec.NeighborMi
	miRowsSaved, miColsSaved, miOK := e.snapshotVP9MiRect(miRows, miCols,
		miRow, miCol, int(common.Num8x8BlocksHighLookup[root]),
		int(common.Num8x8BlocksWideLookup[root]), miSaved[:])
	if !ctxOK || !miOK {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		return vp9KeyframePartitionRD{}, false
	}
	restoreBase := func() {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol,
			miRowsSaved, miColsSaved, miSaved[:])
		e.restoreVP9PartitionContexts(ctxSnap)
		e.restoreVP9PartitionReconSnapshotPixels(reconSnap)
	}

	rdmult := e.vp9KeyframePartitionRDMul(miRow, miCol, root)
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	bestSet := false
	var best vp9KeyframePartitionRD
	consider := func(partition common.PartitionType,
		score func(bool, bool) (vp9KeyframePartitionRD, bool),
	) {
		restoreBase()
		rd, ok := score(true, false)
		if !ok {
			return
		}
		rd.partition = partition
		rd.target = common.SubsizeLookup[partition][root]
		if partition == common.PartitionNone {
			rd.target = root
		}
		rd.rate += encoder.PartitionRateCost(partitionProbs, ctx, partition,
			hasRows, hasCols)
		rd.score = encoder.RDCost(rdmult, encoder.RDDivBits, rd.rate,
			rd.distortion)
		if !bestSet || rd.score < best.score ||
			(rd.score == best.score && rd.rate < best.rate) {
			best = rd
			bestSet = true
		}
	}
	if noneAllowed {
		consider(common.PartitionNone, func(apply, store bool) (vp9KeyframePartitionRD, bool) {
			rd, ok := e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile,
				miRows, miCols, miRow, miCol, root, txMode, bestRD, apply, store)
			if ok && apply {
				e.updateVP9PartitionContextForChoice(miRow, miCol, root,
					common.PartitionNone, root)
			}
			return rd, ok
		})
	}
	if doSplit {
		consider(common.PartitionSplit, func(apply, store bool) (vp9KeyframePartitionRD, bool) {
			return e.scoreVP9KeyframeRDPartitionSplit(key, tile, partitionProbs,
				miRows, miCols, miRow, miCol, root, splitSize, txMode,
				bestRD, apply, store)
		})
	}
	if horzAllowed {
		consider(common.PartitionHorz, func(apply, store bool) (vp9KeyframePartitionRD, bool) {
			return e.scoreVP9KeyframeRDPartitionRect(key, tile, miRows, miCols,
				miRow, miCol, root, horzSize, common.PartitionHorz, bs, 0,
				txMode, bestRD, apply, store)
		})
	}
	if vertAllowed {
		consider(common.PartitionVert, func(apply, store bool) (vp9KeyframePartitionRD, bool) {
			return e.scoreVP9KeyframeRDPartitionRect(key, tile, miRows, miCols,
				miRow, miCol, root, vertSize, common.PartitionVert, 0, bs,
				txMode, bestRD, apply, store)
		})
	}
	if !bestSet {
		restoreBase()
		e.partitionReconScratchTop = reconSnap.top
		return vp9KeyframePartitionRD{}, false
	}
	if !apply {
		restoreBase()
		e.partitionReconScratchTop = reconSnap.top
		return best, true
	}

	restoreBase()
	var committed vp9KeyframePartitionRD
	switch best.partition {
	case common.PartitionNone:
		committed, ok = e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile,
			miRows, miCols, miRow, miCol, root, txMode, bestRD, true, store)
		if ok {
			e.updateVP9PartitionContextForChoice(miRow, miCol, root,
				common.PartitionNone, root)
		}
	case common.PartitionSplit:
		committed, ok = e.scoreVP9KeyframeRDPartitionSplit(key, tile,
			partitionProbs, miRows, miCols, miRow, miCol, root, splitSize,
			txMode, bestRD, true, store)
	case common.PartitionHorz:
		committed, ok = e.scoreVP9KeyframeRDPartitionRect(key, tile, miRows,
			miCols, miRow, miCol, root, horzSize, common.PartitionHorz, bs, 0,
			txMode, bestRD, true, store)
	case common.PartitionVert:
		committed, ok = e.scoreVP9KeyframeRDPartitionRect(key, tile, miRows,
			miCols, miRow, miCol, root, vertSize, common.PartitionVert, 0, bs,
			txMode, bestRD, true, store)
	default:
		ok = false
	}
	if !ok {
		restoreBase()
		e.partitionReconScratchTop = reconSnap.top
		return vp9KeyframePartitionRD{}, false
	}
	committed.partition = best.partition
	committed.target = best.target
	committed.rate += encoder.PartitionRateCost(partitionProbs, ctx, best.partition,
		hasRows, hasCols)
	committed.score = encoder.RDCost(rdmult, encoder.RDDivBits, committed.rate,
		committed.distortion)
	if store {
		e.storeVP9KeyframePartitionDecision(miRow, miCol, root, best.target)
	}
	e.partitionReconScratchTop = reconSnap.top
	return committed, true
}

func (e *VP9Encoder) scoreVP9KeyframeRDPartitionSplit(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root, child common.BlockSize,
	txMode common.TxMode, bestRD uint64, apply, store bool,
) (vp9KeyframePartitionRD, bool) {
	var out vp9KeyframePartitionRD
	if child < common.Block8x8 {
		rd, ok := e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile,
			miRows, miCols, miRow, miCol, child, txMode, bestRD, apply, store)
		if !ok {
			return vp9KeyframePartitionRD{}, false
		}
		out.rate += rd.rate
		out.distortion += rd.distortion
	} else {
		stepMi := int(common.Num8x8BlocksWideLookup[child])
		for rowOff := 0; rowOff <= stepMi; rowOff += stepMi {
			for colOff := 0; colOff <= stepMi; colOff += stepMi {
				if miRow+rowOff >= miRows || miCol+colOff >= miCols {
					continue
				}
				rd, ok := e.scoreVP9KeyframeRDPartitionTree(key, tile,
					partitionProbs, miRows, miCols, miRow+rowOff,
					miCol+colOff, child, txMode, bestRD, true, store)
				if !ok {
					return vp9KeyframePartitionRD{}, false
				}
				out.rate += rd.rate
				out.distortion += rd.distortion
			}
		}
	}
	if apply {
		e.updateVP9PartitionContextForChoice(miRow, miCol, root,
			common.PartitionSplit, child)
	}
	out.target = child
	out.partition = common.PartitionSplit
	return out, true
}

func (e *VP9Encoder) scoreVP9KeyframeRDPartitionRect(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	root, child common.BlockSize, partition common.PartitionType, rowOff, colOff int,
	txMode common.TxMode, bestRD uint64, apply, store bool,
) (vp9KeyframePartitionRD, bool) {
	first, ok := e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile, miRows,
		miCols, miRow, miCol, child, txMode, bestRD, apply, store)
	if !ok {
		return vp9KeyframePartitionRD{}, false
	}
	out := vp9KeyframePartitionRD{
		target:     child,
		partition:  partition,
		rate:       first.rate,
		distortion: first.distortion,
	}
	if child >= common.Block8x8 {
		secondRow := miRow + rowOff
		secondCol := miCol + colOff
		if secondRow < miRows && secondCol < miCols {
			second, ok := e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile,
				miRows, miCols, secondRow, secondCol, child, txMode,
				bestRD, apply, store)
			if !ok {
				return vp9KeyframePartitionRD{}, false
			}
			out.rate += second.rate
			out.distortion += second.distortion
		}
	}
	if apply {
		e.updateVP9PartitionContextForChoice(miRow, miCol, root, partition, child)
	}
	return out, true
}

func (e *VP9Encoder) scoreVP9KeyframeRDPartitionLeafForTree(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode, refBestRD uint64,
	apply, store bool,
) (vp9KeyframePartitionRD, bool) {
	rdmult := e.vp9KeyframePartitionRDMul(miRow, miCol, bsize)
	rd, decision, ok := e.scoreVP9KeyframeRDPartitionLeaf(key, tile,
		miRows, miCols, miRow, miCol, bsize, txMode, rdmult, refBestRD)
	if !ok {
		return vp9KeyframePartitionRD{}, false
	}
	if apply {
		e.applyVP9KeyframeRDLeafDecision(key, tile, miRows, miCols, miRow,
			miCol, bsize, decision, store)
	}
	return vp9KeyframePartitionRD{
		target:     bsize,
		partition:  common.PartitionNone,
		rate:       rd.rate,
		distortion: rd.distortion,
		score:      encoder.RDCost(rdmult, encoder.RDDivBits, rd.rate, rd.distortion),
	}, true
}

func (e *VP9Encoder) applyVP9KeyframeRDLeafDecision(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, decision vp9KeyframeModeDecision, store bool,
) {
	reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       decision.txSize,
		Mode:         decision.mode,
		Bmi:          decision.bmi,
		RefFrame:     [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
		InterpFilter: uint8(vp9dec.SwitchableFilters),
		Skip:         1,
	}
	if e.prepareVP9KeyframeBlockResidue(key, tile, miRows, miCols, miRow, miCol,
		reconBsize, &mi, decision.uvMode) {
		mi.Skip = 0
	}
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, mi)
	if store {
		e.storeVP9LeafKeyframeDecision(miRow, miCol, bsize, decision)
	}
}

func (e *VP9Encoder) vp9KeyframePartitionRDMul(miRow, miCol int,
	bsize common.BlockSize,
) int {
	rdmult := encoder.KeyframeRDMul(e.vp9EncoderModeDecisionQIndex())
	if e.tpl.Enabled && bsize < common.BlockSizes {
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, rdmult)
	}
	return rdmult
}

func (e *VP9Encoder) pickVP9KeyframeSub8x8RDPartitionBlockSize(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
) (common.BlockSize, bool) {
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, common.Block8x8)
	if !ok {
		return common.BlockInvalid, false
	}
	defer e.releaseVP9PartitionReconSnapshot(reconSnap)
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx, miRow, miCol,
		common.Block8x8)
	probs := tables.KfPartitionProbs
	qindex := e.vp9EncoderModeDecisionQIndex()
	rdmult := encoder.KeyframeRDMul(qindex)
	if e.tpl.Enabled {
		rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, 1, 1, rdmult)
	}
	bsl := int(common.BWidthLog2Lookup[common.Block8x8])
	hasRows := miRow < miRows
	hasCols := miCol < miCols

	bestSize := common.BlockInvalid
	bestScore := uint64(^uint64(0))
	bestRate := 0
	var bestDistortion uint64
	var bestDecision vp9KeyframeModeDecision
	bestValid := false
	doRect := true
	// libvpx's rd_pick_partition restores entropy contexts between
	// NONE/SPLIT/HORZ/VERT trials but not the reconstructed pixels left by
	// rd_pick_sb_modes. Keep those candidate side effects during scoring,
	// then restore the caller-visible recon state after the pick.
	for _, cand := range [...]common.BlockSize{
		common.Block8x8,
		common.Block4x4,
		common.Block8x4,
		common.Block4x8,
	} {
		partition := common.PartitionLookup[bsl][cand]
		if !doRect && (partition == common.PartitionHorz ||
			partition == common.PartitionVert) {
			continue
		}
		partRate := encoder.PartitionRateCost(&probs, ctx, partition, hasRows, hasCols)
		refBestRD := uint64(^uint64(0))
		if bestValid {
			refRate := bestRate
			if partition == common.PartitionHorz || partition == common.PartitionVert {
				refRate -= partRate
			}
			refBestRD = encoder.RDCost(rdmult, encoder.RDDivBits, refRate, bestDistortion)
		}
		prevBestScore := bestScore
		rd, decision, ok := e.scoreVP9KeyframeRDPartitionLeaf(key, tile, miRows, miCols,
			miRow, miCol, cand, common.TxModeSelect, rdmult, refBestRD)
		if !ok {
			if partition == common.PartitionSplit {
				doRect = e.vp9KeyframeRDPartitionRectAllowedAfterSplit(bestDistortion)
			}
			continue
		}
		rate := rd.rate + partRate
		score := encoder.RDCost(rdmult, encoder.RDDivBits, rate, rd.distortion)
		if !bestValid || score < bestScore {
			bestSize = cand
			bestScore = score
			bestRate = rate
			bestDistortion = rd.distortion
			bestDecision = decision
			bestValid = true
		}
		if partition == common.PartitionSplit && score >= prevBestScore {
			doRect = e.vp9KeyframeRDPartitionRectAllowedAfterSplit(bestDistortion)
		}
	}
	e.restoreVP9PartitionReconSnapshot(reconSnap)
	if !bestValid {
		return common.BlockInvalid, false
	}
	if key.counts != nil {
		e.storeVP9LeafKeyframeDecision(miRow, miCol, bestSize, bestDecision)
	}
	return bestSize, true
}

func (e *VP9Encoder) vp9KeyframeRDPartitionRectAllowedAfterSplit(bestDistortion uint64) bool {
	if e.sf.LessRectangularCheck == 0 {
		return true
	}
	distBreakoutThr := e.sf.PartitionSearchBreakoutThr.Dist
	shift := 8 - (int(common.BWidthLog2Lookup[common.Block8x8]) +
		int(common.BHeightLog2Lookup[common.Block8x8]))
	if shift > 0 {
		distBreakoutThr >>= uint(shift)
	}
	if common.Block8x8 > e.sf.UseSquareOnlyThreshHigh {
		return false
	}
	if distBreakoutThr > 0 && bestDistortion < uint64(distBreakoutThr) {
		return false
	}
	return true
}

func (e *VP9Encoder) scoreVP9KeyframeRDPartitionLeaf(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode, rdmult int, refBestRD uint64,
) (vp9KeyframeIntraRD, vp9KeyframeModeDecision, bool) {
	if key == nil || bsize >= common.BlockSizes {
		return vp9KeyframeIntraRD{}, vp9KeyframeModeDecision{}, false
	}
	txSize := common.Tx4x4
	if bsize >= common.Block8x8 {
		txSize = common.MaxTxsizeLookup[bsize]
		if txMode < common.TxModes {
			txSize = min(txSize, common.TxModeToBiggestTxSize[txMode])
		}
		if key.lossless {
			txSize = common.Tx4x4
		}
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       txSize,
		RefFrame:     [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
		InterpFilter: uint8(vp9dec.SwitchableFilters),
	}
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	var yRD vp9KeyframeIntraRD
	var ok bool
	if bsize < common.Block8x8 {
		yRD, ok = e.pickVP9KeyframeSub8x8YMode(key, tile, miRows, miCols,
			miRow, miCol, bsize, &mi, refBestRD)
	} else {
		yRD, ok = e.pickVP9KeyframeYModeRD(key, tile, miRows, miCols,
			miRow, miCol, bsize, &mi, txMode, rdmult, refBestRD)
	}
	if !ok {
		e.cbRdmult = prevCbRdmult
		return vp9KeyframeIntraRD{}, vp9KeyframeModeDecision{}, false
	}
	uvRD, ok := e.pickVP9KeyframeUvModeRD(key, tile, miRows, miCols,
		miRow, miCol, bsize, &mi)
	e.cbRdmult = prevCbRdmult
	if !ok {
		return vp9KeyframeIntraRD{}, vp9KeyframeModeDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	rate := yRD.rate + uvRD.rate + encoder.VP9CostBit(skipProb, 0)
	if yRD.skippable && uvRD.skippable {
		rate = yRD.rate + uvRD.rate - yRD.rateTokenOnly - uvRD.rateTokenOnly +
			encoder.VP9CostBit(skipProb, 1)
	}
	decision := vp9KeyframeModeDecision{
		mode:   mi.Mode,
		bmi:    mi.Bmi,
		txSize: mi.TxSize,
		uvMode: uvRD.mode,
	}
	return vp9KeyframeIntraRD{
		mode:       mi.Mode,
		rate:       rate,
		distortion: yRD.distortion + uvRD.distortion,
		skippable:  yRD.skippable && uvRD.skippable,
	}, decision, true
}

func (e *VP9Encoder) vp9KeyframeRDRefinementEnabled() bool {
	if e == nil || !e.opts.RateControlModeSet || e.opts.RateControlMode != RateControlQ {
		return false
	}
	return e.opts.Width <= 128 && e.opts.Height <= 64
}

func (e *VP9Encoder) vp9KeyframeRDPartitionEnabled(key *vp9KeyframeEncodeState) bool {
	if key == nil || key.hdr == nil || key.dq == nil ||
		key.hdr.FrameType != common.KeyFrame || key.lossless {
		return false
	}
	return e.sf.UseNonrdPickMode == 0 && e.vp9KeyframeRDRefinementEnabled() &&
		e.sf.PartitionSearchType == VarBasedPartition
}

func vp9KeyframeSquareBlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize,
) common.BlockSize {
	maxW := min(miCols-miCol, int(common.Num8x8BlocksWideLookup[root]))
	maxH := min(miRows-miRow, int(common.Num8x8BlocksHighLookup[root]))
	if maxW >= 4 && maxH >= 4 {
		return common.Block32x32
	}
	if maxW >= 2 && maxH >= 2 {
		return common.Block16x16
	}
	if maxW >= 1 && maxH >= 1 {
		return common.Block8x8
	}
	return common.Block4x4
}

func vp9KeyframeEdgeBlockHasNonNeutralLuma(key *vp9KeyframeEncodeState,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
) bool {
	if key == nil || key.img == nil {
		return false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[root])
	blockMiH := int(common.Num8x8BlocksHighLookup[root])
	if miCol+blockMiW <= miCols && miRow+blockMiH <= miRows {
		return false
	}
	src, stride, width, height := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || stride <= 0 || width <= 0 || height <= 0 {
		return false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 >= width || y0 >= height {
		return false
	}
	w := min(width-x0, blockMiW*common.MiSize)
	h := min(height-y0, blockMiH*common.MiSize)
	for y := range h {
		row := src[(y0+y)*stride+x0 : (y0+y)*stride+x0+w]
		for _, px := range row {
			if px != 128 {
				return true
			}
		}
	}
	return false
}

func (e *VP9Encoder) pickVP9KeyframeVariancePartitionBlockSize(key *vp9KeyframeEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (common.BlockSize, bool) {
	if !e.vp9KeyframeVariancePartitionEnabled(key) {
		return common.BlockInvalid, false
	}
	// Phase C wiring: when the libvpx choose_partitioning gate is
	// enabled, populate the per-SB partition cache on first call into
	// this SB and read the partition decision back from
	// e.varPartGrid. Falls through to the legacy single-level picker
	// below when the gate is off (default) so existing trace
	// tests stay green.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:5470 nonrd_use_partition
	// reads xd->mi[]->sb_type to drive the encode walk.
	if e.vp9RealtimeVariancePartitionEnabled() &&
		e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, key, nil) {
		return e.vp9VarPartDecisionFor(miCols, miRow, miCol, bsize)
	}
	horzSize, vertSize, splitSize, ok := encoder.SquareInterPartitionSizes(bsize)
	if !ok || splitSize < common.Block8x8 {
		return common.BlockInvalid, false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[bsize])
	blockMiH := int(common.Num8x8BlocksHighLookup[bsize])
	if miCol+blockMiW > miCols || miRow+blockMiH > miRows {
		return common.BlockInvalid, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return common.BlockInvalid, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) {
		return common.BlockInvalid, false
	}
	threshold := vp9KeyframeVariancePartitionThreshold(key.dq.Y[0][1], bsize)
	variance := encoder.BlockSourceVariance128(src, srcStride, x0, y0, blockW, blockH)
	if bsize > common.Block32x32 || variance > threshold<<4 {
		return splitSize, true
	}
	if variance < threshold {
		return common.BlockInvalid, false
	}
	halfW := blockW >> 1
	halfH := blockH >> 1
	if miRow+(blockMiH>>1) < miRows {
		left := encoder.BlockSourceVariance128(src, srcStride, x0, y0, halfW, blockH)
		right := encoder.BlockSourceVariance128(src, srcStride,
			x0+halfW, y0, halfW, blockH)
		if left < threshold && right < threshold {
			return vertSize, true
		}
	}
	if miCol+(blockMiW>>1) < miCols {
		top := encoder.BlockSourceVariance128(src, srcStride, x0, y0, blockW, halfH)
		bottom := encoder.BlockSourceVariance128(src, srcStride,
			x0, y0+halfH, blockW, halfH)
		if top < threshold && bottom < threshold {
			return horzSize, true
		}
	}
	return splitSize, true
}

// vp9KeyframeVariancePartitionEnabled mirrors libvpx's keyframe
// choose_partitioning gate. A realtime speed can set
// sf->partition_search_type = VAR_BASED_PARTITION while still using the RD
// superblock row when sf->use_nonrd_pick_mode is false; that RD row only
// consumes choose_partitioning for non-keyframes. Keyframes reach
// choose_partitioning through the non-RD row.
//
// libvpx: vp9/encoder/vp9_encodeframe.c:4259-4260 and :5323-5328.
func (e *VP9Encoder) vp9KeyframeVariancePartitionEnabled(key *vp9KeyframeEncodeState) bool {
	return key != nil && key.dq != nil && key.hdr != nil &&
		key.hdr.FrameType == common.KeyFrame && !key.lossless &&
		e.rc.enabled && e.sf.UseNonrdPickMode != 0 &&
		e.vp9RealtimeVariancePartitionEnabled()
}

func vp9KeyframeVariancePartitionThreshold(yAcDequant int16, bsize common.BlockSize) uint64 {
	if yAcDequant <= 0 {
		return 0
	}
	base := uint64(yAcDequant) * 20
	switch bsize {
	case common.Block64x64:
		return base
	case common.Block32x32, common.Block16x16:
		return base >> 2
	default:
		return base << 2
	}
}
