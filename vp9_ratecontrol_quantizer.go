package govpx

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (rc *vp9RateControlState) keyFrameTargetBits(frameIndex int) int {
	if rc.mode != RateControlCBR {
		// onePassVBRKeyFrameTargetBits already routes through
		// clampIFrameTargetBits, which (per libvpx
		// vp9_rc_clamp_iframe_target_size) applies the max-intra cap.
		// The explicit applyVP9MaxIntraBound here is a defensive no-op
		// when the cap was already enforced upstream.
		target := rc.onePassVBRKeyFrameTargetBits()
		return rc.applyVP9MaxIntraBound(target)
	}
	target := rc.onePassCBRKeyFrameTargetBits(frameIndex)
	return rc.applyVP9MaxIntraBound(target)
}

// onePassCBRKeyFrameTargetBits ports libvpx
// vp9_calc_iframe_target_size_one_pass_cbr. For the first video frame the
// target is starting_buffer_level / 2. For subsequent keyframes the target
// scales with kf_boost: target = ((16 + kf_boost) * avg_frame_bandwidth) >> 4
// where kf_boost = max(32, round(2*framerate - 16)) and is ramped up
// proportionally for keyframes that arrive earlier than framerate/2 frames
// after the previous key.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:2205-2232.
func (rc *vp9RateControlState) onePassCBRKeyFrameTargetBits(frameIndex int) int {
	if frameIndex == 0 {
		target := rc.bufferInitialBits >> 1
		return rc.clampIFrameTargetBits(target)
	}
	framerate := rc.framerateHz()
	kfBoost := 32
	if framerate > 0 {
		boost := int(2*framerate - 16 + 0.5)
		if boost > kfBoost {
			kfBoost = boost
		}
		halfRate := framerate / 2
		if halfRate > 0 && float64(rc.framesSinceKey) < halfRate {
			kfBoost = int(float64(kfBoost)*float64(rc.framesSinceKey)/halfRate + 0.5)
		}
	}
	target64 := min((int64(16+kfBoost)*int64(rc.bitsPerFrame))>>4, int64(maxInt()))
	return rc.clampIFrameTargetBits(int(target64))
}

// framerateHz returns the encoded framerate in Hz derived from the rate
// controller's timing fields, or 0 when unset. Mirrors libvpx's cpi->framerate
// (vp9_encoder.h:486, populated via vp9_new_framerate).
func (rc *vp9RateControlState) framerateHz() float64 {
	if rc == nil || rc.frameRateNum <= 0 || rc.frameRateDen <= 0 {
		return 0
	}
	return float64(rc.frameRateNum) / float64(rc.frameRateDen)
}

func (rc *vp9RateControlState) interFrameTargetBits() int {
	target := rc.perFrameBandwidthTargetBits()
	return rc.applyVP9MaxInterBound(target)
}

func (rc *vp9RateControlState) perFrameBandwidthTargetBits() int {
	target := rc.bitsPerFrame
	target = rc.applyVP9UndershootBound(target)
	target = rc.applyVP9OvershootBound(target)
	if target < encoder.FrameOverhead {
		return encoder.FrameOverhead
	}
	return target
}

// onePassCBRInterFrameTargetBits ports libvpx
// vp9_calc_pframe_target_size_one_pass_cbr for the non-SVC path. The CBR
// target is the average frame bandwidth adjusted by the current buffer level;
// gf_cbr_boost_pct redistributes that budget between golden-refresh frames
// and ordinary inter frames before the buffer adjustment.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:2154-2203.
func (rc *vp9RateControlState) onePassCBRInterFrameTargetBits(refreshFlags uint8) int {
	if rc == nil {
		return encoder.FrameOverhead
	}
	minTarget := max(rc.bitsPerFrame>>4, encoder.FrameOverhead)
	target := int64(rc.bitsPerFrame)
	interval := int64(rc.baselineGFInterval)
	if interval <= 0 {
		interval = (encoder.MinGFInterval + encoder.MaxGFInterval) >> 1
	}
	if rc.gfCBRBoostPct > 0 && interval > 0 {
		afRatioPct := int64(rc.gfCBRBoostPct + 100)
		den := interval*100 + afRatioPct - 100
		if den > 0 {
			mul := int64(100)
			if refreshFlags&(1<<vp9GoldenRefSlot) != 0 {
				mul = afRatioPct
			}
			target = int64(rc.bitsPerFrame) * interval * mul / den
		}
	}
	diff := int64(rc.bufferOptimalBits) - int64(rc.bufferLevelBits)
	onePctBits := int64(1 + rc.bufferOptimalBits/100)
	if onePctBits <= 0 {
		onePctBits = 1
	}
	if diff > 0 {
		pctLow := min(diff/onePctBits, int64(rc.undershootPct))
		target -= target * pctLow / 200
	} else if diff < 0 {
		pctHigh := min(-diff/onePctBits, int64(rc.overshootPct))
		target += target * pctHigh / 200
	}
	if rc.maxInterBitratePct > 0 && rc.bitsPerFrame > 0 {
		maxRate := int64(rc.bitsPerFrame) * int64(rc.maxInterBitratePct) / 100
		if target > maxRate {
			target = maxRate
		}
	}
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	if int(target) < minTarget {
		return minTarget
	}
	return int(target)
}

func (rc *vp9RateControlState) setOnePassVBRFrameTarget(intraOnly bool, refreshFlags uint8) {
	if !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if intraOnly {
		rc.frameTargetBits = rc.onePassVBRKeyFrameTargetBits()
		return
	}
	rc.frameTargetBits = rc.onePassVBRInterFrameTargetBits(refreshFlags)
}

func (rc *vp9RateControlState) onePassVBRKeyFrameTargetBits() int {
	target := min(int64(rc.bitsPerFrame)*25, int64(maxInt()))
	return rc.clampIFrameTargetBits(int(target))
}

// onePassVBRInterFrameTargetBits ports libvpx
// vp9_calc_pframe_target_size_one_pass_vbr (vp9_ratectrl.c:2027-2045). The
// target is avg_frame_bandwidth * baseline_gf_interval /
// (baseline_gf_interval + af_ratio - 1), with an extra af_ratio multiplier in
// the numerator on boosted (golden/altref refresh) frames. The gf interval and
// af_ratio are taken verbatim from rc->baseline_gf_interval /
// rc->af_ratio_onepass_vbr (already clamped by vp9_set_gf_update_one_pass_vbr);
// the libvpx source does no further interval clamping here.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:2027-2045.
func (rc *vp9RateControlState) onePassVBRInterFrameTargetBits(refreshFlags uint8) int {
	afRatio := int(rc.afRatioOnePassVBR)
	if afRatio <= 0 {
		afRatio = encoder.DefaultAFRatioOnePassVBR
	}
	interval := int(rc.baselineGFInterval)
	if interval <= 0 {
		interval = (encoder.MinGFInterval + encoder.MaxGFInterval) >> 1
	}
	den := int64(interval) + int64(afRatio) - 1
	var target int64
	if vp9BoostedInterRefresh(refreshFlags) {
		target = int64(rc.bitsPerFrame) * int64(interval) * int64(afRatio)
	} else {
		target = int64(rc.bitsPerFrame) * int64(interval)
	}
	if den > 0 {
		target /= den
	}
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	return rc.applyVP9MaxInterBound(rc.clampPFrameTargetBits(int(target)))
}

func (rc *vp9RateControlState) clampPFrameTargetBits(target int) int {
	minTarget := max(rc.minFrameBandwidth, encoder.FrameOverhead)
	if rc.bitsPerFrame > 0 && rc.bitsPerFrame>>5 > minTarget {
		minTarget = rc.bitsPerFrame >> 5
	}
	if target < minTarget {
		target = minTarget
	}
	if rc.maxFrameBandwidth > 0 && target > rc.maxFrameBandwidth {
		target = rc.maxFrameBandwidth
	}
	return target
}

// clampIFrameTargetBits ports libvpx VP9 vp9_rc_clamp_iframe_target_size.
// libvpx applies rc_max_intra_bitrate_pct first, then max_frame_bandwidth.
// Without the max-intra step, MaxIntraBitratePct never reaches the one-pass
// VBR keyframe target (which only routes through clampIFrameTargetBits, not
// through keyFrameTargetBits's post-clamp applyVP9MaxIntraBound call).
//
// libvpx: vp9/encoder/vp9_ratectrl.c:245-255 (vp9_rc_clamp_iframe_target_size).
func (rc *vp9RateControlState) clampIFrameTargetBits(target int) int {
	target = rc.applyVP9MaxIntraBound(target)
	if rc.maxFrameBandwidth > 0 && target > rc.maxFrameBandwidth {
		return rc.maxFrameBandwidth
	}
	if target < encoder.FrameOverhead {
		return encoder.FrameOverhead
	}
	return target
}

func (rc *vp9RateControlState) cbrQuantizer(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int, cyclic *encoder.CyclicRefreshState, encodeSpeed int) int {
	if !rc.enabled || rc.mode != RateControlCBR || macroblocks <= 0 {
		return int(rc.bestQuality)
	}
	activeBest, activeWorst := rc.cbrActiveQuantizerBounds(intraOnly, refreshFlags, frameIndex)
	correctionFactor := rc.rateCorrectionFactor(intraOnly, refreshFlags)
	q := vp9RegulatedQuantizer(intraOnly, rc.frameTargetBits, macroblocks,
		activeBest, activeWorst, correctionFactor, cyclic, encodeSpeed)
	return rc.adjustCBRQuantizer(q, refreshFlags)
}

func (rc *vp9RateControlState) cbrQuantizerWithBounds(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int, cyclic *encoder.CyclicRefreshState, encodeSpeed int) (q int, activeBest int, activeWorst int, correctionFactor float64) {
	if !rc.enabled || rc.mode != RateControlCBR || macroblocks <= 0 {
		best := int(rc.bestQuality)
		return best, best, int(rc.worstQuality), 1
	}
	activeBest, activeWorst = rc.cbrActiveQuantizerBounds(intraOnly, refreshFlags, frameIndex)
	correctionFactor = rc.rateCorrectionFactor(intraOnly, refreshFlags)
	q = vp9RegulatedQuantizer(intraOnly, rc.frameTargetBits, macroblocks,
		activeBest, activeWorst, correctionFactor, cyclic, encodeSpeed)
	return rc.adjustCBRQuantizer(q, refreshFlags), activeBest, activeWorst, correctionFactor
}

func (rc *vp9RateControlState) vbrQuantizer(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int, cyclic *encoder.CyclicRefreshState, encodeSpeed int) int {
	q, _, _, _ := rc.vbrQuantizerWithBounds(intraOnly, refreshFlags,
		frameIndex, macroblocks, cyclic, encodeSpeed)
	return q
}

func (rc *vp9RateControlState) vbrQuantizerWithBounds(intraOnly bool, refreshFlags uint8, frameIndex int, macroblocks int, cyclic *encoder.CyclicRefreshState, encodeSpeed int) (q int, activeBest int, activeWorst int, correctionFactor float64) {
	if !rc.enabled || rc.mode == RateControlCBR || macroblocks <= 0 {
		best := int(rc.bestQuality)
		return best, best, int(rc.worstQuality), 1
	}
	activeBest, activeWorst = rc.vbrActiveQuantizerBounds(intraOnly,
		refreshFlags, frameIndex)
	correctionFactor = rc.rateCorrectionFactor(intraOnly, refreshFlags)
	if rc.mode == RateControlQ {
		return activeBest, activeBest, activeWorst, correctionFactor
	}
	q = vp9RegulatedQuantizer(intraOnly, rc.frameTargetBits, macroblocks,
		activeBest, activeWorst, correctionFactor, cyclic, encodeSpeed)
	return q, activeBest, activeWorst, correctionFactor
}

// vp9RegulatedQuantizer selects the frame qindex. When cyclic refresh is
// active on an inter frame, libvpx's vp9_rc_regulate_q uses
// vp9_cyclic_refresh_rc_bits_per_mb instead of vp9_rc_bits_per_mb.
func vp9RegulatedQuantizer(intraOnly bool, targetBits int, macroblocks int, activeBest, activeWorst int, correctionFactor float64, cyclic *encoder.CyclicRefreshState, encodeSpeed int) int {
	if cyclic != nil && cyclic.Enabled && cyclic.ApplyCyclicRefresh && !intraOnly {
		return encoder.RegulatedQuantizerWithCyclicRefresh(intraOnly,
			targetBits, macroblocks, activeBest, activeWorst, cyclic,
			encodeSpeed, correctionFactor)
	}
	return encoder.RegulatedQuantizer(intraOnly, targetBits, macroblocks,
		activeBest, activeWorst, correctionFactor)
}

func (rc *vp9RateControlState) onePassRecodeAllowed() bool {
	// libvpx VP9 forces DISALLOW_RECODE for pass 0, which is the realtime
	// helper path used by the current byte-parity oracle.
	return false
}

func (rc *vp9RateControlState) cbrActiveQuantizerBounds(intraOnly bool, refreshFlags uint8, frameIndex int) (int, int) {
	best := int(rc.bestQuality)
	worst := int(rc.worstQuality)
	activeWorst := min(max(rc.cbrActiveWorstQuantizer(intraOnly, frameIndex), best), worst)

	activeBest := best
	if intraOnly {
		activeBest = best
		if frameIndex > 0 {
			activeBest = encoder.KFActiveQuality(int(rc.avgFrameQIndexKey))
			if int64(rc.codedWidth)*int64(rc.codedHeight) <= 352*288 {
				activeBest += encoder.ComputeQDelta(best, worst, activeBest, 75, 100)
			}
		}
	} else {
		qBasis := activeWorst
		if frameIndex > 1 && int(rc.avgFrameQIndexInter) < activeWorst {
			qBasis = int(rc.avgFrameQIndexInter)
		} else if frameIndex <= 1 && int(rc.avgFrameQIndexKey) < activeWorst {
			qBasis = int(rc.avgFrameQIndexKey)
		}
		activeBest = encoder.RTCMinQ(qBasis)
	}
	activeBest = min(max(rc.applyVP9RefreshActiveBestBias(activeBest, intraOnly,
		refreshFlags, best, worst), best), worst)
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	return activeBest, activeWorst
}

// applyVP9RefreshActiveBestBias applies the FramePeriodicBoost
// active-best-Q bias on GF / ALTREF refresh frames.
// FramePeriodicBoost mirrors VP9E_SET_FRAME_PERIODIC_BOOST: it biases
// the active-best Q downward (lower quantizer, more bits) so the
// regulated quantizer reaches a tighter target on the periodic
// refresh frame.
//
// AltRefAQ deliberately does not participate here: libvpx v1.16.0 wires
// VP9E_SET_ALT_REF_AQ through the control surface, but vp9_alt_ref_aq.c is
// a stub. A prior govpx approximation biased alt-ref Q coarser and regressed
// the BD-rate gate, so parity is to leave the stream unchanged.
//
// Intra-only frames and non-boosted inter frames are untouched.
func (rc *vp9RateControlState) applyVP9RefreshActiveBestBias(activeBest int, intraOnly bool, refreshFlags uint8, best, worst int) int {
	if intraOnly || !vp9BoostedInterRefresh(refreshFlags) {
		return activeBest
	}
	if rc.framePeriodicBoost {
		activeBest += encoder.ComputeQDelta(best, worst, activeBest, 3, 4)
	}
	if activeBest < best {
		activeBest = best
	}
	if activeBest > worst {
		activeBest = worst
	}
	return activeBest
}

func (rc *vp9RateControlState) vbrActiveQuantizerBounds(intraOnly bool, refreshFlags uint8, frameIndex int) (int, int) {
	best := int(rc.bestQuality)
	worst := int(rc.worstQuality)
	activeWorst := min(max(rc.vbrActiveWorstQuantizer(intraOnly, refreshFlags,
		frameIndex), best), worst)
	cqLevel := rc.activeCQLevelOnePass()
	activeBest := best
	if intraOnly {
		if rc.mode == RateControlQ {
			activeBest = max(cqLevel+encoder.ComputeQDelta(best, worst, cqLevel,
				1, 4), best)
		} else {
			activeBest = encoder.KFActiveQuality(int(rc.avgFrameQIndexKey))
			if int64(rc.codedWidth)*int64(rc.codedHeight) <= 352*288 {
				activeBest += encoder.ComputeQDelta(best, worst,
					activeBest, 75, 100)
			}
		}
	} else if vp9BoostedInterRefresh(refreshFlags) {
		qBasis := int(rc.avgFrameQIndexKey)
		if rc.framesSinceKey > 1 {
			qBasis = min(int(rc.avgFrameQIndexInter), activeWorst)
		}
		// libvpx get_gf_active_quality indexes the GF min-q tables by
		// rc->gfu_boost (vp9_ratectrl.c:906-919); for the one-pass VBR path
		// rc->gfu_boost is (re)computed per golden group by
		// vp9_set_gf_update_one_pass_vbr, so thread the live boost through
		// instead of the DEFAULT_GF_BOOST=2000 default.
		gfBoost := int(rc.gfuBoost)
		if gfBoost <= 0 {
			gfBoost = encoder.DefaultGFBoost
		}
		switch rc.mode {
		case RateControlCQ:
			if qBasis < cqLevel {
				qBasis = cqLevel
			}
			activeBest = (encoder.GFActiveQualityWithBoost(qBasis, gfBoost) * 15) >> 4
		case RateControlQ:
			num := 1
			den := 2
			if refreshFlags&(1<<vp9AltRefSlot) != 0 {
				num = 2
				den = 5
			}
			activeBest = max(cqLevel+encoder.ComputeQDelta(best, worst,
				cqLevel, num, den), best)
		default:
			activeBest = encoder.GFActiveQualityWithBoost(qBasis, gfBoost)
		}
	} else if rc.mode == RateControlQ {
		num, den := encoder.PublicQModeInterRate(frameIndex)
		activeBest = max(cqLevel+encoder.ComputeQDelta(best, worst, cqLevel,
			num, den), best)
	} else {
		if frameIndex > 1 {
			activeBest = encoder.InterMinQ(min(int(rc.avgFrameQIndexInter),
				activeWorst))
		} else {
			activeBest = encoder.InterMinQ(int(rc.avgFrameQIndexKey))
		}
		if rc.mode == RateControlCQ && activeBest < cqLevel {
			activeBest = cqLevel
		}
	}

	activeBest = min(max(rc.applyVP9RefreshActiveBestBias(activeBest, intraOnly,
		refreshFlags, best, worst), best), worst)
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	if intraOnly && frameIndex != 0 {
		activeWorst += encoder.ComputeQDeltaByRate(best, worst, intraOnly,
			activeWorst, 2, 1)
		if activeWorst < activeBest {
			activeWorst = activeBest
		}
	} else if !intraOnly && vp9BoostedInterRefresh(refreshFlags) {
		activeWorst += encoder.ComputeQDeltaByRate(best, worst, intraOnly,
			activeWorst, 7, 4)
		if activeWorst < activeBest {
			activeWorst = activeBest
		}
	}
	if activeWorst > worst {
		activeWorst = worst
	}
	return activeBest, activeWorst
}

func (rc *vp9RateControlState) vbrActiveWorstQuantizer(intraOnly bool, refreshFlags uint8, frameIndex int) int {
	worst := int(rc.worstQuality)
	if intraOnly {
		if frameIndex == 0 {
			return worst
		}
		return min(int(rc.lastQKey)<<1, worst)
	}
	if vp9BoostedInterRefresh(refreshFlags) {
		if frameIndex == 1 {
			return min((int(rc.lastQKey)*5)>>2, worst)
		}
		return min(int(rc.lastQInter)*int(rc.facActiveWorstGF)/100, worst)
	}
	if frameIndex == 1 {
		return min(int(rc.lastQKey)<<1, worst)
	}
	return min(int(rc.avgFrameQIndexInter)*int(rc.facActiveWorstInter)/100,
		worst)
}

func (rc *vp9RateControlState) activeCQLevelOnePass() int {
	level := int(rc.cqLevel)
	if rc.mode == RateControlCQ && rc.totalTargetBits > 0 {
		adjusted := (int64(level) * rc.totalActualBits * 10) /
			rc.totalTargetBits
		if adjusted < int64(level) {
			level = int(adjusted)
		}
	}
	return level
}

// updateRollingBits mirrors the inter-frame rolling-monitor EMA update in
// vp9_rc_postencode_update (vp9_ratectrl.c:1929-1939). Only inter frames feed
// the short rolling_{target,actual}_bits monitors; intra-only frames leave
// them unchanged. ROUND64_POWER_OF_TWO(x, 2) == (x + 2) >> 2.
func (rc *vp9RateControlState) updateRollingBits(intraOnly bool, encodedBits int) {
	if rc == nil || !rc.enabled || intraOnly {
		return
	}
	rc.rollingTargetBits = int((int64(rc.rollingTargetBits)*3 +
		int64(rc.frameTargetBits) + 2) >> 2)
	rc.rollingActualBits = int((int64(rc.rollingActualBits)*3 +
		int64(encodedBits) + 2) >> 2)
}

// setGFUpdateOnePassVBR ports libvpx vp9_set_gf_update_one_pass_vbr
// (vp9_ratectrl.c:2077-2127) for the non-SVC, non-cyclic-refresh one-pass VBR
// path. When the golden countdown reaches zero it recomputes the GF interval,
// af_ratio, and gfu_boost for the new golden group, re-seeds the countdown,
// and arms refresh_golden_frame. Once current_video_frame > 30 the gfu_boost
// and af_ratio are damped by avg_frame_low_motion (and the rolling-bits
// rate_err), which lowers the golden-frame target and lifts the regulated q on
// later golden refreshes. The cyclic-refresh golden-update branch
// (CYCLIC_REFRESH_AQ) and the altref-onepass arming are SVC/auto-arf only and
// do not apply to the realtime cpu8 lane here.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:2077-2127.
func (rc *vp9RateControlState) setGFUpdateOnePassVBR(currentVideoFrame int) {
	if rc == nil || !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if rc.framesTillGF != 0 {
		return
	}
	rateErr := 1.0
	rc.gfuBoost = uint16(encoder.DefaultGFBoost)
	// Non-cyclic-refresh path: baseline_gf_interval =
	// VPXMIN(20, VPXMAX(10, (min_gf_interval + max_gf_interval) / 2)).
	baseline := min(20, max(10, (rc.resolvedMinGFInterval+rc.resolvedMaxGFInterval)/2))
	rc.afRatioOnePassVBR = 10
	if rc.rollingTargetBits > 0 {
		rateErr = float64(rc.rollingActualBits) / float64(rc.rollingTargetBits)
	}
	if currentVideoFrame > 30 {
		worst := int(rc.worstQuality)
		if int(rc.avgFrameQIndexInter) > (7*worst)>>3 && rateErr > 3.5 {
			baseline = min(15, (3*baseline)>>1)
		} else if rc.avgFrameLowMotion > 0 && rc.avgFrameLowMotion < 20 {
			baseline = max(6, baseline>>1)
		}
		if rc.avgFrameLowMotion > 0 {
			boost := max(encoder.DefaultGFBoost*(rc.avgFrameLowMotion<<1)/
				(rc.avgFrameLowMotion+100), 500)
			rc.gfuBoost = uint16(boost)
		} else if rc.avgFrameLowMotion == 0 && rateErr > 1.0 {
			rc.gfuBoost = uint16(encoder.DefaultGFBoost >> 1)
		}
		rc.afRatioOnePassVBR = uint8(min(15, max(5, 3*int(rc.gfuBoost)/400)))
	}
	rc.baselineGFInterval = uint8(baseline)
	// constrain_gf_key_freq_onepass_vbr is 1 by default (vp9_ratectrl.c:424).
	rc.adjustGFIntFrameConstraint(rc.framesToKey)
	rc.framesTillGF = rc.baselineGFInterval
	rc.refreshGoldenFrame = true
}

// adjustGFIntFrameConstraint ports adjust_gfint_frame_constraint
// (vp9_ratectrl.c:2058-2075): keep the golden interval consistent with the
// remaining frames-to-key budget.
func (rc *vp9RateControlState) adjustGFIntFrameConstraint(frameConstraint int) {
	gi := int(rc.baselineGFInterval)
	if frameConstraint <= (7*gi)>>2 && frameConstraint > gi {
		gi = frameConstraint >> 1
		if gi < 5 {
			gi = frameConstraint
		}
	} else if gi > frameConstraint {
		gi = frameConstraint
	}
	rc.baselineGFInterval = uint8(gi)
}

func (rc *vp9RateControlState) setRuntimeOnePassVBRGoldenCadence(prev vp9RateControlState) {
	if rc == nil || !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if prev.enabled && prev.mode != RateControlCBR {
		rc.framesTillGF = prev.framesTillGF
		return
	}
	if rc.framesTillGF == 0 {
		rc.framesTillGF = rc.runtimeOnePassVBRGoldenInterval()
	}
}

// runtimeOnePassVBRGoldenInterval ports the non-cyclic baseline_gf_interval
// recompute in libvpx vp9_set_gf_update_one_pass_vbr
// (vp9/encoder/vp9_ratectrl.c:2086-2087): when frames_till_gf_update_due hits
// 0 and aq_mode != CYCLIC_REFRESH_AQ,
//
//	rc->baseline_gf_interval =
//	    VPXMIN(20, VPXMAX(10, (rc->min_gf_interval + rc->max_gf_interval) / 2));
//
// The interval is recomputed from min/max each cycle rather than read back
// from the stored baseline, and is clamped into [10, 20] — not into
// [min_gf_interval, max_gf_interval]. The later avg_frame_low_motion /
// rate_err adjustments (vp9_ratectrl.c:2092-2114) only apply once
// current_video_frame > 30, so the steady-state realtime cadence is this
// midpoint-clamped value.
func (rc *vp9RateControlState) runtimeOnePassVBRGoldenInterval() uint8 {
	minGF := int(rc.minGFInterval)
	maxGF := int(rc.maxGFInterval)
	if minGF == 0 {
		minGF = encoder.MinGFInterval
	}
	if maxGF == 0 {
		maxGF = encoder.MaxGFInterval
	}
	interval := min(max((minGF+maxGF)/2, 10), 20)
	return uint8(interval)
}

// postOnePassVBRRefresh mirrors the golden countdown decrement in
// update_golden_frame_stats (vp9_ratectrl.c:1759-1784): a golden-refresh frame
// still decrements frames_till_gf_update_due, and so does a non-altref frame;
// only a pure altref frame leaves the countdown alone. The re-seed itself is
// performed at frame begin by setGFUpdateOnePassVBR (vp9_ratectrl.c:2115),
// mirroring libvpx's begin-of-frame vp9_set_gf_update_one_pass_vbr.
func (rc *vp9RateControlState) postOnePassVBRRefresh(refreshFlags uint8) {
	if !rc.enabled || rc.mode == RateControlCBR {
		return
	}
	if refreshFlags&(1<<vp9GoldenRefSlot) != 0 ||
		refreshFlags&(1<<vp9AltRefSlot) == 0 {
		if rc.framesTillGF > 0 {
			rc.framesTillGF--
		}
	}
}

func (rc *vp9RateControlState) cbrActiveWorstQuantizer(intraOnly bool, frameIndex int) int {
	worst := int(rc.worstQuality)
	if intraOnly {
		return worst
	}
	if rc.forceMaxQ {
		return worst
	}
	bufferLevel := rc.bufferLevelBits
	criticalLevel := rc.bufferOptimalBits >> 3
	ambientQP := int(rc.avgFrameQIndexInter)
	if frameIndex < 5 {
		if int(rc.avgFrameQIndexKey) < ambientQP {
			ambientQP = int(rc.avgFrameQIndexKey)
		}
	}
	activeWorst := min((ambientQP*5)>>2, worst)
	if bufferLevel > rc.bufferOptimalBits {
		maxAdjustmentDown := activeWorst / 3
		if maxAdjustmentDown > 0 {
			step := (rc.bufferSizeBits - rc.bufferOptimalBits) / maxAdjustmentDown
			adjustment := 0
			if step > 0 {
				adjustment = (bufferLevel - rc.bufferOptimalBits) / step
			}
			activeWorst -= adjustment
		}
	} else if bufferLevel > criticalLevel {
		if criticalLevel > 0 {
			step := rc.bufferOptimalBits - criticalLevel
			if step > 0 {
				activeWorst = ambientQP + int((int64(worst-ambientQP)*int64(rc.bufferOptimalBits-bufferLevel))/int64(step))
			}
		}
	} else if !rc.disableOvershootMaxQCBR {
		// DisableOvershootMaxQCBR (VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR)
		// suppresses the promotion to worstQuality on overshoot. With it
		// disabled, the buffer-driven active-worst remains in force even
		// while the buffer is in the critical region.
		activeWorst = worst
	}
	return activeWorst
}

// adjustCBRQuantizer ports libvpx adjust_q_cbr (vp9_ratectrl.c:679-700): the
// anti-resonance clamp between the previous two frame Qs applies unless
// gf_cbr_boost_pct is set AND the frame refreshes golden/altref. With
// gf_cbr_boost_pct == 0 the clamp applies on every inter frame, including
// golden-refresh frames. The reset_high_source_sad guard is not modeled here.
func (rc *vp9RateControlState) adjustCBRQuantizer(q int, refreshFlags uint8) int {
	// libvpx: !gf_cbr_boost_pct || !(refresh_alt_ref_frame || refresh_golden_frame).
	boostGate := rc.gfCBRBoostPct == 0 ||
		refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) == 0
	if rc.rc1Frame*rc.rc2Frame == -1 && rc.q1Frame != rc.q2Frame && boostGate {
		low := int(rc.q1Frame)
		high := int(rc.q2Frame)
		if low > high {
			low, high = high, low
		}
		qClamp := min(max(q, low), high)
		if rc.rc1Frame == -1 && q > qClamp {
			q = (q + qClamp) >> 1
		} else {
			q = qClamp
		}
	}
	best := int(rc.bestQuality)
	worst := int(rc.worstQuality)
	return min(max(q, best), worst)
}

func (rc *vp9RateControlState) rateFactorLevel(intraOnly bool, refreshFlags uint8) int {
	if intraOnly {
		return encoder.RateFactorKFStd
	}
	if vp9BoostedInterRefresh(refreshFlags) && rc.mode != RateControlCBR {
		return encoder.RateFactorGFARFStd
	}
	return encoder.RateFactorInterNormal
}

func (rc *vp9RateControlState) rateCorrectionFactor(intraOnly bool, refreshFlags uint8) float64 {
	level := rc.rateFactorLevel(intraOnly, refreshFlags)
	return encoder.NormalizeRateCorrectionFactor(rc.rateCorrectionFactors[level])
}

func (rc *vp9RateControlState) setRateCorrectionFactor(intraOnly bool, refreshFlags uint8, factor float64) {
	level := rc.rateFactorLevel(intraOnly, refreshFlags)
	rc.rateCorrectionFactors[level] = min(max(factor, encoder.MinBPBFactor), encoder.MaxBPBFactor)
}

func (rc *vp9RateControlState) updateRateCorrectionFactor(actualBits int, qindex int, intraOnly bool, refreshFlags uint8, macroblocks int, cyclic *encoder.CyclicRefreshState, dampedRFLevel int) {
	if actualBits <= 0 || macroblocks <= 0 {
		return
	}
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	// libvpx indexes damped_adjustment by the gf_group rf_level
	// (cpi->twopass.gf_group.rf_level[cpi->twopass.gf_group.index]), NOT by
	// the frame-type rate-factor level used for the rate_correction_factors
	// get/set. In one-pass mode the gf_group is never populated, so this is
	// always INTER_NORMAL regardless of frame type; threading it in keeps the
	// two-pass path on its real rf_level.
	// libvpx: vp9/encoder/vp9_ratectrl.c:755-756, 784-786.
	rateCorrectionFactor := rc.rateCorrectionFactor(intraOnly, refreshFlags)
	projectedBits := encoder.EstimatedBitsAtQ(intraOnly, qindex, macroblocks, rateCorrectionFactor)
	if cyclic != nil && cyclic.Enabled && cyclic.Apply && !intraOnly {
		projectedBits = vp9CyclicRefreshEstimateBitsAtQ(qindex, macroblocks, rateCorrectionFactor, cyclic)
	}
	correctionFactor := 100
	if projectedBits > encoder.FrameOverhead {
		correctionFactor = int((100 * int64(actualBits)) / int64(projectedBits))
	}
	adjustmentLimit := 1.0
	if rc.dampedAdjustment[dampedRFLevel] {
		adjustmentLimit = 0.25 + 0.5*math.Min(1, math.Abs(math.Log10(0.01*float64(correctionFactor))))
	} else {
		rc.dampedAdjustment[dampedRFLevel] = true
	}

	rc.q2Frame = rc.q1Frame
	rc.q1Frame = uint8(qindex)
	rc.rc2Frame = rc.rc1Frame
	if correctionFactor > 110 {
		rc.rc1Frame = -1
	} else if correctionFactor < 90 {
		rc.rc1Frame = 1
	} else {
		rc.rc1Frame = 0
	}
	if rc.rc1Frame == -1 && rc.rc2Frame == 1 && correctionFactor > 1000 {
		rc.rc2Frame = 0
	}

	if correctionFactor > 102 {
		correctionFactor = int(100 + float64(correctionFactor-100)*adjustmentLimit)
		// libvpx evaluates (rate_correction_factor * correction_factor) / 100
		// with correction_factor an int (vp9/encoder/vp9_ratectrl.c:814): the
		// multiply happens before the divide-by-100, so the IEEE-754 rounding
		// differs from rcf *= cf/100 (which rounds cf/100 first). Over a long
		// CBR run this accumulates and can flip the regulated q by one qindex.
		rateCorrectionFactor = rateCorrectionFactor * float64(correctionFactor) / 100
	} else if correctionFactor < 99 {
		correctionFactor = int(100 - float64(100-correctionFactor)*adjustmentLimit)
		// libvpx: vp9/encoder/vp9_ratectrl.c:822, same (rcf * cf) / 100 ordering.
		rateCorrectionFactor = rateCorrectionFactor * float64(correctionFactor) / 100
	}
	rc.setRateCorrectionFactor(intraOnly, refreshFlags, rateCorrectionFactor)
}

// vp9CyclicRefreshEstimateBitsAtQ mirrors libvpx's
// vp9_cyclic_refresh_estimate_bits_at_q (vp9_aq_cyclicrefresh.c:105-129).
// It estimates the encoded bits by taking a segment-weighted average of the
// base and boosted qindices using the realized (ActualNumSeg{1,2}Blocks)
// counts from the just-encoded frame.
func vp9CyclicRefreshEstimateBitsAtQ(baseQindex int, macroblocks int, correctionFactor float64, cr *encoder.CyclicRefreshState) int {
	if cr == nil || macroblocks <= 0 {
		return encoder.EstimatedBitsAtQ(false, baseQindex, macroblocks, correctionFactor)
	}
	num8x8 := macroblocks << 2
	if num8x8 <= 0 {
		return encoder.EstimatedBitsAtQ(false, baseQindex, macroblocks, correctionFactor)
	}
	w1 := float64(cr.ActualNumSeg1Blocks) / float64(num8x8)
	w2 := float64(cr.ActualNumSeg2Blocks) / float64(num8x8)
	if w1 < 0 {
		w1 = 0
	}
	if w2 < 0 {
		w2 = 0
	}
	if w1+w2 > 1 {
		// Defensive: corrupt counts shouldn't blow up the estimate.
		scale := 1 / (w1 + w2)
		w1 *= scale
		w2 *= scale
	}
	q1 := baseQindex + cr.QIndexDelta[encoder.CyclicRefreshSegmentBoost1]
	q2 := baseQindex + cr.QIndexDelta[encoder.CyclicRefreshSegmentBoost2]
	if q1 < 0 {
		q1 = 0
	} else if q1 > 255 {
		q1 = 255
	}
	if q2 < 0 {
		q2 = 0
	} else if q2 > 255 {
		q2 = 255
	}
	// FrameType is always inter here (cyclic refresh is inter-only), so intraOnly=false.
	base := float64(encoder.EstimatedBitsAtQ(false, baseQindex, macroblocks, correctionFactor))
	seg1 := float64(encoder.EstimatedBitsAtQ(false, q1, macroblocks, correctionFactor))
	seg2 := float64(encoder.EstimatedBitsAtQ(false, q2, macroblocks, correctionFactor))
	est := (1-w1-w2)*base + w1*seg1 + w2*seg2
	if est < 0 {
		return encoder.FrameOverhead
	}
	if est > float64(maxInt()) {
		return maxInt()
	}
	v := int(est + 0.5) // round() equivalent for non-negative values.
	if v < encoder.FrameOverhead {
		return encoder.FrameOverhead
	}
	return v
}

func vp9BoostedInterRefresh(refreshFlags uint8) bool {
	return refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0
}

func vp9DefaultMinGFInterval(timing timingState) int {
	num := int64(timing.timebaseDen)
	den := int64(timing.timebaseNum) * int64(timing.frameDuration) * 8
	interval := encoder.RoundedRatio(num, den)
	if interval < encoder.MinGFInterval {
		return encoder.MinGFInterval
	}
	if interval > encoder.MaxGFInterval {
		return encoder.MaxGFInterval
	}
	return interval
}

func vp9DefaultMaxGFInterval(timing timingState, minInterval int) int {
	num := int64(timing.timebaseDen) * 3
	den := int64(timing.timebaseNum) * int64(timing.frameDuration) * 4
	interval := min(encoder.RoundedRatio(num, den), encoder.MaxGFInterval)
	interval += interval & 1
	if interval < minInterval {
		interval = minInterval
	}
	return interval
}

// vp9DefaultMinGFIntervalAtRate mirrors libvpx vp9_rc_get_default_min_gf_interval
// (vp9/encoder/vp9_ratectrl.c:348) verbatim, including the 4K-20fps factor_safe
// guard. The dynamic adjust_frame_rate path feeds a floating framerate, so this
// takes the rate directly rather than reconstructing an integer timebase.
func vp9DefaultMinGFIntervalAtRate(width, height int, framerate float64) int {
	const factorSafe = 3840.0 * 2160.0 * 20.0
	factor := float64(width) * float64(height) * framerate
	defaultInterval := vp9ClampGFInterval(int(roundHalfAwayFromZero(framerate*0.125)),
		encoder.MinGFInterval, encoder.MaxGFInterval)
	if factor <= factorSafe {
		return defaultInterval
	}
	scaled := int(roundHalfAwayFromZero(float64(encoder.MinGFInterval) * factor / factorSafe))
	if scaled > defaultInterval {
		return scaled
	}
	return defaultInterval
}

// vp9DefaultMaxGFIntervalAtRate mirrors libvpx vp9_rc_get_default_max_gf_interval
// (vp9/encoder/vp9_ratectrl.c:367) verbatim.
func vp9DefaultMaxGFIntervalAtRate(framerate float64, minInterval int) int {
	interval := min(encoder.MaxGFInterval, int(roundHalfAwayFromZero(framerate*0.75)))
	interval += interval & 1
	if interval < minInterval {
		return minInterval
	}
	return interval
}

func vp9ClampGFInterval(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// roundHalfAwayFromZero matches C round(): ties go away from zero. The framerate
// inputs here are always positive, so this reduces to floor(x + 0.5).
func roundHalfAwayFromZero(x float64) float64 {
	return math.Floor(x + 0.5)
}

func (rc *vp9RateControlState) updateQHistory(qindex int, intraOnly bool, refreshFlags uint8, showFrame bool) {
	rc.updateQHistoryWithAltRef(qindex, intraOnly, refreshFlags, showFrame, false)
}

func (rc *vp9RateControlState) updateQHistoryWithAltRef(qindex int, intraOnly bool, refreshFlags uint8, showFrame bool, altRefEnabled bool) {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	q := uint8(qindex)
	rc.q1Frame = q
	if intraOnly {
		rc.lastQKey = q
		rc.avgFrameQIndexKey = uint8((3*int(rc.avgFrameQIndexKey) + qindex + 2) >> 2)
		rc.lastBoostedQIndex = q
		rc.framesSinceKey = 0
	} else if refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) == 0 {
		rc.lastQInter = q
		rc.avgFrameQIndexInter = uint8((3*int(rc.avgFrameQIndexInter) + qindex + 2) >> 2)
	}
	if !intraOnly && refreshFlags&(1<<vp9GoldenRefSlot|1<<vp9AltRefSlot) != 0 {
		rc.lastBoostedQIndex = q
	}
	if showFrame {
		rc.incrementFramesSinceKey()
	}
	refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
	refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
	if intraOnly || refreshGolden || (refreshAlt && altRefEnabled) {
		rc.framesSinceGolden = 0
	} else if showFrame && !refreshAlt && rc.framesSinceGolden != ^uint16(0) {
		rc.framesSinceGolden++
	}
}

func (rc *vp9RateControlState) incrementFramesSinceKey() {
	if rc.framesSinceKey != ^uint16(0) {
		rc.framesSinceKey++
	}
}
