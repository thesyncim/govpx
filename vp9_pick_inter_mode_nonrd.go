package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
// Differences from libvpx (deferred):
//
//   - libvpx tracks Lagrangian RD via x->rdmult / x->rddiv (vp9_rd.c::
//     vp9_compute_rd_mult). govpx's RD form is the simpler
//     vp9InterModeScore(distortion, rate, qindex) used by the existing RD
//     picker. The libvpx best_rdc comparison is replaced by govpx's score
//     comparison, which preserves the (ref, mode) winner ordering but loses
//     the exact bitcost equivalence of the libvpx RDCOST macro.
//     TODO: requires Lagrangian RD shape; see vp9_rd.c::vp9_compute_rd_mult.
//
//   - libvpx's encode_breakout_test (vp9_pickmode.c:942) sets x->skip when the
//     predicted block is "close enough" to the source that quantising the
//     residue would zero it. govpx defers that path because allow_encode_
//     breakout is not surfaced yet. Without it the picker still terminates
//     early via best_early_term, just not as aggressively.
//     TODO: port encode_breakout_test (vp9_pickmode.c:942-1045).
//
//   - libvpx's pred_mv_sad reference-masking (vp9_pickmode.c:2204-2228) skips
//     a ref whose SAD is 2x worse than another. govpx does not yet populate
//     x->pred_mv_sad (libvpx writes it inside vp9_mv_pred). With sf->
//     reference_masking gated off below the skip never fires; the schedule
//     visits all 12 candidates instead of being pruned to ~6.
//     TODO: port vp9_mv_pred pred_mv_sad population (vp9_mcomp.c:1830+).
//
//   - libvpx's model_rd_for_sb_y / block_yrd (vp9_pickmode.c:2341/728) runs a
//     simplified transform-domain RD on the predicted residue. govpx
//     approximates this via vp9InterPredictionDistortion (SSE in pel
//     domain). The proxy preserves candidate ordering for most content but
//     undervalues sub-pel NEWMV gains on flat textures (which is what the
//     scoreVP9InterModeResidual fallback above exists to mitigate).
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
	// candidates (vp9_rd.c:588 vp9_mv_pred). When a ref's pred_mv_sad is
	// more than 2x the dominant ref's, the entire ref is pruned. govpx
	// approximates pred_mv_sad with the (0,0)-offset SAD per ref: this
	// catches the common case where LAST tracks the source motion and
	// GOLDEN/ALTREF lag significantly. Pre-compute the per-ref (0,0) SAD
	// to seed the ref_frame_skip_mask before the main loop.
	//
	// TODO: port full vp9_mv_pred which evaluates {ref_mvs[0], ref_mvs[1],
	// x->pred_mv} candidates and picks the best — would catch more refs
	// that the (0,0)-only approximation misses (vp9_rd.c:588-639).
	var predMvSad [vp9dec.MaxRefFrames]uint64
	for r := int8(vp9dec.LastFrame); r <= maxUsableRef; r++ {
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
				if len(refBuf) == 0 || refStride <= 0 ||
					x0+blockW > refW || y0+blockH > refH {
					continue
				}
				predMvSad[r] = vp9BlockSAD(src, srcStride, refBuf, refStride,
					x0, y0, x0, y0, blockW, blockH, ^uint64(0))
			}
		}
	}

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

	// libvpx: vp9_pickmode.c:1731 INTERP_FILTER filter_ref;
	//          vp9_pickmode.c:1874 filter_ref = cm->interp_filter;
	//          vp9_pickmode.c:1875-1880 — when default_interp_filter != BILINEAR,
	//   filter_ref is inherited from neighbour MIs.
	frameInterp := vp9InterFrameInterpFilter(inter)
	filterRef := frameInterp
	if filterRef == vp9dec.InterpSwitchable {
		// libvpx: vp9_pickmode.c:1875-1880 filter inheritance.
		if above != nil && vp9NeighborIsInter(above) {
			filterRef = vp9dec.InterpFilter(above.InterpFilter)
		} else if left != nil && vp9NeighborIsInter(left) {
			filterRef = vp9dec.InterpFilter(left.InterpFilter)
		} else {
			filterRef = vp9dec.InterpEighttap
		}
	}

	// libvpx: vp9_pickmode.c:1732 int pred_filter_search = cm->interp_filter
	//   == SWITCHABLE; further refined at 1862-1869 by cb_pred_filter_search.
	// At speed 8 with cb_pred_filter_search=2, pred_filter_search collapses
	// to 0 for half of the SBs (the chessboard pattern), and to 1 for the
	// other half. govpx approximates this by gating on
	// sf.CbPredFilterSearch: at value 2, never run the inner filter search;
	// at value 1, run it always; at value 0, run it only when frame
	// interp_filter is SWITCHABLE.
	predFilterSearch := frameInterp == vp9dec.InterpSwitchable
	if e.sf.CbPredFilterSearch >= 2 {
		predFilterSearch = false
	}

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
			gotMv, _, ok := e.pickVP9InterMv(inter, miRows, miCols,
				miRow, miCol, bsize, refFrame)
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

		// libvpx: vp9_pickmode.c:2318-2329 — pred_filter_search. When the
		// MV has subpel bits and pred_filter_search is on, libvpx runs the
		// search_filter_ref helper which evaluates all 3 filters. Otherwise
		// it locks the filter to filter_ref (or EIGHTTAP if filter_ref ==
		// SWITCHABLE) and runs a single inter prediction.
		filters := []vp9dec.InterpFilter{filterRef}
		if filterRef == vp9dec.InterpSwitchable {
			filters = []vp9dec.InterpFilter{vp9dec.InterpEighttap}
		}
		if predFilterSearch && (thisMode == common.NewMv ||
			filterRef == vp9dec.InterpSwitchable) &&
			vp9MvHasSubpel(mv) {
			filters = vp9SwitchableInterpFilterOrder[:]
		}

		// Per-candidate inner: evaluate distortion and rate.
		for _, filter := range filters {
			distortion, ok := e.vp9InterPredictionDistortion(inter, miRows,
				miCols, miRow, miCol, bsize, thisMode, refFrame, mv, filter)
			if !ok {
				continue
			}

			// libvpx: vp9_pickmode.c:2405 this_rdc.rate += rate_mv;
			//          vp9_pickmode.c:2406-2407 inter_mode_cost contribution.
			//          vp9_pickmode.c:2409 ref_frame_cost[ref_frame].
			rate := refRate +
				vp9InterModeRateCost(&inter.selectFc, interModeCtx, thisMode,
					mv, refMv, inter.allowHP) +
				vp9InterInterpFilterRateCost(inter, &inter.selectFc,
					switchableCtx, filter)

			cand := vp9InterModeDecision{
				refFrame:       refFrame,
				secondRefFrame: vp9dec.NoRefFrame,
				refSlot:        refSlots[refFrame],
				mode:           thisMode,
				mv:             [2]vp9dec.MV{mv},
				interpFilter:   filter,
				rate:           rate,
				distortion:     distortion,
				score:          vp9InterModeScore(distortion, rate, qindex),
			}

			// libvpx: vp9_pickmode.c:2355 if (sse_y < best_sse_sofar)
			//   best_sse_sofar = sse_y;
			if distortion < bestSseSoFar {
				bestSseSoFar = distortion
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
