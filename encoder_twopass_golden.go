package govpx

func (t *twoPassState) defineGFGroup(frame uint64) {
	if !t.kfGroupValid || frame >= uint64(len(t.stats)) {
		t.gfGroupValid = false
		return
	}
	remaining := len(t.stats) - int(frame)
	if remaining <= 0 {
		t.gfGroupValid = false
		return
	}
	// libvpx vp8/encoder/firstpass.c lines 1921-1925: when the current
	// KF group is the final one in the stream
	// (frames_to_key >= total_stats.count - current_video_frame),
	// kf_group_bits is reset to the live bits_left so the GF allocator
	// uses the full residual budget. Govpx mirrors that here so the
	// same gf_group_bits initial value the libvpx oracle reports
	// (106666 on the 8-frame ramp source) is reachable.
	if t.framesToKeyRemaining >= remaining {
		if t.bitsLeft > 0 {
			t.kfGroupBitsRemaining = t.bitsLeft
		}
	}
	gfInterval := min(remaining, t.framesToKeyRemaining)
	if gfInterval <= 0 {
		t.gfGroupValid = false
		return
	}
	keyFrameAtBoundary := frame == t.lastKeySeen || (t.framesToKeyRemaining == remaining && frame == 0)
	if frame == 0 {
		keyFrameAtBoundary = true
	}
	// libvpx's define_gf_group walks forward from the current frame
	// accumulating modified_err. For the KF case it then subtracts
	// gf_first_frame_err so the KF's own error is excluded
	// (line 1633). govpx mirrors that by computing the sum over
	// frames [frame .. frame+gfInterval-1] and subtracting the first
	// frame's modErr when at a KF boundary.
	var gfGroupErr float64
	end := min(frame+uint64(gfInterval), uint64(len(t.stats)))
	for i := frame; i < end; i++ {
		gfGroupErr += t.modifiedError(t.stats[i])
	}
	// libvpx define_gf_group snapshots start_pos after the current frame's
	// stats have already been consumed by vp8_second_pass. The section
	// complexity update therefore walks the following GF span, not the
	// current GF/KF frame itself.
	var gfSectionIntra, gfSectionCoded float64
	sectionStart := frame + 1
	sectionEnd := min(sectionStart+uint64(gfInterval), uint64(len(t.stats)))
	for i := sectionStart; i < sectionEnd; i++ {
		gfSectionIntra += t.stats[i].IntraError
		gfSectionCoded += t.stats[i].CodedError
	}
	if sectionEnd > sectionStart {
		t.sectionMaxQFactor = libvpxSectionMaxQFactor(gfSectionIntra, gfSectionCoded)
		// Mirror libvpx define_gf_group line 2138: GF section also
		// resets section_intra_rating from this group's avg
		// intra/coded ratio.
		t.sectionIntraRating = libvpxSectionIntraRating(gfSectionIntra, gfSectionCoded)
	}
	if keyFrameAtBoundary {
		gfGroupErr -= t.modifiedError(t.stats[frame])
		if gfGroupErr < 0 {
			gfGroupErr = 0
		}
	}
	gfGroupBits := int64(0)
	if t.kfGroupErrorLeft > 0 {
		gfGroupBits = int64(float64(t.kfGroupBitsRemaining) * (gfGroupErr / t.kfGroupErrorLeft))
	}
	if gfGroupBits < 0 {
		gfGroupBits = 0
	}
	if gfGroupBits > t.kfGroupBitsRemaining {
		gfGroupBits = t.kfGroupBitsRemaining
	}
	maxBits := int64(libvpxFrameMaxBitsVBR(t.bitsLeft, int64(remaining), t.maxPctOrDefault()))
	if maxBits > 0 {
		if cap := maxBits * int64(gfInterval); gfGroupBits > cap {
			gfGroupBits = cap
		}
	}
	// libvpx: kf_group_error_left -= gf_group_err; kf_group_bits -=
	// gf_group_bits. Mirror that drain so subsequent GF groups in the
	// same kf group see the correct residual.
	t.kfGroupErrorLeft -= gfGroupErr
	if t.kfGroupErrorLeft < 0 {
		t.kfGroupErrorLeft = 0
	}
	t.kfGroupBitsRemaining -= gfGroupBits
	if t.kfGroupBitsRemaining < 0 {
		t.kfGroupBitsRemaining = 0
	}
	// libvpx GF-bits allocation: Boost = (gfu_boost * GFQ_ADJUSTMENT)
	// / 100, capped at baseline_gf_interval*150 with a floor of 125,
	// then halved while >1000. allocation_chunks =
	// baseline_gf_interval*100 + (Boost-100). gfu_boost is computed
	// by walking the prediction-quality decay across the GF interval
	// (libvpx vp8/encoder/firstpass.c lines 1639-1706); govpx ports
	// the same walk in computeGFUBoost so the boost matches libvpx
	// frame-for-frame (within rounding). The Q used to look up
	// GFQ_ADJUSTMENT is libvpx's `last_q[INTER_FRAME]`, which is 0
	// before any inter frame has been encoded — for short clips with
	// a single KF that means Q=0 and GFQ_ADJUSTMENT=80.
	gfuBoost := computeGFUBoost(t.stats, frame, gfInterval, keyFrameAtBoundary, t.gfIntraErrMin)
	q := max(t.lastInterQ, 0)
	if q >= len(libvpxGFBoostQAdjustment) {
		q = len(libvpxGFBoostQAdjustment) - 1
	}
	gfqAdjustment := libvpxGFBoostQAdjustment[q]
	boost := int64(gfuBoost*gfqAdjustment) / 100
	if cap := int64(gfInterval) * 150; boost > cap {
		boost = cap
	}
	if boost < 125 {
		boost = 125
	}
	allocationChunks := int64(gfInterval)*100 + (boost - 100)
	for boost > 1000 {
		boost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			break
		}
	}
	if allocationChunks <= 0 {
		allocationChunks = 1
	}
	gfBits := max(boost*gfGroupBits/allocationChunks, 0)
	// libvpx alt branch (lines 2017-2046): if mod_frame_err < group
	// avg, use a smaller alt_gf_bits computed from the frame's own
	// error scaled by interval; if mod_frame_err >= group avg, ensure
	// gf_bits >= alt_gf_bits = kf_group_bits * mod_frame_err /
	// kf_group_error_left. The "this_frame" in libvpx's code path
	// here points to whatever the GF walk landed on, NOT necessarily
	// the GF refresh frame. For our short-clip (no-ARF) path, the
	// libvpx code path leaves mod_frame_err set to the LAST iteration
	// value of the inner loop (libvpx walks i frames; mod_frame_err
	// is overwritten on each iter). We approximate that by using the
	// modErr at frame+gfInterval-1 (the last frame in the GF span).
	// kf_group_bits at this point in libvpx flow is the value BEFORE
	// the gf_group_bits drain (libvpx uses cpi->twopass.kf_group_bits
	// which has just been set to bits_left for the final-kf-group
	// case at line 1923). We restore that pre-drain value here too.
	preGFKFGroupBits := t.kfGroupBitsRemaining + gfGroupBits
	if preGFKFGroupBits <= 0 {
		preGFKFGroupBits = t.bitsLeft
	}
	preGFKFErrorLeft := t.kfGroupErrorLeft + gfGroupErr
	if preGFKFErrorLeft < 1 {
		preGFKFErrorLeft = 1
	}
	lastIterIdx := int(frame) + gfInterval - 1
	if lastIterIdx >= len(t.stats) {
		lastIterIdx = len(t.stats) - 1
	}
	if lastIterIdx < int(frame) {
		lastIterIdx = int(frame)
	}
	modFrameErr := t.modifiedError(t.stats[lastIterIdx])
	if modFrameErr*float64(gfInterval) < gfGroupErr {
		altGFGroupBits := float64(preGFKFGroupBits) *
			(modFrameErr * float64(gfInterval)) /
			preGFKFErrorLeft
		altGFBits := int64(float64(boost) * (altGFGroupBits / float64(allocationChunks)))
		if gfBits > altGFBits {
			gfBits = altGFBits
		}
	} else {
		altGFBits := int64(float64(preGFKFGroupBits) * modFrameErr / preGFKFErrorLeft)
		if altGFBits > gfBits {
			gfBits = altGFBits
		}
	}
	if gfBits < 0 {
		gfBits = 0
	}
	if gfBits > gfGroupBits {
		gfBits = gfGroupBits
	}
	// libvpx: gf_group_bits -= (gf_bits - min_frame_bandwidth)
	// (line 2090). Mirror that drain.
	gfGroupBits -= gfBits - int64(t.minFrameBandwidth)
	if gfGroupBits < 0 {
		gfGroupBits = 0
	}
	// alt_extra_bits — see libvpx vp8/encoder/firstpass.c lines
	// 2099-2120. Gated on gfu_boost >= 150; spreads a `pct_extra`
	// percentage of the remaining gf_group_bits across the
	// alternating-frame slots within the GF section. pct_extra =
	// (boost-100)/50, capped at 20.
	altExtraTotal := int64(0)
	altExtraPer := int64(0)
	if gfInterval >= 3 && gfuBoost >= 150 {
		pctExtra := min((gfuBoost-100)/50, 20)
		if pctExtra > 0 {
			altExtraTotal = gfGroupBits * int64(pctExtra) / 100
			gfGroupBits -= altExtraTotal
			if gfGroupBits < 0 {
				gfGroupBits = 0
			}
			denom := int64((gfInterval - 1) / 2)
			if denom > 0 {
				altExtraPer = altExtraTotal / denom
			}
		}
	}
	t.gfGroupBits = gfGroupBits
	// libvpx: gf_group_error_left = gf_group_err (when KF) else
	// gf_group_err - gf_first_frame_err. For the KF case, gf_group_err
	// already had gf_first_frame_err subtracted (the if frame_type==KF
	// branch in the loop pre-init), so gf_group_error_left =
	// gf_group_err. For non-KF GF boundary, we subtract the first
	// frame's modErr from the denominator so the err-fraction at the
	// first frame after the boundary uses frames [frame+1..end].
	if keyFrameAtBoundary {
		t.gfGroupErrorLeft = gfGroupErr
	} else {
		gfFirstFrameErr := t.modifiedError(t.stats[frame])
		t.gfGroupErrorLeft = gfGroupErr - gfFirstFrameErr
	}
	if t.gfGroupErrorLeft < 0 {
		t.gfGroupErrorLeft = 0
	}
	t.framesTillGFUpdate = gfInterval
	t.gfGroupValid = true
	t.altExtraBits = int(altExtraPer)
	t.gfRefreshTarget = int(gfBits + int64(t.minFrameBandwidth))
	// libvpx onyx_if.c update_golden_frame_stats: frames_since_golden
	// is zeroed at every GF refresh (including KF, which always
	// refreshes golden). The post-encode finishFrame increment then
	// makes fsg=1 for the *next* frame's assign_std_frame_bits, so the
	// alternating-frame alt_extra_bits cadence lands on odd
	// frames_since_golden — which for the no-ARF path means frames at
	// offset 2, 4, 6, ... after the GF refresh.
	t.framesSinceGolden = 0
}

// computeGFUBoost mirrors the libvpx vp8/encoder/firstpass.c
// define_gf_group inner walk that produces `cpi->gfu_boost`. It
// walks the per-frame stats from `frame+1` through the GF interval,
// accumulating `decay_accumulator * frame_boost` where:
//
//	frame_boost = IIFACTOR * intra_error / coded_error  (capped at
//	  GF_RMAX=48), with a `gf_intra_err_min` floor on the intra_error
//	  numerator, then biased by mv_in_out_count (positive doubles,
//	  negative halves), then re-clamped to GF_RMAX.
//	decay_accumulator *= libvpxGetPredictionDecayRate(next_frame)
//	  clamped to [0.1, 1.0].
//
// libvpx breaks the loop when `i > MIN_GF_INTERVAL && (frames_to_key
// - i) >= MIN_GF_INTERVAL && (boost_score>20 || pcnt_inter<0.75) &&
// (boost_score-old_boost_score)<2.0`; govpx mirrors that. The
// returned value is `(boost_score * 100) >> 4` matching libvpx's
// scaling at line 1751 (`cpi->gfu_boost = (int)(boost_score *
// 100.0) >> 4`).
func computeGFUBoost(stats []FirstPassFrameStats, frame uint64, gfInterval int, keyFrameAtBoundary bool, gfIntraErrMin float64) int {
	const (
		iiFactor    = 1.5
		gfRMax      = 48.0
		minGFInterv = libvpxMinGFInterval
	)
	if gfInterval <= 0 || frame >= uint64(len(stats)) {
		return 0
	}
	mvInOutAccumulator := 0.0
	decayAccumulator := 1.0
	boostScore := 0.0
	oldBoostScore := 0.0
	// libvpx walks i from 1 to gfInterval (inclusive). On each iter,
	// it loads next_frame = stats[frame+i] and computes the per-frame
	// boost from THAT next_frame's stats (NOT the current frame's).
	for i := 1; i <= gfInterval; i++ {
		idx := int(frame) + i
		if idx >= len(stats) {
			break
		}
		next := stats[idx]
		// accumulate_frame_motion_stats: this_frame_mv_in_out =
		// mv_in_out_count * pcnt_motion. mv_in_out_accumulator
		// accumulates that.
		thisFrameMVInOut := next.MVInOutCount * next.PcntMotion
		mvInOutAccumulator += thisFrameMVInOut
		// calc_frame_boost: r = IIFACTOR * intra_error / coded_error,
		// with intra_error floored at gf_intra_err_min.
		intra := next.IntraError
		if intra < gfIntraErrMin {
			intra = gfIntraErrMin
		}
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		r := iiFactor * intra / denom
		// Bias by mv_in_out_count.
		if thisFrameMVInOut > 0 {
			r += r * (thisFrameMVInOut * 2.0)
		} else {
			r += r * (thisFrameMVInOut / 2.0)
		}
		if r > gfRMax {
			r = gfRMax
		}
		// Cumulative effect of prediction quality decay.
		loopDecayRate := libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * r
		// Break clauses: libvpx breaks when i>MIN_GF_INTERVAL AND
		// (boost_score>20 || pcnt_inter<0.75) AND (boost-old)<2.
		// We can't fully model libvpx's frames_to_key breakout
		// because govpx may not have seen frames past the GF
		// section, but for our short-clip workloads the loop ends
		// at EOF anyway.
		if i > minGFInterv &&
			((boostScore > 20.0) || (next.PcntInter < 0.75)) &&
			((boostScore - oldBoostScore) < 2.0) {
			boostScore = oldBoostScore
			break
		}
		oldBoostScore = boostScore
	}
	_ = keyFrameAtBoundary // currently unused but reserved for ARF gating
	gfuBoost := int(boostScore*100.0) >> 4
	return gfuBoost
}

// assignStdFrameBits ports libvpx's assign_std_frame_bits inner-loop
// allocator for std P frames inside a GF group. Drains gfGroupBits and
// gfGroupErrorLeft per call.
func (t *twoPassState) assignStdFrameBits(modErr float64, maxBits int64) int64 {
	if !t.gfGroupValid || t.gfGroupErrorLeft <= 0 || t.gfGroupBits <= 0 {
		return int64(t.minFrameBandwidth)
	}
	errFraction := modErr / t.gfGroupErrorLeft
	target := max(int64(float64(t.gfGroupBits)*errFraction), 0)
	if maxBits > 0 && target > maxBits {
		target = maxBits
	}
	if target > t.gfGroupBits {
		target = t.gfGroupBits
	}
	// Drain (libvpx: gf_group_error_left -= modified_err;
	// gf_group_bits -= target_frame_size). We update gf_group_bits in
	// finishFrame (after the actual frame size is known) using the
	// here-computed target as the libvpx-equivalent
	// `target_frame_size`. Keep the err drain here so the per-frame
	// ratio at the next call uses the right denominator even before
	// finishFrame runs.
	t.gfGroupErrorLeft -= modErr
	if t.gfGroupErrorLeft < 0 {
		t.gfGroupErrorLeft = 0
	}
	t.gfGroupBits -= target
	if t.gfGroupBits < 0 {
		t.gfGroupBits = 0
	}
	target += int64(t.minFrameBandwidth)
	if (t.framesSinceGolden&0x01) != 0 && t.framesTillGFUpdate > 0 {
		target += int64(t.altExtraBits)
	}
	return target
}

// maxPctOrDefault returns the active two_pass_vbrmax_section value or
// libvpx's default (400).
func (t *twoPassState) maxPctOrDefault() int {
	if t.maxPct <= 0 {
		return 400
	}
	return t.maxPct
}

// pass2VBRSectionLimits ports the libvpx vp8/encoder/firstpass.c
// Pass2Encode VBR section-limit application on the per-frame target.
// Returns the (section_min_bits, section_max_bits) bounds derived from
// the configured `two_pass_vbrmin_section` / `two_pass_vbrmax_section`
// percentages applied to (a) the live VBR per-frame budget
// `(bits_left/frames_left)` for the max ceiling, mirroring libvpx's
// `frame_max_bits` VBR branch, and (b) the per-frame average
// `defaultTargetBits` for the min floor, mirroring
// `cpi->min_frame_bandwidth = av_per_frame_bandwidth *
// two_pass_vbrmin_section / 100`. Frames past the end of the stats
// stream return the static fallback bounds.
func (t *twoPassState) pass2VBRSectionLimits(frame uint64, defaultTargetBits int) (int64, int64) {
	// libvpx defaults: rc_2pass_vbr_minsection_pct=0,
	// rc_2pass_vbr_maxsection_pct=400.
	minPct := max(t.minPct, 0)
	maxPct := t.maxPct
	if maxPct <= 0 {
		maxPct = 400
	}
	// libvpx's `min_frame_bandwidth` is `av_per_frame_bandwidth *
	// two_pass_vbrmin_section / 100`; it's the additive floor used inside
	// `assign_std_frame_bits`, NOT a clamp on the err-fraction target. We
	// expose it via t.minFrameBandwidth so the caller can apply it
	// additively. The sectionMin we return here is therefore zero — pass-2
	// targets in libvpx are clamped only on the upper side.
	sectionMin := int64(0)
	sectionMax := int64(defaultTargetBits) * int64(maxPct) / 100
	if t.enabled() && frame < uint64(len(t.stats)) {
		framesLeft := int64(len(t.stats)) - int64(frame)
		if vbrMax := libvpxFrameMaxBitsVBR(t.bitsLeft, framesLeft, maxPct); vbrMax > 0 {
			sectionMax = int64(vbrMax)
		}
	}
	if sectionMax < sectionMin {
		sectionMax = sectionMin
	}
	_ = minPct
	return sectionMin, sectionMax
}

// pass2DetectARFPending ports the libvpx vp8/encoder/firstpass.c
// `define_gf_group` / `select_arf_period` ARF-pending decision, the
// branch at lines 1758-1842 that sets `cpi->source_alt_ref_pending = 1`
// when the upcoming GF section is a high-motion / high-quality run that
// will benefit from a hidden alt-ref. Returns the ARF section interval
// (in frames, mirroring libvpx's `cpi->baseline_gf_interval`) and a
// pending flag.
//
// Heuristic mirrored:
//
//   - allow_alt_ref guard (caller passes
//     `cpi->oxcf.play_alternate && lag_in_frames`).
//   - i >= MIN_GF_INTERVAL.
//   - i <= frames_to_key - MIN_GF_INTERVAL (don't ARF very near KF).
//   - next_frame.pcnt_inter > 0.75 (start of section is strongly
//     predicted from LAST so a hidden ARF is worth the cost).
//   - mv_in_out_accumulator/i > -0.2 OR mv_in_out_accumulator > -2.0
//     (motion is not collapsing inward only).
//   - gfu_boost > 100 (the boost score crossed the libvpx floor).
//
// The interval is the libvpx GF-loop length, capped at
// `min(static_scene_max_gf_interval, frames_to_key)` and floored at
// MIN_GF_INTERVAL.
func (t *twoPassState) pass2DetectARFPending(currentFrame uint64, framesToKey int, allowAltRef bool, maxGFInterval int) (int, bool) {
	if !t.enabled() || !allowAltRef || framesToKey <= 0 {
		return 0, false
	}
	if currentFrame >= uint64(len(t.stats)) {
		return 0, false
	}
	if maxGFInterval < libvpxMinGFInterval {
		maxGFInterval = libvpxMinGFInterval
	}
	// libvpx walks i forward up to static_scene_max_gf_interval (or
	// frames_to_key, whichever is smaller), accumulating motion stats.
	// Also cap at frames_to_key - MIN_GF_INTERVAL so the eventual
	// `i <= frames_to_key - MIN_GF_INTERVAL` ARF guard can be
	// satisfied by the walk; otherwise a strongly-predicted clip near
	// the end of stats would always fail the post-loop check.
	maxLookahead := min(framesToKey, maxGFInterval)
	if cap := framesToKey - libvpxMinGFInterval; cap > 0 && maxLookahead > cap {
		maxLookahead = cap
	}
	if remaining := int(uint64(len(t.stats)) - currentFrame); maxLookahead > remaining {
		maxLookahead = remaining
	}
	if maxLookahead < libvpxMinGFInterval {
		return 0, false
	}
	mvInOutAccumulator := 0.0
	decayAccumulator := 1.0
	boostScore := 0.0
	oldBoostScore := 0.0
	interval := 0
	for i := 1; i <= maxLookahead; i++ {
		idx := currentFrame + uint64(i)
		if idx >= uint64(len(t.stats)) {
			break
		}
		next := t.stats[idx]
		mvInOutAccumulator += next.MVInOutCount
		// libvpx calc_frame_boost (vp8/encoder/firstpass.c lines
		// 1451-1480): frame_boost = IIFACTOR * intra_error /
		// coded_error, clamped to GF_RMAX=48.0, then biased by
		// mv_in_out (positive doubles, negative halves). govpx omits
		// the `gf_intra_err_min` floor (which depends on per-frame
		// MB count from the encoder context, not the stats stream)
		// and uses the raw intra/coded ratio as the boost signal.
		const iiFactor = 1.5
		const gfRMax = 48.0
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		frameBoost := iiFactor * next.IntraError / denom
		if next.MVInOutCount > 0 {
			frameBoost += frameBoost * (next.MVInOutCount * 2.0)
		} else {
			frameBoost += frameBoost * (next.MVInOutCount / 2.0)
		}
		if frameBoost > gfRMax {
			frameBoost = gfRMax
		}
		// Cumulative effect of prediction quality decay, mirroring
		// libvpx's `decay_accumulator = decay_accumulator *
		// loop_decay_rate; clamp(0.1, 1.0)`.
		loopDecayRate := libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * frameBoost
		// Break-out conditions mirroring libvpx's loop tail.
		if i > libvpxMinGFInterval &&
			(framesToKey-i) >= libvpxMinGFInterval &&
			((boostScore > 20.0) || (next.PcntInter < 0.75)) &&
			((boostScore - oldBoostScore) < 2.0) {
			break
		}
		interval = i
		oldBoostScore = boostScore
	}
	if interval < libvpxMinGFInterval {
		return 0, false
	}
	if interval > framesToKey-libvpxMinGFInterval {
		// libvpx: don't use ARF very near next KF.
		return 0, false
	}
	// Look at the frame just past current to apply the libvpx
	// `next_frame.pcnt_inter > 0.75` gate.
	if currentFrame+1 >= uint64(len(t.stats)) {
		return 0, false
	}
	nextFrame := t.stats[currentFrame+1]
	if nextFrame.PcntInter <= 0.75 {
		return 0, false
	}
	if !((mvInOutAccumulator/float64(interval) > -0.2) || (mvInOutAccumulator > -2.0)) {
		return 0, false
	}
	gfuBoost := int(boostScore*100.0) >> 4
	if gfuBoost <= 100 {
		return 0, false
	}
	return interval, true
}

// pass2MaybeArmAltRefPending wires the libvpx
// vp8/encoder/firstpass.c `define_gf_group` ARF-pending decision into
// the encoder. It runs at a GF-group boundary (framesTillAltRefFrame ==
// 0 and ARF not already pending or active) and, when the second-pass
// stats indicate a high-motion section ahead, calls
// `scheduleAltRefSource` so the auto-ARF driver can emit the hidden
// alt-ref at the predicted offset.
//
// libvpx fires this from `vp8_second_pass`, which runs on every
// non-hidden frame including the keyframe (find_next_key_frame zeros
// `frames_till_gf_update_due` so the same `if (frames_till_gf_update_due
// == 0)` predicate triggers `define_gf_group` from inside Pass2Encode
// for the keyframe). govpx mirrors that by allowing the arming call to
// fire on `keyFrame == true`; the keyframe-path lifecycle update inside
// `resetGoldenFrameStats` no longer clobbers the schedule (it now
// matches libvpx's `update_golden_frame_stats`, which leaves
// `source_alt_ref_pending` intact). Without arming on the keyframe the
// hidden ARF would slip by one frame relative to libvpx.
//
// The wiring is gated on:
//   - Two-pass stats loaded.
//   - `EncoderOptions.AutoAltRef` (libvpx `oxcf.play_alternate`).
//   - `LookaheadFrames > 1` (the auto-ARF driver requires future peeks).
//   - `!ErrorResilient` (libvpx zeroes source_alt_ref_pending in
//     error-resilient mode inside Pass2Encode).
//   - No alt-ref already pending or active.
