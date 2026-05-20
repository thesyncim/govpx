package govpx

import (
	"errors"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// Reset returns the encoder to its cold-start state while retaining
// allocated buffers and the validated [EncoderOptions]. Rate-control,
// reference, lookahead, ARNR, denoiser, two-pass, and temporal-layer
// state are all cleared; queued lookahead frames are discarded; the
// next [VP8Encoder.EncodeInto] starts as if from [NewVP8Encoder]
// without re-running its allocations. [VP8Encoder.LastQuantizer]
// reports ok=false again until the next committed frame. Calls on a
// nil encoder are no-ops.
func (e *VP8Encoder) Reset() {
	if e == nil {
		return
	}
	// Encoder-level scalars / flags.
	e.forceKeyFrame = false
	e.frameCount = 0
	e.keyFrameFrequency = e.opts.KeyFrameInterval
	e.keyFramesDisabled = false
	e.fixedKeyFrameCounter = 0
	e.lastQuantizerPublic = 0
	e.lastQuantizerInternal = 0
	e.lastQuantizerValid = false
	e.cyclicRefreshIndex = 0
	e.segmentationHeaderEnabled = false
	e.lastSegmentationConfig = vp8enc.SegmentationConfig{}
	e.clearRuntimePreservedSegmentationHeader()
	e.rtcExternalDisableCyclicRefresh = e.opts.RTCExternalRateControl
	e.lookaheadRead = 0
	e.lookaheadWrite = 0
	e.lookaheadCount = 0
	e.clearPendingLookaheadReferenceSets()
	e.clearLatestLookaheadReferenceSets()
	e.nextReferenceSetSeq = 0
	for i := range e.lookahead {
		clearQueuedReferenceSets(e.lookahead[i].setReferences)
		e.lookahead[i].setReferences = e.lookahead[i].setReferences[:0]
	}
	e.arnrLastReady = false
	e.denoiser.reset()
	e.firstPassCount = 0
	clearCyclicRefreshMap(e.cyclicRefreshMap)
	clearCyclicRefreshMap(e.cyclicRefreshAttemptMap)
	clearUint8Map(e.skinMap)
	clearUint8Map(e.activeMap)
	clear(e.activityMap)
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	clearBoolMap(e.dotArtifactChecked)
	e.activeMapEnabled = false
	e.activityMapValid = false
	e.activityProbeRDMult = 0
	e.activityProbeRDDiv = 0
	e.activityProbeRDValid = false
	e.activityProbeStaleActZbinAdj = 0
	e.activityProbeAboveContextSeeded = false
	e.roi.reset()
	e.useROIStaticThreshold = false
	e.applyChangeConfigSegmentEncodeBreakout()
	e.lastInterZeroMVCount = 0
	e.lastInterSkipCount = 0
	e.lastFrameInterModesValid = false
	e.lastCodedFrameType = vp8common.KeyFrame
	e.clearAltRefSchedule()
	e.resetGoldenFrameStats()
	// Zero the inter-RD threshold-cache generation BEFORE
	// resetInterRDThresholdMultipliers bumps it back to 1 so cold-start
	// parity is preserved (NewVP8Encoder seeds gen=1 via this same call).
	e.interRDThreshBaselineGen = 0
	e.resetInterRDThresholdMultipliers()
	e.interRDFrameActive = false
	e.probSkipFalse = 128
	e.lastSkipFalseProbs = [3]uint8{}
	e.baseSkipFalseProbs = vp8enc.DefaultBaseSkipFalseProbs
	// libvpx vp8/encoder/onyx_if.c init_config seeds these per-reference
	// probabilities; without restoring them on Reset the warmed values
	// from a prior run leak into the next encode, biasing rate control.
	e.refProbIntra = 63
	e.refProbLast = 128
	e.refProbGolden = 128
	e.goldenRefAliasesLast = false
	e.altRefAliasesLast = false
	e.goldenRefAliasesAlt = false
	e.referenceFrameNumbers = [vp8common.MaxRefFrames]uint64{}
	e.thisKeyFrameForced = false
	e.ambientErr = 0
	e.mbsZeroLastDotSuppress = 0
	e.currentTemporalLayer = 0
	e.lastFramePercentIntra = 0
	e.framesSinceGolden = 0
	e.sourceAltRefActive = false
	e.sourceAltRefPending = false
	e.altRefSourceValid = false
	e.altRefSourcePTS = 0
	e.framesTillAltRefFrame = 0
	e.clearExternalRefreshMaskAfterPacket()
	e.clearExternalReferenceMaskAfterPacket()
	e.clearCarriedNoUpdateEntropyAfterPacket()
	e.autoAltRefStashValid = false
	e.autoAltRefStashPTS = 0
	e.autoAltRefStashDuration = 0
	e.autoAltRefStashFlags = 0
	e.currentSourcePTS = 0
	e.savedContext = savedCodingContext{}
	e.timing = timingFromEncoderOptions(e.opts)
	e.sourceTS = newEncoderSourceTimestampState(e.timing)
	// Re-zero every per-MB and per-row decision/coefficient buffer.
	for i := range e.keyFrameModes {
		e.keyFrameModes[i] = vp8enc.KeyFrameMacroblockMode{}
	}
	for i := range e.interFrameModes {
		e.interFrameModes[i] = vp8enc.InterFrameMacroblockMode{}
	}
	clearBoolMap(e.gfActiveMap)
	for i := range e.lastFrameInterModes {
		e.lastFrameInterModes[i] = vp8enc.InterFrameMacroblockMode{}
	}
	for i := range e.lastFrameInterModeBias {
		e.lastFrameInterModeBias[i] = false
	}
	for i := range e.keyFrameCoeffs {
		e.keyFrameCoeffs[i] = vp8enc.MacroblockCoefficients{}
	}
	for i := range e.tokenAbove {
		e.tokenAbove[i] = vp8enc.TokenContextPlanes{}
	}
	for i := range e.reconstructAboveTok {
		e.reconstructAboveTok[i] = vp8enc.TokenContextPlanes{}
	}
	for i := range e.reconstructModes {
		e.reconstructModes[i] = vp8dec.MacroblockMode{}
	}
	for i := range e.reconstructTokens {
		e.reconstructTokens[i] = vp8dec.MacroblockTokens{}
	}
	e.dequantTables = vp8common.FrameDequantTables{}
	e.dequants = [vp8common.MaxMBSegments]vp8common.MacroblockDequant{}
	e.reconstructScratch = vp8dec.IntraReconstructionScratch{}
	vp8enc.ResetInterCoefficientTokenCounts(&e.interCoefTokenCounts)
	e.interCoefTokenCountsValid = false
	e.interCoefTokenRecords.Reset(encoderMacroblockRows(e.opts.Height), encoderMacroblockCount(e.opts.Width, e.opts.Height))
	e.interCoefTokenRecordsValid = false
	// libvpx MT helper-history accumulator (vp8_encoder.go
	// mtHelperYModeCountAccum). vp8cx_remove_encoder_threads frees the
	// mb_row_ei pool on Reset, so the accumulator goes with it.
	e.mtHelperYModeCountAccum = [vp8tables.YModeProbCount][2]int{}
	e.mtHelperUVModeCountAccum = [vp8tables.UVModeProbCount][2]int{}
	e.mtHelperRowAccumWorkerCount = 0
	e.mtHelperRowAccumValid = false
	e.lastInterReconstructWorkerCount = 0
	e.lastKeyFrameReconstructWorkerCount = 0
	e.interRDCoeffCacheSlots = [2]interRDCoeffCacheState{}
	e.interRDCoeffCacheWinner = 0
	e.interRDCoeffCacheScratchTarget = nil
	e.partScratch.Reset()
	e.loopInfo = vp8common.LoopFilterInfo{}
	e.loopInfoAlt = vp8common.LoopFilterInfo{}
	e.loopFilterLevel = 0
	e.lfDeltasSignaledOnce = false
	e.lastSignaledRefLFDeltas = [vp8common.MaxRefLFDeltas]int8{}
	e.lastSignaledModeLFDeltas = [vp8common.MaxModeLFDeltas]int8{}
	e.pendingLFDeltaUpdate = false
	e.currentLFDeltaUpdate = false
	e.autoAltRefStashForceLF = false
	e.temporalLayerRefUsage = [vp8common.MaxRefFrames]int{}
	e.coefProbsLast = vp8tables.CoefficientProbs{}
	e.coefProbsGolden = vp8tables.CoefficientProbs{}
	e.coefProbsAltRef = vp8tables.CoefficientProbs{}
	e.coefProbsSnapshotsValid = false
	e.rdPickerCoefProbsActive = nil
	// resetInterRDThresholdMultipliers() bumped interRDThreshBaselineGen
	// once during the encoder-level scalars block above; leave it at 1 to
	// match NewVP8Encoder's cold-start trajectory.
	e.interRDThreshBaselineSlots = [interRDThreshBaselineSlotCount]interRDThreshBaselineSlot{}
	e.interRDFrameBaseQIndex = 0
	e.interRDFrameRefSearchOrder = [4]int8{}
	e.interRDFrameRefSearchOrderValid = false
	e.interMBsTestedSoFar = 0
	e.interModeCheckFreq = [libvpxInterModeCount]int{}
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interModeErrorBins = [1024]uint32{}
	e.interModeSpeedErrorBins = [1024]uint32{}
	e.resetOracleTraceState()
	// Rate-control: zero the entire struct then re-apply config so every
	// field NewVP8Encoder ever touches lands at the cold-start value.
	// Reseed the cached frame dimensions before applyConfig so the
	// libvpx raw-target-rate cap inside setBitrateKbps survives the
	// rateControlState zeroing (see libvpxClampToRawTargetRate).
	e.rc = rateControlState{}
	e.rc.setFrameDimensions(e.opts.Width, e.opts.Height)
	cfg := defaultRateControlConfig(e.opts)
	_ = e.rc.applyConfig(cfg, e.timing)
	e.rc.keyFrameFrequency = e.opts.KeyFrameInterval
	// libvpx vp8/vp8_cx_iface.c:377-378 derives `cpi->oxcf.auto_key` as
	// `kf_mode == VPX_KF_AUTO && kf_min_dist != kf_max_dist`. govpx's
	// `EncoderOptions.KeyFrameInterval > 0` mirrors the `kf_max_dist`
	// max-cadence cohort (kf_min_dist defaults to 0), so any non-zero
	// interval is the libvpx auto_key=1 case. The bootstrap clamp inside
	// `estimateKeyFrameFrequency` (ratectrl.c:1318-1323) only fires when
	// `oxcf.auto_key && (1 + fps*2) > key_freq`; without mirroring
	// auto_key here, govpx's first-keyframe `kf_bitrate_adjustment` uses
	// the unclamped two-second estimate while libvpx clamps to
	// `key_freq`, diverging the first inter frame's per-frame target
	// (see task #202 regression seed
	// regression_vbr_300kbps_kf30_splitmv_defbuf_rt_cpu0_fbab04f4).
	e.rc.autoKeyFrames = !e.keyFramesDisabled && e.opts.KeyFrameInterval > 0
	e.rc.minFrameBandwidth = vbrMinFrameBandwidthBits(e.rc.bitsPerFrame, e.opts.TwoPassMinPct)
	if e.rc.mode != RateControlCBR && len(e.opts.TwoPassStats) == 0 {
		e.rc.framesTillGFUpdateDue = libvpxDefaultGFInterval
		e.rc.onePassAutoGold = true
	}
	// libvpx onyx_if.c vp8_create_compressor (line 1818 then re-overridden
	// at line 1886) leaves cpi->baseline_gf_interval == gf_interval_onepass_cbr
	// for any (CBR && !error_resilient) one-pass compressor. Reseed here
	// so a Reset preserves the same first-keyframe contract NewVP8Encoder
	// established (sentinel 0 defers gf_interval_onepass_cbr derivation
	// to libvpxKeyFrameSetupGFInterval at first-KF time).
	if e.rc.mode == RateControlCBR && !e.opts.ErrorResilient && len(e.opts.TwoPassStats) == 0 {
		e.rc.baselineGFInterval = 0
	} else {
		e.rc.baselineGFInterval = libvpxDefaultGFInterval
	}
	e.cyclicRefreshConfigured = e.opts.ErrorResilient ||
		(e.rc.mode == RateControlCBR && len(e.opts.TwoPassStats) == 0)
	e.runtimePreserveSegmentationUpdate = false
	e.runtimeSegmentationUpdatePending = false
	e.rtcExternalDisableCyclicRefresh = e.opts.RTCExternalRateControl
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
	} else {
		e.rc.currentQuantizer = e.rc.minQuantizer
	}
	e.rc.lastQuantizer = e.rc.currentQuantizer
	e.rc.lastInterQuantizer = e.rc.currentQuantizer
	e.rc.bufferLevelBits = e.rc.bufferInitialBits
	e.rc.avgFrameQuantizer = e.rc.maxQuantizer
	e.rc.normalInterAvgQuantizer = e.rc.maxQuantizer
	e.rc.frameTargetBits = e.rc.bitsPerFrame
	// libvpx vp8_create_compressor seeds cpi->force_maxqp = 0 and
	// cpi->frames_since_last_drop_overshoot = 0; mirror that on Reset
	// so a sequence re-init does not leak overshoot-drop state from the
	// previous run.
	e.clearForceMaxQuantizer()
	e.framePredictionError = 0
	e.lastPredErrorMB = 0
	// Temporal layer state.
	e.temporal.frameIndex = 0
	e.temporal.tl0PicIdx = 0
	e.temporal.tl0Valid = false
	e.temporal.refLayer = [temporalReferenceCount]int{}
	e.temporal.accounting = [MaxTemporalLayers]temporalLayerAccounting{}
	e.temporal.buffersSet = false
	e.temporal.codingState = [MaxTemporalLayers]temporalLayerCodingState{}
	e.temporal.codingValid = [MaxTemporalLayers]bool{}
	e.initializeTemporalLayerCodingStates()
	e.twoPass.configure(e.opts.TwoPassStats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	e.twoPass.configureQuantizerBounds(e.rc.minQuantizer, e.rc.maxQuantizer)
	e.twoPass.configureErrorResilient(e.opts.ErrorResilient || e.opts.ErrorResilientPartitions)
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	// libvpx vp8_cx_iface.c reseeds cpi->oxcf.auto_key and
	// cpi->key_frame_frequency in init_config; Reset() mirrors that
	// re-seeding so prepareKFGroup / framesToKey see the same
	// configured KF cadence as a cold-start encoder.
	e.twoPass.configureKeyFrameInterval(e.opts.KeyFrameInterval, e.opts.AdaptiveKeyFrames && e.opts.KeyFrameInterval > 0)
	// libvpx vp8/encoder/firstpass.c frame_max_bits (lines 316-368)
	// dispatches on cpi->oxcf.end_usage; Reset() reseeds it to mirror
	// NewVP8Encoder's cold-start configuration so two-pass + CBR/VBR
	// dispatch stays aligned after a Reset.
	e.twoPass.configureEndUsage(libvpxVP8EndUsageFromRateControlMode(e.rc.mode))
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	e.subMVRefProbs = vp8enc.DefaultSubMVRefProbs
	e.resetNoUpdateEntropyRollbackContext()
	e.mvCostTables = vp8enc.MotionVectorCostTables{}
	e.mvCostProbs = [2][vp8tables.MVPCount]uint8{}
	e.mvCostTablesValid = false
	// libvpx vp8_create_compressor seeds cpi->Speed=0 and avg_pick_mode_time
	// /avg_encode_time = 0 (zero-initialised under calloc). Mirror that on
	// Reset() so a sequence re-init does not leak the warmed adaptive Speed
	// from the previous run; otherwise the bench harness's warmup pass
	// drives e.autoSpeed away from the cold-start seed of 4 before the
	// measured pass starts, producing a non-deterministic per-frame size
	// distribution and an inflated avg_interframe_bytes ratio vs libvpx
	// (which always starts cold under vpxenc).
	e.resetAutoSpeedTiming()
	e.current.Reset()
	e.analysis.Reset()
	e.lastRef.Reset()
	e.goldenRef.Reset()
	e.altRef.Reset()
	e.loopFilterPick.Reset()
	e.loopFilterBest.Reset()
	e.loopFilterPickAlt.Reset()
	e.loopFilterPickReady = false
	e.loopFilterPickLevel = 0
	e.loopFilterPickBest = false
	e.loopFilterSegmentLF = [vp8common.MaxMBSegments]int8{}
	e.rowWorkers.resetForEncoderReset()
}

// Close releases encoder state and shuts down any row-worker pool. After
// Close, every method on this encoder returns [ErrClosed]. Calling Close
// on a nil or already-closed encoder also returns [ErrClosed].
func (e *VP8Encoder) Close() error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.Reset()
	if e.rowWorkers != nil {
		e.rowWorkers.shutdownPool()
		e.rowWorkers = nil
	}
	e.closed = true
	return nil
}

func normalizeEncoderOptions(opts EncoderOptions) (EncoderOptions, timingState, error) {
	if !validDimension(opts.Width) || !validDimension(opts.Height) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.Threads == 0 {
		// Mirror libvpx default (vp8/encoder/onyx_if.c init: oxcf
		// multi_threaded defaults to 0/1). Threads=0 is the historical
		// zero-initialized govpx default; collapse it onto 1 so internal
		// code can call effectiveThreadCount() without re-checking.
		opts.Threads = 1
	}
	if opts.Threads > maxEncoderThreads {
		opts.Threads = maxEncoderThreads
	}
	if opts.FPS < 0 || opts.TimebaseNum < 0 || opts.TimebaseDen < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.TimebaseNum == 0 && opts.TimebaseDen != 0 || opts.TimebaseNum != 0 && opts.TimebaseDen == 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.FPS == 0 && opts.TimebaseNum == 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if !validRateControlMode(opts.RateControlMode) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.TargetBitrateKbps <= 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidBitrate
	}
	if opts.UndershootPct < 0 || opts.UndershootPct > maxRateControlUndershootPct ||
		opts.OvershootPct < 0 || opts.OvershootPct > maxRateControlOvershootPct {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.MaxIntraBitratePct < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.GFCBRBoostPct < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.MinQuantizer > maxQuantizer || opts.MaxQuantizer > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.MinQuantizer > opts.MaxQuantizer && opts.MaxQuantizer != 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.CQLevel < 0 || opts.CQLevel > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.Deadline < DeadlineBestQuality || opts.Deadline > DeadlineRealtime {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.Tuning < TunePSNR || opts.Tuning > TuneSSIM {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.CpuUsed < -16 || opts.CpuUsed > 16 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	opts.CpuUsed = libvpxEffectiveCPUUsed(opts.Deadline, opts.CpuUsed)
	if opts.KeyFrameInterval < 0 || opts.LookaheadFrames < 0 || opts.LookaheadFrames > maxLookaheadFrames || opts.TokenPartitions < int(vp8common.OnePartition) || opts.TokenPartitions > int(vp8common.EightPartition) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.ARNRType == 0 {
		opts.ARNRType = 3
	}
	if opts.Sharpness < 0 || opts.Sharpness > 7 ||
		opts.NoiseSensitivity < 0 || opts.NoiseSensitivity > 6 ||
		opts.ARNRMaxFrames < 0 || opts.ARNRMaxFrames > maxARNRFrames ||
		opts.ARNRStrength < 0 || opts.ARNRStrength > 6 ||
		opts.ARNRType < 1 || opts.ARNRType > 3 ||
		opts.TwoPassVBRBiasPct < 0 || opts.TwoPassMinPct < 0 || opts.TwoPassMaxPct < 0 ||
		opts.ScreenContentMode < 0 || opts.ScreenContentMode > 2 || opts.StaticThreshold < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}

	timing := timingState{frameDuration: 1}
	if opts.TimebaseNum > 0 {
		timing.timebaseNum = opts.TimebaseNum
		timing.timebaseDen = opts.TimebaseDen
	} else {
		timing.timebaseNum = 1
		timing.timebaseDen = opts.FPS
		opts.TimebaseNum = 1
		opts.TimebaseDen = opts.FPS
	}
	if opts.FPS == 0 && timing.timebaseNum == 1 {
		opts.FPS = timing.timebaseDen
	}
	if opts.KeyFrameInterval == 0 {
		opts.KeyFrameInterval = 120
	}
	return opts, timing, nil
}

func validDimension(v int) bool {
	return v > 0 && v <= maxVP8Dimension
}

func translateEncoderError(err error) error {
	switch {
	case errors.Is(err, vp8enc.ErrBufferTooSmall):
		return ErrBufferTooSmall
	case errors.Is(err, vp8enc.ErrInvalidPacketConfig), errors.Is(err, vp8enc.ErrModeBufferTooSmall):
		return ErrInvalidConfig
	default:
		return err
	}
}
