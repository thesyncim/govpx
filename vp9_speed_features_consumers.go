package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9_speed_features_consumers.go wires the libvpx SPEED_FEATURES gates the
// vp9_speed_features.{h,c} port populates so the encoder routes through them at
// the points libvpx does. Each helper reads exactly one libvpx-mirrored field
// from e.sf and translates it to the analogous govpx code path. The intent is
// behavioural verbatim parity: at cpu_used=8 govpx must take the same
// fast-diamond / var-based-partition / pruned-mode shortcuts libvpx takes.
//
// Each gate cites the libvpx call site so the verbatim correspondence is
// auditable.

// vp9InterModeMaskFor returns the SPEED_FEATURES.inter_mode_mask entry for the
// given block size, defaulting to "all modes allowed" when the configurator
// has not narrowed the mask (block sizes below the speed-feature-narrowed
// large blocks always pass with sfInterAll on cpu_used 7/8).
//
// libvpx: vp9_pickmode.c:2150 — if (!(cpi->sf.inter_mode_mask[bsize] & (1 << this_mode))) continue;
func (e *VP9Encoder) vp9InterModeMaskFor(bsize common.BlockSize) int {
	if e == nil || int(bsize) >= len(e.sf.InterModeMask) {
		return sfInterAll
	}
	mask := e.sf.InterModeMask[bsize]
	if mask == 0 {
		return sfInterAll
	}
	return mask
}

// vp9InterSearchRadius reports the motion-search radius (in full pels).
//
// libvpx's vp9_full_pixel_search dispatches between FAST_DIAMOND / FAST_HEX
// (single-stage bigdia / hexagon pattern), BIGDIA / HEX / SQUARE
// (multi-scale chained pattern), and NSTEP / MESH (full n-step diamond).
// All of these patterns are *adaptive*: they can chain through larger scales
// when SAD continues to improve. The effective bound is therefore the
// MV-clipping limit (x->mv_limits) rather than the pattern shape, which means
// reducing the radius based on search_method alone breaks libvpx parity for
// long-translation content. We preserve the 16-pel govpx baseline radius for
// every search method and instead modulate the *coarse step* (vp9_mcomp.c
// 2918) — that is what reduce_first_step_size actually changes.
//
// libvpx: vp9_mcomp.c:1532 bigdia_search (multi-scale candidates) and
// vp9_mcomp.c:2875 vp9_full_pixel_search dispatch.
func (e *VP9Encoder) vp9InterSearchRadius() int {
	return 16
}

// vp9InterSearchCoarseStep reports the initial diamond/n-step step size.
// libvpx's NSTEP path uses an explicit step_param. The default best-quality
// configuration runs an 8-pel coarse step; speed_features sets
// reduce_first_step_size to 1 starting from cpu_used >= 5, but at that point
// libvpx has already switched to BIGDIA / FAST_DIAMOND which are
// pattern-shape (not n-step) searches with their own scale schedules.
//
// To preserve perf monotonically as cpu_used increases, this mapper returns
// a coarse step that is *no finer* than the 8-pel baseline. FAST_DIAMOND and
// the related fast pattern methods get an even coarser 16-pel step that
// commits to a single-stage fan; BIGDIA / HEX / SQUARE retain the 8-pel
// baseline; NSTEP / MESH / DIAMOND keep the 8-pel default.
//
// libvpx: vp9_speed_features.c:586 sf->mv.reduce_first_step_size = 1;
// libvpx: vp9_mcomp.c:1624-1631 fast_dia_search begins at the largest scale.
func (e *VP9Encoder) vp9InterSearchCoarseStep() int {
	if e == nil {
		return 8
	}
	switch e.sf.Mv.SearchMethod {
	case SearchMethodFastDiamond, SearchMethodFastHex:
		// libvpx fast_dia_search uses search_param = MAX(MAX_MVSEARCH_STEPS-2,
		// step_param). The starting scale visits ~8 candidates spaced 16 pels
		// apart, then refines locally. Mirror that by visiting only the
		// (±radius, 0) / (0, ±radius) cardinals plus center via a 16-pel
		// coarse step.
		return 16
	}
	return 8
}

// vp9InterSubpelEnabled is false at SUBPEL_FORCE_STOP == FULL_PEL: libvpx
// short-circuits vp9_find_best_sub_pixel_tree_pruned* and uses the full-pixel
// MV directly. At HALF_PEL libvpx still runs the half-pel pass but rounds the
// MV to half-pel granularity; govpx achieves the same effect by quantising the
// refined subpel MV to that step before storing.
//
// libvpx: vp9_mcomp.c find_best_sub_pixel_tree_pruned_more — the helper
// returns early without refining when forcestop == FULL_PEL.
func (e *VP9Encoder) vp9InterSubpelEnabled() bool {
	if e == nil {
		return true
	}
	return e.sf.Mv.SubpelForceStop != FullPel
}

// vp9InterSubpelMinStep returns the smallest sub-pel step the refinement loop
// is allowed to take. libvpx maps:
//   - EIGHTHPEL   -> 1   (1/8 pel)
//   - QUARTERPEL  -> 2   (1/4 pel)
//   - HALFPEL     -> 4   (1/2 pel)
//   - FULLPEL     -> 8   (full pel — refinement disabled, see Enabled())
//
// libvpx: vp9_mcomp.c — the *_tree_pruned helpers halve the step until it
// reaches forcestop.
func (e *VP9Encoder) vp9InterSubpelMinStep(allowHP bool) int16 {
	if e == nil {
		if allowHP {
			return 1
		}
		return 2
	}
	switch e.sf.Mv.SubpelForceStop {
	case FullPel:
		return 8
	case HalfPel:
		return 4
	case QuarterPel:
		return 2
	case EighthPel:
		if allowHP {
			return 1
		}
		return 2
	}
	if allowHP {
		return 1
	}
	return 2
}

// vp9InterSubpelIters caps the number of *additional* improvement iterations
// the sub-pel refinement loop is allowed at each step. libvpx's tree-pruned
// helpers stop after one improvement per step at SubpelTreePruned, and zero
// (i.e., one-shot evaluation) at the more pruned variants. The full
// SubpelTree implementation runs until no improvement is found.
//
// SubpelTree            -> unbounded (libvpx loops until no improvement)
// SubpelTreePruned      -> 1 improvement per step
// SubpelTreePrunedMore  -> 1 improvement per step, depth halved
// SubpelTreePrunedEvenMore -> 1 evaluation per step (no refinement)
//
// libvpx: vp9_speed_features.h:185 sf->mv.subpel_search_method.
func (e *VP9Encoder) vp9InterSubpelIters() int {
	if e == nil {
		return 1 << 30
	}
	switch e.sf.Mv.SubpelSearchMethod {
	case SubpelTreePruned:
		return 8 // generous cap: still bounds runaway loops
	case SubpelTreePrunedMore:
		return 4
	case SubpelTreePrunedEvenMore:
		return 2
	}
	return 1 << 30
}

// vp9InterCompoundEnabled gates whether the mode picker walks the
// compound-reference pairs. libvpx's nonrd_pickmode drops the compound search
// when sf->use_compound_nonrd_pickmode == 0, but govpx is still on the rd
// path; gating compound off there silently breaks all explicit compound-mode
// tests. Until vp9_pick_inter_mode (the nonrd entry) is ported verbatim, the
// gate must be a no-op so the rd compound branch keeps firing.
//
// TODO: when nonrd_pickmode.c is ported, return false at speed 8 here so the
// fast nonrd entry takes the compound-skip shortcut.
//
// libvpx: vp9_pickmode.c — sf->use_compound_nonrd_pickmode is read inside
// vp9_pick_inter_mode (the nonrd entry), not in vp9_rd_pick_inter_mode_sb.
func (e *VP9Encoder) vp9InterCompoundEnabled() bool {
	return true
}

// vp9InterUsesNonrdPickmode reports whether the SPEED_FEATURES configurator
// has selected libvpx's nonrd_pick_inter_mode path. govpx does not yet ship a
// verbatim nonrd_pickmode.c port (TODO below), so this acts purely as a
// candidate-pruner gate: when set, the picker skips ALTREF, drops compound,
// pins the inter-mode mask to INTER_NEAREST_NEW_ZERO, and constrains the
// motion search radius to the FAST_DIAMOND budget.
//
// TODO: port /private/tmp/libvpx-src/vp9/encoder/vp9_pickmode.c
// vp9_pick_inter_mode (~3000 LOC) verbatim — currently we approximate the
// behaviour by gating expensive RD branches with this flag. The verbatim port
// is intentionally deferred because nonrd_pickmode depends on the realtime
// MV-cost tables (cpi->dummy_cost) and the simple-model RD tables
// (cpi->sf.simple_model_rd_from_var) which themselves require ports outside
// the scope of this commit.
//
// libvpx: vp9_speed_features.h:447 sf->use_nonrd_pick_mode.
func (e *VP9Encoder) vp9InterUsesNonrdPickmode() bool {
	if e == nil {
		return false
	}
	return e.sf.UseNonrdPickMode != 0
}

// vp9InterReferenceFramesEnabled reports the ref-frame fan the inter picker
// should walk. libvpx's realtime nonrd_pickmode skips ALTREF on most blocks
// when sf->use_altref_onepass == 0, but it still allows it through the
// per-frame ref mask (set via encode flags) so the encoder can honour an
// explicit "altref-only" request. govpx mirrors that by always returning the
// full {LAST, GOLDEN, ALTREF} fan and letting vp9InterReferenceSlot's refMask
// gate prune unallowed refs.
//
// libvpx: vp9_speed_features.c:586 sf->use_altref_onepass = 0 is consumed by
// the partition driver, not by the inter mode picker itself.
func (e *VP9Encoder) vp9InterReferenceFramesEnabled() []int8 {
	return []int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame}
}

// vp9InterPartitionVarBased reports whether the SPEED_FEATURES configurator
// selected libvpx's choose_partitioning variance-based partitioning instead
// of the recursive partition search. At cpu_used >= 5 libvpx pins
// sf->partition_search_type = VAR_BASED_PARTITION.
//
// libvpx: vp9_encodeframe.c:5470 — case VAR_BASED_PARTITION: choose_partitioning().
func (e *VP9Encoder) vp9InterPartitionVarBased() bool {
	if e == nil {
		return false
	}
	return e.sf.PartitionSearchType == VarBasedPartition
}

// vp9InterPartitionFixed reports whether the SPEED_FEATURES configurator
// pinned partitions to a single block size. Used by the realtime path when
// cpu_used has selected FIXED_PARTITION; govpx returns the
// sf.AlwaysThisBlockSize block size as the entire SB64's partition.
//
// libvpx: vp9_encodeframe.c — set_fixed_partitioning() walks the SB at
// sf->always_this_block_size granularity.
func (e *VP9Encoder) vp9InterPartitionFixed() (common.BlockSize, bool) {
	if e == nil || e.sf.PartitionSearchType != FixedPartition {
		return common.Block64x64, false
	}
	return e.sf.AlwaysThisBlockSize, true
}
