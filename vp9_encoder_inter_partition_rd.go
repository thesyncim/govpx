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

// vp9InterUseDeepRDPartition selects the genuine depth-first
// pickVP9InterPartitionRD recursion over the shallow rdPickVP9InterPartition
// skeleton inside pickVP9InterPartitionBlockSize. Default false keeps the
// legacy opt-in test flag off; the production realtime cpu0 SearchPartition
// lane is enabled by vp9InterUseProductionDeepRDSearchPartition below.
var vp9InterUseDeepRDPartition = false

// vp9InterUseDeepRDThisRDScore gates the GENUINE per-mode this_rd assembly
// (vp9FullRDInterThisRD: super_block_yrd + super_block_uvrd + mode/MV/filter/ref
// rate + the rd_pick_inter_mode_sb skip pick) into the inter mode loop's
// candidate score, REPLACING the model-RD vp9InterModeScore approximation.
//
// Default false for explicit tests: the scoped production cpu0 SearchPartition
// lane enables this through vp9UseProductionDeepRDSearchPartitionPath, while the
// deep-RD partition serialization tests can still exercise model-score leaves.
// The standalone assembly is pinned against libvpx independently via the
// oracle-trace path
// (TestVP9FullRDInterThisRDFrame1SB0Parity), so it is verified regardless of
// this flag's default.
//
// libvpx: vp9/encoder/vp9_rdopt.c:3445 vp9_rd_pick_inter_mode_sb scores every
// candidate with the genuine handle_inter_mode this_rd, not a model.
var vp9InterUseDeepRDThisRDScore = false

// vp9InterUseDeepRDSub8x8 gates the GENUINE sub-8x8 joint-motion RD producer
// (rdPickInterModeSub8x8 → rdPickBestSub8x8Mode + encodeInterMbSegment, the
// verbatim port of vp9_rd_pick_inter_mode_sub8x8 + rd_pick_best_sub8x8_mode)
// into the production sub-8x8 leaf decision, REPLACING the pickVP9Sub8InterMode
// model stand-in (vp9_encoder_inter_modes.go:1944) that only scores
// ZEROMV/NEARESTMV/NEARMV with the SSE model and never runs the NEWMV joint
// search.
//
// Default false for explicit tests: the scoped production cpu0 SearchPartition
// lane enables this through vp9UseProductionDeepRDSearchPartitionPath. The
// standalone producer is pinned against libvpx independently via the oracle-trace
// path
// (TestVP9FullRDSub8x8Frame1Parity), so it is verified regardless of this
// flag's default.
var vp9InterUseDeepRDSub8x8 = false

// vp9InterDeepRDReplayWrites controls whether the bitstream write descent
// replays the deep-RD SEARCH->WRITE leaf decision cache
// (vp9LookupDeepInterRDDecision) instead of re-picking each leaf. Default true:
// when the deep recursion is active the writer replays the search's committed
// decision. It is consulted only after the deep SearchPartition predicate has
// already passed. A round-trip test flips it to false to prove that disabling
// the replay resurrects the re-pick bug (the write pass picks a different
// MV/mode than the search committed), demonstrating the cache is what fixes it.
var vp9InterDeepRDReplayWrites = true

// vp9InterUseDeepRDUsePartition drives the VAR_BASED_PARTITION full-RD inter
// leaf path (cpu_used==4 realtime, e.g. long-fixture seed {0,1,1,0,1}) through
// the GENUINE per-leaf RD: the libvpx-faithful single_motion_search step_param
// + search-method dispatch (vp9FullRDFullPelMv) AND the genuine per-mode this_rd
// scoring (vp9FullRDInterThisRD via vp9InterUseDeepRDThisRDScore). This is the
// rd_use_partition driver wiring (libvpx vp9_encodeframe.c:2566 — choose_-
// partitioning's variance partition fed into rd_pick_sb_modes per 8x8 leaf).
//
// Default true: production now uses the proven libvpx-shaped rd_use_partition
// leaf path for VAR_BASED_PARTITION full-RD inter frames, while the separate
// SEARCH_PARTITION/sub-8x8 deep recursion remains gated off. When true,
// vp9FullRDFullPelMv honours e.sf.Mv.SearchMethod (FAST_HEX for cpu4) and
// computes step_param via the auto_mv_step_size + adaptive_motion_search boffset
// path (FullRdSingleMotionStepParam @
// internal/vp9/encoder/fullrd_motion_search.go), exactly as libvpx
// single_motion_search does (vp9_rdopt.c:2613-2675).
var vp9InterUseDeepRDUsePartition = true

// vp9InterUseDeepRDTxDomainDistortion switches the genuine inter super_block_yrd
// producer (vp9FullRDInterYPlaneTxCandidate) from pixel-domain distortion
// (pixel_sse on the inverse-added recon) to TRANSFORM-domain distortion
// (vp9_block_error on coeff/dqcoeff >> shift), matching libvpx's x->block_tx_domain
// path (vp9/encoder/vp9_encodeframe.c:2041-2048; vp9_rdopt.c:571-600). For
// REALTIME speed >= 1 (cpu4) libvpx forces block_tx_domain = 1
// (allow_txfm_domain_distortion=1, tx_domain_thresh=0, vp9_speed_features.c:
// 486-489), so the producer's per-tx-size dist/sse should be transform-domain.
//
// It drives BOTH the Y producer (vp9FullRDInterYPlaneTxCandidate) and the UV
// producer (vp9FullRDInterUVPlaneTxCandidate) plus the writer's zcoeff_blk
// recompute, which all must use the same distortion domain block_rd_txfm did.
// With it ON the per-tx-size dist/sse reproduce libvpx exactly (verified for
// {0,1,1,0,1} frame-1 mi(0,4): TX_8X8 d=56334 sse=119666, TX_4X4 d=46797
// sse=119991, byte-identical to the libvpx choose_tx_size_from_rd capture), and
// Y+UV together pick NEARMV/TX_4X4 exactly as libvpx — the headline enabler of
// the first byte-exact full-RD inter frame (see
// TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity). Y-domain alone would
// invert the NEARESTMV-vs-NEARMV this_rd tie, so the flag toggles all three
// sites at once.
//
// Default true: transform-domain distortion is active when the speed features
// request it (for example cpu4 realtime), and remains inert for cpu0 paths where
// allow_txfm_domain_distortion is off.
var vp9InterUseDeepRDTxDomainDistortion = true

// vp9InterUseDeepRDRefBestRD threads the running best RD (the mode loop's
// best_rd / ref_best_rd) as the genuine per-candidate handle_inter_mode budget
// and applies the handle_inter_mode early breakouts, instead of always running
// the genuine RD with an INFINITE budget. This is the FOUNDATIONAL libvpx
// mode-pre-filtering mechanism: in vp9_rd_pick_inter_mode_sb the mode loop
// passes best_rd into every handle_inter_mode call (vp9_rdopt.c:3872-3877), and
// handle_inter_mode prunes (returns INT64_MAX, the caller `continue`s at :3881)
// when:
//
//   - the rate-only RD already exceeds the budget and the mode is not NEARESTMV
//     (vp9_rdopt.c:2994-2996), OR
//   - (use_rd_breakout) the per-filter / post-filter MODEL rd/2 exceeds the
//     budget (vp9_rdopt.c:3103-3108, :3180-3187), OR
//   - super_block_yrd / super_block_uvrd early-exit their txfm RD accumulator
//     past the budget and return rate==INT_MAX / is_cost_valid==0
//     (vp9_rdopt.c:846-849, :3214-3218, :3227-3233).
//
// The third breakout is the one that closes long-fixture seed {0,1,1,0,1}
// frame-1 SB0 mi(1,1): after NEARESTMV commits best_rd=33898630, NEWMV(16,-6)'s
// genuine TX_8X8 yrd accumulates this_rd=36.3M > 33.9M, so super_block_yrd
// early-exits and (tx_size_search_breakout) skips TX_4X4 → NEWMV is pruned and
// NEARESTMV (8,14) SMOOTH wins (libvpx ground truth, $TMPDIR vpxenc fprintf at
// handle_inter_mode/block_rd_txfm 2026-06-05). Without the budget govpx ran
// NEWMV's full RD at the smaller TX_4X4 (this_rd=28.4M) and let it win.
//
// Default true for the production rd_use_partition full-RD path. The genuine
// producers already implement the budget early-exit (vp9_fullrd_inter_yrd.go /
// _uvrd.go); this flag switches the mode loop from feeding them ^uint64(0) to
// feeding them the running best, and makes a pruned candidate
// (grd.Valid==false) drop out of the loop the way libvpx's `continue` does.
var vp9InterUseDeepRDRefBestRD = true

// vp9InterUseDeepRDIntraSkipEncode applies libvpx's x->skip_encode behavior to
// the inter-frame intra RD producer (vp9FullRDInterIntraSB). vp9_rd_pick_inter_
// mode_sb sets x->skip_encode = sf->skip_encode_frame && x->q_index <
// QIDX_SKIP_THRESH (vp9/encoder/vp9_rdopt.c:3519; QIDX_SKIP_THRESH == 115,
// vp9_rd.h:36) at the top of the inter-frame mode search — but rd_pick_intra_
// mode_sb (the KEYFRAME path) forces it to 0 (vp9_rdopt.c:3231), which is why
// the keyframe intra producer never needs this. When x->skip_encode is set,
// vp9_encode_block_intra (vp9/encoder/vp9_encodemb.c:840-843, 886/902/918/934)
// changes the intra producer in three coupled ways:
//
//  1. The intra prediction reads its above/left/above-left NEIGHBOUR samples
//     from the SOURCE plane (p->src.buf) instead of the reconstruction
//     (pd->dst.buf): the predict ref pointer is `x->skip_encode ? src : dst`.
//  2. The inverse transform is NOT added back into the recon — `if
//     (!x->skip_encode && *eob)` gates idctNxN_add — so the block's recon
//     stays at the source-based prediction (later intra blocks in the SB then
//     predict from THAT).
//  3. dist_block scores in the transform domain with the skip_encode model
//     adjustment: out_dist += mean_quant_error >> 4, out_sse += mean_quant_
//     error, where mean_quant_error = (dequant[1]^2 << ss_txfrm_size) >>
//     (shift + 2) (vp9_rdopt.c:589-600, gated on x->skip_encode &&
//     !is_inter_block). The transform-domain branch itself is gated on
//     x->block_tx_domain (already wired via vp9InterUseDeepRDTxDomainDistortion
//     for the inter producers).
//
// This is the {0,1,1,0,1} frame-8 mi(0,4) frontier: at base_qindex 112 (< 115)
// with sf->skip_encode_frame == 1, govpx's intra producer predicted DC=79 from
// recon while libvpx predicted from source, inverting the DC residual (govpx
// coeff[0]=412 vs libvpx 604) so intra (govpx) beat NEWMV-LAST mv=(-12,52)
// instead of losing to it (libvpx ground truth, $TMPDIR vpxenc fprintf at
// vp9_rdopt.c block_rd_txfm / dist_block 2026-06-06).
//
// Default true with the production rd_use_partition stack; it is still inert for
// keyframe/cpu0 producers where skip_encode is off.
var vp9InterUseDeepRDIntraSkipEncode = true

// vp9InterUseProductionDeepRDSearchPartition enables the now-proven realtime
// cpu0 SearchPartition deep full-RD lane by default. Component oracle probes can
// temporarily disable it when they need the older standalone 64x64 candidate
// capture point; normal encoder construction leaves it on.
var vp9InterUseProductionDeepRDSearchPartition = true

func (e *VP9Encoder) vp9UseDeepRDUsePartitionPath() bool {
	return vp9InterUseDeepRDUsePartition && e != nil &&
		e.sf.PartitionSearchType == VarBasedPartition
}

func (e *VP9Encoder) vp9UseProductionDeepRDSearchPartitionPath() bool {
	return vp9InterUseProductionDeepRDSearchPartition &&
		e != nil &&
		e.sf.PartitionSearchType == SearchPartition &&
		e.opts.Deadline == DeadlineRealtime &&
		e.opts.CpuUsed == 0
}

func (e *VP9Encoder) vp9UseDeepRDSearchPartitionPath() bool {
	if e == nil || e.sf.PartitionSearchType != SearchPartition {
		return false
	}
	return vp9InterUseDeepRDPartition ||
		e.vp9UseProductionDeepRDSearchPartitionPath()
}

func (e *VP9Encoder) vp9UseDeepRDSub8x8Path() bool {
	return vp9InterUseDeepRDSub8x8 ||
		e.vp9UseProductionDeepRDSearchPartitionPath()
}

func (e *VP9Encoder) vp9UseDeepRDThisRDPath() bool {
	return vp9InterUseDeepRDThisRDScore ||
		e.vp9UseDeepRDUsePartitionPath() ||
		e.vp9UseProductionDeepRDSearchPartitionPath()
}

func (e *VP9Encoder) vp9UseDeepRDPredMvPath() bool {
	return e.vp9UseDeepRDUsePartitionPath() ||
		e.vp9UseDeepRDSub8x8Path()
}

func (e *VP9Encoder) vp9UseDeepRDInterResiduePath() bool {
	if e == nil {
		return false
	}
	if e.vp9UseDeepRDUsePartitionPath() {
		return true
	}
	if e.vp9UseProductionDeepRDSearchPartitionPath() {
		return true
	}
	return vp9InterUseDeepRDPartition &&
		vp9InterUseDeepRDThisRDScore &&
		e.sf.PartitionSearchType == SearchPartition
}

func (e *VP9Encoder) vp9UseDeepRDRefBestPath() bool {
	return vp9InterUseDeepRDRefBestRD &&
		(e.vp9UseDeepRDUsePartitionPath() || e.vp9UseDeepRDSub8x8Path())
}

// vp9QIdxSkipThresh mirrors libvpx's QIDX_SKIP_THRESH (vp9/encoder/vp9_rd.h:36),
// the base_qindex below which x->skip_encode is armed when sf->skip_encode_frame
// is set (vp9_rdopt.c:3519).
const vp9QIdxSkipThresh = 115

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
