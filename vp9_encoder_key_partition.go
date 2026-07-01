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
	skippable  bool
}

// pickVP9KeyframeRDPartitionBlockSize mirrors libvpx's keyframe
// rd_pick_partition dispatch. Even when speed features set
// VAR_BASED_PARTITION, keyframes do not enter choose_partitioning unless the
// non-RD path is active; they fall through to the RD picker.
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
	distBreakoutThr, rateBreakoutThr := e.vp9KeyframeRDPartitionBreakoutThresholds(root)
	doRect := true
	type replayState struct {
		recon     vp9PartitionReconSnapshot
		ctx       vp9PartitionContextSnapshot
		mi        [64]vp9dec.NeighborMi
		miRows    int
		miCols    int
		cache     vp9KeyframeDecisionRegionSnapshot
		haveCache bool
		ok        bool
	}
	var bestReplay replayState
	releaseBestReplay := func() {
		if bestReplay.ok {
			e.releaseVP9PartitionReconSnapshot(bestReplay.recon)
			bestReplay.ok = false
		}
	}
	defer releaseBestReplay()
	saveBestReplay := func() bool {
		releaseBestReplay()
		recon, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, root)
		if !ok {
			return false
		}
		ctx, ctxOK := e.snapshotVP9PartitionContexts(miRow, miCol, root)
		rows, cols, miSnapOK := e.snapshotVP9MiRect(miRows, miCols,
			miRow, miCol, int(common.Num8x8BlocksHighLookup[root]),
			int(common.Num8x8BlocksWideLookup[root]), bestReplay.mi[:])
		if !ctxOK || !miSnapOK {
			e.releaseVP9PartitionReconSnapshot(recon)
			return false
		}
		var cache vp9KeyframeDecisionRegionSnapshot
		haveCache := false
		if store {
			if !e.snapshotVP9KeyframeDecisionRegion(miRows, miCols,
				miRow, miCol, root, &cache) {
				e.releaseVP9PartitionReconSnapshot(recon)
				return false
			}
			haveCache = true
		}
		bestReplay.recon = recon
		bestReplay.ctx = ctx
		bestReplay.miRows = rows
		bestReplay.miCols = cols
		bestReplay.cache = cache
		bestReplay.haveCache = haveCache
		bestReplay.ok = true
		return true
	}
	restoreBestReplay := func() bool {
		if !bestReplay.ok {
			return false
		}
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol,
			bestReplay.miRows, bestReplay.miCols, bestReplay.mi[:])
		e.restoreVP9PartitionContexts(bestReplay.ctx)
		e.restoreVP9PartitionReconSnapshotPixels(bestReplay.recon)
		if bestReplay.haveCache {
			e.restoreVP9KeyframeDecisionRegion(bestReplay.cache)
		}
		return true
	}
	consider := func(partition common.PartitionType,
		refBestRD uint64, score func(uint64, bool, bool) (vp9KeyframePartitionRD, bool),
	) (vp9KeyframePartitionRD, bool, bool) {
		restoreBase()
		scoreStore := false
		var cacheBefore vp9KeyframeDecisionRegionSnapshot
		if store && e.snapshotVP9KeyframeDecisionRegion(miRows, miCols,
			miRow, miCol, root, &cacheBefore) {
			scoreStore = true
		}
		rd, ok := score(refBestRD, true, scoreStore)
		if scoreStore {
			defer e.restoreVP9KeyframeDecisionRegion(cacheBefore)
		}
		if !ok {
			return vp9KeyframePartitionRD{}, false, false
		}
		rd.partition = partition
		rd.target = common.SubsizeLookup[partition][root]
		if partition == common.PartitionNone {
			rd.target = root
		}
		// libvpx rd_pick_partition adds cpi->partition_cost[pl][partition], the
		// unconditional full-tree cost (vp9_encodeframe.c:3826/3969/4035/4085),
		// not the writer's hasRows/hasCols-clamped form. See
		// vp9_fullrd_partition_cost.go.
		rd.rate += RDPartitionCost(partitionProbs, ctx, partition)
		rd.score = encoder.RDCost(rdmult, encoder.RDDivBits, rd.rate,
			rd.distortion)
		improved := !bestSet || rd.score < best.score
		if improved {
			best = rd
			bestSet = true
			if !saveBestReplay() {
				bestReplay.ok = false
			}
		}
		return rd, true, improved
	}
	if noneAllowed {
		noneRD, noneOK, noneImproved := consider(common.PartitionNone, bestRD,
			func(refBestRD uint64, apply, store bool) (vp9KeyframePartitionRD, bool) {
				rd, ok := e.scoreVP9KeyframeRDPartitionLeafForTree(key, tile,
					miRows, miCols, miRow, miCol, root, txMode, refBestRD, apply, store)
				if ok && apply {
					e.updateVP9PartitionContextForChoice(miRow, miCol, root,
						common.PartitionNone, root)
				}
				return rd, ok
			})
		if noneOK && noneImproved && (doSplit || doRect) &&
			e.vp9KeyframeRDPartitionNoneBreakout(root, noneRD, key.lossless) {
			doSplit = false
			doRect = false
		}
	}
	if doSplit {
		splitBestRD := bestRD
		if bestSet {
			splitBestRD = best.score
		}
		splitRD, splitOK, splitImproved := consider(common.PartitionSplit, splitBestRD,
			func(refBestRD uint64, apply, store bool) (vp9KeyframePartitionRD, bool) {
				return e.scoreVP9KeyframeRDPartitionSplit(key, tile, partitionProbs,
					miRows, miCols, miRow, miCol, root, splitSize, txMode,
					refBestRD, apply, store)
			})
		if splitOK {
			if splitImproved {
				if best.distortion < distBreakoutThr>>2 ||
					(best.distortion < distBreakoutThr &&
						best.rate < rateBreakoutThr) {
					doRect = false
				}
			} else if e.sf.LessRectangularCheck != 0 &&
				(root > e.sf.UseSquareOnlyThreshHigh ||
					best.distortion < distBreakoutThr) {
				doRect = e.vp9KeyframeRDRectAllowedAfterSplitMiss(root,
					noneAllowed, doRect)
			}
			_ = splitRD
		}
	}
	if horzAllowed && (doRect || vp9ActiveHEdge(miRow, bs, miRows)) {
		partRate := RDPartitionCost(partitionProbs, ctx, common.PartitionHorz)
		consider(common.PartitionHorz, e.vp9KeyframeRDPartitionRectBestRD(
			rdmult, bestRD, best, bestSet, partRate),
			func(refBestRD uint64, apply, store bool) (vp9KeyframePartitionRD, bool) {
				return e.scoreVP9KeyframeRDPartitionRect(key, tile, miRows, miCols,
					miRow, miCol, root, horzSize, common.PartitionHorz, bs, 0,
					txMode, refBestRD, apply, store)
			})
		if best.partition == common.PartitionHorz &&
			e.sf.LessRectangularCheck != 0 &&
			root > e.sf.UseSquareOnlyThreshHigh {
			doRect = false
		}
	}
	if vertAllowed && (doRect || vp9ActiveVEdge(miCol, bs, miCols)) {
		partRate := RDPartitionCost(partitionProbs, ctx, common.PartitionVert)
		consider(common.PartitionVert, e.vp9KeyframeRDPartitionRectBestRD(
			rdmult, bestRD, best, bestSet, partRate),
			func(refBestRD uint64, apply, store bool) (vp9KeyframePartitionRD, bool) {
				return e.scoreVP9KeyframeRDPartitionRect(key, tile, miRows, miCols,
					miRow, miCol, root, vertSize, common.PartitionVert, 0, bs,
					txMode, refBestRD, apply, store)
			})
	}
	if !bestSet {
		restoreBase()
		e.partitionReconScratchTop = reconSnap.top
		return vp9KeyframePartitionRD{}, false
	}
	if !apply {
		restoreBase()
		releaseBestReplay()
		e.partitionReconScratchTop = reconSnap.top
		return best, true
	}

	if restoreBestReplay() {
		if store {
			e.storeVP9KeyframePartitionDecision(miRow, miCol, root, best.target)
		}
		out := best
		releaseBestReplay()
		e.partitionReconScratchTop = reconSnap.top
		return out, true
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
	committed.rate += RDPartitionCost(partitionProbs, ctx, best.partition)
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
		out.skippable = rd.skippable
	} else {
		rdmult := e.vp9KeyframePartitionRDMul(miRow, miCol, root)
		stepMi := int(common.Num8x8BlocksWideLookup[child])
		haveChild := false
		for rowOff := 0; rowOff <= stepMi; rowOff += stepMi {
			for colOff := 0; colOff <= stepMi; colOff += stepMi {
				if miRow+rowOff >= miRows || miCol+colOff >= miCols {
					continue
				}
				childBestRD := bestRD
				if bestRD != ^uint64(0) {
					usedRD := encoder.RDCost(rdmult, encoder.RDDivBits,
						out.rate, out.distortion)
					if usedRD >= bestRD {
						return vp9KeyframePartitionRD{}, false
					}
					childBestRD = bestRD - usedRD
				}
				rd, ok := e.scoreVP9KeyframeRDPartitionTree(key, tile,
					partitionProbs, miRows, miCols, miRow+rowOff,
					miCol+colOff, child, txMode, childBestRD, true, store)
				if !ok {
					return vp9KeyframePartitionRD{}, false
				}
				out.rate += rd.rate
				out.distortion += rd.distortion
				if !haveChild {
					out.skippable = rd.skippable
					haveChild = true
				} else {
					out.skippable = out.skippable && rd.skippable
				}
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
		skippable:  first.skippable,
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
			out.skippable = out.skippable && second.skippable
		}
	}
	if apply {
		e.updateVP9PartitionContextForChoice(miRow, miCol, root, partition, child)
	}
	return out, true
}

// vp9ActiveHEdge mirrors libvpx vp9_active_h_edge
// (vp9/encoder/vp9_rdopt.c:3375). It reports whether the [mi_row,
// mi_row+mi_step) span straddles the top or bottom image edge so that
// rd_pick_partition keeps PARTITION_HORZ live even after the square-vs-split
// breakout has cleared do_rect (vp9_encodeframe.c:4034). The two-pass
// inactive-zone bar adjustment (oxcf.pass == 2) is omitted here because the RD
// keyframe partition path only runs in the one-pass deadlines, where
// top_edge/bottom_edge keep their image-boundary defaults.
func vp9ActiveHEdge(miRow, miStep, miRows int) bool {
	topEdge := 0
	bottomEdge := miRows
	if (topEdge >= miRow && topEdge < miRow+miStep) ||
		(bottomEdge >= miRow && bottomEdge < miRow+miStep) {
		return true
	}
	return false
}

// vp9ActiveVEdge mirrors libvpx vp9_active_v_edge
// (vp9/encoder/vp9_rdopt.c:3403); see vp9ActiveHEdge for the one-pass caveat.
// It keeps PARTITION_VERT live at the left/right image edges
// (vp9_encodeframe.c:4084).
func vp9ActiveVEdge(miCol, miStep, miCols int) bool {
	leftEdge := 0
	rightEdge := miCols
	if (leftEdge >= miCol && leftEdge < miCol+miStep) ||
		(rightEdge >= miCol && rightEdge < miCol+miStep) {
		return true
	}
	return false
}

func (e *VP9Encoder) vp9KeyframeRDPartitionBreakoutThresholds(root common.BlockSize) (uint64, int) {
	distBreakoutThr := e.sf.PartitionSearchBreakoutThr.Dist
	rateBreakoutThr := e.sf.PartitionSearchBreakoutThr.Rate
	if root < common.BlockSizes {
		shift := 8 - (int(common.BWidthLog2Lookup[root]) +
			int(common.BHeightLog2Lookup[root]))
		if shift > 0 {
			distBreakoutThr >>= uint(shift)
		}
		rateBreakoutThr *= int(common.NumPelsLog2Lookup[root])
	}
	return uint64(distBreakoutThr), rateBreakoutThr
}

func (e *VP9Encoder) vp9KeyframeRDPartitionNoneBreakout(root common.BlockSize,
	rd vp9KeyframePartitionRD, lossless bool,
) bool {
	if lossless || !rd.skippable {
		return false
	}
	distBreakoutThr, rateBreakoutThr := e.vp9KeyframeRDPartitionBreakoutThresholds(root)
	return rd.distortion < distBreakoutThr>>2 ||
		(rd.distortion < distBreakoutThr && rd.rate < rateBreakoutThr)
}

func (e *VP9Encoder) vp9KeyframeRDRectAllowedAfterSplitMiss(root common.BlockSize,
	noneAllowed, doRect bool,
) bool {
	if !doRect {
		return false
	}
	if !noneAllowed {
		return true
	}
	// Keep sub-8 rectangular candidates reachable for the fixed-Q RD keyframe
	// refinement path. libvpx's full RD_COST state can keep these candidates
	// live at BLOCK_8X8 boundaries; govpx's scalar scorer otherwise prunes
	// them early and loses byte parity on forced keyframes.
	return root == common.Block8x8
}

func (e *VP9Encoder) vp9KeyframeRDPartitionRectBestRD(rdmult int,
	fallback uint64, best vp9KeyframePartitionRD, bestSet bool, partRate int,
) uint64 {
	if !bestSet {
		return fallback
	}
	rate := max(best.rate-partRate, 0)
	return encoder.RDCost(rdmult, encoder.RDDivBits, rate, best.distortion)
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
		skippable:  rd.skippable,
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
		// Unconditional full-tree partition cost (cpi->partition_cost[pl][type]),
		// matching libvpx rd_pick_partition. See vp9_fullrd_partition_cost.go.
		partRate := RDPartitionCost(&probs, ctx, partition)
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
	if e == nil {
		return false
	}
	if vp9ResolveDeadlineMode(e.opts.Deadline) == vp9ModeBest {
		return true
	}
	if !e.opts.RateControlModeSet || e.opts.RateControlMode != RateControlQ {
		return false
	}
	return e.opts.Width <= 128 && e.opts.Height <= 64
}

func (e *VP9Encoder) vp9KeyframeRDPartitionEnabled(key *vp9KeyframeEncodeState) bool {
	if key == nil || key.hdr == nil || key.dq == nil ||
		key.hdr.FrameType != common.KeyFrame || e.sf.UseNonrdPickMode != 0 {
		return false
	}
	return e.sf.PartitionSearchType != FixedPartition
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
	if e.vp9KeyframeChoosePartitioningEnabled(key) &&
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
// choose_partitioning gate. A realtime speed can set either
// VAR_BASED_PARTITION or REFERENCE_PARTITION; both non-RD rows call
// choose_partitioning before nonrd_use_partition for keyframes.
//
// libvpx: vp9/encoder/vp9_encodeframe.c:5304-5357.
func (e *VP9Encoder) vp9KeyframeVariancePartitionEnabled(key *vp9KeyframeEncodeState) bool {
	return key != nil && key.dq != nil && key.hdr != nil &&
		key.hdr.FrameType == common.KeyFrame && !key.lossless &&
		e.sf.UseNonrdPickMode != 0 &&
		e.vp9KeyframeChoosePartitioningEnabled(key)
}

func (e *VP9Encoder) vp9KeyframeChoosePartitioningEnabled(key *vp9KeyframeEncodeState) bool {
	if e == nil || key == nil || key.dq == nil || key.hdr == nil ||
		key.hdr.FrameType != common.KeyFrame || key.lossless ||
		e.sf.UseNonrdPickMode == 0 {
		return false
	}
	return e.sf.PartitionSearchType == VarBasedPartition ||
		e.sf.PartitionSearchType == ReferencePartition
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
