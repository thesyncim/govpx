package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func vp9SetGoodSpeedFeatureFramesizeDependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	minFrameSize := vp9MinDim(ctx.width, ctx.height)
	is480pOrLarger := minFrameSize >= 480
	is720pOrLarger := minFrameSize >= 720
	is1080pOrLarger := minFrameSize >= 1080
	is2160pOrLarger := minFrameSize >= 2160
	boosted := vp9FrameIsKfGfArf(ctx)

	// speed 0 features. libvpx: vp9_speed_features.c:76-79.
	sf.PartitionSearchBreakoutThr.Dist = 1 << 20
	sf.PartitionSearchBreakoutThr.Rate = 80
	sf.UseSquareOnlyThreshHigh = common.BlockSizes
	sf.UseSquareOnlyThreshLow = common.Block4x4

	if is480pOrLarger {
		// libvpx: vp9_speed_features.c:81-86.
		sf.RdMlPartition.SearchEarlyTermination = 1
		sf.RecodeToleranceHigh = 45
	} else {
		sf.UseSquareOnlyThreshHigh = common.Block32x32
	}
	if is720pOrLarger {
		sf.AltRefSearchFp = 1
	}

	if !is1080pOrLarger {
		// libvpx: vp9_speed_features.c:93-104.
		sf.RdMlPartition.SearchBreakout = 1
		if is720pOrLarger {
			sf.RdMlPartition.SearchBreakoutThresh[0] = 0.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = 0.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = 0.0
		} else {
			sf.RdMlPartition.SearchBreakoutThresh[0] = 2.5
			sf.RdMlPartition.SearchBreakoutThresh[1] = 1.5
			sf.RdMlPartition.SearchBreakoutThresh[2] = 1.5
		}
	}

	if !is720pOrLarger {
		// libvpx: vp9_speed_features.c:106-111.
		if is480pOrLarger {
			if boosted {
				sf.PruneSingleModeBasedOnMvDiffModeRate = 0
			} else {
				sf.PruneSingleModeBasedOnMvDiffModeRate = 1
			}
		} else {
			sf.PruneSingleModeBasedOnMvDiffModeRate = 1
		}
	}

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:113-142.
		sf.RdMlPartition.SearchEarlyTermination = 0
		sf.RdMlPartition.SearchBreakout = 1
		if is480pOrLarger {
			sf.UseSquareOnlyThreshHigh = common.Block64x64
		} else {
			sf.UseSquareOnlyThreshHigh = common.Block32x32
		}
		sf.UseSquareOnlyThreshLow = common.Block16x16
		if is720pOrLarger {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
			sf.PartitionSearchBreakoutThr.Dist = 1 << 22
			sf.RdMlPartition.SearchBreakoutThresh[0] = -5.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = -5.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = -9.0
		} else {
			sf.DisableSplitMask = sfDisableCompoundSplit
			sf.PartitionSearchBreakoutThr.Dist = 1 << 21
			sf.RdMlPartition.SearchBreakoutThresh[0] = -1.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = -1.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = -1.0
		}
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:144-174.
		sf.UseSquareOnlyThreshHigh = common.Block4x4
		sf.UseSquareOnlyThreshLow = common.BlockSizes
		if is720pOrLarger {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
			sf.AdaptivePredInterpFilter = 0
			sf.PartitionSearchBreakoutThr.Dist = 1 << 24
			sf.PartitionSearchBreakoutThr.Rate = 120
			sf.RdMlPartition.SearchBreakout = 0
		} else {
			sf.DisableSplitMask = sfLastAndIntraSplitOnly
			sf.PartitionSearchBreakoutThr.Dist = 1 << 22
			sf.PartitionSearchBreakoutThr.Rate = 100
			sf.RdMlPartition.SearchBreakoutThresh[0] = 0.0
			sf.RdMlPartition.SearchBreakoutThresh[1] = -1.0
			sf.RdMlPartition.SearchBreakoutThresh[2] = -4.0
		}
		sf.RdAutoPartitionMinLimit = vp9SetPartitionMinLimit(ctx.width, ctx.height)

		if is2160pOrLarger {
			// libvpx: vp9_speed_features.c:165-173.
			sf.UseSquarePartitionOnly = 1
			sf.IntraYModeMask[common.Tx32x32] = sfIntraDC
			sf.IntraUvModeMask[common.Tx32x32] = sfIntraDC
			sf.AltRefSearchFp = 1
			sf.CbPredFilterSearch = 2
			sf.AdaptiveInterpFilterSearch = 1
			sf.DisableSplitMask = sfDisableAllSplit
		}
	}

	if speed >= 3 {
		// libvpx: vp9_speed_features.c:176-190.
		sf.RdMlPartition.SearchBreakout = 0
		if is720pOrLarger {
			sf.DisableSplitMask = sfDisableAllSplit
			if ctx.baseQIndex < 220 {
				sf.ScheduleModeSearch = 1
			} else {
				sf.ScheduleModeSearch = 0
			}
			sf.PartitionSearchBreakoutThr.Dist = 1 << 25
			sf.PartitionSearchBreakoutThr.Rate = 200
		} else {
			sf.MaxIntraBsize = common.Block32x32
			sf.DisableSplitMask = sfDisableAllInterSplit
			if ctx.baseQIndex < 175 {
				sf.ScheduleModeSearch = 1
			} else {
				sf.ScheduleModeSearch = 0
			}
			sf.PartitionSearchBreakoutThr.Dist = 1 << 23
			sf.PartitionSearchBreakoutThr.Rate = 120
		}
	}

	// libvpx: vp9_speed_features.c:195-199.
	if speed >= 1 && e.twoPass.enabled() &&
		(ctx.frContentType == vp9FCGraphicsAnimation || ctx.internalImageEdge) {
		sf.DisableSplitMask = sfDisableCompoundSplit
	}

	if speed >= 4 {
		// libvpx: vp9_speed_features.c:201-209.
		sf.PartitionSearchBreakoutThr.Rate = 300
		if is720pOrLarger {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 26
		} else {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 24
		}
		sf.DisableSplitMask = sfDisableAllSplit
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:211-213.
		sf.PartitionSearchBreakoutThr.Rate = 500
	}
}

// vp9SetGoodSpeedFeatureFramesizeIndependent ports
// set_good_speed_feature_framesize_independent().
//
// libvpx: vp9_speed_features.c:219-411.
func vp9SetGoodSpeedFeatureFramesizeIndependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	boosted := vp9FrameIsKfGfArf(ctx)

	// libvpx: vp9_speed_features.c:227-256.
	sf.AdaptiveInterpFilterSearch = 1
	sf.AdaptivePredInterpFilter = 1
	sf.AdaptiveRdThresh = 1
	sf.AdaptiveRdThreshRowMt = 0
	sf.AllowSkipRecode = 1
	sf.LessRectangularCheck = 1
	sf.Mv.AutoMvStepSize = 1
	sf.Mv.UseDownsampledSad = 1
	sf.PruneRefFrameForRectPartitions = 1
	sf.TemporalFilterSearchMethod = SearchMethodNStep
	sf.TxSizeSearchBreakout = 1
	if boosted {
		sf.UseSquarePartitionOnly = 0
	} else {
		sf.UseSquarePartitionOnly = 1
	}
	sf.EarlyTermInterpSearchPlaneRd = 1
	sf.CbPredFilterSearch = 1
	if sf.OptimizeCoefficients != 0 {
		sf.TrellisOptTxRd.Method = EnableTrellisOptTxRdResidualMse
	} else {
		sf.TrellisOptTxRd.Method = DisableTrellisOpt
	}
	if boosted {
		sf.TrellisOptTxRd.Thresh = 4.0
	} else {
		sf.TrellisOptTxRd.Thresh = 3.0
	}

	sf.IntraYModeMask[common.Tx32x32] = sfIntraDCHV
	sf.CompInterJointSearchIterLevel = 1

	// libvpx: vp9_speed_features.c:249-250 — reference masking unsupported in
	// dynamic resize. govpx does not currently expose resize_mode; assume
	// non-dynamic so reference_masking = 1.
	sf.ReferenceMasking = 1

	sf.RdMlPartition.VarPruning = 1
	sf.RdMlPartition.PruneRectThresh[0] = -1
	sf.RdMlPartition.PruneRectThresh[1] = 350
	sf.RdMlPartition.PruneRectThresh[2] = 325
	sf.RdMlPartition.PruneRectThresh[3] = 250

	// libvpx: vp9_speed_features.c:258-262.
	if ctx.frContentType == vp9FCGraphicsAnimation {
		sf.ExhaustiveSearchesThresh = 1 << 22
	} else {
		sf.ExhaustiveSearchesThresh = math.MaxInt32
	}

	for i := range sfMaxMeshSteps {
		sf.MeshPatterns[i] = vp9GoodQualityMeshPatterns[0][i]
	}

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:272-316.
		if boosted {
			sf.RdMlPartition.VarPruning = 0
		} else {
			sf.RdMlPartition.VarPruning = 1
		}
		sf.RdMlPartition.PruneRectThresh[1] = 225
		sf.RdMlPartition.PruneRectThresh[2] = 225
		sf.RdMlPartition.PruneRectThresh[3] = 225

		// libvpx: vp9_speed_features.c:278-288.
		if e.twoPass.enabled() &&
			(ctx.frContentType == vp9FCGraphicsAnimation || ctx.internalImageEdge) {
			sf.UseSquarePartitionOnly = 0
			if !boosted {
				sf.UseSquarePartitionOnly = 1
			}
		} else {
			sf.UseSquarePartitionOnly = 0
			if !vp9FrameIsIntraOnly(ctx) {
				sf.UseSquarePartitionOnly = 1
			}
		}

		sf.AllowTxfmDomainDistortion = 1
		idx := speed
		if idx >= 6 {
			idx = 5
		}
		sf.TxDomainThresh = vp9TxDomThresholds[idx]
		if sf.OptimizeCoefficients != 0 {
			sf.TrellisOptTxRd.Method = EnableTrellisOptTxRdSrcVar
		} else {
			sf.TrellisOptTxRd.Method = DisableTrellisOpt
		}
		sf.TrellisOptTxRd.Thresh = vp9QoptThresholds[idx]
		sf.LessRectangularCheck = 1
		sf.UseRdBreakout = 1
		sf.AdaptiveMotionSearch = 1
		sf.AdaptiveRdThresh = 2
		sf.Mv.SubpelSearchLevel = 1
		if vp9ResolveContent(e.opts.ScreenContentMode) != vp9ContentFilm {
			sf.ModeSkipStart = 10
		}
		sf.AllowAcl = 0

		sf.IntraUvModeMask[common.Tx32x32] = sfIntraDCHV
		if vp9ResolveContent(e.opts.ScreenContentMode) != vp9ContentFilm {
			sf.IntraYModeMask[common.Tx16x16] = sfIntraDCHV
			sf.IntraUvModeMask[common.Tx16x16] = sfIntraDCHV
		}

		sf.RecodeToleranceLow = 15
		sf.RecodeToleranceHigh = 30

		// libvpx: vp9_speed_features.c:313-315.
		if ctx.frContentType == vp9FCGraphicsAnimation {
			sf.ExhaustiveSearchesThresh = 1 << 23
		} else {
			sf.ExhaustiveSearchesThresh = math.MaxInt32
		}
		sf.UseAccurateSubpelSearch = Use4Taps
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:319-356.
		sf.RdMlPartition.VarPruning = 0
		// libvpx: vp9_speed_features.c:321-324 — oxcf->vbr_corpus_complexity
		// fork. When corpus VBR is active, libvpx widens the recode loop to
		// ALLOW_RECODE_FIRST (loop after the first encode attempt) so the
		// per-frame Q can hit the corpus-relative target; otherwise the
		// non-corpus path uses ALLOW_RECODE_KFARFGF.
		if e.opts.VBRCorpusComplexity != 0 {
			sf.RecodeLoop = RecodeLoopAllowFirst
		} else {
			sf.RecodeLoop = RecodeLoopAllowKfArfGf
		}

		if vp9FrameIsKfGfArf(ctx) {
			sf.TxSizeSearchMethod = UseFullRD
		} else {
			sf.TxSizeSearchMethod = UseLargestAll
		}

		if ctx.frameType == common.KeyFrame {
			sf.ModeSearchSkipFlags = 0
		} else {
			sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch | FlagSkipIntraBestInter |
				FlagSkipCompBestIntra | FlagSkipIntraLowVar
		}
		sf.DisableFilterSearchVarThresh = 100
		sf.CompInterJointSearchIterLevel = 2
		sf.AutoMinMaxPartitionSize = AutoMinMaxRelaxedNeighboring
		sf.RecodeToleranceHigh = 45
		sf.EnhancedFullPixelMotionSearch = 0
		sf.PruneRefFrameForRectPartitions = 0
		sf.RdMlPartition.PruneRectThresh[1] = -1
		sf.RdMlPartition.PruneRectThresh[2] = -1
		sf.RdMlPartition.PruneRectThresh[3] = -1
		sf.Mv.SubpelSearchLevel = 0

		// libvpx: vp9_speed_features.c:345-353.
		if ctx.frContentType == vp9FCGraphicsAnimation {
			for i := range sfMaxMeshSteps {
				sf.MeshPatterns[i] = vp9GoodQualityMeshPatterns[1][i]
			}
		}

		sf.UseAccurateSubpelSearch = Use2Taps
	}

	if speed >= 3 {
		// libvpx: vp9_speed_features.c:358-383.
		if vp9FrameIsIntraOnly(ctx) {
			sf.UseSquarePartitionOnly = 0
		} else {
			sf.UseSquarePartitionOnly = 1
		}
		if vp9FrameIsIntraOnly(ctx) {
			sf.TxSizeSearchMethod = UseFullRD
		} else {
			sf.TxSizeSearchMethod = UseLargestAll
		}
		sf.Mv.SubpelSearchMethod = SubpelTreePruned
		sf.AdaptivePredInterpFilter = 0
		sf.AdaptiveModeSearch = 1
		if boosted {
			sf.CbPartitionSearch = 0
		} else {
			sf.CbPartitionSearch = 1
		}
		sf.CbPredFilterSearch = 2
		sf.AltRefSearchFp = 1
		sf.RecodeLoop = RecodeLoopAllowKfMaxBw
		sf.AdaptiveRdThresh = 3
		sf.ModeSkipStart = 6
		sf.IntraYModeMask[common.Tx32x32] = sfIntraDC
		sf.IntraUvModeMask[common.Tx32x32] = sfIntraDC
		if ctx.frContentType == vp9FCGraphicsAnimation {
			for i := range sfMaxMeshSteps {
				sf.MeshPatterns[i] = vp9GoodQualityMeshPatterns[2][i]
			}
		}
	}

	if speed >= 4 {
		// libvpx: vp9_speed_features.c:385-398.
		sf.UseSquarePartitionOnly = 1
		sf.TxSizeSearchMethod = UseLargestAll
		sf.Mv.SearchMethod = SearchMethodBigDia
		sf.Mv.SubpelSearchMethod = SubpelTreePrunedMore
		sf.AdaptiveRdThresh = 4
		if ctx.frameType != common.KeyFrame {
			sf.ModeSearchSkipFlags |= FlagEarlyTerminate
		}
		sf.DisableFilterSearchVarThresh = 200
		sf.UseLp32x32Fdct = 1
		sf.UseFastCoefUpdates = OneLoopReduced
		sf.UseFastCoefCosting = 1
		if boosted {
			sf.MotionFieldModeSearch = 0
		} else {
			sf.MotionFieldModeSearch = 1
		}
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:400-410.
		sf.OptimizeCoefficients = 0
		sf.Mv.SearchMethod = SearchMethodHex
		sf.DisableFilterSearchVarThresh = 500
		for i := range common.TxSizes {
			sf.IntraYModeMask[i] = sfIntraDC
			sf.IntraUvModeMask[i] = sfIntraDC
		}
		sf.Mv.ReduceFirstStepSize = 1
		sf.SimpleModelRdFromVar = 1
	}
}

// vp9SetRtSpeedFeatureFramesizeDependent ports
// set_rt_speed_feature_framesize_dependent().
//
// libvpx: vp9_speed_features.c:414-450.
