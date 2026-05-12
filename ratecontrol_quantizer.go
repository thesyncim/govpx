package govpx

func (rc *rateControlState) selectQuantizerForFrame(keyFrame bool, macroblocks int) {
	rc.selectQuantizerForFrameKind(keyFrame, false, macroblocks)
}

func (rc *rateControlState) selectQuantizerForFrameKind(keyFrame bool, goldenFrame bool, macroblocks int) {
	rc.selectQuantizerForFrameKindWithScreenContent(keyFrame, goldenFrame, macroblocks, 0)
}

func (rc *rateControlState) selectQuantizerForFrameKindWithScreenContent(keyFrame bool, goldenFrame bool, macroblocks int, screenContentMode int) {
	rc.selectQuantizerForFrameKindWithAltRef(keyFrame, goldenFrame, false, macroblocks, screenContentMode)
}

// selectQuantizerForFrameKindWithAltRef extends
// selectQuantizerForFrameKindWithScreenContent with libvpx's
// `cm->refresh_alt_ref_frame` branch from
// `vp8/encoder/onyx_if.c:encode_frame_to_data_rate`. In one-pass mode, libvpx
// folds an ARF refresh into the same active-best/worst regulation path as a
// golden refresh: both arms gate on
// `(cm->refresh_golden_frame || cpi->common.refresh_alt_ref_frame)` with
// `oxcf.number_of_layers == 1`, and both consult `gf_high_motion_minq` for
// the active-best-quality floor. The split only matters for the
// `zbin_oq_high` cap (see libvpxZbinOverQuantHigh) and for the recode
// rate-correction-factor accounting (which is already keyed off
// `goldenFrame`). Pass `altRefFrame=true` from the encode driver when
// `cpi->common.refresh_alt_ref_frame` is set; pass `goldenFrame=true` when
// the encoder is producing an overlay or a regular GF refresh.
func (rc *rateControlState) selectQuantizerForFrameKindWithAltRef(keyFrame bool, goldenFrame bool, altRefFrame bool, macroblocks int, screenContentMode int) {
	if macroblocks <= 0 {
		return
	}
	if !rateControlModeUsesQuantizerRegulator(rc.mode) {
		return
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return
	}
	rc.activeWorstQChanged = false
	gfOrArf := goldenFrame || altRefFrame
	correctionFactor := rc.rateCorrectionFactorForFrame(keyFrame, gfOrArf)
	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(keyFrame, goldenFrame, altRefFrame)
	rc.currentQuantizer, rc.currentZbinOverQuant = libvpxRegulatedQuantizerWithZbinAltRef(keyFrame, goldenFrame, altRefFrame, targetBits, macroblocks, activeBest, activeWorst, correctionFactor)
	if rc.mode == RateControlCQ {
		rc.currentQuantizer = rc.clampedCQQuantizerValue(rc.currentQuantizer)
		if rc.currentQuantizer < vp8MaxQIndex {
			rc.currentZbinOverQuant = 0
		}
	}
	if rc.mode == RateControlCBR && screenContentMode > 0 && !keyFrame {
		rc.currentQuantizer = libvpxLimitCBRInterQuantizerDrop(rc.lastInterQuantizer, rc.currentQuantizer)
		if rc.currentQuantizer < vp8MaxQIndex {
			rc.currentZbinOverQuant = 0
		}
	}
	rc.clampQuantizer()
	if rc.currentQuantizer < vp8MaxQIndex {
		rc.currentZbinOverQuant = 0
	}
}

// libvpxActiveQuantizerBounds is the legacy two-argument entry point. ARF
// refresh callers should use libvpxActiveQuantizerBoundsForFrame so the
// returned bounds honor libvpx's `cm->refresh_alt_ref_frame` branch.
func (rc *rateControlState) libvpxActiveQuantizerBounds(keyFrame bool, goldenFrame bool) (int, int) {
	return rc.libvpxActiveQuantizerBoundsForFrame(keyFrame, goldenFrame, false)
}

// libvpxActiveQuantizerBoundsForFrame ports the active-best/worst-Q selection
// at `vp8/encoder/onyx_if.c:3616-3750`. The ARF refresh case follows the
// single-layer GF branch (`cm->refresh_golden_frame ||
// cpi->common.refresh_alt_ref_frame`) which uses gf_high_motion_minq for the
// one-pass active-best floor and may pull `Q` toward `cpi->avg_frame_qindex`
// when it is below `active_worst_quality`. For altRefFrame=true callers, the
// branch fires regardless of `goldenFrame` so the caller can drive a hidden
// ARF without first marking the source frame as a golden refresh.
func (rc *rateControlState) libvpxActiveQuantizerBoundsForFrame(keyFrame bool, goldenFrame bool, altRefFrame bool) (int, int) {
	activeWorst := rc.libvpxActiveWorstQuantizer()
	if rc.mode == RateControlCBR && rc.bufferOptimalBits > 0 && rc.bufferLevelBits >= rc.bufferOptimalBits {
		activeWorst = rc.libvpxCBRFullBufferActiveWorst(activeWorst)
	}
	activeWorst = rc.clampedQuantizerValue(activeWorst)

	gfOrArf := goldenFrame || altRefFrame
	activeBest := rc.minQuantizer
	// libvpx vp8/encoder/onyx_if.c line 3619 gates the active-best
	// branches on `(cpi->pass == 2) || (cpi->ni_frames > 150)`. govpx
	// historically only honored the ni_frames>150 arm; for pass-2 we
	// also enable the libvpxKeyFrameHighMotionMinQ / GoldenMinQ /
	// InterMinQ floor lookups so the regulator sees the same active
	// best-Q lower bound libvpx does. Without this, pass-2 inter
	// frames pick Q values much lower than libvpx (q_match=8% on
	// desktopqvga vs 100% target_match) because the regulator's
	// activeBest stays at minQuantizer.
	pass2 := rc.pass2ActiveWorstQValid
	if rc.normalInterFrames > 150 || pass2 {
		q := clampQuantizerValue(activeWorst, 0, vp8MaxQIndex)
		switch {
		case keyFrame:
			// libvpx pass-2 KF branch (onyx_if.c lines 3624-3642):
			// kf_low_motion_minq when gfu_boost > 600, else
			// kf_high_motion_minq. govpx does not track gfu_boost
			// from the pass-2 driver here, so we use the high-motion
			// table for both ni_frames>150 and pass-2 fallthrough.
			// TODO: thread gfu_boost from twoPassState to pick the
			// low-motion table when boost > 600.
			activeBest = libvpxKeyFrameHighMotionMinQ[q]
		case gfOrArf && rc.currentTemporalLayers <= 1:
			if rc.framesSinceKeyframe > 1 && rc.avgFrameQuantizer < q {
				q = rc.avgFrameQuantizer
			}
			if rc.mode == RateControlCQ && q < rc.cqLevel {
				q = rc.cqLevel
			}
			q = clampQuantizerValue(q, 0, vp8MaxQIndex)
			activeBest = libvpxGoldenFrameHighMotionMinQ[q]
		default:
			activeBest = libvpxInterMinQ[q]
			if rc.mode == RateControlCQ && activeBest < rc.cqLevel {
				activeBest = rc.cqLevel
			}
		}
		if rc.mode == RateControlCBR {
			activeBest = rc.libvpxCBRFullBufferActiveBest(activeBest)
		}
	} else if rc.mode == RateControlCQ {
		if !keyFrame && !gfOrArf && activeBest < rc.cqLevel {
			activeBest = rc.cqLevel
		}
	}

	activeBest = rc.clampedQuantizerValue(activeBest)
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	return activeBest, activeWorst
}

func (rc *rateControlState) libvpxActiveWorstQuantizer() int {
	activeWorst := rc.maxQuantizer
	// libvpx vp8/encoder/firstpass.c vp8_second_pass first-frame branch
	// (lines 2349-2363) overwrites active_worst_quality with the
	// estimate_max_q result, then damps it on subsequent frames
	// (lines 2381-2392). When the pass-2 driver has seeded the
	// override we substitute it here so the regulator's worst-Q
	// ceiling matches libvpx's. The override is clamped to the
	// user-configured [minQuantizer, maxQuantizer] envelope to honor
	// CLI / public-API bounds.
	if rc.pass2ActiveWorstQValid {
		override := max(min(rc.pass2ActiveWorstQOverride, rc.maxQuantizer), rc.minQuantizer)
		activeWorst = override
	}
	if rc.mode != RateControlCBR || rc.normalInterFrames <= 150 || rc.bufferOptimalBits <= 0 {
		if rc.mode == RateControlCQ && activeWorst < rc.cqLevel {
			activeWorst = rc.cqLevel
		}
		return activeWorst
	}
	if rc.bufferLevelBits >= rc.bufferOptimalBits {
		activeWorst = rc.normalInterAvgQuantizer
	} else if rc.bufferLevelBits > rc.bufferOptimalBits>>2 {
		denom := (rc.bufferOptimalBits * 3) >> 2
		if denom > 0 {
			qadjustmentRange := rc.maxQuantizer - rc.normalInterAvgQuantizer
			aboveBase := rc.bufferLevelBits - (rc.bufferOptimalBits >> 2)
			activeWorst = rc.maxQuantizer - int((int64(qadjustmentRange)*int64(aboveBase))/int64(denom))
		}
	}
	return activeWorst
}

func (rc *rateControlState) libvpxCBRFullBufferActiveWorst(activeWorst int) int {
	if rc.maximumBufferBits <= rc.bufferOptimalBits {
		return activeWorst
	}
	adjustment := activeWorst / 4
	if adjustment <= 0 {
		return activeWorst
	}
	if rc.bufferLevelBits < rc.maximumBufferBits {
		bufferLevelStep := (rc.maximumBufferBits - rc.bufferOptimalBits) / adjustment
		if bufferLevelStep > 0 {
			adjustment = (rc.bufferLevelBits - rc.bufferOptimalBits) / bufferLevelStep
		} else {
			adjustment = 0
		}
	}
	return activeWorst - adjustment
}

func (rc *rateControlState) libvpxCBRFullBufferActiveBest(activeBest int) int {
	if rc.bufferOptimalBits <= 0 || rc.maximumBufferBits <= rc.bufferOptimalBits {
		return activeBest
	}
	switch {
	case rc.bufferLevelBits >= rc.maximumBufferBits:
		return rc.minQuantizer
	case rc.bufferLevelBits > rc.bufferOptimalBits:
		fraction := int((int64(rc.bufferLevelBits-rc.bufferOptimalBits) * 128) / int64(rc.maximumBufferBits-rc.bufferOptimalBits))
		minQAdjustment := ((activeBest - rc.minQuantizer) * fraction) / 128
		return activeBest - minQAdjustment
	default:
		return activeBest
	}
}

func (rc *rateControlState) clampBuffer() {
	if rc.bufferLevelBits > rc.maximumBufferBits {
		rc.bufferLevelBits = rc.maximumBufferBits
	}
}

func (rc *rateControlState) clampQuantizer() {
	rc.currentQuantizer = min(max(rc.currentQuantizer, rc.minQuantizer), rc.maxQuantizer)
	rc.lastQuantizer = min(max(rc.lastQuantizer, rc.minQuantizer), rc.maxQuantizer)
	rc.lastInterQuantizer = min(max(rc.lastInterQuantizer, rc.minQuantizer), rc.maxQuantizer)
}
