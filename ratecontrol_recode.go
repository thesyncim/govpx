package govpx

type frameSizeRecodeState struct {
	qLow                int
	qHigh               int
	zbinOQLow           int
	zbinOQHigh          int
	zbinOverQuant       int
	correctionFactor    float64
	activeWorstQChanged bool
	overshootSeen       bool
	undershootSeen      bool
}

func (rc *rateControlState) newFrameSizeRecodeState(keyFrame bool, goldenFrame bool) frameSizeRecodeState {
	return rc.newFrameSizeRecodeStateWithAltRef(keyFrame, goldenFrame, false)
}

// newFrameSizeRecodeStateWithAltRef extends newFrameSizeRecodeState with
// libvpx's `cm->refresh_alt_ref_frame` branch so the recode loop's q_low /
// q_high seeds and the `zbin_oq_high` cap honor an ARF refresh. The
// rate-correction-factor entry in the recode state still indexes through
// rateCorrectionFactorForFrame(keyFrame, goldenFrame || altRefFrame),
// matching libvpx which shares the GF rate-correction-factor with ARF
// refresh in single-layer one-pass mode.
func (rc *rateControlState) newFrameSizeRecodeStateWithAltRef(keyFrame bool, goldenFrame bool, altRefFrame bool) frameSizeRecodeState {
	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(keyFrame, goldenFrame, altRefFrame)
	return frameSizeRecodeState{
		qLow:             activeBest,
		qHigh:            activeWorst,
		zbinOQHigh:       libvpxZbinOverQuantHighAltRef(keyFrame, goldenFrame, altRefFrame),
		zbinOverQuant:    rc.currentZbinOverQuant,
		correctionFactor: rc.rateCorrectionFactorForFrame(keyFrame, goldenFrame || altRefFrame),
	}
}

func (rc *rateControlState) frameSizeRecodeQuantizerWithContext(sizeBytes int, keyFrame bool, goldenFrame bool, macroblocks int, recode *frameSizeRecodeState) (int, bool) {
	return rc.frameSizeRecodeQuantizerWithContextBits(encodedSizeBits(sizeBytes), keyFrame, goldenFrame, macroblocks, recode)
}

func (rc *rateControlState) frameSizeRecodeQuantizerWithContextBits(actualBits int, keyFrame bool, goldenFrame bool, macroblocks int, recode *frameSizeRecodeState) (int, bool) {
	if recode == nil {
		return rc.currentQuantizer, false
	}
	q := rc.currentQuantizer
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 || macroblocks <= 0 {
		return rc.clampedFrameQuantizerValue(q), false
	}
	undershootLimit, overshootLimit := rc.frameSizeBoundsBits(keyFrame, goldenFrame, targetBits)
	recode.activeWorstQChanged = rc.relaxActiveWorstQuantizerForOvershoot(actualBits, overshootLimit, q, recode)
	rc.activeWorstQChanged = recode.activeWorstQChanged
	if !rc.shouldRecodeFrameSize(actualBits, undershootLimit, overshootLimit, q, keyFrame, goldenFrame, recode) {
		return rc.clampedFrameQuantizerValue(q), false
	}

	var next int
	if actualBits > targetBits {
		if q < recode.qHigh {
			recode.qLow = q + 1
		} else {
			recode.qLow = recode.qHigh
		}
		if recode.zbinOverQuant > 0 {
			recode.zbinOQLow = min(recode.zbinOverQuant+1, recode.zbinOQHigh)
		}
		if recode.undershootSeen {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, macroblocks, 1, recode.correctionFactor)
			}
			next = (recode.qHigh + recode.qLow + 1) / 2
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			} else {
				recode.zbinOQLow = min(recode.zbinOverQuant+1, recode.zbinOQHigh)
				recode.zbinOverQuant = (recode.zbinOQHigh + recode.zbinOQLow) / 2
			}
		} else {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, macroblocks, 0, recode.correctionFactor)
			}
			next, recode.zbinOverQuant = libvpxRegulatedQuantizerWithZbin(keyFrame, goldenFrame, targetBits, macroblocks, recode.qLow, recode.qHigh, recode.correctionFactor)
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			}
		}
		recode.overshootSeen = true
	} else {
		if recode.zbinOverQuant == 0 && q > recode.qLow {
			recode.qHigh = q - 1
		} else if recode.zbinOverQuant > 0 {
			recode.zbinOQHigh = max(recode.zbinOverQuant-1, recode.zbinOQLow)
		} else {
			recode.qHigh = recode.qLow
		}
		if recode.overshootSeen {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, macroblocks, 1, recode.correctionFactor)
			}
			next = (recode.qHigh + recode.qLow) / 2
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			} else {
				recode.zbinOverQuant = (recode.zbinOQHigh + recode.zbinOQLow) / 2
			}
		} else {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, macroblocks, 0, recode.correctionFactor)
			}
			next, recode.zbinOverQuant = libvpxRegulatedQuantizerWithZbin(keyFrame, goldenFrame, targetBits, macroblocks, recode.qLow, recode.qHigh, recode.correctionFactor)
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			}
			if rc.mode == RateControlCQ && next < recode.qLow {
				recode.qLow = next
			}
		}
		recode.undershootSeen = true
	}
	if next > recode.qHigh {
		next = recode.qHigh
	} else if next < recode.qLow {
		next = recode.qLow
	}
	if recode.zbinOverQuant < recode.zbinOQLow {
		recode.zbinOverQuant = recode.zbinOQLow
	} else if recode.zbinOverQuant > recode.zbinOQHigh {
		recode.zbinOverQuant = recode.zbinOQHigh
	}
	if next < vp8MaxQIndex {
		recode.zbinOverQuant = 0
	}
	return rc.clampedFrameQuantizerValue(next), true
}

// forcedKeyFrameRecodeQuantizer ports the libvpx vp8/encoder/onyx_if.c
// "Special case handling for forced key frames" branch in
// encode_frame_to_data_rate (around line 4065). Given the SS-error of the
// just-encoded forced key frame and the ambient error baseline captured from
// the frame preceding the forced KF, libvpx narrows the recode q_low/q_high
// bounds and picks the midpoint. The caller is expected to feed currentQuantizer
// as the just-attempted Q; the returned (Q, recoded) pair indicates the next Q
// and whether a recode is required (Q != last Q).
//
// Branch semantics from libvpx:
//   - kf_err > ambient_err * 7/8: KF too lossy; lower q_high to (Q-1) (or q_low),
//     pick Q = (q_high + q_low) >> 1.
//   - kf_err < ambient_err / 2: KF much better than previous; raise q_low to
//     (Q+1) (or q_high), pick Q = (q_high + q_low + 1) >> 1.
//   - Else: leave Q alone; no recode.
//
// Q is clamped to [q_low, q_high] before returning.
func (rc *rateControlState) forcedKeyFrameRecodeQuantizer(kfErr int, ambientErr int, recode *frameSizeRecodeState) (int, bool) {
	if recode == nil || ambientErr <= 0 {
		return rc.currentQuantizer, false
	}
	q := rc.currentQuantizer
	lastQ := q
	threshTooLossy := (ambientErr * 7) >> 3
	threshMuchBetter := ambientErr >> 1
	switch {
	case kfErr > threshTooLossy:
		if q > recode.qLow {
			recode.qHigh = q - 1
		} else {
			recode.qHigh = recode.qLow
		}
		q = (recode.qHigh + recode.qLow) >> 1
	case kfErr < threshMuchBetter:
		if q < recode.qHigh {
			recode.qLow = q + 1
		} else {
			recode.qLow = recode.qHigh
		}
		q = (recode.qHigh + recode.qLow + 1) >> 1
	}
	if q > recode.qHigh {
		q = recode.qHigh
	}
	if q < recode.qLow {
		q = recode.qLow
	}
	q = rc.clampedFrameQuantizerValue(q)
	return q, q != lastQ
}

func (rc *rateControlState) relaxActiveWorstQuantizerForOvershoot(actualBits int, overshootLimit int, q int, recode *frameSizeRecodeState) bool {
	if recode == nil || actualBits <= overshootLimit || overshootLimit <= 0 {
		return false
	}
	if q != recode.qHigh || recode.qHigh >= rc.maxQuantizer {
		return false
	}
	overSizePercent := ((actualBits - overshootLimit) * 100) / overshootLimit
	changed := false
	for recode.qHigh < rc.maxQuantizer && overSizePercent > 0 {
		recode.qHigh++
		overSizePercent = (overSizePercent * 96) / 100
		changed = true
	}
	if recode.qHigh < recode.qLow {
		recode.qHigh = recode.qLow
	}
	return changed
}

func (rc *rateControlState) shouldRecodeFrameSize(actualBits int, undershootLimit int, overshootLimit int, q int, keyFrame bool, goldenFrame bool, recode *frameSizeRecodeState) bool {
	if (actualBits > overshootLimit && q < recode.qHigh) || (actualBits < undershootLimit && q > recode.qLow) {
		return true
	}
	if rc.mode != RateControlCQ {
		return false
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return false
	}
	if q > rc.cqLevel && actualBits < (targetBits*7)>>3 {
		return true
	}
	return !keyFrame && !goldenFrame && q > rc.cqLevel && actualBits < rc.minimumFrameBandwidthBits() && recode.qLow > rc.cqLevel
}

func (rc *rateControlState) rateCorrectionFactorAfterFrameSize(actualBits int, keyFrame bool, macroblocks int, dampVar int, rateCorrectionFactor float64) float64 {
	if actualBits <= 0 || macroblocks <= 0 {
		return rateCorrectionFactor
	}
	q := rc.currentQuantizer
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	if q < 0 || q >= len(libvpxBitsPerMB[frameType]) {
		return rateCorrectionFactor
	}
	rateCorrectionFactor = normalizedRateCorrectionFactor(rateCorrectionFactor)
	projectedBits := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, rateCorrectionFactor, rc.currentZbinOverQuant)
	if projectedBits <= 0 {
		return rateCorrectionFactor
	}
	correctionFactor := int((100 * int64(actualBits)) / int64(projectedBits))
	adjustmentLimit := 0.25
	switch dampVar {
	case 0:
		adjustmentLimit = 0.75
	case 1:
		adjustmentLimit = 0.375
	}
	switch {
	case correctionFactor > 102:
		correctionFactor = int(100.5 + float64(correctionFactor-100)*adjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor > libvpxMaxBPBFactor {
			rateCorrectionFactor = libvpxMaxBPBFactor
		}
	case correctionFactor < 99:
		correctionFactor = int(100.5 - float64(100-correctionFactor)*adjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor < libvpxMinBPBFactor {
			rateCorrectionFactor = libvpxMinBPBFactor
		}
	}
	return rateCorrectionFactor
}

func (rc *rateControlState) minimumFrameBandwidthBits() int {
	target := rc.bitsPerFrame
	if rc.frameTargetBits > 0 {
		target = rc.frameTargetBits
	}
	if target <= 0 {
		return 0
	}
	minTarget := max(target/8, 1)
	return minTarget
}

func (rc *rateControlState) clampedFrameQuantizerValue(q int) int {
	if rc.mode == RateControlCQ {
		return rc.clampedCQQuantizerValue(q)
	}
	return rc.clampedQuantizerValue(q)
}

func (rc *rateControlState) clampedQuantizerValue(q int) int {
	if q < rc.minQuantizer {
		return rc.minQuantizer
	}
	if q > rc.maxQuantizer {
		return rc.maxQuantizer
	}
	return q
}

func (rc *rateControlState) clampedCQQuantizerValue(q int) int {
	if q < rc.cqLevel {
		return rc.cqLevel
	}
	return rc.clampedQuantizerValue(q)
}

func (rc *rateControlState) adjustQuantizer(actualBits int, targetBits int) {
	rc.adjustQuantizerWithContext(actualBits, targetBits, false, false)
}

func (rc *rateControlState) adjustQuantizerWithContext(actualBits int, targetBits int, keyFrame bool, goldenFrame bool) {
	if targetBits <= 0 {
		return
	}
	undershootLimit, overshootLimit := rc.frameSizeBoundsBits(keyFrame, goldenFrame, targetBits)
	switch {
	case actualBits > overshootLimit:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) {
			step = 2
		}
		rc.currentQuantizer += step
	case actualBits < undershootLimit:
		rc.currentQuantizer--
	}
}

func (rc *rateControlState) adjustCQQuantizerWithContext(actualBits int, targetBits int, keyFrame bool, goldenFrame bool) {
	if targetBits <= 0 {
		return
	}
	undershootLimit, overshootLimit := rc.frameSizeBoundsBits(keyFrame, goldenFrame, targetBits)
	switch {
	case actualBits > overshootLimit:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) {
			step = 2
		}
		rc.currentQuantizer += step
	case actualBits < undershootLimit:
		rc.currentQuantizer--
	}
	rc.currentQuantizer = rc.clampedCQQuantizerValue(rc.currentQuantizer)
}

func (rc *rateControlState) frameSizeBoundsBits(keyFrame bool, goldenFrame bool, targetBits int) (int, int) {
	if targetBits <= 0 {
		return 0, 0
	}
	target := min(int64(targetBits), libvpxIntMax)

	var undershootLimit int64
	var overshootLimit int64
	switch {
	case keyFrame || goldenFrame || rc.currentTemporalLayers > 1:
		overshootLimit = target * 9 / 8
		undershootLimit = target * 7 / 8
	case rc.mode == RateControlCBR:
		bufferLevel := int64(rc.bufferLevelBits)
		optimalBuffer := int64(rc.bufferOptimalBits)
		maximumBuffer := int64(rc.maximumBufferBits)
		switch {
		case bufferLevel >= (optimalBuffer+maximumBuffer)/2:
			overshootLimit = target * 12 / 8
			undershootLimit = target * 6 / 8
		case bufferLevel <= optimalBuffer/2:
			overshootLimit = target * 10 / 8
			undershootLimit = target * 4 / 8
		default:
			overshootLimit = target * 11 / 8
			undershootLimit = target * 5 / 8
		}
	case rc.mode == RateControlCQ:
		overshootLimit = target * 11 / 8
		undershootLimit = target * 2 / 8
	default:
		overshootLimit = target * 11 / 8
		undershootLimit = target * 5 / 8
	}

	overshootLimit += 200
	undershootLimit -= 200
	if undershootLimit < 0 {
		undershootLimit = 0
	}
	if undershootLimit > libvpxIntMax {
		undershootLimit = libvpxIntMax
	}
	if overshootLimit > libvpxIntMax {
		overshootLimit = libvpxIntMax
	}
	return int(undershootLimit), int(overshootLimit)
}

// applyPass2CBRBufferAdjustment ports the libvpx vp8/encoder/firstpass.c
// Pass2Encode CBR (USAGE_STREAM_FROM_SERVER) per-frame target adjustment
// based on `cpi->buffer_level` versus `cpi->oxcf.optimal_buffer_level`.
// libvpx's Pass2 path leaves the second-pass error-fraction target alone
// for VBR but, when CBR is active, re-clamps the per-frame target through
// the same buffer-state adjustment that calc_pframe_target_size applies in
// the one-pass path: when the buffer is below optimal the target is shrunk
// to help refill the buffer, and when the buffer is above optimal the
// target is grown to drain the surplus. This mirrors govpx's existing
// `bufferAdjustedFrameTargetBits` (which already runs for one-pass CBR
// inter frames inside beginFrameWithTargetAndContext) but applies it to
// the second-pass target after the two-pass error-fraction allocation
// has run, so the buffer state still pulls the target back when
// twoPassState.frameTargetBits overrides the one-pass value. Returns
// targetBits unchanged for non-CBR modes, key frames (libvpx defers KF
// adjustments to the kf_bits buffer cap path), or zero / negative
// targets.
