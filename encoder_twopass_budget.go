package govpx

import "math"

func (e *VP8Encoder) pass2MaybeArmAltRefPending(currentFrame uint64, currentPTS uint64, keyFrame bool) {
	_ = keyFrame
	interval, pending := e.pass2AltRefPendingPlan(currentFrame)
	e.pass2ArmAltRefPending(currentPTS, interval, pending)
}

func (e *VP8Encoder) pass2AltRefPendingPlan(currentFrame uint64) (int, bool) {
	if !e.twoPass.enabled() {
		return 0, false
	}
	if !e.opts.AutoAltRef || e.opts.ErrorResilient || e.opts.LookaheadFrames <= 1 {
		return 0, false
	}
	if e.sourceAltRefPending || e.sourceAltRefActive {
		return 0, false
	}
	if e.framesTillAltRefFrame > 0 {
		return 0, false
	}
	if e.twoPass.gfGroupValid && e.twoPass.framesTillGFUpdate > 0 {
		return 0, false
	}
	framesToKey := e.twoPass.framesToKey(currentFrame, e.opts.KeyFrameInterval)
	if framesToKey <= 0 {
		return 0, false
	}
	maxGFInterval := e.opts.KeyFrameInterval
	if maxGFInterval <= 0 || maxGFInterval > e.opts.LookaheadFrames-1 {
		maxGFInterval = e.opts.LookaheadFrames - 1
	}
	return e.twoPass.pass2DetectARFPending(currentFrame, framesToKey, true, maxGFInterval)
}

func (e *VP8Encoder) pass2ArmAltRefPending(currentPTS uint64, interval int, pending bool) {
	if !pending {
		return
	}
	// libvpx alt_ref_source identifies the future lookahead entry that
	// will become the hidden ARF source. govpx uses PTS as the
	// identifier; without an exact future PTS we fall back to a
	// per-frame offset on the assumption of constant duration. The
	// driver matches by PTS via isSrcFrameAltRef, so as long as we
	// arrive at the same value when the frame is later popped from
	// the lookahead, scheduling is consistent.
	futurePTS := currentPTS + uint64(interval)
	e.scheduleAltRefSource(futurePTS, interval)
}

func (t *twoPassState) finishFrame(actualBits int) {
	if !t.enabled() {
		return
	}
	if t.frameIndex < uint64(len(t.stats)) {
		t.errorLeft -= t.modifiedError(t.stats[t.frameIndex])
		if t.errorLeft < 0 {
			t.errorLeft = 0
		}
	}
	// libvpx onyx_if.c Pass2Encode: bits_left -= 8 * size; bits_left +=
	// (target_bandwidth * vbrmin_section/100) / framerate. The minimum
	// is the additive credit equal to min_frame_bandwidth (in libvpx
	// shorthand) per visible frame.
	t.bitsLeft -= int64(actualBits)
	t.bitsLeft += int64(t.minFrameBandwidth)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	// libvpx onyx_if.c update_rd_ref_frame_probs / update_golden_frame
	// statistics: frames_since_golden and frames_till_gf_update_due
	// advance per visible frame. KF/GF refresh frames reset
	// frames_since_golden to 0 in update_golden_frame_stats and do NOT
	// increment it; the increment only fires in the
	// `!cpi->common.refresh_alt_ref_frame` else branch. Mirror that
	// gating so the assign_std_frame_bits caller observes
	// frames_since_golden=0 for the first inter frame after a GF
	// refresh (libvpx-parity).
	if !t.errorResilient {
		if t.framesTillGFUpdate > 0 {
			t.framesTillGFUpdate--
		}
		if t.currentFrameIsGFRefresh {
			t.framesSinceGolden = 0
		} else {
			t.framesSinceGolden++
		}
	}
	if t.framesToKeyRemaining > 0 {
		t.framesToKeyRemaining--
	}
	t.frameIndex++
	t.currentFrameIsGFRefresh = false
}

func (t *twoPassState) chargeAltRefFrameBits(actualBits int) {
	t.chargeAltRefFrameBitsWithProjection(actualBits, actualBits)
}

func (t *twoPassState) chargeAltRefFrameBitsWithProjection(actualBits int, projectedBits int) {
	if !t.enabled() {
		return
	}
	t.bitsLeft -= int64(actualBits)
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	// libvpx encode_frame_to_data_rate treats hidden ARFs as GF/ARF refresh
	// frames for GF-group residual accounting:
	//
	//   twopass.gf_group_bits += this_frame_target - projected_frame_size
	//
	// The actual byte size still debits twopass.bits_left in Pass2Encode, but
	// the section allocator uses projected_frame_size to put ARF underspend back
	// into the visible frames that follow.
	if t.gfGroupValid && t.altRefTarget > 0 {
		t.gfGroupBits += int64(t.altRefTarget - projectedBits)
		if t.gfGroupBits < 0 {
			t.gfGroupBits = 0
		}
	}
}

// libvpxEstimateMaxQ ports the libvpx vp8/encoder/firstpass.c
// estimate_max_q Q-search loop: walk Q from maxq_min_limit upward
// computing
//
//	bits_per_mb = err_correction * speed_correction * est_max_qcorrection
//	            * section_max_qfactor * (vp8_bits_per_mb[INTER][Q] + overhead)
//
// where err_correction is `libvpxCalcCorrectionFactor(err_per_mb,
// 150.0, 0.40, 0.90, Q)` and overhead decays by 0.98 per Q step. The
// search returns the lowest Q for which `bits_per_mb_at_q <=
// target_norm_bits_per_mb`. target_norm_bits_per_mb derives from
// section_target_bandwidth via libvpx's overflow-aware
// `(512 * section_target_bandwidth) / num_mbs` formula. When
// `section_target_bandwidth <= 0`, libvpx returns
// `maxq_max_limit` immediately.
//
// The CQ floor (`USAGE_CONSTRAINED_QUALITY` -> max(Q, cq_target_quality))
// is left to callers since it depends on encoder mode state.
func libvpxEstimateMaxQ(numMBs int, sectionTargetBandwidth int, overheadBits int, errPerMB float64, speedCorrection float64, estMaxQCorrection float64, sectionMaxQFactor float64, maxqMinLimit int, maxqMaxLimit int) int {
	if numMBs <= 0 || maxqMaxLimit <= maxqMinLimit {
		return maxqMaxLimit
	}
	if sectionTargetBandwidth <= 0 {
		return maxqMaxLimit
	}
	var targetNormBitsPerMB int
	if sectionTargetBandwidth < (1 << 20) {
		targetNormBitsPerMB = (512 * sectionTargetBandwidth) / numMBs
	} else {
		targetNormBitsPerMB = 512 * (sectionTargetBandwidth / numMBs)
	}
	overheadBitsPerMB := overheadBits / numMBs
	overheadBitsPerMB = int(float64(overheadBitsPerMB) * math.Pow(0.98, float64(maxqMinLimit)))
	for Q := maxqMinLimit; Q < maxqMaxLimit; Q++ {
		errCorrection := libvpxCalcCorrectionFactor(errPerMB, 150.0, 0.40, 0.90, Q)
		baseBitsPerMB := 0
		if uint(Q) < uint(len(libvpxBitsPerMB[1])) {
			baseBitsPerMB = libvpxBitsPerMB[1][Q]
		}
		baseBitsPerMB += overheadBitsPerMB
		bitsPerMBAtQ := int(0.5 + errCorrection*speedCorrection*estMaxQCorrection*sectionMaxQFactor*float64(baseBitsPerMB))
		overheadBitsPerMB = int(float64(overheadBitsPerMB) * 0.98)
		if bitsPerMBAtQ <= targetNormBitsPerMB {
			return Q
		}
	}
	return maxqMaxLimit
}

// libvpxEstimateModeMVCost ports vp8/encoder/firstpass.c
// estimate_modemvcost. The returned value is normalized to the same
// bits*512 scale consumed by libvpxEstimateMaxQ:
//
//	av_pct_inter = fpstats->pcnt_inter / fpstats->count
//	av_pct_motion = fpstats->pcnt_motion / fpstats->count
//	mv_cost = ((int)(new_mv_count / count) * 8) << 9
//	mode_cost = int(weighted mode entropy * MBs) * 512
func libvpxEstimateModeMVCost(stats FirstPassFrameStats, numMBs int) int {
	if numMBs <= 0 || stats.Count <= 0 {
		return 0
	}
	avPctInter := stats.PcntInter / stats.Count
	avPctMotion := stats.PcntMotion / stats.Count
	avIntra := 1.0 - avPctInter
	zzCost := libvpxBitCost(avPctInter - avPctMotion)
	motionCost := libvpxBitCost(avPctMotion)
	intraCost := libvpxBitCost(avIntra)
	mvCost := (int(stats.NewMVCount/stats.Count) * 8) << 9
	modeEntropy := ((avPctInter - avPctMotion) * zzCost) +
		(avPctMotion * motionCost) +
		(avIntra * intraCost)
	modeCost := int64(modeEntropy*float64(numMBs)) * 512
	return mvCost + int(modeCost)
}

func libvpxBitCost(prob float64) float64 {
	if prob > 0.000122 {
		return -math.Log(prob) / math.Log(2.0)
	}
	return 13.0
}

// libvpxEstimateQ ports the libvpx vp8/encoder/firstpass.c
// estimate_q Q-search loop (the section-target Q probe used inside
// new_section_complete / Pass2Encode). It walks Q from 0 upward
// computing
//
//	bits_per_mb = err_correction * speed_correction *
//	              est_max_qcorrection * vp8_bits_per_mb[INTER][Q]
//
// (no overhead/section_max_qfactor scaling, distinguishing it from
// estimate_max_q). Returns the lowest Q whose bits_per_mb_at_q is at
// or below the target.
func libvpxEstimateQ(numMBs int, sectionTargetBandwidth int, errPerMB float64, speedCorrection float64, estMaxQCorrection float64) int {
	if min(numMBs, sectionTargetBandwidth) <= 0 {
		return vp8MaxQIndex
	}
	var targetNormBitsPerMB int
	if sectionTargetBandwidth < (1 << 20) {
		targetNormBitsPerMB = (512 * sectionTargetBandwidth) / numMBs
	} else {
		targetNormBitsPerMB = 512 * (sectionTargetBandwidth / numMBs)
	}
	for Q := range len(libvpxBitsPerMB[1]) {
		errCorrection := libvpxCalcCorrectionFactor(errPerMB, 150.0, 0.40, 0.90, Q)
		bitsPerMBAtQ := int(0.5 + errCorrection*speedCorrection*estMaxQCorrection*float64(libvpxBitsPerMB[1][Q]))
		if bitsPerMBAtQ <= targetNormBitsPerMB {
			return Q
		}
	}
	return len(libvpxBitsPerMB[1]) - 1
}

// libvpxEstimateKFGroupQ ports the libvpx vp8/encoder/firstpass.c
// estimate_kf_group_q worst-case KF-group Q estimator. It mirrors:
//
//	pow_highq = (POW1 < 0.6) ? POW1+0.3 : 0.90
//	pow_lowq  = (POW1 < 0.7) ? POW1+0.1 : 0.80
//	if long_rolling_target_bits <= 0:
//	  current_spend_ratio = 10.0
//	else:
//	  current_spend_ratio = clamp(long_rolling_actual/long_rolling_target,
//	                              0.1, 10.0)
//	iiratio_correction_factor =
//	  max(0.5, 1.0 - (group_iiratio - 6.0) * 0.1)
//	combined = speed_correction * iiratio_correction_factor *
//	            current_spend_ratio
//	for Q in 0..MAXQ:
//	  cf = calc_correction_factor(err_per_mb, 150, pow_lowq, pow_highq, Q)
//	  bits = cf * combined * vp8_bits_per_mb[INTER][Q]
//	  if bits <= target: break
//	while (bits > target && Q < MAXQ*2):
//	  bits = 0.96 * bits; Q++
//
// POW1 in libvpx is `oxcf.two_pass_vbrbias / 100.0`; callers pass it
// directly. Returns MAXQ*2 when the budget is non-positive (libvpx's
// `if (target_norm_bits_per_mb <= 0) return MAXQ * 2;`).
func libvpxEstimateKFGroupQ(numMBs int, sectionTargetBandwidth int, errPerMB float64, groupIIRatio float64, vbrBiasPct int, longRollingActualBits int, longRollingTargetBits int, speedCorrection float64) int {
	const maxQ = vp8MaxQIndex + 1
	if numMBs <= 0 {
		return maxQ * 2
	}
	targetNormBitsPerMB := (512 * sectionTargetBandwidth) / numMBs
	if targetNormBitsPerMB <= 0 {
		return maxQ * 2
	}
	pow1 := float64(vbrBiasPct) / 100.0
	powHighQ := 0.90
	if pow1 < 0.6 {
		powHighQ = pow1 + 0.3
	}
	powLowQ := 0.80
	if pow1 < 0.7 {
		powLowQ = pow1 + 0.1
	}
	currentSpendRatio := 10.0
	if longRollingTargetBits > 0 {
		currentSpendRatio = float64(longRollingActualBits) / float64(longRollingTargetBits)
		if currentSpendRatio > 10.0 {
			currentSpendRatio = 10.0
		} else if currentSpendRatio < 0.1 {
			currentSpendRatio = 0.1
		}
	}
	iiratioCorrection := 1.0 - (groupIIRatio-6.0)*0.1
	if iiratioCorrection < 0.5 {
		iiratioCorrection = 0.5
	}
	combined := speedCorrection * iiratioCorrection * currentSpendRatio
	bitsPerMBAtQ := 0
	Q := 0
	for ; Q < maxQ; Q++ {
		errCorrection := libvpxCalcCorrectionFactor(errPerMB, 150.0, powLowQ, powHighQ, Q)
		bitsPerMBAtQ = int(0.5 + errCorrection*combined*float64(libvpxBitsPerMB[1][Q]))
		if bitsPerMBAtQ <= targetNormBitsPerMB {
			break
		}
	}
	for bitsPerMBAtQ > targetNormBitsPerMB && Q < maxQ*2 {
		bitsPerMBAtQ = int(0.96 * float64(bitsPerMBAtQ))
		Q++
	}
	return Q
}

// libvpxCalcCorrectionFactor ports the libvpx
// vp8/encoder/firstpass.c calc_correction_factor:
//
//	error_term = err_per_mb / err_devisor
//	power_term = clamp(pt_low + Q*0.01, +inf, pt_high)
//	correction_factor = pow(error_term, power_term)
//	clamp(correction_factor, 0.05, 5.0)
//
// Used by estimate_max_q / estimate_min_q / estimate_q to compute
// the per-Q rate model correction.
func libvpxCalcCorrectionFactor(errPerMB float64, errDevisor float64, ptLow float64, ptHigh float64, Q int) float64 {
	if errDevisor == 0 {
		errDevisor = 1.0
	}
	errorTerm := errPerMB / errDevisor
	powerTerm := ptLow + float64(Q)*0.01
	if powerTerm > ptHigh {
		powerTerm = ptHigh
	}
	cf := math.Pow(errorTerm, powerTerm)
	if cf < 0.05 {
		return 0.05
	}
	if cf > 5.0 {
		return 5.0
	}
	return cf
}

// libvpxEstimateMaxQRollingRatioAdjustment ports the rolling
// est_max_qcorrection_factor update from estimate_max_q:
//
//	rolling_ratio = rolling_actual_bits / rolling_target_bits
//	if ratio < 0.95: factor -= 0.005
//	if ratio > 1.05: factor += 0.005
//	clamp(factor, 0.1, 10.0)
//
// Returns the updated factor. Caller passes the previous factor and
// the rolling stats; the inner libvpx gate
// `(rolling_target_bits > 0) && (active_worst_quality < worst_quality)`
// is enforced by the caller.
func libvpxEstimateMaxQRollingRatioAdjustment(prevFactor float64, rollingActualBits int, rollingTargetBits int) float64 {
	if rollingTargetBits <= 0 {
		return prevFactor
	}
	ratio := float64(rollingActualBits) / float64(rollingTargetBits)
	factor := prevFactor
	if ratio < 0.95 {
		factor -= 0.005
	} else if ratio > 1.05 {
		factor += 0.005
	}
	if factor < 0.1 {
		factor = 0.1
	}
	if factor > 10.0 {
		factor = 10.0
	}
	return factor
}

// libvpxSectionStats accumulates the libvpx FIRSTPASS_STATS section
// totals used by find_next_key_frame and define_gf_group to derive
// section_intra_rating and section_max_qfactor. Mirrors libvpx's
// FIRSTPASS_STATS accumulate_stats / avg_stats pattern: callers
// call addFrame for each frame in the section and then call avg()
// once before reading sectionIntra / sectionCoded.
type libvpxSectionStats struct {
	count        int
	sectionIntra float64
	sectionCoded float64
}

// addFrame mirrors libvpx's accumulate_stats over the per-frame
// FIRSTPASS_STATS intra_error / coded_error fields.
func (s *libvpxSectionStats) addFrame(intraError, codedError float64) {
	s.count++
	s.sectionIntra += intraError
	s.sectionCoded += codedError
}

// avg mirrors libvpx's avg_stats: divides each accumulator by
// `count`. Callers should call this exactly once before reading
// sectionIntra / sectionCoded.
func (s *libvpxSectionStats) avg() {
	if s.count <= 0 {
		return
	}
	s.sectionIntra /= float64(s.count)
	s.sectionCoded /= float64(s.count)
}

// libvpxSectionIntraRating ports the libvpx vp8/encoder/firstpass.c
// section_intra_rating computation:
//
//	section_intra_rating = sectionIntra / DOUBLE_DIVIDE_CHECK(sectionCoded)
//
// where DOUBLE_DIVIDE_CHECK(x) returns 1.0 when |x|<1e-12 and x
// otherwise. Returns 0 when both error totals are 0 (libvpx asserts
// non-empty section in normal flow). The libvpx field is unsigned int,
// so the result is truncated to non-negative.
func libvpxSectionIntraRating(sectionIntra, sectionCoded float64) int {
	denom := sectionCoded
	if denom < 1e-12 && denom > -1e-12 {
		denom = 1.0
	}
	v := sectionIntra / denom
	if v < 0 {
		return 0
	}
	return int(v)
}

// libvpxSectionMaxQFactor ports the libvpx vp8/encoder/firstpass.c
// section_max_qfactor formula:
//
//	Ratio = sectionIntra / DOUBLE_DIVIDE_CHECK(sectionCoded)
//	section_max_qfactor = 1.0 - ((Ratio - 10.0) * 0.025)
//	if section_max_qfactor < 0.80: section_max_qfactor = 0.80
//
// The 0.80 floor mirrors libvpx exactly. Returns 1.0 when both error
// totals are 0 (libvpx's DOUBLE_DIVIDE_CHECK fallback).
func libvpxSectionMaxQFactor(sectionIntra, sectionCoded float64) float64 {
	denom := sectionCoded
	if denom < 1e-12 && denom > -1e-12 {
		denom = 1.0
	}
	ratio := sectionIntra / denom
	factor := 1.0 - ((ratio - 10.0) * 0.025)
	if factor < 0.80 {
		factor = 0.80
	}
	return factor
}

// libvpxAssignStdFrameBits ports the libvpx vp8/encoder/firstpass.c
// assign_std_frame_bits per-frame allocator inside a GF group:
//
//	err_fraction = modified_err / gf_group_error_left
//	target = gf_group_bits * err_fraction
//	clamp(target, 0, min(max_bits, gf_group_bits))
//	target += min_frame_bandwidth
//	if (frames_since_golden & 1) && frames_till_gf_update_due>0:
//	    target += alt_extra_bits
//
// Returns the per-frame bit target. Callers are expected to update
// gf_group_error_left and gf_group_bits themselves so the allocator
// stays a pure function.
func libvpxAssignStdFrameBits(modifiedErr float64, gfGroupErrorLeft float64, gfGroupBits int64, maxBitsPerFrame int, minFrameBandwidth int, framesSinceGolden int, framesTillGFUpdateDue int, altExtraBits int) int {
	errFraction := 0.0
	if gfGroupErrorLeft > 0 && gfGroupBits > 0 {
		errFraction = modifiedErr / gfGroupErrorLeft
	}
	target := int(float64(gfGroupBits) * errFraction)
	if target < 0 {
		target = 0
	} else {
		if maxBitsPerFrame > 0 && target > maxBitsPerFrame {
			target = maxBitsPerFrame
		}
		if gfGroupBits > 0 && int64(target) > gfGroupBits {
			target = int(gfGroupBits)
		}
	}
	target += minFrameBandwidth
	if (framesSinceGolden&0x01) != 0 && framesTillGFUpdateDue > 0 {
		target += altExtraBits
	}
	if target < 0 {
		return 0
	}
	return target
}

// libvpxFrameMaxBitsCBR ports the CBR branch of libvpx's
// vp8/encoder/firstpass.c frame_max_bits:
//
//	max_bits = av_per_frame_bandwidth * (two_pass_vbrmax_section / 100)
//	if buffer_level < optimal:
//	  buffer_fullness_ratio = buffer_level / optimal
//	  max_bits *= buffer_fullness_ratio
//	  min_max_bits = min(av_per_frame_bandwidth>>2, max_bits>>2 (pre-scale))
//	  max_bits = max(max_bits, min_max_bits)
//
// avPerFrameBandwidth is libvpx's `cpi->av_per_frame_bandwidth`, which
// equals govpx's `bitsPerFrame` in steady state. vbrMaxSection is
// `cpi->oxcf.two_pass_vbrmax_section` (govpx's
// EncoderOptions.TwoPassMaxPct). Returns 0 when the budget would be
// negative.
func libvpxFrameMaxBitsCBR(avPerFrameBandwidth int, vbrMaxSection int, bufferLevel int, optimalBufferLevel int) int {
	if min(avPerFrameBandwidth, vbrMaxSection) <= 0 {
		return 0
	}
	maxBits := avPerFrameBandwidth * vbrMaxSection / 100
	if optimalBufferLevel > 0 && bufferLevel < optimalBufferLevel {
		// Capture the pre-scale max_bits>>2 for the min floor calculation
		// (libvpx evaluates the min before the buffer-ratio scale).
		minMaxBits := avPerFrameBandwidth >> 2
		if (maxBits >> 2) < minMaxBits {
			minMaxBits = maxBits >> 2
		}
		maxBits = max(int(float64(maxBits)*float64(bufferLevel)/float64(optimalBufferLevel)), minMaxBits)
	}
	if maxBits < 0 {
		return 0
	}
	return maxBits
}

// libvpxFrameMaxBitsVBR ports the VBR branch of libvpx's frame_max_bits:
//
//	max_bits = (bits_left / frames_left) * (two_pass_vbrmax_section / 100)
//
// Returns 0 when bits_left or frames_left are non-positive.
func libvpxFrameMaxBitsVBR(bitsLeft int64, framesLeft int64, vbrMaxSection int) int {
	if min(bitsLeft, framesLeft) <= 0 || vbrMaxSection <= 0 {
		return 0
	}
	bitsPerFrame := float64(bitsLeft) / float64(framesLeft)
	maxBits := int(bitsPerFrame * float64(vbrMaxSection) / 100.0)
	if maxBits < 0 {
		return 0
	}
	return maxBits
}

// libvpxGFGroupBits ports the libvpx vp8/encoder/firstpass.c GF-group
// allocation:
//
//	gf_group_bits = kf_group_bits * (gf_group_err / kf_group_error_left)
//
// then clamped to [0, kf_group_bits], then capped at
// `max_bits * baseline_gf_interval`. Returns 0 when kf_group_bits<=0
// or kf_group_error_left<=0.
func libvpxGFGroupBits(kfGroupBits int64, gfGroupErr float64, kfGroupErrorLeft float64, maxBitsPerFrame int, baselineGFInterval int) int64 {
	if kfGroupBits <= 0 || kfGroupErrorLeft <= 0 {
		return 0
	}
	gfGroupBits := min(max(int64(float64(kfGroupBits)*(gfGroupErr/kfGroupErrorLeft)), 0), kfGroupBits)
	if maxBitsPerFrame > 0 && baselineGFInterval > 0 {
		cap := int64(maxBitsPerFrame) * int64(baselineGFInterval)
		if gfGroupBits > cap {
			gfGroupBits = cap
		}
	}
	return gfGroupBits
}

// libvpxGFBitsAllocation ports the libvpx vp8/encoder/firstpass.c
// gf_bits allocator: for the GF (or ARF when isARF=true), pre-clamp
// the boost via the GFQ_ADJUSTMENT scaling, apply min/max caps based
// on baseline_gf_interval, and compute
//
//	gf_bits = Boost * (gf_group_bits / allocation_chunks)
//
// with the libvpx >1000-boost halving guard. The two branches diverge:
//   - ARF (i==0 with source_alt_ref_pending):
//     Boost = (gfu_boost * 3 * GFQ_ADJUSTMENT) / (2 * 100) + interval*50
//     cap = (interval+1)*200, floor = 125
//     allocation_chunks = (interval+1)*100 + Boost
//   - GF: Boost = (gfu_boost * GFQ_ADJUSTMENT) / 100
//     cap = interval*150, floor = 125
//     allocation_chunks = interval*100 + (Boost - 100)
//
// gfuBoost is the libvpx `cpi->gfu_boost` (last_boost-equivalent),
// gfqAdjustment is `vp8_gf_boost_qadjustment[Q]`. interval is
// `baseline_gf_interval`.
func libvpxGFBitsAllocation(isARF bool, gfuBoost int, gfqAdjustment int, gfGroupBits int64, baselineGFInterval int) int {
	if gfGroupBits <= 0 || baselineGFInterval <= 0 { // gfGroupBits is int64; baselineGFInterval is int
		return 0
	}
	var boost, allocationChunks int
	if isARF {
		boost = (gfuBoost * 3 * gfqAdjustment) / (2 * 100)
		boost += baselineGFInterval * 50
		if cap := (baselineGFInterval + 1) * 200; boost > cap {
			boost = cap
		}
		if boost < 125 {
			boost = 125
		}
		allocationChunks = (baselineGFInterval+1)*100 + boost
	} else {
		boost = (gfuBoost * gfqAdjustment) / 100
		if cap := baselineGFInterval * 150; boost > cap {
			boost = cap
		}
		if boost < 125 {
			boost = 125
		}
		allocationChunks = baselineGFInterval*100 + (boost - 100)
	}
	for boost > 1000 {
		boost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			return 0
		}
	}
	if allocationChunks <= 0 {
		return 0
	}
	gfBits := max(int(float64(boost)*(float64(gfGroupBits)/float64(allocationChunks))), 0)
	return gfBits
}

// kfGroupModifiedError ports the inner-loop accumulator
//
//	kf_group_err += calculate_modified_err(cpi, this_frame);
//
// from libvpx vp8/encoder/firstpass.c find_next_key_frame: total
// modified error across the KF group starting at `frame` and lasting
// `framesToKey` frames. Returns 0 when stats are not loaded.
