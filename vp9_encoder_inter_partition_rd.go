package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9InterPredMvSentinel is libvpx's INT16_MAX per-ref pred_mv reset value
// (vp9/encoder/vp9_encodeframe.c:4215-4218):
//
//	for (i = 0; i < MAX_REF_FRAMES; ++i)
//	  x->pred_mv[i].row = x->pred_mv[i].col = INT16_MAX;
//
// Distinct from vp9dec.InvalidMV (INT16_MIN / 0x80008000), which marks an
// intra block's mv[0] for the NEWMV-diff bias scan. In step (a) this sentinel
// is written but never read.
var vp9InterPredMvSentinel = vp9dec.MV{Row: int16(0x7fff), Col: int16(0x7fff)}

// vp9_encoder_inter_partition_rd.go stands up the depth-first
// rd_pick_partition recursion skeleton for the full-RD INTER path
// (libvpx vp9/encoder/vp9_encodeframe.c:3667 rd_pick_partition). It is the
// inter analogue of the keyframe template scoreVP9KeyframeRDPartitionTree
// (vp9_encoder_key_partition.go:167).
//
// This is STEP (a) of the port (docs/vp9_fullrd_partition_port_plan.md): a
// PROVEN NO-OP. The function reproduces the exact partition + mode decisions
// of pickVP9InterPartitionBlockSize's shallow-RD tail
// (vp9_encoder_inter_partition.go) so it can be wired in behind the existing
// PartitionSearchType==SearchPartition gate without moving a single bit.
//
// What this step DOES carry (structure, libvpx-shaped):
//   - PARTITION_NONE searched first (the parent's leaf decision), matching
//     rd_pick_partition's PARTITION_NONE arm (vp9_encodeframe.c:3811-3876).
//   - A per-node vp9InterPartitionRDNode whose predMv[] slot is the home for
//     x->pred_mv[MAX_REF_FRAMES] (libvpx MACROBLOCK::pred_mv). storePredMv /
//     loadPredMv mirror libvpx store_pred_mv/load_pred_mv
//     (vp9_encodeframe.c:2983-2989) — the save/restore hooks the future
//     candidate[2] thread (step b/c) will populate and consume.
//   - The NONE -> {SPLIT,HORZ,VERT} fan-out with a loadPredMv hook before each
//     rectangular/split arm, mirroring the load_pred_mv calls at
//     vp9_encodeframe.c:3898 (SPLIT), :4037 (HORZ), :4087 (VERT).
//   - The unconditional full-tree partition rate add (RDPartitionCost ==
//     cpi->partition_cost[pl][type], vp9_encodeframe.c:3826/3969/4035/4085)
//     and the strict-less RD tie-break (vp9_encodeframe.c:3829/3973).
//
// What this step DELIBERATELY does NOT do (deferred, so it stays a no-op):
//   - predMv is WRITTEN to the node but NEVER READ. storePredMv/loadPredMv are
//     inert plumbing here; the candidate[2] consumer
//     (vp9InterMvPredStateForRef -> vp9VarPartSBPredMv) is untouched. Threading
//     it is step (b); enabling candidate[2] = x->pred_mv[ref] is step (c).
//   - The arm scorers stay the existing SHALLOW peeks
//     (scoreVP9InterPartitionPairShallow / scoreVP9InterPartitionSplitShallow);
//     they do NOT recurse rd_pick_partition. A genuine depth-first SPLIT
//     (scoreVP9InterPartitionSplit) produces a different sum_rdc and would
//     move bytes — that convergence is step (d).
//   - The arm evaluation ORDER and tie-break are govpx's current tail order
//     (NONE -> HORZ -> VERT -> SPLIT, with SPLIT updating only bestSize, never
//     bestScore, because it is evaluated last). libvpx's canonical order is
//     NONE -> SPLIT -> HORZ -> VERT (vp9_encodeframe.c). Because the RD compare
//     is strict-less (ties keep the earlier winner), reordering the arms can
//     flip a tie and is therefore a behavioural change reserved for step (c).
//     Preserving govpx order is what makes step (a) byte-identical.
//
// libvpx ref: rd_pick_partition (vp9/encoder/vp9_encodeframe.c:3667-4164),
//             store_pred_mv/load_pred_mv (:2983-2989).

// vp9InterPartitionRDNode is the per-node PICK_MODE_CONTEXT slice the
// depth-first inter recursion carries. It is the future home of libvpx's
// MACROBLOCK::pred_mv[MAX_REF_FRAMES] save/restore (store_pred_mv /
// load_pred_mv). In step (a) predMv is written by storePredMv and re-seeded by
// loadPredMv but is NOT consumed anywhere, so the plumbing is provably inert.
type vp9InterPartitionRDNode struct {
	// predMv mirrors x->pred_mv[ref] per reference frame: the NEWMV result the
	// PARTITION_NONE search left for each ref, snapshotted by store_pred_mv and
	// re-seeded before each child arm by load_pred_mv
	// (vp9/encoder/vp9_encodeframe.c:2983-2989). Reset to the INT16_MAX
	// sentinel (vp9InterPredMvSentinel) at construction, matching the per-SB
	// reset at vp9_encodeframe.c:4215-4218.
	predMv [vp9dec.MaxRefFrames]vp9dec.MV
	// partitioning records the chosen partition subsize for this node, the
	// govpx analogue of pc_tree->partitioning consumed by encode_sb.
	partitioning common.BlockSize
}

// newVP9InterPartitionRDNode initialises a node with every pred_mv slot at the
// INT16_MAX sentinel, mirroring the per-SB reset
//
//	for (i = 0; i < MAX_REF_FRAMES; ++i)
//	  x->pred_mv[i].row = x->pred_mv[i].col = INT16_MAX;
//
// at vp9/encoder/vp9_encodeframe.c:4215-4218.
func newVP9InterPartitionRDNode(partitioning common.BlockSize) vp9InterPartitionRDNode {
	node := vp9InterPartitionRDNode{partitioning: partitioning}
	for i := range node.predMv {
		node.predMv[i] = vp9InterPredMvSentinel
	}
	return node
}

// storePredMv mirrors libvpx store_pred_mv (vp9/encoder/vp9_encodeframe.c:2983):
//
//	static void store_pred_mv(MACROBLOCK *x, PICK_MODE_CONTEXT *ctx) {
//	  memcpy(ctx->pred_mv, x->pred_mv, sizeof(x->pred_mv));
//	}
//
// In step (a) the source x->pred_mv is not yet threaded, so src is the
// caller's (sentinel) snapshot and the copy is inert. Kept so the call site
// matches rd_pick_partition's structure at :3879.
func (node *vp9InterPartitionRDNode) storePredMv(src [vp9dec.MaxRefFrames]vp9dec.MV) {
	node.predMv = src
}

// loadPredMv mirrors libvpx load_pred_mv (vp9/encoder/vp9_encodeframe.c:2987):
//
//	static void load_pred_mv(MACROBLOCK *x, PICK_MODE_CONTEXT *ctx) {
//	  memcpy(x->pred_mv, ctx->pred_mv, sizeof(x->pred_mv));
//	}
//
// re-seeding x->pred_mv from the parent node before each child arm. In step (a)
// the returned value is intentionally discarded by the call sites (no consumer
// reads x->pred_mv yet); it exists so the SPLIT/HORZ/VERT arms carry the
// load_pred_mv hook the candidate[2] thread (step c) will switch on.
func (node *vp9InterPartitionRDNode) loadPredMv() [vp9dec.MaxRefFrames]vp9dec.MV {
	return node.predMv
}

// rdPickVP9InterPartition is the depth-first rd_pick_partition skeleton for the
// full-RD inter path. In step (a) it reproduces the shallow-RD tail of
// pickVP9InterPartitionBlockSize byte-for-byte: PARTITION_NONE (already scored
// by the caller and passed as noneScore), then HORZ, VERT, SPLIT shallow peeks,
// picking by the same vp9AddModeDecisionRate(RDPartitionCost) comparison with
// the same strict-less tie-break and the same NONE->HORZ->VERT->SPLIT order.
//
// The recon/ctx/mi save-restore (save_context/restore_context,
// vp9_encodeframe.c:3783/3872) and the caller-state (inter.ref / predInterp)
// snapshots stay in the caller; the shallow scorers self-restore their mi-rect,
// exactly as the inlined tail did. The node parameter carries the predMv slot +
// the store/load hooks but is otherwise unread in step (a).
//
// libvpx ref: rd_pick_partition (vp9/encoder/vp9_encodeframe.c:3667-4164).
func (e *VP9Encoder) rdPickVP9InterPartition(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds,
	rateCostProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	miRows, miCols, miRow, miCol int, root common.BlockSize,
	horzSize, vertSize, splitSize common.BlockSize,
	noneScore uint64, noneRD vp9InterPartitionRD, node *vp9InterPartitionRDNode,
	qindex int,
) common.BlockSize {
	bsl := int(common.BWidthLog2Lookup[root])
	bs := (1 << uint(bsl)) / 4
	hasRows := miRow+bs < miRows
	hasCols := miCol+bs < miCols
	ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
		miRow, miCol, root)

	// PARTITION_NONE: the parent's leaf RD, already computed by the caller
	// (rd_pick_sb_modes for ctx == pc_tree->none, vp9_encodeframe.c:3819).
	// noneScore already folds RDPartitionCost(PARTITION_NONE).
	bestSize := root
	bestScore := noneScore

	// store_pred_mv (vp9_encodeframe.c:3879): snapshot the per-ref MVs the
	// NONE search left in x->pred_mv[] into the node. In step (a) the source
	// is the sentinel-seeded node value (no thread yet); the call keeps the
	// skeleton shaped like libvpx so step (b) only has to feed real MVs in.
	_ = noneRD
	node.storePredMv(node.predMv)

	// The arm scorers below re-seed x->pred_mv from the node via loadPredMv
	// before each child (mirroring load_pred_mv at :3898/:4037/:4087). The
	// returned snapshot is discarded in step (a): no consumer reads
	// x->pred_mv until step (c). Evaluation order and tie-break match govpx's
	// current tail (NONE done; then HORZ, VERT, SPLIT) so the committed size
	// is byte-identical — see the file header for why the libvpx canonical
	// order is deferred.
	if hasRows {
		_ = node.loadPredMv() // load_pred_mv before PARTITION_HORZ (:4037)
		if score, ok := e.scoreVP9InterPartitionPairShallow(inter, tile,
			miRows, miCols, miRow, miCol, horzSize, bs, 0); ok {
			score = e.vp9AddModeDecisionRate(score,
				RDPartitionCost(rateCostProbs, ctx, common.PartitionHorz), qindex)
			if score < bestScore {
				bestScore = score
				bestSize = horzSize
			}
		}
	}
	if hasCols {
		_ = node.loadPredMv() // load_pred_mv before PARTITION_VERT (:4087)
		if score, ok := e.scoreVP9InterPartitionPairShallow(inter, tile,
			miRows, miCols, miRow, miCol, vertSize, 0, bs); ok {
			score = e.vp9AddModeDecisionRate(score,
				RDPartitionCost(rateCostProbs, ctx, common.PartitionVert), qindex)
			if score < bestScore {
				bestScore = score
				bestSize = vertSize
			}
		}
	}
	if hasRows && hasCols {
		_ = node.loadPredMv() // load_pred_mv before PARTITION_SPLIT (:3898)
		if score, ok := e.scoreVP9InterPartitionSplitShallow(inter, tile,
			miRows, miCols, miRow, miCol, splitSize); ok {
			score = e.vp9AddModeDecisionRate(score,
				RDPartitionCost(rateCostProbs, ctx, common.PartitionSplit), qindex)
			// SPLIT is evaluated last in govpx order: it updates only bestSize,
			// never bestScore (no further candidate compares against it). This
			// matches the inlined tail at vp9_encoder_inter_partition.go.
			if score < bestScore {
				bestSize = splitSize
			}
		}
	}
	node.partitioning = bestSize
	return bestSize
}
