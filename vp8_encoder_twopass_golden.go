package govpx

import "math"

// libvpxSaturateCastDoubleToInt mirrors libvpx's
// `saturate_cast_double_to_int` helper from vpx_dsp/vpx_dsp_common.h
// (v1.16.0, lines 86-90):
//
//	static INLINE int saturate_cast_double_to_int(double d) {
//	  if (d > INT_MAX) return INT_MAX;
//	  return (int)d;
//	}
//
// libvpx's `define_gf_group` consumes this at lines 2009-2011 and
// 2039-2041 (the gf_bits / alt_gf_bits double-to-int reduction) and
// `assign_std_frame_bits` consumes it at line 2175. Porting the same
// saturating cast (rather than the prior pure-int truncation) keeps
// the float-order arithmetic of the libvpx pass-2 GF/ARF allocator
// byte-identical against the reference; that was the +5.14% BD-rate
// drift flagged by task #283 and tightened here by task #287.
//
// Returns an int64 so the caller can compose it back into the
// int64-typed `gfGroupBits` / `kfGroupBitsRemaining` fields; the
// saturating boundary (INT_MAX) matches libvpx's int return type.
func libvpxSaturateCastDoubleToInt(d float64) int64 {
	if d > math.MaxInt32 {
		return math.MaxInt32
	}
	return int64(d)
}

func (t *twoPassState) defineGFGroup(frame uint64, altRefInterval int, useAltRef bool) {
	t.gfRefreshTarget = 0
	t.altRefTarget = 0
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
	gfInterval := t.defineGFGroupInterval(frame, remaining)
	if useAltRef && altRefInterval > 0 && altRefInterval < gfInterval {
		gfInterval = altRefInterval
	}
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
	// libvpx vp8/encoder/firstpass.c:1602 in define_gf_group:
	//   int max_bits = frame_max_bits(cpi); /* Max for a single frame */
	// dispatches on cpi->oxcf.end_usage (firstpass.c:316-368). govpx
	// routes through twoPassState.frameMaxBits so CBR runs the
	// buffer-aware libvpxFrameMaxBitsCBR branch and VBR/CQ/Q run
	// libvpxFrameMaxBitsVBR.
	maxBits := int64(t.frameMaxBits(int64(remaining)))
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
	// libvpx GF/ARF-bits allocation. When an ARF is pending, the first
	// allocation loop entry computes twopass.gf_bits for the hidden ARF
	// with the larger ARF boost formula. On non-key ARF groups the loop
	// runs a second entry for the visible GF target. Otherwise the normal
	// GF formula is used: Boost = (gfu_boost * GFQ_ADJUSTMENT) / 100,
	// capped at baseline_gf_interval*150 with a floor of 125, then halved
	// while >1000. allocation_chunks = baseline_gf_interval*100 +
	// (Boost-100). gfu_boost is computed by walking the prediction-quality
	// decay across the GF interval (libvpx vp8/encoder/firstpass.c lines
	// 1639-1706); govpx ports the same walk in computeGFUBoost so the
	// boost matches libvpx frame-for-frame (within rounding). The Q used
	// to look up GFQ_ADJUSTMENT is libvpx's `last_q[INTER_FRAME]`, which
	// is 0 before any inter frame has been encoded — for short clips with
	// a single KF that means Q=0 and GFQ_ADJUSTMENT=80.
	gfuBoost := computeGFUBoost(t.stats, frame, gfInterval, t.gfIntraErrMin)
	q := max(t.lastInterQ, 0)
	if q >= len(libvpxGFBoostQAdjustment) {
		q = len(libvpxGFBoostQAdjustment) - 1
	}
	gfqAdjustment := libvpxGFBoostQAdjustment[q]
	// libvpx vp8/encoder/firstpass.c:1753-1786 (NEW_BOOST=1, the default
	// per firstpass.c:62): compute the alt-ref boost from forward and
	// backward sweeps centred on the post-GF-walk stats cursor, and when
	// the alt-ref is selected reassign `cpi->gfu_boost = alt_boost`.
	// govpx's stats cursor equivalent after the walk is `frame + 1 +
	// gfInterval` (libvpx's `start_pos + i` after `input_stats` was
	// called i times). `f_frames = b_frames = i - 1` per libvpx
	// firstpass.c:1755.
	altBoost := 0
	fBoost := 0
	bBoost := 0
	if gfInterval > 1 {
		cursor := int(frame) + 1 + gfInterval
		fBoost, bBoost, altBoost = libvpxCalcARFBoost(t.stats, cursor, gfInterval-1, gfInterval-1, t.gfIntraErrMin)
	}
	t.lastAltBoostFBoost = fBoost
	t.lastAltBoostBBoost = bBoost
	t.lastAltBoost = altBoost
	// libvpx firstpass.c:1785 — inside the ARF-selected branch only,
	// `cpi->gfu_boost = alt_boost`. govpx mirrors that override here so
	// the downstream alt_extra_bits guard and Boost formula see the
	// alt_boost value (libvpx firstpass.c:1799-1800 selects
	// `Boost = (alt_boost * GFQ_ADJUSTMENT) / 100` over the legacy
	// `(gfu_boost * 3 * GFQ_ADJUSTMENT) / (2 * 100)`).
	if useAltRef && altBoost > 0 {
		gfuBoost = altBoost
	}
	// Publish the finalized `cpi->gfu_boost` (post-alt-ref reassignment
	// at libvpx firstpass.c:1785) onto twoPassState so the pass-2
	// active-best-quality path at libvpx onyx_if.c:3624-3674 can read
	// it through `gfuBoostValue()` and select between
	// kf_low_motion_minq / kf_high_motion_minq (>600) and between
	// gf_low_motion_minq / gf_mid_motion_minq / gf_high_motion_minq
	// (>1000 / <400). Mirror libvpx's calloc default (0) until the
	// first define_gf_group runs by gating downstream consumers on
	// `gfuBoostValid`.
	t.gfuBoost = gfuBoost
	t.gfuBoostValid = true
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

	allocateGFBits := func(arf bool) (int64, int64) {
		boost := int64(gfuBoost*gfqAdjustment) / 100
		allocationChunks := int64(0)
		if arf {
			// libvpx vp8/encoder/firstpass.c:1799-1803 (NEW_BOOST=1):
			//   Boost = (alt_boost * GFQ_ADJUSTMENT) / 100;
			// versus the NEW_BOOST=0 legacy formula
			//   Boost = (gfu_boost * 3 * GFQ_ADJUSTMENT) / (2 * 100);
			// `gfuBoost` here already equals `alt_boost` because
			// defineGFGroup reassigned it above (firstpass.c:1785
			// `cpi->gfu_boost = alt_boost`).
			boost = int64(gfuBoost*gfqAdjustment) / 100
			boost += int64(gfInterval * 50)
			if cap := int64(gfInterval+1) * 200; boost > cap {
				boost = cap
			}
			if boost < 125 {
				boost = 125
			}
			allocationChunks = int64(gfInterval+1)*100 + boost
		} else {
			if cap := int64(gfInterval) * 150; boost > cap {
				boost = cap
			}
			if boost < 125 {
				boost = 125
			}
			allocationChunks = int64(gfInterval)*100 + (boost - 100)
		}
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
		// libvpx vp8/encoder/firstpass.c:2009-2011 (v1.16.0):
		//
		//	gf_bits = saturate_cast_double_to_int(
		//	    (double)Boost *
		//	    (cpi->twopass.gf_group_bits / (double)allocation_chunks));
		//
		// The float-order divides gf_group_bits by allocation_chunks
		// in double precision FIRST, then multiplies by Boost. Doing
		// the multiply first in int64 (`boost*gfGroupBits/allocationChunks`)
		// produces a different rounding because the intermediate
		// product is integer-truncated rather than carrying the
		// fractional bits forward — that order divergence drove the
		// +5.14% govpx-vs-libvpx BD-rate flagged by task #283.
		gfBits := max(libvpxSaturateCastDoubleToInt(
			float64(boost)*(float64(gfGroupBits)/float64(allocationChunks))), 0)
		if modFrameErr*float64(gfInterval) < gfGroupErr {
			altGFGroupBits := float64(preGFKFGroupBits) *
				(modFrameErr * float64(gfInterval)) /
				preGFKFErrorLeft
			// libvpx vp8/encoder/firstpass.c:2026-2027:
			//   alt_gf_bits = (int)((double)Boost *
			//       (alt_gf_grp_bits / (double)allocation_chunks));
			// Plain (int) cast — no saturation — but the float
			// arithmetic ORDER is "divide before multiply" so
			// mirror it exactly. Truncation toward zero matches
			// Go's int64 conversion of a positive double.
			altGFBits := int64(float64(boost) * (altGFGroupBits / float64(allocationChunks)))
			if gfBits > altGFBits {
				gfBits = altGFBits
			}
		} else {
			// libvpx vp8/encoder/firstpass.c:2039-2041:
			//   alt_gf_bits = saturate_cast_double_to_int(
			//       (double)kf_group_bits * mod_frame_err /
			//       (double)VPXMAX(kf_group_error_left, 1));
			altGFBits := libvpxSaturateCastDoubleToInt(
				float64(preGFKFGroupBits) * modFrameErr / preGFKFErrorLeft)
			if altGFBits > gfBits {
				gfBits = altGFBits
			}
		}
		if gfBits < 0 {
			gfBits = 0
		}
		return gfBits, gfBits + int64(t.minFrameBandwidth)
	}

	gfBits, gfTarget := allocateGFBits(false)
	arfTarget := int64(0)
	if useAltRef {
		arfBits, target := allocateGFBits(true)
		gfBits = arfBits
		arfTarget = target
		if keyFrameAtBoundary {
			gfTarget = target
		}
	}
	// libvpx: gf_group_bits -= (gf_bits - min_frame_bandwidth)
	// (line 2090). Mirror that drain.
	gfGroupBits -= gfBits
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
	// libvpx: gf_group_error_left = gf_group_err when a group starts on
	// a KF or uses an ARF; otherwise it subtracts the first visible GF
	// frame's error. In an ARF group, the future ARF source forms the
	// start of the next group, so this group keeps the full denominator.
	if keyFrameAtBoundary || useAltRef {
		t.gfGroupErrorLeft = gfGroupErr
	} else {
		gfFirstFrameErr := t.modifiedError(t.stats[frame])
		t.gfGroupErrorLeft = gfGroupErr - gfFirstFrameErr
	}
	if t.gfGroupErrorLeft < 0 {
		t.gfGroupErrorLeft = 0
	}
	t.framesTillGFUpdate = gfInterval
	// libvpx vp8/encoder/firstpass.c define_gf_group sets
	// cpi->baseline_gf_interval to the chosen GF group length here. The
	// early-portion damped active_worst_quality update reads this value
	// when gating the window (firstpass.c:2374).
	t.baselineGFInterval = gfInterval
	t.gfGroupValid = true
	t.altExtraBits = int(altExtraPer)
	t.gfRefreshTarget = int(gfTarget)
	t.altRefTarget = int(arfTarget)
	// libvpx onyx_if.c update_golden_frame_stats: frames_since_golden
	// is zeroed at every GF refresh (including KF, which always
	// refreshes golden). The post-encode finishFrame increment then
	// makes fsg=1 for the *next* frame's assign_std_frame_bits, so the
	// alternating-frame alt_extra_bits cadence lands on odd
	// frames_since_golden — which for the no-ARF path means frames at
	// offset 2, 4, 6, ... after the GF refresh.
	t.framesSinceGolden = 0
}

func (t *twoPassState) defineGFGroupInterval(frame uint64, remaining int) int {
	framesToKey := t.framesToKeyRemaining
	if framesToKey <= 0 || remaining <= 0 || frame >= uint64(len(t.stats)) {
		return 0
	}
	staticSceneMax := t.staticSceneMaxGFInterval
	if staticSceneMax <= 0 {
		staticSceneMax = framesToKey
	}
	maxGFInterval := t.maxGFInterval
	if maxGFInterval <= 0 {
		maxGFInterval = staticSceneMax
	}
	if maxGFInterval > staticSceneMax {
		maxGFInterval = staticSceneMax
	}
	if maxGFInterval <= 0 {
		maxGFInterval = framesToKey
	}

	i := 0
	boostScore := 0.0
	oldBoostScore := 0.0
	decayAccumulator := 1.0
	mvRatioAccumulator := 0.0
	mvInOutAccumulator := 0.0
	absMVInOutAccumulator := 0.0
	loopDecayRate := 1.0

	for ((i < staticSceneMax) || ((framesToKey - i) < libvpxMinGFInterval)) && i < framesToKey {
		i++
		idx := int(frame) + i
		if idx >= len(t.stats) {
			break
		}
		next := t.stats[idx]
		thisFrameMVInOut := next.MVInOutCount * next.PcntMotion
		mvInOutAccumulator += thisFrameMVInOut
		if thisFrameMVInOut < 0 {
			absMVInOutAccumulator -= thisFrameMVInOut
		} else {
			absMVInOutAccumulator += thisFrameMVInOut
		}
		if next.PcntMotion > 0.05 {
			mvR := next.MVr
			if mvR < 0 {
				mvR = -mvR
			}
			if mvR < 1e-12 {
				mvR = 1.0
			}
			mvC := next.MVc
			if mvC < 0 {
				mvC = -mvC
			}
			if mvC < 1e-12 {
				mvC = 1.0
			}
			mvrRatio := next.MVrAbs / mvR
			if mvrRatio < next.MVrAbs {
				mvRatioAccumulator += mvrRatio * next.PcntMotion
			} else {
				mvRatioAccumulator += next.MVrAbs * next.PcntMotion
			}
			mvcRatio := next.MVcAbs / mvC
			if mvcRatio < next.MVcAbs {
				mvRatioAccumulator += mvcRatio * next.PcntMotion
			} else {
				mvRatioAccumulator += next.MVcAbs * next.PcntMotion
			}
		}

		intra := next.IntraError
		if intra < t.gfIntraErrMin {
			intra = t.gfIntraErrMin
		}
		denom := next.CodedError
		if denom > -1e-12 && denom < 1e-12 {
			denom = 1.0
		}
		r := 1.5 * intra / denom
		if thisFrameMVInOut > 0 {
			r += r * (thisFrameMVInOut * 2.0)
		} else {
			r += r * (thisFrameMVInOut / 2.0)
		}
		if r > 48.0 {
			r = 48.0
		}
		loopDecayRate = libvpxGetPredictionDecayRate(next)
		decayAccumulator *= loopDecayRate
		if decayAccumulator < 0.1 {
			decayAccumulator = 0.1
		}
		boostScore += decayAccumulator * r

		if t.detectGFTransitionToStill(frame, i, loopDecayRate, decayAccumulator) {
			break
		}
		if i >= maxGFInterval && decayAccumulator < 0.995 {
			break
		}
		if i > libvpxMinGFInterval &&
			(framesToKey-i) >= libvpxMinGFInterval &&
			((boostScore > 20.0) || (next.PcntInter < 0.75)) &&
			((mvRatioAccumulator > 100.0) ||
				(absMVInOutAccumulator > 3.0) ||
				(mvInOutAccumulator < -2.0) ||
				((boostScore - oldBoostScore) < 2.0)) {
			boostScore = oldBoostScore
			break
		}
		oldBoostScore = boostScore
	}

	if (framesToKey - i) < libvpxMinGFInterval {
		for i < framesToKey {
			i++
			if int(frame)+i >= len(t.stats) {
				break
			}
		}
	}
	if i > remaining {
		i = remaining
	}
	if i < 0 {
		i = 0
	}
	return i
}

func (t *twoPassState) detectGFTransitionToStill(frame uint64, interval int, loopDecayRate float64, decayAccumulator float64) bool {
	const stillInterval = 5
	rates := make([]float64, 0, stillInterval)
	start := int(frame) + interval + 1
	for j := 0; j < stillInterval && start+j < len(t.stats); j++ {
		rates = append(rates, libvpxGetPredictionDecayRate(t.stats[start+j]))
	}
	return libvpxDetectTransitionToStill(interval, stillInterval, loopDecayRate, decayAccumulator, rates)
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
func computeGFUBoost(stats []FirstPassFrameStats, frame uint64, gfInterval int, gfIntraErrMin float64) int {
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
	gfuBoost := int(boostScore*100.0) >> 4
	return gfuBoost
}

// assignStdFrameBits ports libvpx's assign_std_frame_bits inner-loop
// allocator for std P frames inside a GF group. Drains gfGroupBits and
// gfGroupErrorLeft per call.
//
// Verbatim port from vp8/encoder/firstpass.c assign_std_frame_bits
// (lines 2156-2208), v1.16.0. The libvpx body is, in order:
//
//	int max_bits = frame_max_bits(cpi);
//	modified_err = calculate_modified_err(cpi, this_frame);
//	if (cpi->twopass.gf_group_error_left > 0)
//	    err_fraction = modified_err / cpi->twopass.gf_group_error_left;
//	else err_fraction = 0.0;
//	target_frame_size = saturate_cast_double_to_int(
//	    (double)cpi->twopass.gf_group_bits * err_fraction);
//	if (target_frame_size < 0) target_frame_size = 0;
//	else {
//	    if (target_frame_size > max_bits) target_frame_size = max_bits;
//	    if (target_frame_size > cpi->twopass.gf_group_bits)
//	        target_frame_size = (int)cpi->twopass.gf_group_bits;
//	}
//	cpi->twopass.gf_group_error_left -= (int)modified_err;
//	cpi->twopass.gf_group_bits -= target_frame_size;
//	if (cpi->twopass.gf_group_bits < 0) cpi->twopass.gf_group_bits = 0;
//	target_frame_size += cpi->min_frame_bandwidth;
//	if ((cpi->frames_since_golden & 0x01) &&
//	    (cpi->frames_till_gf_update_due > 0))
//	    target_frame_size += cpi->twopass.alt_extra_bits;
//	cpi->per_frame_bandwidth = target_frame_size;
//
// Key invariants govpx mirrors:
//   - `gf_group_error_left -= (int)modified_err` truncates modified_err
//     toward zero (Go's int64() of a positive float64 matches), and the
//     subtraction is allowed to go negative. The next call's
//     `gf_group_error_left > 0` guard then routes through the zero
//     err_fraction branch — semantically the same as clamping to 0, but
//     we keep the unclamped value to mirror libvpx exactly for any
//     callers that read `gfGroupErrorLeft` between calls.
//   - `gf_group_bits` IS clamped to 0 if negative.
//   - target_frame_size's lower clamp at 0 happens before max_bits /
//     gf_group_bits upper clamps (matching libvpx's if/else block above).
//   - min_frame_bandwidth and alt_extra_bits are additive on top of the
//     clamped target_frame_size.
func (t *twoPassState) assignStdFrameBits(modErr float64, maxBits int64) int64 {
	if !t.gfGroupValid {
		return int64(t.minFrameBandwidth)
	}
	var errFraction float64
	if t.gfGroupErrorLeft > 0 {
		errFraction = modErr / t.gfGroupErrorLeft
	}
	// libvpx vp8/encoder/firstpass.c:2175 (v1.16.0):
	//   target_frame_size = saturate_cast_double_to_int(
	//       (double)cpi->twopass.gf_group_bits * err_fraction);
	// saturate_cast_double_to_int clamps at INT_MAX before truncating
	// toward zero. Port the helper so the upper clamp matches libvpx
	// for high-rate two-pass curves; on the 720p VBR ladder this is
	// part of the float-order alignment task #287 brought from
	// +5.14% BD-rate over libvpx down into single-digit territory.
	target := libvpxSaturateCastDoubleToInt(float64(t.gfGroupBits) * errFraction)
	if target < 0 {
		target = 0
	} else {
		if maxBits > 0 && target > maxBits {
			target = maxBits
		}
		if target > t.gfGroupBits {
			target = t.gfGroupBits
		}
	}
	// Drain mirrors libvpx exactly: error_left drains the int-truncated
	// modified_err (no clamp), gf_group_bits drains the clamped target
	// and IS clamped to zero.
	t.gfGroupErrorLeft -= float64(int64(modErr))
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
		// libvpx vp8/encoder/firstpass.c:2162 in assign_std_frame_bits:
		//   int max_bits = frame_max_bits(cpi);
		// dispatches on cpi->oxcf.end_usage (firstpass.c:316-368).
		// govpx routes through twoPassState.frameMaxBits so CBR runs
		// the buffer-aware libvpxFrameMaxBitsCBR branch and VBR/CQ/Q
		// run libvpxFrameMaxBitsVBR.
		framesLeft := int64(len(t.stats)) - int64(frame)
		if maxBits := t.frameMaxBits(framesLeft); maxBits > 0 {
			sectionMax = int64(maxBits)
		}
	}
	if sectionMax < sectionMin {
		sectionMax = sectionMin
	}
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
	// libvpx vp8/encoder/firstpass.c define_gf_group (line 1647)
	// accumulates mod_frame_err per loop iteration into gf_group_err.
	// We mirror that so the ARF-feasibility gate at line 1830 can
	// compute group_bits = kf_group_bits * (gf_group_err /
	// kf_group_error_left). The starting frame's modified error is
	// captured so we can later replicate libvpx's KF-boundary
	// `gf_group_err -= gf_first_frame_err` at line 1633 when applicable.
	gfFirstFrameErr := t.modifiedError(t.stats[currentFrame])
	gfGroupErr := 0.0
	modFrameErr := gfFirstFrameErr
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
		// libvpx vp8/encoder/firstpass.c line 1647: mod_frame_err is
		// the per-iteration calculate_modified_err(cpi, this_frame).
		// At loop exit it holds the modified error of the LAST visited
		// `this_frame`, which is the last frame in the candidate GF
		// section. estimate_q at line 1830 consumes that final value.
		modFrameErr = t.modifiedError(t.stats[idx])
		gfGroupErr += modFrameErr
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
	// libvpx vp8/encoder/firstpass.c lines 1788-1842: after the
	// pre-estimate boost / motion gates pass, libvpx computes the
	// would-be ARF allocation and probes estimate_q. The gate is
	// `tmp_q < cpi->worst_quality`; only then is
	// `cpi->source_alt_ref_pending = 1` set. Wire that final
	// feasibility test here so govpx mirrors the libvpx ARF eligibility
	// decision verbatim instead of stopping at the heuristic gates.
	//
	// The libvpx caller path:
	//   group_bits = kf_group_bits * (gf_group_err / kf_group_error_left)
	//   Boost      = (gfu_boost * 3 * GFQ_ADJUSTMENT) / (2 * 100) + i*50
	//   clamped to [125, (i+1)*200]
	//   allocation_chunks = (i+1)*100 + Boost
	//   while Boost > 1000: Boost/=2; allocation_chunks/=2
	//   arf_frame_bits = Boost * (group_bits / allocation_chunks)
	//   tmp_q = estimate_q(cpi, mod_frame_err, arf_frame_bits)
	//   if (tmp_q < cpi->worst_quality) -> ARF eligible.
	//
	// The gate is skipped when govpx state required to evaluate it has
	// not been initialised (numMBs == 0 or worstQuality == 0 — the
	// pre-configure default), since libvpx's calloc default would not
	// satisfy `tmp_q < 0` and would always fail the gate. Mirroring that
	// strict semantics would block ARFs unconditionally when the encoder
	// has not yet set quantizer bounds; we instead fall back to the
	// boost-only decision in that case so test callers that exercise the
	// pre-estimate gates only continue to behave the same.
	if t.numMBs > 0 && t.worstQuality > 0 {
		// libvpx's KF-boundary case (line 1633) subtracts the
		// gf_first_frame_err from gf_group_err so the keyframe's own
		// error is not counted toward the GF group. govpx mirrors that
		// when currentFrame is the latest seen keyframe.
		gfGroupErrAdj := gfGroupErr
		atKFBoundary := currentFrame == t.lastKeySeen || currentFrame == 0
		if atKFBoundary {
			gfGroupErrAdj -= gfFirstFrameErr
			if gfGroupErrAdj < 0 {
				gfGroupErrAdj = 0
			}
		}
		// libvpx vp8/encoder/firstpass.c flow: find_next_key_frame runs
		// before define_gf_group and populates cpi->twopass.kf_group_bits
		// and kf_group_error_left. govpx's encoder runs pass2DetectARFPending
		// BEFORE prepareKFGroup (the find_next_key_frame port), so on the
		// first frame of a KF group those fields are still zero. Lazy-
		// compute them here so the estimate_q probe sees libvpx-shaped
		// inputs even on the very first frame. The lazy values mirror
		// prepareKFGroup (line 454) but do not mutate twoPassState — this
		// is a pure read-only probe.
		kfGroupBits := t.kfGroupBitsRemaining
		kfGroupErrorLeft := t.kfGroupErrorLeft
		if kfGroupBits <= 0 || kfGroupErrorLeft <= 0 {
			if atKFBoundary && t.bitsLeft > 0 && t.errorLeft > 0 {
				framesToKeyForSeed := min(len(t.stats)-int(currentFrame), framesToKey)
				if framesToKeyForSeed > 0 {
					var kfGroupErr, kfModErr float64
					end := min(currentFrame+uint64(framesToKeyForSeed), uint64(len(t.stats)))
					for i := currentFrame; i < end; i++ {
						kfGroupErr += t.modifiedError(t.stats[i])
					}
					kfModErr = t.modifiedError(t.stats[currentFrame])
					seededKFGroupBits := int64(float64(t.bitsLeft) * (kfGroupErr / t.errorLeft))
					// libvpx vp8/encoder/firstpass.c:2657 in
					// find_next_key_frame dispatches frame_max_bits on
					// cpi->oxcf.end_usage. Mirror that here in the lazy
					// kf-group seed probe so CBR and VBR see the same
					// ceiling as the authoritative prepareKFGroup path.
					maxBits := int64(t.frameMaxBits(int64(framesToKeyForSeed)))
					if maxBits > 0 {
						if cap := maxBits * int64(framesToKeyForSeed); seededKFGroupBits > cap {
							seededKFGroupBits = cap
						}
					}
					if seededKFGroupBits > 0 {
						kfGroupBits = seededKFGroupBits
						kfGroupErrorLeft = kfGroupErr - kfModErr
						if kfGroupErrorLeft < 0 {
							kfGroupErrorLeft = 0
						}
					}
				}
			}
		}
		groupBits := int64(0)
		if kfGroupBits > 0 && kfGroupErrorLeft > 0 {
			groupBits = int64(float64(kfGroupBits) *
				(gfGroupErrAdj / kfGroupErrorLeft))
		}
		if groupBits > 0 {
			// libvpx GFQ_ADJUSTMENT lookup: vp8_gf_boost_qadjustment[Q].
			// Q is libvpx's last_q[INTER_FRAME] (or oxcf.fixed_q when
			// non-negative; govpx never sets fixed_q in pass-2 paths).
			q := max(t.lastInterQ, 0)
			if q >= len(libvpxGFBoostQAdjustment) {
				q = len(libvpxGFBoostQAdjustment) - 1
			}
			gfqAdjustment := libvpxGFBoostQAdjustment[q]
			// libvpx vp8/encoder/firstpass.c lines 1798-1813 (NEW_BOOST
			// branch uses alt_boost here; govpx's gfuBoost computed
			// above mirrors the non-NEW_BOOST gfu_boost — equivalent
			// for the ARF cost-vs-budget probe since both feed the same
			// Boost formula when scaled by GFQ_ADJUSTMENT).
			Boost := (gfuBoost * 3 * gfqAdjustment) / (2 * 100)
			Boost += interval * 50
			if cap := (interval + 1) * 200; Boost > cap {
				Boost = cap
			}
			if Boost < 125 {
				Boost = 125
			}
			allocationChunks := (interval+1)*100 + Boost
			for Boost > 1000 {
				Boost /= 2
				allocationChunks /= 2
				if allocationChunks <= 0 {
					break
				}
			}
			if allocationChunks > 0 {
				arfFrameBits := int(float64(Boost) *
					(float64(groupBits) / float64(allocationChunks)))
				if arfFrameBits > 0 {
					// libvpx estimate_q at line 1084 takes
					// section_target_bandwidth and section_err
					// (mod_frame_err of the last loop frame).
					// speed_correction is 1.0 here because the
					// govpx-default compressor_speed is 0/2; libvpx's
					// 1.04-1.25 speed_correction only fires at speeds
					// 1 or 3.
					estCorrection := t.estMaxQCorrection
					if estCorrection <= 0 {
						estCorrection = 1.0
					}
					errPerMB := modFrameErr / float64(t.numMBs)
					tmpQ := libvpxEstimateQ(t.numMBs, arfFrameBits, errPerMB, 1.0, estCorrection)
					if tmpQ >= t.worstQuality {
						// libvpx vp8/encoder/firstpass.c line 1904:
						// cpi->source_alt_ref_pending = 0 — the ARF
						// would not be codable at a lower Q than the
						// surrounding frames, so it is not worthwhile.
						return 0, false
					}
				}
			}
		}
	}
	return interval, true
}

// pass2AltRefPendingPlan wires the libvpx vp8/encoder/firstpass.c
// `define_gf_group` ARF-pending decision into the encoder. It runs at a
// GF-group boundary (framesTillAltRefFrame == 0 and ARF not already pending or
// active), and pass2ArmAltRefPending calls scheduleAltRefSource so the auto-ARF
// driver can emit the hidden alt-ref at the predicted offset.
//
// libvpx fires this from `vp8_second_pass`, which runs on every
// non-hidden frame including the keyframe (find_next_key_frame zeros
// `frames_till_gf_update_due` so the same `if (frames_till_gf_update_due
// == 0)` predicate triggers `define_gf_group` from inside Pass2Encode
// for the keyframe). govpx mirrors that by not special-casing keyframes in the
// caller; the keyframe-path lifecycle update inside `resetGoldenFrameStats` no
// longer clobbers the schedule (it now matches libvpx's
// `update_golden_frame_stats`, which leaves
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
