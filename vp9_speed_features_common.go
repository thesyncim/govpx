package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
	if sf.AdaptiveRdThreshRowMt == 0 && e.opts.Threads > 1 && e.opts.RowMT {
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
	if sf.AdaptiveRdThreshRowMt == 0 && e.opts.Threads > 1 && e.opts.RowMT {
		sf.AdaptiveRdThresh = 0
	}
}

// vp9SetGoodSpeedFeatureFramesizeDependent ports
// set_good_speed_feature_framesize_dependent().
//
// libvpx: vp9_speed_features.c:64-214.
