package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func (e *VP8Encoder) encodeKeyFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, invisible bool, staticSegmentationAllowed bool) (keyFrameEncodeAttempt, error) {
	recode := e.rc.newFrameSizeRecodeState(true, false)
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
		result, err := e.encodeKeyFrameAttempt(dst, source, rows, cols, required, invisible, staticSegmentationAllowed, cyclicRefreshQ)
		if err != nil {
			return keyFrameEncodeAttempt{}, err
		}
		if attempt+1 >= encoderQuantizerFeedbackMaxAttempts {
			return result, nil
		}
		// libvpx forced-key-frame special-case branch
		// (encode_frame_to_data_rate around line 4065): when the encoder is
		// emitting a forced KF and the ambient_err baseline from the prior
		// frame is available, drive Q based on the SS-error gap rather than
		// the normal projected-size recode logic.
		if e.thisKeyFrameForced && e.ambientErr > 0 {
			kfErr := calcKeyFrameSSError(source, &e.analysis.Img, rows, cols)
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
		if !e.updateQuantizerForProjectedFrameSize(result.ProjectedSizeBits, true, false, required, &recode) {
			return result, nil
		}
		if traceEnabled {
			e.setOracleTraceRecodeReason("size_recode")
		}
		// Recode accepted: restore the pre-loop snapshot before re-encoding.
		e.restoreCodingContext()
	}
}

func (e *VP8Encoder) encodeKeyFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, invisible bool, staticSegmentationAllowed bool, cyclicRefreshQ int) (keyFrameEncodeAttempt, error) {
	e.phaseCountAttempt(true)
	if len(e.keyFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return keyFrameEncodeAttempt{}, ErrInvalidConfig
	}
	quantDeltas := libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	segmentation := vp8enc.SegmentationConfig{}
	roiSegmentation := e.roiSegmentationConfig()
	if roiSegmentation.Enabled {
		segmentation = roiSegmentation
	} else if staticSegmentationAllowed {
		// libvpx applies the screen-content-mode=2 golden-refresh cyclic
		// refresh exception in the inter-frame encode path. Keyframes keep
		// the cyclic-refresh segmentation header when cyclic refresh is on.
		segmentation = e.cyclicRefreshSegmentationConfigForQuantizer(false, cyclicRefreshQ)
	}
	var err error
	projectedRate := 0
	phase := e.phaseStart()
	if segmentation.Enabled {
		if roiSegmentation.Enabled {
			if !e.assignKeyFrameROISegments(rows, cols, e.keyFrameModes[:required]) {
				return keyFrameEncodeAttempt{}, ErrInvalidConfig
			}
		} else {
			assignKeyFrameStaticSegments(rows, cols, e.keyFrameModes[:required])
		}
		projectedRate, err = e.buildReconstructingKeyFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	} else {
		projectedRate, err = e.buildReconstructingKeyFrameCoefficients(source, e.rc.currentQuantizer, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	}
	e.phaseEnd(encoderPhaseKeyReconstruct, phase)
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	lfLevel, lfSharpness := e.encoderLoopFilter(vp8common.KeyFrame)
	phase = e.phaseStart()
	lfLevel, err = e.pickLoopFilterLevel(source, vp8common.KeyFrame, lfLevel, lfSharpness, rows, cols, required, segmentation, false, false)
	e.phaseEnd(encoderPhaseLoopFilterPick, phase)
	if err != nil {
		return keyFrameEncodeAttempt{}, err
	}
	lfHeader := e.encoderLoopFilterHeader(lfLevel, lfSharpness)
	phase = e.phaseStart()
	err = e.applyReconstructionLoopFilter(vp8common.KeyFrame, lfHeader, segmentation, rows, cols, required)
	e.phaseEnd(encoderPhaseLoopFilterApply, phase)
	if err != nil {
		return keyFrameEncodeAttempt{}, err
	}
	if segmentation.Enabled {
		updateKeyFrameSegmentationTreeProbs(&segmentation, e.keyFrameModes[:required])
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
		Segmentation:          segmentation,
		RefreshEntropyProbs:   !e.opts.ErrorResilient || e.opts.ErrorResilientPartitions,
		IndependentContexts:   e.opts.ErrorResilientPartitions,
		// libvpx initializes pc->mb_no_coeff_skip = 1 for every frame
		// (alloccommon.c), so the keyframe header always carries the
		// mb_no_coeff_skip bit and the 8-bit prob_skip_false literal.
		// govpx currently emits skip_coeff=0 for every keyframe MB so
		// no token writes are elided; the header bits alone close the
		// 1-byte stream-byte parity gap surfaced by
		// TestOracleEncoderStreamByteParity.
		MBNoCoeffSkip: true,
		ProbSkipFalse: 255,
	}
	phase = e.phaseStart()
	var prebuiltKeyCoefCounts *vp8enc.InterCoefficientTokenCounts
	if e.keyFrameCoefTokenCountsValid && !cfg.IndependentContexts {
		prebuiltKeyCoefCounts = &e.keyFrameCoefTokenCounts
	}
	n, frameCoefProbs, err := vp8enc.WriteCoefficientKeyFrameWithProbabilityBaseScratchAndCounts(dst, e.opts.Width, e.opts.Height, cfg, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs, &e.partScratch, prebuiltKeyCoefCounts)
	e.phaseEnd(encoderPhasePacketWrite, phase)
	if err != nil {
		return keyFrameEncodeAttempt{}, translateEncoderError(err)
	}
	projectedBits, coefSavings, refFrameSavings := e.projectedFrameSizeBitsFromRateWithSavings(true, required, projectedRate, false, false)
	return keyFrameEncodeAttempt{FrameCoefProbs: frameCoefProbs, Size: n, ProjectedSizeBits: projectedBits, CoefSavingsBits: coefSavings, RefFrameSavingsBits: refFrameSavings, LoopFilterLevel: lfLevel, SharpnessLevel: lfSharpness, LFDeltaEnabled: cfg.LFDeltaEnabled, LFDeltaUpdate: cfg.LFDeltaUpdate, RefLFDeltas: cfg.RefLFDeltas, ModeLFDeltas: cfg.ModeLFDeltas, RefreshEntropyProbs: cfg.RefreshEntropyProbs, SegmentationEnabled: segmentation.Enabled}, nil
}

func (e *VP8Encoder) encodeInterFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, boostedReferenceFrame bool, staticSegmentationAllowed bool, sourceIsAltRef bool) (interFrameEncodeAttempt, error) {
	recode := e.rc.newFrameSizeRecodeState(false, boostedReferenceFrame)
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
	cyclicRefreshQ := e.rc.currentQuantizer
	for attempt := 0; ; attempt++ {
		if traceEnabled {
			e.incrementOracleTraceRecodeLoop()
		}
		needProjectedSize := allowRecode || traceEnabled
		result, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags, temporalActive, goldenCBRRefresh, staticSegmentationAllowed, sourceIsAltRef, cyclicRefreshQ, needProjectedSize, rdRefProbsPreconfigured)
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
		if !allowRecode || attempt+1 >= encoderQuantizerFeedbackMaxAttempts || !e.updateQuantizerForProjectedFrameSize(result.ProjectedSizeBits, false, boostedReferenceFrame, required, &recode) {
			return result, nil
		}
		if traceEnabled {
			e.setOracleTraceRecodeReason("size_recode")
		}
		nextRefIntra, nextRefLast, nextRefGolden := e.refProbIntra, e.refProbLast, e.refProbGolden
		if libvpxShouldConvertRefCountsToProb(e.libvpxTemporalLayerCount(), result.Config.RefreshGolden, result.Config.RefreshAltRef) {
			intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:required])
			if probIntra, probLast, probGolden, ok := refFrameProbsFromUsage(intra, last, golden, alt); ok {
				nextRefIntra, nextRefLast, nextRefGolden = probIntra, probLast, probGolden
			}
		}
		// Recode accepted: restore the pre-loop snapshot before re-encoding.
		// libvpx's coding-context restore does not include
		// prob_intra_coded / prob_last_coded / prob_gf_coded; when
		// vp8_encode_frame converted the rejected attempt's ref counts,
		// those converted probabilities intentionally feed the next recode
		// iteration's ref_frame_cost table.
		e.restoreCodingContext()
		e.refProbIntra = nextRefIntra
		e.refProbLast = nextRefLast
		e.refProbGolden = nextRefGolden
		e.refProbUseDefaultOnNextInterRD = false
		rdRefProbsPreconfigured = true
	}
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
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		speed := e.libvpxCPUUsed()
		switch {
		case speed <= 2:
			return true
		case speed == 3:
			return boostedReferenceFrame
		default:
			return false
		}
	default:
		return true
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
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() <= 3
	default:
		return true
	}
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
// `pred_err_mb > 2 * cpi->last_pred_err_mb`. govpx does not yet
// accumulate `cpi->mb.prediction_error` during inter mode picking, so
// `lastPredErrorMB` is permanently 0 and the inner gate currently never
// fires. The outer state management
// (frames_since_last_drop_overshoot increment, force_maxqp clears) runs
// regardless and matches libvpx for the common no-drop case; that keeps
// the gate ready for a future pred-err-tracking patch.
//
// Inputs: Q is the frame's chosen quantizer, projectedSizeBytes is the
// final packed bitstream length (libvpx's
// `cpi->projected_frame_size = (*size) << 3` post-pack value, here in
// bytes for convenience), macroblocks is the frame MB count, and
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
	framerate := outputFrameRate(e.timing)
	rcThresholdMet := rcf < 8.0*libvpxMinBPBFactor &&
		framerate > 0 &&
		float64(e.rc.framesSinceLastDropOvershoot) > framerate
	outerGate := e.opts.ScreenContentMode == 2 ||
		(e.rc.dropFrameAllowed && rcThresholdMet)
	if !outerGate {
		// Outside the outer gate libvpx still resets force_maxqp and
		// advances the post-drop counter so the rcf-watchdog branch can
		// arm next time.
		e.forceMaxQuantizer = false
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
		e.forceMaxQuantizer = true
		// libvpx resets buffer_level + bits_off_target to optimal so the
		// next-frame target estimator does not try to "earn back" the
		// overspent bits on a single frame.
		if e.rc.bufferOptimalBits > 0 {
			e.rc.bufferLevelBits = e.rc.bufferOptimalBits
		}
		// Bump rate_correction_factor toward the target/worst-quality
		// ratio, clamped at min(2*current, MAX_BPB_FACTOR).
		if macroblocks > 0 && uint(e.rc.maxQuantizer) < uint(len(libvpxBitsPerMB[1])) {
			targetBitsPerMB := libvpxTargetBitsPerMB(e.rc.bitsPerFrame, macroblocks)
			worstBitsPerMB := libvpxBitsPerMB[1][e.rc.maxQuantizer]
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
	e.forceMaxQuantizer = false
	e.rc.framesSinceLastDropOvershoot++
	return false
}

func (e *VP8Encoder) currentPredictionErrorMB(macroblocks int) int {
	if macroblocks <= 0 {
		return 0
	}
	return int(e.framePredictionError / int64(macroblocks))
}

func (e *VP8Encoder) encodeInterFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool, staticSegmentationAllowed bool, sourceIsAltRef bool, cyclicRefreshQ int, needProjectedSize bool, rdRefProbsPreconfigured bool) (interFrameEncodeAttempt, error) {
	e.phaseCountAttempt(false)
	e.framePredictionError = 0
	cfg := vp8enc.DefaultInterFrameStateConfig(uint8(e.rc.currentQuantizer))
	cfg.InvisibleFrame = flags&EncodeInvisibleFrame != 0
	cfg.TokenPartition = vp8common.TokenPartition(e.opts.TokenPartitions)
	cfg.QuantDeltas = libvpxFrameQuantDeltas(e.rc.currentQuantizer, e.opts.ScreenContentMode)
	cfg.LoopFilterLevel, cfg.SharpnessLevel = e.encoderLoopFilter(vp8common.InterFrame)
	cfg.SimpleLoopFilter = e.encoderUsesSimpleLoopFilter()
	cfg.RefreshEntropyProbs = flags&EncodeNoUpdateEntropy == 0 && !e.opts.ErrorResilient && !e.opts.ErrorResilientPartitions
	cfg.IndependentContexts = e.opts.ErrorResilientPartitions
	cfg.RefreshLast = flags&EncodeNoUpdateLast == 0
	// Match libvpx's normal interframe shape: LAST advances by default while
	// golden/altref remain long-lived references unless a future policy updates them.
	cfg.RefreshGolden = false
	cfg.RefreshAltRef = false
	if temporalActive {
		cfg.RefreshGolden = flags&EncodeNoUpdateGolden == 0
		cfg.RefreshAltRef = flags&EncodeNoUpdateAltRef == 0
	} else if goldenCBRRefresh || flags&EncodeForceGoldenFrame != 0 {
		cfg.RefreshGolden = true
	}
	if flags&EncodeForceAltRefFrame != 0 {
		cfg.RefreshAltRef = true
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
		segmentation = e.cyclicRefreshSegmentationConfigForQuantizer(cfg.RefreshGolden, cyclicRefreshQ)
		cyclicRefreshEnabled = segmentation.Enabled
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
			cfg.ProbSkipFalse = interFrameModeSkipFalseProbability(rows, cols, e.interFrameModes[:required], cfg.ProbSkipFalse)
			phase := e.phaseStart()
			n, err := vp8enc.WriteZeroReferenceInterFrame(dst, e.opts.Width, e.opts.Height, cfg, refFrame)
			e.phaseEnd(encoderPhasePacketWrite, phase)
			if err != nil {
				return interFrameEncodeAttempt{}, translateEncoderError(err)
			}
			return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: e.coefProbs, FrameYModeProbs: e.modeProbs.YMode, FrameUVModeProbs: e.modeProbs.UVMode, FrameMVProbs: e.modeProbs.MV, RefFrame: refFrame, Ref: ref, Size: n, ProjectedSizeBits: encodedSizeBits(n), ZeroReference: true}, nil
		}
	}
	if len(e.interFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return interFrameEncodeAttempt{}, ErrInvalidConfig
	}
	// Mirror libvpx update_rd_ref_frame_probs: bias the previous-frame
	// reference-frame probabilities for *this* frame's RD scoring based on
	// the upcoming refresh policy. The base values are restored on every
	// return so commitInterFrameAttempt's updateRefFrameProbsFromAttempt
	// recomputes them from this frame's mb_ref_frame counts (the equivalent
	// of vp8_convert_rfct_to_prob at packet write time).
	if !rdRefProbsPreconfigured {
		previousRefProbIntra := e.refProbIntra
		previousRefProbLast := e.refProbLast
		previousRefProbGolden := e.refProbGolden
		if e.refProbUseDefaultOnNextInterRD {
			e.resetRefFrameProbsToDefaultInterRD()
		}
		if !e.opts.TemporalScalability.Enabled {
			e.applyLibvpxRdRefFrameProbRefreshAdjustments(cfg.RefreshAltRef)
		}
		defer func() {
			e.refProbIntra = previousRefProbIntra
			e.refProbLast = previousRefProbLast
			e.refProbGolden = previousRefProbGolden
		}()
	}
	var err error
	projectedRate := 0
	cyclicRefreshNextIndex := e.cyclicRefreshIndex
	// Mirror libvpx vp8/encoder/rdopt.c vp8_initialize_rd_consts: the RD
	// picker's per-frame fill_token_costs reads from cpi->lfc_a, cpi->lfc_g,
	// or cpi->lfc_n depending on which reference the current frame refreshes —
	// NOT from cm->fc.coef_probs (which is what govpx's e.coefProbs mirrors).
	// Frames that refresh golden/altref score against a colder snapshot
	// (e.g. lfc_g, last touched at the previous keyframe) which raises every
	// candidate's rate, lifts bestScore over rd_threshes[SPLITMV], and lets
	// SPLITMV evaluate. Without this swap, govpx's RD scores run ~0.5x of
	// libvpx's on golden-refresh frames and SPLITMV's gate spuriously fires.
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
	phase := e.phaseStart()
	if segmentation.Enabled {
		if roiSegmentation.Enabled {
			if !e.assignInterFrameROISegments(rows, cols, e.interFrameModes[:required]) {
				return interFrameEncodeAttempt{}, ErrInvalidConfig
			}
		} else {
			cyclicRefreshNextIndex = e.assignInterFrameStaticSegmentsForQuantizer(source, rows, cols, e.interFrameModes[:required], cyclicRefreshQ)
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
	e.phaseEnd(encoderPhaseInterReconstruct, phase)
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	// libvpx denoiser runs per-MB after mode decision and reconstruction.
	// Output goes to denoiser.runningAvg[INTRA] which propagates to
	// reference-aligned buffers in commitInterFrameAttempt.
	e.applyDenoiserToInterFrame(source, rows, cols)
	phase = e.phaseStart()
	cfg.LoopFilterLevel, err = e.pickLoopFilterLevel(source, vp8common.InterFrame, cfg.LoopFilterLevel, cfg.SharpnessLevel, rows, cols, required, segmentation, cfg.RefreshGolden, cfg.RefreshAltRef)
	e.phaseEnd(encoderPhaseLoopFilterPick, phase)
	if err != nil {
		return interFrameEncodeAttempt{}, err
	}
	lfHeader := e.encoderLoopFilterHeader(cfg.LoopFilterLevel, cfg.SharpnessLevel)
	cfg.SimpleLoopFilter = lfHeader.Type == vp8dec.SimpleLoopFilter
	cfg.LFDeltaEnabled = lfHeader.DeltaEnabled
	cfg.LFDeltaUpdate = e.computeLFDeltaUpdateBit(vp8common.InterFrame, lfHeader.DeltaEnabled, lfHeader.RefDeltas, lfHeader.ModeDeltas)
	cfg.LFDeltaForceUpdateAll = e.forceLFDeltaUpdates()
	cfg.RefLFDeltas = lfHeader.RefDeltas
	cfg.ModeLFDeltas = lfHeader.ModeDeltas
	if cfg.RefreshLast || cfg.RefreshGolden || cfg.RefreshAltRef {
		phase = e.phaseStart()
		err = e.applyReconstructionLoopFilter(vp8common.InterFrame, lfHeader, segmentation, rows, cols, required)
		e.phaseEnd(encoderPhaseLoopFilterApply, phase)
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
	} else {
		e.loopFilterPickReady = false
		e.loopFilterPickBest = false
	}
	if segmentation.Enabled {
		updateInterFrameSegmentationTreeProbs(&segmentation, e.interFrameModes[:required])
		cfg.Segmentation = segmentation
	}
	cfg.ProbSkipFalse = interFrameModeSkipFalseProbability(rows, cols, e.interFrameModes[:required], cfg.ProbSkipFalse)
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
		MVBase:     &e.modeProbs.MV,
		Scratch:    &e.partScratch,
	}
	// Lane D: hand the pre-built coefficient token counts and record stream
	// to the packet writer so it can skip its own count/context/QCoeff walks.
	// The caches are only valid after a successful single-threaded
	// reconstruction pass; threaded reconstructions do not populate them
	// (see encoder_row_threaded.go) and the writer falls back in that case.
	if e.interCoefTokenCountsValid {
		packet.PrebuiltCoefCounts = &e.interCoefTokenCounts
	}
	if e.interCoefTokenRecordsValid {
		packet.PrebuiltCoefTokens = &e.interCoefTokenRecords
	}
	phase = e.phaseStart()
	packetResult, err := packet.Write()
	e.phaseEnd(encoderPhasePacketWrite, phase)
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	n := packetResult.Size
	projectedBits := encodedSizeBits(n)
	coefSavings := 0
	refFrameSavings := 0
	if needProjectedSize {
		coefSavings = packetResult.CoefSavingsBits
		projectedBits, refFrameSavings = e.projectedFrameSizeBitsFromRateWithKnownCoefSavings(false, required, projectedRate, coefSavings, cfg.RefreshGolden, cfg.RefreshAltRef)
	}
	return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: packetResult.FrameCoefProbs, FrameYModeProbs: packetResult.FrameYModeProbs, FrameUVModeProbs: packetResult.FrameUVModeProbs, FrameMVProbs: packetResult.FrameMVProbs, Size: n, ProjectedSizeBits: projectedBits, CoefSavingsBits: coefSavings, RefFrameSavingsBits: refFrameSavings, CyclicRefresh: cyclicRefreshEnabled, CyclicRefreshNextIndex: cyclicRefreshNextIndex}, nil
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
	return max(projectedBits-coefSavings-refFrameSavings, 0), refFrameSavings
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
// projected_frame_size in TestOracleEncoderTraceDecisionCompare. Mirroring
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
	return libvpxRefFrameEntropySavings(false, intra, last, golden, alt, int(e.refProbIntra), int(e.refProbLast), int(e.refProbGolden))
}

func (e *VP8Encoder) coefficientEntropySavingsBits(keyFrame bool, macroblocks int) int {
	if macroblocks <= 0 {
		return 0
	}
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
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
