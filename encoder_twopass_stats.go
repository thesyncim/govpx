package govpx

import "math"

func (t *twoPassState) kfGroupModifiedError(frame uint64, framesToKey int) float64 {
	if !t.enabled() || framesToKey <= 0 || frame >= uint64(len(t.stats)) {
		return 0
	}
	end := min(frame+uint64(framesToKey), uint64(len(t.stats)))
	var sum float64
	for i := frame; i < end; i++ {
		sum += t.modifiedError(t.stats[i])
	}
	return sum
}

// kfGroupBits ports the libvpx vp8/encoder/firstpass.c
// find_next_key_frame KF-group bit allocation:
//
//	kf_group_bits = bits_left * (kf_group_err / modified_error_left)
//
// clamped by max_bits * frames_to_key (the per-frame ceiling). Returns
// 0 when stats are not loaded, when bits_left has been depleted, or
// when modified_error_left is 0 (libvpx's `if (bits_left > 0 &&
// modified_error_left > 0.0)` gate). The caller passes the libvpx
// frame_max_bits value (libvpx caps any single normal frame at this
// rate; defaults to av_per_frame_bandwidth * (max_section_pct/100)).
func (t *twoPassState) kfGroupBits(frame uint64, framesToKey int, maxBitsPerFrame int) int64 {
	if !t.enabled() || framesToKey <= 0 || t.bitsLeft <= 0 || t.errorLeft <= 0 {
		return 0
	}
	groupErr := t.kfGroupModifiedError(frame, framesToKey)
	if groupErr <= 0 {
		return 0
	}
	groupBits := int64(float64(t.bitsLeft) * (groupErr / t.errorLeft))
	if maxBitsPerFrame > 0 {
		maxGroupBits := int64(maxBitsPerFrame) * int64(framesToKey)
		if groupBits > maxGroupBits {
			groupBits = maxGroupBits
		}
	}
	if groupBits < 0 {
		groupBits = 0
	}
	return groupBits
}

// libvpxFindNextKeyFrameWalk ports the libvpx
// vp8/encoder/firstpass.c find_next_key_frame walk verbatim
// (firstpass.c lines 2533-2596 plus the post-loop centering at lines
// 2603-2608). Returns the number of frames in the KF group starting
// at index `start` — i.e., libvpx's `cpi->twopass.frames_to_key`. The
// walk:
//
//  1. Initializes frames_to_key=1, i=0 (firstpass.c line 2523).
//  2. Loops while `stats_in < stats_in_end`: at each iteration
//     accumulates the current frame into kf_group_err, then advances
//     this_frame via input_stats, then peeks next_frame via
//     lookup_next_frame_stats.
//  3. When auto_key is set and the lookahead succeeds (line 2553):
//     a. If `i >= MIN_GF_INTERVAL` and test_candidate_kf fires for
//     (last_frame, this_frame, next_frame), break (line 2555).
//     b. Otherwise compute loop_decay_rate via
//     get_prediction_decay_rate(next_frame), update the
//     recent_loop_decay[i%8] ringbuffer, recompute
//     decay_accumulator = prod(recent_loop_decay[0..8]).
//     c. If detect_transition_to_still fires, break (line 2576).
//     d. Otherwise frames_to_key++ and check `frames_to_key >= 2 *
//     key_freq` (line 2588) to break.
//  4. When auto_key is unset or lookahead is at EOF, frames_to_key++
//     (line 2592).
//  5. After the loop, if `auto_key && frames_to_key > key_freq`,
//     halve frames_to_key (lines 2603-2608, centering rule).
//
// Returns 0 when stats are not loaded or `start` is past the end.
//
// This is the shared natural-KF walk consumed by both prepareKFGroup
// (which seeds kf_group_bits / kf_group_err / section_max_qfactor)
// and pass2AltRefPendingPlan via framesToKey (which previously
// implemented a simplified, divergent walk).
func libvpxFindNextKeyFrameWalk(stats []FirstPassFrameStats, start int, keyFrameFrequency int, autoKey bool) int {
	if start < 0 || start >= len(stats) {
		return 0
	}
	// libvpx initializes cpi->twopass.frames_to_key = 1 (line 2523) —
	// the KF itself counts as one frame.
	framesToKey := 1
	recentLoopDecay := [8]float64{1, 1, 1, 1, 1, 1, 1, 1}
	// libvpx's caller (vp8_second_pass) calls input_stats(this_frame)
	// before invoking find_next_key_frame, which advances stats_in to
	// point at stats[start+1]. find_next_key_frame's while gate is
	// `cpi->twopass.stats_in < cpi->twopass.stats_in_end`. At iteration
	// i, stats_in points to stats[start+1+i] when the gate is checked,
	// and `this_frame` (before this iteration's input_stats) is
	// stats[start+i]. Inside the loop, input_stats advances stats_in
	// to stats[start+2+i] and reloads this_frame from stats[start+1+i].
	// lookup_next_frame_stats then peeks at stats[start+2+i] without
	// advancing, returning EOF when start+2+i >= n.
	n := len(stats)
	for i := 0; start+1+i < n; i++ {
		// libvpx accumulates kf_group_err using this_frame
		// (= stats[start+i]); we ignore the value here since the
		// caller (prepareKFGroup) re-walks [start, start+framesToKey)
		// to compute its own accumulators, and the framesToKey
		// caller only needs the count.
		_ = stats[start+i]
		// post-advance this_frame index = start+1+i; next_frame index
		// = start+2+i (only valid when start+2+i < n).
		thisAfterIdx := start + 1 + i
		nextLookupIdx := start + 2 + i
		haveLookahead := autoKey && nextLookupIdx < n
		if haveLookahead {
			lastFrame := stats[start+i]
			thisFrame := stats[thisAfterIdx]
			nextFrame := stats[nextLookupIdx]
			// libvpx firstpass.c line 2555: scene-cut break.
			if i >= libvpxMinGFInterval &&
				libvpxTestCandidateKFFrames(lastFrame, thisFrame, nextFrame, stats[nextLookupIdx+1:]) {
				break
			}
			// libvpx firstpass.c line 2561: get_prediction_decay_rate.
			loopDecayRate := libvpxGetPredictionDecayRate(nextFrame)
			recentLoopDecay[i%8] = loopDecayRate
			decayAccumulator := 1.0
			for j := range recentLoopDecay {
				decayAccumulator *= recentLoopDecay[j]
			}
			// libvpx firstpass.c line 2576: detect_transition_to_still
			// peeks `still_interval` frames forward from stats_in
			// (which after input_stats is at stats[start+2+i]) and
			// resets the fpf position afterwards. Mirror that
			// lookahead by passing the slice starting one position
			// past next_frame.
			stillInterval := keyFrameFrequency - i
			var nextDecayRates []float64
			if stillInterval > 0 {
				nextDecayRates = collectDecayRates(stats, nextLookupIdx+1, stillInterval)
			}
			if libvpxDetectTransitionToStill(i, stillInterval, loopDecayRate, decayAccumulator, nextDecayRates) {
				break
			}
			// libvpx firstpass.c line 2583: frames_to_key++.
			framesToKey++
			// libvpx firstpass.c line 2588: 2x clamp.
			if keyFrameFrequency > 0 && framesToKey >= 2*keyFrameFrequency {
				break
			}
		} else {
			// libvpx firstpass.c line 2592: at EOF or with auto_key
			// disabled, just bump frames_to_key.
			framesToKey++
		}
	}
	// libvpx post-loop centering (firstpass.c lines 2603-2608): when
	// auto_key is set and frames_to_key exceeded the user max but
	// stopped short of 2x, halve frames_to_key. The 2x cap was
	// already applied inside the loop; this handles the (1x, 2x]
	// range.
	if autoKey && keyFrameFrequency > 0 && framesToKey > keyFrameFrequency {
		framesToKey /= 2
	}
	return framesToKey
}

// libvpxTestCandidateKFFrames is the per-frame-pointer variant of
// libvpxTestCandidateKeyFrame: instead of indexing stats by a single
// position, the caller passes the (last, this, next) frames already
// loaded and a tail slice starting at the frame after `next` for the
// inner 16-frame boost walk. This matches libvpx's signature
// `test_candidate_kf(cpi, last_frame, this_frame, next_frame)` and
// the inner `lookup_next_frame_stats` lookahead (firstpass.c
// lines 2401-2479).
func libvpxTestCandidateKFFrames(lastFrame, thisFrame, nextFrame FirstPassFrameStats, lookahead []FirstPassFrameStats) bool {
	if thisFrame.PcntSecondRef >= 0.10 || nextFrame.PcntSecondRef >= 0.10 {
		return false
	}
	if !((thisFrame.PcntInter < 0.05) ||
		(((thisFrame.PcntInter - thisFrame.PcntNeutral) < 0.25) &&
			((thisFrame.IntraError / doubleDivideCheck(thisFrame.CodedError)) < 2.5) &&
			((math.Abs(lastFrame.CodedError-thisFrame.CodedError)/doubleDivideCheck(thisFrame.CodedError) > 0.40) ||
				(math.Abs(lastFrame.IntraError-thisFrame.IntraError)/doubleDivideCheck(thisFrame.IntraError) > 0.40) ||
				((nextFrame.IntraError / doubleDivideCheck(nextFrame.CodedError)) > 3.5)))) {
		return false
	}
	// libvpx test_candidate_kf inner walk (firstpass.c lines
	// 2439-2470): loop i=0..16, start local_next_frame = *next_frame,
	// then advance through subsequent frames via input_stats at the
	// END of each iteration. Iteration 0 examines next_frame itself;
	// iteration k+1 examines lookahead[k].
	boostScore := 0.0
	oldBoostScore := 0.0
	decayAccumulator := 1.0
	localNext := nextFrame
	i := 0
	for ; i < 16; i++ {
		nextIIRatio := libvpxIIKFactor1 * localNext.IntraError / doubleDivideCheck(localNext.CodedError)
		if nextIIRatio > libvpxRMax {
			nextIIRatio = libvpxRMax
		}
		if localNext.PcntInter > 0.85 {
			decayAccumulator *= localNext.PcntInter
		} else {
			decayAccumulator *= (0.85 + localNext.PcntInter) / 2.0
		}
		boostScore += decayAccumulator * nextIIRatio
		if localNext.PcntInter < 0.05 ||
			nextIIRatio < 1.5 ||
			(((localNext.PcntInter - localNext.PcntNeutral) < 0.20) && nextIIRatio < 3.0) ||
			((boostScore - oldBoostScore) < 0.5) ||
			localNext.IntraError < 200 {
			break
		}
		oldBoostScore = boostScore
		// libvpx: `if (EOF == input_stats(cpi, &local_next_frame)) break;`
		// The advance happens at the END of the iteration. Next
		// iteration's local_next_frame is lookahead[i].
		if i >= len(lookahead) {
			break
		}
		localNext = lookahead[i]
	}
	return boostScore > 5.0 && i > 3
}

// collectDecayRates builds the per-frame prediction-decay-rate slice
// for detect_transition_to_still's still_interval lookahead. Returns
// up to `count` rates starting at `start`; the helper short-circuits
// to a shorter slice when stats run out (libvpx's
// detect_transition_to_still treats that as a non-trigger, matching
// the existing libvpxDetectTransitionToStill behaviour when
// `limit > len(nextDecayRates)`).
func collectDecayRates(stats []FirstPassFrameStats, start int, count int) []float64 {
	if count <= 0 || start < 0 {
		return nil
	}
	end := min(start+count, len(stats))
	if start >= end {
		return nil
	}
	rates := make([]float64, end-start)
	for i := start; i < end; i++ {
		rates[i-start] = libvpxGetPredictionDecayRate(stats[i])
	}
	return rates
}

// framesToKey returns `cpi->twopass.frames_to_key` for the KF group
// starting at `frame`. Wraps libvpxFindNextKeyFrameWalk so the
// pass2AltRefPendingPlan caller (encoder_twopass_budget.go) and the
// natural-KF reseed in prepareKFGroup share the same libvpx-verbatim
// walker. keyFrameInterval is the user-configured
// `cpi->key_frame_frequency`; when 0 the libvpx 2x and centering
// clamps are disabled.
func (t *twoPassState) framesToKey(frame uint64, keyFrameInterval int) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) {
		return 0
	}
	return libvpxFindNextKeyFrameWalk(t.stats, int(frame), keyFrameInterval, keyFrameInterval > 0)
}

func (t *twoPassState) markKeyFrame(frame uint64) {
	if t.enabled() {
		t.lastKeySeen = frame
	}
}

// libvpxGetPredictionDecayRate ports the libvpx
// vp8/encoder/firstpass.c get_prediction_decay_rate:
//
//	rate = pcnt_inter
//	motion_decay = 1.0 - (pcnt_motion / 20.0)
//	rate = min(rate, motion_decay)
//	mv_rabs = |mvr_abs * pcnt_motion|
//	mv_cabs = |mvc_abs * pcnt_motion|
//	distance_factor = sqrt(mv_rabs^2 + mv_cabs^2) / 250.0
//	distance_factor = (distance_factor > 1.0) ? 0.0 : (1.0 - distance_factor)
//	rate = min(rate, distance_factor)
func libvpxGetPredictionDecayRate(stats FirstPassFrameStats) float64 {
	rate := stats.PcntInter
	motionDecay := 1.0 - (stats.PcntMotion / 20.0)
	if motionDecay < rate {
		rate = motionDecay
	}
	mvRAbs := math.Abs(stats.MVrAbs * stats.PcntMotion)
	mvCAbs := math.Abs(stats.MVcAbs * stats.PcntMotion)
	distanceFactor := math.Sqrt(mvRAbs*mvRAbs+mvCAbs*mvCAbs) / 250.0
	if distanceFactor > 1.0 {
		distanceFactor = 0.0
	} else {
		distanceFactor = 1.0 - distanceFactor
	}
	if distanceFactor < rate {
		rate = distanceFactor
	}
	return rate
}

// libvpxDetectTransitionToStill ports the libvpx
// vp8/encoder/firstpass.c detect_transition_to_still: returns true
// when a complex transition is followed by a static section (used to
// trigger an extra KF for slide-show / fade content).
//
//	trans_to_still = (frameInterval > MIN_GF_INTERVAL) &&
//	                 (loop_decay_rate >= 0.999) &&
//	                 (decay_accumulator < 0.9) &&
//	                 (all next still_interval frames have
//	                   prediction_decay_rate >= 0.999)
//
// The lookahead-walk parameter `nextDecayRates` holds the decay rates
// for the next `still_interval` frames; libvpx peeks them from
// `cpi->twopass.stats_in` and resets the file position afterwards.
func libvpxDetectTransitionToStill(frameInterval int, stillInterval int, loopDecayRate float64, decayAccumulator float64, nextDecayRates []float64) bool {
	if frameInterval <= libvpxMinGFInterval {
		return false
	}
	if loopDecayRate < 0.999 || decayAccumulator >= 0.9 {
		return false
	}
	if stillInterval <= 0 {
		return false
	}
	limit := stillInterval
	if limit > len(nextDecayRates) {
		// libvpx returns false when the lookahead runs out before
		// still_interval frames have been examined.
		return false
	}
	for j := range limit {
		if nextDecayRates[j] < 0.999 {
			return false
		}
	}
	return true
}

// libvpxCalculateModifiedErr ports the libvpx vp8/encoder/firstpass.c
// calculate_modified_err formula:
//
//	av_err = total_ssim_weighted_pred_err / count
//	this_err = this_frame.ssim_weighted_pred_err
//	if this_err > av_err: modified = av_err * pow(this/av_err, POW1)
//	else:                  modified = av_err * pow(this/av_err, POW2)
//
// where POW1 == POW2 == oxcf.two_pass_vbrbias / 100. Mirrors the
// libvpx DOUBLE_DIVIDE_CHECK fallback for av_err==0.
func libvpxCalculateModifiedErr(thisErr float64, totalSSIMErr float64, count float64, vbrBiasPct int) float64 {
	if count <= 0 {
		return 0
	}
	avErr := totalSSIMErr / count
	avDenom := avErr
	if avDenom < 1e-12 && avDenom > -1e-12 {
		avDenom = 1.0
	}
	pow := float64(vbrBiasPct) / 100.0
	return avErr * math.Pow(thisErr/avDenom, pow)
}

func normalizeTwoPassStats(stats []FirstPassFrameStats) ([]FirstPassFrameStats, FirstPassFrameStats) {
	if len(stats) == 0 {
		return nil, FirstPassFrameStats{}
	}
	if len(stats) > 1 {
		last := stats[len(stats)-1]
		// Prefer the explicit IsTotal sentinel emitted by
		// FinalizeFirstPassStats, which mirrors libvpx's terminal
		// total-stats packet from vp8_end_first_pass.
		if last.IsTotal {
			return stats[:len(stats)-1], last
		}
		// Legacy heuristic: a trailing entry with Count == N is the
		// rolled-up total libvpx writes to `cpi->twopass.stats_in_end`.
		if last.Count > 1 && math.Abs(last.Count-float64(len(stats)-1)) < 1e-9 {
			return stats[:len(stats)-1], last
		}
	}
	var total FirstPassFrameStats
	for i := range stats {
		accumulateFirstPassStats(&total, stats[i])
	}
	return stats, total
}

func (t *twoPassState) modifiedError(stats FirstPassFrameStats) float64 {
	if min(min(t.totalStats.Count, t.totalStats.SSIMWeightedPredErr), stats.SSIMWeightedPredErr) > 0 {
		if err := libvpxCalculateModifiedErr(stats.SSIMWeightedPredErr, t.totalStats.SSIMWeightedPredErr, t.totalStats.Count, t.vbrBiasPct); err > 0 {
			return err
		}
	}
	return twoPassModifiedError(stats, t.vbrBiasPct)
}

func twoPassModifiedError(stats FirstPassFrameStats, biasPct int) float64 {
	err := stats.CodedError
	if stats.SSIMWeightedPredErr > 0 {
		err = stats.SSIMWeightedPredErr
	}
	if err < 1 {
		err = 1
	}
	pow := float64(biasPct) / 100.0
	if pow <= 0 {
		return err
	}
	return math.Pow(err, pow)
}

func libvpxTestCandidateKeyFrame(stats []FirstPassFrameStats, idx int) bool {
	if idx <= 0 || idx+1 >= len(stats) {
		return false
	}
	lastFrame := stats[idx-1]
	thisFrame := stats[idx]
	nextFrame := stats[idx+1]
	if thisFrame.PcntSecondRef >= 0.10 || nextFrame.PcntSecondRef >= 0.10 {
		return false
	}
	if !((thisFrame.PcntInter < 0.05) ||
		(((thisFrame.PcntInter - thisFrame.PcntNeutral) < 0.25) &&
			((thisFrame.IntraError / doubleDivideCheck(thisFrame.CodedError)) < 2.5) &&
			((math.Abs(lastFrame.CodedError-thisFrame.CodedError)/doubleDivideCheck(thisFrame.CodedError) > 0.40) ||
				(math.Abs(lastFrame.IntraError-thisFrame.IntraError)/doubleDivideCheck(thisFrame.IntraError) > 0.40) ||
				((nextFrame.IntraError / doubleDivideCheck(nextFrame.CodedError)) > 3.5)))) {
		return false
	}
	boostScore := 0.0
	oldBoostScore := 0.0
	decayAccumulator := 1.0
	i := 0
	for ; i < 16 && idx+1+i < len(stats); i++ {
		localNext := stats[idx+1+i]
		nextIIRatio := libvpxIIKFactor1 * localNext.IntraError / doubleDivideCheck(localNext.CodedError)
		if nextIIRatio > libvpxRMax {
			nextIIRatio = libvpxRMax
		}
		if localNext.PcntInter > 0.85 {
			decayAccumulator *= localNext.PcntInter
		} else {
			decayAccumulator *= (0.85 + localNext.PcntInter) / 2.0
		}
		boostScore += decayAccumulator * nextIIRatio
		if localNext.PcntInter < 0.05 ||
			nextIIRatio < 1.5 ||
			(((localNext.PcntInter - localNext.PcntNeutral) < 0.20) && nextIIRatio < 3.0) ||
			((boostScore - oldBoostScore) < 0.5) ||
			localNext.IntraError < 200 {
			break
		}
		oldBoostScore = boostScore
	}
	return boostScore > 5.0 && i > 3
}

func doubleDivideCheck(v float64) float64 {
	if v < 0 {
		return v - 0.000001
	}
	return v + 0.000001
}
