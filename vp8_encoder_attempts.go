package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	vpxrc "github.com/thesyncim/govpx/internal/vpx/ratecontrol"
)

func (e *VP8Encoder) encodeKeyFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, invisible bool, staticSegmentationAllowed bool) (keyFrameEncodeAttempt, error) {
	recode := e.rc.newFrameSizeRecodeState(true, false)
	recode.onePass = !e.twoPass.enabled()
	recode.screenContentMode = e.opts.ScreenContentMode
	// libvpx vp8/encoder/onyx_if.c encode_frame_to_data_rate snapshots the
	// coding context once before entering the recode do-loop. Each rejected
	// attempt restores this snapshot so the next attempt re-encodes from the
	// same pre-attempt entropy/skip-prob state.
	e.saveCodingContext()
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if traceEnabled {
		e.resetOracleTraceRecode()
	}
	cyclicRefreshQ := e.rc.currentQuantizer
	for attempt := 0; ; attempt++ {
		if traceEnabled {
			e.incrementOracleTraceRecodeLoop()
		}
		// libvpx vp8/encoder/encodeframe.c:721-732 rebuilds the per-MB
		// activity_map inside every vp8_encode_frame call. Each keyframe
		// recode attempt therefore observes an activity_map keyed off the
		// new cm->base_qindex. Mirror that here: the pre-loop call in
		// vp8_encoder_frame.go seeded the first attempt; subsequent attempts
		// rebuild against the recoded currentQuantizer.
		if attempt > 0 && e.opts.Tuning == TuneSSIM {
			if err := e.prepareTuningActivityMap(source, rows, cols); err != nil {
				return keyFrameEncodeAttempt{}, err
			}
		}
		result, err := e.encodeKeyFrameAttempt(dst, source, rows, cols, required, flags, invisible, staticSegmentationAllowed, cyclicRefreshQ)
		if err != nil {
			return keyFrameEncodeAttempt{}, err
		}
		// libvpx leaves cpi->mb.act_zbin_adj and cpi->mb.rdmult at the
		// last MB's vp8_activity_masking output across vp8_encode_frame
		// calls; the next attempt's vp8cx_frame_init_quantizer reads the
		// stale act_zbin_adj through ZBIN_EXTRA_Y when seeding
		// b->zbin_extra for the activity probe, and vp8_optimize_mby
		// reads mb->rdmult directly. Mirror that carry per-attempt (not
		// per-frame) so the recode loop's next prepareTuningActivityMap
		// call observes the same bias libvpx applies.
		e.captureActivityProbeAttemptCarry(e.rc.currentQuantizer, e.rc.currentZbinOverQuant, rows, cols)
		// libvpx VP8 MT helpers' mb->uv_mode_count / mb->ymode_count are
		// NEVER zeroed between recode iterations
		// (vp8/encoder/ethreading.c:478-486 vp8cx_init_mbrthread_data only
		// zeroes helpers' coef_counts/skip_true_count/MVcount and MAIN's
		// ymode_count; helpers' ymode_count and uv_mode_count survive).
		// Each vp8_encode_frame call (one per recode iteration of the
		// onyx_if.c:3844 do-loop) adds the helper rows' intra mode events
		// on top of the carryover, and the final pack reads the running
		// sum via encodeframe.c:840-843. govpx mirrors that sticky
		// semantics by absorbing each attempt's helper-row branch counts
		// here; rejected attempts still contribute, because libvpx
		// helpers cannot un-do their per-MB increments.
		e.absorbKeyFrameMTHelperRowIntraCounts()
		// libvpx forced-key-frame special-case branch
		// (encode_frame_to_data_rate around line 4065): when the encoder is
		// emitting a forced KF and the ambient_err baseline from the prior
		// frame is available, drive Q based on the SS-error gap rather than
		// the normal projected-size recode logic.
		if e.thisKeyFrameForced && e.ambientErr > 0 {
			kfErr := vp8enc.CalcKeyFrameSSError(source, &e.analysis.Img, rows, cols)
			nextQ, recoded := e.rc.forcedKeyFrameRecodeQuantizer(kfErr, e.ambientErr, &recode)
			if !recoded {
				return result, nil
			}
			if traceEnabled {
				e.setOracleTraceRecodeReason("kf_forced_quality")
			}
			e.rc.currentQuantizer = nextQ
			e.restoreCodingContext()
			continue
		}
		// libvpx gates the size-recode branch on cpi->sf.recode_loop in
		// recode_loop_test (vp8/encoder/onyx_if.c). Mode 2 (realtime) and
		// good-quality cpu_used >= 4 set recode_loop=0 in set_speed_features,
		// so libvpx accepts the regulator's first Q and lets the
		// rate-correction-factor reconcile across subsequent frames. govpx
		// mirrors that gate via libvpxKeyFrameRecodeLoopActive; the
		// recode_loop_test in libvpx itself feeds the pre-pack
		// `cpi->projected_frame_size` (totalrate>>8 minus entropy savings)
		// from vp8_encode_frame, which is what result.ProjectedSizeBits
		// already mirrors.
		if !e.libvpxKeyFrameRecodeLoopActive() {
			return result, nil
		}
		preQ := e.rc.currentQuantizer
		preZbin := e.rc.currentZbinOverQuant
		preActiveBest := recode.activeBest
		preActiveWorst := recode.activeWorst
		preRCF := recode.correctionFactor
		recoded := e.updateQuantizerForProjectedFrameSize(result.ProjectedSizeBits, true, false, required, &recode)
		if traceEnabled {
			targetBits := e.rc.frameTargetBits
			if targetBits <= 0 {
				targetBits = e.rc.bitsPerFrame
			}
			undershootLimit, overshootLimit := e.rc.frameSizeBoundsBits(true, false, targetBits)
			rawRate := result.ProjectedSizeBits + result.CoefSavingsBits + result.RefFrameSavingsBits
			e.emitOracleRecodeIterTrace(oracleTraceRecodeIterSummary{
				Iter:                 attempt + 1,
				Q:                    preQ,
				ProjectedFrameSize:   result.ProjectedSizeBits,
				ThisFrameTarget:      targetBits,
				QLow:                 recode.qLow,
				QHigh:                recode.qHigh,
				ActiveBest:           preActiveBest,
				ActiveWorst:          preActiveWorst,
				ActiveWorstQChanged:  recode.activeWorstQChanged,
				OvershootSeen:        recode.overshootSeen,
				UndershootSeen:       recode.undershootSeen,
				ZbinOverQuant:        preZbin,
				RateCorrectionFactor: preRCF,
				NextQ:                e.rc.currentQuantizer,
				Recoded:              recoded,
				OvershootLimit:       overshootLimit,
				UndershootLimit:      undershootLimit,
				RawRate:              rawRate,
				CoefSavingsBits:      result.CoefSavingsBits,
				RefFrameSavingsBits:  result.RefFrameSavingsBits,
			})
		}
		if !recoded {
			return result, nil
		}
		if traceEnabled {
			e.setOracleTraceRecodeReason("size_recode")
		}
		// Recode accepted: restore the pre-loop snapshot before re-encoding.
		e.restoreCodingContext()
	}
}

func (e *VP8Encoder) encodeKeyFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, invisible bool, staticSegmentationAllowed bool, cyclicRefreshQ int) (keyFrameEncodeAttempt, error) {
	if vp8PhaseStatsEnabled {
		e.phaseCountAttempt(true)
	}
	if len(e.keyFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return keyFrameEncodeAttempt{}, ErrInvalidConfig
	}
	quantDeltas := libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	segmentation := vp8enc.SegmentationConfig{}
	roiSegmentation := e.roiSegmentationConfig()
	if roiSegmentation.Enabled {
		// libvpx setup_features() runs on every keyframe and reasserts the
		// segmentation update bits while ROI segmentation remains enabled.
		roiSegmentation.UpdateMap = true
		roiSegmentation.UpdateData = true
		segmentation = roiSegmentation
	} else if staticSegmentationAllowed {
		// libvpx applies the screen-content-mode=2 golden-refresh cyclic
		// refresh exception in the inter-frame encode path. Keyframes keep
		// the cyclic-refresh segmentation header when cyclic refresh is on.
		segmentation = e.cyclicRefreshSegmentationConfigForQuantizer(false, cyclicRefreshQ)
		if !segmentation.Enabled && e.runtimePreserveSegmentation {
			// Runtime config changes can leave libvpx's segmentation header
			// enabled after the cyclic-refresh producer is disabled. Keyframes
			// assign every macroblock to segment 0, so preserving the stale
			// feature data keeps the packet header shape without changing
			// reconstruction.
			segmentation = e.runtimePreservedSegmentation
			if !e.rtcExternalDisableCyclicRefresh && !e.cyclicRefreshConfigured {
				segmentation = e.cyclicRefreshSegmentationConfigForQuantizerUnchecked(cyclicRefreshQ)
			}
			if !segmentation.Enabled {
				segmentation = e.cyclicRefreshSegmentationConfigForQuantizerUnchecked(cyclicRefreshQ)
			}
		}
	}
	var err error
	projectedRate := 0
	var phase int64
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	if segmentation.Enabled {
		if roiSegmentation.Enabled {
			if !e.assignKeyFrameROISegments(rows, cols, e.keyFrameModes[:required]) {
				return keyFrameEncodeAttempt{}, ErrInvalidConfig
			}
		} else {
			vp8enc.AssignKeyFrameStaticSegments(rows, cols, e.keyFrameModes[:required])
		}
		projectedRate, err = e.buildReconstructingKeyFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	} else {
		projectedRate, err = e.buildReconstructingKeyFrameCoefficients(source, e.rc.currentQuantizer, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	}
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhaseKeyReconstruct, phase)
	}
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	lfLevel, lfSharpness := e.encoderLoopFilter(vp8common.KeyFrame)
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	lfLevel, err = e.pickLoopFilterLevel(source, vp8common.KeyFrame, lfLevel, lfSharpness, rows, cols, required, segmentation, false, false)
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhaseLoopFilterPick, phase)
	}
	if err != nil {
		return keyFrameEncodeAttempt{}, err
	}
	segmentation = e.segmentationConfigForLoopFilterLevel(segmentation, lfLevel)
	lfHeader := e.encoderLoopFilterHeader(lfLevel, lfSharpness)
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	err = e.applyReconstructionLoopFilter(vp8common.KeyFrame, lfHeader, segmentation, rows, cols, required)
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhaseLoopFilterApply, phase)
	}
	if err != nil {
		return keyFrameEncodeAttempt{}, err
	}
	if segmentation.Enabled {
		vp8enc.UpdateKeyFrameSegmentationTreeProbs(&segmentation, e.keyFrameModes[:required])
	}

	cfg := vp8enc.KeyFrameStateConfig{
		InvisibleFrame:        invisible,
		SimpleLoopFilter:      lfHeader.Type == vp8dec.SimpleLoopFilter,
		TokenPartition:        vp8common.TokenPartition(e.opts.TokenPartitions),
		BaseQIndex:            uint8(e.rc.currentQuantizer),
		QuantDeltas:           quantDeltas,
		LoopFilterLevel:       lfLevel,
		SharpnessLevel:        lfSharpness,
		LFDeltaEnabled:        lfHeader.DeltaEnabled,
		LFDeltaUpdate:         e.computeLFDeltaUpdateBit(vp8common.KeyFrame, lfHeader.DeltaEnabled, lfHeader.RefDeltas, lfHeader.ModeDeltas),
		LFDeltaForceUpdateAll: e.forceLFDeltaUpdates(),
		RefLFDeltas:           lfHeader.RefDeltas,
		ModeLFDeltas:          lfHeader.ModeDeltas,
		RefLFDeltasBase:       [vp8common.MaxRefLFDeltas]int8{},
		ModeLFDeltasBase:      [vp8common.MaxModeLFDeltas]int8{},
		Segmentation:          segmentation,
		RefreshEntropyProbs:   e.keyFrameRefreshEntropyProbs(flags),
		IndependentContexts:   e.opts.ErrorResilientPartitions,
		// libvpx initializes pc->mb_no_coeff_skip = 1 for every frame
		// (alloccommon.c). The packet writer derives prob_skip_false from
		// keyframe mode skip counts before emitting the header.
		MBNoCoeffSkip: true,
		HorizScale:    e.horizScale,
		VertScale:     e.vertScale,
	}
	var prebuiltKeyCoefCounts *vp8enc.InterCoefficientTokenCounts
	if e.keyFrameCoefTokenCountsValid && !cfg.IndependentContexts {
		prebuiltKeyCoefCounts = &e.keyFrameCoefTokenCounts
	}
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	n, frameCoefProbs, err := vp8enc.WriteCoefficientKeyFrameWithProbabilityBaseScratchAndCounts(dst, e.opts.Width, e.opts.Height, cfg, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs, &e.partScratch, prebuiltKeyCoefCounts)
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhasePacketWrite, phase)
	}
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	projectedBits, coefSavings, refFrameSavings := e.projectedFrameSizeBitsFromRateWithSavings(true, required, projectedRate, false, false)
	return keyFrameEncodeAttempt{FrameCoefProbs: frameCoefProbs, Size: n, ProjectedSizeBits: projectedBits, CoefSavingsBits: coefSavings, RefFrameSavingsBits: refFrameSavings, LoopFilterLevel: lfLevel, SharpnessLevel: lfSharpness, LFDeltaEnabled: cfg.LFDeltaEnabled, LFDeltaUpdate: cfg.LFDeltaUpdate, RefLFDeltas: cfg.RefLFDeltas, ModeLFDeltas: cfg.ModeLFDeltas, RefreshEntropyProbs: cfg.RefreshEntropyProbs, SegmentationEnabled: segmentation.Enabled, SegmentationConfig: segmentation}, nil
}

func (e *VP8Encoder) keyFrameRefreshEntropyProbs(flags EncodeFlags) bool {
	if e.opts.ErrorResilientPartitions {
		return true
	}
	return flags&EncodeNoUpdateEntropy == 0 && !e.carriedNoUpdateEntropy && !e.opts.ErrorResilient
}

func (e *VP8Encoder) encodeInterFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, boostedReferenceFrame bool, staticSegmentationAllowed bool, sourceIsAltRef bool) (interFrameEncodeAttempt, error) {
	recode := e.rc.newFrameSizeRecodeState(false, boostedReferenceFrame)
	recode.onePass = !e.twoPass.enabled()
	recode.screenContentMode = e.opts.ScreenContentMode
	// libvpx vp8/encoder/onyx_if.c snapshots the coding context once before
	// the recode do-loop and restores it on every rejected attempt; mirror
	// that here so the inter recode loop has the same pre-attempt invariants
	// as libvpx.
	e.saveCodingContext()
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if traceEnabled {
		e.resetOracleTraceRecode()
	}
	// libvpx gates the inter recode loop on `cpi->sf.recode_loop`:
	// 0 -> no recode, 1 -> recode all, 2 -> recode key/golden/altref only.
	// At realtime (`compressor_speed == 2`) recode_loop is always 0, so a
	// non-boosted inter frame never recodes; at good-quality the threshold
	// rises with cpu-used. Without this gate the inter frame keeps cycling
	// through Q values driven by the picker's pre-pack rate estimate, which
	// is per-design coarse - the resulting Q drifts well below libvpx's at
	// constrained bitrates. See libvpx vp8/encoder/onyx_if.c
	// `recode_loop_test` and `set_speed_features` case 1/2/3.
	allowRecode := e.libvpxInterRecodeLoopActive(boostedReferenceFrame)
	rdRefProbsPreconfigured := false
	cyclicRefresh := newInterFrameCyclicRefreshRecodeState(e.rc.currentQuantizer)
	for attempt := 0; ; attempt++ {
		if traceEnabled {
			e.incrementOracleTraceRecodeLoop()
		}
		// libvpx vp8/encoder/encodeframe.c:721-732 rebuilds the per-MB
		// activity_map inside every vp8_encode_frame call. The activity
		// probe reads cm->base_qindex (via vp8_initialize_rd_consts) and
		// the new_fb_idx border state, so a recoded Q produces a fresh
		// activity_map and therefore fresh per-MB act_zbin_adj / RD
		// multiplier values. Mirror that here: the pre-loop call in
		// vp8_encoder_frame.go seeds the first attempt; subsequent attempts
		// rebuild against the recoded currentQuantizer.
		if attempt > 0 && e.opts.Tuning == TuneSSIM {
			if err := e.prepareTuningActivityMap(source, rows, cols); err != nil {
				return interFrameEncodeAttempt{}, err
			}
		}
		needProjectedSize := allowRecode || traceEnabled
		result, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags, temporalActive, goldenCBRRefresh, staticSegmentationAllowed, sourceIsAltRef, &cyclicRefresh, needProjectedSize, rdRefProbsPreconfigured)
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
		// libvpx leaves cpi->mb.act_zbin_adj and cpi->mb.rdmult at the
		// last MB's vp8_activity_masking output across vp8_encode_frame
		// calls. Mirror that carry per-attempt so the recode loop's next
		// prepareTuningActivityMap call observes the same bias libvpx
		// applies.
		e.captureActivityProbeAttemptCarry(e.rc.currentQuantizer, e.rc.currentZbinOverQuant, rows, cols)
		// libvpx VP8 MT helpers' mb->uv_mode_count / mb->ymode_count are
		// NEVER zeroed between recode iterations (helpers' state survives
		// every vp8_encode_frame call). govpx mirrors the sticky semantics
		// by absorbing each attempt's helper-row branch counts here;
		// rejected attempts still contribute. See vp8_encoder.go
		// mtHelperYModeCountAccum and the keyframe-side absorb in
		// encodeKeyFrameWithQuantizerFeedback.
		e.absorbInterFrameMTHelperRowIntraCounts()
		// libvpx evaluates vp8_drop_encodedframe_overshoot inside the
		// encode/recode do-loop, immediately after each vp8_encode_frame
		// attempt and before the size-recode decision. A non-drop attempt
		// still updates cpi->last_pred_err_mb, so later recode attempts use
		// the immediately prior attempt's prediction error instead of the
		// previous displayed frame's value.
		if e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, result.PickerProjectedSizeBytes, required, false) {
			result.OvershootDropped = true
			return result, nil
		}
		e.lastPredErrorMB = e.currentPredictionErrorMB(required)
		preQ := e.rc.currentQuantizer
		preZbin := e.rc.currentZbinOverQuant
		preActiveBest := recode.activeBest
		preActiveWorst := recode.activeWorst
		preRCF := recode.correctionFactor
		recoded := false
		if allowRecode {
			recoded = e.updateQuantizerForProjectedFrameSize(result.ProjectedSizeBits, false, boostedReferenceFrame, required, &recode)
		}
		if traceEnabled {
			targetBits := e.rc.frameTargetBits
			if targetBits <= 0 {
				targetBits = e.rc.bitsPerFrame
			}
			undershootLimit, overshootLimit := e.rc.frameSizeBoundsBits(false, boostedReferenceFrame, targetBits)
			// Decompose ProjectedSizeBits into raw_rate vs entropy_savings.
			// raw_rate = result.ProjectedSizeBits + CoefSavingsBits + RefFrameSavingsBits
			// (mirroring projectedFrameSizeBitsFromRateWithKnownCoefSavings's
			// bits = max(rawRateBits - coefSavings - refFrameSavings, 0)).
			rawRate := result.ProjectedSizeBits + result.CoefSavingsBits + result.RefFrameSavingsBits
			// Pin the per-iteration rfct (count_mb_ref_frame_usage) and the
			// prob_intra/last/gf evolution. Pre-* are the picker-side values
			// consumed by vp8_calc_ref_frame_costs during this iteration;
			// post-* are the rfct-derived values vp8_convert_rfct_to_prob
			// computes at the end of vp8_encode_frame (encodeframe.c:969)
			// for the next iteration's picker. The pre-* mirror the values
			// stored in result.PreProb* by encodeInterFrameAttempt before
			// defer restore.
			rfctIntra, rfctLast, rfctGolden, rfctAlt := countInterFrameRefUsage(e.interFrameModes[:required])
			postProbIntra := int(e.refProbIntra)
			postProbLast := int(e.refProbLast)
			postProbGolden := int(e.refProbGolden)
			if pi, pl, pg, ok := refFrameProbsFromUsage(rfctIntra, rfctLast, rfctGolden, rfctAlt); ok {
				postProbIntra = int(pi)
				postProbLast = int(pl)
				postProbGolden = int(pg)
			}
			e.emitOracleRecodeIterTrace(oracleTraceRecodeIterSummary{
				Iter:                 attempt + 1,
				Q:                    preQ,
				ProjectedFrameSize:   result.ProjectedSizeBits,
				ThisFrameTarget:      targetBits,
				QLow:                 recode.qLow,
				QHigh:                recode.qHigh,
				ActiveBest:           preActiveBest,
				ActiveWorst:          preActiveWorst,
				ActiveWorstQChanged:  recode.activeWorstQChanged,
				OvershootSeen:        recode.overshootSeen,
				UndershootSeen:       recode.undershootSeen,
				ZbinOverQuant:        preZbin,
				RateCorrectionFactor: preRCF,
				NextQ:                e.rc.currentQuantizer,
				Recoded:              recoded,
				OvershootLimit:       overshootLimit,
				UndershootLimit:      undershootLimit,
				RawRate:              rawRate,
				CoefSavingsBits:      result.CoefSavingsBits,
				RefFrameSavingsBits:  result.RefFrameSavingsBits,
				RfctIntra:            rfctIntra,
				RfctLast:             rfctLast,
				RfctGolden:           rfctGolden,
				RfctAlt:              rfctAlt,
				PreProbIntra:         result.PickerProbIntra,
				PreProbLast:          result.PickerProbLast,
				PreProbGolden:        result.PickerProbGolden,
				PostProbIntra:        postProbIntra,
				PostProbLast:         postProbLast,
				PostProbGolden:       postProbGolden,
			})
		}
		if !recoded {
			return result, nil
		}
		if traceEnabled {
			e.setOracleTraceRecodeReason("size_recode")
		}
		nextRefIntra, nextRefLast, nextRefGolden := e.interRecodeNextRDRefFrameProbs(result.Config.RefreshGolden, result.Config.RefreshAltRef, required, rdRefProbsPreconfigured)
		// Recode accepted: restore the pre-loop snapshot before re-encoding.
		// libvpx's coding-context restore does not include
		// prob_intra_coded / prob_last_coded / prob_gf_coded. update_rd_ref_frame_probs
		// runs once before the libvpx recode loop; after each rejected attempt
		// either the encode-frame convert hook feeds the next pass, or a
		// single-layer GF/ARF refresh keeps the same one-time RD adjustment.
		e.restoreCodingContext()
		e.refProbIntra = nextRefIntra
		e.refProbLast = nextRefLast
		e.refProbGolden = nextRefGolden
		e.refProbUseDefaultOnNextInterRD = false
		rdRefProbsPreconfigured = true
	}
}

func (e *VP8Encoder) interRecodeNextRDRefFrameProbs(refreshGolden bool, refreshAltRef bool, required int, rdRefProbsPreconfigured bool) (uint8, uint8, uint8) {
	nextRefIntra := e.refProbIntra
	nextRefLast := e.refProbLast
	nextRefGolden := e.refProbGolden
	if !rdRefProbsPreconfigured {
		nextRefIntra, nextRefLast, nextRefGolden = e.interAttemptRDRefFrameProbs(refreshAltRef)
	}
	if libvpxShouldConvertRefCountsToProb(e.libvpxTemporalLayerCount(), refreshGolden, refreshAltRef) && len(e.interFrameModes) >= required {
		intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:required])
		if probIntra, probLast, probGolden, ok := refFrameProbsFromUsage(intra, last, golden, alt); ok {
			nextRefIntra, nextRefLast, nextRefGolden = probIntra, probLast, probGolden
		}
	}
	return nextRefIntra, nextRefLast, nextRefGolden
}

// libvpxInterRecodeLoopActive returns true when libvpx's inter recode loop
// would run for this frame, mirroring `cpi->sf.recode_loop` in the encoder
// speed-feature table at vp8/encoder/onyx_if.c set_speed_features and the
// recode_loop_test in the same file. The libvpx mapping is:
//
//   - Mode == 2 (realtime):                       recode_loop = 0 (off)
//   - Mode == 1 (good), Speed in 0..2:            recode_loop = 1 (recode all)
//   - Mode == 1 (good), Speed == 3:               recode_loop = 2 (KF/GF/AR only)
//   - Mode == 1 (good), Speed >= 4:               recode_loop = 0 (off)
//   - Mode == 0 (best):                           recode_loop = 1 (recode all)
//
// recode_loop_test returns true when:
//   - recode_loop == 1, OR
//   - recode_loop == 2 AND (KEY || refresh_golden || refresh_alt_ref)
//
// govpx encodes the KF path separately, so this helper covers the inter
// branch only. boostedReferenceFrame mirrors `(cm->refresh_golden_frame
// || cm->refresh_alt_ref_frame)`.
func (e *VP8Encoder) libvpxInterRecodeLoopActive(boostedReferenceFrame bool) bool {
	switch libvpxSpeedFeatureRecodeLoop(e.opts.Deadline, e.libvpxCPUUsed()) {
	case 0:
		return false
	case 1:
		return true
	case 2:
		return boostedReferenceFrame
	default:
		return false
	}
}

// libvpxKeyFrameRecodeLoopActive mirrors recode_loop_test for KEY_FRAME:
// the size_recode branch fires when recode_loop is 1 (or 2, since KEY_FRAME
// satisfies the second clause). At realtime (recode_loop=0) and good-quality
// cpu_used >= 4 (also recode_loop=0), libvpx skips KF size recoding entirely
// and accepts the regulator's first Q. The forced-KF SS-error special path
// (vp8_special_case_for_forced_key_frame) is independent of recode_loop and
// is gated separately at the call site.
func (e *VP8Encoder) libvpxKeyFrameRecodeLoopActive() bool {
	recodeLoop := libvpxSpeedFeatureRecodeLoop(e.opts.Deadline, e.libvpxCPUUsed())
	return recodeLoop == 1 || recodeLoop == 2
}

// vp8DropEncodedframeOvershoot ports vp8/encoder/ratectrl.c
// vp8_drop_encodedframe_overshoot: a post-encode drop that fires when an
// inter frame at low Q badly overshoots the buffer budget on screen-
// content / drop-frame-allowed configurations. When it fires, the encoded
// frame is discarded, the buffer is reset to the optimal level, the
// rate-correction-factor is bumped (capped at 2x or MAX_BPB_FACTOR), and
// `cpi->force_maxqp` is set so the next frame is forced to worst_quality.
//
// libvpx's call site (vp8/encoder/onyx_if.c:3970-3982) is gated on
// `pass==0 AND end_usage==USAGE_STREAM_FROM_SERVER AND
// rt_drop_recode_on_overshoot==1`. VP8E_SET_RTC_EXTERNAL_RATECTRL clears
// rt_drop_recode_on_overshoot in libvpx; govpx mirrors that with
// EncoderOptions.RTCExternalRateControl.
//
// The inner drop test requires `pred_err_mb > thresh_pred_err_mb` and
// `pred_err_mb > 2 * cpi->last_pred_err_mb`. govpx's
// vp8_encoder_reconstruct.go accumulates the per-MB residual sum into
// `framePredictionError` and the inter recode loop writes back
// `lastPredErrorMB` after every non-drop attempt, mirroring libvpx
// onyx_if.c:3982-3983. This matters when the size-recode loop runs: libvpx
// evaluates the overshoot gate on each rejected Q attempt, so the final
// attempt compares prediction error against the previous attempt, not only
// the previous displayed frame.
//
// Inputs: Q is the frame's chosen quantizer, projectedSizeBytes is the
// libvpx `cpi->projected_frame_size` byte count, which is the pre-pack
// rate estimate `totalrate >> 8` from vp8_encode_frame
// (vp8/encoder/encodeframe.c:946) — NOT the final packed bitstream
// size. libvpx applies the entropy-savings subtraction at
// onyx_if.c:3986, AFTER vp8_drop_encodedframe_overshoot consumes the
// value. The caller in vp8_encoder_frame.go therefore forwards
// `attempt.PickerProjectedSizeBytes`, which mirrors the pre-savings
// picker total. Comparing the packed size here under-feeds the gate
// (the packed size is dominated by coef tokens and shrinks well below
// the picker estimate at low Q), and lets govpx encode frames that
// libvpx drops (e.g. the first inter frame after a fat keyframe on
// screen-content mode 2). macroblocks is the frame MB count, and
// keyFrame skips the gate so libvpx's `frame_type != KEY_FRAME` check
// is honored. Returns true when the caller must discard the frame.
func (e *VP8Encoder) vp8DropEncodedframeOvershoot(Q int, projectedSizeBytes int, macroblocks int, keyFrame bool) bool {
	// Only fires in one-pass CBR with the rt-drop-recode signal active.
	if e.rc.mode != RateControlCBR || e.twoPass.enabled() {
		return false
	}
	if e.opts.RTCExternalRateControl {
		return false
	}
	if keyFrame {
		// libvpx skips the function body entirely on KFs (frame_type !=
		// KEY_FRAME guard). Counters do not advance on KFs either.
		return false
	}
	// Outer gate (vp8/encoder/ratectrl.c lines ~1505-1510):
	//   screen_content_mode == 2  OR
	//   (drop_frames_allowed AND
	//    (force_drop_overshoot OR
	//     (rate_correction_factor < 8*MIN_BPB_FACTOR AND
	//      frames_since_last_drop_overshoot > framerate)))
	// govpx does not surface multi-resolution force_drop_overshoot, so
	// the inner OR collapses to the rcf+timing branch.
	rcf := e.rc.rateCorrectionFactorForFrame(false, false)
	framerate := float64(e.rc.outputFrameRate)
	rcThresholdMet := rcf < 8.0*libvpxMinBPBFactor &&
		framerate > 0 &&
		float64(e.rc.framesSinceLastDropOvershoot) > framerate
	outerGate := e.opts.ScreenContentMode == 2 ||
		(e.rc.dropFrameAllowed && rcThresholdMet)
	if !outerGate {
		// Outside the outer gate libvpx still resets force_maxqp and
		// advances the post-drop counter so the rcf-watchdog branch can
		// arm next time.
		e.clearForceMaxQuantizer()
		e.rc.framesSinceLastDropOvershoot++
		return false
	}
	// Inner drop trigger (vp8/encoder/ratectrl.c lines ~1532-1543).
	const threshPredErrMB = 200 << 4 // libvpx: thresh_pred_err_mb = (200 << 4)
	threshQ := (3 * e.rc.maxQuantizer) >> 2
	avBytesPerFrame := e.rc.bitsPerFrame >> 3
	threshRate := 2 * avBytesPerFrame
	predErrMB := e.currentPredictionErrorMB(macroblocks)
	if e.rc.dropFrameAllowed && predErrMB > (threshPredErrMB<<4) {
		// libvpx widens the trigger when the current frame shows extreme
		// prediction error: thresh_rate >>= 3.
		threshRate >>= 3
	}
	if Q < threshQ &&
		projectedSizeBytes > threshRate &&
		predErrMB > threshPredErrMB &&
		predErrMB > 2*e.lastPredErrorMB {
		// Drop fires.
		e.setForceMaxQuantizer()
		// libvpx resets buffer_level + bits_off_target to optimal so the
		// next-frame target estimator does not try to "earn back" the
		// overspent bits on a single frame.
		if e.rc.bufferOptimalBits > 0 {
			e.rc.bufferLevelBits = e.rc.bufferOptimalBits
		}
		// Bump rate_correction_factor toward the target/worst-quality
		// ratio, clamped at min(2*current, MAX_BPB_FACTOR).
		if macroblocks > 0 && uint(e.rc.maxQuantizer) < uint(len(vp8enc.LibvpxBitsPerMB[1])) {
			targetBitsPerMB := vp8enc.LibvpxTargetBitsPerMB(e.rc.bitsPerFrame, macroblocks)
			worstBitsPerMB := vp8enc.LibvpxBitsPerMB[1][e.rc.maxQuantizer]
			if worstBitsPerMB > 0 {
				newCF := float64(targetBitsPerMB) / float64(worstBitsPerMB)
				if newCF > rcf {
					capped := 2.0 * rcf
					if newCF > capped {
						newCF = capped
					}
					if newCF > libvpxMaxBPBFactor {
						newCF = libvpxMaxBPBFactor
					}
					e.rc.setRateCorrectionFactorForFrame(false, false, newCF)
				}
			}
		}
		e.rc.framesSinceLastDropOvershoot = 0
		return true
	}
	e.clearForceMaxQuantizer()
	e.rc.framesSinceLastDropOvershoot++
	return false
}

func (e *VP8Encoder) setForceMaxQuantizer() {
	e.forceMaxQuantizer = true
}

func (e *VP8Encoder) clearForceMaxQuantizer() {
	e.forceMaxQuantizer = false
}

func (e *VP8Encoder) pendingForceMaxQuantizer() int {
	return e.rc.maxQuantizer
}

func (e *VP8Encoder) currentPredictionErrorMB(macroblocks int) int {
	if macroblocks <= 0 {
		return 0
	}
	return int(e.framePredictionError / int64(macroblocks))
}

func (e *VP8Encoder) encodeInterFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, staticSegmentationAllowed bool, sourceIsAltRef bool, cyclicRefresh *interFrameCyclicRefreshRecodeState, needProjectedSize bool, rdRefProbsPreconfigured bool) (interFrameEncodeAttempt, error) {
	if vp8PhaseStatsEnabled {
		e.phaseCountAttempt(false)
	}
	e.framePredictionError = 0
	cfg := vp8enc.DefaultInterFrameStateConfig(uint8(e.rc.currentQuantizer))
	cfg.InvisibleFrame = flags&EncodeInvisibleFrame != 0
	cfg.TokenPartition = vp8common.TokenPartition(e.opts.TokenPartitions)
	cfg.QuantDeltas = libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	cfg.LoopFilterLevel, cfg.SharpnessLevel = e.encoderLoopFilter(vp8common.InterFrame)
	cfg.SimpleLoopFilter = e.encoderUsesSimpleLoopFilter()
	cfg.RefreshEntropyProbs = flags&EncodeNoUpdateEntropy == 0 && !e.carriedNoUpdateEntropy && !e.opts.ErrorResilient && !e.opts.ErrorResilientPartitions
	cfg.IndependentContexts = e.opts.ErrorResilientPartitions
	// Match libvpx's normal interframe shape: LAST advances by default while
	// golden/altref remain long-lived references unless a future policy
	// (auto-GF, temporal SVC parity report, FORCE_GF/FORCE_ARF flags) updates
	// them. When the caller provides any of the per-frame update flags,
	// libvpx vp8/vp8_cx_iface.c:vp8e_set_frame_flags routes the request
	// through vp8_update_reference which rewrites cm->refresh_*_frame from
	// an explicit "update" mask (start at all-three, XOR off each NO_UPD_*
	// bit); mirror that mask so the inter-frame refresh header bits and
	// the downstream rdopt token-cost / mode-cost branches see the same
	// state as libvpx.
	if refreshLast, refreshGolden, refreshAltRef, ok := e.currentExternalRefreshMask(); ok {
		cfg.RefreshLast, cfg.RefreshGolden, cfg.RefreshAltRef = refreshLast, refreshGolden, refreshAltRef
	} else if temporalActive {
		// The temporal SVC layer manager passes the per-layer parity report
		// in flags. libvpx only rewrites the refresh mask when NO_UPD_*
		// or FORCE_* flags are present; NO_REF_* alone still uses the
		// normal LAST-only inter-frame default.
		if externalRefreshFlagsPending(flags) {
			cfg.RefreshLast, cfg.RefreshGolden, cfg.RefreshAltRef = libvpxExternalRefreshMask(flags)
		} else {
			cfg.RefreshLast = true
			cfg.RefreshGolden = goldenCBRRefresh
			cfg.RefreshAltRef = false
		}
	} else if externalRefreshFlagsPending(flags) {
		cfg.RefreshLast, cfg.RefreshGolden, cfg.RefreshAltRef = libvpxExternalRefreshMask(flags)
	} else {
		cfg.RefreshLast = true
		cfg.RefreshGolden = goldenCBRRefresh
		cfg.RefreshAltRef = false
	}
	signBias := e.interFrameSignBias()
	cfg.GoldenSignBias = signBias[vp8common.GoldenFrame]
	cfg.AltRefSignBias = signBias[vp8common.AltRefFrame]
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(e.opts.ErrorResilient, goldenCBRRefresh, flags) {
		cfg.CopyBufferToAltRef = 2
	}
	// Enforce libvpx onyx_if.c update_reference_frames ARF invariants
	// before validation: assert(!cm->copy_buffer_to_arf) on hidden ARF
	// frames and clear both copy fields on the deferred show frame.
	suppressInterFrameCopyBuffersOnAltRefEdges(&cfg, e.isSrcFrameAltRef(e.currentSourcePTS))
	cfg.ProbSkipFalse = e.interFrameAnalysisSkipFalseProb(e.rc.currentQuantizer, cfg.RefreshGolden, cfg.RefreshAltRef, sourceIsAltRef)
	previousProbSkipFalse := e.probSkipFalse
	e.probSkipFalse = cfg.ProbSkipFalse
	defer func() {
		e.probSkipFalse = previousProbSkipFalse
	}()
	segmentation := vp8enc.SegmentationConfig{}
	cyclicRefreshEnabled := false
	roiSegmentation := e.roiSegmentationConfig()
	if roiSegmentation.Enabled {
		segmentation = roiSegmentation
	} else if staticSegmentationAllowed {
		segmentation, cyclicRefreshEnabled = e.interFrameCyclicRefreshSegmentationForRecode(cyclicRefresh, cfg.RefreshGolden)
	}
	if segmentation.Enabled {
		cfg.Segmentation = segmentation
	}
	if cfg.LoopFilterLevel == 0 && !segmentation.Enabled {
		refFrame, ref, ok := e.matchingZeroInterFrameReference(source, flags)
		if ok {
			if len(e.interFrameModes) < required {
				return interFrameEncodeAttempt{}, ErrInvalidConfig
			}
			fillZeroInterFrameModes(e.interFrameModes[:required], refFrame)
			cfg.ProbSkipFalse = vp8enc.InterFrameModeSkipFalseProbability(rows, cols, e.interFrameModes[:required], cfg.ProbSkipFalse)
			var phase int64
			if vp8PhaseStatsEnabled {
				phase = e.phaseStart()
			}
			n, err := vp8enc.WriteZeroReferenceInterFrame(dst, e.opts.Width, e.opts.Height, cfg, refFrame)
			if vp8PhaseStatsEnabled {
				e.phaseEnd(encoderPhasePacketWrite, phase)
			}
			if err != nil {
				return interFrameEncodeAttempt{}, translateEncoderError(err)
			}
			return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: e.coefProbs, FrameYModeProbs: e.modeProbs.YMode, FrameUVModeProbs: e.modeProbs.UVMode, FrameMVProbs: e.modeProbs.MV, RefFrame: refFrame, Ref: ref, Size: n, ProjectedSizeBits: vpxrc.EncodedSizeBits(n), PickerProjectedSizeBytes: n, ZeroReference: true}, nil
		}
	}
	if len(e.interFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return interFrameEncodeAttempt{}, ErrInvalidConfig
	}
	e.prepareInterFrameSkinMap(source, rows, cols)
	// Mirror libvpx update_rd_ref_frame_probs: bias the previous-frame
	// reference-frame probabilities for *this* frame's RD scoring based on
	// the upcoming refresh policy. The base values are restored on every
	// return so the accepted packet path recomputes them from this frame's
	// mb_ref_frame counts (the equivalent of vp8_convert_rfct_to_prob at
	// packet write time).
	if !rdRefProbsPreconfigured {
		previousRefProbIntra := e.refProbIntra
		previousRefProbLast := e.refProbLast
		previousRefProbGolden := e.refProbGolden
		e.refProbIntra, e.refProbLast, e.refProbGolden = e.interAttemptRDRefFrameProbs(cfg.RefreshAltRef)
		defer func() {
			e.refProbIntra = previousRefProbIntra
			e.refProbLast = previousRefProbLast
			e.refProbGolden = previousRefProbGolden
		}()
	}
	// Snapshot the picker-side prob_intra/last/gf used by this attempt's RD
	// scoring after policy adjustments but before the deferred restore.
	pickerProbIntra := int(e.refProbIntra)
	pickerProbLast := int(e.refProbLast)
	pickerProbGolden := int(e.refProbGolden)
	var err error
	projectedRate := 0
	cyclicRefreshNextIndex := e.cyclicRefreshIndex
	// Mirror libvpx vp8/encoder/rdopt.c vp8_initialize_rd_consts: the RD
	// picker's per-frame fill_token_costs reads from the frame-context table
	// selected by the current refresh policy. Single-layer encodes choose
	// lfc_a/lfc_g/lfc_n directly; temporal multilayer encodes follow the
	// temporal refresh path's effective context selection.
	//
	// We stash the picker-side snapshot on e.rdPickerCoefProbs so the picker
	// helpers (selectInterFrameModeDecision et al.) read from it; the
	// committed encode path keeps using e.coefProbs, mirroring libvpx's
	// tokenize.c which reads cm->fc.coef_probs.
	previousRDPickerCoefProbs := e.rdPickerCoefProbsActive
	e.rdPickerCoefProbsActive = e.rdPickerCoefProbs(cfg.RefreshGolden, cfg.RefreshAltRef)
	defer func() {
		e.rdPickerCoefProbsActive = previousRDPickerCoefProbs
	}()
	var phase int64
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	if segmentation.Enabled {
		if roiSegmentation.Enabled {
			if !e.assignInterFrameROISegments(rows, cols, e.interFrameModes[:required]) {
				return interFrameEncodeAttempt{}, ErrInvalidConfig
			}
		} else {
			cyclicRefreshNextIndex = e.prepareInterFrameCyclicRefreshSegmentsForRecode(cyclicRefresh, source, rows, cols, e.interFrameModes[:required])
		}
		if e.rowWorkers != nil {
			projectedRate, err = e.buildReconstructingInterFrameCoefficientsWithSegmentationMaybeThreaded(source, e.rc.currentQuantizer, segmentation, true, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
		} else {
			projectedRate, err = e.buildReconstructingInterFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
		}
	} else {
		if e.rowWorkers != nil {
			projectedRate, err = e.buildReconstructingInterFrameCoefficientsMaybeThreaded(source, e.rc.currentQuantizer, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
		} else {
			projectedRate, err = e.buildReconstructingInterFrameCoefficients(source, e.rc.currentQuantizer, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
		}
	}
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhaseInterReconstruct, phase)
	}
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	cyclicRefreshMapLive := false
	if cyclicRefreshEnabled && !roiSegmentation.Enabled {
		e.updateInterFrameCyclicRefreshAttemptMapForRecode(cyclicRefresh, rows, cols, e.interFrameModes[:required])
		cyclicRefreshMapLive = cyclicRefresh != nil && cyclicRefresh.mapLive
	}
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	loopFilterSource := source
	if e.opts.NoiseSensitivity > 0 && e.denoiser.allocated {
		e.denoiser.runningAvg[denoiserAvgIntra].ExtendBorders()
		loopFilterSource = vp8enc.CodedSourceImageFromImage(&e.denoiser.runningAvg[denoiserAvgIntra].Img)
	}
	cfg.LoopFilterLevel, err = e.pickLoopFilterLevel(loopFilterSource, vp8common.InterFrame, cfg.LoopFilterLevel, cfg.SharpnessLevel, rows, cols, required, segmentation, cfg.RefreshGolden, cfg.RefreshAltRef)
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhaseLoopFilterPick, phase)
	}
	if err != nil {
		return interFrameEncodeAttempt{}, err
	}
	segmentation = e.segmentationConfigForLoopFilterLevel(segmentation, cfg.LoopFilterLevel)
	lfHeader := e.encoderLoopFilterHeader(cfg.LoopFilterLevel, cfg.SharpnessLevel)
	cfg.SimpleLoopFilter = lfHeader.Type == vp8dec.SimpleLoopFilter
	cfg.LFDeltaEnabled = lfHeader.DeltaEnabled
	cfg.LFDeltaUpdate = e.computeLFDeltaUpdateBit(vp8common.InterFrame, lfHeader.DeltaEnabled, lfHeader.RefDeltas, lfHeader.ModeDeltas)
	cfg.LFDeltaForceUpdateAll = e.forceLFDeltaUpdates()
	cfg.RefLFDeltas = lfHeader.RefDeltas
	cfg.ModeLFDeltas = lfHeader.ModeDeltas
	if !e.currentLFDeltaUpdate {
		cfg.RefLFDeltasBase = e.lastSignaledRefLFDeltas
		cfg.ModeLFDeltasBase = e.lastSignaledModeLFDeltas
	}
	if cfg.RefreshLast || cfg.RefreshGolden || cfg.RefreshAltRef {
		if vp8PhaseStatsEnabled {
			phase = e.phaseStart()
		}
		err = e.applyReconstructionLoopFilter(vp8common.InterFrame, lfHeader, segmentation, rows, cols, required)
		if vp8PhaseStatsEnabled {
			e.phaseEnd(encoderPhaseLoopFilterApply, phase)
		}
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
	} else {
		e.loopFilterPickReady = false
		e.loopFilterPickBest = false
	}
	if segmentation.Enabled {
		vp8enc.UpdateInterFrameSegmentationTreeProbs(&segmentation, e.interFrameModes[:required])
		cfg.Segmentation = segmentation
	} else if e.runtimePreserveSegmentation && !e.forceMaxQuantizer {
		if e.runtimePreserveSegmentationUpdate && e.runtimePreservedSegmentation.Enabled {
			preserved := e.runtimePreservedSegmentation
			if !e.rtcExternalDisableCyclicRefresh && !e.cyclicRefreshConfigured {
				if cyclicRefresh != nil {
					preserved = e.cyclicRefreshSegmentationConfigForQuantizerUnchecked(cyclicRefresh.q)
				} else {
					preserved = e.cyclicRefreshSegmentationConfigForQuantizerUnchecked(e.rc.currentQuantizer)
				}
			}
			preserved.UpdateMap = true
			preserved.UpdateData = true
			vp8enc.UpdateInterFrameSegmentationTreeProbs(&preserved, e.interFrameModes[:required])
			cfg.Segmentation = preserved
		} else {
			cfg.Segmentation = vp8enc.SegmentationConfig{Enabled: true}
		}
	}
	cfg.ProbSkipFalse = vp8enc.InterFrameModeSkipFalseProbability(rows, cols, e.interFrameModes[:required], cfg.ProbSkipFalse)
	packet := vp8enc.InterFramePacket{
		Dst:        dst,
		Width:      e.opts.Width,
		Height:     e.opts.Height,
		State:      cfg,
		Modes:      e.interFrameModes[:required],
		Coeffs:     e.keyFrameCoeffs[:required],
		Above:      e.tokenAbove[:cols],
		CoefBase:   &e.coefProbs,
		YModeBase:  &e.modeProbs.YMode,
		UVModeBase: &e.modeProbs.UVMode,
		BModeBase:  &e.modeProbs.BMode,
		MVBase:     &e.modeProbs.MV,
		Scratch:    &e.partScratch,
	}
	// Threaded reconstruction at workerCount >= 2 inherits libvpx VP8 MT's
	// ymode_count / uv_mode_count helper-history bias. See vp8_encoder.go
	// `mtHelperYModeCountAccum` for the libvpx reference; the accumulator
	// covers helper-thread rows from every prior MT inter frame and is
	// rolled forward post-commit by absorbContext below in
	// commitInterFrameMTHelperAccumulator.
	if e.lastInterReconstructWorkerCount >= 2 && e.mtHelperRowAccumValid && e.mtHelperRowAccumWorkerCount == e.lastInterReconstructWorkerCount {
		packet.YModeCountBias = &e.mtHelperYModeCountAccum
		packet.UVModeCountBias = &e.mtHelperUVModeCountAccum
	}
	// Lane D: hand the pre-built coefficient token counts and record stream
	// to the packet writer so it can skip its own count/context/QCoeff walks.
	// The caches are only valid after a successful single-threaded
	// reconstruction pass; threaded reconstructions do not populate them
	// (see vp8_encoder_row_threaded.go) and the writer falls back in that case.
	if e.interCoefTokenCountsValid {
		packet.PrebuiltCoefCounts = &e.interCoefTokenCounts
	}
	if e.interCoefTokenRecordsValid {
		packet.PrebuiltCoefTokens = &e.interCoefTokenRecords
	}
	if vp8PhaseStatsEnabled {
		phase = e.phaseStart()
	}
	packetResult, err := packet.Write()
	if vp8PhaseStatsEnabled {
		e.phaseEnd(encoderPhasePacketWrite, phase)
	}
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	n := packetResult.Size
	projectedBits := vpxrc.EncodedSizeBits(n)
	coefSavings := 0
	refFrameSavings := 0
	if needProjectedSize {
		coefSavings = packetResult.CoefSavingsBits
		projectedBits, refFrameSavings = e.projectedFrameSizeBitsFromRateWithKnownCoefSavings(false, required, projectedRate, coefSavings, cfg.RefreshGolden, cfg.RefreshAltRef)
	}
	cfg.MVUpdate = packetResult.FrameMVUpdate
	cfg.MVUpdateCount = packetResult.FrameMVUpdateCount
	// libvpx vp8/encoder/encodeframe.c:946 sets
	// cpi->projected_frame_size = totalrate >> 8 *before*
	// vp8_drop_encodedframe_overshoot consumes it
	// (vp8/encoder/onyx_if.c:3977). The entropy-savings subtraction at
	// onyx_if.c:3986 happens AFTER the overshoot drop, so the overshoot
	// gate's "projected" input must be the raw picker rate in bytes.
	pickerProjectedBytes := projectedRate >> 8
	return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: packetResult.FrameCoefProbs, FrameYModeProbs: packetResult.FrameYModeProbs, FrameUVModeProbs: packetResult.FrameUVModeProbs, FrameMVProbs: packetResult.FrameMVProbs, Size: n, ProjectedSizeBits: projectedBits, PickerProjectedSizeBytes: pickerProjectedBytes, CoefSavingsBits: coefSavings, RefFrameSavingsBits: refFrameSavings, CyclicRefresh: cyclicRefreshEnabled, CyclicRefreshNextIndex: cyclicRefreshNextIndex, CyclicRefreshMapLive: cyclicRefreshMapLive, PickerProbIntra: pickerProbIntra, PickerProbLast: pickerProbLast, PickerProbGolden: pickerProbGolden}, nil
}

func (e *VP8Encoder) updateQuantizerForProjectedFrameSize(projectedBits int, keyFrame bool, goldenFrame bool, macroblocks int, recode *frameSizeRecodeState) bool {
	next, ok := e.rc.frameSizeRecodeQuantizerWithContextBits(projectedBits, keyFrame, goldenFrame, macroblocks, recode)
	if !ok {
		return false
	}
	if next == e.rc.currentQuantizer {
		e.rc.currentZbinOverQuant = recode.zbinOverQuant
		return false
	}
	e.rc.currentQuantizer = next
	e.rc.currentZbinOverQuant = recode.zbinOverQuant
	return true
}

// projectedFrameSizeBitsFromRateWithSavings projects the post-savings
// frame-size bits and returns the per-component entropy-savings breakdown
// alongside it. Used by the oracle trace to localize entropy-savings
// parity gaps. The breakdown is the PRE-clamp value: when the
// post-savings projection would underflow, the bits return clamps to 0
// but the savings scalars still reflect what was subtracted.
// refreshGolden / refreshAltRef mirror libvpx
// cm->refresh_golden_frame / cm->refresh_alt_ref_frame for the in-flight
// inter-frame attempt; the values gate the libvpx vp8_convert_rfct_to_prob
// hook documented in refFrameEntropySavingsBitsForFrame. Key frames pass
// false/false (libvpx skips the hook for KEY frames anyway).
func (e *VP8Encoder) projectedFrameSizeBitsFromRateWithSavings(keyFrame bool, macroblocks int, projectedRate int, refreshGolden bool, refreshAltRef bool) (bits int, coefSavings int, refFrameSavings int) {
	if projectedRate <= 0 {
		return 0, 0, 0
	}
	coefSavings = e.coefficientEntropySavingsBits(keyFrame, macroblocks)
	projectedBits, refFrameSavings := e.projectedFrameSizeBitsFromRateWithKnownCoefSavings(keyFrame, macroblocks, projectedRate, coefSavings, refreshGolden, refreshAltRef)
	return projectedBits, coefSavings, refFrameSavings
}

func (e *VP8Encoder) projectedFrameSizeBitsFromRateWithKnownCoefSavings(keyFrame bool, macroblocks int, projectedRate int, coefSavings int, refreshGolden bool, refreshAltRef bool) (bits int, refFrameSavings int) {
	if projectedRate <= 0 {
		return 0, 0
	}
	projectedBits := projectedRate >> 8
	refFrameSavings = e.refFrameEntropySavingsBitsForFrame(keyFrame, macroblocks, refreshGolden, refreshAltRef)
	bits = max(projectedBits-coefSavings-refFrameSavings, 0)
	return bits, refFrameSavings
}

// refFrameEntropySavingsBitsForFrame mirrors libvpx's inter-frame ref-frame
// branch of vp8_estimate_entropy_savings (vp8/encoder/bitstream.c) for the
// CURRENT frame's accepted attempt. Crucial parity nuance:
// vp8/encoder/encodeframe.c:vp8_encode_frame (around line 980) calls
// vp8_convert_rfct_to_prob(cpi) at the tail of the encode pass for any
// inter frame that is NOT a single-layer GF/ARF refresh, which OVERWRITES
// cpi->prob_intra_coded / prob_last_coded / prob_gf_coded with the
// probabilities derived from THIS frame's count_mb_ref_frame_usage --
// before vp8_estimate_entropy_savings runs at onyx_if.c line 3996. Since
// the same rfct then drives both the "old" cost (post-overwrite) and the
// "new" cost inside vp8_estimate_entropy_savings, the inter-frame branch
// returns zero savings on every frame where the convert hook fired.
//
// govpx's previous behaviour subtracted the heuristic-biased
// e.refProb{Intra,Last,Golden} values, which produced spurious savings of
// up to ~64 bits per inter frame and was the residual gap behind
// projected_frame_size in TestVP8OracleTraceDecisionCompare. Mirroring
// the libvpx convert hook here zeros that out for the same gate libvpx
// uses (libvpxShouldConvertRefCountsToProb) and keeps the heuristic-biased
// fallback for the GF/ARF refresh branch (single-layer, refresh) where
// libvpx skips the convert hook.
func (e *VP8Encoder) refFrameEntropySavingsBitsForFrame(keyFrame bool, macroblocks int, refreshGolden bool, refreshAltRef bool) int {
	if keyFrame || macroblocks <= 0 || len(e.interFrameModes) < macroblocks {
		return 0
	}
	if libvpxShouldConvertRefCountsToProb(e.libvpxTemporalLayerCount(), refreshGolden, refreshAltRef) {
		return 0
	}
	intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:macroblocks])
	return vp8enc.ReferenceFrameEntropySavings(false, intra, last, golden, alt, int(e.refProbIntra), int(e.refProbLast), int(e.refProbGolden))
}

func (e *VP8Encoder) coefficientEntropySavingsBits(keyFrame bool, macroblocks int) int {
	if macroblocks <= 0 {
		return 0
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	if rows <= 0 || cols <= 0 || rows*cols != macroblocks || len(e.tokenAbove) < cols {
		return 0
	}
	if keyFrame {
		if len(e.keyFrameModes) < macroblocks || len(e.keyFrameCoeffs) < macroblocks {
			return 0
		}
		if e.opts.ErrorResilientPartitions {
			if e.keyFrameCoefTokenCountsValid {
				savings, err := vp8enc.CoefficientEntropySavingsIndependentFromPrebuiltCounts(&vp8tables.DefaultCoefProbs, &e.keyFrameCoefTokenCounts, true)
				if err != nil {
					return 0
				}
				return savings
			}
			savings, err := vp8enc.KeyFrameCoefficientEntropySavingsIndependent(rows, cols, e.keyFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs)
			if err != nil {
				return 0
			}
			return savings
		}
		if e.keyFrameCoefTokenCountsValid {
			savings, err := vp8enc.CoefficientEntropySavingsFromPrebuiltCounts(&vp8tables.DefaultCoefProbs, &e.keyFrameCoefTokenCounts)
			if err != nil {
				return 0
			}
			return savings
		}
		savings, err := vp8enc.KeyFrameCoefficientEntropySavings(rows, cols, e.keyFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs)
		if err != nil {
			return 0
		}
		return savings
	}
	if len(e.interFrameModes) < macroblocks || len(e.keyFrameCoeffs) < macroblocks {
		return 0
	}
	if e.opts.ErrorResilientPartitions {
		if e.interCoefTokenCountsValid {
			savings, err := vp8enc.CoefficientEntropySavingsIndependentFromPrebuiltCounts(&e.coefProbs, &e.interCoefTokenCounts, false)
			if err != nil {
				return 0
			}
			return savings
		}
		savings, err := vp8enc.InterCoefficientEntropySavingsIndependent(rows, cols, e.interFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &e.coefProbs)
		if err != nil {
			return 0
		}
		return savings
	}
	if e.interCoefTokenCountsValid {
		savings, err := vp8enc.CoefficientEntropySavingsFromPrebuiltCounts(&e.coefProbs, &e.interCoefTokenCounts)
		if err != nil {
			return 0
		}
		return savings
	}
	savings, err := vp8enc.InterCoefficientEntropySavings(rows, cols, e.interFrameModes[:macroblocks], e.keyFrameCoeffs[:macroblocks], e.tokenAbove[:cols], &e.coefProbs)
	if err != nil {
		return 0
	}
	return savings
}
