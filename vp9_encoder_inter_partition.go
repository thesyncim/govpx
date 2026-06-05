package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func (e *VP9Encoder) pickVP9InterPartitionBlockSize(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
) common.BlockSize {
	horzSize, vertSize, splitSize, ok := encoder.InterRDPartitionSizes(root)
	if !ok {
		return root
	}
	// libvpx vp9_encodeframe.c:4215-4218 — each 64x64 SB resets
	// x->pred_mv[MAX_REF_FRAMES] to the INT16_MAX sentinel before
	// rd_pick_partition. Mirror that per-SB reset for the full-RD pred_mv
	// thread (the genuine depth-first recursion's candidate[2] source). Gated on
	// the full deep stack (vp9InterUseDeepRDSub8x8): the deep-partition-only
	// round-trip harness and production keep candidate[2] on the var-part cache.
	if (vp9InterUseDeepRDSub8x8 || vp9InterUseDeepRDUsePartition) &&
		root == common.Block64x64 {
		for i := range e.fullRDPredMv {
			e.fullRDPredMv[i] = vp9InterPredMvSentinel
		}
	}
	// SPEED_FEATURES.partition_search_type == FIXED_PARTITION (cpu_used=8
	// realtime in libvpx) pins the whole SB to sf.AlwaysThisBlockSize. We
	// only honour it for square block sizes that fit; otherwise fall through
	// to the variance / RD path so non-square edges remain decodable.
	//
	// libvpx: vp9_encodeframe.c set_fixed_partitioning walks the SB at
	// sf->always_this_block_size granularity.
	if fixed, on := e.vp9InterPartitionFixed(); on {
		if fixed >= common.Block8x8 && fixed <= root {
			return fixed
		}
	}
	// SPEED_FEATURES.partition_search_type == ML_BASED_PARTITION (cpu_used=8
	// realtime + w*h <= 352*288, libvpx vp9_speed_features.c:751-768 +
	// 825-826). vp9MLPickPartitionEntry seeds per-SB est_pred via
	// get_estimated_pred (libvpx vp9_encodeframe.c:5314) and
	// vp9NonrdPickPartition mirrors the ml_based_partitioning=1 branch of
	// nonrd_pick_partition (libvpx vp9_encodeframe.c:4598-4855 + 4660-4667).
	//
	// The full recursive walker runs at every ML-eligible recursion level
	// (BLOCK_64X64, BLOCK_32X32, BLOCK_16X16). govpx's writeVP9ModesSb
	// walker calls this dispatcher once per (miRow, miCol, bsize) region;
	// when the picker
	// returns the same bsize the walker commits PARTITION_NONE, when it
	// returns the PARTITION_SPLIT subsize the walker recurses 4 ways. That
	// folds the libvpx recursive nonrd_pick_partition body onto govpx's
	// already-recursive write walker without a separate PC_TREE substrate.
	// Forced-edge splits (libvpx vp9_encodeframe.c:4617-4626) are honoured
	// by vp9NonrdPickPartition for trailing rows/cols at the frame edge.
	// On the -1 ("no confidence") branch the libvpx picker RD-compares
	// PARTITION_NONE against PARTITION_SPLIT (libvpx vp9_encodeframe.c:
	// 4676-4746); govpx runs that compare via
	// vp9NonrdPickPartitionRDFallback — pickVP9InterReference-
	// Mode supplies the PARTITION_NONE candidate (libvpx 4677 nonrd_pick_-
	// sb_modes invoking vp9_pick_inter_mode at vp9_pickmode.c:1696) and
	// scoreVP9InterPartitionSplit supplies the recursive PARTITION_SPLIT
	// candidate (libvpx 4725 recursive nonrd_pick_partition call) plus
	// the partition_cost rate (libvpx 4686 / 4715). When both candidates
	// fail the dispatcher continues to the variance / RD fallback below.
	//
	// This dispatch follows sf.PartitionSearchType == ML_BASED_PARTITION
	// directly, matching libvpx's use_ml_based_partitioning predicate.
	if e.sf.PartitionSearchType == MlBasedPartition {
		if root == common.Block64x64 || root == common.Block32x32 ||
			root == common.Block16x16 {
			if mlCtx := e.vp9MLPickPartitionEntry(inter, miRows, miCols,
				miRow, miCol); mlCtx != nil {
				if picked, ok := e.vp9NonrdPickPartition(mlCtx, miRows,
					miCols, miRow, miCol, root); ok {
					return picked
				}
				// libvpx vp9_encodeframe.c:4675-4746 — NN=-1
				// PARTITION_NONE vs PARTITION_SPLIT RDCOST compare.
				if picked, ok := e.vp9NonrdPickPartitionRDFallback(
					inter, tile, partitionProbs, miRows, miCols,
					miRow, miCol, root); ok {
					return picked
				}
			}
		} else if root >= common.Block8x8 && root < common.BlockSizes {
			// libvpx ML_BASED dispatch pins min_partition_size to BLOCK_8X8
			// (vp9_encodeframe.c:5316). nonrd_pick_partition clears do_split
			// at 8x8; govpx's recursive writeVP9ModesSb walker re-enters
			// this dispatcher at 8x8 — commit the leaf instead of falling
			// through to the recursive RD partition search below.
			return root
		}
	}
	// Deep full-RD inter partition (opt-in vp9InterUseDeepRDPartition, default
	// OFF). When active and the SB is on the SearchPartition path, the genuine
	// depth-first pickVP9InterPartitionRD recursion is the UNCONDITIONAL
	// partition decision: route to it here, BEFORE the shallow early-exits
	// below (full.distortion==0 -> NONE, variance/textured-split shortcuts). On
	// perfectly-predicted planted motion those shortcuts return NONE without
	// ever running the recursion, which left the SEARCH->WRITE caches unpopulated
	// for that SB and forced the writer to re-pick the leaf (the architecture
	// bug). Running the recursion here populates the per-node partition cache and
	// per-leaf decision cache for the whole SB, and returns the recursion's own
	// committed root target so the writer descends exactly that tree. The
	// recursion is partition-context-neutral (it restores e.above/leftSegCtx), so
	// the writer still owns the single canonical UpdatePartitionContext stamp.
	// Flag OFF: skipped entirely; the shallow tail below is byte-identical to
	// production. libvpx ref: rd_pick_partition (vp9_encodeframe.c:3667).
	if vp9InterUseDeepRDPartition && e.sf.PartitionSearchType == SearchPartition &&
		inter != nil {
		deepRateCostProbs := &inter.selectFc.PartitionProb
		// Run the recursion unconditionally (under the flag) so the SEARCH->WRITE
		// caches are populated for this SB. When replay is enabled, return the
		// recursion's committed root target so the writer descends exactly the
		// search's tree. When replay is DISABLED (the negative-control path),
		// fall through to the shallow early-exits below: the cache still holds the
		// search's commit, but the writer re-decides the partition independently —
		// reproducing the WRITE-diverges-from-SEARCH bug the caches fix.
		//
		// pickVP9InterPartitionRD's committed scorer leaves inter.ref / pred-filter
		// pointing at the last committed leaf; the shallow tail snapshots and
		// restores these itself, so restore the entry state here too (mirrors the
		// old dispatch's post-recursion restore) before returning or falling
		// through.
		hoistSavedRef := inter.ref
		hoistSavedPredFilter := inter.predInterpFilter
		hoistSavedPredFilterValid := inter.predFilterValid
		rd, ok := e.pickVP9InterPartitionRD(inter, tile, deepRateCostProbs,
			miRows, miCols, miRow, miCol, root)
		inter.ref = hoistSavedRef
		inter.predInterpFilter = hoistSavedPredFilter
		inter.predFilterValid = hoistSavedPredFilterValid
		if vp9InterDeepRDReplayWrites {
			if ok {
				return rd.target
			}
			return root
		}
	}
	// libvpx REFERENCE_PARTITION: choose_partitioning stamps varPartGrid,
	// then nonrd_select_partition walks it (vp9_encodeframe.c:5348-5357).
	// govpx maps that walk onto writeVP9ModesSb via vp9VarPartDecisionFor.
	if e.sf.PartitionSearchType == ReferencePartition && inter != nil {
		if e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, nil, inter) {
			if picked, ok := e.vp9NonrdSelectPartitionBlockSize(inter, tile,
				partitionProbs, miRows, miCols, miRow, miCol, root); ok {
				return picked
			}
		}
	}
	if varianceSize, ok := e.pickVP9CBRVariancePartitionBlockSize(inter,
		miRows, miCols, miRow, miCol, root); ok {
		return varianceSize
	}
	// SPEED_FEATURES.partition_search_type == VAR_BASED_PARTITION (cpu_used
	// >= 5 in libvpx realtime) replaces the recursive RD partition search
	// with a single choose_partitioning pass. govpx's variance picker above
	// is the equivalent of that pass; when it returns BlockInvalid (no
	// confidence) libvpx still runs the recursive search at speeds 5-7, but
	// at speed 8 (when UseNonrdPickMode is set) it commits the root size
	// outright. Mirror that here.
	//
	// libvpx: vp9_encodeframe.c:5470 — case VAR_BASED_PARTITION.
	if e.vp9InterPartitionVarBased() && e.vp9InterUsesNonrdPickmode() {
		return root
	}
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, root)
	if !ok {
		return root
	}
	defer e.releaseVP9PartitionReconSnapshot(reconSnap)
	savedRef := inter.ref
	savedPredFilter := inter.predInterpFilter
	savedPredFilterValid := inter.predFilterValid
	pickPredSnap := e.saveVP9MLPickPredSnapshot(inter, miRows, miCols,
		miRow, miCol)
	defer e.restoreVP9MLPickPredSnapshot(pickPredSnap)
	full, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, root)
	if !ok {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}
	if full.distortion == 0 {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}
	if e.vp9InterPreferVarianceRoot(inter, miRows, miCols, miRow, miCol, root) {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return root
	}
	if e.vp9InterPreferTexturedSplit(inter, miRow, miCol, root) &&
		splitSize >= common.Block8x8 {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		return splitSize
	}
	if root == common.Block8x8 && e.sf.AdaptivePredInterpFilter != 0 {
		inter.predInterpFilter = full.interpFilter
		if full.intra || inter.predInterpFilter >= vp9dec.InterpSwitchable {
			inter.predInterpFilter = vp9dec.InterpEighttap
		}
		inter.predFilterValid = true
	}
	// libvpx VAR_BASED_PARTITION (set at RT speed >= 4) decides the
	// partition up front in vp9_choose_partitioning and DOES NOT compare
	// horz/vert/split RD scores against the root: nonrd_use_partition
	// walks the pre-baked partition tree and runs vp9_pick_inter_mode
	// per leaf. When SPEED_FEATURES asks for VAR_BASED_PARTITION the
	// remaining horz/vert/split exploration here is pure overhead that
	// libvpx never runs. The variance/textured fast paths above already
	// committed any pre-baked decision; falling through here means
	// keeping the root partition.
	// libvpx: vp9/encoder/vp9_speed_features.c:582 / 667
	// (partition_search_type = VAR_BASED_PARTITION), vp9/encoder/
	// vp9_encodeframe.c:4854 nonrd_use_partition.
	if e.sf.PartitionSearchType == VarBasedPartition {
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		inter.predInterpFilter = savedPredFilter
		inter.predFilterValid = savedPredFilterValid
		return root
	}

	// Cost partition tokens against inter.selectFc.PartitionProb, the
	// pre-WriteCompressedHeader snapshot of e.fc.PartitionProb that
	// inter.selectFc captures at the start of encodeVP9FrameInto*. The
	// `partitionProbs` argument carries the post-WriteCompressedHeader
	// values used by the writer at WritePartitionForBlock so encoder
	// emission stays bit-identical with what the decoder reads through
	// d.fc.PartitionProb (also post-WriteCompressedHeader). Using
	// partitionProbs directly here flips partition decisions between
	// the prepass (which sees pre-update partitionProbs) and the real
	// write pass (which sees post-update partitionProbs) on uniform
	// synthetic content where the RD margins between adjacent partition
	// sizes are within a handful of cost units, leaving the bool reader
	// to underflow the tile body and reject the frame with
	// ErrInvalidVP9Data. libvpx avoids the entire failure mode by
	// running mode decision once (with the pre-update probs) and
	// emitting bits in a separate pass; mirroring its rate-cost source
	// here keeps the prepass / real-pass walks bit-for-bit identical
	// while preserving the post-update writer probs the decoder
	// expects.
	rateCostProbs := partitionProbs
	if inter != nil {
		rateCostProbs = &inter.selectFc.PartitionProb
	}
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	qindex := e.vp9EncoderModeDecisionQIndex()
	// PARTITION_NONE seed: the parent leaf RD already computed above as `full`
	// (rd_pick_sb_modes for pc_tree->none, vp9_encodeframe.c:3819). libvpx
	// scores partition candidates with the unconditional full-tree cost
	// (cpi->partition_cost[pl][type]); the hasRows/hasCols-clamped form is only
	// for the bitstream writer. See vp9_fullrd_partition_cost.go.
	noneScore := e.vp9AddModeDecisionRate(full.score,
		RDPartitionCost(rateCostProbs, ctx, common.PartitionNone), qindex)

	// Full-RD inter path: delegate the NONE/HORZ/VERT/SPLIT square-partition
	// search to the depth-first rd_pick_partition skeleton
	// (rdPickVP9InterPartition, vp9_encoder_inter_partition_rd.go). Gated on
	// PartitionSearchType==SearchPartition so only the full-RD inter path
	// reaches it (the FixedPartition / ML / REFERENCE / VAR / nonrd dispatches
	// all returned above). Step (a) of the port: this is a structural no-op —
	// the skeleton reproduces the shallow-RD tail's decision byte-for-byte. The
	// opt-in genuine depth-first recursion (vp9InterUseDeepRDPartition) is
	// dispatched earlier, before the shallow early-exits, so it is the
	// unconditional partition decision when active; this skeleton is the flag-OFF
	// production path.
	//
	// libvpx ref: rd_pick_partition (vp9/encoder/vp9_encodeframe.c:3667).
	bestSize := root
	if e.sf.PartitionSearchType == SearchPartition {
		node := newVP9InterPartitionRDNode(root)
		bestSize = e.rdPickVP9InterPartition(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, root, horzSize, vertSize, splitSize,
			noneScore, vp9InterPartitionRD{
				target:     root,
				rate:       full.rate,
				distortion: full.distortion,
				score:      full.score,
			}, &node, qindex)
	}
	e.restoreVP9PartitionReconSnapshot(reconSnap)
	inter.ref = savedRef
	inter.predInterpFilter = savedPredFilter
	inter.predFilterValid = savedPredFilterValid
	return bestSize
}

// vp9InterPreferVarianceRoot mirrors libvpx realtime speed-8
// choose_partitioning's 64x64 variance threshold for the non-key LAST_FRAME
// path. It catches flat temporal deltas where splitting only buys mode/MV
// noise in the bitstream.
func (e *VP9Encoder) vp9InterPreferVarianceRoot(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) bool {
	if !e.vp9RealtimeVariancePartitionEnabled() || inter == nil ||
		inter.dq == nil || bsize != common.Block64x64 {
		return false
	}
	if miRow+int(common.Num8x8BlocksHighLookup[bsize]) > miRows ||
		miCol+int(common.Num8x8BlocksWideLookup[bsize]) > miCols {
		return false
	}
	refSlot, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame)
	if !ok {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(&e.refFrames[refSlot], 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > refW || y0+blockH > refH {
		return false
	}
	variance := encoder.BlockDiffVariance(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH)
	threshold := encoder.RealtimeVariancePartitionThreshold64(inter.dq.Y[0][1],
		srcW, srcH)
	return variance < threshold
}

func (e *VP9Encoder) vp9InterPreferTexturedSplit(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	if bsize <= common.Block8x8 {
		return false
	}
	sse, activity, ok := e.vp9InterTxResidualStats(inter, miRow, miCol, bsize)
	if !ok || sse == 0 {
		return false
	}
	pixels := uint64(common.Num4x4BlocksWideLookup[bsize]) *
		uint64(common.Num4x4BlocksHighLookup[bsize]) * 16
	return sse > pixels*512 && activity > pixels*128
}

// vp9ChoosePartitioningSBIndex returns the SB index for (miRow, miCol)
// in e.varPartSBComputed. Mirrors libvpx's sb_offset computation
// (vp9_encodeframe.c:1314): sb_offset = (mi_stride >> 3) * (mi_row >> 3)
// + (mi_col >> 3). govpx flattens to (sbRow * sbCols + sbCol).
func (e *VP9Encoder) vp9ChoosePartitioningSBIndex(miCols, miRow, miCol int) int {
	sbCols := (miCols + 7) >> 3
	sbRow := miRow >> 3
	sbCol := miCol >> 3
	return sbRow*sbCols + sbCol
}

func (e *VP9Encoder) vp9EnsureVarPartSBMotionCaches(miRows, miCols int) int {
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	if sbCount <= 0 {
		return 0
	}
	e.varPartSBUseMvPart = buffers.EnsureLenZeroTail(e.varPartSBUseMvPart, sbCount)
	e.varPartSBMvPart = buffers.EnsureLen(e.varPartSBMvPart, sbCount)
	e.varPartSBPredValid = buffers.EnsureLenZeroTail(e.varPartSBPredValid, sbCount)
	e.varPartSBPredLast = buffers.EnsureLen(e.varPartSBPredLast, sbCount)
	e.varPartSBColorSensitivity = buffers.EnsureLenZeroTail(
		e.varPartSBColorSensitivity, sbCount)
	return sbCount
}

func (e *VP9Encoder) vp9VarPartSBMvPart(miCols, miRow, miCol int) (vp9dec.MV, bool) {
	if e == nil {
		return vp9dec.MV{}, false
	}
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	idx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if idx < 0 || idx >= len(e.varPartSBUseMvPart) ||
		idx >= len(e.varPartSBMvPart) || !e.varPartSBUseMvPart[idx] {
		return vp9dec.MV{}, false
	}
	return e.varPartSBMvPart[idx], true
}

func (e *VP9Encoder) vp9VarPartSBPredMv(miCols, miRow, miCol int,
	refFrame int8,
) (vp9dec.MV, bool) {
	if e == nil || refFrame != vp9dec.LastFrame {
		return vp9dec.MV{}, false
	}
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	idx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if idx < 0 || idx >= len(e.varPartSBPredValid) ||
		idx >= len(e.varPartSBPredLast) || !e.varPartSBPredValid[idx] {
		return vp9dec.MV{}, false
	}
	return e.varPartSBPredLast[idx], true
}

func (e *VP9Encoder) vp9VarPartSBColorSensitivity(miCols, miRow, miCol int) (
	[2]bool, bool,
) {
	if e == nil {
		return [2]bool{}, false
	}
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	idx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if idx < 0 || idx >= len(e.varPartSBColorSensitivity) ||
		idx >= len(e.varPartSBComputed) || !e.varPartSBComputed[idx] {
		return [2]bool{}, false
	}
	return e.varPartSBColorSensitivity[idx], true
}

func (e *VP9Encoder) vp9VarPartForceSkipLowTempVar(miCols, miRow, miCol int,
	bsize common.BlockSize,
) bool {
	forceSkip, _ := e.vp9VarPartForceSkipLowTempVarOK(miCols, miRow, miCol,
		bsize)
	return forceSkip
}

func (e *VP9Encoder) vp9VarPartForceSkipLowTempVarOK(miCols, miRow, miCol int,
	bsize common.BlockSize,
) (forceSkip bool, ok bool) {
	if e == nil || e.sf.ShortCircuitLowTempVar == 0 {
		return false, false
	}
	// libvpx ML_BASED_PARTITION (cpu_used=8, w*h <= 352*288) reaches the
	// nonrd picker via get_estimated_pred + nonrd_pick_partition
	// (vp9_encodeframe.c:5313-5321) and never calls choose_partitioning, so
	// x->variance_low stays at its calloc/reset all-zero value: every
	// get_force_skip_low_temp_var lookup (vp9_pickmode.c:1452-1496) returns 0.
	// Mirror that as a known force_skip=false rather than letting the cold-SB
	// cache fall through to the choose_partitioning warm-path heuristic, which
	// defaults a missing stamp to force_skip=true.
	if e.sf.PartitionSearchType == MlBasedPartition {
		return false, true
	}
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	idx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if idx < 0 || idx >= len(e.varPartSBVarLow) ||
		idx >= len(e.varPartSBComputed) || !e.varPartSBComputed[idx] {
		return false, false
	}
	varianceLow := e.varPartSBVarLow[idx]
	i := (miRow & 0x7) >> 1
	j := (miCol & 0x7) >> 1
	switch bsize {
	case common.Block64x64:
		return varianceLow[0] != 0, true
	case common.Block64x32:
		if (miCol&0x7) == 0 && (miRow&0x7) == 0 {
			return varianceLow[1] != 0, true
		}
		if (miCol&0x7) == 0 && (miRow&0x7) != 0 {
			return varianceLow[2] != 0, true
		}
	case common.Block32x64:
		if (miCol&0x7) == 0 && (miRow&0x7) == 0 {
			return varianceLow[3] != 0, true
		}
		if (miCol&0x7) != 0 && (miRow&0x7) == 0 {
			return varianceLow[4] != 0, true
		}
	case common.Block32x32:
		if (miCol&0x7) == 0 && (miRow&0x7) == 0 {
			return varianceLow[5] != 0, true
		}
		if (miCol&0x7) != 0 && (miRow&0x7) == 0 {
			return varianceLow[6] != 0, true
		}
		if (miCol&0x7) == 0 && (miRow&0x7) != 0 {
			return varianceLow[7] != 0, true
		}
		if (miCol&0x7) != 0 && (miRow&0x7) != 0 {
			return varianceLow[8] != 0, true
		}
	case common.Block16x16:
		return varianceLow[encoder.PosShift16x16[i][j]] != 0, true
	case common.Block32x16:
		j2 := ((miCol + 2) & 0x7) >> 1
		return varianceLow[encoder.PosShift16x16[i][j]] != 0 &&
			varianceLow[encoder.PosShift16x16[i][j2]] != 0, true
	case common.Block16x32:
		i2 := ((miRow + 2) & 0x7) >> 1
		return varianceLow[encoder.PosShift16x16[i][j]] != 0 &&
			varianceLow[encoder.PosShift16x16[i2][j]] != 0, true
	case common.Block8x8, common.Block4x4:
		// libvpx get_force_skip_low_temp_var (vp9_pickmode.c:1452-1496) has
		// no BLOCK_8X8/BLOCK_4X4 branch and returns 0. Returning ok=false
		// here made the warm-path refMask clamp fire on every 8x8 leaf even
		// after choose_partitioning stamped the SB cache.
		return false, true
	}
	return false, false
}

func (e *VP9Encoder) vp9RecordVarPartSBColorSensitivity(miRows, miCols int,
	sbMiRow, sbMiCol, sbIdx int, inter *vp9InterEncodeState,
	args *encoder.ChoosePartitioningArgs,
) {
	if e == nil || inter == nil || inter.img == nil || args == nil ||
		sbIdx < 0 || sbIdx >= len(e.varPartSBColorSensitivity) {
		return
	}
	subBsize := encoder.GetEstimatedPredSubBsize(sbMiRow, sbMiCol, miRows, miCols)
	if subBsize >= common.BlockSizes || len(args.PlaneDst) == 0 ||
		args.DstStride <= 0 {
		e.varPartSBColorSensitivity[sbIdx] = [2]bool{}
		return
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	blockW := int(common.Num4x4BlocksWideLookup[subBsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[subBsize]) * 4
	x0 := sbMiCol * common.MiSize
	y0 := sbMiRow * common.MiSize
	if !vp9PlaneWindowFits(src, srcStride, x0, y0, blockW, blockH) ||
		!encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
		!vp9PlaneWindowOffsetFits(args.PlaneDst, args.DstStride,
			args.PlaneDstOff, blockW, blockH) {
		e.varPartSBColorSensitivity[sbIdx] = [2]bool{}
		return
	}
	ySad := encoder.BlockSADOffsets(src, y0*srcStride+x0, srcStride,
		args.PlaneDst, args.PlaneDstOff, args.DstStride, blockW, blockH,
		^uint64(0))
	uvSad, ok := e.vp9VarPartChromaSAD(inter, miRows, miCols, sbMiRow,
		sbMiCol, subBsize, args.PartitionRefFrame, args.PartitionMV)
	if !ok {
		e.varPartSBColorSensitivity[sbIdx] = [2]bool{}
		return
	}
	sensitivity := encoder.ChromaCheck(encoder.ChromaCheckArgs{
		YSAD:                   ySad,
		UVSAD:                  uvSad,
		Speed:                  e.vp9SpeedFeatureCPUUsed(),
		ScreenContent:          e.opts.ScreenContentMode == int8(VP9ScreenContentScreen),
		SceneChangeDetected:    e.rc.highSourceSAD,
		BaseQIndex:             args.BaseQIndex,
		VariancePartThreshMult: args.VariancePartThreshMult,
		Width:                  args.FrameWidth,
		Height:                 args.FrameHeight,
		ContentState:           args.ContentState,
		NoiseEstimateEnabled:   args.NoiseEstimateEnabled,
		NoiseLevel:             args.NoiseLevel,
		AvgFrameQIndexInter:    args.AvgFrameQIndexInter,
		Disable16x16PartNonkey: args.Disable16x16PartNonkey,
	})
	e.varPartSBColorSensitivity[sbIdx] = sensitivity
}

func (e *VP9Encoder) vp9VarPartChromaSAD(inter *vp9InterEncodeState,
	miRows, miCols, sbMiRow, sbMiCol int, bsize common.BlockSize,
	refFrame int8, mv vp9dec.MV,
) ([2]uint64, bool) {
	var sad [2]uint64
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         common.ZeroMv,
		InterpFilter: uint8(vp9dec.InterpBilinear),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, sbMiRow, sbMiCol,
		bsize, &mi) {
		return sad, false
	}
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return sad, false
		}
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		dst, dstStride := e.vp9EncoderReconPlane(plane)
		blockW := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		blockH := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		x0 := (sbMiCol * common.MiSize) >> pd.SubsamplingX
		y0 := (sbMiRow * common.MiSize) >> pd.SubsamplingY
		if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
			!vp9PlaneWindowFits(src, srcStride, x0, y0, blockW, blockH) ||
			!vp9PlaneWindowFits(dst, dstStride, x0, y0, blockW, blockH) {
			return sad, false
		}
		sad[plane-1] = encoder.BlockSAD(src, srcStride, dst, dstStride,
			x0, y0, x0, y0, blockW, blockH, ^uint64(0))
	}
	return sad, true
}

func vp9PlaneWindowFits(buf []byte, stride, x0, y0, w, h int) bool {
	if len(buf) == 0 || stride <= 0 || x0 < 0 || y0 < 0 || w <= 0 || h <= 0 {
		return false
	}
	if x0+w > stride {
		return false
	}
	off := y0*stride + x0
	last := off + (h-1)*stride + w
	return off >= 0 && last <= len(buf)
}

func vp9PlaneWindowOffsetFits(buf []byte, stride, off, w, h int) bool {
	if len(buf) == 0 || stride <= 0 || off < 0 || w <= 0 || h <= 0 {
		return false
	}
	if w > stride {
		return false
	}
	last := off + (h-1)*stride + w
	return last <= len(buf)
}

// vp9EnsureSBPartitionChosen runs encoder.ChoosePartitioning for the 64x64 SB
// containing (miRow, miCol) iff it hasn't been computed this frame.
// Writes the partition tree into e.varPartGrid and marks
// e.varPartSBComputed[sbIdx] = true.
//
// libvpx ref: vp9_encodeframe.c:1253-1763 (choose_partitioning called
// once per SB from encode_rtc_frame at line 5470).
func (e *VP9Encoder) vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol int,
	key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) bool {
	miGridLen := miRows * miCols
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	// Lazy alloc: first activation of the libvpx picker on this encoder
	// instance grows the per-SB tracking slices to fit the current frame
	// dimensions. Subsequent calls reuse the capacity. The per-frame
	// reset of these buffers is handled by the frame-setup path
	// (vp9_encoder.go:3327-3340) — wiping the grid on every per-MI call
	// would destroy partition decisions stamped by earlier SBs in the
	// same frame (libvpx's xd->mi[]->sb_type grid is persistent across
	// the encode walk).
	e.varPartGrid = buffers.EnsureLenZeroTail(e.varPartGrid, miGridLen)
	e.varPartSBComputed = buffers.EnsureLenZeroTail(e.varPartSBComputed, sbCount)
	e.varPartSBUseMvPart = buffers.EnsureLenZeroTail(e.varPartSBUseMvPart, sbCount)
	e.varPartSBMvPart = buffers.EnsureLen(e.varPartSBMvPart, sbCount)
	e.varPartSBPredValid = buffers.EnsureLenZeroTail(e.varPartSBPredValid, sbCount)
	e.varPartSBPredLast = buffers.EnsureLen(e.varPartSBPredLast, sbCount)
	e.varPartSBVarLow = buffers.EnsureLen(e.varPartSBVarLow, sbCount)
	e.varPartSBColorSensitivity = buffers.EnsureLenZeroTail(
		e.varPartSBColorSensitivity, sbCount)
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if sbIdx < 0 || sbIdx >= len(e.varPartSBComputed) {
		return false
	}
	if e.varPartSBComputed[sbIdx] {
		return true
	}
	if sbIdx >= 0 && sbIdx < len(e.varPartSBVarLow) {
		e.varPartSBVarLow[sbIdx] = [25]uint8{}
	}

	args := encoder.ChoosePartitioningArgs{
		MiGrid:                 e.varPartGrid,
		MiRows:                 miRows,
		MiCols:                 miCols,
		MiRow:                  sbMiRow,
		MiCol:                  sbMiCol,
		Speed:                  e.vp9SpeedFeatureCPUUsed(),
		ShortCircuitLowTempVar: e.sf.ShortCircuitLowTempVar,
		PartitionRefFrame:      vp9dec.LastFrame,
		VarianceLow:            &e.varPartSBVarLow[sbIdx],
		VarianceTree:           &e.varPartTreeScratch,
		VarianceTreeLowRes:     &e.varPartTreeLowResScratch,
		NoiseEstimateEnabled:   e.noiseEstimate.Enabled,
		NoiseLevel:             e.noiseEstimate.ExtractLevel(),
		// libvpx vp9_encodeframe.c:1379 feeds set_vbp_thresholds with
		// cpi->sf.variance_part_thresh_mult. The configurator sets this
		// to 2 for resolutions w*h >= 640*360 (vp9_speed_features.c:813),
		// otherwise 1 (vp9_speed_features.c:479). Read the live SF value
		// rather than hard-coding 1 so the threshold base scales with
		// resolution the libvpx way.
		VariancePartThreshMult: e.sf.VariancePartThreshMult,
		// libvpx vp9_encodeframe.c:1310 — use_4x4_partition is gated on
		// !sf->nonrd_keyframe. At speed >= 8 the realtime configurator
		// sets sf->nonrd_keyframe = 1 (vp9_speed_features.c:751-757),
		// which suppresses the keyframe 4x4-leaf split. Thread the
		// speed-feature flag through so encoder.ChoosePartitioning respects
		// it on the keyframe walker.
		NonRdKeyframe: e.sf.NonrdKeyframe != 0,
	}
	switch {
	case key != nil && key.img != nil && key.dq != nil:
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, 0)
		if len(src) == 0 || srcStride <= 0 {
			return false
		}
		x0 := sbMiCol * common.MiSize
		y0 := sbMiRow * common.MiSize
		if x0 >= srcW || y0 >= srcH {
			return false
		}
		args.PlaneSrc = src
		args.PlaneSrcOff = y0*srcStride + x0
		args.SrcStride = srcStride
		args.FrameWidth = srcW
		args.FrameHeight = srcH
		args.IsKeyFrame = true
		// libvpx feeds set_vbp_thresholds with cm->base_qindex
		// (vp9_encodeframe.c:1379), not a per-segment dequant. Read it
		// straight from the header so segmentation deltas on segment 0
		// don't perturb the threshold derivation.
		if key.hdr != nil {
			args.BaseQIndex = int(key.hdr.Quant.BaseQindex)
		}
	case inter != nil && inter.img != nil && inter.dq != nil:
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
		if len(src) == 0 || srcStride <= 0 {
			return false
		}
		x0 := sbMiCol * common.MiSize
		y0 := sbMiRow * common.MiSize
		if x0 >= srcW || y0 >= srcH {
			return false
		}
		args.PlaneSrc = src
		args.PlaneSrcOff = y0*srcStride + x0
		args.SrcStride = srcStride
		args.FrameWidth = srcW
		args.FrameHeight = srcH
		args.IsKeyFrame = false
		// libvpx feeds set_vbp_thresholds with cm->base_qindex
		// (vp9_encodeframe.c:1379). See keyframe branch above for
		// motivation.
		args.BaseQIndex = inter.baseQindex
		if e.opts.AQMode == VP9AQCyclicRefresh &&
			e.vp9HeaderScratch.Seg.Enabled {
			segmentID := e.vp9PartitionSegmentID(sbMiRow, sbMiCol,
				e.vp9StaticSegmentIDForMap(), inter.img, inter)
			if encoder.CyclicRefreshSegmentIDBoosted(segmentID) {
				args.CyclicRefreshSegmentIdBoosted = true
				args.BaseQIndex = vp9dec.GetSegmentQindex(
					&e.vp9HeaderScratch.Seg, int(segmentID), inter.baseQindex)
			}
		}
		args.AvgFrameQIndexInter = int(e.rc.avgFrameQIndexInter)
		args.UseSourceSAD = e.sf.UseSourceSad != 0
		args.ScreenContent = e.opts.ScreenContentMode == int8(VP9ScreenContentScreen)
		e.vp9EnsureSBLastHighContentCached(miRows, miCols, sbMiRow, sbMiCol)
		if sadState, ok := e.vp9SourceSADState(inter.img,
			miRows, miCols, sbMiRow, sbMiCol); ok {
			args.ContentState = sadState.ContentState
			args.ZeroTempSADSource = sadState.ZeroTempSADSource
		}
		// Inter predictor. libvpx vp9_encodeframe.c:1450-1497:
		//   if (cpi->oxcf.speed >= 8 && !low_res &&
		//       x->content_state_sb != kVeryHighSad) {
		//     y_sad = sdf(src, pre);              // zero-MV SAD only
		//   } else {
		//     const MV dummy_mv = { 0, 0 };
		//     y_sad = int-pro motion_estimation(...); // sets mi->mv[0]
		//   }
		//   vp9_build_inter_predictors_sb(xd, mi_row, mi_col, BLOCK_64X64);
		//   d = xd->plane[0].dst.buf;
		//
		// low_res predicate: libvpx vp9_encodeframe.c:1311.
		lowRes := srcW <= 352 && srcH <= 288
		if refSlot, ok := e.vp9PartitionReferenceSlot(vp9dec.LastFrame); ok {
			refPx, refStride, refW, refH := vp9ReferenceVisiblePlane(
				&e.refFrames[refSlot], 0)
			if len(refPx) > 0 && refStride > 0 &&
				x0 < refW && y0 < refH {
				wired := false
				// libvpx vp9_encodeframe.c:1456-1458:
				//   y_sad = int-pro motion_estimation(cpi, x, bsize,
				//                                         mi_row, mi_col,
				//                                         &dummy_mv);
				// Followed by vp9_build_inter_predictors_sb (line 1487)
				// which lands the resulting MV's luma prediction in
				// xd->plane[0].dst.buf. We fire this on low_res — the
				// libvpx condition for entering the int_pro branch over
				// the zero-MV sdf branch at speed >= 8
				// (vp9_encodeframe.c:1451).
				if lowRes && e.lastBorderedValid &&
					e.lastBordered.W == refW && e.lastBordered.H == refH {
					// Build the per-frame border-padded source mirror
					// once per frame; reuse across SBs.
					if !e.intProSrcBorderedValid ||
						e.intProSrcBordered.W != srcW ||
						e.intProSrcBordered.H != srcH {
						common.YV12BuildBorderedPlane(&e.intProSrcBordered,
							src, srcStride, srcW, srcH,
							common.VP9EncBorderInPixels)
						e.intProSrcBorderedValid = true
					}
					// Wire int_pro motion search against the bordered
					// LAST plane. The visible (mi_row, mi_col) origin
					// inside the padded buffer is (Border+y0,
					// Border+x0) so refOff - (bw>>1) stays inside the
					// allocation for the selected sub-bsize; the
					// BLOCK_64X64 worst case still fits inside the
					// encoder border
					// (libvpx vp9/encoder/vp9_mcomp.c:2317-2320).
					srcOriginX := e.intProSrcBordered.OriginX()
					srcOriginY := e.intProSrcBordered.OriginY()
					refOriginX := e.lastBordered.OriginX()
					refOriginY := e.lastBordered.OriginY()
					srcStrideB := e.intProSrcBordered.Stride
					refStrideB := e.lastBordered.Stride
					subBsize := encoder.GetEstimatedPredSubBsize(sbMiRow,
						sbMiCol, miRows, miCols)
					estIn := &encoder.GetEstimatedPredInterInput{
						Bsize:                  subBsize,
						Src:                    e.intProSrcBordered.Pixels,
						SrcOff:                 (srcOriginY+y0)*srcStrideB + (srcOriginX + x0),
						SrcStride:              srcStrideB,
						LastRef:                e.lastBordered.Pixels,
						LastRefOff:             (refOriginY+y0)*refStrideB + (refOriginX + x0),
						LastRefStride:          refStrideB,
						Speed:                  e.vp9SpeedFeatureCPUUsed(),
						ShortCircuitLowTempVar: e.sf.ShortCircuitLowTempVar != 0,
						// MvLimits: full-pel limits derived from the
						// SB origin's distance to the bordered frame
						// edges (mirrors libvpx's
						// vp9_set_mv_search_range output for the
						// BLOCK_64X64 SB at (mi_row, mi_col); see
						// vp9_encoder.c set_mv_limits at the call
						// site).
						MvLimits: encoder.MvLimits{
							ColMin: -(x0 + common.VP9EncBorderInPixels),
							ColMax: refW - x0 + common.VP9EncBorderInPixels,
							RowMin: -(y0 + common.VP9EncBorderInPixels),
							RowMax: refH - y0 + common.VP9EncBorderInPixels,
						},
					}
					// encoder.GetEstimatedPred dispatches to the inter
					// path for !isKeyFrame, which runs int-pro motion
					// search + ref-frame selection, then drives the 64x64
					// luma BILINEAR convolve port of
					// vp9_build_inter_predictors_sb.
					// libvpx: vp9_reconinter.c:253-258.
					chosenRef, intProMV := encoder.GetEstimatedPred(false, estIn,
						e.intProEstPred[:])
					args.PartitionMV = intProMV
					if chosenRef == encoder.RefGolden {
						args.PartitionRefFrame = vp9dec.GoldenFrame
					} else {
						args.PartitionRefFrame = vp9dec.LastFrame
					}
					if sbIdx >= 0 && sbIdx < len(e.varPartSBUseMvPart) {
						// libvpx choose_partitioning stores the int-pro MV
						// in x->sb_mv{row,col}_part and makes the later
						// nonrd NEWMV search reuse it instead of running a
						// fresh full-pel search (vp9_pickmode.c:217-224).
						e.varPartSBUseMvPart[sbIdx] = true
						e.varPartSBMvPart[sbIdx] = intProMV
						if chosenRef != encoder.RefGolden &&
							sbIdx < len(e.varPartSBPredValid) {
							// When LAST (or source-altref-as-LAST) wins
							// the partition prepass, libvpx also writes
							// x->pred_mv[LAST_FRAME] for vp9_mv_pred's
							// optional third candidate.
							e.varPartSBPredValid[sbIdx] = true
							e.varPartSBPredLast[sbIdx] = intProMV
						}
					}
					args.PlaneDst = e.intProEstPred[:]
					args.PlaneDstOff = 0
					args.DstStride = 64
					wired = true
				}
				if !wired {
					// Fallback: byte-exact with libvpx's "speed>=8
					// && !low_res && content_state != kVeryHighSad"
					// zero-MV SAD-only branch — the predictor stays at
					// the LAST plane at (mi_row, mi_col).
					args.PlaneDst = refPx
					args.PlaneDstOff = y0*refStride + x0
					args.DstStride = refStride
				}
			}
		}
		args.HighSourceSAD = e.rc.highSourceSAD
		// libvpx ref: vp9_encodeframe.c:1284 (force_64_split feeder).
	default:
		return false
	}

	encoder.ChoosePartitioning(args)
	if inter != nil {
		e.vp9RecordVarPartSBColorSensitivity(miRows, miCols, sbMiRow, sbMiCol,
			sbIdx, inter, &args)
	}
	e.varPartSBComputed[sbIdx] = true
	e.varPartFrameValid = true
	return true
}

func (e *VP9Encoder) vp9PartitionReferenceSlot(refFrame int8) (int, bool) {
	slot, ok := vp9EncoderReferenceSlot(refFrame)
	if !ok {
		return 0, false
	}
	if !e.refFrames[slot].valid {
		return 0, false
	}
	return slot, true
}

// vp9VarPartDecisionFor reads xd->mi[(miRow*miCols+miCol)].sb_type and
// returns the libvpx subsize the walker should consume. Verbatim port
// of vp9/encoder/vp9_encodeframe.c:5007-5010 (nonrd_use_partition):
//
//	if (mi_row >= cm->mi_rows || mi_col >= cm->mi_cols) return;
//	subsize = (bsize >= BLOCK_8X8) ? mi[0]->sb_type : BLOCK_4X4;
//	partition = partition_lookup[bsl][subsize];
//
// Returns (BlockInvalid, false) when partition_lookup yields
// PARTITION_NONE (caller stays at bsize) or PARTITION_INVALID (defensive
// fallback). Returns (subsize, true) for PARTITION_HORZ / VERT / SPLIT
// — the walker re-derives PartitionType via PartitionLookup[bsl][target].
//
// libvpx ref: vp9_encodeframe.c:4993-5100 nonrd_use_partition.
func (e *VP9Encoder) vp9VarPartDecisionFor(miCols, miRow, miCol int,
	bsize common.BlockSize,
) (common.BlockSize, bool) {
	// Verbatim port of vp9/encoder/vp9_encodeframe.c:5007-5010
	// (nonrd_use_partition):
	//
	//   if (mi_row >= cm->mi_rows || mi_col >= cm->mi_cols) return;
	//   subsize = (bsize >= BLOCK_8X8) ? mi[0]->sb_type : BLOCK_4X4;
	//   partition = partition_lookup[bsl][subsize];
	//
	// The walker (writeVP9ModesSb) re-derives the PartitionType from
	// PartitionLookup[bsl][target], so we return the libvpx `subsize`
	// directly when partition != PARTITION_NONE.
	//
	// Critically, we MUST NOT treat picked==Block4x4 (enum 0) as
	// "unstamped cell": that conflates a legitimate libvpx
	// PARTITION_SPLIT leaf at bsize=BLOCK_8X8 with the zero-init grid
	// sentinel. The varPartSBComputed flag (managed by
	// vp9EnsureSBPartitionChosen) is the only valid stamped oracle, and
	// the picker stamps the upper-left mi of every terminal block via
	// set_block_size (vp9_encodeframe.c:340), so reads at the upper-left
	// of the outer bsize always see a real stamp once the SB has been
	// computed.
	if len(e.varPartGrid) == 0 || !e.varPartFrameValid {
		return common.BlockInvalid, false
	}
	idx := miRow*miCols + miCol
	if idx < 0 || idx >= len(e.varPartGrid) {
		return common.BlockInvalid, false
	}
	// libvpx: subsize = (bsize >= BLOCK_8X8) ? mi[0]->sb_type : BLOCK_4X4;
	var subsize common.BlockSize
	if bsize >= common.Block8x8 {
		subsize = e.varPartGrid[idx].SbType
	} else {
		subsize = common.Block4x4
	}
	// Map outer bsize to PartitionLookup row: BLOCK_4X4..BLOCK_64X64 →
	// row 0..4. b_width_log2_lookup gives the row index directly for the
	// square outer sizes nonrd_use_partition is ever called with.
	if bsize >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	if bsl < 0 || bsl >= len(common.PartitionLookup) {
		return common.BlockInvalid, false
	}
	if subsize >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	// libvpx: partition = partition_lookup[bsl][subsize];
	partition := common.PartitionLookup[bsl][subsize]
	switch partition {
	case common.PartitionNone:
		// libvpx stamped bsize at this cell — encode the whole block as
		// a single leaf. Return (bsize, true) so the caller commits to
		// PARTITION_NONE (PartitionLookup[bsl][bsize] = PartitionNone);
		// returning (BlockInvalid, false) here would let the dispatch
		// fall through to a non-libvpx heuristic and diverge.
		return bsize, true
	case common.PartitionHorz, common.PartitionVert, common.PartitionSplit:
		// Walker derives this partition back from
		// PartitionLookup[bsl][subsize]; return subsize to feed that.
		return subsize, true
	default:
		// PartitionInvalid: defensive fallback for an illegal subsize
		// at this outer bsize.
		return common.BlockInvalid, false
	}
}

// vp9NonrdSelectPartitionBlockSize ports the partition-size dispatch of
// libvpx nonrd_select_partition (vp9_encodeframe.c:4859-4898) for govpx's
// recursive writeVP9ModesSb walker. Mode picking stays in
// pickVP9InterReferenceMode; this helper only decides how far to split.
func (e *VP9Encoder) vp9NonrdSelectPartitionBlockSize(
	inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (common.BlockSize, bool) {
	if inter == nil || !e.varPartFrameValid || len(e.varPartGrid) == 0 {
		return common.BlockInvalid, false
	}
	idx := miRow*miCols + miCol
	if idx < 0 || idx >= len(e.varPartGrid) {
		return common.BlockInvalid, false
	}
	subsize := e.varPartGrid[idx].SbType
	if bsize < common.Block8x8 {
		subsize = common.Block4x4
	}
	if bsize >= common.BlockSizes || subsize >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	if bsl < 0 || bsl >= len(common.PartitionLookup) {
		return common.BlockInvalid, false
	}
	partition := common.PartitionLookup[bsl][subsize]
	subsizeRef := common.Block16x16
	if e.sf.AdaptPartitionSourceSad != 0 {
		subsizeRef = common.Block8x8
	}
	tryMLPick := func() (common.BlockSize, bool) {
		if mlCtx := e.vp9MLPickPartitionEntry(inter, miRows, miCols,
			miRow, miCol); mlCtx != nil {
			if picked, ok := e.vp9NonrdPickPartition(mlCtx, miRows, miCols,
				miRow, miCol, bsize); ok {
				return picked, true
			}
			if picked, ok := e.vp9NonrdPickPartitionRDFallback(inter, tile,
				partitionProbs, miRows, miCols, miRow, miCol, bsize); ok {
				return picked, true
			}
		}
		return common.BlockInvalid, false
	}
	switch {
	case bsize == common.Block32x32 && subsize == common.Block32x32:
		return tryMLPick()
	case bsize == common.Block32x32 && partition != common.PartitionNone &&
		subsize >= subsizeRef:
		return tryMLPick()
	case bsize == common.Block16x16 && partition != common.PartitionNone:
		return tryMLPick()
	default:
		return e.vp9VarPartDecisionFor(miCols, miRow, miCol, bsize)
	}
}

func (e *VP9Encoder) pickVP9CBRVariancePartitionBlockSize(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) (common.BlockSize, bool) {
	if !e.vp9CBRVariancePartitionEnabled(inter) {
		return common.BlockInvalid, false
	}
	// When the libvpx choose_partitioning gate is enabled, populate the
	// per-SB partition cache on first call into this SB and read the
	// partition decision back from e.varPartGrid. Falls through to the
	// variance picker below when the gate is off.
	//
	// libvpx ref: vp9/encoder/vp9_encodeframe.c:5470 nonrd_use_partition
	// reads xd->mi[]->sb_type to drive the encode walk.
	if e.vp9RealtimeVariancePartitionEnabled() &&
		e.vp9EnsureSBPartitionChosen(miRows, miCols, miRow, miCol, nil, inter) {
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
	refSlot, ok := e.vp9InterReferenceSlot(inter, vp9dec.LastFrame)
	if !ok {
		return common.BlockInvalid, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(&e.refFrames[refSlot], 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return common.BlockInvalid, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if !encoder.VisibleBlockFits(x0, y0, blockW, blockH, srcW, srcH) ||
		!encoder.VisibleBlockFits(x0, y0, blockW, blockH, refW, refH) {
		return common.BlockInvalid, false
	}
	if bsize == common.Block64x64 {
		sad := encoder.BlockSAD(src, srcStride, ref, refStride,
			x0, y0, x0, y0, blockW, blockH, ^uint64(0))
		sadThreshold := encoder.CBRVariancePartitionSADThreshold(inter.dq.Y[0][1],
			srcW, srcH)
		if sad < sadThreshold {
			return common.BlockInvalid, false
		}
	}
	threshold := encoder.CBRVariancePartitionThreshold(inter.dq.Y[0][1],
		srcW, srcH, bsize, e.rc.avgFrameQIndexInter)
	variance := encoder.BlockDiffVariance(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH)
	if variance < threshold {
		return common.BlockInvalid, false
	}
	halfW := blockW >> 1
	halfH := blockH >> 1
	if miRow+(blockMiH>>1) < miRows {
		left := encoder.BlockDiffVariance(src, srcStride, ref, refStride,
			x0, y0, x0, y0, halfW, blockH)
		right := encoder.BlockDiffVariance(src, srcStride, ref, refStride,
			x0+halfW, y0, x0+halfW, y0, halfW, blockH)
		if left < threshold && right < threshold {
			return vertSize, true
		}
	}
	if miCol+(blockMiW>>1) < miCols {
		top := encoder.BlockDiffVariance(src, srcStride, ref, refStride,
			x0, y0, x0, y0, blockW, halfH)
		bottom := encoder.BlockDiffVariance(src, srcStride, ref, refStride,
			x0, y0+halfH, x0, y0+halfH, blockW, halfH)
		if top < threshold && bottom < threshold {
			return horzSize, true
		}
	}
	return splitSize, true
}

// vp9CBRVariancePartitionEnabled mirrors libvpx's choose_partitioning gate
// for inter frames. libvpx dispatches via partition_search_type ==
// VAR_BASED_PARTITION (vp9/encoder/vp9_encodeframe.c:5304-5311); the gate is
// NOT rc_mode-specific, NOT gated on drop-frame-allowed, and NOT gated on a
// fixed public quantizer. At speed >= 6 (vp9_speed_features.c:667) the
// configurator sets the type unconditionally regardless of VPX_CBR / VPX_VBR
// / VPX_CQ / VPX_Q. The dispatch is purely on partition_search_type. The
// !vp9FixedPublicQuantizer() predicate was previously here but has no libvpx
// counterpart and is removed for verbatim-libvpx faithfulness; the remaining
// predicates (inter != nil, dq != nil, !lossless, rc.enabled, RealtimeVar)
// guard the govpx-internal preconditions that vp9EnsureSBPartitionChosen
// inherits from libvpx's xd->dq / cm->frame_type / encode-state lifecycle.
//
// libvpx: vp9/encoder/vp9_speed_features.c:667, vp9_encodeframe.c:5304-5311.
func (e *VP9Encoder) vp9CBRVariancePartitionEnabled(inter *vp9InterEncodeState) bool {
	if inter == nil || inter.dq == nil || inter.lossless ||
		!e.rc.enabled || !e.vp9RealtimeVariancePartitionEnabled() {
		return false
	}
	return true
}

func (e *VP9Encoder) vp9FixedPublicQuantizer() bool {
	if e.opts.Quantizer != 0 {
		return true
	}
	minQ, maxQ, _ := vp9NormalizedPublicQuantizers(e.opts)
	return minQ == maxQ && minQ > 0
}

type vp9InterPartitionRD struct {
	target            common.BlockSize
	predInterpFilter  vp9dec.InterpFilter
	rate              int
	distortion        uint64
	score             uint64
	predFilterPresent bool
}

func (e *VP9Encoder) scoreVP9InterPartitionLeaf(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterPartitionRD, bool) {
	decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, bsize)
	if !ok {
		return vp9InterPartitionRD{}, false
	}
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize,
		vp9InterModeDecisionMi(bsize, decision))
	// Entropy-context stamp (encode_sb): libvpx's partition recursion encode_b's
	// each committed non-last split child (pc_tree->index != 3) before the next
	// sibling's rd_pick_sb_modes, so the sibling's seed (memcpy(t_above,
	// pd->above_context), vp9_rdopt.c:2120-2121,872) reads the just-coded block's
	// plane entropy context (vp9_encodeframe.c:4163 encode_sb +
	// save_context/restore_context :2167-2218). govpx's deep-RD recursion scores
	// without an intervening encode_b, so stamp the committed leaf's context here;
	// the partition-context snapshot/restore now carries the entropy context
	// across trials, and pickVP9InterPartitionRD's final restore is seg-only so
	// the stamp persists for the next sibling. Gated behind the deep sub-8x8 flag
	// (production keeps the model stand-in and never reaches this stamp).
	if vp9InterUseDeepRDSub8x8 {
		e.vp9CommitInterLeafEntropyContext(inter, miRows, miCols, miRow, miCol,
			bsize, decision)
	}
	// SEARCH->WRITE replay: when the depth-first RD recursion is the active
	// partition decision, commit this leaf's full picker decision keyed by
	// (miRow, miCol, bsize) so the bitstream write descent replays it instead
	// of re-picking with a different x->pred_mv context. The recursion re-runs
	// the winning arm last, so the final store at each key the writer descends
	// is the committed leaf's decision; this mirrors the fillVP9MiGrid commit
	// above, which already populates the mi grid the writer reads back. Gated:
	// production (flag off) never stores (cache slice is nil → no-op).
	// libvpx: rd_pick_partition's encode_b writes the chosen mbmi into
	// mi_grid_visible once; write_modes_b replays it (vp9_bitstream.c).
	if vp9InterUseDeepRDPartition {
		e.storeVP9LeafInterRDDecision(miRow, miCol, bsize, decision)
	}
	predFilter := decision.interpFilter
	if decision.intra || predFilter >= vp9dec.InterpSwitchable {
		predFilter = vp9dec.InterpEighttap
	}
	return vp9InterPartitionRD{
		target:            bsize,
		predInterpFilter:  predFilter,
		rate:              decision.rate,
		distortion:        decision.distortion,
		score:             decision.score,
		predFilterPresent: true,
	}, true
}

func (e *VP9Encoder) updateVP9PartitionContextForChoice(miRow, miCol int,
	root common.BlockSize, partition common.PartitionType, subsize common.BlockSize,
) {
	if root < common.Block8x8 {
		return
	}
	if root != common.Block8x8 && partition == common.PartitionSplit {
		return
	}
	bsl := int(common.BWidthLog2Lookup[root])
	bs := (1 << uint(bsl)) / 4
	vp9dec.UpdatePartitionContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
}

func (e *VP9Encoder) scoreVP9InterPartitionNone(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
	hasRows, hasCols bool, qindex int,
) (vp9InterPartitionRD, bool) {
	rd, ok := e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
		miRow, miCol, root)
	if !ok {
		return vp9InterPartitionRD{}, false
	}
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	// libvpx rd_pick_partition adds the UNCONDITIONAL full-tree partition
	// cost (cpi->partition_cost[pl][PARTITION_NONE], vp9_encodeframe.c:3826),
	// not the writer's hasRows/hasCols-clamped form. See
	// vp9_fullrd_partition_cost.go.
	rd.rate += RDPartitionCost(rateCostProbs, ctx, common.PartitionNone)
	rd.score = e.vp9InterModeScore(rd.distortion, rd.rate, qindex)
	e.updateVP9PartitionContextForChoice(miRow, miCol, root,
		common.PartitionNone, root)
	return rd, true
}

func (e *VP9Encoder) scoreVP9InterPartitionRect(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root, child common.BlockSize,
	partition common.PartitionType, rowOff, colOff int,
	hasRows, hasCols bool, qindex int,
) (vp9InterPartitionRD, bool) {
	first, ok := e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
		miRow, miCol, child)
	if !ok {
		return vp9InterPartitionRD{}, false
	}
	rate := first.rate
	distortion := first.distortion
	if child >= common.Block8x8 {
		second, ok := e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
			miRow+rowOff, miCol+colOff, child)
		if !ok {
			return vp9InterPartitionRD{}, false
		}
		rate += second.rate
		distortion += second.distortion
	}
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	// libvpx rd_pick_partition uses the unconditional full-tree partition
	// cost for HORZ/VERT (cpi->partition_cost[pl][partition],
	// vp9_encodeframe.c:4035, :4085), not the writer's clamped form.
	rate += RDPartitionCost(rateCostProbs, ctx, partition)
	e.updateVP9PartitionContextForChoice(miRow, miCol, root, partition, child)
	return vp9InterPartitionRD{
		target:     child,
		rate:       rate,
		distortion: distortion,
		score:      e.vp9InterModeScore(distortion, rate, qindex),
	}, true
}

func (e *VP9Encoder) scoreVP9InterPartitionSplit(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root, child common.BlockSize,
	hasRows, hasCols bool, qindex int,
) (vp9InterPartitionRD, bool) {
	rate := 0
	var distortion uint64
	if child < common.Block8x8 {
		rd, ok := e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
			miRow, miCol, child)
		if !ok {
			return vp9InterPartitionRD{}, false
		}
		rate += rd.rate
		distortion += rd.distortion
	} else {
		stepMi := int(common.Num8x8BlocksWideLookup[child])
		for rowOff := 0; rowOff <= stepMi; rowOff += stepMi {
			for colOff := 0; colOff <= stepMi; colOff += stepMi {
				if miRow+rowOff >= miRows || miCol+colOff >= miCols {
					continue
				}
				rd, ok := e.pickVP9InterPartitionRD(inter, tile, rateCostProbs,
					miRows, miCols, miRow+rowOff, miCol+colOff, child)
				if !ok {
					return vp9InterPartitionRD{}, false
				}
				rate += rd.rate
				distortion += rd.distortion
			}
		}
	}
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)
	// libvpx rd_pick_partition adds the unconditional full-tree PARTITION_SPLIT
	// cost (cpi->partition_cost[pl][PARTITION_SPLIT], vp9_encodeframe.c:3969),
	// not the writer's clamped form.
	rate += RDPartitionCost(rateCostProbs, ctx, common.PartitionSplit)
	e.updateVP9PartitionContextForChoice(miRow, miCol, root,
		common.PartitionSplit, child)
	return vp9InterPartitionRD{
		target:     child,
		rate:       rate,
		distortion: distortion,
		score:      e.vp9InterModeScore(distortion, rate, qindex),
	}, true
}

func (e *VP9Encoder) pickVP9InterPartitionRD(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
) (vp9InterPartitionRD, bool) {
	if root < common.Block8x8 {
		return e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
			miRow, miCol, root)
	}
	horzSize, vertSize, splitSize, ok := encoder.InterRDPartitionSizes(root)
	if !ok {
		return e.scoreVP9InterPartitionLeaf(inter, tile, miRows, miCols,
			miRow, miCol, root)
	}

	bsl := int(common.BWidthLog2Lookup[root])
	bs := (1 << uint(bsl)) / 4
	hasRows := miRow+bs < miRows
	hasCols := miCol+bs < miCols
	qindex := e.vp9EncoderModeDecisionQIndex()
	reconSnap, ok := e.saveVP9PartitionReconSnapshot(miRow, miCol, root)
	if !ok {
		return vp9InterPartitionRD{}, false
	}
	defer e.releaseVP9PartitionReconSnapshot(reconSnap)
	savedRef := inter.ref
	savedPredFilter := inter.predInterpFilter
	savedPredFilterValid := inter.predFilterValid
	pickPredSnap := e.saveVP9MLPickPredSnapshot(inter, miRows, miCols,
		miRow, miCol)
	ctxSnap, ctxOK := e.snapshotVP9PartitionContexts(miRow, miCol, root)
	var miSaved [64]vp9dec.NeighborMi
	miRowsSaved, miColsSaved, miOK := e.snapshotVP9MiRect(miRows, miCols,
		miRow, miCol, int(common.Num8x8BlocksHighLookup[root]),
		int(common.Num8x8BlocksWideLookup[root]), miSaved[:])
	if !ctxOK || !miOK {
		e.restoreVP9MLPickPredSnapshot(pickPredSnap)
		e.restoreVP9PartitionReconSnapshot(reconSnap)
		inter.ref = savedRef
		inter.predInterpFilter = savedPredFilter
		inter.predFilterValid = savedPredFilterValid
		return vp9InterPartitionRD{}, false
	}
	restoreBase := func() {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol,
			miRowsSaved, miColsSaved, miSaved[:])
		e.restoreVP9PartitionContexts(ctxSnap)
		e.restoreVP9MLPickPredSnapshot(pickPredSnap)
		e.restoreVP9PartitionReconSnapshotPixels(reconSnap)
		inter.ref = savedRef
		inter.predInterpFilter = savedPredFilter
		inter.predFilterValid = savedPredFilterValid
	}

	// store_pred_mv/load_pred_mv thread (vp9_encodeframe.c:3913/3932/4071/4121):
	// snapshot the parent's pred_mv at node entry, restore it before the NONE
	// search (so NONE's vp9_mv_pred candidate[2] sees the parent value), capture
	// the post-NONE pred_mv (store_pred_mv on ctx==pc_tree->none), and reload it
	// before each child arm (load_pred_mv). Gated: production (flag off) never
	// touches fullRDPredMv, so this snapshot/restore is inert there.
	entryPredMv := e.fullRDPredMv
	var nonePredMv [vp9dec.MaxRefFrames]vp9dec.MV
	loadNonePredMv := func() {
		if vp9InterUseDeepRDSub8x8 {
			e.fullRDPredMv = nonePredMv
		}
	}

	bestSet := false
	var best vp9InterPartitionRD
	updateBest := func(rd vp9InterPartitionRD) {
		if !bestSet || rd.score < best.score ||
			(rd.score == best.score && rd.rate < best.rate) {
			best = rd
			bestSet = true
		}
	}
	consider := func(score func() (vp9InterPartitionRD, bool)) {
		restoreBase()
		loadNonePredMv()
		rd, ok := score()
		if ok {
			updateBest(rd)
		}
	}
	restoreBase()
	if vp9InterUseDeepRDSub8x8 {
		e.fullRDPredMv = entryPredMv
	}
	noneRD, noneOK := e.scoreVP9InterPartitionNone(inter, tile, rateCostProbs,
		miRows, miCols, miRow, miCol, root, hasRows, hasCols, qindex)
	// store_pred_mv(x, ctx==pc_tree->none): capture x->pred_mv as left by the
	// NONE mode loop's single_motion_search (vp9_encodeframe.c:3913).
	nonePredMv = e.fullRDPredMv
	if noneOK {
		updateBest(noneRD)
	}
	predFromNone := root == common.Block8x8 && e.sf.AdaptivePredInterpFilter != 0 &&
		noneOK && noneRD.predFilterPresent
	if hasRows && hasCols {
		consider(func() (vp9InterPartitionRD, bool) {
			if predFromNone {
				inter.predInterpFilter = noneRD.predInterpFilter
				inter.predFilterValid = true
			}
			return e.scoreVP9InterPartitionSplit(inter, tile, rateCostProbs,
				miRows, miCols, miRow, miCol, root, splitSize,
				hasRows, hasCols, qindex)
		})
	}
	if hasRows {
		consider(func() (vp9InterPartitionRD, bool) {
			if predFromNone {
				inter.predInterpFilter = noneRD.predInterpFilter
				inter.predFilterValid = true
			}
			return e.scoreVP9InterPartitionRect(inter, tile, rateCostProbs,
				miRows, miCols, miRow, miCol, root, horzSize,
				common.PartitionHorz, bs, 0, hasRows, hasCols, qindex)
		})
	}
	if hasCols {
		consider(func() (vp9InterPartitionRD, bool) {
			if predFromNone {
				inter.predInterpFilter = noneRD.predInterpFilter
				inter.predFilterValid = true
			}
			return e.scoreVP9InterPartitionRect(inter, tile, rateCostProbs,
				miRows, miCols, miRow, miCol, root, vertSize,
				common.PartitionVert, 0, bs, hasRows, hasCols, qindex)
		})
	}
	if !bestSet {
		restoreBase()
		e.partitionReconScratchTop = reconSnap.top
		return vp9InterPartitionRD{}, false
	}
	restoreBase()
	var committed vp9InterPartitionRD
	switch best.target {
	case root:
		// NONE re-run: reseed pred_mv from the parent value (entry) so the
		// re-run's vp9_mv_pred reproduces the search's NEWMV exactly.
		if vp9InterUseDeepRDSub8x8 {
			e.fullRDPredMv = entryPredMv
		}
		committed, ok = e.scoreVP9InterPartitionNone(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, root, hasRows, hasCols, qindex)
	case splitSize:
		loadNonePredMv()
		if predFromNone {
			inter.predInterpFilter = noneRD.predInterpFilter
			inter.predFilterValid = true
		}
		committed, ok = e.scoreVP9InterPartitionSplit(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, root, splitSize,
			hasRows, hasCols, qindex)
	case horzSize:
		loadNonePredMv()
		if predFromNone {
			inter.predInterpFilter = noneRD.predInterpFilter
			inter.predFilterValid = true
		}
		committed, ok = e.scoreVP9InterPartitionRect(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, root, horzSize,
			common.PartitionHorz, bs, 0, hasRows, hasCols, qindex)
	case vertSize:
		loadNonePredMv()
		if predFromNone {
			inter.predInterpFilter = noneRD.predInterpFilter
			inter.predFilterValid = true
		}
		committed, ok = e.scoreVP9InterPartitionRect(inter, tile, rateCostProbs,
			miRows, miCols, miRow, miCol, root, vertSize,
			common.PartitionVert, 0, bs, hasRows, hasCols, qindex)
	default:
		ok = false
	}
	if !ok {
		restoreBase()
		e.partitionReconScratchTop = reconSnap.top
		return vp9InterPartitionRD{}, false
	}
	e.restoreVP9MLPickPredSnapshot(pickPredSnap)
	inter.predInterpFilter = savedPredFilter
	inter.predFilterValid = savedPredFilterValid
	e.partitionReconScratchTop = reconSnap.top
	// Partition-context neutrality: the committed scorer above filled the mi
	// grid (which the bitstream writer re-derives the partition tree from) but
	// also left e.aboveSegCtx/e.leftSegCtx stamped with the chosen partition's
	// update via updateVP9PartitionContextForChoice. In govpx the RD picker and
	// the writeVP9ModesSb serializer share the SAME above/left segmentation
	// context arrays, whereas in libvpx the rd_pick_partition pass restores the
	// context after every arm (restore_context at vp9_encodeframe.c:3872 NONE,
	// :3995 SPLIT, :4079 HORZ, :4125 VERT) and performs the one canonical
	// update_partition_context only via the final encode_sb at :4148
	// (encode_sb -> update_partition_context, :2318). govpx splits that into two
	// passes: the picker fills the mi grid here, and writeVP9ModesSb re-derives
	// the partition tree from mi.SbType and runs the single canonical
	// UpdatePartitionContext from a clean per-tile reset
	// (vp9_bitstream.c write_modes). Restoring the entry-state snapshot makes
	// this function net context-neutral so the writer's WritePartitionForBlock
	// reads the correct pre-decision context and its own UpdatePartitionContext
	// performs the single stamp — eliminating the double-update that desynced
	// the decoded partition tree (the BLOCKER 1 "invalid VP9 data" failure).
	//
	// ENTROPY-context sibling propagation (RD parity): libvpx's SPLIT loop
	// encode_sb's split children 0..2 (pc_tree->index != 3 guard,
	// vp9_encodeframe.c:4163) so each child's plane entropy context
	// (above_context/left_context) is visible to the next sibling's
	// rd_pick_best_sub8x8_mode seed (memcpy(t_above, pd->above_context),
	// vp9_rdopt.c:2120-2121). encode_sb runs AFTER the function's per-arm
	// restore_context calls, so the committed stamp is NOT undone — the next
	// sibling reads it. Therefore restore ONLY the partition seg context here (for
	// serializer neutrality, see above) and LEAVE the entropy context carrying the
	// committed arm's stamp (scoreVP9InterPartitionLeaf stamped the sub-8x8 leaf's
	// segment context; for larger arms the committed scorer left the block's own
	// context). The per-trial restoreBase() above still restores BOTH so trials
	// stay isolated. Gated: production never captured the entropy snapshot
	// (snap.entOK false), so restoreVP9PartitionContexts == seg-only there.
	e.restoreVP9PartitionSegContextsOnly(ctxSnap)
	// SEARCH->WRITE: record this node's committed partition (child block size)
	// so the writer's region picker descends the search's tree rather than
	// re-deciding the node via pickVP9InterPartitionBlockSize, whose early-exits
	// (e.g. full.distortion==0 -> NONE) can diverge from the recursion's choice.
	// The depth-first commit re-runs the winning arm last, so for every node the
	// writer descends this is the final (winning) store. Gated: production (flag
	// off) never stores (cache nil -> no-op).
	if vp9InterUseDeepRDPartition {
		e.storeVP9InterPartitionRDDecision(miRow, miCol, root, committed.target)
	}
	return committed, true
}

func (e *VP9Encoder) scoreVP9InterPartitionPairShallow(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	child common.BlockSize, rowOff, colOff int,
) (uint64, bool) {
	pickPredSnap := e.saveVP9MLPickPredSnapshot(inter, miRows, miCols,
		miRow, miCol)
	defer e.restoreVP9MLPickPredSnapshot(pickPredSnap)

	childRows := int(common.Num8x8BlocksHighLookup[child])
	childCols := int(common.Num8x8BlocksWideLookup[child])
	var saved [64]vp9dec.NeighborMi
	rows, cols, ok := e.snapshotVP9MiRect(miRows, miCols, miRow, miCol,
		childRows+rowOff, childCols+colOff, saved[:])
	if !ok {
		return 0, false
	}
	first, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow, miCol, child)
	if !ok {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
		return 0, false
	}
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, child,
		vp9InterModeDecisionMi(child, first))
	if child < common.Block8x8 {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
		return first.score, true
	}
	second, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
		miRow+rowOff, miCol+colOff, child)
	if !ok {
		e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
		return 0, false
	}
	e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
	return first.score + second.score, true
}

func (e *VP9Encoder) scoreVP9InterPartitionSplitShallow(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	child common.BlockSize,
) (uint64, bool) {
	if child < common.Block8x8 {
		decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
			miRow, miCol, child)
		if !ok {
			return 0, false
		}
		return decision.score, true
	}
	pickPredSnap := e.saveVP9MLPickPredSnapshot(inter, miRows, miCols,
		miRow, miCol)
	defer e.restoreVP9MLPickPredSnapshot(pickPredSnap)

	stepMi := int(common.Num8x8BlocksWideLookup[child])
	var saved [64]vp9dec.NeighborMi
	rows, cols, ok := e.snapshotVP9MiRect(miRows, miCols, miRow, miCol,
		stepMi*2, stepMi*2, saved[:])
	if !ok {
		return 0, false
	}
	var splitScore uint64
	for rowOff := 0; rowOff <= stepMi; rowOff += stepMi {
		for colOff := 0; colOff <= stepMi; colOff += stepMi {
			if miRow+rowOff >= miRows || miCol+colOff >= miCols {
				continue
			}
			decision, ok := e.pickVP9InterReferenceMode(inter, tile, miRows, miCols,
				miRow+rowOff, miCol+colOff, child)
			if !ok {
				e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
				return 0, false
			}
			e.fillVP9MiGrid(miRows, miCols, miRow+rowOff, miCol+colOff, child,
				vp9InterModeDecisionMi(child, decision))
			splitScore += decision.score
		}
	}
	e.restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols, saved[:])
	return splitScore, true
}

func vp9InterModeDecisionMi(bsize common.BlockSize, decision vp9InterModeDecision) vp9dec.NeighborMi {
	mv := decision.mv
	if decision.intra {
		// libvpx: vp9/encoder/vp9_pickmode.c:2644-2645 — when the intra
		// fallback wins inside vp9_pick_inter_mode, libvpx parks both MV
		// slots at INVALID_MV (mi->mv[0] = mi->mv[1] = INVALID_MV). The MV
		// is never read for intra blocks via the ref-frame-guarded
		// ADD_MV_REF_LIST scan, but vp9_NEWMV_diff_bias inspects raw
		// neighbour mv[0] and rejects INVALID_MV (vp9_pickmode.c:1327,1332),
		// so this sentinel keeps that bias's above/left validity check
		// byte-exact with libvpx.
		mv = [2]vp9dec.MV{vp9dec.InvalidMV, vp9dec.InvalidMV}
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         decision.mode,
		RefFrame:     [2]int8{decision.refFrame, decision.secondRefFrame},
		Mv:           mv,
		Bmi:          decision.bmi,
		InterpFilter: uint8(decision.interpFilter),
	}
	if decision.txSize < common.TxSizes {
		mi.TxSize = clampVP9TxSizeForBlock(decision.txSize, bsize)
	}
	return mi
}

func (e *VP9Encoder) snapshotVP9MiRect(miRows, miCols, miRow, miCol, rows, cols int,
	out []vp9dec.NeighborMi,
) (int, int, bool) {
	if rows <= 0 || cols <= 0 || miRow < 0 || miCol < 0 ||
		miRow >= miRows || miCol >= miCols {
		return 0, 0, false
	}
	rows = min(rows, miRows-miRow)
	cols = min(cols, miCols-miCol)
	if rows*cols > len(out) {
		return 0, 0, false
	}
	for r := 0; r < rows; r++ {
		copy(out[r*cols:(r+1)*cols],
			e.miGrid[(miRow+r)*miCols+miCol:(miRow+r)*miCols+miCol+cols])
	}
	return rows, cols, true
}

func (e *VP9Encoder) restoreVP9MiRect(miRows, miCols, miRow, miCol, rows, cols int,
	saved []vp9dec.NeighborMi,
) {
	if rows <= 0 || cols <= 0 || rows*cols > len(saved) {
		return
	}
	for r := 0; r < rows && miRow+r < miRows; r++ {
		copy(e.miGrid[(miRow+r)*miCols+miCol:(miRow+r)*miCols+miCol+cols],
			saved[r*cols:(r+1)*cols])
	}
}

type vp9PartitionContextSnapshot struct {
	aboveStart int
	aboveLen   int
	leftStart  int
	leftLen    int
	above      [common.MiBlockSize]int8
	left       [common.MiBlockSize]int8
	// Per-plane entropy context (pd->above_context/left_context) over the block's
	// footprint, the entropy-context half of libvpx save_context/restore_context
	// (vp9/encoder/vp9_encodeframe.c:2167-2218). Saved/restored alongside the
	// partition seg context so SPLIT-trial entropy stamps don't leak across
	// trials. Captured ONLY on the deep-RD sub-8x8 path (entOK gates restore;
	// production never sets it so the entropy context is left untouched).
	entOK       bool
	entAbove    [vp9dec.MaxMbPlane][16]uint8
	entLeft     [vp9dec.MaxMbPlane][16]uint8
	entAboveOff [vp9dec.MaxMbPlane]int
	entLeftOff  [vp9dec.MaxMbPlane]int
	entAboveLen [vp9dec.MaxMbPlane]int
	entLeftLen  [vp9dec.MaxMbPlane]int
}

func (e *VP9Encoder) snapshotVP9PartitionContexts(miRow, miCol int,
	bsize common.BlockSize,
) (vp9PartitionContextSnapshot, bool) {
	var snap vp9PartitionContextSnapshot
	if miRow < 0 || miCol < 0 || bsize >= common.BlockSizes {
		return snap, false
	}
	width := int(common.Num8x8BlocksWideLookup[bsize])
	height := int(common.Num8x8BlocksHighLookup[bsize])
	if width <= 0 || height <= 0 ||
		width > len(snap.above) || height > len(snap.left) {
		return snap, false
	}
	snap.aboveStart = miCol
	snap.aboveLen = min(width, len(e.aboveSegCtx)-miCol)
	snap.leftStart = miRow & common.MiMask
	snap.leftLen = min(height, len(e.leftSegCtx)-snap.leftStart)
	if snap.aboveLen <= 0 || snap.leftLen <= 0 {
		return snap, false
	}
	copy(snap.above[:snap.aboveLen],
		e.aboveSegCtx[snap.aboveStart:snap.aboveStart+snap.aboveLen])
	copy(snap.left[:snap.leftLen],
		e.leftSegCtx[snap.leftStart:snap.leftStart+snap.leftLen])
	// Per-plane entropy context — the entropy half of libvpx save_context
	// (vp9_encodeframe.c:2207-2218). Captured only when the deep sub-8x8 inter
	// path is active (it is the only consumer of the entropy-context restore).
	if vp9InterUseDeepRDSub8x8 {
		aboveOff, leftOff := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		ok := true
		for plane := range vp9dec.MaxMbPlane {
			pd := &e.planes[plane]
			planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
			if planeBsize >= common.BlockSizes {
				ok = false
				break
			}
			aw := int(common.Num4x4BlocksWideLookup[planeBsize])
			ah := int(common.Num4x4BlocksHighLookup[planeBsize])
			if aw > len(snap.entAbove[plane]) || ah > len(snap.entLeft[plane]) {
				ok = false
				break
			}
			ao := aboveOff[plane]
			lo := leftOff[plane]
			if ao < 0 || ao+aw > len(pd.AboveContext) ||
				lo < 0 || lo+ah > len(pd.LeftContext) {
				ok = false
				break
			}
			copy(snap.entAbove[plane][:aw], pd.AboveContext[ao:ao+aw])
			copy(snap.entLeft[plane][:ah], pd.LeftContext[lo:lo+ah])
			snap.entAboveOff[plane] = ao
			snap.entLeftOff[plane] = lo
			snap.entAboveLen[plane] = aw
			snap.entLeftLen[plane] = ah
		}
		snap.entOK = ok
	}
	return snap, true
}

func (e *VP9Encoder) restoreVP9PartitionContexts(snap vp9PartitionContextSnapshot) {
	e.restoreVP9PartitionSegContextsOnly(snap)
	e.restoreVP9PartitionEntropyContextsOnly(snap)
}

// restoreVP9PartitionSegContextsOnly restores just the partition seg context
// (above_seg_context/left_seg_context). Used at node finalisation, where the seg
// context must return to the entry snapshot for serializer neutrality (see the
// long comment at the pickVP9InterPartitionRD tail) while the entropy context is
// left carrying the committed encode_sb stamp for the next sibling.
func (e *VP9Encoder) restoreVP9PartitionSegContextsOnly(snap vp9PartitionContextSnapshot) {
	if snap.aboveLen > 0 && snap.aboveStart >= 0 &&
		snap.aboveStart+snap.aboveLen <= len(e.aboveSegCtx) {
		copy(e.aboveSegCtx[snap.aboveStart:snap.aboveStart+snap.aboveLen],
			snap.above[:snap.aboveLen])
	}
	if snap.leftLen > 0 && snap.leftStart >= 0 &&
		snap.leftStart+snap.leftLen <= len(e.leftSegCtx) {
		copy(e.leftSegCtx[snap.leftStart:snap.leftStart+snap.leftLen],
			snap.left[:snap.leftLen])
	}
}

// restoreVP9PartitionEntropyContextsOnly restores the per-plane entropy context
// (pd->above_context/left_context), the entropy half of libvpx restore_context
// (vp9_encodeframe.c:2178-2188). Only restored when the snapshot captured it
// (deep sub-8x8 inter path); production leaves the entropy context untouched.
func (e *VP9Encoder) restoreVP9PartitionEntropyContextsOnly(snap vp9PartitionContextSnapshot) {
	if !snap.entOK {
		return
	}
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		aw := snap.entAboveLen[plane]
		ah := snap.entLeftLen[plane]
		ao := snap.entAboveOff[plane]
		lo := snap.entLeftOff[plane]
		if aw > 0 && ao >= 0 && ao+aw <= len(pd.AboveContext) {
			copy(pd.AboveContext[ao:ao+aw], snap.entAbove[plane][:aw])
		}
		if ah > 0 && lo >= 0 && lo+ah <= len(pd.LeftContext) {
			copy(pd.LeftContext[lo:lo+ah], snap.entLeft[plane][:ah])
		}
	}
}

type vp9PartitionReconPlaneSnapshot struct {
	x, y, w, h int
	off        int
}

type vp9PartitionReconSnapshot struct {
	planes [vp9dec.MaxMbPlane]vp9PartitionReconPlaneSnapshot
	top    int
	end    int
}

func (e *VP9Encoder) saveVP9PartitionReconSnapshot(miRow, miCol int,
	bsize common.BlockSize,
) (vp9PartitionReconSnapshot, bool) {
	var snap vp9PartitionReconSnapshot
	total := 0
	base := e.partitionReconScratchTop
	snap.top = base
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		data, stride := e.vp9EncoderReconPlane(plane)
		if len(data) == 0 || stride <= 0 {
			return snap, false
		}
		rows := len(data) / stride
		x := (miCol * common.MiSize) >> pd.SubsamplingX
		y := (miRow * common.MiSize) >> pd.SubsamplingY
		w := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		h := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		if x >= stride || y >= rows {
			return snap, false
		}
		if x+w > stride {
			w = stride - x
		}
		if y+h > rows {
			h = rows - y
		}
		if w <= 0 || h <= 0 {
			return snap, false
		}
		snap.planes[plane] = vp9PartitionReconPlaneSnapshot{
			x: x, y: y, w: w, h: h, off: base + total,
		}
		total += w * h
	}
	need := base + total
	snap.end = need
	if cap(e.partitionReconScratch) < need {
		next := make([]byte, need)
		copy(next, e.partitionReconScratch[:min(base, len(e.partitionReconScratch))])
		e.partitionReconScratch = next
	} else if len(e.partitionReconScratch) < need {
		e.partitionReconScratch = e.partitionReconScratch[:need]
	}
	e.partitionReconScratchTop = need
	for plane := range vp9dec.MaxMbPlane {
		p := snap.planes[plane]
		if p.w == 0 || p.h == 0 {
			continue
		}
		data, stride := e.vp9EncoderReconPlane(plane)
		for y := 0; y < p.h; y++ {
			copy(e.partitionReconScratch[p.off+y*p.w:p.off+(y+1)*p.w],
				data[(p.y+y)*stride+p.x:(p.y+y)*stride+p.x+p.w])
		}
	}
	return snap, true
}

func (e *VP9Encoder) restoreVP9PartitionReconSnapshotPixels(snap vp9PartitionReconSnapshot) {
	for plane := range vp9dec.MaxMbPlane {
		p := snap.planes[plane]
		if p.w == 0 || p.h == 0 {
			continue
		}
		data, stride := e.vp9EncoderReconPlane(plane)
		if len(data) == 0 || stride <= 0 {
			continue
		}
		for y := 0; y < p.h; y++ {
			copy(data[(p.y+y)*stride+p.x:(p.y+y)*stride+p.x+p.w],
				e.partitionReconScratch[p.off+y*p.w:p.off+(y+1)*p.w])
		}
	}
}

func (e *VP9Encoder) restoreVP9PartitionReconSnapshot(snap vp9PartitionReconSnapshot) {
	e.restoreVP9PartitionReconSnapshotPixels(snap)
}

func (e *VP9Encoder) releaseVP9PartitionReconSnapshot(snap vp9PartitionReconSnapshot) {
	if e.partitionReconScratchTop == snap.end {
		e.partitionReconScratchTop = snap.top
	}
}
