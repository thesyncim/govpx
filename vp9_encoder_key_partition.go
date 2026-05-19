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
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, width, height) {
		return common.BlockInvalid, false
	}
	variance := vp9BlockSourceVariance128(src, stride, x0, y0, blockW, blockH)
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
	rdmult := vp9KeyframeRDMul(qindex)
	rdmult = e.getVP9TPLRDMultDelta(miRow, miCol, 1, 1, rdmult)
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
		partRate := vp9PartitionRateCost(&probs, ctx, partition, hasRows, hasCols)
		refBestRD := uint64(^uint64(0))
		if bestValid {
			refRate := bestRate
			if partition == common.PartitionHorz || partition == common.PartitionVert {
				refRate -= partRate
			}
			refBestRD = vp9RDCost(rdmult, vp9RDDivBits, refRate, bestDistortion)
		}
		prevBestScore := bestScore
		rd, decision, ok := e.scoreVP9KeyframeRDPartitionLeaf(key, tile, miRows, miCols,
			miRow, miCol, cand, rdmult, refBestRD)
		if !ok {
			if partition == common.PartitionSplit {
				doRect = e.vp9KeyframeRDPartitionRectAllowedAfterSplit(bestDistortion)
			}
			continue
		}
		rate := rd.rate + partRate
		score := vp9RDCost(rdmult, vp9RDDivBits, rate, rd.distortion)
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
	bsize common.BlockSize, rdmult int, refBestRD uint64,
) (vp9KeyframeIntraRD, vp9KeyframeModeDecision, bool) {
	if key == nil || bsize > common.Block8x8 {
		return vp9KeyframeIntraRD{}, vp9KeyframeModeDecision{}, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       common.Tx4x4,
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
			miRow, miCol, bsize, &mi, common.TxModeSelect, rdmult, refBestRD)
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
	if !e.vp9CBRKeyframeVariancePartitionEnabled(key) {
		return common.BlockInvalid, false
	}
	// Phase C wiring: when the libvpx choose_partitioning gate is
	// enabled, populate the per-SB partition cache on first call into
	// this SB and read the partition decision back from
	// e.varPartGrid. Falls through to the legacy single-level picker
	// below when the gate is off (default) so existing scoreboard
	// tests stay green.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:5470 nonrd_use_partition
	// reads xd->mi[]->sb_type to drive the encode walk.
	if e.vp9RealtimeVariancePartitionEnabled() &&
		e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, key, nil) {
		return e.vp9VarPartDecisionFor(miCols, miRow, miCol, bsize)
	}
	horzSize, vertSize, splitSize, ok := vp9SquareInterPartitionSizes(bsize)
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
	if !vp9VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) {
		return common.BlockInvalid, false
	}
	threshold := vp9KeyframeVariancePartitionThreshold(key.dq.Y[0][1], bsize)
	variance := vp9BlockSourceVariance128(src, srcStride, x0, y0, blockW, blockH)
	if bsize > common.Block32x32 || variance > threshold<<4 {
		return splitSize, true
	}
	if variance < threshold {
		return common.BlockInvalid, false
	}
	halfW := blockW >> 1
	halfH := blockH >> 1
	if miRow+(blockMiH>>1) < miRows {
		left := vp9BlockSourceVariance128(src, srcStride, x0, y0, halfW, blockH)
		right := vp9BlockSourceVariance128(src, srcStride,
			x0+halfW, y0, halfW, blockH)
		if left < threshold && right < threshold {
			return vertSize, true
		}
	}
	if miCol+(blockMiW>>1) < miCols {
		top := vp9BlockSourceVariance128(src, srcStride, x0, y0, blockW, halfH)
		bottom := vp9BlockSourceVariance128(src, srcStride,
			x0, y0+halfH, blockW, halfH)
		if top < threshold && bottom < threshold {
			return horzSize, true
		}
	}
	return splitSize, true
}

// vp9CBRKeyframeVariancePartitionEnabled mirrors libvpx's
// vp9_set_variance_partition_thresholds / choose_partitioning enablement for
// keyframes at speed >= 6. libvpx unconditionally sets
// `sf->partition_search_type = VAR_BASED_PARTITION` at speed 6+
// (vp9/encoder/vp9_speed_features.c:667) and at speed 4 keyframe path
// (vp9_speed_features.c:582). The gate is NOT rc_mode-specific, NOT gated on
// drop-frame-allowed, and NOT gated on a fixed public quantizer; libvpx fires
// choose_partitioning at every keyframe whose `partition_search_type` is
// VAR_BASED_PARTITION regardless of VPX_CBR / VPX_VBR / VPX_CQ / VPX_Q
// (vp9_encodeframe.c:5304-5311 dispatches on partition_search_type alone).
//
// libvpx: vp9/encoder/vp9_speed_features.c:582 / :667, vp9_encodeframe.c:5304.
func (e *VP9Encoder) vp9CBRKeyframeVariancePartitionEnabled(key *vp9KeyframeEncodeState) bool {
	return key != nil && key.dq != nil && key.hdr != nil &&
		key.hdr.FrameType == common.KeyFrame && !key.lossless &&
		e.rc.enabled && e.vp9RealtimeVariancePartitionEnabled()
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
