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
	e.lastQuantizerPublic = 0
	e.lastQuantizerInternal = 0
	e.lastQuantizerValid = false
	e.cyclicRefreshIndex = 0
	e.lookaheadRead = 0
	e.lookaheadWrite = 0
	e.lookaheadCount = 0
	e.arnrLastReady = false
	e.denoiser.reset()
	e.firstPassCount = 0
	clearCyclicRefreshMap(e.cyclicRefreshMap)
	clearCyclicRefreshMap(e.cyclicRefreshAttemptMap)
	clearUint8Map(e.skinMap)
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	clearBoolMap(e.dotArtifactChecked)
	e.lastInterZeroMVCount = 0
	e.lastInterSkipCount = 0
	e.lastFrameInterModesValid = false
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
	e.baseSkipFalseProbs = libvpxBaseSkipFalseProbs
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
	e.autoAltRefStashValid = false
	e.autoAltRefStashPTS = 0
	e.autoAltRefStashDuration = 0
	e.autoAltRefStashFlags = 0
	e.currentSourcePTS = 0
	e.savedContext = savedCodingContext{}
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
	vp8enc.ResetInterCoefficientTokenRecords(&e.interCoefTokenRecords, encoderMacroblockRows(e.opts.Height), encoderMacroblockCount(e.opts.Width, e.opts.Height))
	e.interCoefTokenRecordsValid = false
	e.partScratch.Reset()
	e.loopInfo = vp8common.LoopFilterInfo{}
	e.loopInfoAlt = vp8common.LoopFilterInfo{}
	e.loopFilterLevel = 0
	e.lfDeltasSignaledOnce = false
	e.lastSignaledRefLFDeltas = [vp8common.MaxRefLFDeltas]int8{}
	e.lastSignaledModeLFDeltas = [vp8common.MaxModeLFDeltas]int8{}
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
	e.interRDFrameRefSearchOrder = [4]int{}
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
	e.rc.autoKeyFrames = e.opts.AdaptiveKeyFrames
	e.rc.minFrameBandwidth = vbrMinFrameBandwidthBits(e.rc.bitsPerFrame, e.opts.TwoPassMinPct)
	if e.rc.mode != RateControlCBR && len(e.opts.TwoPassStats) == 0 {
		e.rc.framesTillGFUpdateDue = libvpxDefaultGFInterval
	}
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
	e.forceMaxQuantizer = false
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
	e.twoPass.configure(e.opts.TwoPassStats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
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
