package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_pick_inter_mode_nonrd.go ports libvpx v1.16.0's realtime nonrd inter-mode
// picker (vp9_pickmode.c::vp9_pick_inter_mode) verbatim. The realtime picker is
// dramatically simpler than the full RD search: it walks a small, fixed
// (ref, mode) schedule (libvpx's ref_mode_set[]), uses an SSE/SAD proxy for
// per-candidate distortion, and applies aggressive early termination once a
// "good enough" candidate is found. Speed-feature gates (use_nonrd_pick_mode,
// inter_mode_mask, default_interp_filter, etc.) prune the candidate set
// further. govpx routes through this picker when e.sf.UseNonrdPickMode != 0
// (cpu_used >= 5 in libvpx realtime mode).
//
// The libvpx C entry is ~1100 LOC and depends on many subsystems govpx has
// not yet ported (RD-mult/rddiv Lagrangian shape, denoiser SVC plumbing,
// model_rd_for_sb_y, noise_estimate, content_state_sb, encode_breakout_test,
// pred_mv_sad reference-masking, etc.). This port stays faithful to the
// libvpx control-flow shape — the (ref, mode) cross-product schedule, the
// ref_frame_skip_mask short-circuit, the inter_mode_mask gate, and the
// best_early_term shortcut — and reuses govpx's existing predictor /
// distortion / rate-cost helpers for the per-candidate inner work. Deferrals
// are flagged inline with TODO + libvpx citation so a follow-up patch can
// fill them in.
//
// Structural inventory (block-by-block coverage map of libvpx
// vp9_pickmode.c:1696-2488 vs the split nonrd picker files):
//
//   - vp9_pickmode.c:1706    BEST_PICKMODE init_best_pickmode →
//     vp9BestPickmode.reset
//   - vp9_pickmode.c:1731-1880 filter_ref / pred_filter_search /
//     cb_pred_filter_search → vp9NonrdFilterRef +
//     vp9NonrdPredFilterSearch
//   - vp9_pickmode.c:1779    thresh_skip_golden = 500 default →
//     const threshSkipGolden in pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2002-2012 find_predictors pre-loop population →
//     pickVP9InterReferenceModeNonRD per-ref NEAR/NEAREST pre-fill via
//     vp9dec.FindInterMvRefsFields)
//   - vp9_pickmode.c:2050-2082 ref/mode/comp_pred candidate set-up →
//     pickVP9InterReferenceModeNonRD numInterModes loop
//   - vp9_pickmode.c:2084-2128 ref-frame skip + CBR golden-skip +
//     ref_frame_flags + inter_mode_mask gates → pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2204-2228 sf->reference_masking 2× pred_mv_sad
//     ref skip → vp9NonrdPredMVSAD in vp9_pick_inter_mode_nonrd_pred.go
//   - vp9_pickmode.c:2259-2264 search_new_mv NEWMV →
//     pickVP9InterMvWithOptions*
//   - vp9_pickmode.c:2269-2278 mode_checked × zero-MV dedup →
//     pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2296-2299 duplicate-NEARESTMV dedup →
//     pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2318-2330 search_filter_ref filter sweep →
//     vp9SearchFilterRef in vp9_pick_inter_mode_nonrd_filter.go
//   - vp9_pickmode.c:2336    vp9_build_inter_predictors_sby + var/sse
//     → vp9InterPredictionVarianceSSE
//   - vp9_pickmode.c:2346    model_rd_for_sb_y →
//     internal/vp9/encoder.ModelRdForSbY (verbatim port including calculate_tx_size at
//     vp9_pickmode.c:363-394)
//   - vp9_pickmode.c:2350-2354 sse_zeromv_normalized for CBR gold skip
//     → pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2358-2374 block_yrd / is_skippable + skip-vs-non-
//     skip RDCOST compare → pickVP9InterReferenceModeNonRD (encoder.BlockYrd)
//   - vp9_pickmode.c:2401-2410 ref_frame_cost + inter_mode_cost +
//     skip_bit finalize → pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2414-2422 NEWMV_diff_bias (CBR speed>=5 non-screen)
//     → encoder.NewmvDiffBias
//   - vp9_pickmode.c:2425-2435 encode_breakout_test + x->skip →
//     pickVP9InterReferenceModeNonRD (encoder.EncodeBreakoutTest)
//   - vp9_pickmode.c:2460-2462 strict-< winner + best_early_term →
//     pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2478-2480 x->skip outer-loop break →
//     pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2484-2488 best_early_term shortcut →
//     pickVP9InterReferenceModeNonRD
//   - vp9_pickmode.c:2525-2648 intra-fallback section →
//     vp9NonrdEstimateIntraFallback in vp9_pick_inter_mode_nonrd_intra.go
//
// Structural gaps remaining (the only items NOT yet ported from
// vp9_pickmode.c:1696-2488):
//
//   (A) vp9_pickmode.c:2240-2257 mode_rd_thresh + rd_less_than_thresh
//       early-exit gate. Skips a candidate when
//       (best_rdc.rdcost < (rd_threshes[mode_index] *
//       thresh_freq_fact[bsize][mode_index] >> 5)) AND
//       (frame_mv[this_mode][ref_frame].as_int != 0).
//
//       PORTED verbatim from libvpx:
//
//         1. rd state: thresh_mult[MAX_MODES=30] + threshes[1]
//            [BLOCK_SIZES][MAX_MODES] (single-segment collapse) +
//            mode_idx[MAX_REF_FRAMES][4] +
//            rd_thresh_block_size_factor[BLOCK_SIZES] all live in
//            vp9_rd_thresh.go (vp9RDThreshState).
//         2. vp9_set_rd_speed_thresholds (vp9_rd.c:693-745) ⇒
//            vp9RDThreshState.setRDSpeedThresholds; called from
//            vp9EncoderInitializeRDConsts at every frame init.
//         3. set_block_thresholds (vp9_rd.c:355-385) ⇒
//            vp9RDThreshState.setBlockThresholds; called from
//            vp9EncoderInitializeRDConsts after the qindex resolves.
//         4. Per-tile thresh_freq_fact init to RD_THRESH_INIT_FACT=32
//            ⇒ vp9RDThreshState.initFreqFact (one-shot at first
//            frame init, matching libvpx vp9_encodeframe.c:5421-5427
//            tile-data birth).
//         5. update_thresh_freq_fact at picker tail
//            (vp9_pickmode.c:1148-1163, non-row-MT branch) ⇒
//            vp9RDThreshState.updateThreshFreqFact; called from the inter
//            and intra best-mode update tail at the bottom of
//            pickVP9InterReferenceModeNonRD before return.
//         6. The gate at the picker's per-(ref, mode) head ⇒ inline
//            block in pickVP9InterReferenceModeNonRD right after
//            set_ref_ptrs and before search_new_mv.
//
//       Empirical byte impact on the deferred-seed corpus with both
//       gates on while the ML nonrd path was still staged: zero — the per-seed
//       size_deltas and first_byte_diff values stay identical to the
//       pre-port baseline (RefControl agg +446B, RuntimeControls
//       agg +485B; see TestVP9DeferredSeedsRemeasure{RefControl,
//       RuntimeControls} under tags=govpx_oracle_trace). The gate
//       skips candidates the existing govpx score function would have
//       evaluated and discarded as losers anyway, so the structural
//       port preserves byte parity without changing winners. This
//       matches the libvpx-side intent (gate is an optimisation, not
//       a correctness primitive).
//
//       Status: gap (A) is now structurally closed — the libvpx
//       picker shape at vp9_pickmode.c:1696-2488 is fully ported.
//       The residual byte divergence on the deferred seeds is driven
//       by downstream paths (block_yrd SATD rate vs libvpx's
//       cost_coeffs in the FULL RD picker — which the realtime nonrd
//       path does NOT use; cost_coeffs IS already wired through the
//       intra RD chain.
//
//   (B) vp9_pickmode.c:2176 const_motion[ref] && NEARMV skip.
//       Confirmed NO-OP for the deferred seeds: const_motion[ref] is
//       set by mv_refs_rt (vp9_pickmode.c:60-162) which runs ONLY when
//       cm->use_prev_frame_mvs is FALSE. The deferred seeds run with
//       prev_frame_mvs TRUE on every non-key frame (cm->last_show_frame
//       && !cm->intra_only && same dims), so libvpx takes the
//       vp9_find_mv_refs branch (vp9_pickmode.c:1287-1288). govpx
//       matches that branch verbatim. Negative finding.
//
//   (C) vp9_pickmode.c:2152-2174 lag_in_frames>0 + VBR alt_ref_gf_group
//       gates. Deferred seeds run with LagInFrames=0 one-pass realtime,
//       gate never fires. Negative finding.
//
//   (D) vp9_pickmode.c:2178-2193 force_skip_low_temp_var gates. Requires
//       sf.short_circuit_low_temp_var >= 1 which is set only under CBR
//       realtime non-screen (vp9_speed_features.c:1907-1909). Deferred
//       seeds run RateControlQ, gate never fires. Negative finding.
//
//   (E) vp9_pickmode.c:2195-2199 cpi->use_svc + svc_force_zero_mode.
//       Deferred seeds are single-layer non-SVC. Negative finding.
//
//   (F) vp9_pickmode.c:2247-2249 bias_golden mode_rd_thresh boost.
//       Requires sf.bias_golden (CBR non-screen only,
//       vp9_speed_features.c:640). Q-mode seeds, gate never fires.
//       Negative finding.
//
// Net: of 6 structural gaps, 5 (B-F) are CONFIRMED no-ops on the
// deferred-seed configuration because they require CBR/SVC/VBR/AQ
// subsystems govpx hasn't ported AND the deferred seeds don't
// exercise. Gap (A) mode_rd_thresh is now PORTED verbatim but
// produces zero byte impact on the deferred
// seeds because it skips candidates the picker's score function
// would have evaluated and discarded as losers anyway. The byte-9 /
// byte-16 RefControl + RuntimeControls clusters therefore remain
// open with their pre-port size_deltas intact; the residual is
// driven by orthogonal upstream paths (the rate component
// downstream of the picker, the partition decision under cpu_used=8
// ML, and the cpu=-8 RT speed=8 compressed-header coef-update walk
// at byte 4) — not by the gate itself. Closure depends on landing
// the inter-RD chain's full rate path (cost_coeffs in the full RD
// picker — NOT the realtime nonrd path which uses block_yrd
// verbatim and is already libvpx-faithful) AND/OR resolving the
// upstream (mode, mv, filter) divergence cited by the deferred-seed
// remeasure notes.
//
// Tx-size leaf-commit threading was independently audited by the
// deferred-seed remeasure docstring: two
// candidate ports — verbatim calculate_tx_size at the leaf and
// pickedTxSize plumbed through vp9InterModeDecision — both
// REGRESSED aggregate size_delta by 19.7x because they land
// calculate_tx_size on govpx's diverged upstream (mode, mv, filter)
// pick state. The score-based pickVP9InterTxSize is govpx-specific
// but produces tx counts CLOSER to libvpx's output under the
// diverged upstream; the vp9InterTxApplyForces wrap (Tx16x16 cap +
// boost + screen-content force at vp9_pickmode.c:380-388) already
// runs and is libvpx-faithful. Closure depends on the upstream
// (mode, mv, filter) pick converging first (this task's scope).

// REF_MODE pairs a reference frame with a prediction mode.
//
// libvpx: vp9_pickmode.c:1243-1246
//
//	typedef struct {
//	  MV_REFERENCE_FRAME ref_frame;
//	  PREDICTION_MODE pred_mode;
//	} REF_MODE;
type vp9RefMode struct {
	refFrame int8
	predMode common.PredictionMode
}

// RT_INTER_MODES is the number of (ref, mode) candidates in the non-SVC
// realtime schedule.
//
// libvpx: vp9_pickmode.c:1248
//
//	#define RT_INTER_MODES 12
const vp9RTInterModes = 12

// ref_mode_set is the non-SVC realtime (ref, mode) schedule. The order is
// significant: candidates are evaluated in order, and the best_early_term
// shortcut terminates the search once a "good enough" candidate is found.
// LAST is visited before GOLDEN before ALTREF because the picker biases toward
// the most-recent reference.
//
// libvpx: vp9_pickmode.c:1249-1256
//
//	static const REF_MODE ref_mode_set[RT_INTER_MODES] = {
//	  { LAST_FRAME, ZEROMV },   { LAST_FRAME, NEARESTMV },
//	  { GOLDEN_FRAME, ZEROMV }, { LAST_FRAME, NEARMV },
//	  { LAST_FRAME, NEWMV },    { GOLDEN_FRAME, NEARESTMV },
//	  { GOLDEN_FRAME, NEARMV }, { GOLDEN_FRAME, NEWMV },
//	  { ALTREF_FRAME, ZEROMV }, { ALTREF_FRAME, NEARESTMV },
//	  { ALTREF_FRAME, NEARMV }, { ALTREF_FRAME, NEWMV }
//	};
var vp9RefModeSet = [vp9RTInterModes]vp9RefMode{
	{vp9dec.LastFrame, common.ZeroMv}, {vp9dec.LastFrame, common.NearestMv},
	{vp9dec.GoldenFrame, common.ZeroMv}, {vp9dec.LastFrame, common.NearMv},
	{vp9dec.LastFrame, common.NewMv}, {vp9dec.GoldenFrame, common.NearestMv},
	{vp9dec.GoldenFrame, common.NearMv}, {vp9dec.GoldenFrame, common.NewMv},
	{vp9dec.AltrefFrame, common.ZeroMv}, {vp9dec.AltrefFrame, common.NearestMv},
	{vp9dec.AltrefFrame, common.NearMv}, {vp9dec.AltrefFrame, common.NewMv},
}

// vp9BestPickmode mirrors libvpx's BEST_PICKMODE struct, holding the winning
// (mode, ref, filter, tx) tuple across the per-candidate loop.
//
// libvpx: vp9_pickmode.c:45-54
//
//	typedef struct {
//	  PRED_BUFFER *best_pred;
//	  PREDICTION_MODE best_mode;
//	  TX_SIZE best_tx_size;
//	  TX_SIZE best_intra_tx_size;
//	  MV_REFERENCE_FRAME best_ref_frame;
//	  MV_REFERENCE_FRAME best_second_ref_frame;
//	  uint8_t best_mode_skip_txfm;
//	  INTERP_FILTER best_pred_filter;
//	} BEST_PICKMODE;
type vp9BestPickmode struct {
	bestMode           common.PredictionMode
	bestTxSize         common.TxSize
	bestIntraTxSize    common.TxSize
	bestRefFrame       int8
	bestSecondRefFrame int8
	bestModeSkipTxfm   uint8
	bestPredFilter     vp9dec.InterpFilter
	// govpx tracks the winning candidate's MV / decision in-place rather than
	// using libvpx's reuse_inter_pred PRED_BUFFER pool (which exists to avoid
	// re-running the inter-prediction at commit time). govpx re-runs the
	// inter predictor at commit time via predictVP9InterBlock, so the pool
	// is unnecessary. libvpx: vp9_pickmode.c:46 best_pred.
	winnerSet bool
	winner    vp9InterModeDecision
}

// reset mirrors libvpx's init_best_pickmode.
//
// libvpx: vp9_pickmode.c:1685-1694
//
//	static INLINE void init_best_pickmode(BEST_PICKMODE *bp) {
//	  bp->best_mode = ZEROMV;
//	  bp->best_ref_frame = LAST_FRAME;
//	  bp->best_tx_size = TX_SIZES;
//	  bp->best_intra_tx_size = TX_SIZES;
//	  bp->best_pred_filter = EIGHTTAP;
//	  bp->best_mode_skip_txfm = SKIP_TXFM_NONE;
//	  bp->best_second_ref_frame = NO_REF_FRAME;
//	  bp->best_pred = NULL;
//	}
func (bp *vp9BestPickmode) reset() {
	bp.bestMode = common.ZeroMv
	bp.bestRefFrame = vp9dec.LastFrame
	bp.bestTxSize = common.TxSizes
	bp.bestIntraTxSize = common.TxSizes
	bp.bestPredFilter = vp9dec.InterpEighttap
	bp.bestModeSkipTxfm = 0
	bp.bestSecondRefFrame = vp9dec.NoRefFrame
	bp.winnerSet = false
}

// pickVP9InterReferenceModeNonRD ports libvpx's vp9_pick_inter_mode realtime
// nonrd entry. It walks the libvpx ref_mode_set[] schedule (verbatim order),
// applies the inter_mode_mask, ref_frame_skip_mask, and use_compound_nonrd
// gates from SPEED_FEATURES, and returns the winning (ref, mode, mv, filter)
// tuple as a vp9InterModeDecision.
//
// Differences from libvpx:
//
//   - libvpx tracks Lagrangian RD via x->rdmult / x->rddiv (vp9_rd.c::
//     vp9_compute_rd_mult). govpx routes through the libvpx-faithful
//     encoder.RDCost macro that consumes activeRDMult(qindex) + encoder.RDDivBits=7
//     (same scale as libvpx). The picker now constructs (rate, dist)
//     per candidate via the verbatim model_rd_for_sb_y port in
//     internal/vp9/encoder.ModelRdForSbY, so the RDCOST comparison
//     reproduces libvpx's quantizer-aware ordering rather than the previous
//     SSE-only proxy.
//
//   - libvpx's encode_breakout_test (vp9_pickmode.c:942) is ported in
//     internal/vp9/encoder.EncodeBreakoutTest. The gate fires for non-
//     lossless candidates when (cpi->oxcf.encode_breakout > 0 &&
//     motion_low) OR (var==0 && sse==0) (a true perfect match). The
//     deferred RefControl seed configurations run with
//     static_thresh / encode_breakout == 0 so only the perfect-match
//     path is reachable; once the StaticThreshold control is plumbed
//     through the fuzz seeds the > 0 path will exercise too.
//
//   - libvpx's pred_mv_sad reference-masking (vp9_pickmode.c:2204-2228)
//     skips a ref whose SAD is 2x worse than another. govpx ports the
//     full vp9_mv_pred candidate-set SAD (vp9_rd.c:588-639) in
//     vp9_mv_pred.go — the same {ref_mvs[0], ref_mvs[1], x->pred_mv}
//     candidate triple with INT16_MAX skip, near_same_nearest dedup,
//     zero_seen dedup, and sub-pel-to-full-pel rounding.
//
//   - libvpx's block_yrd (vp9_pickmode.c:728-854) refines (rate, dist)
//     with Hadamard + quantize_fp + satd. govpx still uses model_rd as
//     the proxy for that refinement; under speed=8 with
//     sf->use_simple_block_yrd=1 libvpx itself bypasses block_yrd for
//     bsize < BLOCK_32X32, so the gap only shows for the four 32x32 /
//     64x64 partition leaves on these seeds.
//     TODO: port block_yrd full kernel (Phase E1b).
//
// The model_rd_for_sb_y substrate now runs by default through
// vp9NonrdPickPartitionEnabled(); the legacy SSE-only proxy remains in the
// source as the fallback branch but is no longer selected by default.
//
// The pickVP9InterReferenceMode entry routes to this function when
// e.sf.UseNonrdPickMode != 0 (cpu_used >= 5 realtime). At cpu_used < 5 the
// existing RD picker handles selection.
//
// libvpx: vp9_pickmode.c:1696 vp9_pick_inter_mode.
func (e *VP9Encoder) pickVP9InterReferenceModeNonRD(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if inter == nil {
		return vp9InterModeDecision{}, false
	}
	// libvpx: vp9_pickmode.c:1706 BEST_PICKMODE best_pickmode;
	//          vp9_pickmode.c:1785 init_best_pickmode(&best_pickmode);
	var bp vp9BestPickmode
	bp.reset()

	// libvpx: vp9_pickmode.c:1700 SPEED_FEATURES *const sf = &cpi->sf;
	//          vp9_pickmode.c:2150 sf->inter_mode_mask[bsize] gate.
	interModeMask := e.vp9InterModeMaskFor(bsize)
	modeAllowed := func(mode common.PredictionMode) bool {
		return interModeMask&(1<<uint(mode)) != 0
	}

	// libvpx: vp9_pickmode.c:1771 num_inter_modes = (cpi->use_svc) ?
	//   RT_INTER_MODES_SVC : RT_INTER_MODES.
	// govpx single-layer: always RT_INTER_MODES. SVC schedule is a TODO when
	// the SVC layer-context port lands.
	numInterModes := vp9RTInterModes

	// libvpx: vp9_pickmode.c:1748 int ref_frame_skip_mask = 0;
	refFrameSkipMask := 0

	// Pre-cache per-ref slot lookups so the inner loop is a fast table read.
	// libvpx walks find_predictors per usable_ref_frame ahead of the main
	// loop (vp9_pickmode.c:2002-2012); govpx's equivalent is the
	// vp9InterReferenceSlot lookup deferred to candidate evaluation.
	var refSlots [vp9dec.MaxRefFrames]int
	var refSlotValid [vp9dec.MaxRefFrames]bool
	for r := int8(vp9dec.LastFrame); r <= int8(vp9dec.AltrefFrame); r++ {
		slot, ok := e.vp9InterReferenceSlot(inter, r)
		if ok {
			refSlots[r] = slot
			refSlotValid[r] = true
		}
	}

	// libvpx: vp9_pickmode.c:1818-1831 establishes gf_temporal_ref for
	// non-SVC, then vp9_pickmode.c:1918-1939 seeds usable_ref_frame:
	// LAST only on the first frame after GF when the block is not inside an
	// ARF/GF group and the previous frame was not a source-altref overlay;
	// otherwise GOLDEN. Active ARF groups and source-altref overlays promote
	// ALTREF so the scheduled ARF can be searched, while the VBR+lag hidden
	// first-frame exception below can still narrow back to LAST.
	gfTemporalRef := true
	if refSlotValid[vp9dec.LastFrame] && refSlotValid[vp9dec.GoldenFrame] {
		lastRef := &e.refFrames[refSlots[vp9dec.LastFrame]]
		goldenRef := &e.refFrames[refSlots[vp9dec.GoldenFrame]]
		if lastRef != nil && goldenRef != nil {
			gfTemporalRef =
				lastRef.img.Width == goldenRef.img.Width &&
					lastRef.img.Height == goldenRef.img.Height
		}
	}
	maxUsableRef := int8(vp9dec.GoldenFrame)
	if e.rc.framesSinceGolden == 0 && gfTemporalRef &&
		!e.rc.altRefGFGroup && !e.rc.lastFrameIsSrcAltRef {
		maxUsableRef = vp9dec.LastFrame
	}
	if e.rc.altRefGFGroup || e.rc.isSrcFrameAltRef {
		maxUsableRef = vp9dec.AltrefFrame
	}
	if e.opts.LookaheadFrames > 0 && e.opts.RateControlModeSet &&
		e.opts.RateControlMode == RateControlVBR {
		if !inter.showFrame && e.rc.framesSinceKey == 1 {
			maxUsableRef = vp9dec.LastFrame
		}
	}
	forceSkipLowTempVar := e.vp9VarPartForceSkipLowTempVar(miCols, miRow,
		miCol, bsize)
	if vp9NonrdForceLastReference(e.sf.ShortCircuitLowTempVar,
		e.sf.UseNonrdPickMode != 0, forceSkipLowTempVar) {
		maxUsableRef = vp9dec.LastFrame
	}
	useGoldenNonzeromv := refSlotValid[vp9dec.GoldenFrame] && !forceSkipLowTempVar
	sceneChangeDetected := e.rc.highSourceSAD
	highNumBlocksWithMotion := e.rc.highNumBlocksWithMotion
	sourceVariance := ^uint(0)
	if e.sf.ShortCircuitFlatBlocks != 0 || e.sf.LimitNewmvEarlyExit != 0 {
		if v, ok := e.vp9NonrdSourceVariance(inter, miRow, miCol, bsize); ok {
			sourceVariance = v
		}
	}

	// libvpx: vp9_pickmode.c:2204-2228 — sf->reference_masking gate.
	// libvpx's pred_mv_sad[ref] is the best SAD across the per-ref MV
	// candidate set {ref_mvs[0], ref_mvs[1], x->pred_mv[ref]} produced by
	// vp9_mv_pred (vp9_rd.c:588). When a ref's pred_mv_sad is more than 2x
	// the dominant ref's, the entire ref is pruned.
	//
	// Full vp9_mv_pred candidate-set SAD via encoder.MvPredScanCandidates is
	// needed in two places: as the integer-search seed for NEWMV (libvpx
	// x->mv_best_ref_index) and as the pred_mv_sad input to reference
	// masking. The third candidate (x->pred_mv[ref]) is included only at
	// bsize < max_partition_size; libvpx sets
	// max_partition_size = BLOCK_64X64 for the ML_BASED_PARTITION case
	// (vp9_encodeframe.c:5315) and INT16_MAX for sizes >=
	// max_partition_size (vp9_encodeframe.c:4216-4217), so it is skipped at
	// root BLOCK_64X64. choose_partitioning seeds LAST via its int-pro
	// prepass; govpx caches that per SB and feeds it back here when
	// available.
	//
	// libvpx: vp9_rd.c:599-601 num_mv_refs formula.
	// libvpx: vp9_rd.c:602-606 candidate triple population.
	useMvPredCandidateSet := e.sf.ReferenceMasking != 0
	useMvPredSearchSeed := true
	maxPartitionSize := e.sf.DefaultMaxPartitionSize
	if maxPartitionSize == 0 {
		// libvpx: vp9_speed_features.c:876 — default at speed 0 / cpu_used 0.
		maxPartitionSize = common.Block64x64
	}
	numMvRefs := encoder.MvPredNumCandidates(bsize, maxPartitionSize)
	var predMvSad [vp9dec.MaxRefFrames]uint64
	var mvBestRefIndex [vp9dec.MaxRefFrames]int
	var maxMvContext [vp9dec.MaxRefFrames]int
	var mvPredSearchSeed [vp9dec.MaxRefFrames]vp9dec.MV
	var mvPredSearchSeedValid [vp9dec.MaxRefFrames]bool
	for r := int8(vp9dec.LastFrame); r <= maxUsableRef; r++ {
		// libvpx: vp9_pickmode.c:1278 — x->pred_mv_sad[ref] = INT_MAX
		// (find_predictors precondition); govpx mirrors with the widened
		// uint64 sentinel.
		predMvSad[r] = 1<<63 - 1
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if len(src) > 0 && srcStride > 0 && x0+blockW <= srcW && y0+blockH <= srcH {
		for r := int8(vp9dec.LastFrame); r <= maxUsableRef; r++ {
			if !refSlotValid[r] {
				continue
			}
			refSlot := refSlots[r]
			inter.ref = &e.refFrames[refSlot]
			refBuf, refStride, refOriginX, refOriginY, _, _, refOK :=
				e.vp9SubpelReferencePlane(r, inter.ref)
			if len(refBuf) == 0 || refStride <= 0 {
				continue
			}
			if !refOK {
				continue
			}
			refRows := len(refBuf) / refStride

			if useMvPredSearchSeed || useMvPredCandidateSet {
				// libvpx: vp9_rd.c:602-606 — populate pred_mv[0..2]
				// from ref_mvs[ref][0], ref_mvs[ref][1],
				// x->pred_mv[ref]. govpx derives ref_mvs[ref][0..1]
				// from vp9dec.FindInterMvRefsFields in its mode-independent
				// shape: mode=NearMv sets earlyBreak=false in the scanner.
				var candidates [encoder.MvPredMaxCandidates]encoder.MvPredInputCandidate
				refList, refCount := vp9dec.FindInterMvRefsFields(e.miGrid,
					e.useVP9EncoderPrevFrameMvs(miRows, miCols),
					e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
					tile, miRows, miCols, miRow, miCol, bsize,
					common.NearMv, r, inter.refSignBias, -1)
				if refCount >= 1 {
					candidates[0] = encoder.MvPredInputCandidate{
						MV:    refList[0],
						Valid: true,
					}
				}
				if refCount >= 2 {
					candidates[1] = encoder.MvPredInputCandidate{
						MV:    refList[1],
						Valid: true,
					}
				}
				// libvpx: vp9_rd.c:606 — pred_mv[2] =
				// x->pred_mv[ref_frame]. choose_partitioning seeds LAST via
				// its int-pro prepass; govpx caches that per SB and feeds it
				// back here when available.
				if predMv, ok := e.vp9VarPartSBPredMv(miCols, miRow, miCol, r); ok {
					candidates[2] = encoder.MvPredInputCandidate{
						MV:    predMv,
						Valid: true,
					}
				}

				result := encoder.MvPredScanCandidates(candidates[:], numMvRefs,
					src, srcStride, x0, y0,
					refBuf, refStride, x0, y0, refOriginX, refOriginY, refRows,
					blockW, blockH)
				if result.BestSad != ^uint64(0) {
					mvBestRefIndex[r] = result.BestIndex
					maxMvContext[r] = result.MaxMvContext
					if useMvPredCandidateSet {
						predMvSad[r] = result.BestSad
					}
					if result.BestIndex >= 0 &&
						result.BestIndex < len(candidates) &&
						candidates[result.BestIndex].Valid {
						mvPredSearchSeed[r] = candidates[result.BestIndex].MV
						mvPredSearchSeedValid[r] = true
					}
				}
			}
		}
	}
	_ = mvBestRefIndex // libvpx writes to x->mv_best_ref_index; cached via mvPredSearchSeed.
	_ = maxMvContext   // Future NEWMV-diff bias/limit-newmv plumbing reads this.

	// Read the neighbour MIs once for per-candidate rate cost computation.
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)

	// libvpx: vp9_pickmode.c:1710 int_mv frame_mv[MB_MODE_COUNT][MAX_REF_FRAMES].
	// Pre-compute the (mode, ref) MV table that find_predictors populates
	// outside the main candidate loop (vp9_pickmode.c:2002-2012). The table
	// holds:
	//
	//   frame_mv[ZEROMV][ref]    = 0      (libvpx vp9_pickmode.c:1280)
	//   frame_mv[NEARESTMV][ref] = vp9_find_best_ref_mvs's nearestmv result
	//   frame_mv[NEARMV][ref]    = vp9_find_best_ref_mvs's nearmv result
	//   frame_mv[NEWMV][ref]     = INVALID_MV (filled by search_new_mv)
	//
	// govpx walks vp9dec.FindInterMvRefsFields once per ref to populate
	// NEAREST/NEAR; ZERO is constant; NEW is computed lazily inside the
	// main loop via pickVP9InterMvWithOptions. This eliminates the prior per-iteration
	// vp9EncoderInterModeCandidateMv re-walk and surfaces the libvpx-exact
	// dedup at lines 2269-2278 (mode_checked) and 2296-2299 (NEARESTMV).
	var frameMv [common.MbModeCount][vp9dec.MaxRefFrames]vp9dec.MV
	var frameMvValid [common.MbModeCount][vp9dec.MaxRefFrames]bool
	for r := int8(vp9dec.LastFrame); r <= maxUsableRef; r++ {
		if !refSlotValid[r] {
			continue
		}
		// libvpx: vp9_pickmode.c:1280 frame_mv[ZEROMV][ref] = 0.
		frameMv[common.ZeroMv][r] = vp9dec.MV{}
		frameMvValid[common.ZeroMv][r] = true
		// libvpx: vp9_pickmode.c:1295-1297 vp9_find_best_ref_mvs ->
		//   nearestmv = candidates[0]; nearmv = candidates[1].
		// govpx vp9dec.FindInterMvRefsFields returns refList[0..1] with
		// NearMv-mode walk (no earlyBreak) — matches libvpx's
		// candidates[0..1] post-clamp.
		refList, refCount := vp9dec.FindInterMvRefsFields(e.miGrid,
			e.useVP9EncoderPrevFrameMvs(miRows, miCols),
			e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
			tile, miRows, miCols, miRow, miCol, bsize,
			common.NearMv, r, inter.refSignBias, -1)
		if refCount >= 1 {
			mvN := refList[0]
			vp9dec.LowerMvPrecision(&mvN, inter.allowHP)
			frameMv[common.NearestMv][r] = mvN
			frameMvValid[common.NearestMv][r] = true
		}
		if refCount >= 2 {
			mvNear := refList[1]
			vp9dec.LowerMvPrecision(&mvNear, inter.allowHP)
			frameMv[common.NearMv][r] = mvNear
			frameMvValid[common.NearMv][r] = true
		}
		// frame_mv[NEWMV][ref] is left invalid; search_new_mv fills it
		// lazily inside the main loop when NEWMV is visited.
	}

	// libvpx: vp9_pickmode.c:1711 uint8_t mode_checked[MB_MODE_COUNT][MAX_REF_FRAMES].
	// Tracks per-(mode, ref) which candidates have already been scored so
	// the dedup at vp9_pickmode.c:2269-2278 can skip duplicate-MV
	// candidates. memset to 0 at vp9_pickmode.c:1838.
	var modeChecked [common.MbModeCount][vp9dec.MaxRefFrames]bool
	sourceAltRefOverlay := e.vp9OnePassVBRSourceAltRefOverlay(inter)
	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()
	lowvarHighsumdiff := false
	lowvarHighsumdiffSet := false
	newmvDiffBiasInputs := func() (bool, bool, bool, bool) {
		noiseEnabled, noiseAtLeastMedium := e.vp9NewmvDiffBiasNoiseInputs()
		if !lowvarHighsumdiffSet {
			if stats, ok := e.vp9AvgSourceSADStats(inter.img, miCols, miRow, miCol); ok {
				lowvarHighsumdiff = vp9NewmvDiffBiasLowvarInput(stats.contentState)
			}
			lowvarHighsumdiffSet = true
		}
		return noiseEnabled, noiseAtLeastMedium, lowvarHighsumdiff, false
	}
	// libvpx: vp9_encodeframe.c:4244-4248 — every SB's rd_pick_sb_modes call
	// seeds x->cb_rdmult from get_rdmult_delta so per-mode RDCOST consumes
	// a TPL-biased multiplier rather than the bare per-frame rd.RDMULT.
	// The nonrd-pickmode path in libvpx scores via vp9_pickmode.c::model_rd_*
	// which reads x->rdmult (set from x->cb_rdmult at line 1955); govpx
	// mirrors that by priming e.cbRdmult before the per-candidate score
	// loop and clearing it on every exit.  Inline save/restore (no defer)
	// preserves the alloc-parity gate.
	prevCbRdmult := e.cbRdmult
	if e.tpl.enabled && bsize < common.BlockSizes {
		baseRdmult := e.rc.rdmult
		if baseRdmult <= 0 {
			baseRdmult = encoder.ComputeRDMultBasedOnQindex(qindex, encoder.RDFrameInter)
		}
		bwMi := int(common.Num8x8BlocksWideLookup[bsize])
		bhMi := int(common.Num8x8BlocksHighLookup[bsize])
		baseRdmult = e.getVP9TPLRDMultDelta(miRow, miCol, bhMi, bwMi, baseRdmult)
		if baseRdmult <= 0 {
			baseRdmult = 1
		}
		e.cbRdmult = baseRdmult
	}

	// libvpx: vp9_pickmode.c:1731 INTERP_FILTER filter_ref;
	//          vp9_pickmode.c:1874 filter_ref = cm->interp_filter;
	//          vp9_pickmode.c:1875-1880 — when default_interp_filter != BILINEAR,
	//   filter_ref is inherited from neighbour MIs.
	frameInterp := vp9InterFrameInterpFilter(inter)
	filterRef := vp9NonrdFilterRef(frameInterp, e.sf.DefaultInterpFilter,
		above, left)

	// libvpx: vp9_pickmode.c:1732 int pred_filter_search = cm->interp_filter
	//   == SWITCHABLE; further refined at 1862-1869 by cb_pred_filter_search,
	//   which keys a chessboard pattern off (mi_row + mi_col) >>
	//   mi_width_log2_lookup[bsize] + (current_video_frame & 1). govpx ports
	//   the chessboard verbatim so half the SBs run the per-mode filter sweep
	//   and the other half fall through to the filter_ref shortcut.
	predFilterSearch := vp9NonrdPredFilterSearch(frameInterp,
		e.sf.CbPredFilterSearch, miRow, miCol, bsize, e.frameIndex)

	// libvpx: vp9_pickmode.c:1759 unsigned int best_sse_sofar = UINT_MAX;
	bestSseSoFar := uint64(1<<63 - 1)
	bestSet := false
	var best vp9InterModeDecision

	reuseInterPred := e.vp9NonrdReuseInterPredReady(inter, miRows, miCols,
		miRow, miCol, bsize)
	var reuseMLCtx *vp9MLPartitionContext
	var livePred []byte
	livePredStride, livePredX, livePredY := 0, 0, 0
	if reuseInterPred {
		reuseMLCtx = e.vp9MLPickPartitionEntry(inter, miRows, miCols,
			miRow, miCol)
		if reuseMLCtx == nil || !reuseMLCtx.pickPredReady {
			reuseInterPred = false
		} else {
			livePred = reuseMLCtx.pickPred[:]
			livePredStride = 64
			livePredX = (miCol - reuseMLCtx.sbMiCol) * common.MiSize
			livePredY = (miRow - reuseMLCtx.sbMiRow) * common.MiSize
		}
	}
	predPlane, predStride, predX, predY, predW, predH, predOK :=
		e.vp9NonrdLumaPredRect(miRow, miCol, bsize)
	if !predOK {
		reuseInterPred = false
	}
	if reuseInterPred && (livePredX < 0 || livePredY < 0 ||
		livePredX+predW > livePredStride ||
		livePredY+predH > len(livePred)/livePredStride) {
		reuseInterPred = false
	}
	if !reuseInterPred {
		livePred = predPlane
		livePredStride = predStride
		livePredX = predX
		livePredY = predY
	}
	origPredValid := false
	bestPredValid := false
	bestPredFromOrig := false

	// libvpx: vp9_pickmode.c:1758 unsigned int sse_zeromv_normalized = UINT_MAX.
	// Updated at vp9_pickmode.c:2350-2354 only when (ref_frame == LAST_FRAME &&
	// frame_mv[this_mode][LAST_FRAME].as_int == 0) — sseY normalised by
	// b_width_log2 + b_height_log2 (i.e. log2 of total pixel count). Read at
	// vp9_pickmode.c:2123-2126 for the CBR GOLDEN_FRAME skip gate. govpx
	// tracks it for libvpx-shape parity; the gate currently fires only for
	// CBR runs but the value is part of the picker's verbatim state.
	sseZeromvNormalized := uint64(1<<63 - 1)

	// libvpx: vp9_pickmode.c:1860 x->skip = 0; the candidate loop tracks
	// x->skip locally (per-iteration) — set to 1 by encode_breakout_test
	// (vp9_pickmode.c:1026) AND consumed at vp9_pickmode.c:2478-2480 to
	// break out of the candidate loop early when force_test_gf_zeromv is
	// not asserted or GOLDEN/ZEROMV has already been scored.
	xSkip := false

	// libvpx: vp9_pickmode.c:2033-2036 force_test_gf_zeromv. Set for SVC
	// spatial-layer > 0 with no_scaling and base_qindex > lower_layer + 10.
	// govpx single-layer encodes have spatial_layer_id == 0, so the gate is
	// always 0; folded as a constant to preserve the libvpx shape at
	// vp9_pickmode.c:2479 / 2485.
	forceTestGfZeromv := false
	_ = forceTestGfZeromv // referenced symbolically in the early-break gate.

	// libvpx: vp9_pickmode.c:1751 int best_early_term = 0. Set at
	// vp9_pickmode.c:2462 only when the winning candidate flowed through
	// model_rd_for_sb_y_large or search_filter_ref AND those kernels set
	// *this_early_term = 1 (UV transform-skip on both planes). govpx has
	// neither kernel ported yet, so this_early_term is always 0 and the
	// libvpx-faithful early-term break at vp9_pickmode.c:2484-2488 cannot
	// fire. Tracked here as a no-op so the control-flow shape mirrors
	// libvpx; once model_rd_for_sb_y_large lands the gate flips on
	// automatically. Replaces the prior heuristic 1/64-ratio early-term
	// which was a govpx invention and caused libvpx-divergent breaks.
	bestEarlyTerm := false

	// libvpx: vp9_pickmode.c:2050 for (idx = 0; idx < num_inter_modes +
	//   comp_modes; ++idx).
	// govpx defers compound modes (comp_modes loop tail at idx >=
	// num_inter_modes) because compound prediction is handled by the
	// separate pickVP9CompoundInterMode path. libvpx merges them into the
	// same loop; the schedule order does not affect the winner.
	for idx := range numInterModes {
		// libvpx: vp9_pickmode.c:2067-2074 — read (this_mode, ref_frame).
		thisMode := vp9RefModeSet[idx].predMode
		refFrame := vp9RefModeSet[idx].refFrame

		// libvpx: vp9_pickmode.c:2084 if (ref_frame > usable_ref_frame) continue;
		if refFrame > maxUsableRef {
			continue
		}
		if !refSlotValid[refFrame] {
			continue
		}
		// libvpx: vp9_pickmode.c:2152-2156 — a source-alt-ref overlay
		// in one-pass VBR/lag mode is coded only against ALTREF with a
		// zero motion vector. The rule applies whether the hidden ARF was
		// raw or ARNR-filtered.
		if sourceAltRefOverlay {
			mvZero := frameMvValid[thisMode][refFrame] &&
				frameMv[thisMode][refFrame] == (vp9dec.MV{})
			if refFrame != vp9dec.AltrefFrame || !mvZero {
				continue
			}
		}

		// libvpx: vp9_pickmode.c:2123-2126 — CBR golden-frame skip gate.
		//   if (ref_frame == GOLDEN_FRAME && cpi->oxcf.rc_mode == VPX_CBR &&
		//       (... sse_zeromv_normalized < thresh_skip_golden))
		//     continue;
		// libvpx caches the LAST/ZEROMV normalised sse the first time it's
		// scored (vp9_pickmode.c:2350-2354). On the GOLDEN_FRAME iterations
		// the gate prunes the whole ref when the prior LAST/ZEROMV
		// prediction was good enough. govpx tracks sseZeromvNormalized
		// the same way; the gate only fires under CBR (thresh_skip_golden
		// = 500 default, vp9_pickmode.c:1779) — the deferred-seed
		// configurations run RateControlMode = RateControlQ so it's a
		// no-op there, but the shape is part of the libvpx-faithful
		// candidate filter. SVC's thresh_svc_skip_golden branch is not
		// surfaced (govpx is single-layer); single-layer uses the
		// non-SVC 500 default.
		if refFrame == vp9dec.GoldenFrame &&
			e.opts.RateControlModeSet &&
			e.opts.RateControlMode == RateControlCBR {
			const threshSkipGolden = 500
			if sseZeromvNormalized < threshSkipGolden {
				continue
			}
		}

		// libvpx: vp9_pickmode.c:2128 if (!(cpi->ref_frame_flags &
		//   ref_frame_to_flag(ref_frame))) continue;
		// govpx: the refMask check inside vp9InterReferenceSlot covers this.

		// libvpx: vp9_pickmode.c:2150 inter_mode_mask gate.
		if !modeAllowed(thisMode) {
			continue
		}

		// libvpx: vp9_pickmode.c:2204-2228 — sf->reference_masking.
		// libvpx's ref_frame_skip_mask is populated lazily as candidates
		// are visited. govpx pre-computes pred_mv_sad above and applies
		// the same 2x threshold here. The mask is sticky across
		// subsequent (ref, mode) iterations: once ref is skipped, all
		// modes for that ref are skipped.
		frameMvZero := thisMode == common.ZeroMv ||
			(frameMvValid[thisMode][refFrame] &&
				frameMv[thisMode][refFrame] == (vp9dec.MV{}))
		if e.sf.ReferenceMasking != 0 &&
			!(frameMvZero && refFrame == vp9dec.LastFrame) {
			if maxUsableRef < vp9dec.AltrefFrame {
				if !forceSkipLowTempVar && maxUsableRef > vp9dec.LastFrame {
					other := int8(vp9dec.LastFrame)
					if refFrame == vp9dec.LastFrame {
						other = vp9dec.GoldenFrame
					}
					if refSlotValid[other] &&
						predMvSad[refFrame] > predMvSad[other]<<1 {
						refFrameSkipMask |= 1 << uint(refFrame)
					}
				}
			} else if !e.rc.isSrcFrameAltRef &&
				!(frameMvZero && refFrame == vp9dec.AltrefFrame) {
				ref1 := int8(vp9dec.GoldenFrame)
				if refFrame == vp9dec.GoldenFrame {
					ref1 = vp9dec.LastFrame
				}
				ref2 := int8(vp9dec.AltrefFrame)
				if refFrame == vp9dec.AltrefFrame {
					ref2 = vp9dec.LastFrame
				}
				if (refSlotValid[ref1] &&
					predMvSad[refFrame] > predMvSad[ref1]<<1) ||
					(refSlotValid[ref2] &&
						predMvSad[refFrame] > predMvSad[ref2]<<1) {
					refFrameSkipMask |= 1 << uint(refFrame)
				}
			}
		}

		// libvpx: vp9_pickmode.c:2227 ref_frame_skip_mask gate.
		if refFrameSkipMask&(1<<uint(refFrame)) != 0 {
			continue
		}

		// libvpx: vp9_pickmode.c:2238 set_ref_ptrs(cm, xd, ref_frame, ...).
		// govpx: stash the active reference pointer for the predictor.
		inter.ref = &e.refFrames[refSlots[refFrame]]

		// libvpx: vp9_pickmode.c:2240-2257 mode_rd_thresh + rd_less_than_thresh
		// early-exit gate.
		//
		//   mode_index = mode_idx[ref_frame][INTER_OFFSET(this_mode)];
		//   mode_rd_thresh = best_pickmode.best_mode_skip_txfm
		//                      ? rd_threshes[mode_index] << 1
		//                      : rd_threshes[mode_index];
		//   if (cpi->sf.bias_golden && ref_frame == GOLDEN_FRAME &&
		//       cpi->rc.frames_since_golden > 4)
		//     mode_rd_thresh = mode_rd_thresh << 3;
		//   if ((cpi->sf.adaptive_rd_thresh_row_mt && rd_less_than_thresh_row_mt(...))
		//    || (!cpi->sf.adaptive_rd_thresh_row_mt && rd_less_than_thresh(...)))
		//     if (frame_mv[this_mode][ref_frame].as_int != 0) continue;
		//
		// govpx single-tile no-row-MT: the row-MT branch is folded out (sf
		// AdaptiveRdThreshRowMt=0 for deferred-seed configs).
		//
		// The gate skips when best_rdc.rdcost is already below the
		// scheduled threshold AND the candidate's frame_mv is non-zero.
		// frame_mv semantics at this point:
		//   - ZEROMV  ⇒ (0,0) ⇒ never skipped
		//   - NEAREST ⇒ MV returned by find_best_ref_mvs; may be (0,0)
		//   - NEAR    ⇒ MV returned by find_best_ref_mvs; may be (0,0)
		//   - NEWMV   ⇒ INVALID_MV (libvpx vp9_pickmode.c:1279)
		//               ⇒ as_int != 0 in libvpx; treat as non-zero here.
		if bsize >= common.Block8x8 {
			modeIndex := vp9ModeIdxTable[refFrame][vp9ModeOffsetInter(thisMode)]
			modeRdThresh := vp9NonrdModeRdThreshold(
				e.rdThresh.threshes[bsize][modeIndex],
				bp.bestModeSkipTxfm != 0,
				e.sf.BiasGolden != 0,
				refFrame,
				e.rc.framesSinceGolden)
			bestRd := uint64(math.MaxUint64)
			if bestSet {
				bestRd = best.score
			}
			thresholdFires := vp9RDLessThanThresh(bestRd, modeRdThresh,
				e.rdThresh.threshFreqFact[bsize][modeIndex])
			if thresholdFires {
				// frame_mv non-zero check. For NEWMV the MV has not yet
				// been searched (search_new_mv runs below at line 2259);
				// libvpx parks it at INVALID_MV which makes as_int != 0
				// always true. NEAREST/NEAR/ZERO read the precomputed
				// frame_mv table; ZEROMV is (0,0).
				mvAsZero := false
				switch thisMode {
				case common.ZeroMv:
					mvAsZero = true
				case common.NearestMv, common.NearMv:
					if frameMvValid[thisMode][refFrame] &&
						frameMv[thisMode][refFrame] == (vp9dec.MV{}) {
						mvAsZero = true
					}
				case common.NewMv:
					// frame_mv[NEWMV] is INVALID_MV equivalent at this
					// point; gate fires (mvAsZero=false ⇒ continue).
				}
				if !mvAsZero {
					continue
				}
			}
		}

		// libvpx: vp9_pickmode.c:2401 ref_frame_cost[ref_frame] is the
		// per-ref bitcost contribution. govpx computes this through
		// encoder.SingleRefModeRateCost.
		refRate := encoder.SingleRefModeRateCost(&inter.selectFc, above, left,
			inter.referenceMode, inter.compoundRefs, refFrame)

		// libvpx: vp9_pickmode.c:2259-2264 — search_new_mv issues
		// vp9_single_motion_search for NEWMV and returns its rate cost.
		// govpx invokes the existing pickVP9InterMvWithOptions helper, which wraps the
		// motion-search and returns the winning MV. NEAREST/NEAR/ZERO read
		// from the pre-computed frame_mv table (find_predictors-equivalent
		// populated above).
		var mv vp9dec.MV
		var refMv vp9dec.MV
		if thisMode == common.NewMv {
			var gotMv vp9dec.MV
			var ok bool
			refMvOpt := vp9dec.MV{}
			refMvValid := frameMvValid[common.NearestMv][refFrame]
			if refMvValid {
				refMvOpt = frameMv[common.NearestMv][refFrame]
			}
			mvOpts := vp9InterMvSearchOptions{
				refMv:           refMvOpt,
				refMvValid:      refMvValid,
				nonrdSubpelTree: vp9NonrdPickPartitionEnabled(),
			}
			// libvpx vp9_pickmode.c:2046-2047 clears sb_use_mv_part for
			// SVC, speed <= 7, or leaves smaller than BLOCK_32X32. govpx's
			// non-SVC realtime lane mirrors the speed/block-size legs here.
			if vp9NonrdPickPartitionEnabled() &&
				e.vp9SpeedFeatureCPUUsed() > 7 &&
				bsize >= common.Block32x32 {
				if mvPart, ok := e.vp9VarPartSBMvPart(miCols, miRow, miCol); ok {
					mvOpts.seed = mvPart
					mvOpts.seedValid = true
					mvOpts.useMvPart = true
				}
			}
			if !mvOpts.useMvPart && mvPredSearchSeedValid[refFrame] {
				mvOpts.seed = mvPredSearchSeed[refFrame]
				mvOpts.seedValid = true
			}
			gotMv, _, ok = e.pickVP9InterMvWithOptions(inter, miRows, miCols,
				miRow, miCol, bsize, refFrame, mvOpts)
			if !ok {
				continue
			}
			// libvpx combined_motion_search returns false after MV search when
			// the NEWMV mode+MV signalling cost alone exceeds best_rdc_sofar:
			//
			//   rv = !(RDCOST(rdmult, rddiv, rate_mv + rate_mode, 0) >
			//          best_rd_sofar)
			//
			// That prevents periodic-motion blocks from carrying an expensive
			// NEWMV into the full candidate scoring when NEAREST/NEAR/ZERO has
			// already won cheaply. encoder.InterModeRateCost folds both the
			// inter mode bit cost and MV bit cost, matching rate_mv+rate_mode.
			if bestSet {
				rateModeMv := encoder.InterModeRateCost(vp9InterModeCostFrameContext(inter),
					interModeCtx, common.NewMv, gotMv, refMvOpt, inter.allowHP)
				if encoder.RDCost(e.activeRDMult(qindex), encoder.RDDivBits,
					rateModeMv, 0) > best.score {
					continue
				}
			}
			mv = gotMv
			// libvpx vp9_pickmode.c:2302 mi->mv[0] = frame_mv[NEWMV][ref];
			// frame_mv[NEWMV] is the NEW search winner. ref_mv (for
			// inter_mode_cost) is frame_mv[NEARESTMV][ref] (libvpx
			// uses MBMI_EXT->ref_mvs[ref][0] which equals nearestmv
			// post-find_best_ref_mvs).
			frameMv[common.NewMv][refFrame] = mv
			frameMvValid[common.NewMv][refFrame] = true
			if refMvValid {
				refMv = refMvOpt
			}
			if useGoldenNonzeromv && refFrame == vp9dec.LastFrame {
				if sad, ok := e.vp9NonrdPredMVSAD(inter, miRow, miCol,
					bsize, refFrame, mv); ok {
					predMvSad[vp9dec.LastFrame] = sad
				}
			}
		} else if thisMode == common.NearestMv || thisMode == common.NearMv {
			// libvpx: vp9_pickmode.c:2302 — mi->mv[0] is set from
			// frame_mv[this_mode][ref_frame], which find_predictors
			// filled with the nearest/near MV from the reference MV
			// stack. NEAR mode requires refCount >= 2 (frameMvValid).
			if !frameMvValid[thisMode][refFrame] {
				continue
			}
			mv = frameMv[thisMode][refFrame]
			refMv = mv
		} else {
			// ZEROMV: libvpx: vp9_pickmode.c:1280 frame_mv[ZEROMV][ref] = 0.
			mv = vp9dec.MV{}
			refMv = vp9dec.MV{}
		}

		// libvpx: vp9_pickmode.c:2269-2278 — mode_checked dedup. Walk
		// inter_mv_mode in {NEARESTMV..NEWMV}: when a prior mode for
		// the same ref has already been scored AND it has the same MV
		// as this candidate AND that MV is zero, skip this candidate.
		// This is the more permissive verbatim libvpx check vs the
		// previous narrowed bp.winner-based dedup.
		{
			skipThisMv := false
			for prior := common.NearestMv; prior <= common.NewMv; prior++ {
				if prior == thisMode {
					continue
				}
				if !modeChecked[prior][refFrame] {
					continue
				}
				if !frameMvValid[prior][refFrame] {
					continue
				}
				if frameMv[thisMode][refFrame] == frameMv[prior][refFrame] &&
					frameMv[prior][refFrame] == (vp9dec.MV{}) {
					skipThisMv = true
					break
				}
			}
			if skipThisMv {
				continue
			}
		}

		// libvpx: vp9_pickmode.c:2296-2299 — duplicate-NEARESTMV dedup.
		//   if (this_mode != NEARESTMV && !comp_pred &&
		//       frame_mv[this_mode][ref_frame].as_int ==
		//           frame_mv[NEARESTMV][ref_frame].as_int)
		//     continue;
		// Verbatim port using the pre-computed frame_mv table.
		if thisMode != common.NearestMv && frameMvValid[common.NearestMv][refFrame] {
			if mv == frameMv[common.NearestMv][refFrame] {
				continue
			}
		}

		// libvpx: vp9_pickmode.c:2318-2330 — pred_filter_search. When the
		// MV has subpel bits and pred_filter_search is on and the ref is
		// LAST (or one of the special GOLDEN cases — SVC or VBR — which
		// govpx does not surface here), libvpx runs search_filter_ref
		// which sweeps {EIGHTTAP, EIGHTTAP_SMOOTH} (filter_end =
		// EIGHTTAP_SMOOTH; EIGHTTAP_SHARP is NOT evaluated in the realtime
		// path). Otherwise libvpx locks to
		// filter = (filter_ref == SWITCHABLE) ? EIGHTTAP : filter_ref.
		//
		// libvpx: vp9_pickmode.c:1523-1525 search_filter_ref filter loop.
		// libvpx: vp9_pickmode.c:2330 mi->interp_filter fallback.
		var filters []vp9dec.InterpFilter
		switch {
		case predFilterSearch && refFrame == vp9dec.LastFrame &&
			(thisMode == common.NewMv || filterRef == vp9dec.InterpSwitchable) &&
			vp9MvHasSubpel(mv):
			filters = vp9NonrdSwitchableInterpFilterOrder[:]
		case filterRef == vp9dec.InterpSwitchable:
			filters = vp9EighttapInterpFilterOrder[:]
		default:
			switch filterRef {
			case vp9dec.InterpEighttap:
				filters = vp9EighttapInterpFilterOrder[:]
			case vp9dec.InterpEighttapSmooth:
				filters = vp9SmoothInterpFilterOrder[:]
			case vp9dec.InterpEighttapSharp:
				filters = vp9SharpInterpFilterOrder[:]
			case vp9dec.InterpBilinear:
				filters = vp9BilinearInterpFilterOrder[:]
			default:
				filters = vp9EighttapInterpFilterOrder[:]
			}
		}

		// libvpx vp9_pickmode.c:1787 — x->encode_breakout is seeded from
		// cpi->oxcf.encode_breakout (or the per-segment override). govpx's
		// equivalent is the StaticThreshold option; zero by default.
		encodeBreakout := e.opts.StaticThreshold

		// libvpx vp9_pickmode.c:2425-2435 — encode_breakout_test fires when
		// cpi->allow_encode_breakout is set, !xd->lossless, and the current
		// frame is not a scene/high-motion change.
		allowEncodeBreakout := vp9NonrdAllowEncodeBreakout(inter.lossless,
			sceneChangeDetected, highNumBlocksWithMotion)

		// libvpx vp9_pickmode.c:2369-2374 — skip rate is the skip-bit cost
		// from the per-frame skip probability; the unsigned skip pre-cost
		// is added once per candidate (see line 2367).
		var skipProb uint8
		ctx := vp9dec.GetSkipContext(above, left)
		if ctx >= 0 && ctx < len(e.fc.SkipProbs) {
			skipProb = e.fc.SkipProbs[ctx]
		}
		// libvpx: vp9_cost_bit(skip_prob, 1/0). The picker computes both
		// branches because encode_breakout / block_yrd skip override the
		// dist=sse branch with skip=1.
		skipBitOn := 0
		skipBitOff := 0
		if skipProb > 0 {
			skipBitOn = encoder.VP9CostBit(skipProb, 1)
			skipBitOff = encoder.VP9CostBit(skipProb, 0)
		}

		// libvpx vp9_speed_features.c:713 / 791 — sf->use_simple_block_yrd
		// is set at speed >= 8 (and at SVC temporal/spatial layer > 0 at
		// speed >= 7). govpx mirrors via the speed-features field; the
		// realtime nonrd path uses this to bypass block_yrd for sub-32x32
		// blocks (vp9_pickmode.c:747-759), returning sse=INT_MAX so the
		// RDCOST(0, this_sse) skip comparison never wins.
		useSimpleBlockYrd := e.sf.UseSimpleBlockYrd != 0 &&
			bsize < common.Block32x32

		// useModelRD selects the libvpx-faithful model_rd_for_sb_y +
		// block_yrd substrate. The historical env gate is retired; the
		// legacy SSE-only proxy remains below as a fallback branch.
		useModelRD := vp9NonrdPickPartitionEnabled()

		// segId for dequant lookup.
		var dequantY [2]int16
		var dequantU, dequantV [2]int16
		if inter.dq != nil {
			// Realtime nonrd uses the SB segment id (0 when segmentation
			// is off, which matches the deferred-seed configurations).
			dequantY = inter.dq.Y[0]
			dequantU = inter.dq.Uv[0]
			dequantV = inter.dq.Uv[0]
		}

		// libvpx: vp9_pickmode.c:2318-2328 — when the gate at the top of
		// the per-mode body fires, libvpx invokes search_filter_ref which
		// sweeps {EIGHTTAP, EIGHTTAP_SMOOTH} using model_rd_for_sb_y and
		// selects the winner via the libvpx-faithful Lagrangian RDCOST.
		// The chosen filter is then committed to mi->interp_filter and the
		// rest of the per-candidate body runs once at that filter.
		//
		// In the legacy SSE-only proxy below the per-filter loop iterates
		// over all candidates and scores each via raw SSE; for the
		// {EIGHTTAP, EIGHTTAP_SMOOTH} sweep case that mis-orders the per-
		// block filter race vs libvpx (raw SSE deltas are typically too
		// small to flip the winner — EIGHTTAP wins every block, collapsing
		// counts.SwitchableInterp to c==1 and tripping the fix_interp_filter
		// demotion at libvpx vp9_bitstream.c:864-885 so the frame header
		// emits the wrong InterpFilter bit). model_rd_for_sb_y combines
		// var + sse with the quantizer-aware DC/AC rate model so the per-
		// block winner can flip between EIGHTTAP and EIGHTTAP_SMOOTH in
		// the libvpx-faithful way. Pre-select the winning filter here and
		// shrink the filter list to a single entry; the rest of the per-
		// candidate body runs unchanged.
		//
		// Only the multi-filter sweep case (len(filters) > 1, i.e., the
		// search_filter_ref invocation gate at vp9_pickmode.c:2318-2328
		// fired) gets the pre-selection. Single-filter fallback cases
		// (filter_ref locked or BILINEAR) keep the existing single-entry
		// list — search_filter_ref is not invoked by libvpx in those
		// branches either.
		//
		// useModelRD path keeps the full model_rd_for_sb_y + block_yrd
		// chain inside the per-filter loop unchanged (libvpx vp9_pickmode.c:
		// 2336-2410 substrate). The pre-selection only applies to the
		// legacy SSE proxy path, where it ports the libvpx search_filter_ref
		// per-block filter race that the raw-SSE proxy collapses.
		//
		// libvpx: vp9_pickmode.c:2325 search_filter_ref(...).
		if len(filters) > 1 && !useModelRD {
			bestFilter, _, _, _, _, _, sfok := e.vp9SearchFilterRef(inter,
				miRows, miCols, miRow, miCol, bsize, thisMode, refFrame, mv,
				filters, switchableCtx, dequantY, qindex)
			if sfok {
				filters = []vp9dec.InterpFilter{bestFilter}
			}
		}

		// Per-candidate inner: evaluate distortion and rate.
		for _, filter := range filters {
			var cand vp9InterModeDecision
			if useModelRD {
				// libvpx vp9_pickmode.c:2336 vp9_build_inter_predictors_sby +
				// vp9_pickmode.c:2346 model_rd_for_sb_y.
				varY, sseY, ok := e.vp9InterPredictionVarianceSSE(inter, miRows,
					miCols, miRow, miCol, bsize, thisMode, refFrame, mv, filter)
				if !ok {
					continue
				}

				// libvpx: vp9_pickmode.c:2349-2354 — save normalised sse
				// for (LAST, ZEROMV). The shift is log2 of the total
				// pixel count (b_width_log2 + b_height_log2), matching
				// the libvpx formula. Read by the CBR GOLDEN_FRAME skip
				// gate at vp9_pickmode.c:2123-2126 — currently a no-op
				// for non-CBR seeds, but the value is part of the
				// picker's verbatim state.
				if refFrame == vp9dec.LastFrame &&
					frameMv[thisMode][refFrame] == (vp9dec.MV{}) {
					sseZeromvNormalized = vp9NonrdNormalizeSSE(sseY, bsize)
				}

				// libvpx: vp9_pickmode.c:2355 if (sse_y < best_sse_sofar)
				//   best_sse_sofar = sse_y;
				if sseY < bestSseSoFar {
					bestSseSoFar = sseY
				}

				// libvpx vp9_pickmode.c:2346 model_rd_for_sb_y — produces
				// (rate_y, dist_y) in libvpx's prob-cost / shifted-domain
				// distortion units. govpx ports the kernel in
				// internal/vp9/encoder.ModelRdForSbY. The kernel also
				// produces tx_size via calculate_tx_size (libvpx
				// vp9_pickmode.c:660-680); block_yrd consumes
				// min(tx_size, TX_16X16).
				rateY, distY, _, mrdTxSize := encoder.ModelRdForSbY(bsize, qindex,
					dequantY, varY, sseY, 0)

				// libvpx vp9_pickmode.c:2358-2374 — when block_yrd runs
				// (rd_computed=1 from the model_rd call above, and
				// !use_simple_block_yrd or bsize >= 32x32), it overwrites
				// (rate_y, dist_y) with the Hadamard + quantize_fp + SATD
				// refinement and sets this_sse from the model_rd sse_y
				// value. govpx mirrors:
				//
				//   - For (use_simple_block_yrd && bsize < BLOCK_32X32):
				//     libvpx skips block_yrd via the early-return at
				//     vp9_pickmode.c:747-759 (rd_computed=1 path); govpx
				//     keeps the model_rd (rate, dist) tuple unchanged and
				//     the skip-comparison runs against sse_y << 4.
				//
				//   - For (!use_simple_block_yrd || bsize >= BLOCK_32X32):
				//     encoder.BlockYrd is called with tx_size =
				//     min(mrdTxSize, TX_16X16) (vp9_pickmode.c:2361). The
				//     result.rate/result.dist replace (rateY, distY); the
				//     skip comparison runs against result.sse (which is
				//     sse_y << 4, same scaling).
				thisSse := sseY << 4
				finalRate := rateY
				finalDist := uint64(distY)
				blockYrdFired := false
				if !useSimpleBlockYrd {
					txClamp := min(mrdTxSize, common.Tx16x16)
					src, srcStride, _, _ := vp9EncoderSourcePlane(inter.img, 0)
					dst, dstStride := e.vp9EncoderReconPlane(0)
					blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
					blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
					x0 := miCol * common.MiSize
					y0 := miRow * common.MiSize
					// libvpx block_yrd does the prediction-build via
					// vp9_build_inter_predictors_sby just above (line
					// 2336); govpx's vp9InterPredictionVarianceSSE call
					// above already wrote the predictor to recon. The
					// kernel reads src and recon directly.
					if len(src) > 0 && len(dst) > 0 && srcStride > 0 &&
						dstStride > 0 {
						// libvpx uses bw/bh from num_4x4_w/h (the full
						// block extent, not the visible window) for the
						// src_diff stride. govpx mirrors; the visible-
						// clamp happens via maxBlocksWide / maxBlocksHigh
						// inside block_yrd at the tx-unit loop, but for
						// realtime BLOCK_32X32+ candidates the picker only
						// commits a candidate whose visible window equals
						// the full block (encoder.VisibleInterScoreBlock check
						// inside vp9InterPredictionVarianceSSE), so the
						// edge clamp is a no-op here.
						byrd := encoder.BlockYrd(src, srcStride, x0, y0,
							dst, dstStride, x0, y0,
							blockW, blockH, txClamp, dequantY, sseY,
							e.vp9BlockYrdScratch[:])
						if byrd.Valid {
							thisSse = uint64(byrd.SSE)
							if byrd.Skippable {
								// libvpx vp9_pickmode.c:2363-2364 —
								// is_skippable forces rate = skip-bit
								// cost (added below) and dist = sse;
								// the post-compare is then skipped.
								finalRate = 0
								finalDist = uint64(byrd.SSE)
								blockYrdFired = true
							} else {
								// libvpx vp9_pickmode.c:2365-2374 — the
								// non-skippable branch runs the RDCOST
								// compare with block_yrd's refined (rate,
								// dist) and the model_rd-derived sseY.
								finalRate = byrd.Rate
								finalDist = uint64(byrd.Dist)
								// blockYrdFired stays false: the RDCOST
								// compare below still runs.
							}
						}
					}
				}
				_ = blockYrdFired

				// libvpx vp9_pickmode.c:2366-2374 — skip-vs-non-skip RDCOST
				// comparison. When use_simple_block_yrd is set and bsize
				// is small, block_yrd returns sse=INT_MAX which makes the
				// skip branch unreachable; govpx mirrors by skipping the
				// compare. When encoder.BlockYrd fired with skippable=true the
				// is_skippable branch already locked finalRate/finalDist
				// to the skip override (rate=0, dist=sse) — leave the
				// post-compare alone.
				// useSkipCheck mirrors libvpx vp9_pickmode.c:2365-2374. The
				// post-compare runs when block_yrd produced a non-skippable
				// refinement (or was bypassed by use_simple_block_yrd / no
				// kernel run). When block_yrd reported skippable=true the
				// is_skippable branch already set finalRate=0, finalDist=sse
				// — the post-compare is skipped (blockYrdFired guard).
				//
				// libvpx's compare is verbatim:
				//
				//   if (RDCOST(rdmult, rddiv, this_rdc.rate, this_rdc.dist) <
				//       RDCOST(rdmult, rddiv, 0, this_sse)) { ... } else { ... }
				//
				// NOTE the non-skip side is RDCOST(this_rdc.rate, dist) with NO
				// skip-bit-off added, and the skip side is RDCOST(0, this_sse)
				// with NO skip-bit-on added. The chosen skip-bit cost is
				// appended AFTER the compare (libvpx vp9_pickmode.c:2368 or
				// :2370). govpx previously biased the compare by adding
				// skipBitOff / skipBitOn into the two sides, which shifted the
				// skip-vs-non-skip break-even point by (skipBitOff -
				// skipBitOn) * rdmult — a context-dependent over/under-skip.
				useSkipCheck := !useSimpleBlockYrd && !blockYrdFired
				isSkip := blockYrdFired // skippable=true counts as the skip branch
				if useSkipCheck {
					rdNonSkip := encoder.RDCost(e.activeRDMult(qindex), encoder.RDDivBits,
						finalRate, finalDist)
					rdSkip := encoder.RDCost(e.activeRDMult(qindex), encoder.RDDivBits,
						0, thisSse)
					if rdSkip < rdNonSkip {
						// libvpx: this_rdc.rate = vp9_cost_bit(skip_prob, 1);
						//         this_rdc.dist = this_sse;
						finalRate = 0
						finalDist = thisSse
						isSkip = true
					}
				}

				// libvpx vp9_pickmode.c:2405-2410 — finalize the
				// (rate, dist) tuple by adding rate_mv + inter_mode_cost
				// + ref_frame_cost + the chosen skip bit.
				interModeBitCost := encoder.InterModeRateCost(vp9InterModeCostFrameContext(inter),
					interModeCtx, thisMode, mv, refMv, inter.allowHP)
				interpFilterCost := 0
				if vp9MvHasSubpel(mv) {
					interpFilterCost = vp9InterInterpFilterRateCost(inter,
						vp9InterModeCostFrameContext(inter), switchableCtx, filter)
				}
				rate := refRate + interModeBitCost + interpFilterCost + finalRate
				if isSkip {
					rate += skipBitOn
				} else {
					rate += skipBitOff
				}

				// libvpx vp9_pickmode.c:2425-2435 — encode_breakout_test
				// override. Fires only when allow_encode_breakout, not
				// lossless, encode_breakout > 0, and motion-low. For the
				// deferred-seed configurations encode_breakout == 0 so
				// the gate falls through unless var==0 && sse==0 (a true
				// near-perfect prediction).
				if allowEncodeBreakout && (encodeBreakout > 0 ||
					(varY == 0 && sseY == 0)) {
					varU, sseU, varV, sseV, uvOk := e.vp9NonrdUVVarianceSSE(
						inter, miRows, miCols, miRow, miCol, bsize, thisMode,
						refFrame, mv, filter)
					if uvOk {
						fired, ebDist, _ := encoder.EncodeBreakoutTest(bsize,
							dequantY, mv.Row, mv.Col, varY, sseY,
							[2][2]int16{dequantU, dequantV},
							varU, sseU, varV, sseV,
							encodeBreakout, false, interModeBitCost)
						if fired {
							// libvpx vp9_pickmode.c:1029-1030 (inside
							// encode_breakout_test) +:2431 (callsite):
							//   *rate = inter_mode_cost[ctx][INTER_OFFSET(mode)];
							//   *dist = sse << 4;
							//   this_rdc.rate += rate_mv;
							//
							// rate_mv is already folded into govpx's
							// interModeBitCost for NEWMV (zero otherwise);
							// inter_mode_cost is the mode-context bit cost
							// inside that helper too. NO ref_frame_cost is
							// added: libvpx's encode_breakout-fired branch
							// emits the block with x->skip=1 and writes only
							// inter_mode + rate_mv. The "rate += skip_bit(1)"
							// add is intentionally commented out at libvpx
							// vp9_pickmode.c:1033.
							rate = interModeBitCost
							finalDist = uint64(ebDist)
							// libvpx: vp9_pickmode.c:1026 x->skip = 1;
							// Surface the firing through the outer-loop
							// state so the early-break at
							// vp9_pickmode.c:2478-2480 can fire.
							xSkip = true
						}
					}
				}

				// libvpx vp9_pickmode.c:2410 — this_rdc.rdcost = RDCOST(...).
				score := encoder.RDCost(e.activeRDMult(qindex), encoder.RDDivBits,
					rate, finalDist)
				if vp9NonrdScreenZeroLastBias(
					e.opts.ScreenContentMode == int8(VP9ScreenContentScreen),
					sceneChangeDetected, highNumBlocksWithMotion, refFrame, mv,
					sourceVariance, sseY) {
					score <<= 2
				}

				// libvpx vp9_pickmode.c:2414-2422 — vp9_NEWMV_diff_bias.
				// Gated on (rc_mode == VPX_CBR && speed >= 5 && content
				// != VP9E_CONTENT_SCREEN). govpx mirrors the gate at the
				// call site; the bias adjusts this_rdc.rdcost in place.
				//
				// libvpx vp9_pickmode.c:2415:
				//   if (cpi->oxcf.rc_mode == VPX_CBR && cpi->oxcf.speed >= 5 &&
				//       cpi->oxcf.content != VP9E_CONTENT_SCREEN)
				//     vp9_NEWMV_diff_bias(...);
				if e.opts.RateControlModeSet &&
					e.opts.RateControlMode == RateControlCBR &&
					e.vp9SpeedFeatureCPUUsed() >= 5 &&
					e.opts.ScreenContentMode != int8(VP9ScreenContentScreen) {
					noiseEnabled, noiseAtLeastMedium, lowvarHighsumdiff, isSkin :=
						newmvDiffBiasInputs()
					biased := encoder.NewmvDiffBias(thisMode, score, bsize,
						int(mv.Row), int(mv.Col),
						above, left,
						refFrame == vp9dec.LastFrame,
						noiseEnabled, noiseAtLeastMedium, lowvarHighsumdiff, isSkin)
					score = biased.RDCost
				}

				cand = vp9InterModeDecision{
					refFrame:       refFrame,
					secondRefFrame: vp9dec.NoRefFrame,
					refSlot:        refSlots[refFrame],
					mode:           thisMode,
					mv:             [2]vp9dec.MV{mv},
					interpFilter:   filter,
					rate:           rate,
					distortion:     finalDist,
					score:          score,
				}
			} else {
				// Legacy SSE-only proxy path. Kept so the cpu_used=8-
				// default oracle parity tests (LosslessInter, Checker,
				// Lookahead) stay byte-exact while Phase E ramps in.
				distortion, ok := e.vp9InterPredictionDistortion(inter,
					miRows, miCols, miRow, miCol, bsize, thisMode,
					refFrame, mv, filter)
				if !ok {
					continue
				}
				interModeBitCost := encoder.InterModeRateCost(vp9InterModeCostFrameContext(inter),
					interModeCtx, thisMode, mv, refMv, inter.allowHP)
				interpFilterCost := 0
				if vp9MvHasSubpel(mv) {
					interpFilterCost = vp9InterInterpFilterRateCost(inter,
						vp9InterModeCostFrameContext(inter), switchableCtx, filter)
				}
				rate := refRate + interModeBitCost + interpFilterCost
				cand = vp9InterModeDecision{
					refFrame:       refFrame,
					secondRefFrame: vp9dec.NoRefFrame,
					refSlot:        refSlots[refFrame],
					mode:           thisMode,
					mv:             [2]vp9dec.MV{mv},
					interpFilter:   filter,
					rate:           rate,
					distortion:     distortion,
					score:          e.vp9InterModeScore(distortion, rate, qindex),
				}
				if e.opts.RateControlModeSet &&
					e.opts.RateControlMode == RateControlCBR &&
					e.vp9SpeedFeatureCPUUsed() >= 5 &&
					e.opts.ScreenContentMode != int8(VP9ScreenContentScreen) {
					noiseEnabled, noiseAtLeastMedium, lowvarHighsumdiff, isSkin :=
						newmvDiffBiasInputs()
					biased := encoder.NewmvDiffBias(thisMode, cand.score, bsize,
						int(mv.Row), int(mv.Col),
						above, left,
						refFrame == vp9dec.LastFrame,
						noiseEnabled, noiseAtLeastMedium, lowvarHighsumdiff, isSkin)
					cand.score = biased.RDCost
				}
				if distortion < bestSseSoFar {
					bestSseSoFar = distortion
				}
				if vp9NonrdScreenZeroLastBias(
					e.opts.ScreenContentMode == int8(VP9ScreenContentScreen),
					sceneChangeDetected, highNumBlocksWithMotion, refFrame, mv,
					sourceVariance, distortion) {
					cand.score <<= 2
				}
			}

			scoredIntoOrig := false
			if reuseInterPred {
				if !origPredValid {
					vp9CopyPredRectToScratch(e.nonrdOrigPredScratch[:],
						predPlane, predStride, predX, predY, predW, predH)
					vp9CopyPredRectFromScratch(livePred, livePredStride,
						livePredX, livePredY, predW, predH,
						e.nonrdOrigPredScratch[:])
					origPredValid = true
					scoredIntoOrig = true
				}
			}

			// libvpx: vp9_pickmode.c:2460 if (this_rdc.rdcost <
			//   best_rdc.rdcost || x->skip) {
			//     best_rdc = this_rdc;
			//     best_early_term = this_early_term;
			//     ...
			//   }
			// Strict `<` mirrors libvpx; the previous govpx tie-break on
			// (score == best.score && cand.rate < best.rate) was a govpx
			// invention and could swap a libvpx-equivalent loser in on
			// rate parity. Drop the tie-break so candidate ordering
			// matches libvpx's first-seen-wins semantics. The `|| xSkip`
			// disjunct lets an encode_breakout-fired candidate win
			// unconditionally — same as libvpx's `|| x->skip`.
			if !bestSet || cand.score < best.score || xSkip {
				best = cand
				bestSet = true
				bp.bestMode = cand.mode
				bp.bestRefFrame = cand.refFrame
				bp.bestSecondRefFrame = vp9dec.NoRefFrame
				bp.bestPredFilter = cand.interpFilter
				bp.winner = cand
				bp.winnerSet = true
				// libvpx: vp9_pickmode.c:2462 best_early_term =
				// this_early_term. govpx's this_early_term is always 0
				// until model_rd_for_sb_y_large lands (see declaration
				// site for the deferral note); the assignment is kept
				// shape-equivalent.
				bestEarlyTerm = false
				if reuseInterPred {
					if scoredIntoOrig {
						bestPredFromOrig = true
						bestPredValid = false
					} else {
						vp9CopyPredRectToScratch(e.nonrdBestPredScratch[:],
							predPlane, predStride, predX, predY, predW, predH)
						bestPredFromOrig = false
						bestPredValid = true
					}
				}
			}
			if reuseInterPred && origPredValid && !scoredIntoOrig {
				vp9CopyPredRectFromScratch(predPlane, predStride, predX, predY,
					predW, predH, e.nonrdOrigPredScratch[:])
			}
		}

		// libvpx: vp9_pickmode.c:2458 mode_checked[this_mode][ref_frame] = 1.
		// Tracks per-(mode, ref) that a candidate has been scored so the
		// dedup at 2269-2278 can skip duplicate-MV candidates on subsequent
		// iterations. Marked AFTER the inner filter loop so the candidate
		// counts as scored when at least one filter sweep completed.
		modeChecked[thisMode][refFrame] = true

		// libvpx: vp9_pickmode.c:2478-2480 — encode_breakout / x->skip
		// early-break. Once a candidate fired encode_breakout_test (x->skip
		// = 1), libvpx breaks out of the candidate loop UNLESS the SVC
		// force_test_gf_zeromv flag is asserted AND GOLDEN/ZEROMV hasn't
		// been scored yet. For non-SVC encodes (govpx single-layer),
		// force_test_gf_zeromv is always 0 so the break always fires on
		// xSkip.
		//
		//   if (x->skip &&
		//       (!force_test_gf_zeromv || mode_checked[ZEROMV][GOLDEN_FRAME]))
		//     break;
		if xSkip &&
			(!forceTestGfZeromv ||
				modeChecked[common.ZeroMv][vp9dec.GoldenFrame]) {
			break
		}

		// libvpx: vp9_pickmode.c:2484-2488 — best_early_term shortcut.
		//   if (best_early_term && idx > 0 && !scene_change_detected &&
		//       (!force_test_gf_zeromv ||
		//        mode_checked[ZEROMV][GOLDEN_FRAME])) {
		//     x->skip = 1;
		//     break;
		//   }
		// govpx's bestEarlyTerm is always false until
		// model_rd_for_sb_y_large lands (see declaration note); the scene
		// gate is still wired so the control flow stays aligned when that
		// substrate starts producing early-term candidates.
		if bestEarlyTerm && idx > 0 && !sceneChangeDetected &&
			(!forceTestGfZeromv ||
				modeChecked[common.ZeroMv][vp9dec.GoldenFrame]) {
			xSkip = true
			break
		}
	}

	e.cbRdmult = prevCbRdmult

	// libvpx: vp9_pickmode.c:2525-2655 — intra-fallback section inside
	// vp9_pick_inter_mode. After the inter-mode RD scan completes, libvpx
	// walks intra_mode_list (DC_PRED, V_PRED, H_PRED, TM_PRED) and scores
	// each candidate via estimate_block_intra (per-tx-block predict +
	// block_yrd Y + model_rd_for_sb_uv UV). When intra beats inter,
	// libvpx replaces best_pickmode.best_mode / best_ref_frame with the
	// winning intra entry and the bitstream emits an intra block.
	//
	// govpx routing: carry the exact intra fallback winner through the same
	// leaf decision cache as inter choices. This mirrors libvpx storing
	// best_pickmode into mi[0]->mbmi at vp9_pickmode.c:2658-2666; it avoids
	// the older sentinel bridge, which could dispatch to govpx's generic
	// inter-frame intra picker and choose modes outside libvpx's
	// {DC_PRED,V_PRED,H_PRED,TM_PRED} nonrd fallback list.
	//
	// Runs by default with the other nonrd pickmode substrates
	// (model_rd_for_sb_y and pred_mv_sad candidate-set SAD).
	if vp9NonrdPickPartitionEnabled() {
		bestInterScore := uint64(^uint64(0) >> 1)
		if bestSet {
			bestInterScore = best.score
		}
		var pickPred []byte
		pickPredStride, pickPredOriginMiRow, pickPredOriginMiCol := 0, 0, 0
		if reuseInterPred && reuseMLCtx != nil {
			pickPred = livePred
			pickPredStride = livePredStride
			pickPredOriginMiRow = reuseMLCtx.sbMiRow
			pickPredOriginMiCol = reuseMLCtx.sbMiCol
		}
		const qidxSkipThresh = 115
		skipEncode := e.sf.SkipEncodeSb != 0 && e.frameIndex > 1 &&
			qindex < qidxSkipThresh
		if intra, intraFires := e.vp9NonrdEstimateIntraFallback(inter, tile,
			miRows, miCols, miRow, miCol, bsize, qindex,
			above, left, sourceVariance, bestInterScore, forceSkipLowTempVar, xSkip,
			pickPred, pickPredStride, pickPredOriginMiRow,
			pickPredOriginMiCol, skipEncode); intraFires {
			// libvpx: vp9_pickmode.c:2637-2647 — if intra wins, replace
			// best_pickmode's mode/ref/tx/skip state with the intra entry.
			best = vp9InterModeDecision{
				intra:          true,
				refFrame:       vp9dec.IntraFrame,
				secondRefFrame: vp9dec.NoRefFrame,
				mode:           intra.mode,
				interpFilter:   vp9dec.InterpFilter(vp9dec.SwitchableFilters),
				txSize:         intra.txSize,
				uvMode:         intra.uvMode,
				rate:           intra.rate,
				score:          intra.score,
			}
			bestSet = true
			bp.bestMode = intra.mode
			bp.bestRefFrame = vp9dec.IntraFrame
			bp.bestSecondRefFrame = vp9dec.NoRefFrame
			bp.bestIntraTxSize = intra.txSize
			bp.bestModeSkipTxfm = 0
		}
	}
	if bestSet && !best.intra && reuseInterPred && origPredValid {
		// libvpx: vp9_pickmode.c:2888-2912 restores pd->dst to orig_dst,
		// then copies best_pickmode.best_pred back when the inter winner was
		// evaluated in a temporary PRED_BUFFER. If intra fallback overwrote
		// orig_dst and the first candidate stayed best, the saved orig copy is
		// the protected tmp buffer created at vp9_pickmode.c:2721-2740.
		var predScratch []byte
		if bestPredFromOrig {
			predScratch = e.nonrdOrigPredScratch[:]
		} else if bestPredValid {
			predScratch = e.nonrdBestPredScratch[:]
		}
		if predScratch != nil {
			vp9CopyPredRectFromScratch(predPlane, predStride, predX, predY,
				predW, predH, predScratch)
			vp9CopyPredRectFromScratch(livePred, livePredStride, livePredX,
				livePredY, predW, predH, predScratch)
		}
	} else if bestSet && best.intra && reuseInterPred {
		vp9CopyPredRectToScratch(e.nonrdBestPredScratch[:], livePred,
			livePredStride, livePredX, livePredY, predW, predH)
		vp9CopyPredRectFromScratch(predPlane, predStride, predX, predY,
			predW, predH, e.nonrdBestPredScratch[:])
	}
	if !bestSet {
		return vp9InterModeDecision{}, false
	}

	// libvpx: vp9_pickmode.c:2714-2750 — update thresh_freq_fact when
	// sf.adaptive_rd_thresh fires. For inter winners walk
	// ref_frame ∈ {LAST..GOLDEN}, mode ∈ {NEARESTMV..NEWMV} and update via
	// update_thresh_freq_fact (the non-row-MT branch; govpx is single-row
	// for the deferred-seed configs).
	//
	//   if (cpi->sf.adaptive_rd_thresh) {
	//     THR_MODES best_mode_idx =
	//         mode_idx[best_pickmode.best_ref_frame][mode_offset(mi->mode)];
	//     if (best_pickmode.best_ref_frame == INTRA_FRAME) {
	//       ... intra mode_list walk ...
	//     } else {
	//       for (ref_frame = LAST_FRAME; ref_frame <= GOLDEN_FRAME; ++ref_frame) {
	//         if (best_pickmode.best_ref_frame != ref_frame) continue;
	//         for (this_mode = NEARESTMV; this_mode <= NEWMV; ++this_mode) {
	//           update_thresh_freq_fact(cpi, tile_data, x->source_variance,
	//                                   bsize, ref_frame, best_mode_idx, this_mode);
	//         }
	//       }
	//     }
	//   }
	//
	if e.sf.AdaptiveRdThresh != 0 && bestSet && bsize >= common.Block8x8 {
		bestRefFrame := bp.bestRefFrame
		bestMode := bp.bestMode
		if bestRefFrame >= vp9dec.IntraFrame && bestRefFrame < vp9dec.MaxRefFrames &&
			vp9ModeOffsetInterOrIntra(bestMode) >= 0 {
			bestModeIdx := vp9ModeIdxTable[bestRefFrame][vp9ModeOffsetInterOrIntra(bestMode)]
			if bestRefFrame == vp9dec.IntraFrame {
				// libvpx walks intra_mode_list = {DC, V, H, TM}.
				intraModeList := [...]common.PredictionMode{
					common.DcPred, common.VPred, common.HPred, common.TmPred,
				}
				for _, im := range intraModeList {
					e.rdThresh.updateThreshFreqFact(sourceVariance, bsize,
						vp9dec.IntraFrame, bestModeIdx, im,
						e.sf.LimitNewmvEarlyExit, e.sf.AdaptiveRdThresh)
				}
			} else {
				for rf := int8(vp9dec.LastFrame); rf <= vp9dec.GoldenFrame; rf++ {
					if rf != bestRefFrame {
						continue
					}
					for tm := common.NearestMv; tm <= common.NewMv; tm++ {
						e.rdThresh.updateThreshFreqFact(sourceVariance, bsize,
							rf, bestModeIdx, tm,
							e.sf.LimitNewmvEarlyExit, e.sf.AdaptiveRdThresh)
					}
				}
			}
		}
	}

	return best, true
}
