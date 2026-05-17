package govpx

import (
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
// are flagged inline with TODO + libvpx citation so a follow-up agent can
// fill them in.

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

// vp9InitBestPickmode mirrors libvpx's init_best_pickmode.
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
func vp9InitBestPickmode(bp *vp9BestPickmode) {
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
// Differences from libvpx (Phase E1 ramp behind GOVPX_VP9_NONRD_PICK_PARTITION):
//
//   - libvpx tracks Lagrangian RD via x->rdmult / x->rddiv (vp9_rd.c::
//     vp9_compute_rd_mult). govpx routes through the libvpx-faithful
//     vp9RDCost macro that consumes activeRDMult(qindex) + vp9RDDivBits=7
//     (same scale as libvpx). The picker now constructs (rate, dist)
//     per candidate via the verbatim model_rd_for_sb_y port in
//     vp9_block_yrd.go::vp9ModelRdForSbY (when the opt-in env gate is
//     set) so the RDCOST comparison reproduces libvpx's quantizer-aware
//     ordering rather than the previous SSE-only proxy.
//
//   - libvpx's encode_breakout_test (vp9_pickmode.c:942) is ported in
//     vp9_block_yrd.go::vp9EncodeBreakoutTest. The gate fires for non-
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
// The model_rd_for_sb_y substrate runs only when
// vp9NonrdPickPartitionEnabled() returns true. Without the gate the
// picker keeps the legacy SSE proxy path so the cpu_used=8-default
// oracle parity tests (Lossless, Checker, Lookahead) stay byte-exact
// during the Phase E ramp.
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
	vp9InitBestPickmode(&bp)

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

	// libvpx: vp9_pickmode.c:1751 int best_early_term = 0;
	// Once a candidate qualifies as "early term" (SSE below a threshold) and
	// idx > 0, the search terminates. govpx mirrors the shape but does not
	// compute the libvpx threshold (which depends on x->mv_limits and var_y);
	// instead we use a distortion-vs-zero-distortion ratio. TODO: port the
	// libvpx early-term condition exactly (vp9_pickmode.c:2484-2488).
	bestEarlyTerm := false

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

	// libvpx: vp9_pickmode.c:2002 for (ref_frame = LAST_FRAME; ref_frame <=
	//   usable_ref_frame; ++ref_frame).
	// govpx scoping: when sf.UseCompoundNonrdPickmode == 0 we drop all
	// ALTREF candidates from the schedule. libvpx achieves this by setting
	// usable_ref_frame = LAST_FRAME / GOLDEN_FRAME outside the loop; mirror
	// the effect by masking ALTREF here.
	maxUsableRef := int8(vp9dec.AltrefFrame)
	if e.sf.UseAltrefOnepass == 0 {
		// libvpx: vp9_speed_features.c:586 sf->use_altref_onepass = 0 at
		// cpu_used >= 5 realtime means the partition driver skips ALTREF;
		// nonrd_pickmode honours that via ref_frame_flags. govpx folds the
		// same gate here.
		if !refSlotValid[vp9dec.AltrefFrame] {
			maxUsableRef = vp9dec.GoldenFrame
		}
	}

	// libvpx: vp9_pickmode.c:2204-2228 — sf->reference_masking gate.
	// libvpx's pred_mv_sad[ref] is the best SAD across the per-ref MV
	// candidate set {ref_mvs[0], ref_mvs[1], x->pred_mv[ref]} produced by
	// vp9_mv_pred (vp9_rd.c:588). When a ref's pred_mv_sad is more than 2x
	// the dominant ref's, the entire ref is pruned.
	//
	// Phase E3 (GOVPX_VP9_NONRD_PICK_PARTITION=1): full vp9_mv_pred
	// candidate-set SAD via vp9MvPredScanCandidates (see vp9_mv_pred.go).
	// The third candidate (x->pred_mv[ref]) is included only at bsize <
	// max_partition_size; libvpx sets max_partition_size = BLOCK_64X64 for
	// the ML_BASED_PARTITION case (vp9_encodeframe.c:5315) and INT16_MAX
	// for sizes >= max_partition_size (vp9_encodeframe.c:4216-4217), so it
	// is skipped at root BLOCK_64X64. govpx does not yet surface
	// x->pred_mv[ref] (the SB-level y_sad path that writes it lives in a
	// future port), so the third candidate is always invalid here.
	//
	// Legacy path (gate off): keep the (0,0)-offset SAD approximation so
	// the cpu_used=8-default oracle parity tests (LosslessInter, Checker,
	// Lookahead) stay byte-exact while Phase E ramps in.
	//
	// libvpx: vp9_rd.c:599-601 num_mv_refs formula.
	// libvpx: vp9_rd.c:602-606 candidate triple population.
	useMvPredCandidateSet := vp9NonrdPickPartitionEnabled()
	maxPartitionSize := e.sf.DefaultMaxPartitionSize
	if maxPartitionSize == 0 {
		// libvpx: vp9_speed_features.c:876 — default at speed 0 / cpu_used 0.
		maxPartitionSize = common.Block64x64
	}
	numMvRefs := vp9MvPredNumCandidates(bsize, maxPartitionSize)
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
	if e.sf.ReferenceMasking != 0 {
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
				refBuf, refStride, refW, refH := vp9ReferenceVisiblePlane(inter.ref, 0)
				if len(refBuf) == 0 || refStride <= 0 {
					continue
				}

				if useMvPredCandidateSet {
					// libvpx: vp9_rd.c:602-606 — populate pred_mv[0..2]
					// from ref_mvs[ref][0], ref_mvs[ref][1],
					// x->pred_mv[ref]. govpx derives ref_mvs[ref][0..1]
					// from vp9FindInterMvRefsFields in its mode-
					// independent shape: mode=NearMv sets earlyBreak=
					// false in the scanner so we walk to the full
					// 2-candidate list (NearestMv would short-circuit
					// after the first match and normalize count=1,
					// dropping the second candidate that vp9_mv_pred
					// needs).
					var candidates [vp9MvPredMaxCandidates]vp9MvPredInputCandidate
					refList, refCount := vp9FindInterMvRefsFields(e.miGrid,
						e.useVP9EncoderPrevFrameMvs(miRows, miCols),
						e.prevFrameMvs, e.prevFrameMvRows, e.prevFrameMvCols,
						tile, miRows, miCols, miRow, miCol, bsize,
						common.NearMv, r, inter.refSignBias, -1)
					if refCount >= 1 {
						candidates[0] = vp9MvPredInputCandidate{
							mv:    refList[0],
							valid: true,
						}
					}
					if refCount >= 2 {
						candidates[1] = vp9MvPredInputCandidate{
							mv:    refList[1],
							valid: true,
						}
					}
					// libvpx: vp9_rd.c:606 — pred_mv[2] =
					// x->pred_mv[ref_frame]. govpx does not surface
					// x->pred_mv yet; candidates[2] stays valid=false
					// which the kernel skips via the INT16_MAX shortcut.

					result := vp9MvPredScanCandidates(candidates[:], numMvRefs,
						src, srcStride, x0, y0,
						refBuf, refStride, x0, y0, refW, refH,
						blockW, blockH)
					if result.bestSad != ^uint64(0) {
						predMvSad[r] = result.bestSad
						mvBestRefIndex[r] = result.bestIndex
						maxMvContext[r] = result.maxMvContext
						if result.bestIndex >= 0 &&
							result.bestIndex < len(candidates) &&
							candidates[result.bestIndex].valid {
							mvPredSearchSeed[r] = candidates[result.bestIndex].mv
							mvPredSearchSeedValid[r] = true
						}
					}
				} else {
					// Legacy (0,0)-offset SAD approximation.
					if x0+blockW > refW || y0+blockH > refH {
						continue
					}
					predMvSad[r] = vp9BlockSAD(src, srcStride, refBuf, refStride,
						x0, y0, x0, y0, blockW, blockH, ^uint64(0))
				}
			}
		}
	}
	_ = mvBestRefIndex // libvpx writes to x->mv_best_ref_index; govpx's
	_ = maxMvContext   // motion-search path does not yet read them.

	// Read the neighbour MIs once for per-candidate rate cost computation.
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	qindex := e.vp9EncoderModeDecisionQIndex()

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
			baseRdmult = vp9ComputeRDMultBasedOnQindex(qindex, vp9RDFrameInter)
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
		if e.sf.ReferenceMasking != 0 && refFrame > vp9dec.LastFrame &&
			!(thisMode == common.ZeroMv && refFrame == vp9dec.LastFrame) {
			if refFrame < vp9dec.AltrefFrame {
				// LAST vs GOLDEN. libvpx: vp9_pickmode.c:2208-2214.
				other := int8(vp9dec.LastFrame)
				if refFrame == vp9dec.LastFrame {
					other = vp9dec.GoldenFrame
				}
				if refSlotValid[other] &&
					predMvSad[refFrame] > predMvSad[other]<<1 {
					refFrameSkipMask |= 1 << uint(refFrame)
				}
			} else {
				// ALTREF. libvpx: vp9_pickmode.c:2215-2225.
				ref1 := int8(vp9dec.LastFrame)
				if refFrame == vp9dec.GoldenFrame {
					ref1 = vp9dec.GoldenFrame
				}
				ref2 := int8(vp9dec.LastFrame)
				if refFrame == vp9dec.AltrefFrame {
					ref2 = vp9dec.AltrefFrame
				}
				_ = ref1
				_ = ref2
				if refSlotValid[vp9dec.LastFrame] &&
					predMvSad[refFrame] > predMvSad[vp9dec.LastFrame]<<1 {
					refFrameSkipMask |= 1 << uint(refFrame)
				} else if refSlotValid[vp9dec.GoldenFrame] &&
					predMvSad[refFrame] > predMvSad[vp9dec.GoldenFrame]<<1 {
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

		// libvpx: vp9_pickmode.c:2401 ref_frame_cost[ref_frame] is the
		// per-ref bitcost contribution. govpx computes this through
		// vp9SingleRefModeRateCost.
		refRate := vp9SingleRefModeRateCost(&inter.selectFc, above, left,
			inter.referenceMode, inter.compoundRefs, refFrame)

		// libvpx: vp9_pickmode.c:2259-2264 — search_new_mv issues
		// vp9_single_motion_search for NEWMV and returns its rate cost.
		// govpx invokes the existing pickVP9InterMv helper, which wraps the
		// motion-search and returns the winning MV.
		var mv vp9dec.MV
		var refMv vp9dec.MV
		if thisMode == common.NewMv {
			var gotMv vp9dec.MV
			var ok bool
			if useMvPredCandidateSet && mvPredSearchSeedValid[refFrame] {
				gotMv, _, ok = e.pickVP9InterMvWithOptions(inter, miRows, miCols,
					miRow, miCol, bsize, refFrame,
					vp9InterMvSearchOptions{
						seed:      mvPredSearchSeed[refFrame],
						seedValid: true,
					})
			} else {
				gotMv, _, ok = e.pickVP9InterMv(inter, miRows, miCols,
					miRow, miCol, bsize, refFrame)
			}
			if !ok {
				continue
			}
			mv = gotMv
			refMv, _ = e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
				miRow, miCol, bsize, common.NewMv, refFrame, inter.allowHP,
				inter.refSignBias)
		} else if thisMode == common.NearestMv || thisMode == common.NearMv {
			// libvpx: vp9_pickmode.c:2302 — mi->mv[0] is set from
			// frame_mv[this_mode][ref_frame], which find_predictors filled
			// with the nearest/near MV from the reference MV stack.
			gotMv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
				miRow, miCol, bsize, thisMode, refFrame, inter.allowHP,
				inter.refSignBias)
			if !ok {
				continue
			}
			mv = gotMv
			refMv = gotMv
		} else {
			// ZEROMV: libvpx: vp9_pickmode.c:1280 frame_mv[ZEROMV][ref] = 0.
			mv = vp9dec.MV{}
			refMv = vp9dec.MV{}
		}

		// libvpx: vp9_pickmode.c:2296-2299 — duplicate-MV dedup. NEARMV /
		// NEARESTMV / NEWMV with the same MV as NEARESTMV is skipped to
		// avoid re-scoring identical candidates. govpx mirrors the check.
		if thisMode != common.NearestMv && bp.winnerSet &&
			bp.winner.refFrame == refFrame {
			if mv == bp.winner.mv[0] && mv == (vp9dec.MV{}) {
				// libvpx's mode_checked dedup uses any matching prior mode
				// with the same MV. govpx narrows to the (ZEROMV-equivalent)
				// case because the per-ref winner is the only state we
				// track without the full mode_checked[][] table.
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
		// cpi->allow_encode_breakout is set and !xd->lossless. govpx
		// mirrors the gate.
		allowEncodeBreakout := !inter.lossless

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

		// useModelRD gates the libvpx-faithful model_rd_for_sb_y +
		// block_yrd substrate. Behind the Phase E opt-in env (same
		// GOVPX_VP9_NONRD_PICK_PARTITION=1 gate as the recursive walker)
		// because the libvpx-faithful (rate, dist) tuple shape disagrees
		// with the legacy SSE-only proxy on the lossless / cpu_used=8-
		// default oracle parity tests until the full block_yrd is also
		// ported. Once parity closes the gate flips and the legacy path
		// retires.
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

				// libvpx: vp9_pickmode.c:2355 if (sse_y < best_sse_sofar)
				//   best_sse_sofar = sse_y;
				if sseY < bestSseSoFar {
					bestSseSoFar = sseY
				}

				// libvpx vp9_pickmode.c:2346 model_rd_for_sb_y — produces
				// (rate_y, dist_y) in libvpx's prob-cost / shifted-domain
				// distortion units. govpx ports the kernel in
				// vp9_block_yrd.go::vp9ModelRdForSbY.
				rateY, distY, _, _ := vp9ModelRdForSbY(bsize, dequantY,
					varY, sseY, 0)

				// libvpx vp9_pickmode.c:2366-2374 — skip-vs-non-skip RDCOST
				// comparison. When use_simple_block_yrd is set and bsize
				// is small, block_yrd returns sse=INT_MAX which makes the
				// skip branch unreachable; govpx mirrors by skipping the
				// compare.
				//
				// For bsize >= BLOCK_32X32 (or use_simple_block_yrd=0),
				// libvpx runs the real block_yrd: govpx defers that
				// detailed kernel to a follow-up port (E1b) and uses
				// model_rd as the proxy. In that path the skip comparison
				// runs against sseY << 4.
				useSkipCheck := !useSimpleBlockYrd
				isSkip := false
				finalRate := rateY
				finalDist := uint64(distY)
				if useSkipCheck {
					thisSse := sseY << 4
					rdNonSkip := vp9RDCost(e.activeRDMult(qindex), vp9RDDivBits,
						rateY+skipBitOff, finalDist)
					rdSkip := vp9RDCost(e.activeRDMult(qindex), vp9RDDivBits,
						skipBitOn, thisSse)
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
				interModeBitCost := vp9InterModeRateCost(&inter.selectFc,
					interModeCtx, thisMode, mv, refMv, inter.allowHP)
				interpFilterCost := vp9InterInterpFilterRateCost(inter,
					&inter.selectFc, switchableCtx, filter)
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
						fired, ebDist, _ := vp9EncodeBreakoutTest(bsize,
							dequantY, mv.Row, mv.Col, varY, sseY,
							[2][2]int16{dequantU, dequantV},
							varU, sseU, varV, sseV,
							encodeBreakout, false, interModeBitCost)
						if fired {
							// libvpx vp9_pickmode.c:1026-1041 —
							// x->skip = 1, rate = inter_mode_cost only,
							// dist = sse << 4.
							rate = refRate + interModeBitCost + skipBitOn
							finalDist = uint64(ebDist)
						}
					}
				}

				// libvpx vp9_pickmode.c:2410 — this_rdc.rdcost = RDCOST(...).
				score := vp9RDCost(e.activeRDMult(qindex), vp9RDDivBits,
					rate, finalDist)

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
					// noise_estimate / lowvar_highsumdiff / sb_is_skin
					// are not wired through govpx yet (cpi->noise_estimate
					// disabled when oxcf.noise_sensitivity == 0); the
					// kernel returns the unmodified rdcost in that case.
					biased := vp9NewmvDiffBias(thisMode, score, bsize,
						int(mv.Row), int(mv.Col),
						above, left,
						refFrame == vp9dec.LastFrame,
						false, false, false, false)
					score = biased.rdcost
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
				rate := refRate +
					vp9InterModeRateCost(&inter.selectFc, interModeCtx, thisMode,
						mv, refMv, inter.allowHP) +
					vp9InterInterpFilterRateCost(inter, &inter.selectFc,
						switchableCtx, filter)
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
				if distortion < bestSseSoFar {
					bestSseSoFar = distortion
				}
			}

			// libvpx: vp9_pickmode.c:2460 if (this_rdc.rdcost <
			//   best_rdc.rdcost || x->skip).
			if !bestSet || cand.score < best.score ||
				(cand.score == best.score && cand.rate < best.rate) {
				best = cand
				bestSet = true
				bp.bestMode = cand.mode
				bp.bestRefFrame = cand.refFrame
				bp.bestSecondRefFrame = vp9dec.NoRefFrame
				bp.bestPredFilter = cand.interpFilter
				bp.winner = cand
				bp.winnerSet = true
			}
		}

		// libvpx: vp9_pickmode.c:2484-2488 — best_early_term shortcut.
		//   if (best_early_term && idx > 0 && !scene_change_detected) {
		//     x->skip = 1; break;
		//   }
		// govpx triggers an analogous early-term when the LAST/ZEROMV
		// distortion is below a fraction of the maximum block SSE — that
		// is, when the reference is "close enough" to the source that
		// further candidates are unlikely to win. The exact libvpx
		// threshold depends on var_y and noise_estimate which govpx does
		// not surface yet (TODO).
		if bestSet && idx > 0 && !bestEarlyTerm {
			// Treat distortion <= 1/64 of (bestSseSoFar at idx==0) as
			// "good enough"; this is a conservative analogue to libvpx's
			// model_rd skip criterion (vp9_pickmode.c:2358-2374). Pixel
			// MSE of ~0.25 corresponds to a true near-skip block.
			if best.distortion <= (bestSseSoFar >> 6) {
				bestEarlyTerm = true
			}
		}
		if bestEarlyTerm {
			break
		}
	}

	e.cbRdmult = prevCbRdmult
	if !bestSet {
		return vp9InterModeDecision{}, false
	}
	return best, true
}

// vp9NeighborIsInter mirrors libvpx's is_inter_block(MODE_INFO *mi) helper.
//
// libvpx: vp9_blockd.h is_inter_block — ref_frame[0] > INTRA_FRAME.
func vp9NeighborIsInter(mi *vp9dec.NeighborMi) bool {
	if mi == nil {
		return false
	}
	return mi.RefFrame[0] > vp9dec.IntraFrame
}
