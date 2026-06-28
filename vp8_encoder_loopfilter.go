package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) initReferenceFrames(width int, height int) error {
	if err := e.current.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.analysis.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.lastRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.goldenRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.altRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.loopFilterPick.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.loopFilterBest.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	// loopFilterPickAlt is only ever read/written on the Threads >= 2
	// parallel pickFull path; allocating it on Threads=1 would waste
	// ~921 KB at 720p for no benefit and break the "Threads=1 zero
	// added cost" contract. The Resize is done in NewVP8Encoder once
	// the rowWorkers pool has been constructed (it is not yet wired
	// up at the point initReferenceFrames runs).
	return nil
}

func (e *VP8Encoder) encoderLoopFilter(frameType vp8common.FrameType) (uint8, uint8) {
	level := vp8enc.LibvpxInitialLoopFilterLevel(e.rc.currentQuantizer)
	if frameType == vp8common.InterFrame {
		level = int(e.loopFilterLevel)
	}
	level = min(vp8enc.LibvpxClampLoopFilterLevel(e.rc.currentQuantizer, level), 63)
	sharpness := e.opts.Sharpness
	if frameType == vp8common.KeyFrame {
		sharpness = 0
	}
	return uint8(level), uint8(sharpness)
}

func (e *VP8Encoder) encoderLoopFilterHeader(level uint8, sharpness uint8) vp8dec.LoopFilterHeader {
	header := vp8dec.LoopFilterHeader{
		Level:          level,
		SharpnessLevel: sharpness,
	}
	if e.encoderUsesSimpleLoopFilter() {
		header.Type = vp8dec.SimpleLoopFilter
	}
	header.DeltaEnabled = true
	header.DeltaUpdate = true
	header.RefDeltas = [vp8common.MaxRefLFDeltas]int8{2, 0, -2, -2}
	header.ModeDeltas = [vp8common.MaxModeLFDeltas]int8{4, e.encoderLoopFilterInterModeDelta(), 2, 4}
	return header
}

func (e *VP8Encoder) encoderUsesSimpleLoopFilter() bool {
	if e.opts.Deadline != DeadlineRealtime {
		return false
	}
	return e.libvpxCPUUsed() >= 14
}

// computeLFDeltaUpdateBit mirrors libvpx vp8/encoder/bitstream.c pack_lf_deltas:
//
//	int send_update = xd->mode_ref_lf_delta_update || cpi->oxcf.error_resilient_mode;
//
// libvpx's `mode_ref_lf_delta_update` flag is set once at init in
// set_default_lf_deltas and cleared after every packed frame (see
// vp8/encoder/onyx_if.c). Every keyframe also calls setup_features before
// encoding, resetting last_*_lf_deltas to zero and setting the update flag
// again; the decoder similarly resets its loop-filter deltas on keyframes.
// Inter frames then write `update=0` until the defaults change, except in
// error-resilient mode where libvpx forces the update path. libvpx stores
// error-resilient mode as a bitmask, so VPX_ERROR_RESILIENT_PARTITIONS also
// forces the update path.
func (e *VP8Encoder) computeLFDeltaUpdateBit(frameType vp8common.FrameType, deltaEnabled bool, refDeltas [vp8common.MaxRefLFDeltas]int8, modeDeltas [vp8common.MaxModeLFDeltas]int8) bool {
	if !deltaEnabled {
		return false
	}
	if e.forceLFDeltaUpdates() {
		return true
	}
	if e.currentLFDeltaUpdate {
		return true
	}
	if frameType == vp8common.KeyFrame {
		return true
	}
	if !e.lfDeltasSignaledOnce {
		return true
	}
	return refDeltas != e.lastSignaledRefLFDeltas || modeDeltas != e.lastSignaledModeLFDeltas
}

func (e *VP8Encoder) forceLFDeltaUpdates() bool {
	return e.opts.ErrorResilient || e.opts.ErrorResilientPartitions
}

// forceNextLFDeltaUpdate mirrors libvpx vp8_change_config, which routes
// runtime encoder config updates through set_default_lf_deltas and leaves
// xd->mode_ref_lf_delta_update set for the input frame receiving that
// control. With lookahead enabled that input can be encoded several calls
// later, so the force bit is carried by the lookahead entry rather than by
// the next packet emitted from the queue.
//
// The same vp8_change_config path re-runs setup_features
// (vp8/encoder/onyx_if.c:1558), which in turn memsets
// xd->last_ref_lf_deltas and xd->last_mode_lf_deltas to zero
// (vp8/encoder/onyx_if.c:396-399):
//
//	memset(cpi->mb.e_mbd.last_ref_lf_deltas, 0,
//	       sizeof(cpi->mb.e_mbd.ref_lf_deltas));
//	memset(cpi->mb.e_mbd.last_mode_lf_deltas, 0,
//	       sizeof(cpi->mb.e_mbd.mode_lf_deltas));
//
// Mirror that reset here so the next pack_lf_deltas comparison in
// computeLFDeltaUpdateBit sees the post-change_config baseline (all zero)
// rather than the deltas signaled by the previous frame. Without this
// reset, on a runtime-control transition we either suppress send_update
// where libvpx emits it or emit deltas where libvpx writes zero updates,
// diverging from libvpx byte-for-byte on the inter frame following the
// control call (gap D on the noise:0 transition seed).
//
// If ROI segmentation is currently enabled, libvpx also marks both the
// segmentation map and feature data for update on that next frame.
func (e *VP8Encoder) forceNextLFDeltaUpdate() {
	e.pendingLFDeltaUpdate = true
	e.lastSignaledRefLFDeltas = [vp8common.MaxRefLFDeltas]int8{}
	e.lastSignaledModeLFDeltas = [vp8common.MaxModeLFDeltas]int8{}
	if e.roi.enabled {
		e.roi.updateMap = true
		e.roi.updateData = true
	}
}

func (e *VP8Encoder) consumePendingLFDeltaUpdate() bool {
	force := e.pendingLFDeltaUpdate
	e.pendingLFDeltaUpdate = false
	return force
}

func (e *VP8Encoder) restorePendingLFDeltaUpdateAfterDrop(force bool) {
	if force {
		e.pendingLFDeltaUpdate = true
	}
}

// updateLastSignaledLFDeltas commits the per-frame loop-filter delta
// snapshot that future frames compare against to decide whether to set
// mode_ref_lf_delta_update. Called from the keyframe / inter-frame commit
// paths so recode iterations within a frame see the pre-frame state.
func (e *VP8Encoder) updateLastSignaledLFDeltas(deltaEnabled bool, refDeltas [vp8common.MaxRefLFDeltas]int8, modeDeltas [vp8common.MaxModeLFDeltas]int8) {
	if !deltaEnabled {
		return
	}
	e.lastSignaledRefLFDeltas = refDeltas
	e.lastSignaledModeLFDeltas = modeDeltas
	e.lfDeltasSignaledOnce = true
}

func (e *VP8Encoder) encoderLoopFilterInterModeDelta() int8 {
	if e.opts.Deadline == DeadlineRealtime {
		return -12
	}
	return -2
}

func (e *VP8Encoder) pickLoopFilterLevel(src vp8enc.SourceImage, frameType vp8common.FrameType, seedLevel uint8, sharpness uint8, rows int, cols int, required int, segmentation vp8enc.SegmentationConfig, refreshGolden bool, refreshAltRef bool) (uint8, error) {
	e.loopFilterPickReady = false
	e.loopFilterPickLevel = 0
	e.loopFilterPickBest = false
	if len(e.reconstructModes) < required {
		return 0, ErrInvalidConfig
	}
	minLevel := e.libvpxMinLoopFilterLevelForFrame(frameType, refreshGolden, refreshAltRef)
	ctx := e.newLoopFilterPickContext(src, frameType, sharpness, rows, cols, required, segmentation)
	if e.loopFilterUsesFastSearchForFrame() {
		level, err := ctx.pickFast(seedLevel, minLevel)
		if err == nil && level > 0 {
			e.installLoopFilterSegmentLF(segmentation)
		}
		return level, err
	}
	return ctx.pickFull(seedLevel, minLevel)
}

func (e *VP8Encoder) loopFilterUsesFastSearchForFrame() bool {
	return loopFilterUsesFastSearchForDeadlineSpeed(e.opts.Deadline, e.libvpxRealtimeCPISpeedForAutoFilterGate())
}

func (e *VP8Encoder) loopFilterUsesFastSearch() bool {
	return loopFilterUsesFastSearchForDeadlineSpeed(e.opts.Deadline, e.libvpxCPUUsed())
}

func loopFilterUsesFastSearchForDeadlineSpeed(deadline Deadline, speed int) bool {
	// libvpx vp8/encoder/onyx_if.c vp8_set_speed_features (Mode==2 realtime,
	// Mode==1 good-quality): sf->RD = 0 (partial-frame picker) flips at
	// speed > 4 for good-quality and speed == 3 || speed > 4 for
	// realtime. Mirrored exactly.
	switch deadline {
	case DeadlineGoodQuality:
		return speed > 4
	case DeadlineRealtime:
		return speed == 3 || speed > 4
	default:
		return false
	}
}

type loopFilterPickContext struct {
	encoder         *VP8Encoder
	src             vp8enc.SourceImage
	modes           []vp8dec.MacroblockMode
	fastFrameConfig vp8common.LoopFilterFrameConfig
	fullFrameConfig vp8common.LoopFilterFrameConfig
	frameType       vp8common.FrameType
	filterType      vp8dec.LoopFilterType
	rows            int
	cols            int
}

func (e *VP8Encoder) newLoopFilterPickContext(src vp8enc.SourceImage, frameType vp8common.FrameType, sharpness uint8, rows int, cols int, required int, segmentation vp8enc.SegmentationConfig) loopFilterPickContext {
	header := e.encoderLoopFilterHeader(0, sharpness)
	vp8common.InitLoopFilterInfo(&e.loopInfo, int(sharpness))
	fullConfig := vp8dec.LoopFilterFrameConfig(header, loopFilterSegmentationHeader(segmentation))
	fastConfig := fullConfig
	if segmentation.Enabled {
		fastConfig.SegmentLF = e.loopFilterSegmentLF
	}
	return loopFilterPickContext{
		encoder:         e,
		src:             src,
		modes:           e.reconstructModes[:required],
		fastFrameConfig: fastConfig,
		fullFrameConfig: fullConfig,
		frameType:       frameType,
		filterType:      header.Type,
		rows:            rows,
		cols:            cols,
	}
}

func (e *VP8Encoder) installLoopFilterSegmentLF(segmentation vp8enc.SegmentationConfig) {
	if !segmentation.Enabled {
		return
	}
	var installed [vp8common.MaxMBSegments]int8
	for segment := range vp8common.MaxMBSegments {
		if segmentation.FeatureEnabled[vp8common.MBLvlAltLF][segment] {
			installed[segment] = segmentation.FeatureData[vp8common.MBLvlAltLF][segment]
		}
	}
	e.loopFilterSegmentLF = installed
}

func (ctx *loopFilterPickContext) pickFast(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	if vp8PhaseStatsEnabled {
		if stats := e.phaseStats(); stats != nil {
			return ctx.pickFastStats(seedLevel, minLevel, stats)
		}
	}
	return ctx.pickFastNoStats(seedLevel, minLevel)
}

func (ctx *loopFilterPickContext) pickFastNoStats(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	level := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	bestErr := ctx.cachedPartialLumaSSE(level, &ssErr, &ssSet)
	if traceEnabled {
		e.emitOracleLFTrial("seed", level, bestErr)
	}

	filtLevel := level - vp8enc.LoopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := ctx.cachedPartialLumaSSE(filtLevel, &ssErr, &ssSet)
		if traceEnabled {
			e.emitOracleLFTrial("down", filtLevel, filtErr)
		}
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= vp8enc.LoopFilterSearchStep(filtLevel)
	}

	filtLevel = level + vp8enc.LoopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := ctx.cachedPartialLumaSSE(filtLevel, &ssErr, &ssSet)
			if traceEnabled {
				e.emitOracleLFTrial("up", filtLevel, filtErr)
			}
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += vp8enc.LoopFilterSearchStep(filtLevel)
		}
	}
	return uint8(vp8enc.ClampLoopFilterPickLevel(bestLevel, minLevel, maxLevel)), nil
}

func (ctx *loopFilterPickContext) pickFastStats(seedLevel uint8, minLevel int, stats *EncoderPhaseStats) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	level := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	bestErr := ctx.cachedPartialLumaSSEStats(level, &ssErr, &ssSet, stats)
	if traceEnabled {
		e.emitOracleLFTrial("seed", level, bestErr)
	}

	filtLevel := level - vp8enc.LoopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := ctx.cachedPartialLumaSSEStats(filtLevel, &ssErr, &ssSet, stats)
		if traceEnabled {
			e.emitOracleLFTrial("down", filtLevel, filtErr)
		}
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= vp8enc.LoopFilterSearchStep(filtLevel)
	}

	filtLevel = level + vp8enc.LoopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := ctx.cachedPartialLumaSSEStats(filtLevel, &ssErr, &ssSet, stats)
			if traceEnabled {
				e.emitOracleLFTrial("up", filtLevel, filtErr)
			}
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += vp8enc.LoopFilterSearchStep(filtLevel)
		}
	}
	return uint8(vp8enc.ClampLoopFilterPickLevel(bestLevel, minLevel, maxLevel)), nil
}

func (ctx *loopFilterPickContext) pickFull(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	if vp8PhaseStatsEnabled {
		if stats := e.phaseStats(); stats != nil {
			return ctx.pickFullStats(seedLevel, minLevel, stats)
		}
	}
	return ctx.pickFullNoStats(seedLevel, minLevel)
}

func (ctx *loopFilterPickContext) pickFullNoStats(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if ctx.fullFrameConfig.SegmentationEnabled {
		e.loopFilterSegmentLF = ctx.fullFrameConfig.SegmentLF
	}
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	filtMid := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	residentLevel := -1
	bestLumaLevel := -1

	bestErr := ctx.cachedFullLumaSSE(filtMid, &ssErr, &ssSet, &residentLevel)
	if traceEnabled {
		e.emitOracleLFTrial("full", filtMid, bestErr)
	}
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		// Mirror libvpx vp8/encoder/picklpf.c vp8cx_pick_filter_level
		// (Bias = (best_err >> (15 - (filt_mid / 8))) * filter_step), then
		// scale that bias by section_intra_rating / 20 when the intra
		// rating is below 20. One-pass and realtime paths can therefore
		// use zero bias because libvpx's calloc'd two-pass state leaves
		// section_intra_rating at zero.
		bias := vp8enc.LoopFilterFullPickerBias(bestErr, filtMid, filterStep, e.twoPass.sectionIntraRating)
		filtHigh := min(filtMid+filterStep, maxLevel)
		filtLow := max(filtMid-filterStep, minLevel)

		// Parallel filt_low / filt_high trials at Threads >= 2 when this
		// iteration would otherwise run both serially (filtDirection == 0,
		// both bracket levels distinct from filtMid, neither cached). The
		// filt_low trial runs on the row-worker pool against the alt
		// scratch (loopFilterPickAlt + loopInfoAlt); the filt_high trial
		// runs inline on the calling goroutine against the resident
		// scratch (loopFilterPick + loopInfo). After both finish the
		// score-update conditionals are applied in the same serial order
		// as the non-parallel branch, so bestErr / filtBest converge
		// byte-identically to the serial run. residentLevel is set to
		// filtHigh to match the post-iteration state of the serial code.
		// loopFilterPick (resident scratch) holds the filtHigh-filtered
		// luma; loopFilterPickAlt holds the filtLow-filtered luma. If
		// filtLow becomes the new filtBest we patch loopFilterBest with
		// the alt-scratch luma so the chroma post-pass reuse decision in
		// applyReconstructionLoopFilter sees the correct preserved image
		// (mirroring the serial preserveBestBeforeTrial-before-filtHigh
		// copy that would have run had the trials been serial).
		// ssSet is [MaxLoopFilter+1=64]bool (pow2); LF trial levels are
		// clamped to [0, MaxLoopFilter]. AND-mask with 63 elides the
		// per-iter bounds check on both lookups.
		if filtDirection == 0 && filtLow != filtMid && filtHigh != filtMid &&
			!ssSet[filtLow&63] && !ssSet[filtHigh&63] && e.canParallelLFTrials() {
			ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
			filtErrLow, filtErrHigh := ctx.dispatchLFTrialPair(filtLow, filtHigh)
			// ssErr/ssSet are [MaxLoopFilter+1=64]; mask with 63 to elide
			// the bounds check on the indexed writes (matches the read
			// path above).
			ssErr[filtLow&63] = filtErrLow
			ssSet[filtLow&63] = true
			ssErr[filtHigh&63] = filtErrHigh
			ssSet[filtHigh&63] = true
			if filtHigh != 0 {
				residentLevel = filtHigh
			}
			if traceEnabled {
				e.emitOracleLFTrial("full", filtLow, filtErrLow)
				e.emitOracleLFTrial("full", filtHigh, filtErrHigh)
			}
			if filtErrLow-bias < bestErr {
				if filtErrLow < bestErr {
					bestErr = filtErrLow
				}
				filtBest = filtLow
				// Replicate the serial "preserveBestBeforeTrial(filtHigh,
				// filtBest=filtLow)" copy that would have run between
				// the two trials: residentLevel == filtBest == filtLow,
				// so loopFilterPick (which would hold the filtLow-filtered
				// luma in serial) gets copied to loopFilterBest. In
				// parallel that filtLow-filtered luma is in
				// loopFilterPickAlt instead, so we copy from there.
				if filtBest > 0 && bestLumaLevel != filtBest {
					vp8common.CopyImageLuma(&e.loopFilterBest.Img, &e.loopFilterPickAlt.Img)
					bestLumaLevel = filtBest
				}
			}
			if filtErrHigh < bestErr-bias {
				bestErr = filtErrHigh
				filtBest = filtHigh
			}
		} else {
			if filtDirection <= 0 && filtLow != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtLow]
				filtErr := ctx.cachedFullLumaSSE(filtLow, &ssErr, &ssSet, &residentLevel)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtLow, filtErr)
				}
				if filtErr-bias < bestErr {
					if filtErr < bestErr {
						bestErr = filtErr
					}
					filtBest = filtLow
				}
			}
			if filtDirection >= 0 && filtHigh != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtHigh, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtHigh]
				filtErr := ctx.cachedFullLumaSSE(filtHigh, &ssErr, &ssSet, &residentLevel)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtHigh, filtErr)
				}
				if filtErr < bestErr-bias {
					bestErr = filtErr
					filtBest = filtHigh
				}
			}
		}
		if filtBest == filtMid {
			filterStep /= 2
			filtDirection = 0
		} else {
			if filtBest < filtMid {
				filtDirection = -1
			} else {
				filtDirection = 1
			}
			filtMid = filtBest
		}
	}
	if filtBest > 0 {
		switch {
		case residentLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = false
		case bestLumaLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = true
		}
	}
	return uint8(filtBest), nil
}

func (ctx *loopFilterPickContext) pickFullStats(seedLevel uint8, minLevel int, stats *EncoderPhaseStats) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if ctx.fullFrameConfig.SegmentationEnabled {
		e.loopFilterSegmentLF = ctx.fullFrameConfig.SegmentLF
	}
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	filtMid := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	residentLevel := -1
	bestLumaLevel := -1

	bestErr := ctx.cachedFullLumaSSEStats(filtMid, &ssErr, &ssSet, &residentLevel, stats)
	if traceEnabled {
		e.emitOracleLFTrial("full", filtMid, bestErr)
	}
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		bias := vp8enc.LoopFilterFullPickerBias(bestErr, filtMid, filterStep, e.twoPass.sectionIntraRating)
		filtHigh := min(filtMid+filterStep, maxLevel)
		filtLow := max(filtMid-filterStep, minLevel)

		if filtDirection == 0 && filtLow != filtMid && filtHigh != filtMid &&
			!ssSet[filtLow&63] && !ssSet[filtHigh&63] && e.canParallelLFTrials() {
			ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
			filtErrLow, filtErrHigh := ctx.dispatchLFTrialPair(filtLow, filtHigh)
			ssErr[filtLow&63] = filtErrLow
			ssSet[filtLow&63] = true
			ssErr[filtHigh&63] = filtErrHigh
			ssSet[filtHigh&63] = true
			if filtHigh != 0 {
				residentLevel = filtHigh
			}
			if traceEnabled {
				e.emitOracleLFTrial("full", filtLow, filtErrLow)
				e.emitOracleLFTrial("full", filtHigh, filtErrHigh)
			}
			if filtErrLow-bias < bestErr {
				if filtErrLow < bestErr {
					bestErr = filtErrLow
				}
				filtBest = filtLow
				if filtBest > 0 && bestLumaLevel != filtBest {
					vp8common.CopyImageLuma(&e.loopFilterBest.Img, &e.loopFilterPickAlt.Img)
					bestLumaLevel = filtBest
				}
			}
			if filtErrHigh < bestErr-bias {
				bestErr = filtErrHigh
				filtBest = filtHigh
			}
		} else {
			if filtDirection <= 0 && filtLow != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtLow]
				filtErr := ctx.cachedFullLumaSSEStats(filtLow, &ssErr, &ssSet, &residentLevel, stats)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtLow, filtErr)
				}
				if filtErr-bias < bestErr {
					if filtErr < bestErr {
						bestErr = filtErr
					}
					filtBest = filtLow
				}
			}
			if filtDirection >= 0 && filtHigh != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtHigh, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtHigh]
				filtErr := ctx.cachedFullLumaSSEStats(filtHigh, &ssErr, &ssSet, &residentLevel, stats)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtHigh, filtErr)
				}
				if filtErr < bestErr-bias {
					bestErr = filtErr
					filtBest = filtHigh
				}
			}
		}
		if filtBest == filtMid {
			filterStep /= 2
			filtDirection = 0
		} else {
			if filtBest < filtMid {
				filtDirection = -1
			} else {
				filtDirection = 1
			}
			filtMid = filtBest
		}
	}
	if filtBest > 0 {
		switch {
		case residentLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = false
		case bestLumaLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = true
		}
	}
	return uint8(filtBest), nil
}

func (ctx *loopFilterPickContext) preserveBestLoopFilterLumaBeforeTrial(nextLevel int, bestLevel int, ssSet *[vp8common.MaxLoopFilter + 1]bool, residentLevel int, bestLumaLevel *int) {
	if bestLevel <= 0 || nextLevel == bestLevel || ssSet[nextLevel] {
		return
	}
	if residentLevel == bestLevel && *bestLumaLevel != bestLevel {
		vp8common.CopyImageLuma(&ctx.encoder.loopFilterBest.Img, &ctx.encoder.loopFilterPick.Img)
		*bestLumaLevel = bestLevel
	}
}

func (ctx *loopFilterPickContext) cachedPartialLumaSSE(level int, ssErr *[vp8common.MaxLoopFilter + 1]int, ssSet *[vp8common.MaxLoopFilter + 1]bool) int {
	if ssSet[level] {
		return ssErr[level]
	}
	err := ctx.trialLumaSSEPartial(level)
	ssErr[level] = err
	ssSet[level] = true
	return err
}

func (ctx *loopFilterPickContext) cachedPartialLumaSSEStats(level int, ssErr *[vp8common.MaxLoopFilter + 1]int, ssSet *[vp8common.MaxLoopFilter + 1]bool, stats *EncoderPhaseStats) int {
	if ssSet[level] {
		return ssErr[level]
	}
	err := ctx.trialLumaSSEPartialStats(level, stats)
	ssErr[level] = err
	ssSet[level] = true
	return err
}

func (ctx *loopFilterPickContext) cachedFullLumaSSE(level int, ssErr *[vp8common.MaxLoopFilter + 1]int, ssSet *[vp8common.MaxLoopFilter + 1]bool, residentLevel *int) int {
	if ssSet[level] {
		return ssErr[level]
	}
	trialErr := ctx.trialLumaSSEFull(level)
	ssErr[level] = trialErr
	ssSet[level] = true
	if level != 0 {
		*residentLevel = level
	}
	return trialErr
}

func (ctx *loopFilterPickContext) cachedFullLumaSSEStats(level int, ssErr *[vp8common.MaxLoopFilter + 1]int, ssSet *[vp8common.MaxLoopFilter + 1]bool, residentLevel *int, stats *EncoderPhaseStats) int {
	if ssSet[level] {
		return ssErr[level]
	}
	trialErr := ctx.trialLumaSSEFullStats(level, stats)
	ssErr[level] = trialErr
	ssSet[level] = true
	if level != 0 {
		*residentLevel = level
	}
	return trialErr
}

// trialLumaSSE applies the candidate loop-filter level to a copy of
// the analysis image and returns the Y SSE between the source and filtered
// buffer. Even level 0 goes through the trial filter path: libvpx's picker
// compiles out the y-only/partial early return, so mode/ref deltas and
// segmentation can still produce nonzero per-MB levels during scoring.
func (ctx *loopFilterPickContext) trialLumaSSE(level int, partial bool) int {
	e := ctx.encoder
	if vp8PhaseStatsEnabled {
		stats := e.phaseStats()
		if partial {
			if stats != nil {
				return ctx.trialLumaSSEPartialStats(level, stats)
			}
			return ctx.trialLumaSSEPartial(level)
		}
		if stats != nil {
			return ctx.trialLumaSSEFullStats(level, stats)
		}
		return ctx.trialLumaSSEFull(level)
	}
	if partial {
		return ctx.trialLumaSSEPartial(level)
	}
	return ctx.trialLumaSSEFull(level)
}

func (ctx *loopFilterPickContext) trialLumaSSEPartial(level int) int {
	e := ctx.encoder
	startRow, rowCount := vp8enc.LoopFilterPartialFrameWindow(ctx.rows)
	vp8enc.CopyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
	vp8dec.ApplyLoopFilterPartialConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fastFrameConfig, &e.loopInfo, startRow, rowCount)
	return vp8enc.LoopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, true)
}

func (ctx *loopFilterPickContext) trialLumaSSEPartialStats(level int, stats *EncoderPhaseStats) int {
	e := ctx.encoder
	startRow, rowCount := vp8enc.LoopFilterPartialFrameWindow(ctx.rows)
	stats.LoopFilterTrials++
	phase := nanotime()
	vp8enc.CopyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
	stats.LoopFilterTrialCopyNS += nanotime() - phase
	phase = nanotime()
	vp8dec.ApplyLoopFilterPartialConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fastFrameConfig, &e.loopInfo, startRow, rowCount)
	stats.LoopFilterTrialFilterNS += nanotime() - phase
	phase = nanotime()
	err := vp8enc.LoopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, true)
	stats.LoopFilterTrialSSENS += nanotime() - phase
	return err
}

func (ctx *loopFilterPickContext) trialLumaSSEFull(level int) int {
	e := ctx.encoder
	vp8common.CopyImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
	vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fullFrameConfig, &e.loopInfo)
	return vp8enc.LoopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, false)
}

func (ctx *loopFilterPickContext) trialLumaSSEFullStats(level int, stats *EncoderPhaseStats) int {
	e := ctx.encoder
	stats.LoopFilterTrials++
	phase := nanotime()
	vp8common.CopyImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
	stats.LoopFilterTrialCopyNS += nanotime() - phase
	phase = nanotime()
	vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fullFrameConfig, &e.loopInfo)
	stats.LoopFilterTrialFilterNS += nanotime() - phase
	phase = nanotime()
	err := vp8enc.LoopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, false)
	stats.LoopFilterTrialSSENS += nanotime() - phase
	return err
}

func (e *VP8Encoder) libvpxMinLoopFilterLevelForFrame(frameType vp8common.FrameType, refreshGolden bool, refreshAltRef bool) int {
	if frameType == vp8common.InterFrame && e.sourceAltRefActive && refreshGolden && !refreshAltRef {
		return 0
	}
	return vp8enc.LibvpxMinLoopFilterLevel(e.rc.currentQuantizer)
}

func (e *VP8Encoder) libvpxMaxLoopFilterLevelForFrame() int {
	if e.twoPass.sectionIntraRating > 8 {
		return vp8common.MaxLoopFilter * 3 / 4
	}
	return vp8enc.LibvpxMaxLoopFilterLevel(0)
}

func (e *VP8Encoder) applyReconstructionLoopFilter(frameType vp8common.FrameType, header vp8dec.LoopFilterHeader, segmentation vp8enc.SegmentationConfig, rows int, cols int, required int) error {
	if header.Level == 0 {
		e.loopFilterPickReady = false
		e.loopFilterPickBest = false
		return nil
	}
	e.installLoopFilterSegmentLF(segmentation)
	if len(e.reconstructModes) < required {
		return ErrInvalidConfig
	}
	// libvpx vp8_loop_filter_frame_init reads the ALT_LF feature data
	// when cm->segmentation.enabled is set, so the reconstruction-side
	// LF must see the same per-segment deltas the bitstream signals.
	if e.loopFilterPickReady && e.loopFilterPickLevel == header.Level {
		reuse := &e.loopFilterPick.Img
		if e.loopFilterPickBest {
			reuse = &e.loopFilterBest.Img
		}
		vp8common.CopyImageLuma(&e.analysis.Img, reuse)
		if header.Type == vp8dec.NormalLoopFilter {
			if err := e.applyChromaOnlyLoopFilter(rows, cols, required, frameType, header, segmentation); err != nil {
				e.loopFilterPickReady = false
				e.loopFilterPickBest = false
				return ErrInvalidConfig
			}
		}
		e.loopFilterPickReady = false
		e.loopFilterPickBest = false
		e.analysis.ExtendBorders()
		return nil
	}
	e.loopFilterPickReady = false
	e.loopFilterPickBest = false
	if err := vp8dec.ApplyLoopFilter(&e.analysis.Img, rows, cols, e.reconstructModes[:required], frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo); err != nil {
		return ErrInvalidConfig
	}
	e.analysis.ExtendBorders()
	return nil
}

// applyChromaOnlyLoopFilter applies the picker-reuse path's chroma-only
// loop filter in libvpx frame-traversal order. Chroma horizontal edges overlap
// across adjacent MB rows, so this pass must stay serial even when row workers
// are available.
func (e *VP8Encoder) applyChromaOnlyLoopFilter(rows int, cols int, required int, frameType vp8common.FrameType, header vp8dec.LoopFilterHeader, segmentation vp8enc.SegmentationConfig) error {
	return vp8dec.ApplyLoopFilterChromaOnlyPrepared(&e.analysis.Img, rows, cols, e.reconstructModes[:required], frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo)
}
