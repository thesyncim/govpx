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

// framesToKey ports a simplified `cpi->twopass.frames_to_key` lookahead
// from libvpx's vp8/encoder/firstpass.c find_next_key_frame: starting at
// `frame`, walk forward until libvpxTestCandidateKeyFrame fires (with
// the libvpx `i >= MIN_GF_INTERVAL` gate), or until the user-configured
// keyFrameInterval is exhausted, or until end-of-stats. Returns the
// number of frames remaining until the next predicted KF, including
// the current frame at index `frame`. Returns 0 when stats are not
// loaded or `frame` is past the end (libvpx falls back to default
// targets in that case).
func (t *twoPassState) framesToKey(frame uint64, keyFrameInterval int) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) {
		return 0
	}
	maxLookahead := uint64(len(t.stats)) - frame
	if keyFrameInterval > 0 && uint64(2*keyFrameInterval) < maxLookahead {
		// libvpx breaks the loop when frames_to_key >= 2*key_freq.
		maxLookahead = uint64(2 * keyFrameInterval)
	}
	for i := uint64(1); i < maxLookahead; i++ {
		idx := frame + i
		if idx >= uint64(len(t.stats)) {
			break
		}
		// libvpx requires `i >= MIN_GF_INTERVAL` before firing the
		// candidate-KF predicate; mirror that gate.
		if int(i) >= libvpxMinGFInterval && libvpxTestCandidateKeyFrame(t.stats, int(idx)) {
			return int(i) + 1
		}
		if keyFrameInterval > 0 && int(i) >= keyFrameInterval {
			return int(i) + 1
		}
	}
	return int(maxLookahead)
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
	if t.totalStats.Count > 0 && t.totalStats.SSIMWeightedPredErr > 0 && stats.SSIMWeightedPredErr > 0 {
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
