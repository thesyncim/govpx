package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func vp9SetRtSpeedFeatureFramesizeDependent(e *VP9Encoder, sf *SpeedFeatures, speed int, ctx vp9SpeedFrameContext) {
	minDim := vp9MinDim(ctx.width, ctx.height)

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:419-426.
		if minDim >= 720 {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
		} else {
			sf.DisableSplitMask = sfDisableCompoundSplit
		}
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:428-435.
		if minDim >= 720 {
			if ctx.showFrame {
				sf.DisableSplitMask = sfDisableAllSplit
			} else {
				sf.DisableSplitMask = sfDisableAllInterSplit
			}
		} else {
			sf.DisableSplitMask = sfLastAndIntraSplitOnly
		}
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:437-444.
		sf.PartitionSearchBreakoutThr.Rate = 200
		if minDim >= 720 {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 25
		} else {
			sf.PartitionSearchBreakoutThr.Dist = 1 << 23
		}
	}

	if speed >= 7 {
		// libvpx: vp9_speed_features.c:446-449.
		if minDim >= 720 {
			sf.EncodeBreakoutThresh = 800
		} else {
			sf.EncodeBreakoutThresh = 300
		}
	}
}

// vp9SetRtSpeedFeatureFramesizeIndependent ports
// set_rt_speed_feature_framesize_independent().
//
// libvpx: vp9_speed_features.c:452-871.
func vp9SetRtSpeedFeatureFramesizeIndependent(e *VP9Encoder, sf *SpeedFeatures, speed int, content vp9SpeedDispatchContent, ctx vp9SpeedFrameContext) {
	isKeyframe := ctx.frameType == common.KeyFrame
	var framesSinceKey int
	if !isKeyframe {
		framesSinceKey = ctx.framesSinceKey
	}

	// libvpx: vp9_speed_features.c:458-483.
	sf.StaticSegmentation = 0
	sf.AdaptiveRdThresh = 1
	sf.AdaptiveRdThreshRowMt = 0
	sf.UseFastCoefCosting = 1
	sf.ExhaustiveSearchesThresh = math.MaxInt32
	sf.AllowAcl = 0
	sf.CopyPartitionFlag = 0
	sf.UseSourceSad = 0
	sf.UseSimpleBlockYrd = 0
	sf.AdaptPartitionSourceSad = 0
	sf.UseAltrefOnepass = 0
	sf.UseCompoundNonrdPickmode = 0
	sf.NonrdKeyframe = 0
	sf.SvcUseLowresPart = 0
	sf.OvershootDetectionCbrRt = OvershootNoDetection
	sf.Disable16x16PartNonKey = 0
	sf.DisableGoldenRef = 0
	sf.EnableTplModel = 0
	sf.EnhancedFullPixelMotionSearch = 0
	sf.UseAccurateSubpelSearch = Use2Taps
	sf.NonrdUseMlPartition = 0
	sf.VariancePartThreshMult = 1
	sf.CbPredFilterSearch = 0
	sf.ForceSmoothInterpol = 0
	sf.RtIntraDcOnlyLowContent = 0
	sf.Mv.EnableAdaptiveSubpelForceStop = 0

	if speed >= 1 {
		// libvpx: vp9_speed_features.c:485-504.
		sf.AllowTxfmDomainDistortion = 1
		sf.TxDomainThresh = 0.0
		sf.TrellisOptTxRd.Method = DisableTrellisOpt
		sf.TrellisOptTxRd.Thresh = 0.0
		if vp9FrameIsIntraOnly(ctx) {
			sf.UseSquarePartitionOnly = 0
		} else {
			sf.UseSquarePartitionOnly = 1
		}
		sf.LessRectangularCheck = 1
		if vp9FrameIsIntraOnly(ctx) {
			sf.TxSizeSearchMethod = UseFullRD
		} else {
			sf.TxSizeSearchMethod = UseLargestAll
		}

		sf.UseRdBreakout = 1

		sf.AdaptiveMotionSearch = 1
		sf.AdaptivePredInterpFilter = 1
		sf.Mv.AutoMvStepSize = 1
		sf.AdaptiveRdThresh = 2
		sf.IntraYModeMask[common.Tx32x32] = sfIntraDCHV
		sf.IntraUvModeMask[common.Tx32x32] = sfIntraDCHV
		sf.IntraUvModeMask[common.Tx16x16] = sfIntraDCHV
	}

	if speed >= 2 {
		// libvpx: vp9_speed_features.c:506-542.
		if ctx.frameType == common.KeyFrame {
			sf.ModeSearchSkipFlags = 0
		} else {
			sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch | FlagSkipIntraBestInter |
				FlagSkipCompBestIntra | FlagSkipIntraLowVar
		}
		sf.AdaptivePredInterpFilter = 2

		// libvpx: vp9_speed_features.c:514-531 — reference masking, SVC
		// downscale check. Enabled when there is exactly one spatial layer; the
		// dynamic-resize / vp9_is_scaled() inner check that libvpx adds for
		// resize_mode==RESIZE_DYNAMIC or external_resize==1 is a no-op in govpx
		// because applyVP9ResolutionChange() always invalidates every reference
		// frame (refValid[] = false), so the per-ref scale check would skip all
		// slots regardless.
		if ctx.svc.NumberSpatialLayers == 1 {
			sf.ReferenceMasking = 1
		} else {
			sf.ReferenceMasking = 0
		}
		// libvpx: vp9_speed_features.c:518-530 — inner per-reference
		// vp9_is_scaled() loop only fires when reference_masking==1 AND
		// (external_resize==1 OR resize_mode==RESIZE_DYNAMIC). govpx has no
		// dynamic-resize mode and external_resize is never observable (see
		// vp9SpeedFrameContext.externalResize), so the inner clear is a no-op.

		sf.DisableFilterSearchVarThresh = 50
		sf.CompInterJointSearchIterLevel = 2
		sf.AutoMinMaxPartitionSize = AutoMinMaxRelaxedNeighboring
		sf.LfMotionThreshold = LowMotionThreshold
		sf.AdjustPartitioningFromLastFrame = 1
		sf.LastPartitioningRedoFrequency = 3
		sf.UseLp32x32Fdct = 1
		sf.ModeSkipStart = 11
		sf.IntraYModeMask[common.Tx16x16] = sfIntraDCHV
	}

	if speed >= 3 {
		// libvpx: vp9_speed_features.c:544-556.
		sf.UseSquarePartitionOnly = 1
		sf.DisableFilterSearchVarThresh = 100
		sf.UseUvIntraRdEstimate = 1
		sf.SkipEncodeSb = 1
		sf.Mv.SubpelSearchLevel = 0
		sf.AdaptiveRdThresh = 4
		sf.ModeSkipStart = 6
		sf.AllowSkipRecode = 0
		sf.OptimizeCoefficients = 0
		sf.DisableSplitMask = sfDisableAllSplit
		sf.LpfPick = LpfPickFromQ
	}

	if speed >= 4 {
		// libvpx: vp9_speed_features.c:558-583.
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 {
			sf.UseAltrefOnepass = 1
		}
		sf.Mv.SubpelForceStop = QuarterPel
		for i := range common.TxSizes {
			sf.IntraYModeMask[i] = sfIntraDCHV
			sf.IntraUvModeMask[i] = sfIntraDC
		}
		sf.IntraYModeMask[common.Tx32x32] = sfIntraDC
		sf.FrameParameterUpdate = 0
		sf.Mv.SearchMethod = SearchMethodFastHex
		sf.AllowSkipRecode = 0
		sf.MaxIntraBsize = common.Block32x32
		sf.UseFastCoefCosting = 0
		if isKeyframe {
			sf.UseQuantFp = 0
		} else {
			sf.UseQuantFp = 1
		}
		sf.InterModeMask[common.Block32x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block32x64] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x64] = sfInterNearestNewZero
		sf.AdaptiveRdThresh = 2
		if isKeyframe {
			sf.UseFastCoefUpdates = TwoLoop
		} else {
			sf.UseFastCoefUpdates = OneLoopReduced
		}
		sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch
		if isKeyframe {
			sf.TxSizeSearchMethod = UseLargestAll
		} else {
			sf.TxSizeSearchMethod = UseTx8x8
		}
		sf.PartitionSearchType = VarBasedPartition
	}

	if speed >= 5 {
		// libvpx: vp9_speed_features.c:585-660.
		sf.UseAltrefOnepass = 0
		if isKeyframe {
			sf.UseQuantFp = 0
		} else {
			sf.UseQuantFp = 1
		}
		if isKeyframe {
			sf.AutoMinMaxPartitionSize = AutoMinMaxRelaxedNeighboring
		} else {
			sf.AutoMinMaxPartitionSize = AutoMinMaxStrictNeighboring
		}
		sf.DefaultMaxPartitionSize = common.Block32x32
		sf.DefaultMinPartitionSize = common.Block8x8
		if isKeyframe ||
			(sf.LastPartitioningRedoFrequency != 0 &&
				framesSinceKey%(sf.LastPartitioningRedoFrequency<<1) == 1) {
			sf.ForceFrameBoost = 1
		} else {
			sf.ForceFrameBoost = 0
		}
		if isKeyframe {
			sf.MaxDeltaQindex = 20
		} else {
			sf.MaxDeltaQindex = 15
		}
		sf.PartitionSearchType = ReferencePartition
		// libvpx: vp9_speed_features.c:597-600 — is_src_frame_alt_ref VBR
		// override:
		//
		//   if (cpi->oxcf.rc_mode == VPX_VBR && cpi->oxcf.lag_in_frames > 0 &&
		//       cpi->rc.is_src_frame_alt_ref) {
		//     sf->partition_search_type = VAR_BASED_PARTITION;
		//   }
		//
		// ctx.isSrcFrameAltRef threads rc->is_src_frame_alt_ref through the
		// per-frame configurator context (vp9PerFrameSpeedContextArgs /
		// vp9SpeedFrameContext).
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 &&
			ctx.isSrcFrameAltRef {
			sf.PartitionSearchType = VarBasedPartition
		}

		sf.UseNonrdPickMode = 1
		sf.AllowSkipRecode = 0
		sf.InterModeMask[common.Block32x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block32x64] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x32] = sfInterNearestNewZero
		sf.InterModeMask[common.Block64x64] = sfInterNearestNewZero
		sf.AdaptiveRdThresh = 2
		sf.ReuseInterPredSby = 1
		sf.CoeffProbAppxStep = 4
		if isKeyframe {
			sf.UseFastCoefUpdates = TwoLoop
		} else {
			sf.UseFastCoefUpdates = OneLoopReduced
		}
		sf.ModeSearchSkipFlags = FlagSkipIntraDirMismatch
		if isKeyframe {
			sf.TxSizeSearchMethod = UseLargestAll
		} else {
			sf.TxSizeSearchMethod = UseTx8x8
		}
		sf.SimpleModelRdFromVar = 1
		if e.opts.RateControlMode == RateControlVBR {
			sf.Mv.SearchMethod = SearchMethodNStep
		}

		if !isKeyframe {
			// libvpx: vp9_speed_features.c:617-633.
			if content == vp9ContentScreen {
				for i := range common.BlockSizes {
					if i >= common.Block32x32 {
						sf.IntraYModeBsizeMask[i] = sfIntraDCHV
					} else {
						sf.IntraYModeBsizeMask[i] = sfIntraDCTmHV
					}
				}
			} else {
				for i := range common.BlockSizes {
					if i > common.Block16x16 {
						sf.IntraYModeBsizeMask[i] = sfIntraDC
					} else {
						sf.IntraYModeBsizeMask[i] = sfIntraDCHV
					}
				}
			}
		}
		if content == vp9ContentScreen {
			sf.ShortCircuitFlatBlocks = 1
		}
		if e.opts.RateControlMode == RateControlCBR && content != vp9ContentScreen {
			// libvpx: vp9_speed_features.c:637-641.
			sf.LimitNewmvEarlyExit = 1
			if !ctx.svc.UseSvc {
				sf.BiasGolden = 1
			}
		}
		// libvpx: vp9_speed_features.c:642-644 — Keep nonrd_keyframe = 1 for
		// non-base spatial layers to prevent increase in encoding time.
		if ctx.svc.UseSvc && ctx.svc.SpatialLayerID > 0 {
			sf.NonrdKeyframe = 1
		}

		// libvpx: vp9_speed_features.c:645-652 — CBR overshoot detection.
		// libvpx adds use_svc to the inner RE_ENCODE_MAXQ gate so that SVC
		// non-base resolutions skip the recode path. mirror that.
		if ctx.frameType != common.KeyFrame && ctx.resizeStateOrig &&
			e.opts.RateControlMode == RateControlCBR && !ctx.disableOvershootMaxqCbr {
			if ctx.width*ctx.height <= 352*288 && !ctx.svc.UseSvc &&
				content != vp9ContentScreen {
				sf.OvershootDetectionCbrRt = OvershootReEncodeMaxQ
			} else {
				sf.OvershootDetectionCbrRt = OvershootFastDetectionMaxQ
			}
		}
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 &&
			ctx.width <= 1280 && ctx.height <= 720 {
			sf.UseAltrefOnepass = 1
			sf.UseCompoundNonrdPickmode = 1
		}
		if ctx.width*ctx.height > 1280*720 {
			sf.CbPredFilterSearch = 2
		}
		// libvpx: vp9_speed_features.c:659 — if (!cpi->external_resize) sf->use_source_sad = 1;
		if !ctx.externalResize {
			sf.UseSourceSad = 1
		}
	}

	if speed >= 6 {
		// libvpx: vp9_speed_features.c:662-697.
		if e.opts.RateControlMode == RateControlVBR && e.opts.LookaheadFrames > 0 {
			sf.UseAltrefOnepass = 1
			sf.UseCompoundNonrdPickmode = 1
		}
		sf.PartitionSearchType = VarBasedPartition
		sf.Mv.SearchMethod = SearchMethodNStep
		sf.Mv.ReduceFirstStepSize = 1
		sf.SkipEncodeSb = 0

		if sf.UseSourceSad != 0 {
			sf.AdaptPartitionSourceSad = 1
			if ctx.width*ctx.height <= 640*360 {
				sf.AdaptPartitionThresh = 40000
			} else {
				sf.AdaptPartitionThresh = 60000
			}
			// libvpx: vp9_speed_features.c:676-683 — content_state_sb_fd alloc:
			//
			//   if (cpi->content_state_sb_fd == NULL &&
			//       (!cpi->use_svc ||
			//        svc->spatial_layer_id == svc->number_spatial_layers - 1)) {
			//     CHECK_MEM_ERROR(&cm->error, cpi->content_state_sb_fd,
			//         (uint8_t *)vpx_calloc(
			//             (cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1),
			//             sizeof(uint8_t)));
			//   }
			//
			// govpx is single-layer so the !use_svc clause is always
			// satisfied. vp9EnsureContentStateSbFd is the libvpx allocation
			// body, sized from the frame mi grid via encoder.MiDimensionsForFrame.
			e.vp9EnsureContentStateSbFd(ctx.width, ctx.height)
		}
		if e.opts.RateControlMode == RateControlCBR && content != vp9ContentScreen {
			sf.ShortCircuitLowTempVar = 1
		}
		// libvpx: vp9_speed_features.c:689-693.
		if ctx.svc.TemporalLayerID > 0 {
			sf.AdaptiveRdThresh = 4
			sf.LimitNewmvEarlyExit = 0
			sf.BaseMvAggressive = 1
		}

		if ctx.frameType != common.KeyFrame && ctx.resizeStateOrig &&
			e.opts.RateControlMode == RateControlCBR && !ctx.disableOvershootMaxqCbr {
			sf.OvershootDetectionCbrRt = OvershootFastDetectionMaxQ
		}
	}

	if speed >= 7 {
		// libvpx: vp9_speed_features.c:699-749.
		sf.AdaptPartitionSourceSad = 0
		sf.AdaptiveRdThresh = 3
		sf.Mv.SearchMethod = SearchMethodFastDiamond
		sf.Mv.FullpelSearchStepParam = 10
		// libvpx: vp9_speed_features.c:704-711 — For SVC: use better mv search
		// on base temporal layer, and only on base spatial layer if highest
		// resolution is above 640x360.
		if ctx.svc.NumberTemporalLayers > 2 && ctx.svc.TemporalLayerID == 0 &&
			(ctx.svc.SpatialLayerID == 0 ||
				e.opts.Width*e.opts.Height <= 640*360) {
			sf.Mv.SearchMethod = SearchMethodNStep
			sf.Mv.FullpelSearchStepParam = 6
		}
		// libvpx: vp9_speed_features.c:712-716.
		if ctx.svc.TemporalLayerID > 0 || ctx.svc.SpatialLayerID > 1 {
			sf.UseSimpleBlockYrd = 1
			if ctx.svc.NonReferenceFrame {
				sf.Mv.SubpelSearchMethod = SubpelTreePrunedEvenMore
			}
		}
		if ctx.svc.UseSvc && e.opts.RowMT && e.vp9EffectiveThreadHint() > 1 {
			// libvpx: vp9_speed_features.c:717-718.
			sf.AdaptiveRdThreshRowMt = 1
		}
		// libvpx: vp9_speed_features.c:721-734 — partition-copy plumbing.
		e.maxCopiedFrame = 0
		if !ctx.lastFrameDropped && ctx.resizeStateOrig && !ctx.externalResize &&
			(!ctx.svc.UseSvc ||
				(ctx.svc.SpatialLayerID == ctx.svc.NumberSpatialLayers-1 &&
					!ctx.svc.LastLayerDropped[ctx.svc.NumberSpatialLayers-1])) {
			sf.CopyPartitionFlag = 1
			e.maxCopiedFrame = 2
			// The top temporal enhancement layer (for number of temporal
			// layers > 1) are non-reference frames, so use large/max value for
			// max_copied_frame.
			if ctx.svc.NumberTemporalLayers > 1 &&
				ctx.svc.TemporalLayerID == ctx.svc.NumberTemporalLayers-1 {
				e.maxCopiedFrame = 255
			}
		}
		// libvpx: vp9_speed_features.c:735-741 — For SVC: enable use of lower
		// resolution partition for higher resolution, only for 3 spatial
		// layers and when config/top resolution is above VGA. Enable only for
		// non-base temporal layer frames.
		if ctx.svc.UseSvc && ctx.svc.UsePartitionReuse &&
			ctx.svc.NumberSpatialLayers == 3 && ctx.svc.TemporalLayerID > 0 &&
			e.opts.Width*e.opts.Height > 640*480 {
			sf.SvcUseLowresPart = 1
		}
		// libvpx: vp9_speed_features.c:742-747 — For SVC when golden is used
		// as second temporal reference: to avoid encode time increase only use
		// this feature on base temporal layer.
		if ctx.svc.UseSvc && ctx.svc.UseGfTemporalRefCurrentLayer &&
			ctx.svc.TemporalLayerID > 0 {
			e.refFrameFlags &^= encoder.GoldFlag
		}
		if ctx.width*ctx.height > 640*480 {
			sf.CbPredFilterSearch = 2
		}
	}

	if speed >= 8 {
		// libvpx: vp9_speed_features.c:751-793.
		sf.AdaptiveRdThresh = 4
		sf.SkipEncodeSb = 1
		// libvpx: vp9_speed_features.c:754-757.
		if ctx.svc.NumberSpatialLayers > 1 && !ctx.svc.SimulcastMode {
			sf.NonrdKeyframe = 0
		} else {
			sf.NonrdKeyframe = 1
		}
		// libvpx: vp9_speed_features.c:758 — if (!cpi->use_svc) cpi->max_copied_frame = 4;
		if !ctx.svc.UseSvc {
			e.maxCopiedFrame = 4
		}

		if e.opts.RowMT && e.vp9EffectiveThreadHint() > 1 {
			sf.AdaptiveRdThreshRowMt = 1
		}

		if !vp9FrameIsIntraOnly(ctx) && ctx.width*ctx.height <= 352*288 {
			sf.NonrdUseMlPartition = 1
		}

		if content == vp9ContentScreen {
			sf.Mv.SubpelForceStop = HalfPel
		}
		sf.RtIntraDcOnlyLowContent = 1
		// libvpx: vp9_speed_features.c:771-789 — !cpi->use_svc gate so SVC at
		// speed 8 does not engage the aggressive short-circuit / adaptive_rd
		// reduction path.
		if !ctx.svc.UseSvc && e.opts.RateControlMode == RateControlCBR &&
			content != vp9ContentScreen {
			sf.ShortCircuitLowTempVar = 3
			// libvpx: vp9_speed_features.c:777-782 — for HD CBR, drop
			// short_circuit_low_temp_var to level 2 when the noise
			// estimator flags the source as medium-or-higher noise:
			//
			//	if (cpi->noise_estimate.enabled && cm->width >= 1280 &&
			//	    cm->height >= 720) {
			//	  NOISE_LEVEL noise_level =
			//	      vp9_noise_estimate_extract_level(&cpi->noise_estimate);
			//	  if (noise_level >= kMedium) sf->short_circuit_low_temp_var = 2;
			//	}
			if e.noiseEstimate.Enabled && ctx.width >= 1280 && ctx.height >= 720 {
				noiseLevel := e.noiseEstimate.ExtractLevel()
				if noiseLevel >= encoder.NoiseLevelMedium {
					sf.ShortCircuitLowTempVar = 2
				}
			}
			if ctx.width*ctx.height > 352*288 {
				sf.AdaptiveRdThresh = 1
			} else {
				sf.AdaptiveRdThresh = 2
			}
		}
		sf.LimitNewmvEarlyExit = 0
		sf.UseSimpleBlockYrd = 1
		if ctx.width*ctx.height > 352*288 {
			sf.CbPredFilterSearch = 2
		}
	}

	if speed >= 9 {
		// libvpx: vp9_speed_features.c:795-814.
		if !isKeyframe {
			for i := range common.BlockSizes {
				sf.IntraYModeBsizeMask[i] = sfIntraDC
			}
		}
		sf.CbPredFilterSearch = 2
		sf.Mv.EnableAdaptiveSubpelForceStop = 1
		sf.Mv.AdaptSubpelForceStop.MvThresh = 1
		sf.Mv.AdaptSubpelForceStop.ForceStopBelow = QuarterPel
		sf.Mv.AdaptSubpelForceStop.ForceStopAbove = HalfPel
		if ctx.frameType != common.KeyFrame && ctx.width >= 320 && ctx.height >= 240 {
			sf.Disable16x16PartNonKey = 1
		}
		if e.opts.RateControlMode == RateControlCBR {
			sf.DisableGoldenRef = 1
		}
		if ctx.avgFrameLowMotion < 70 {
			sf.DefaultInterpFilter = vp9dec.InterpBilinear
		}
		if ctx.width*ctx.height >= 640*360 {
			sf.VariancePartThreshMult = 2
		}
	}

	// libvpx: vp9_speed_features.c:819-823 — low-res low-Q disable for var
	// partition. Applies to all speeds.
	if ctx.frameType != common.KeyFrame && ctx.width*ctx.height <= 320*240 &&
		sf.PartitionSearchType == VarBasedPartition &&
		ctx.avgFrameQindexInter > 208 && ctx.currentVideoFrame > 8 {
		sf.Disable16x16PartNonKey = 1
	}

	// libvpx: vp9_speed_features.c:825-826.
	if sf.NonrdUseMlPartition != 0 {
		sf.PartitionSearchType = MlBasedPartition
	}

	// libvpx: vp9_speed_features.c:828-844 — altref-onepass FIXED_PARTITION
	// override + ARF usage counter allocation:
	//
	//   if (sf->use_altref_onepass) {
	//     if (cpi->rc.is_src_frame_alt_ref && cm->frame_type != KEY_FRAME) {
	//       sf->partition_search_type = FIXED_PARTITION;
	//       sf->always_this_block_size = BLOCK_64X64;
	//     }
	//     if (cpi->count_arf_frame_usage == NULL) {
	//       CHECK_MEM_ERROR(&cm->error, cpi->count_arf_frame_usage,
	//           (uint8_t *)vpx_calloc((cm->mi_stride >> 3) *
	//                                  ((cm->mi_rows >> 3) + 1),
	//                                  sizeof(*cpi->count_arf_frame_usage)));
	//     }
	//     if (cpi->count_lastgolden_frame_usage == NULL)
	//       CHECK_MEM_ERROR(&cm->error, cpi->count_lastgolden_frame_usage,
	//           (uint8_t *)vpx_calloc((cm->mi_stride >> 3) *
	//                                  ((cm->mi_rows >> 3) + 1),
	//                                  sizeof(*cpi->count_lastgolden_frame_usage)));
	//   }
	//
	// vp9EnsureArfFrameUsage allocates both counters with the libvpx
	// calc_mi_size-derived shape. ctx.isSrcFrameAltRef threads
	// rc->is_src_frame_alt_ref from the per-frame configurator context.
	if sf.UseAltrefOnepass != 0 {
		if ctx.isSrcFrameAltRef && ctx.frameType != common.KeyFrame {
			sf.PartitionSearchType = FixedPartition
			sf.AlwaysThisBlockSize = common.Block64x64
		}
		e.vp9EnsureArfFrameUsage(ctx.width, ctx.height)
	}

	// libvpx: vp9_speed_features.c:845-848.
	if ctx.svc.PreviousFrameIsIntraOnly {
		sf.PartitionSearchType = FixedPartition
		sf.AlwaysThisBlockSize = common.Block64x64
	}
	// libvpx: vp9_speed_features.c:849-857 — Special case for screen content:
	// increase motion search on base spatial layer when high motion is detected
	// or previous SL0 frame was dropped.
	if e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) &&
		e.vp9SpeedFeatureCPUUsed() >= 5 &&
		(ctx.svc.HighNumBlocksWithMotion || ctx.svc.LastLayerDropped[0]) {
		sf.Mv.SearchMethod = SearchMethodNStep
		sf.Mv.FullpelSearchStepParam = 2
	}

	// libvpx: vp9_speed_features.c:858-861 — speed<=3 disables CYCLIC_REFRESH.
	if speed <= 3 && e.opts.AQMode == VP9AQCyclicRefresh {
		// libvpx writes back to cpi->oxcf.aq_mode = 0. govpx mirrors this by
		// clearing the encoder's AQ mode for subsequent frames so cyclic
		// refresh stops engaging at low speeds.
		e.opts.AQMode = VP9AQNone
		e.cyclicAQ.Configure(false, e.opts.Width, e.opts.Height)
	}

	// libvpx: vp9_speed_features.c:863-866 — deadline switch nonrd_keyframe.
	if e.vp9DeadlineModeChanged() {
		sf.NonrdKeyframe = 1
	}

	// libvpx: vp9_speed_features.c:868-870 — forced off for SVC lowres-part.
	sf.SvcUseLowresPart = 0
}

// vp9SpeedIsFirstPass returns true when libvpx's oxcf->pass would be 1.
// govpx does not currently surface an explicit first-pass option; the
// corresponding libvpx fixup (force optimize_coefficients = 0) is therefore a
// no-op here.
//
// TODO: consumer requires opts.TwoPassFirstPass. libvpx:
// vp9_speed_features.c:1052.
func (e *VP9Encoder) vp9SpeedIsFirstPass() bool {
	return false
}

// vp9SpeedIsOnePass returns true when libvpx's oxcf->pass would be 0
// (one-pass encoding). govpx is one-pass unless TwoPassStats marks a second
// pass.
func (e *VP9Encoder) vp9SpeedIsOnePass() bool {
	if e == nil {
		return true
	}
	return len(e.opts.TwoPassStats) == 0
}
