package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9InterIntraDecision struct {
	mode       common.PredictionMode
	uvMode     common.PredictionMode
	txSize     common.TxSize
	rate       int
	score      uint64
	skip       bool
	skipTxfm   encoder.SkipTxfmFlag
	predData   []byte
	predStride int
	predX      int
	predY      int
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
	ref, refStride := e.vp9EncoderReconPlane(0)
	return e.vp9NoReferenceIntraResidualStatsScratchLiveNoRestore(key, mode,
		txSize, tile, miRows, miCols, miRow, miCol, bsize,
		scratch, scratchStride, originMiRow, originMiCol, ref, refStride)
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
			stats, ok := encoder.BlockDiffStatsClampedSource(src, srcStride,
				srcW, srcH, dst, dstStride, x0, y0, 0, 0, bs, bs)
			if !ok {
				predOK = false
				break residualLoop
			}
			sse += stats.SSE
			sum += stats.Sum
			count += stats.Count
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

func (e *VP9Encoder) vp9NoReferenceIntraResidualStatsScratchLiveNoRestore(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, txSize common.TxSize, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	scratch []byte, scratchStride, originMiRow, originMiCol int,
	ref []byte, refStride int,
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
	if len(src) == 0 || srcStride <= 0 || len(scratch) == 0 ||
		scratchStride <= 0 || len(ref) == 0 || refStride <= 0 {
		return 0, 0, false
	}
	max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
		miRow, miCol, bsize, pd, planeBsize)
	step := 1 << uint(txSize)
	bs := 4 << uint(txSize)
	originX := originMiCol * common.MiSize
	originY := originMiRow * common.MiSize
	var sum int64
	var count uint64
	for rr := 0; rr < max4x4H; rr += step {
		for cc := 0; cc < max4x4W; cc += step {
			dst, dstStride, x0, y0, ok := e.predictVP9KeyframeTxScratchLive(
				key.hdr, pd, mode, txSize, tile, miRows, miCols,
				miRow, miCol, bsize, rr, cc,
				scratch, scratchStride, originX, originY, ref, refStride)
			if !ok {
				return 0, 0, false
			}
			stats, ok := encoder.BlockDiffStatsClampedSource(src, srcStride,
				srcW, srcH, dst, dstStride, x0, y0, 0, 0, bs, bs)
			if !ok {
				return 0, 0, false
			}
			sse += stats.SSE
			sum += stats.Sum
			count += stats.Count
		}
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
			stats, ok := encoder.BlockDiffStatsClampedSource(src, srcStride,
				srcW, srcH, dst, dstStride, x0, y0, 0, 0, bs, bs)
			if !ok {
				predOK = false
				break residualLoop
			}
			sse += stats.SSE
			sum += stats.Sum
			count += stats.Count
		}
	}
	if restore {
		encoder.RestorePlaneRect(planeData, stride, baseX, baseY, restoreW, restoreH, saved)
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
	// libvpx scores the Y intra mode inside vp9_rd_pick_inter_mode_sb with
	// cpi->mbmode_cost[mi->mode] = cost_tokens(fc->y_mode_prob[1])
	// (vp9_rdopt.c:3864; table built at vp9_rd.c:103). The size-group index is
	// the literal constant 1 — NOT size_group_lookup[bsize], which only drives
	// the bitstream writer (write_intra_mode). Keying the RD cost on the
	// per-bsize size group diverged from libvpx for every block in size group
	// != 1 (BLOCK_16X16 and larger). See vp9_fullrd_intra.go.
	var yModeCosts [common.IntraModes]int
	vp9FullRDInterIntraYModeCosts(yModeCosts[:], &inter.selectFc)

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
	// libvpx UV intra rate is intra_uv_mode_cost[INTER_FRAME][y_mode][uv_mode]
	// = cost_tokens(fc->uv_mode_prob[y_mode]) (vp9_rd.c:107-108, consumed at
	// vp9_rdopt.c:1496). See vp9_fullrd_intra.go.
	var uvModeCosts [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(uvModeCosts[:], vp9FullRDInterFrame, yMode, fc)
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
	skip           bool
	skipTxfm       encoder.SkipTxfmFlag
	rdModeIndex    encoder.ThrMode
	rdModeValid    bool
}

type vp9InterMvPredState struct {
	seed    vp9dec.MV
	predSad uint64
	valid   bool
}

type vp9FullRDRefState struct {
	mvPredState vp9InterMvPredState
	modeSkip    int
	skipNewMv   bool
}

const vp9InterNearestNearZeroMask = (1 << uint(common.NearestMv)) |
	(1 << uint(common.NearMv)) |
	(1 << uint(common.ZeroMv))

var vp9SingleRefInterModeOrder = [...]common.PredictionMode{
	common.NearestMv,
	common.NewMv,
	common.NearMv,
	common.ZeroMv,
}

var vp9CompoundInterModeOrder = [...]common.PredictionMode{
	common.ZeroMv,
	common.NearestMv,
	common.NearMv,
	common.NewMv,
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

// vp9InterPartitionRDDecisionEntry stores the partition (child block size)
// chosen by the depth-first full-RD inter search (pickVP9InterPartitionRD) at
// one tree node, keyed by (version, root). It is the partition-tree half of the
// SEARCH->WRITE replay (the leaf-mode half is vp9LeafInterRDDecisionEntry).
//
// It exists because pickVP9InterPartitionRD's per-node decision and the writer's
// own per-node partition picker (pickVP9InterPartitionBlockSize) do NOT always
// agree: the latter carries early-exits the recursion lacks (e.g. the
// full.distortion==0 PARTITION_NONE shortcut at
// vp9_encoder_inter_partition.go:132), so on perfectly-predicted planted motion
// the writer would descend NONE while the search committed VERT — leaving the
// writer reading the leaf cache at a block size the search never wrote. Caching
// the search's per-node partition and having the writer descend THAT tree keeps
// the partition geometry and the leaf-mode keys in lock-step.
//
// Populated and consumed ONLY when vp9InterUseDeepRDPartition is active;
// production (flag off) never allocates or reads it, so the flag-off path stays
// byte-identical. Mirrors vp9KeyframePartitionDecisionEntry.
//
// libvpx: rd_pick_partition writes the chosen partition into the PC_TREE /
// mi_grid once per SB; the writer replays that tree at write_modes_sb
// (vp9/encoder/vp9_bitstream.c) rather than re-deciding it.
type vp9InterPartitionRDDecisionEntry struct {
	version uint32
	root    common.BlockSize
	target  common.BlockSize
	valid   bool
}

// vp9LeafInterRDDecisionEntry stores one committed leaf decision produced by
// the depth-first full-RD inter partition search (pickVP9InterPartitionRD).
// It is the SEARCH->WRITE replay surface for the deep recursion: the search
// commits each chosen leaf's full vp9InterModeDecision here as it fills the mi
// grid (scoreVP9InterPartitionLeaf), and the bitstream write descent
// (prepareVP9InterPredictionBlock) reads the cached decision back for that mi
// position + block size instead of re-running pickVP9InterReferenceMode with a
// different x->pred_mv / interp-filter context than the search ran. This is the
// govpx analog of libvpx running rd_pick_partition once per SB and replaying
// the cached per-leaf mbmi at write_modes_b time (vp9/encoder/vp9_bitstream.c)
// rather than re-picking.
//
// The cache is populated and consumed ONLY when vp9InterUseDeepRDPartition is
// active (an opt-in serialization/test flag, default false); production encodes
// (flag off) never touch it, so the flag-off path stays byte-identical. Keyed
// by (version, bsize): the version stamp invalidates stale cross-frame entries
// and the bsize discriminator guards a re-entry at a different block size than
// the prior commit (e.g. the search's NONE-arm 64x64 leaf vs the SPLIT-arm
// 32x32 leaf sharing the same top-left mi position).
type vp9LeafInterRDDecisionEntry struct {
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
	collectFilterRD := e.vp9ShouldCollectInterFilterRD(inter, useNonrd)
	var filterRDScores [vp9dec.SwitchableFilterContexts]uint64
	var filterRDScoresPtr *[vp9dec.SwitchableFilterContexts]uint64
	if collectFilterRD {
		vp9InitFilterRDScores(&filterRDScores)
		filterRDScoresPtr = &filterRDScores
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	// libvpx vp9_encodeframe.c:5347-5358 — REFERENCE_PARTITION still calls
	// choose_partitioning() before nonrd_select_partition() to stamp
	// x->variance_low (and x->color_sensitivity via chroma_check) for the
	// downstream nonrd picker even though the partition tree comes from the
	// reference picker. libvpx restricts this choose_partitioning prepass to
	// the VAR_BASED_PARTITION (vp9_encodeframe.c:5309) and REFERENCE_PARTITION
	// (vp9_encodeframe.c:5348) cases only — the ML_BASED_PARTITION
	// (vp9_encodeframe.c:5314, get_estimated_pred + nonrd_pick_partition) and
	// FIXED_PARTITION (vp9_encodeframe.c:5324, set_fixed_partitioning) cases
	// never call choose_partitioning, so x->color_sensitivity stays at the
	// per-SB reset value [0,0] (vp9_encodeframe.c:5245-5246). Gate this extra
	// stamping pass on REFERENCE_PARTITION specifically (the VAR_BASED case
	// stamps through its own dispatch); the previous `!VarBased` test wrongly
	// fired it for ML_BASED_PARTITION (cpu_used=8, w*h <= 352*288), inflating
	// the nonrd inter candidate's RD with a spurious UV chroma term and
	// flipping inter blocks to intra.
	if inter != nil && e.sf.ShortCircuitLowTempVar != 0 &&
		e.sf.PartitionSearchType == ReferencePartition {
		e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, nil, inter)
	}

	// libvpx restricts usable_ref_frame at speed >= 8 to LAST_FRAME for
	// the steady-state inter-block hot path: frames_since_golden > 120
	// or low last_sb_high_content triggers
	// `usable_ref_frame = LAST_FRAME` and skips GOLDEN/ALTREF
	// reference-mode picking entirely. Additionally
	// sf.short_circuit_low_temp_var (3 at speed 8 CBR non-screen) short-
	// circuits non-LAST refs on low-temporal-variance blocks via
	// force_skip_low_temp_var. govpx caches libvpx's per-SB variance_low
	// map from choose_partitioning and applies the LAST-only fan only when
	// that map is stamped for the block (choose_partitioning is called for
	// REFERENCE_PARTITION too — vp9_encodeframe.c:5348). When the cache is
	// still cold, keep the threaded warm-path LAST-only fallback. Frames that
	// explicitly mask out LAST (e.g.
	// EncodeNoReferenceLast for altref-only inter) must keep the full ref set
	// so a fallback ref can still be picked.
	// libvpx: vp9/encoder/vp9_pickmode.c:1962-1985 (usable_ref_frame),
	// vp9_speed_features.c:774 (ShortCircuitLowTempVar = 3 at speed 8
	// CBR non-screen).
	refFramesAll := [...]int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame}
	refFrames := refFramesAll[:]
	forceSkipLowTempVar, forceSkipLowTempKnown :=
		e.vp9VarPartForceSkipLowTempVarOK(miCols, miRow, miCol, bsize)
	if !forceSkipLowTempKnown && e.sf.ShortCircuitLowTempVar >= 1 {
		sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, miRow&^7, miCol&^7)
		if sbIdx < 0 || sbIdx >= len(e.varPartSBComputed) ||
			!e.varPartSBComputed[sbIdx] {
			forceSkipLowTempVar = true
		}
	}
	if encoder.NonrdForceLastReference(e.sf.ShortCircuitLowTempVar,
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
	var fullRDRefs [vp9dec.MaxRefFrames]vp9FullRDRefState
	if !useNonrd && bsize >= common.Block8x8 && len(refFrameSet) > 0 {
		fullRDRefs = e.vp9FullRDRefStates(inter, tile, miRows, miCols,
			miRow, miCol, bsize, refFrameSet)
	}
	// useNonrd: route through vp9_pick_inter_mode_nonrd.go. When
	// choose_partitioning stamped variance_low for this leaf, mirror libvpx
	// by clamping usable refs to LAST for that block only (via refMask +
	// maxUsableRef inside the nonrd picker). Do not assume low-variance
	// when the cache is unset.
	//
	// libvpx: vp9_pickmode.c:1696 vp9_pick_inter_mode.
	if useNonrd {
		savedRefMask := inter.refMask
		blockForceSkip, blockForceSkipKnown :=
			e.vp9VarPartForceSkipLowTempVarOK(miCols, miRow, miCol, bsize)
		narrowLastOnly := false
		if blockForceSkipKnown {
			narrowLastOnly = encoder.NonrdForceLastReference(
				e.sf.ShortCircuitLowTempVar, e.sf.UseNonrdPickMode != 0,
				blockForceSkip)
		} else if e.sf.ShortCircuitLowTempVar >= 1 {
			sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, miRow&^7, miCol&^7)
			if sbIdx < 0 || sbIdx >= len(e.varPartSBComputed) ||
				!e.varPartSBComputed[sbIdx] {
				narrowLastOnly = true
			}
		}
		if narrowLastOnly {
			if _, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame); ok {
				inter.refMask &= 1 << uint(vp9dec.LastFrame)
			}
		}
		decision, ok := e.pickVP9InterReferenceModeNonRD(inter, tile,
			miRows, miCols, miRow, miCol, bsize)
		inter.refMask = savedRefMask
		if ok {
			best = decision
			bestSet = true
		}
	} else if len(refFrameSet) > 0 {
		if bsize >= common.Block8x8 {
			best, bestSet = e.pickVP9FullRDInterReferenceMode(inter, tile,
				miRows, miCols, miRow, miCol, bsize, refFrameSet,
				fullRDRefs, sourceAltRefOverlay, filterRDScoresPtr)
		} else {
			for _, refFrame := range refFrameSet {
				refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
				if !ok {
					continue
				}
				inter.ref = &e.refFrames[refSlot]
				refRate := encoder.SingleRefModeRateCost(&inter.selectFc, above, left,
					inter.referenceMode, inter.compoundRefs, refFrame)
				bestScoreSoFar := uint64(0)
				if bestSet {
					bestScoreSoFar = best.score
				}
				decision, ok := e.pickVP9InterMode(inter, tile, miRows, miCols,
					miRow, miCol, bsize, refFrame, refRate,
					fullRDRefs[refFrame], bestScoreSoFar, bestSet,
					filterRDScoresPtr)
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
	}
	// SPEED_FEATURES.use_compound_nonrd_pickmode gates the compound branch
	// when UseNonrdPickMode is on (cpu_used >= 7 in libvpx realtime). The
	// nonrd_pickmode entry skips compound entirely when the feature is 0.
	//
	// libvpx: vp9/encoder/vp9_speed_features.c:469 / 656 / 665,
	// vp9/encoder/vp9_pickmode.c:1989.
	finishFullRD := func() {
		if useNonrd || !bestSet || e.rc.isSrcFrameAltRef ||
			bsize < common.Block8x8 {
			return
		}
		modeIndex := best.rdModeIndex
		if !best.rdModeValid {
			var ok bool
			modeIndex, ok = encoder.FullRDModeIndex(best.mode,
				best.refFrame, best.secondRefFrame)
			if !ok {
				return
			}
		}
		e.rdThresh.UpdateFullRDThreshFact(bsize, modeIndex,
			e.sf.AdaptiveRdThresh)
	}
	correctFullRDNewMV := func() {
		if useNonrd || !bestSet || best.mode != common.NewMv ||
			bsize < common.Block8x8 {
			return
		}
		refs := [2]int8{best.refFrame, best.secondRefFrame}
		compound := best.secondRefFrame > vp9dec.IntraFrame
		refCount := 1
		if compound {
			refCount = 2
		}
		var nearest, near [2]vp9dec.MV
		var nearestValid, nearValid [2]bool
		for ref := range refCount {
			nearest[ref], nearestValid[ref] = e.vp9EncoderInterModeCandidateMv(
				tile, miRows, miCols, miRow, miCol, bsize, common.NearestMv,
				refs[ref], inter.allowHP, inter.refSignBias)
			near[ref], nearValid[ref] = e.vp9EncoderInterModeCandidateMv(
				tile, miRows, miCols, miRow, miCol, bsize, common.NearMv,
				refs[ref], inter.allowHP, inter.refSignBias)
		}
		best.mode = encoder.FullRDCorrectNewMVMode(best.mode, best.mv, compound,
			nearest, near, nearestValid, nearValid)
	}
	if !e.vp9InterCompoundEnabled() {
		correctFullRDNewMV()
		finishFullRD()
		if collectFilterRD && bestSet {
			e.vp9StoreBlockFilterRDScores(&filterRDScores)
		}
		return best, bestSet
	}
	if sourceAltRefOverlay {
		correctFullRDNewMV()
		finishFullRD()
		if collectFilterRD && bestSet {
			e.vp9StoreBlockFilterRDScores(&filterRDScores)
		}
		return best, bestSet
	}
	if !useNonrd && bsize >= common.Block8x8 {
		correctFullRDNewMV()
		finishFullRD()
		if collectFilterRD && bestSet {
			e.vp9StoreBlockFilterRDScores(&filterRDScores)
		}
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
				[2]int{refSlot, secondRefSlot}, refRate,
				best.score, bestSet, filterRDScoresPtr)
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
	correctFullRDNewMV()
	finishFullRD()
	if collectFilterRD && bestSet {
		e.vp9StoreBlockFilterRDScores(&filterRDScores)
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

func vp9FullRDReferenceInSet(refFrameSet []int8, refFrame int8) bool {
	for _, enabled := range refFrameSet {
		if enabled == refFrame {
			return true
		}
	}
	return false
}

func vp9FullRDApplyBestRefSkipMask(refSkipMask *[2]uint8, refFrame int8) {
	if refSkipMask == nil {
		return
	}
	switch refFrame {
	case vp9dec.LastFrame:
		refSkipMask[0] |= (1 << uint(vp9dec.GoldenFrame)) |
			(1 << uint(vp9dec.AltrefFrame)) |
			(1 << uint(vp9dec.IntraFrame))
	case vp9dec.GoldenFrame:
		refSkipMask[0] |= (1 << uint(vp9dec.LastFrame)) |
			(1 << uint(vp9dec.AltrefFrame)) |
			(1 << uint(vp9dec.IntraFrame))
	case vp9dec.AltrefFrame:
		refSkipMask[0] |= (1 << uint(vp9dec.LastFrame)) |
			(1 << uint(vp9dec.GoldenFrame)) |
			(1 << uint(vp9dec.IntraFrame))
	}
}

func vp9FullRDRefSkipped(refSkipMask [2]uint8, refFrame, secondRefFrame int8) bool {
	if refFrame < 0 || refFrame >= 8 {
		return false
	}
	second := secondRefFrame
	if second < 0 {
		second = 0
	}
	if second >= 8 {
		return false
	}
	return refSkipMask[0]&(1<<uint(refFrame)) != 0 &&
		refSkipMask[1]&(1<<uint(second)) != 0
}

func vp9FullRDZeroMVModeAllowed(mode common.PredictionMode,
	candidateZero, nearestZero, nearZero bool,
	nearCost, nearestCost, zeroCost int,
) bool {
	if !candidateZero {
		return true
	}
	switch mode {
	case common.NearMv:
		return nearCost <= zeroCost
	case common.NearestMv:
		return nearestCost <= zeroCost
	case common.ZeroMv:
		if nearestZero && zeroCost >= nearestCost {
			return false
		}
		if nearZero && zeroCost >= nearCost {
			return false
		}
	}
	return true
}

func (e *VP9Encoder) vp9FullRDCompoundReferenceSlots(inter *vp9InterEncodeState,
	refFrames [2]int8,
) ([2]int, bool) {
	var slots [2]int
	if inter == nil {
		return slots, false
	}
	fixedRef := inter.compoundRefs.CompFixedRef
	fixedIdx := int(inter.refSignBias[fixedRef])
	if fixedIdx < 0 || fixedIdx > 1 || refFrames[fixedIdx] != fixedRef {
		return slots, false
	}
	varRef := refFrames[1-fixedIdx]
	if varRef != inter.compoundRefs.CompVarRef[0] &&
		varRef != inter.compoundRefs.CompVarRef[1] {
		return slots, false
	}
	var ok bool
	slots[0], ok = e.vp9InterReferenceSlot(inter, refFrames[0])
	if !ok {
		return slots, false
	}
	slots[1], ok = e.vp9InterReferenceSlot(inter, refFrames[1])
	if !ok {
		return slots, false
	}
	return slots, true
}

func (e *VP9Encoder) pickVP9FullRDInterReferenceMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrameSet []int8,
	fullRDRefs [vp9dec.MaxRefFrames]vp9FullRDRefState,
	sourceAltRefOverlay bool,
	filterRDScores *[vp9dec.SwitchableFilterContexts]uint64,
) (vp9InterModeDecision, bool) {
	if inter == nil || bsize < common.Block8x8 {
		return vp9InterModeDecision{}, false
	}
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	compoundAllowed := e.vp9InterCompoundEnabled() && !sourceAltRefOverlay &&
		inter.compoundAllowed && inter.referenceMode != vp9dec.SingleReference

	bestSet := false
	var best vp9InterModeDecision
	refSkipMask := [2]uint8{0, 1}
	modeSkipStart := e.sf.ModeSkipStart + 1
	for midx, def := range encoder.FullRDModeOrder {
		if midx == modeSkipStart && bestSet {
			vp9FullRDApplyBestRefSkipMask(&refSkipMask, best.refFrame)
		}
		refFrame := def.RefFrame[0]
		secondRefFrame := def.RefFrame[1]
		if refFrame <= vp9dec.IntraFrame {
			continue
		}
		if vp9FullRDRefSkipped(refSkipMask, refFrame, secondRefFrame) {
			continue
		}
		bestScoreSoFar := uint64(0)
		if bestSet {
			bestScoreSoFar = best.score
		}
		modeOrder := [1]common.PredictionMode{def.Mode}
		if secondRefFrame <= vp9dec.IntraFrame {
			if !vp9FullRDReferenceInSet(refFrameSet, refFrame) {
				continue
			}
			refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
			if !ok {
				continue
			}
			inter.ref = &e.refFrames[refSlot]
			refRate := encoder.SingleRefModeRateCost(&inter.selectFc, above, left,
				inter.referenceMode, inter.compoundRefs, refFrame)
			decision, ok := e.pickVP9InterModeWithOrder(inter, tile,
				miRows, miCols, miRow, miCol, bsize, refFrame, refRate,
				fullRDRefs[refFrame], bestScoreSoFar, bestSet,
				filterRDScores, modeOrder[:])
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
			continue
		}
		if !compoundAllowed {
			continue
		}
		refFrames := [2]int8{refFrame, secondRefFrame}
		refSlots, ok := e.vp9FullRDCompoundReferenceSlots(inter, refFrames)
		if !ok {
			continue
		}
		refRate, ok := encoder.CompoundRefRateCost(&inter.selectFc, above, left,
			inter.referenceMode, inter.compoundRefs, inter.refSignBias,
			refFrames)
		if !ok {
			continue
		}
		decision, ok := e.pickVP9CompoundInterModeWithOrder(inter, tile,
			miRows, miCols, miRow, miCol, bsize, refFrames, refSlots,
			refRate, bestScoreSoFar, bestSet, filterRDScores, modeOrder[:])
		if !ok {
			continue
		}
		if !bestSet || decision.score < best.score ||
			(decision.score == best.score && decision.rate < best.rate) {
			best = decision
			bestSet = true
		}
	}
	return best, bestSet
}

func (e *VP9Encoder) vp9FullRDRefStates(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrameSet []int8,
) [vp9dec.MaxRefFrames]vp9FullRDRefState {
	// libvpx setup_buffer_inter runs vp9_mv_pred for every enabled ref before
	// the full-RD mode loop. The resulting pred_mv_sad drives reference
	// masking, adaptive NEWMV invalidation, and the full-pel search seed.
	var states [vp9dec.MaxRefFrames]vp9FullRDRefState
	if inter == nil || bsize < common.Block8x8 {
		return states
	}
	savedRef := inter.ref
	for _, refFrame := range refFrameSet {
		if refFrame <= vp9dec.IntraFrame || int(refFrame) >= len(states) {
			continue
		}
		refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
		if !ok {
			continue
		}
		inter.ref = &e.refFrames[refSlot]
		state, ok := e.vp9InterMvPredStateForRef(inter, tile, miRows, miCols,
			miRow, miCol, bsize, refFrame)
		if ok {
			states[refFrame].mvPredState = state
		}
	}
	inter.ref = savedRef

	vp9PruneFullRDRefStates(&states, refFrameSet, e.sf.ReferenceMasking,
		e.sf.AdaptiveMotionSearch, e.vp9HeaderScratch.ShowFrame)
	return states
}

func vp9PruneFullRDRefStates(states *[vp9dec.MaxRefFrames]vp9FullRDRefState,
	refFrameSet []int8, referenceMasking, adaptiveMotionSearch int, showFrame bool,
) {
	if states == nil {
		return
	}
	if referenceMasking != 0 {
		for _, refFrame := range refFrameSet {
			if refFrame <= vp9dec.IntraFrame || int(refFrame) >= len(states) ||
				!states[refFrame].mvPredState.valid {
				continue
			}
			predSad := states[refFrame].mvPredState.predSad
			for _, otherRef := range refFrameSet {
				if otherRef <= vp9dec.IntraFrame || int(otherRef) >= len(states) ||
					!states[otherRef].mvPredState.valid {
					continue
				}
				if (predSad >> 2) > states[otherRef].mvPredState.predSad {
					states[refFrame].modeSkip |= vp9InterNearestNearZeroMask
					break
				}
			}
		}
	}
	if adaptiveMotionSearch == 0 || !showFrame {
		return
	}
	for _, refFrame := range refFrameSet {
		if refFrame <= vp9dec.IntraFrame || int(refFrame) >= len(states) ||
			!states[refFrame].mvPredState.valid {
			continue
		}
		predSad := states[refFrame].mvPredState.predSad
		for _, otherRef := range refFrameSet {
			if otherRef <= vp9dec.IntraFrame || int(otherRef) >= len(states) ||
				!states[otherRef].mvPredState.valid {
				continue
			}
			if (predSad >> 3) > states[otherRef].mvPredState.predSad {
				states[refFrame].skipNewMv = true
				break
			}
		}
	}
}

func (e *VP9Encoder) pickVP9CompoundInterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame [2]int8, refSlot [2]int, refRate int,
	bestScoreSoFar uint64, bestScoreSoFarSet bool,
	filterRDScores *[vp9dec.SwitchableFilterContexts]uint64,
) (vp9InterModeDecision, bool) {
	return e.pickVP9CompoundInterModeWithOrder(inter, tile, miRows, miCols,
		miRow, miCol, bsize, refFrame, refSlot, refRate, bestScoreSoFar,
		bestScoreSoFarSet, filterRDScores, vp9CompoundInterModeOrder[:])
}

func (e *VP9Encoder) vp9FullRDCompoundZeroMVAllowed(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mode common.PredictionMode, refFrame [2]int8,
	interModeCtx int, mv [2]vp9dec.MV,
) bool {
	if mode != common.NearMv && mode != common.NearestMv && mode != common.ZeroMv {
		return true
	}
	nearCost := encoder.InterModeRateCostN(&inter.selectFc, interModeCtx,
		common.NearMv, [2]vp9dec.MV{}, [2]vp9dec.MV{}, 2, inter.allowHP)
	nearestCost := encoder.InterModeRateCostN(&inter.selectFc, interModeCtx,
		common.NearestMv, [2]vp9dec.MV{}, [2]vp9dec.MV{}, 2, inter.allowHP)
	zeroCost := encoder.InterModeRateCostN(&inter.selectFc, interModeCtx,
		common.ZeroMv, [2]vp9dec.MV{}, [2]vp9dec.MV{}, 2, inter.allowHP)
	var nearest, near [2]vp9dec.MV
	nearestOK := true
	nearOK := true
	for ref := range 2 {
		var ok bool
		nearest[ref], ok = e.vp9EncoderInterModeCandidateMv(tile, miRows,
			miCols, miRow, miCol, bsize, common.NearestMv, refFrame[ref],
			inter.allowHP, inter.refSignBias)
		nearestOK = nearestOK && ok
		near[ref], ok = e.vp9EncoderInterModeCandidateMv(tile, miRows,
			miCols, miRow, miCol, bsize, common.NearMv, refFrame[ref],
			inter.allowHP, inter.refSignBias)
		nearOK = nearOK && ok
	}
	return vp9FullRDZeroMVModeAllowed(mode,
		mv[0] == (vp9dec.MV{}) && mv[1] == (vp9dec.MV{}),
		nearestOK && nearest[0] == (vp9dec.MV{}) &&
			nearest[1] == (vp9dec.MV{}),
		nearOK && near[0] == (vp9dec.MV{}) &&
			near[1] == (vp9dec.MV{}),
		nearCost, nearestCost, zeroCost)
}

func (e *VP9Encoder) pickVP9CompoundInterModeWithOrder(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame [2]int8, refSlot [2]int, refRate int,
	bestScoreSoFar uint64, bestScoreSoFarSet bool,
	filterRDScores *[vp9dec.SwitchableFilterContexts]uint64,
	modeOrder []common.PredictionMode,
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
	if bsize < common.BlockSizes && e.tpl.Enabled {
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
	bestScoreForGate := func() uint64 {
		if bestSet {
			if !bestScoreSoFarSet || best.score < bestScoreSoFar {
				return best.score
			}
		}
		if bestScoreSoFarSet {
			return bestScoreSoFar
		}
		return ^uint64(0)
	}
	fullRDModeSkipped := func(mode common.PredictionMode) bool {
		modeIndex, ok := encoder.FullRDModeIndex(mode, refFrame[0], refFrame[1])
		if !ok {
			return false
		}
		return e.rdThresh.FullRDModeSkipped(bestScoreForGate(), bsize, modeIndex,
			best.skip, e.sf.ScheduleModeSearch != 0)
	}
	consider := func(mode common.PredictionMode, mv, refMv [2]vp9dec.MV,
		filter vp9dec.InterpFilter, distortion uint64,
	) {
		modeIndex, modeIndexValid := encoder.FullRDModeIndex(mode,
			refFrame[0], refFrame[1])
		filterRate := vp9InterInterpFilterRateCost(inter, &inter.selectFc,
			switchableCtx, filter)
		rate := refRate +
			encoder.InterModeRateCostN(&inter.selectFc, interModeCtx, mode,
				mv, refMv, 2, inter.allowHP) + filterRate
		cand := vp9InterModeDecision{
			refFrame:       refFrame[0],
			secondRefFrame: refFrame[1],
			refSlot:        refSlot[0],
			secondRefSlot:  refSlot[1],
			isCompound:     true,
			mode:           mode,
			mv:             mv,
			interpFilter:   filter,
			txSize:         common.TxSizes,
			rate:           rate,
			distortion:     distortion,
			score:          e.vp9InterModeScore(distortion, rate, qindex),
			rdModeIndex:    modeIndex,
			rdModeValid:    modeIndexValid,
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
		if filterRDScores != nil {
			fixedRate := cand.rate - filterRate
			fixedScore := e.vp9InterModeScore(cand.distortion, fixedRate, qindex)
			vp9RecordFilterRDScore(filterRDScores, filter, fixedScore, cand.score)
		}
	}

	for _, mode := range modeOrder {
		if !modeAllowed(mode) {
			continue
		}
		if fullRDModeSkipped(mode) {
			continue
		}
		switch mode {
		case common.ZeroMv:
			if !e.vp9FullRDCompoundZeroMVAllowed(inter, tile, miRows, miCols,
				miRow, miCol, bsize, mode, refFrame, interModeCtx,
				[2]vp9dec.MV{}) {
				continue
			}
			e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
				refFrame, refSlot, mode, [2]vp9dec.MV{},
				[2]vp9dec.MV{}, consider)
		case common.NearestMv, common.NearMv:
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
				if !e.vp9FullRDCompoundZeroMVAllowed(inter, tile, miRows, miCols,
					miRow, miCol, bsize, mode, refFrame, interModeCtx, mv) {
					continue
				}
				e.evalVP9CompoundMode(inter, miRows, miCols, miRow, miCol, bsize,
					refFrame, refSlot, mode, mv, [2]vp9dec.MV{}, consider)
			}
		case common.NewMv:
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
					refFrame, refSlot, mode, newMv, newRefMv, consider)
			}
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
	refState vp9FullRDRefState, bestScoreSoFar uint64, bestScoreSoFarSet bool,
	filterRDScores *[vp9dec.SwitchableFilterContexts]uint64,
) (vp9InterModeDecision, bool) {
	return e.pickVP9InterModeWithOrder(inter, tile, miRows, miCols,
		miRow, miCol, bsize, refFrame, refRate, refState, bestScoreSoFar,
		bestScoreSoFarSet, filterRDScores, vp9SingleRefInterModeOrder[:])
}

func (e *VP9Encoder) vp9FullRDSingleZeroMVAllowed(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mode common.PredictionMode, refFrame int8,
	interModeCtx int, mv vp9dec.MV,
) bool {
	if mode != common.NearMv && mode != common.NearestMv && mode != common.ZeroMv {
		return true
	}
	nearCost := encoder.InterModeRateCost(&inter.selectFc, interModeCtx,
		common.NearMv, vp9dec.MV{}, vp9dec.MV{}, inter.allowHP)
	nearestCost := encoder.InterModeRateCost(&inter.selectFc, interModeCtx,
		common.NearestMv, vp9dec.MV{}, vp9dec.MV{}, inter.allowHP)
	zeroCost := encoder.InterModeRateCost(&inter.selectFc, interModeCtx,
		common.ZeroMv, vp9dec.MV{}, vp9dec.MV{}, inter.allowHP)
	nearest, nearestOK := e.vp9EncoderInterModeCandidateMv(tile, miRows,
		miCols, miRow, miCol, bsize, common.NearestMv, refFrame,
		inter.allowHP, inter.refSignBias)
	near, nearOK := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
		miRow, miCol, bsize, common.NearMv, refFrame, inter.allowHP,
		inter.refSignBias)
	return vp9FullRDZeroMVModeAllowed(mode, mv == (vp9dec.MV{}),
		nearestOK && nearest == (vp9dec.MV{}),
		nearOK && near == (vp9dec.MV{}),
		nearCost, nearestCost, zeroCost)
}

func (e *VP9Encoder) pickVP9InterModeWithOrder(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8, refRate int,
	refState vp9FullRDRefState, bestScoreSoFar uint64, bestScoreSoFarSet bool,
	filterRDScores *[vp9dec.SwitchableFilterContexts]uint64,
	modeOrder []common.PredictionMode,
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
	scoreW, scoreH, ok := encoder.VisibleInterScoreBlock(x0, y0, blockW, blockH,
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
	if bsize < common.BlockSizes && e.tpl.Enabled {
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
	if e.sf.UseNonrdPickMode == 0 &&
		vp9ResolveDeadlineMode(e.opts.Deadline) == vp9ModeRealtime &&
		e.vp9SpeedFeatureCPUUsed() >= 4 {
		useResidualScore = true
	}
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
		if refState.modeSkip&(1<<uint(mode)) != 0 {
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
	bestScoreForGate := func() uint64 {
		if bestSet {
			if !bestScoreSoFarSet || best.score < bestScoreSoFar {
				return best.score
			}
		}
		if bestScoreSoFarSet {
			return bestScoreSoFar
		}
		return ^uint64(0)
	}
	// vp9FullRDInterThisRDInput holds the per-SB context the genuine per-mode
	// this_rd assembly (handle_inter_mode + the rd_pick_inter_mode_sb skip pick)
	// needs. The genuine assembly is consulted ONLY on the
	// vp9InterUseDeepRDPartition-on branch (and the oracle-trace pin); in
	// production (flag off) it is never invoked, so this is inert state.
	thisRDInput := vp9FullRDInterThisRDInput{
		tile:          tile,
		miRows:        miRows,
		miCols:        miCols,
		miRow:         miRow,
		miCol:         miCol,
		bsize:         bsize,
		refFrame:      refFrame,
		interModeCtx:  interModeCtx,
		refRate:       refRate,
		switchableCtx: switchableCtx,
		above:         above,
		left:          left,
		rdmult:        e.cbRdmult,
		refBestRDInf:  true,
	}
	consider := func(mode common.PredictionMode, mv, refMv vp9dec.MV,
		filter vp9dec.InterpFilter, distortion uint64,
	) {
		modeIndex, modeIndexValid := encoder.FullRDSingleModeIndex(mode, refFrame)
		filterRate := vp9InterInterpFilterRateCost(inter, &inter.selectFc,
			switchableCtx, filter)
		rate := refRate +
			encoder.InterModeRateCost(&inter.selectFc, interModeCtx, mode,
				mv, refMv, inter.allowHP) + filterRate
		cand := vp9InterModeDecision{
			mode:         mode,
			mv:           [2]vp9dec.MV{mv},
			interpFilter: filter,
			txSize:       common.TxSizes,
			rate:         rate,
			distortion:   distortion,
			score:        e.vp9InterModeScore(distortion, rate, qindex),
			rdModeIndex:  modeIndex,
			rdModeValid:  modeIndexValid,
		}
		if useResidualScore {
			if rdDist, rdRate, rdTxSize, skippable, ok := e.scoreVP9InterModeResidual(inter, miRows,
				miCols, miRow, miCol, bsize, mode, refFrame, mv, filter); ok {
				cand.distortion = rdDist
				cand.rate = rate + rdRate
				cand.txSize = rdTxSize
				cand.skip = skippable
				cand.score = e.vp9InterModeScore(cand.distortion, cand.rate, qindex)
			}
		}
		// Deep full-RD inter (opt-in vp9InterUseDeepRDThisRDScore): score the
		// candidate with the GENUINE per-mode this_rd assembled exactly as
		// libvpx's handle_inter_mode + rd_pick_inter_mode_sb skip pick
		// (vp9_fullrd_inter_thisrd.go) over the real Y-RD (super_block_yrd) +
		// UV-RD (super_block_uvrd) + mode/MV/filter/ref rate, instead of the
		// model-RD vp9InterModeScore approximation. PRODUCTION-NEUTRAL: the flag
		// is off in production (and in the deep-RD partition serialization tests,
		// which were stabilized on the model-score leaf decisions), so this
		// branch is never taken and cand.score stays the model score →
		// byte-identical output.
		if vp9InterUseDeepRDThisRDScore {
			if grd := e.vp9FullRDInterThisRD(inter, thisRDInput, mode, mv, refMv,
				filter); grd.Valid {
				cand.distortion = grd.Distortion
				cand.rate = grd.Rate
				cand.txSize = grd.TxSize
				cand.skip = grd.Skip2
				cand.score = grd.ThisRD
			}
		}
		// Oracle-trace-only: run the genuine inter super_block_yrd producer and
		// the full per-mode this_rd assembly for the frame-1 SB0 64x64 NEWMV
		// (ref=LAST, EIGHTTAP_SMOOTH) and stash both for the inter-yrd /
		// inter-this_rd parity tests. Compile-elided in production
		// (vp9OracleTraceBuild is a false const there, so the whole block is
		// dead-code-eliminated).
		if vp9OracleTraceBuild && e.frameIndex == 1 && miRow == 0 && miCol == 0 &&
			bsize == common.Block64x64 && mode == common.NewMv &&
			refFrame == vp9dec.LastFrame && filter == vp9dec.InterpEighttapSmooth &&
			mv.Row == 12 && mv.Col == 4 {
			res := e.vp9FullRDInterSuperBlockYRD(inter, miRows, miCols, miRow,
				miCol, bsize, mode, refFrame, mv, filter, e.cbRdmult, ^uint64(0))
			e.recordVP9FullRDInterYRD(e.frameIndex, miRow, miCol, res)
			grd := e.vp9FullRDInterThisRD(inter, thisRDInput, mode, mv, refMv,
				filter)
			e.recordVP9FullRDInterThisRD(e.frameIndex, miRow, miCol, grd)
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
		if filterRDScores != nil {
			fixedRate := cand.rate - filterRate
			fixedScore := e.vp9InterModeScore(cand.distortion, fixedRate, qindex)
			vp9RecordFilterRDScore(filterRDScores, filter, fixedScore, cand.score)
		}
	}

	zeroDistortion := encoder.BlockSSE(src, srcStride, ref, refStride,
		x0, y0, x0, y0, scoreW, scoreH)
	allFilters := vp9InterInterpFilterCandidates(inter)
	// libvpx: vp9/encoder/vp9_speed_features.c — sf->disable_filter_search_var_thresh
	// prunes non-EIGHTTAP filters when source variance falls below the
	// threshold.  Mirror libvpx's source-only luma variance, not the
	// zero-motion reference error: a flat source block should skip extra
	// filter search even when the current reference is a poor predictor.
	if e.sf.DisableFilterSearchVarThresh > 0 && scoreW > 0 && scoreH > 0 &&
		len(allFilters) > 1 {
		sourceVariance := encoder.SourceVarianceAreaPerPixel(src, srcStride,
			x0, y0, scoreW, scoreH)
		if encoder.InterSkipFilterSearch(sourceVariance,
			e.sf.DisableFilterSearchVarThresh) {
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
	fullRDModeSkipped := func(mode common.PredictionMode) bool {
		if useNonrd || bsize < common.Block8x8 {
			return false
		}
		modeIndex, ok := encoder.FullRDSingleModeIndex(mode, refFrame)
		if !ok {
			return false
		}
		modeRDThresh := e.rdThresh.FullRDModeRDThreshold(bsize, modeIndex,
			best.skip, e.sf.ScheduleModeSearch != 0)
		return encoder.RDLessThanThresh(bestScoreForGate(), modeRDThresh,
			e.rdThresh.ThreshFreqFact(bsize, modeIndex))
	}
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
	evaluateFixedMVMode := func(mode common.PredictionMode) {
		if !modeAllowed(mode) {
			return
		}
		if fullRDModeSkipped(mode) {
			return
		}
		mv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, inter.allowHP,
			inter.refSignBias)
		if !ok {
			return
		}
		if sourceAltRefOverlay && mv != (vp9dec.MV{}) {
			return
		}
		if !e.vp9FullRDSingleZeroMVAllowed(inter, tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame, interModeCtx, mv) {
			return
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
			return
		}
		for _, filter := range filters {
			distortion, ok := e.vp9InterPredictionDistortion(inter, miRows, miCols,
				miRow, miCol, bsize, mode, refFrame, mv, filter)
			if ok {
				consider(mode, mv, mv, filter, distortion)
			}
		}
	}
	evaluateNewMVMode := func() {
		if !modeAllowed(common.NewMv) || refState.skipNewMv ||
			fullRDModeSkipped(common.NewMv) {
			return
		}
		refMv, refMvOK := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, refFrame, inter.allowHP,
			inter.refSignBias)
		mvOpts := vp9InterMvSearchOptions{
			refMv:      refMv,
			refMvValid: refMvOK,
			fullRD:     true,
		}
		if refState.mvPredState.valid {
			mvOpts.seed = refState.mvPredState.seed
			mvOpts.seedValid = true
		} else if state, ok := e.vp9InterMvPredStateForRef(inter, tile,
			miRows, miCols, miRow, miCol, bsize, refFrame); ok {
			mvOpts.seed = state.seed
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
				return
			}
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
	evaluateZeroMVMode := func() {
		if !modeAllowed(common.ZeroMv) || fullRDModeSkipped(common.ZeroMv) {
			return
		}
		if !e.vp9FullRDSingleZeroMVAllowed(inter, tile, miRows, miCols,
			miRow, miCol, bsize, common.ZeroMv, refFrame, interModeCtx,
			vp9dec.MV{}) {
			return
		}
		for _, filter := range pickFilters(common.ZeroMv, vp9dec.MV{}, refIsLast) {
			consider(common.ZeroMv, vp9dec.MV{}, vp9dec.MV{}, filter,
				zeroDistortion)
		}
	}
	for _, mode := range modeOrder {
		switch mode {
		case common.NearestMv, common.NearMv:
			evaluateFixedMVMode(mode)
		case common.NewMv:
			evaluateNewMVMode()
		case common.ZeroMv:
			evaluateZeroMVMode()
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

	filters := e.vp9Sub8InterpFilterCandidates(inter, miRow, miCol, bsize)
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
				txSize:         common.Tx4x4,
				rate:           rate,
				distortion:     distortion,
				score:          e.vp9InterModeScore(distortion, rate, qindex),
			}
			if e.sf.UseNonrdPickMode == 0 {
				if rdDist, rdRate, hasResidue, ok := e.scoreVP9InterTxCandidate(
					inter, miRows, miCols, miRow, miCol, bsize,
					common.Tx4x4, true); ok {
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

func (e *VP9Encoder) vp9Sub8InterpFilterCandidates(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) []vp9dec.InterpFilter {
	filters := vp9InterInterpFilterCandidates(inter)
	if len(filters) <= 1 {
		return filters
	}
	if e.sf.DisableFilterSearchVarThresh > 0 && inter != nil && inter.img != nil {
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
		blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
		x0 := miCol * common.MiSize
		y0 := miRow * common.MiSize
		scoreW, scoreH, ok := encoder.VisibleInterScoreBlock(x0, y0,
			blockW, blockH, srcW, srcH, srcW, srcH)
		if ok && len(src) != 0 && srcStride > 0 {
			sourceVariance := encoder.SourceVarianceAreaPerPixel(src, srcStride,
				x0, y0, scoreW, scoreH)
			if encoder.InterSkipFilterSearch(sourceVariance,
				e.sf.DisableFilterSearchVarThresh) {
				return vp9EighttapInterpFilterOrder[:]
			}
		}
	}
	if e.sf.AdaptivePredInterpFilter == 1 && inter != nil && inter.predFilterValid &&
		inter.predInterpFilter < vp9dec.InterpSwitchable {
		return vp9InterpFilterOrderForSingle(inter.predInterpFilter)
	}
	if e.sf.AdaptivePredInterpFilter == 2 {
		if inter != nil && inter.predFilterValid &&
			inter.predInterpFilter < vp9dec.InterpSwitchable {
			return vp9InterpFilterOrderForSingle(inter.predInterpFilter)
		}
		return vp9EighttapInterpFilterOrder[:]
	}
	return filters
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

func (e *VP9Encoder) vp9InterMvPredStateForRef(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
) (vp9InterMvPredState, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid ||
		bsize < common.Block8x8 {
		return vp9InterMvPredState{}, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	refBuf, refStride, refOriginX, refOriginY, _, _, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if len(src) == 0 || len(refBuf) == 0 || srcStride <= 0 ||
		refStride <= 0 || !refOK {
		return vp9InterMvPredState{}, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH {
		return vp9InterMvPredState{}, false
	}
	refRows := len(refBuf) / refStride
	var candidates [encoder.MvPredMaxCandidates]encoder.MvPredInputCandidate
	refList, refCount := vp9dec.FindInterMvRefsFields(e.miGrid,
		e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
		tile, miRows, miCols, miRow, miCol, bsize,
		common.NearMv, refFrame, inter.refSignBias, -1)
	if refCount >= 1 {
		candidates[0] = encoder.MvPredInputCandidate{MV: refList[0], Valid: true}
	}
	if refCount >= 2 {
		candidates[1] = encoder.MvPredInputCandidate{MV: refList[1], Valid: true}
	}
	if predMv, ok := e.vp9VarPartSBPredMv(miCols, miRow, miCol, refFrame); ok {
		candidates[2] = encoder.MvPredInputCandidate{MV: predMv, Valid: true}
	}
	maxPartitionSize := e.sf.DefaultMaxPartitionSize
	if maxPartitionSize == 0 {
		maxPartitionSize = common.Block64x64
	}
	result := encoder.MvPredScanCandidates(candidates[:],
		encoder.MvPredNumCandidates(bsize, maxPartitionSize),
		src, srcStride, x0, y0,
		refBuf, refStride, x0, y0, refOriginX, refOriginY, refRows,
		blockW, blockH)
	if result.BestIndex < 0 || result.BestIndex >= len(candidates) ||
		!candidates[result.BestIndex].Valid {
		return vp9InterMvPredState{}, false
	}
	return vp9InterMvPredState{
		seed:    candidates[result.BestIndex].MV,
		predSad: result.BestSad,
		valid:   true,
	}, true
}

type vp9InterMvSearchOptions struct {
	seed            vp9dec.MV
	seedValid       bool
	refMv           vp9dec.MV
	refMvValid      bool
	nonrdSubpelTree bool
	useMvPart       bool
	fullRD          bool
	nonrdPrecheck   func(vp9dec.MV) bool
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
) (uint64, int, common.TxSize, bool, bool) {
	if inter == nil || inter.dq == nil {
		return 0, 0, common.TxSizes, false, false
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
		return 0, 0, common.TxSizes, false, false
	}
	distortion, rate, hasResidue, ok := e.scoreVP9InterTxCandidate(inter,
		miRows, miCols, miRow, miCol, bsize, txSize, true)
	if !ok {
		return 0, 0, common.TxSizes, false, false
	}
	if !hasResidue {
		rate = 0
	}
	return distortion, rate, txSize, !hasResidue, true
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
	mvLimits := encoder.EncoderMvLimits(miRows, miCols, miRow, miCol, bsize)
	mvLimits.SetFullpelSearchRange(refMvForRange)
	sadAt := func(dx, dy int) (uint64, bool) {
		if !mvLimits.InFullpelRange(dy, dx) {
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
		return encoder.BlockSADOffsets(src, srcOff, srcStride, ref, refOff,
			refStride, blockW, blockH, ^uint64(0)), true
	}

	sadPerBit := encoder.SADPerBit16(e.vp9EncoderModeDecisionQIndex())
	scoreMv := func(dx, dy int, sad uint64) uint64 {
		return sad + uint64(encoder.FullPelMVSADCost(dy, dx,
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
	if opts.fullRD {
		// Full-RD single_motion_search (vp9_rdopt.c:2563) on the no-recode
		// realtime path: step_param = cpi->mv_step_param == 0 (set_mv_search_
		// params @ vp9_encoder.c:3728 is never called when recode_loop ==
		// DISALLOW_RECODE), and vp9_full_pixel_search NSTEP dispatches to
		// full_pixel_diamond (vp9_mcomp.c:2916/2486), which re-scores every
		// candidate (and the final) with vp9_get_mvpred_var (variance, not
		// SAD; :1454). This is distinct from sf.mv.fullpel_search_step_param
		// (=6), which the NONRD path passes (vp9_pickmode.c:171); the full-RD
		// path must NOT use the SF field.
		var newSad uint64
		bestDx, bestDy, newSad, ok = e.vp9FullRDFullPelMv(inter, miRows, miCols,
			miRow, miCol, bsize, refFrame, opts, &mvLimits, sadAt, sadPerBit,
			refFullDy, refFullDx)
		if !ok {
			return vp9dec.MV{}, 0, false
		}
		bestSad = newSad
		bestScore = scoreMv(bestDx, bestDy, bestSad)
	} else {
		if opts.seedValid {
			seedDx := int(opts.seed.Col) >> 3
			seedDy := int(opts.seed.Row) >> 3
			seedDy, seedDx = mvLimits.ClampFullpel(seedDy, seedDx)
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
			// fast-hex / fast-diamond / NSTEP dispatcher (vp9_mcomp.c:2875).
			// Read that field here instead of always running the square fan.
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
				bestDx, bestDy, bestSad, bestScore = encoder.FastDiamondPatternSearchSAD(
					bestDx, bestDy, bestSad, bestScore, e.sf.Mv.FullpelSearchStepParam,
					&mvLimits, sadAt, scoreMv)
			} else if e.sf.Mv.SearchMethod == SearchMethodFastHex {
				bestDx, bestDy, bestSad, bestScore = encoder.FastHexPatternSearchSAD(
					bestDx, bestDy, bestSad, bestScore, e.sf.Mv.FullpelSearchStepParam,
					&mvLimits, sadAt, scoreMv)
			} else if e.sf.Mv.SearchMethod == SearchMethodNStep ||
				e.sf.Mv.SearchMethod == SearchMethodMesh {
				bestDx, bestDy, bestSad, bestScore = encoder.NStepDiamondSearchSAD(
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
	}
	mv := vp9dec.MV{Row: int16(bestDy * 8), Col: int16(bestDx * 8)}
	vp9dec.ClampMvRef(&mv, miRows, miCols, miRow, miCol, bsize)
	vp9dec.LowerMvPrecision(&mv, inter.allowHP)
	if opts.nonrdPrecheck != nil && !opts.nonrdPrecheck(mv) {
		return vp9dec.MV{}, bestScore, false
	}
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
	if opts.fullRD {
		// libvpx single_motion_search tail stores tmp_mv->as_mv as
		// x->pred_mv[ref] (vp9_rdopt.c:2738), the SUBPEL result that becomes
		// vp9_mv_pred's third candidate for subsequent blocks. The full-pel MV
		// (pre-subpel) was pinned earlier for the SB0 (0,0) full-pel parity
		// test; pin the refined MV here for the SB0 64x64 subpel parity test
		// (no-op in non-trace builds). The candidate[2] propagation itself is
		// held back pending the holistic single-pass rd_pick_partition port —
		// see vp9InterMvPredStateForRef.
		e.recordVP9FullRDFirstInterSubpelMv(e.frameIndex, miRow, miCol,
			refFrame, int(mv.Row), int(mv.Col))
	}
	return mv, bestScore, true
}
