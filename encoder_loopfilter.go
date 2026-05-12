package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
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
	level := libvpxInitialLoopFilterLevel(e.rc.currentQuantizer)
	if frameType == vp8common.InterFrame {
		level = int(e.loopFilterLevel)
	}
	level = min(libvpxClampLoopFilterLevel(e.rc.currentQuantizer, level), 63)
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
// vp8/encoder/onyx_if.c). In effect, libvpx writes `update=1` on the very
// first packed frame (when last_*_lf_deltas are still the all-zero memset
// from setup_features) and `update=0` thereafter, since the default deltas
// never change at runtime. We mirror that by also re-emitting `update=1`
// when the encoder's chosen deltas drift away from the last-signaled values
// or in error-resilient mode. The "signaled once" gate covers the keyframe
// invariant: until we have packed a frame at all, the deltas have not been
// communicated to the decoder.
func (e *VP8Encoder) computeLFDeltaUpdateBit(deltaEnabled bool, refDeltas [vp8common.MaxRefLFDeltas]int8, modeDeltas [vp8common.MaxModeLFDeltas]int8) bool {
	if !deltaEnabled {
		return false
	}
	if e.opts.ErrorResilient {
		return true
	}
	if !e.lfDeltasSignaledOnce {
		return true
	}
	return refDeltas != e.lastSignaledRefLFDeltas || modeDeltas != e.lastSignaledModeLFDeltas
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
	return e.loopFilterUsesFastSearch()
}

func (e *VP8Encoder) loopFilterUsesFastSearch() bool {
	speed := e.libvpxCPUUsed()
	// The FastLoopFilterPick opt-in drops the partial-frame picker gate
	// to speed >= 4 so the libvpx-cold-start realtime+positive-cpu_used
	// case (sf->RD = 0 at speed=4, never bumped on short corpora) stops
	// burning ~25% of EncodeInto on 5 full-frame loop-filter trials per
	// frame. Default off so the gate exactly mirrors libvpx (sf->RD
	// switches at speed > 4 for good-quality, speed == 3 || > 4 for
	// realtime).
	if e.opts.FastLoopFilterPick {
		switch e.opts.Deadline {
		case DeadlineGoodQuality:
			return speed >= 4
		case DeadlineRealtime:
			return speed == 3 || speed >= 4
		default:
			return false
		}
	}
	switch e.opts.Deadline {
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
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	score := func(level int) int {
		if ssSet[level] {
			return ssErr[level]
		}
		err := ctx.trialLumaSSE(level, true)
		ssErr[level] = err
		ssSet[level] = true
		return err
	}
	level := clampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	bestErr := score(level)
	if traceEnabled {
		e.emitOracleLFTrial("seed", level, bestErr)
	}

	filtLevel := level - loopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := score(filtLevel)
		if traceEnabled {
			e.emitOracleLFTrial("down", filtLevel, filtErr)
		}
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= loopFilterSearchStep(filtLevel)
	}

	filtLevel = level + loopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := score(filtLevel)
			if traceEnabled {
				e.emitOracleLFTrial("up", filtLevel, filtErr)
			}
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += loopFilterSearchStep(filtLevel)
		}
	}
	return uint8(clampLoopFilterPickLevel(bestLevel, minLevel, maxLevel)), nil
}

func (ctx *loopFilterPickContext) pickFull(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if ctx.fullFrameConfig.SegmentationEnabled {
		e.loopFilterSegmentLF = ctx.fullFrameConfig.SegmentLF
	}
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	filtMid := clampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	residentLevel := -1
	bestLumaLevel := -1
	preserveBestBeforeTrial := func(nextLevel int, bestLevel int) {
		if bestLevel <= 0 || nextLevel == bestLevel || ssSet[nextLevel] {
			return
		}
		if residentLevel == bestLevel && bestLumaLevel != bestLevel {
			copyFrameImageLuma(&e.loopFilterBest.Img, &e.loopFilterPick.Img)
			bestLumaLevel = bestLevel
		}
	}
	score := func(level int) int {
		if ssSet[level] {
			return ssErr[level]
		}
		trialErr := ctx.trialLumaSSE(level, false)
		ssErr[level] = trialErr
		ssSet[level] = true
		if level != 0 {
			residentLevel = level
		}
		if traceEnabled {
			e.emitOracleLFTrial("full", level, trialErr)
		}
		return trialErr
	}

	bestErr := score(filtMid)
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		// Mirror libvpx vp8/encoder/picklpf.c vp8cx_pick_filter_level
		// (Bias = (best_err >> (15 - (filt_mid / 8))) * filter_step). The
		// shift saturates at zero (filt_mid/8 >= 15 only when filt_mid >=
		// 120, which is above MAX_LOOP_FILTER=63), so it always preserves
		// some bias against raising the filter level. govpx's full picker
		// previously hard-coded bias=0, which silently dropped libvpx's
		// "prefer lower filter level" tie-breaker and steered the picker
		// to a different filt_best on inter frames where multiple trials
		// score within the bias delta of best_err (e.g. the 128x128 panning
		// CBR cpu8 fixture frame 1: govpx picked level 11, libvpx 5).
		bias := loopFilterFullPickerBias(bestErr, filtMid, filterStep, e.twoPass.sectionIntraRating)
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
		if filtDirection == 0 && filtLow != filtMid && filtHigh != filtMid &&
			!ssSet[filtLow] && !ssSet[filtHigh] && e.canParallelLFTrials() {
			preserveBestBeforeTrial(filtLow, filtBest)
			filtErrLow, filtErrHigh := ctx.dispatchLFTrialPair(filtLow, filtHigh)
			ssErr[filtLow] = filtErrLow
			ssSet[filtLow] = true
			ssErr[filtHigh] = filtErrHigh
			ssSet[filtHigh] = true
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
					copyFrameImageLuma(&e.loopFilterBest.Img, &e.loopFilterPickAlt.Img)
					bestLumaLevel = filtBest
				}
			}
			if filtErrHigh < bestErr-bias {
				bestErr = filtErrHigh
				filtBest = filtHigh
			}
		} else {
			if filtDirection <= 0 && filtLow != filtMid {
				preserveBestBeforeTrial(filtLow, filtBest)
				filtErr := score(filtLow)
				if filtErr-bias < bestErr {
					if filtErr < bestErr {
						bestErr = filtErr
					}
					filtBest = filtLow
				}
			}
			if filtDirection >= 0 && filtHigh != filtMid {
				preserveBestBeforeTrial(filtHigh, filtBest)
				filtErr := score(filtHigh)
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

// loopFilterFullPickerBias mirrors libvpx vp8/encoder/picklpf.c
// vp8cx_pick_filter_level's `Bias = (best_err >> (15 - (filt_mid / 8))) *
// filter_step;` followed by `if (section_intra_rating < 20) Bias = Bias *
// section_intra_rating / 20`. The shift amount is `15 - filt_mid/8`. For
// filt_mid in [0, 63] the shift ranges [8, 15].
//
// Critically, libvpx's twopass.section_intra_rating is in the cpi->twopass
// struct which is calloc'd; in one-pass / realtime / CBR encodes it is
// never written so it stays at 0. The unconditional VP8 guard
// `if (section_intra_rating < 20) Bias = Bias * section_intra_rating / 20;`
// then forces Bias = 0 every iteration of the full picker. (VP9's analogue
// adds an `oxcf.pass == 2` predicate, but VP8 does not — the two-pass
// guard is implicit via the zero default.) Mirroring govpx's previous
// "fall through and use unscaled bias" behaviour caused the realtime CBR
// full picker at q=17 / prev_lf=5 to converge on a different filt_best
// than libvpx because the nonzero bias rejected high-side trials that
// libvpx accepted, and accepted low-side trials that libvpx rejected. The
// `section_intra_rating` argument is the integer computed by libvpx's
// `section_intra_rating = section_intra_error / section_coded_error` (or
// 0 in one-pass).
func loopFilterFullPickerBias(bestErr int, filtMid int, filterStep int, sectionIntraRating int) int {
	shift := max(15-(filtMid/8), 0)
	bias := (bestErr >> uint(shift)) * filterStep
	if sectionIntraRating < 20 {
		bias = bias * sectionIntraRating / 20
	}
	return bias
}

// trialLumaSSE applies the candidate loop-filter level to a copy of
// the analysis image and returns the Y SSE between the source and filtered
// buffer. Even level 0 goes through the trial filter path: libvpx's picker
// compiles out the y-only/partial early return, so mode/ref deltas and
// segmentation can still produce nonzero per-MB levels during scoring.
func (ctx *loopFilterPickContext) trialLumaSSE(level int, partial bool) int {
	e := ctx.encoder
	stats := e.opts.PhaseStats
	if partial {
		startRow, rowCount := loopFilterPartialFrameWindow(ctx.rows)
		if stats != nil {
			stats.LoopFilterTrials++
			phase := nanotime()
			copyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
			stats.LoopFilterTrialCopyNS += nanotime() - phase
			phase = nanotime()
			vp8dec.ApplyLoopFilterPartialConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fastFrameConfig, &e.loopInfo, startRow, rowCount)
			stats.LoopFilterTrialFilterNS += nanotime() - phase
			phase = nanotime()
			err := loopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, true)
			stats.LoopFilterTrialSSENS += nanotime() - phase
			return err
		}
		copyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
		vp8dec.ApplyLoopFilterPartialConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fastFrameConfig, &e.loopInfo, startRow, rowCount)
		return loopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, true)
	}
	if stats != nil {
		stats.LoopFilterTrials++
		phase := nanotime()
		copyFrameImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
		stats.LoopFilterTrialCopyNS += nanotime() - phase
		phase = nanotime()
		vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fullFrameConfig, &e.loopInfo)
		stats.LoopFilterTrialFilterNS += nanotime() - phase
		phase = nanotime()
		err := loopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, false)
		stats.LoopFilterTrialSSENS += nanotime() - phase
		return err
	}
	copyFrameImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
	vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(&e.loopFilterPick.Img, ctx.rows, ctx.cols, ctx.modes, ctx.frameType, ctx.filterType, level, ctx.fullFrameConfig, &e.loopInfo)
	return loopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, false)
}

// copyLoopFilterPartialLuma refreshes the luma plane window the partial-frame
// loop-filter trial reads. It mirrors libvpx's yv12_copy_partial_frame:
// copy from ((y_height >> 5) * 16) - 4 for rowCount MB rows plus the 4
// luma context lines above, filling negative top-context rows from the
// visible top row.
func copyLoopFilterPartialLuma(dst *vp8common.Image, src *vp8common.Image, startRow int, rowCount int) {
	if rowCount <= 0 {
		return
	}
	startY := startRow*16 - 4
	lineCount := rowCount*16 + 4
	if lineCount <= 0 {
		return
	}
	if src.YStride == dst.YStride && len(src.YFull) > 0 && len(dst.YFull) > 0 {
		// libvpx yv12_copy_partial_frame copies y_stride bytes from the
		// visible-origin row, preserving right-border/stride bytes used by
		// vp8_loop_filter_partial_frame.
		topOff := src.YOrigin
		srcOff := src.YOrigin + startY*src.YStride
		dstOff := dst.YOrigin + startY*dst.YStride
		for dstOff < dst.YOrigin && lineCount > 0 {
			// Uint range collapses (off<0) + (off+stride > len) into one
			// compare per buffer. Equivalent to uint(off) > uint(len-stride).
			if uint(topOff) > uint(len(src.YFull)-src.YStride) || uint(dstOff) > uint(len(dst.YFull)-dst.YStride) {
				return
			}
			copy(dst.YFull[dstOff:dstOff+dst.YStride], src.YFull[topOff:topOff+src.YStride])
			srcOff += src.YStride
			dstOff += dst.YStride
			lineCount--
		}
		n := lineCount * src.YStride
		// Uint range collapses (srcOff/dstOff >= 0) + (srcOff/dstOff+n <= len)
		// into one compare each on the two buffer dimensions. n is
		// guaranteed >= 0 by the lineCount > 0 guard and YStride > 0.
		if lineCount > 0 && uint(srcOff) <= uint(len(src.YFull)-n) && uint(dstOff) <= uint(len(dst.YFull)-n) {
			copy(dst.YFull[dstOff:dstOff+n], src.YFull[srcOff:srcOff+n])
		}
		return
	}
	width := min(dst.CodedWidth, src.CodedWidth)
	startVisibleY := max(startY, 0)
	endVisibleY := min(min(startY+lineCount, src.CodedHeight), dst.CodedHeight)
	if endVisibleY <= startVisibleY {
		return
	}
	if src.YStride == dst.YStride && width == src.YStride {
		// Fast path: contiguous copy when strides and full coded width match.
		copy(dst.Y[startVisibleY*dst.YStride:endVisibleY*dst.YStride], src.Y[startVisibleY*src.YStride:endVisibleY*src.YStride])
		return
	}
	for row := startVisibleY; row < endVisibleY; row++ {
		copy(dst.Y[row*dst.YStride:row*dst.YStride+width], src.Y[row*src.YStride:row*src.YStride+width])
	}
}

// calcKeyFrameSSError ports libvpx vp8/encoder/onyx_if.c vp8_calc_ss_err over
// the Y plane: full-frame sum of squared 16x16 luma differences between the
// encoded source and the reconstructed frame. Used by the forced-key recode
// branch to compare against ambient_err.
func calcKeyFrameSSError(src vp8enc.SourceImage, recon *vp8common.Image, rows int, cols int) int {
	if rows <= 0 || cols <= 0 {
		return 0
	}
	return loopFilterLumaSSE(src, recon, rows, cols, false)
}

func loopFilterLumaSSE(src vp8enc.SourceImage, img *vp8common.Image, rows int, cols int, partial bool) int {
	startRow, rowCount := 0, rows
	if partial {
		startRow, rowCount = loopFilterPartialFrameWindow(rows)
	}
	total := 0
	srcY := src.Y
	imgY := img.Y
	srcStride := src.YStride
	imgStride := img.YStride
	srcW := src.Width
	srcH := src.Height
	imgW := img.CodedWidth
	imgH := img.CodedHeight
	if cols > 0 && rows > 0 && cols*16 <= srcW && cols*16 <= imgW {
		height := rowCount * 16
		if startRow >= 0 && height > 0 && (startRow+rowCount)*16 <= srcH && (startRow+rowCount)*16 <= imgH {
			srcRowOff := startRow * 16 * srcStride
			imgRowOff := startRow * 16 * imgStride
			for mbCol := range cols {
				baseX := mbCol * 16
				total += dsp.SSE16xNPtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride, height)
			}
			return total
		}
	}
	// Pre-compute the column gating for the hot row (every MB in a fully
	// in-bounds row is covered by the SSE16x16PtrFast SIMD-bypass path).
	// 1280x720 / 1920x1080 / aligned-width frames pass this check for
	// every column, so the inner loop collapses to a tight call sequence.
	colsAllAligned := cols > 0 && cols*16 <= srcW && cols*16 <= imgW
	for mbRow := startRow; mbRow < startRow+rowCount && mbRow < rows; mbRow++ {
		baseY := mbRow * 16
		// Hoist the per-row Y bounds + base offset out of the column loop;
		// once baseY clears the row check, every MB on the row uses the
		// same vertical clearance.
		if baseY+16 <= srcH && baseY+16 <= imgH {
			srcRowOff := baseY * srcStride
			imgRowOff := baseY * imgStride
			if colsAllAligned {
				// Hot path: every MB on the row is fully in-bounds for
				// both src and img — no per-column bounds check needed.
				for mbCol := range cols {
					baseX := mbCol * 16
					total += dsp.SSE16x16PtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride)
				}
				continue
			}
			for mbCol := range cols {
				baseX := mbCol * 16
				if baseX+16 <= srcW && baseX+16 <= imgW {
					total += dsp.SSE16x16PtrFast(&srcY[srcRowOff+baseX], srcStride, &imgY[imgRowOff+baseX], imgStride)
					continue
				}
				total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
			}
			continue
		}
		for mbCol := range cols {
			baseX := mbCol * 16
			total += loopFilterLumaBlockSSE(src, img, baseY, baseX)
		}
	}
	return total
}

func loopFilterLumaBlockSSE(src vp8enc.SourceImage, img *vp8common.Image, baseY int, baseX int) int {
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		imgY := clampEncodeCoord(baseY+row, img.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			imgX := clampEncodeCoord(baseX+col, img.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(img.Y[imgY*img.YStride+imgX])
			sse += diff * diff
		}
	}
	return sse
}

func loopFilterPartialFrameWindow(rows int) (int, int) {
	if rows <= 0 {
		return 0, 0
	}
	start := rows / 2
	count := rows / vp8common.PartialFrameFraction
	count = min(max(count, 1), rows-start)
	return start, count
}

func loopFilterSearchStep(level int) int {
	if level > 10 {
		return 2
	}
	return 1
}

func clampLoopFilterPickLevel(level int, minLevel int, maxLevel int) int {
	return min(max(level, minLevel), maxLevel)
}

func libvpxClampLoopFilterLevel(qIndex int, level int) int {
	return min(max(level, libvpxMinLoopFilterLevel(qIndex)), libvpxMaxLoopFilterLevel(qIndex))
}

func libvpxMinLoopFilterLevel(qIndex int) int {
	if qIndex <= 6 {
		return 0
	}
	if qIndex <= 16 {
		return 1
	}
	return qIndex / 8
}

func (e *VP8Encoder) libvpxMinLoopFilterLevelForFrame(frameType vp8common.FrameType, refreshGolden bool, refreshAltRef bool) int {
	if frameType == vp8common.InterFrame && e.sourceAltRefActive && refreshGolden && !refreshAltRef {
		return 0
	}
	return libvpxMinLoopFilterLevel(e.rc.currentQuantizer)
}

func libvpxMaxLoopFilterLevel(qIndex int) int {
	_ = qIndex
	return 63
}

func (e *VP8Encoder) libvpxMaxLoopFilterLevelForFrame() int {
	if e.twoPass.sectionIntraRating > 8 {
		return vp8common.MaxLoopFilter * 3 / 4
	}
	return libvpxMaxLoopFilterLevel(0)
}

func libvpxInitialLoopFilterLevel(qIndex int) int {
	if qIndex <= 0 {
		return 0
	}
	level := qIndex * 3 / 8
	if level > 63 {
		return 63
	}
	return level
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
		copyFrameImageLuma(&e.analysis.Img, reuse)
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

// applyChromaOnlyLoopFilter dispatches the picker-reuse path's chroma-only
// loop-filter apply. At Threads=1 (e.rowWorkers == nil) the call collapses to
// the canonical serial vp8dec.ApplyLoopFilterChromaOnlyPrepared so no atomic
// loads, channel ops, or goroutine spawns appear on the single-threaded hot
// path — keeping the zero-alloc, byte-identical contract. At Threads>=2 the
// per-row chroma apply is partitioned across the row worker pool. Per-row
// independence: chroma writes for MB row R touch chroma lines R*8-2..R*8+5,
// while row R+1's MB top edge reads R*8+6..R*8+13, so row stripes can run in
// parallel without a wave-front barrier.
func (e *VP8Encoder) applyChromaOnlyLoopFilter(rows int, cols int, required int, frameType vp8common.FrameType, header vp8dec.LoopFilterHeader, segmentation vp8enc.SegmentationConfig) error {
	if e.rowWorkers == nil {
		return vp8dec.ApplyLoopFilterChromaOnlyPrepared(&e.analysis.Img, rows, cols, e.reconstructModes[:required], frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo)
	}
	return e.applyChromaOnlyLoopFilterThreaded(rows, cols, required, frameType, header, segmentation)
}
