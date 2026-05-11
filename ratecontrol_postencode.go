package govpx

func (rc *rateControlState) postEncodeFrame(sizeBytes int, keyFrame bool) {
	rc.postEncodeFrameWithContext(sizeBytes, keyFrame, false, 0)
}

func (rc *rateControlState) postEncodeFrameWithContext(sizeBytes int, keyFrame bool, goldenFrame bool, macroblocks int) {
	rc.postEncodeFrameWithPacketContext(sizeBytes, rateControlPostEncodeContext{
		keyFrame:    keyFrame,
		goldenFrame: goldenFrame,
		macroblocks: macroblocks,
		showFrame:   true,
	})
}

type rateControlPostEncodeContext struct {
	keyFrame              bool
	goldenFrame           bool
	altRefFrame           bool
	macroblocks           int
	showFrame             bool
	skipPostPackOverspend bool
	alwaysUpdateFactor    bool
}

func (rc *rateControlState) postEncodeFrameWithPacketContext(sizeBytes int, ctx rateControlPostEncodeContext) {
	actualBits := encodedSizeBits(sizeBytes)
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	boostedReferenceFrame := ctx.goldenFrame || ctx.altRefFrame
	if ctx.alwaysUpdateFactor || !rc.activeWorstQChanged {
		rc.updateRateCorrectionFactor(actualBits, ctx.keyFrame, boostedReferenceFrame, ctx.macroblocks)
	}
	rc.activeWorstQChanged = false
	rc.updateRollingBitAverages(actualBits, targetBits)
	if ctx.showFrame {
		rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	}
	rc.bufferLevelBits = saturatingSub(rc.bufferLevelBits, actualBits)
	rc.clampBuffer()
	if actualBits > 0 {
		const maxInt64 = int64(^uint64(0) >> 1)
		if rc.totalActualBits > maxInt64-int64(actualBits) {
			rc.totalActualBits = maxInt64
		} else {
			rc.totalActualBits += int64(actualBits)
		}
	}

	// libvpx vp8/encoder/ratectrl.c vp8_adjust_key_frame_context and
	// onyx_if.c update_golden_frame_stats / update_alt_ref_frame_stats
	// accumulate post-pack overspend before the next frame's
	// calc_pframe_target_size runs. Pass2 skips this one-pass bookkeeping.
	if !ctx.skipPostPackOverspend {
		if ctx.altRefFrame && !ctx.keyFrame {
			rc.accumulatePostPackAltRefOverspend(actualBits)
		} else {
			rc.accumulatePostPackOverspend(actualBits, ctx.keyFrame, ctx.goldenFrame)
		}
	}

	encodedQuantizer := rc.currentQuantizer
	rc.lastQuantizer = encodedQuantizer
	if !ctx.keyFrame {
		rc.lastInterQuantizer = encodedQuantizer
	}
	if rc.mode == RateControlCQ {
		rc.adjustCQQuantizerWithContext(actualBits, targetBits, ctx.keyFrame, boostedReferenceFrame)
	} else {
		rc.adjustQuantizerWithContext(actualBits, targetBits, ctx.keyFrame, boostedReferenceFrame)
	}
	rc.clampQuantizer()

	rc.updateQuantizerAverages(encodedQuantizer, ctx.keyFrame, boostedReferenceFrame)
	if ctx.keyFrame {
		rc.framesSinceKeyframe = 0
		rc.framesSinceGolden = 0
		return
	}
	if !ctx.showFrame {
		return
	}
	rc.framesSinceKeyframe++
	if ctx.goldenFrame || ctx.altRefFrame {
		rc.framesSinceGolden = 0
	} else {
		rc.framesSinceGolden++
		if rc.framesTillGFUpdateDue > 0 {
			rc.framesTillGFUpdateDue--
		}
	}
}

// accumulatePostPackOverspend ports libvpx's post-pack overspend
// bookkeeping. For key frames it mirrors vp8_adjust_key_frame_context: when
// the projected (encoded) size exceeds per_frame_bandwidth, 7/8 of the
// overspend is accumulated into kf_overspend_bits and 1/8 into
// gf_overspend_bits (single-layer); kf_bitrate_adjustment is the per-frame
// drain rate computed from estimate_keyframe_frequency. For golden refreshes
// it mirrors update_golden_frame_stats: overspend relative to
// inter_frame_target accumulates into gf_overspend_bits and
// non_gf_bitrate_adjustment is the per-frame drain rate over the next GF
// interval.
func (rc *rateControlState) accumulatePostPackOverspend(actualBits int, keyFrame bool, goldenFrame bool) {
	perFrameBandwidth := rc.bitsPerFrame
	if perFrameBandwidth <= 0 {
		return
	}
	if keyFrame {
		rc.keyFrameCount++
		if actualBits > perFrameBandwidth {
			overspend := actualBits - perFrameBandwidth
			if rc.currentTemporalLayers > 1 {
				rc.kfOverspendBits = saturatingAdd(rc.kfOverspendBits, overspend)
			} else {
				rc.kfOverspendBits = saturatingAdd(rc.kfOverspendBits, overspend*7/8)
				rc.gfOverspendBits = saturatingAdd(rc.gfOverspendBits, overspend/8)
			}
			kfFreq := rc.estimateKeyFrameFrequency()
			if kfFreq <= 0 {
				kfFreq = 1
			}
			rc.kfBitrateAdjustment = rc.kfOverspendBits / kfFreq
			if rc.framesTillGFUpdateDue > 0 {
				rc.nonGFBitrateAdjustment = rc.gfOverspendBits / rc.framesTillGFUpdateDue
			}
		}
		return
	}
	if !goldenFrame {
		return
	}
	// libvpx onyx_if.c update_golden_frame_stats: only accumulate gf
	// overspend on non-key non-altref-active golden refreshes. govpx's
	// CBR oracle does not currently model an active alt-ref, so treat
	// every golden refresh as the non-altref case (matches libvpx
	// behaviour when source_alt_ref_active is 0).
	interTarget := rc.interFrameTarget
	if interTarget <= 0 {
		interTarget = perFrameBandwidth
	}
	if actualBits > interTarget {
		rc.gfOverspendBits = saturatingAdd(rc.gfOverspendBits, actualBits-interTarget)
	}
	if rc.framesTillGFUpdateDue > 0 {
		rc.nonGFBitrateAdjustment = rc.gfOverspendBits / rc.framesTillGFUpdateDue
	}
}

// accumulatePostPackAltRefOverspend ports the libvpx
// vp8/encoder/onyx_if.c update_alt_ref_frame_stats overspend branch.
// Unlike update_golden_frame_stats (which accumulates `projected_frame_size
// - inter_frame_target` because the GF refresh shares the section bandwidth
// with the following p-frames), update_alt_ref_frame_stats accumulates the
// full `projected_frame_size` because the ARF is hidden and the show frames
// after it pay separately. The non_gf_bitrate_adjustment update is the
// same drain-rate computation
// `gf_overspend_bits / frames_till_gf_update_due`.
//
// Caller must have already set rc.framesTillGFUpdateDue to the next
// section length (libvpx's `if (!auto_gold) frames_till_gf_update_due
// = DEFAULT_GF_INTERVAL` is the encoder-side default).
func (rc *rateControlState) accumulatePostPackAltRefOverspend(actualBits int) {
	if actualBits <= 0 {
		return
	}
	rc.gfOverspendBits = saturatingAdd(rc.gfOverspendBits, actualBits)
	if rc.framesTillGFUpdateDue > 0 {
		rc.nonGFBitrateAdjustment = rc.gfOverspendBits / rc.framesTillGFUpdateDue
	}
}

// estimateKeyFrameFrequency ports vp8/encoder/ratectrl.c
// estimate_keyframe_frequency: a weighted average of the last
// KEY_FRAME_CONTEXT key-frame distances (weights 1..5), with the
// key_frame_count == 1 bootstrap returning 1 + 2*output_framerate, clamped
// to key_frame_frequency only when auto-key is active.
func (rc *rateControlState) estimateKeyFrameFrequency() int {
	if rc.keyFrameCount == 1 {
		avg := 1 + rc.outputFrameRate*2
		if avg <= 0 {
			avg = 1
		}
		if rc.keyFrameFrequency > 0 {
			// libvpx only clamps the two-second bootstrap to key_freq when
			// automatic key-frame detection is active.
			if rc.autoKeyFrames && avg > rc.keyFrameFrequency {
				avg = rc.keyFrameFrequency
			}
		}
		rc.priorKeyFrameDistance[keyFrameContextSize-1] = avg
		return avg
	}
	last := rc.framesSinceKeyframe
	if last <= 0 {
		last = 1
	}
	totalWeight := 0
	avg := 0
	for i := range keyFrameContextSize {
		if i < keyFrameContextSize-1 {
			rc.priorKeyFrameDistance[i] = rc.priorKeyFrameDistance[i+1]
		} else {
			rc.priorKeyFrameDistance[i] = last
		}
		avg += libvpxPriorKeyFrameWeight[i] * rc.priorKeyFrameDistance[i]
		totalWeight += libvpxPriorKeyFrameWeight[i]
	}
	if totalWeight > 0 {
		avg /= totalWeight
	}
	if avg < 1 {
		avg = 1
	}
	return avg
}

func libvpxLimitCBRInterQuantizerDrop(lastInterQuantizer int, currentQuantizer int) int {
	const limitDown = 12
	if lastInterQuantizer-currentQuantizer > limitDown {
		return lastInterQuantizer - limitDown
	}
	return currentQuantizer
}

func (rc *rateControlState) clampScreenContentBufferDebt(screenContentMode int) {
	if screenContentMode <= 0 || rc.maximumBufferBits <= 0 {
		return
	}
	minimumBuffer := -rc.maximumBufferBits
	if rc.bufferLevelBits < minimumBuffer {
		rc.bufferLevelBits = minimumBuffer
	}
}

func (rc *rateControlState) updateQuantizerAverages(q int, keyFrame bool, goldenFrame bool) {
	if q < 0 {
		return
	}
	if !keyFrame {
		if rc.avgFrameQuantizer <= 0 {
			rc.avgFrameQuantizer = rc.maxQuantizer
		}
		rc.avgFrameQuantizer = (2 + 3*rc.avgFrameQuantizer + q) >> 2
	}
	if keyFrame || goldenFrame {
		return
	}
	rc.normalInterFrames++
	if rc.normalInterFrames <= 0 {
		rc.normalInterFrames = maxInt()
	}
	rc.normalInterQuantizerTotal = saturatingAdd(rc.normalInterQuantizerTotal, q)
	if rc.normalInterFrames > 150 {
		rc.normalInterAvgQuantizer = rc.normalInterQuantizerTotal / rc.normalInterFrames
	} else {
		rc.normalInterAvgQuantizer = ((rc.normalInterQuantizerTotal / rc.normalInterFrames) + rc.maxQuantizer + 1) / 2
	}
	if q > rc.normalInterAvgQuantizer {
		rc.normalInterAvgQuantizer = q - 1
	}
}

func (rc *rateControlState) updateRateCorrectionFactor(actualBits int, keyFrame bool, goldenFrame bool, macroblocks int) {
	if actualBits <= 0 || macroblocks <= 0 {
		return
	}
	if !rateControlModeUsesQuantizerRegulator(rc.mode) {
		return
	}
	q := rc.currentQuantizer
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	if q < 0 || q >= len(libvpxBitsPerMB[frameType]) {
		return
	}
	rateCorrectionFactor := rc.rateCorrectionFactorForFrame(keyFrame, goldenFrame)
	if rateCorrectionFactor <= 0 {
		rateCorrectionFactor = 1.0
	}
	projectedBits := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, rateCorrectionFactor, rc.currentZbinOverQuant)
	if projectedBits <= 0 {
		return
	}
	correctionFactor := int((100 * int64(actualBits)) / int64(projectedBits))
	const finalPackAdjustmentLimit = 0.25
	switch {
	case correctionFactor > 102:
		correctionFactor = int(100.5 + float64(correctionFactor-100)*finalPackAdjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor > libvpxMaxBPBFactor {
			rateCorrectionFactor = libvpxMaxBPBFactor
		}
	case correctionFactor < 99:
		correctionFactor = int(100.5 - float64(100-correctionFactor)*finalPackAdjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor < libvpxMinBPBFactor {
			rateCorrectionFactor = libvpxMinBPBFactor
		}
	}
	rc.setRateCorrectionFactorForFrame(keyFrame, goldenFrame, rateCorrectionFactor)
}

func (rc *rateControlState) rateCorrectionFactorForFrame(keyFrame bool, goldenFrame bool) float64 {
	if keyFrame {
		return normalizedRateCorrectionFactor(rc.keyFrameCorrectionFactor)
	}
	if rc.usesGoldenFrameCorrectionFactor(goldenFrame) {
		return normalizedRateCorrectionFactor(rc.goldenCorrectionFactor)
	}
	return normalizedRateCorrectionFactor(rc.rateCorrectionFactor)
}

func normalizedRateCorrectionFactor(factor float64) float64 {
	if factor <= 0 {
		return 1.0
	}
	return factor
}

func (rc *rateControlState) setRateCorrectionFactorForFrame(keyFrame bool, goldenFrame bool, factor float64) {
	if keyFrame {
		rc.keyFrameCorrectionFactor = factor
		return
	}
	if rc.usesGoldenFrameCorrectionFactor(goldenFrame) {
		rc.goldenCorrectionFactor = factor
		return
	}
	rc.rateCorrectionFactor = factor
}

func (rc *rateControlState) usesGoldenFrameCorrectionFactor(goldenFrame bool) bool {
	if !goldenFrame {
		return false
	}
	return rc.mode != RateControlCBR || rc.gfCBRBoostPct > 100
}

func (rc *rateControlState) shouldDropInterFrame() bool {
	if !rc.dropFrameAllowed || rc.mode != RateControlCBR {
		return false
	}
	return rc.bufferLevelBits < 0
}

func (rc *rateControlState) postDropFrame() {
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
	if rc.frameDropPressure > 0 {
		rc.frameDropPressure--
	}
	rc.framesSinceKeyframe++
}

// prepareDecimationForFrame mirrors the decimation-factor adjustment ladder
// at the head of libvpx vp8/encoder/onyx_if.c vp8_check_drop_buffer. It
// inspects the current cpi->buffer_level against the configured drop_mark
// percentages and bumps cpi->decimation_factor 0->1->2->3 (or back down) in
// lockstep with libvpx. The drop *decision* (decimation_count gating) lives
// in checkDropBuffer below; we split the two so the per_frame_bandwidth
// boost that libvpx applies INSIDE vp8_check_drop_buffer (1->3/2, 2->5/4,
// 3->5/4) can be propagated into the begin-frame target before
// vp8_pick_frame_size / vp8_regulate_q runs. Without that boost, govpx's
// post-decimation-drop frames see a stale (un-boosted) frame_target and
// regulate Q ~16-24 indices higher than libvpx, which propagates further
// drops on subsequent frames.
//
// Safe to call once per frame (keyframe or inter); does not mutate the
// decimation_count or take a drop decision.
func (rc *rateControlState) prepareDecimationForFrame() {
	if rc.mode != RateControlCBR || !rc.dropFrameAllowed {
		return
	}
	dropMarkBits := rc.dropMarkBits()
	dropMark75 := dropMarkBits * 2 / 3
	dropMark50 := dropMarkBits / 4
	dropMark25 := dropMarkBits / 8
	bl := rc.bufferLevelBits
	if bl > dropMarkBits && rc.decimationFactor > 0 {
		rc.decimationFactor--
	}
	if bl > dropMark75 && rc.decimationFactor > 0 {
		rc.decimationFactor = 1
	} else if bl < dropMark25 && (rc.decimationFactor == 2 || rc.decimationFactor == 3) {
		rc.decimationFactor = 3
	} else if bl < dropMark50 && (rc.decimationFactor == 1 || rc.decimationFactor == 2) {
		rc.decimationFactor = 2
	} else if bl < dropMark75 && (rc.decimationFactor == 0 || rc.decimationFactor == 1) {
		rc.decimationFactor = 1
	}
}

// decimationBoostedBitsPerFrame returns rc.bitsPerFrame multiplied by the
// libvpx vp8_check_drop_buffer per_frame_bandwidth boost that fires when
// decimation_factor>0:
//
//	decimation_factor=1: 3/2
//	decimation_factor=2: 5/4
//	decimation_factor=3: 5/4
//
// The boost mirrors libvpx's pre-pick-frame-size mutation of
// cpi->per_frame_bandwidth so that the begin-frame target consumed by
// calc_pframe_target_size / vp8_regulate_q on a frame following a
// decimation drop matches libvpx's. Returns rc.bitsPerFrame unchanged when
// CBR drops are disabled or decimation_factor==0.
func (rc *rateControlState) decimationBoostedBitsPerFrame() int {
	base := rc.bitsPerFrame
	if base <= 0 {
		return base
	}
	if rc.mode != RateControlCBR || !rc.dropFrameAllowed {
		return base
	}
	switch rc.decimationFactor {
	case 1:
		return base * 3 / 2
	case 2, 3:
		return base * 5 / 4
	default:
		return base
	}
}

// checkDropBuffer mirrors libvpx vp8/encoder/onyx_if.c vp8_check_drop_buffer.
// The factor-adjustment portion has been split into prepareDecimationForFrame
// (which the encoder calls earlier so the per_frame_bandwidth boost can flow
// into the begin-frame target). This entry point assumes the factor is up to
// date and only handles the drop decision.
//
// The boolean return mirrors libvpx's "1 = dropped". When true, the caller
// must perform the post-drop accounting (refund av_per_frame_bandwidth +
// clamp to maximum_buffer_size) just like libvpx does inline; expressed
// here as `postDecimationDropFrame` for a clean single-call drop branch.
//
// keyFrame matches libvpx's `cm->frame_type == KEY_FRAME` early-out: keys
// are never dropped, but the count is still seeded so the next inter frame
// honors the decimation pattern. This keeps the count/factor lifecycle
// independent of which frame ultimately fired the buffer-test.
func (rc *rateControlState) checkDropBuffer(keyFrame bool) bool {
	if rc.mode != RateControlCBR || !rc.dropFrameAllowed {
		// Match libvpx's else branch: when drop_frames_allowed is false,
		// reset decimation_count so a later allow-toggle doesn't honor a
		// stale count.
		rc.decimationCount = 0
		return false
	}
	if rc.decimationFactor <= 0 {
		// Match libvpx's else branch (cpi->decimation_count = 0).
		rc.decimationCount = 0
		return false
	}
	if keyFrame {
		// Key frames are never dropped via decimation; refresh the
		// count so the next inter respects the pattern.
		rc.decimationCount = rc.decimationFactor
		return false
	}
	if rc.decimationCount > 0 {
		rc.decimationCount--
		return true
	}
	rc.decimationCount = rc.decimationFactor
	return false
}

// postDecimationDropFrame commits the buffer accounting libvpx applies
// inside vp8_check_drop_buffer when the function decides to drop:
//
//	cpi->bits_off_target += cpi->av_per_frame_bandwidth;
//	if (cpi->bits_off_target > cpi->oxcf.maximum_buffer_size)
//	    cpi->bits_off_target = cpi->oxcf.maximum_buffer_size;
//	cpi->buffer_level = cpi->bits_off_target;
//
// govpx tracks bufferLevelBits as the equivalent of cpi->bits_off_target
// (the post-encode running buffer balance); the saturating refund matches
// libvpx's clamp.
func (rc *rateControlState) postDecimationDropFrame() {
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
	rc.framesSinceKeyframe++
}

// dropMarkBits returns libvpx's cpi->oxcf.drop_frames_water_mark *
// optimal_buffer_level / 100, expressed in bits to align with govpx's
// bufferLevelBits unit. When the water mark is unset (allow_df=false on
// libvpx), it returns 0 so all four ladder comparisons collapse to "no
// decimation".
func (rc *rateControlState) dropMarkBits() int {
	if rc.dropFramesWaterMark <= 0 {
		return 0
	}
	return rc.bufferOptimalBits * rc.dropFramesWaterMark / 100
}

func (rc *rateControlState) updateRollingBitAverages(actualBits int, targetBits int) {
	rc.rollingActualBits = libvpxRollingBits(rc.rollingActualBits, actualBits, 3, 2)
	rc.rollingTargetBits = libvpxRollingBits(rc.rollingTargetBits, targetBits, 3, 2)
	rc.longRollingActualBits = libvpxRollingBits(rc.longRollingActualBits, actualBits, 31, 5)
	rc.longRollingTargetBits = libvpxRollingBits(rc.longRollingTargetBits, targetBits, 31, 5)
}

func (rc *rateControlState) resetRollingBitAverages() {
	rc.rollingActualBits = rc.bitsPerFrame
	rc.rollingTargetBits = rc.bitsPerFrame
	rc.longRollingActualBits = rc.bitsPerFrame
	rc.longRollingTargetBits = rc.bitsPerFrame
}
