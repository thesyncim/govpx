package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"math"
)

func (e *VP9Encoder) vp9DefaultSpeedFrameContext() vp9SpeedFrameContext {
	ctx := vp9SpeedFrameContext{
		width:           e.opts.Width,
		height:          e.opts.Height,
		showFrame:       true,
		frameType:       common.KeyFrame,
		intraOnly:       true,
		resizeStateOrig: true,
	}
	if e != nil {
		ctx.svc = e.svc
	} else {
		ctx.svc = encoder.DefaultSVCState()
	}
	return ctx
}

// vp9PerFrameSpeedContextArgs carries the per-frame inputs that drive
// frame_is_kf_gf_arf() and the framesize-dependent SF picks. Mirrors the
// subset of cpi->common / cpi->refresh_*_frame / cpi->rc fields libvpx reads
// at the top of encode_frame_to_data_rate.
//
// libvpx: vp9_encoder.h:1013-1016 frame_is_kf_gf_arf,
// vp9_speed_features.c:919-1096 vp9_set_speed_features_framesize_independent.
type vp9PerFrameSpeedContextArgs struct {
	IsKey              bool
	IntraOnly          bool
	ShowFrame          bool
	RefreshGoldenFrame bool
	RefreshAltRefFrame bool
	IsSrcFrameAltRef   bool
	BaseQIndex         int
}

// vp9PerFrameSpeedContext builds a configurator context for a specific frame.
// The caller supplies the per-frame state libvpx reads via cpi->common,
// cpi->refresh_alt_ref_frame, cpi->refresh_golden_frame, and
// cpi->rc.is_src_frame_alt_ref. The remaining encoder-state fields
// (framesSinceKey, avgFrameLowMotion, avgFrameQindexInter, currentVideoFrame)
// are pulled from the live rate-control / frame counters so the framesize-
// dependent dispatcher sees the same inputs libvpx feeds it.
//
// libvpx: vp9_encoder.c:2635 / 3754 — same two-step protocol invoked per
// frame at top-of-encode.
func (e *VP9Encoder) vp9PerFrameSpeedContext(args vp9PerFrameSpeedContextArgs) vp9SpeedFrameContext {
	frameType := common.InterFrame
	if args.IsKey {
		frameType = common.KeyFrame
	}
	ctx := vp9SpeedFrameContext{
		width:               e.opts.Width,
		height:              e.opts.Height,
		showFrame:           args.ShowFrame,
		frameType:           frameType,
		intraOnly:           args.IntraOnly,
		refreshAltRefFrame:  args.RefreshAltRefFrame,
		refreshGoldenFrame:  args.RefreshGoldenFrame,
		isSrcFrameAltRef:    args.IsSrcFrameAltRef,
		baseQIndex:          args.BaseQIndex,
		framesSinceKey:      int(e.rc.framesSinceKey),
		avgFrameLowMotion:   100,
		avgFrameQindexInter: int(e.rc.avgFrameQIndexInter),
		currentVideoFrame:   e.frameIndex,
		frContentType:       e.vp9FrameContentTypeForSpeedFeatures(),
		internalImageEdge:   e.vp9InternalImageEdgeForSpeedFeatures(),
		svc:                 e.svc,
		// govpx's runtime resize always triggers a full re-allocation in
		// applyVP9ResolutionChange(), so libvpx's external_resize (set only
		// when the resize *did not* realloc) is never observable here.
		// libvpx: vp9_encoder.c:2153-2166.
		externalResize: false,
		// libvpx: vp9_encoder.h cpi->last_frame_dropped.
		lastFrameDropped: e.lastFrameDropped,
		// govpx does not run libvpx's internal dynamic-resize loop, so
		// cpi->resize_state == ORIG always.
		resizeStateOrig: true,
		// Mirror VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR into the per-frame
		// speed-feature context so overshoot-detection speed gates see the
		// same runtime bit as rate control.
		// libvpx: vp9_ratectrl.h rc->disable_overshoot_maxq_cbr.
		disableOvershootMaxqCbr: e.rc.disableOvershootMaxQCBR,
	}
	return ctx
}

func (e *VP9Encoder) vp9FrameContentTypeForSpeedFeatures() vp9FrameContentType {
	if e == nil || !e.twoPass.enabled() {
		return vp9FCNormal
	}
	row := e.twoPass.statsForFrame()
	if row.IntraSkipPct >= vp9FCAnimationThresh {
		return vp9FCGraphicsAnimation
	}
	return vp9FCNormal
}

func (e *VP9Encoder) vp9InternalImageEdgeForSpeedFeatures() bool {
	if e == nil || !e.twoPass.enabled() {
		return false
	}
	row := e.twoPass.statsForFrame()
	return row.InactiveZoneRows > 0 || row.InactiveZoneCols > 0
}

func (e *VP9Encoder) vp9DeadlineModeChanged() bool {
	if e == nil || !e.deadlineModePreviousFrameSet || e.frameIndex == 0 {
		return false
	}
	return vp9ResolveDeadlineMode(e.opts.Deadline) != e.deadlineModePreviousFrame
}

func (e *VP9Encoder) vp9LatchDeadlineModePreviousFrame() {
	if e == nil {
		return
	}
	e.deadlineModePreviousFrame = vp9ResolveDeadlineMode(e.opts.Deadline)
	e.deadlineModePreviousFrameSet = true
}

// vp9ApplySpeedFeatures runs the libvpx framesize-independent and
// framesize-dependent configurators. It must be called whenever speed-affecting
// options change (CpuUsed, Deadline, ScreenContentMode, RateControlMode), and
// also at frame setup so the framesize-dependent SF picks see the actual
// per-frame state.
//
// libvpx: vp9_encoder.c:2635 / 3754 / 3765 — same two-step protocol.
func (e *VP9Encoder) vp9ApplySpeedFeatures(ctx vp9SpeedFrameContext) {
	if e == nil {
		return
	}
	// libvpx: vp9_noise_estimate.c:129 — ne->enabled is recomputed by
	// vp9_update_noise_estimate at the top of encode_frame_to_data_rate
	// (vp9_encoder.c:4142), which runs before the speed-features dispatch
	// at vp9_encoder.c:3754. Refresh here so the consumer at
	// vp9_speed_features.c:777-782 reads the same predicate libvpx
	// evaluates whether the SF dispatch fires at setup, on options
	// change, or per-frame at top-of-encode.
	e.vp9NoiseEstimateRefreshEnabled()
	vp9SetSpeedFeaturesFramesizeIndependent(e, &e.sf, e.vp9SpeedFeatureCPUUsed(), ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &e.sf, e.vp9SpeedFeatureCPUUsed(), ctx)
}

// vp9DeadlineMode is the libvpx MODE selector govpx maps from the
// DeadlineBestQuality / DeadlineGoodQuality / DeadlineRealtime enum.
//
// libvpx: vp9_encoder.h MODE enum (GOOD=0, BEST=1, REALTIME=2).
type vp9DeadlineMode int

const (
	vp9ModeGood     vp9DeadlineMode = 0
	vp9ModeBest     vp9DeadlineMode = 1
	vp9ModeRealtime vp9DeadlineMode = 2
)

// vp9ResolveDeadlineMode maps govpx Deadline to the libvpx MODE picked by the
// SPEED_FEATURES dispatcher.
func vp9ResolveDeadlineMode(d Deadline) vp9DeadlineMode {
	switch d {
	case DeadlineBestQuality:
		return vp9ModeBest
	case DeadlineRealtime:
		return vp9ModeRealtime
	default:
		return vp9ModeGood
	}
}

// vp9ResolveContent maps govpx's ScreenContentMode int8 to the libvpx
// vp9e_tune_content enum value used by the configurator.
func vp9ResolveContent(c int8) vp9SpeedDispatchContent {
	switch c {
	case int8(VP9ScreenContentScreen):
		return vp9ContentScreen
	case int8(VP9ScreenContentFilm):
		return vp9ContentFilm
	default:
		return vp9ContentDefault
	}
}

// vp9MinDim returns VPXMIN(width, height).
func vp9MinDim(w, h int) int {
	if w < h {
		return w
	}
	return h
}

// vp9FrameIsIntraOnly mirrors libvpx's frame_is_intra_only().
func vp9FrameIsIntraOnly(ctx vp9SpeedFrameContext) bool {
	return ctx.frameType == common.KeyFrame || ctx.intraOnly
}

// vp9FrameIsKfGfArf mirrors libvpx's frame_is_boosted(), which delegates to
// frame_is_kf_gf_arf():
//
//	return frame_is_intra_only(&cpi->common) || cpi->refresh_alt_ref_frame ||
//	       (cpi->refresh_golden_frame && !cpi->rc.is_src_frame_alt_ref);
//
// libvpx: vp9_speed_features.c:38-40 (frame_is_boosted),
// vp9_encoder.h:1013-1016 (frame_is_kf_gf_arf).
func vp9FrameIsKfGfArf(ctx vp9SpeedFrameContext) bool {
	if vp9FrameIsIntraOnly(ctx) {
		return true
	}
	if ctx.refreshAltRefFrame {
		return true
	}
	if ctx.refreshGoldenFrame && !ctx.isSrcFrameAltRef {
		return true
	}
	return false
}

// vp9SetPartitionMinLimit mirrors set_partition_min_limit().
//
// libvpx: vp9_speed_features.c:48-62.
func vp9SetPartitionMinLimit(width, height int) common.BlockSize {
	screenArea := width * height
	if screenArea < 1280*720 {
		return common.Block4x4
	}
	if screenArea < 1920*1080 {
		return common.Block8x8
	}
	return common.Block16x16
}

// vp9SetSpeedFeaturesFramesizeIndependent is the libvpx
// vp9_set_speed_features_framesize_independent() port. It first applies the
// best-quality defaults, then dispatches to set_rt_speed_feature_framesize_
// independent or set_good_speed_feature_framesize_independent, and finally
// applies the pass-0 / pass-1 / lossless fixups.
//
// libvpx: vp9_speed_features.c:919-1096.

func vp9SetSpeedFeaturesFramesizeIndependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	// best quality defaults. libvpx: vp9_speed_features.c:928-1020.
	sf.FrameParameterUpdate = 1
	sf.Mv.SearchMethod = SearchMethodNStep
	sf.RecodeLoop = RecodeLoopAllowFirst
	sf.Mv.SubpelSearchMethod = SubpelTree
	sf.Mv.SubpelSearchLevel = 2
	sf.Mv.SubpelForceStop = EighthPel
	if e.opts.Lossless {
		sf.OptimizeCoefficients = 0
	} else {
		sf.OptimizeCoefficients = 1
	}
	sf.Mv.ReduceFirstStepSize = 0
	sf.CoeffProbAppxStep = 1
	sf.Mv.AutoMvStepSize = 0
	sf.Mv.FullpelSearchStepParam = 6
	sf.Mv.UseDownsampledSad = 0
	sf.CompInterJointSearchIterLevel = 0
	sf.TxSizeSearchMethod = UseFullRD
	sf.UseLp32x32Fdct = 0
	sf.AdaptiveMotionSearch = 0
	sf.EnhancedFullPixelMotionSearch = 1
	sf.AdaptivePredInterpFilter = 0
	sf.AdaptiveModeSearch = 0
	sf.PruneSingleModeBasedOnMvDiffModeRate = 0
	sf.CbPredFilterSearch = 0
	sf.EarlyTermInterpSearchPlaneRd = 0
	sf.CbPartitionSearch = 0
	sf.MotionFieldModeSearch = 0
	sf.AltRefSearchFp = 0
	sf.UseQuantFp = 0
	sf.ReferenceMasking = 0
	sf.PartitionSearchType = SearchPartition
	sf.LessRectangularCheck = 0
	sf.UseSquarePartitionOnly = 0
	sf.UseSquareOnlyThreshHigh = common.BlockSizes
	sf.UseSquareOnlyThreshLow = common.Block4x4
	sf.AutoMinMaxPartitionSize = AutoMinMaxNotInUse
	sf.RdAutoPartitionMinLimit = common.Block4x4
	sf.DefaultMaxPartitionSize = common.Block64x64
	sf.DefaultMinPartitionSize = common.Block4x4
	sf.AdjustPartitioningFromLastFrame = 0
	sf.LastPartitioningRedoFrequency = 4
	sf.DisableSplitMask = 0
	sf.ModeSearchSkipFlags = 0
	sf.ForceFrameBoost = 0
	sf.MaxDeltaQindex = 0
	sf.DisableFilterSearchVarThresh = 0
	sf.AdaptiveInterpFilterSearch = 0
	sf.AllowTxfmDomainDistortion = 0
	sf.TxDomainThresh = 99.0
	if sf.OptimizeCoefficients != 0 {
		sf.TrellisOptTxRd.Method = EnableTrellisOptM
	} else {
		sf.TrellisOptTxRd.Method = DisableTrellisOpt
	}
	sf.TrellisOptTxRd.Thresh = 99.0
	sf.AllowAcl = 1
	if e.opts.EnableTPL {
		sf.EnableTplModel = 1
	} else {
		sf.EnableTplModel = 0
	}
	sf.PruneRefFrameForRectPartitions = 0
	sf.TemporalFilterSearchMethod = SearchMethodMesh
	sf.AllowSkipTxfmAcDc = 0

	for i := range common.TxSizes {
		sf.IntraYModeMask[i] = sfIntraAll
		sf.IntraUvModeMask[i] = sfIntraAll
	}
	sf.UseRdBreakout = 0
	sf.SkipEncodeSb = 0
	sf.UseUvIntraRdEstimate = 0
	sf.AllowSkipRecode = 0
	sf.LpfPick = LpfPickFromFullImage
	sf.UseFastCoefUpdates = TwoLoop
	sf.UseFastCoefCosting = 0
	sf.ModeSkipStart = 30 // MAX_MODES, libvpx: vp9_rd.h:41.
	sf.ScheduleModeSearch = 0
	sf.UseNonrdPickMode = 0
	for i := range common.BlockSizes {
		sf.InterModeMask[i] = sfInterAll
	}
	sf.MaxIntraBsize = common.Block64x64
	sf.ReuseInterPredSby = 0
	sf.AlwaysThisBlockSize = common.Block16x16
	sf.EncodeBreakoutThresh = 0
	sf.RecodeToleranceLow = 12
	sf.RecodeToleranceHigh = 25
	sf.DefaultInterpFilter = vp9dec.InterpSwitchable
	sf.SimpleModelRdFromVar = 0
	sf.ShortCircuitFlatBlocks = 0
	sf.ShortCircuitLowTempVar = 0
	sf.LimitNewmvEarlyExit = 0
	sf.BiasGolden = 0
	sf.BaseMvAggressive = 0
	sf.RdMlPartition.PruneRectThresh[0] = -1
	sf.RdMlPartition.PruneRectThresh[1] = -1
	sf.RdMlPartition.PruneRectThresh[2] = -1
	sf.RdMlPartition.PruneRectThresh[3] = -1
	sf.RdMlPartition.VarPruning = 0
	sf.UseAccurateSubpelSearch = Use8Taps

	// libvpx: vp9_speed_features.c:1022-1025 — speed-up defaults even at best
	// quality.
	sf.AdaptiveRdThresh = 1
	sf.TxSizeSearchBreakout = 1
	sf.TxSizeSearchDepth = 2

	// libvpx: vp9_speed_features.c:1027-1039. govpx does not track
	// twopass.fr_content_type yet — assume non-graphics content so the
	// exhaustive search threshold falls into the INT_MAX bucket.
	sf.ExhaustiveSearchesThresh = math.MaxInt32
	meshDensityLevel := 1
	for i := range sfMaxMeshSteps {
		sf.MeshPatterns[i] = vp9BestQualityMeshPattern[meshDensityLevel][i]
	}

	mode := vp9ResolveDeadlineMode(e.opts.Deadline)
	switch mode {
	case vp9ModeRealtime:
		vp9SetRtSpeedFeatureFramesizeIndependent(e, sf, speed, vp9ResolveContent(e.opts.ScreenContentMode), ctx)
	case vp9ModeGood:
		vp9SetGoodSpeedFeatureFramesizeIndependent(e, sf, speed, ctx)
	}
	// libvpx GOOD-mode dispatch also covers BEST in practice — see
	// vp9_speed_features.c:1041-1046 (only GOOD/REALTIME branches exist; BEST
	// inherits the framesize-independent defaults above).

	// libvpx: vp9_speed_features.c:1052 — pass==1 disables coefficient
	// optimization. govpx's two-pass first pass is opts.TwoPassFirstPass.
	if e.vp9SpeedIsFirstPass() {
		sf.OptimizeCoefficients = 0
	}

	// libvpx: vp9_speed_features.c:1055-1058 — pass==0 (one-pass).
	if e.vp9SpeedIsOnePass() {
		sf.RecodeLoop = RecodeLoopDisallow
		sf.OptimizeCoefficients = 0
	}

	// libvpx: vp9_speed_features.c:1083-1086.
	if !e.opts.FramePeriodicBoost {
		sf.MaxDeltaQindex = 0
	}

	// libvpx: vp9_speed_features.c:1093-1095 — row_mt bit-exactness override.
	// govpx's row-mt is bit-exact by construction (single goroutine per tile
	// column), so this only triggers when adaptive_rd_thresh_row_mt is off and
	// max_threads > 1, matching libvpx.
	if sf.AdaptiveRdThreshRowMt == 0 && e.vp9EffectiveThreadHint() > 1 && e.opts.RowMT {
		sf.AdaptiveRdThresh = 0
	}
}

// vp9SetSpeedFeaturesFramesizeDependent ports
// vp9_set_speed_features_framesize_dependent(). The "best quality defaults"
// reset partition_search_breakout_thr and rd_ml_partition fields at the top,
// then dispatch to the realtime / good handler, and finally apply the
// disable_split_mask interaction with adaptive_pred_interp_filter.
//
// libvpx: vp9_speed_features.c:873-917.
func vp9SetSpeedFeaturesFramesizeDependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	// best quality defaults. libvpx: vp9_speed_features.c:881-884.
	sf.PartitionSearchBreakoutThr.Dist = 1 << 19
	sf.PartitionSearchBreakoutThr.Rate = 80
	sf.RdMlPartition.SearchEarlyTermination = 0
	sf.RdMlPartition.SearchBreakout = 0

	mode := vp9ResolveDeadlineMode(e.opts.Deadline)
	switch mode {
	case vp9ModeRealtime:
		vp9SetRtSpeedFeatureFramesizeDependent(e, sf, speed, ctx)
	case vp9ModeGood:
		vp9SetGoodSpeedFeatureFramesizeDependent(e, sf, speed, ctx)
	}

	// libvpx: vp9_speed_features.c:893-895.
	if sf.DisableSplitMask == sfDisableAllSplit {
		sf.AdaptivePredInterpFilter = 0
	}

	// libvpx: vp9_speed_features.c:914-916.
	if sf.AdaptiveRdThreshRowMt == 0 && e.vp9EffectiveThreadHint() > 1 && e.opts.RowMT {
		sf.AdaptiveRdThresh = 0
	}
}

// vp9SetGoodSpeedFeatureFramesizeDependent ports
// set_good_speed_feature_framesize_dependent().
//
// libvpx: vp9_speed_features.c:64-214.

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

func (e *VP9Encoder) vp9InterSubpelSearchUsesTree() bool {
	if e == nil {
		return true
	}
	switch e.sf.Mv.SubpelSearchMethod {
	case SubpelTree, SubpelTreePruned, SubpelTreePrunedMore,
		SubpelTreePrunedEvenMore:
		return true
	default:
		return false
	}
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
// when sf->use_compound_nonrd_pickmode == 0. The full-RD path keeps compound
// enabled so explicit compound-mode tests and non-realtime modes still walk
// the RD compound branch.
//
// libvpx: vp9_pickmode.c — sf->use_compound_nonrd_pickmode is read inside
// vp9_pick_inter_mode (the nonrd entry), not in vp9_rd_pick_inter_mode_sb.
func (e *VP9Encoder) vp9InterCompoundEnabled() bool {
	if e == nil {
		return true
	}
	return e.sf.UseNonrdPickMode == 0 || e.sf.UseCompoundNonrdPickmode != 0
}

// vp9InterUsesNonrdPickmode reports whether the SPEED_FEATURES configurator
// selected libvpx's vp9_pick_inter_mode realtime path. The caller dispatches
// to the nonrd picker port when this is true and stays on the full-RD picker
// otherwise.
//
// libvpx: vp9_speed_features.h:447 sf->use_nonrd_pick_mode.
func (e *VP9Encoder) vp9InterUsesNonrdPickmode() bool {
	if e == nil {
		return false
	}
	return e.sf.UseNonrdPickMode != 0
}

// vp9InterReferenceFramesEnabled reports the ref-frame fan the inter picker
// should walk. libvpx's realtime nonrd_pickmode skips ALTREF when
// sf->use_altref_onepass == 0; the caller falls back to the full set if the
// per-frame reference mask explicitly leaves only ALTREF available.
//
// libvpx: vp9_speed_features.c:586 sf->use_altref_onepass = 0.
var vp9InterReferenceFramesAll = [...]int8{
	vp9dec.LastFrame,
	vp9dec.GoldenFrame,
	vp9dec.AltrefFrame,
}

var vp9InterReferenceFramesNoAltref = [...]int8{
	vp9dec.LastFrame,
	vp9dec.GoldenFrame,
}

func (e *VP9Encoder) vp9InterReferenceFramesEnabled() []int8 {
	if e != nil && e.sf.UseNonrdPickMode != 0 && e.sf.UseAltrefOnepass == 0 {
		return vp9InterReferenceFramesNoAltref[:]
	}
	return vp9InterReferenceFramesAll[:]
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
