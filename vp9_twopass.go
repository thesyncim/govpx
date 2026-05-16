package govpx

// govpx VP9 two-pass parity status vs libvpx v1.16.0
//
// Ported verbatim with libvpx file:line citations (this file):
//   - get_distribution_av_err          → distributionAverageError
//     libvpx: vp9/encoder/vp9_firstpass.c:251
//   - calculate_mod_frame_score        → modifiedFrameScore
//     libvpx: vp9/encoder/vp9_firstpass.c:265
//   - calc_norm_frame_score            → normalizedFrameScore
//     libvpx: vp9/encoder/vp9_firstpass.c:285
//   - calculate_active_area            → activeArea
//     libvpx: vp9/encoder/vp9_firstpass.c:239
//   - vp9_init_second_pass (subset)    → configure
//     libvpx: vp9/encoder/vp9_firstpass.c:1621
//   - vp9_rc_clamp_pframe_target_size  → min floor + maxPct cap in
//     frameTargetBits
//     libvpx: vp9/encoder/vp9_ratectrl.c:218
//   - vbr_rate_correction              → applyVBRRateCorrection
//     libvpx: vp9/encoder/vp9_ratectrl.c:2683
//   - vp9_twopass_postencode_update    → finishFrameWithActual
//     libvpx: vp9/encoder/vp9_firstpass.c:3733
//
// Deferred — these require Lagrangian RD shape and VP9_COMP-scale state
// that govpx does not carry today. Each path falls back to a documented
// approximation; the comments below pin the libvpx source the agent
// should port when the surrounding feature gates land.
//
//   TODO: requires Lagrangian RD shape — define_gf_group (the adaptive
//   GOP analyzer). libvpx: vp9/encoder/vp9_firstpass.c:2761
//   define_gf_group reads first-pass motion / SR-coded / zero-motion
//   accumulators across a forward window and emits an ALTREF stack +
//   per-frame rate factor levels. govpx instead treats each show frame
//   as its own normal-frame allocation cell, which is correct for the
//   no-altref / lag=0 path but diverges from libvpx whenever
//   AutoAltRef is enabled.
//
//   TODO: requires Lagrangian RD shape — vp9_rc_pick_q_and_bounds_two_pass
//   (the per-frame qindex picker that consumes layer_depth /
//   rate_factor_level / arf_active_best_quality_adjustment_factor /
//   extend_minq / extend_maxq from the GF group structure).
//   libvpx: vp9/encoder/vp9_ratectrl.c:1468
//   govpx's quantizer regulator (vp9_ratectrl_quantizer.go
//   vbrQuantizerWithBounds) reuses the one-pass-VBR active-best /
//   active-worst flow with the two-pass frame target; it doesn't
//   read the GF group state machine.
//
//   TODO: requires Lagrangian RD shape — calc_arf_boost / kf_boost
//   from boost_factor + sr_decay_rate + zero_motion_factor.
//   libvpx: vp9/encoder/vp9_firstpass.c:1936 compute_arf_boost
//   libvpx: vp9/encoder/vp9_firstpass.c:1891 calc_kf_frame_boost
//   These read the rolling first-pass motion accumulators and feed
//   gf_group.gfu_boost[] which in turn modulates active_best_quality
//   via get_gf_active_quality. govpx approximates the active-best
//   bias via vp9_ratectrl_quantizer.vp9GFActiveQuality without the
//   first-pass-driven boost factor.
//
//   TODO: requires Lagrangian RD shape — find_next_key_frame +
//   kf_zeromotion_pct consumers. libvpx:
//   vp9/encoder/vp9_firstpass.c (find_next_key_frame)
//   vp9/encoder/vp9_ratectrl.c:1598 (STATIC_MOTION_THRESH path)
//   govpx scenecut/forceKey lives in vp9_scenecut.go but doesn't
//   accumulate the libvpx static-motion percentage.
//
// Pass-1 stats schema parity: VP9FirstPassFrameStats fields are
// byte-aligned with FIRSTPASS_STATS (vp9_firstpass_stats.h:20). The
// GOVPX_VP9_FIRSTPASS_STRICT-tagged oracle test (vp9_oracle_firstpass_test.go)
// asserts the field-by-field deltas against the libvpx pass-1 dump and
// is the gate that catches schema drift.

import "math"

const (
	// vp9DefaultTwoPassVBRBiasPct mirrors the libvpx VP9 default of 50.
	// libvpx: vp9/encoder/vp9_encoder.c set_rc_buffer_sizes / oxcf->two_pass_vbrbias
	vp9DefaultTwoPassVBRBiasPct = 50
	// vp9MinActiveArea / vp9MaxActiveArea / vp9ActiveAreaCorrection mirror
	// MIN_ACTIVE_AREA / MAX_ACTIVE_AREA / ACT_AREA_CORRECTION.
	// libvpx: vp9/encoder/vp9_firstpass.c:262 (ACT_AREA_CORRECTION) and
	// vp9_firstpass.c calculate_active_area().
	vp9MinActiveArea        = 0.5
	vp9MaxActiveArea        = 1.0
	vp9ActiveAreaCorrection = 0.5

	// vp9TwoPassVBRPctAdjustmentLimit mirrors VBR_PCT_ADJUSTMENT_LIMIT.
	// libvpx: vp9/encoder/vp9_ratectrl.c:2681
	vp9TwoPassVBRPctAdjustmentLimit = 50
	// vp9TwoPassVBRFrameWindowMax mirrors the window cap inside
	// vbr_rate_correction().
	// libvpx: vp9/encoder/vp9_ratectrl.c:2687
	vp9TwoPassVBRFrameWindowMax = 16
)

// vp9TwoPassState tracks the second-pass VBR budget and per-frame score
// distribution. The model mirrors libvpx's TWO_PASS struct in shape and
// math: each frame contributes a modified-score; per-frame target is
// `bits_left * normalized_score / normalized_score_left`, then refined by
// the running `vbr_bits_off_target` feedback loop.
//
// libvpx parity references:
//   - vp9/encoder/vp9_firstpass.c:1621 vp9_init_second_pass (initial budget,
//     mean_mod_score, normalized_score_left)
//   - vp9/encoder/vp9_firstpass.c:265 calculate_mod_frame_score
//   - vp9/encoder/vp9_firstpass.c:285 calc_norm_frame_score
//   - vp9/encoder/vp9_ratectrl.c:2683 vbr_rate_correction
//   - vp9/encoder/vp9_firstpass.c:3733 vp9_twopass_postencode_update
type vp9TwoPassState struct {
	stats               []VP9FirstPassFrameStats
	totalStats          VP9FirstPassFrameStats
	bitsLeft            int64
	normalizedScoreLeft float64
	meanModScore        float64
	frameIndex          uint64
	currentTargetBits   int
	// baseFrameTarget is the assigned target before vbr_rate_correction.
	// libvpx: vp9/encoder/vp9_ratectrl.c rc->base_frame_target
	baseFrameTarget int
	// vbrBitsOffTarget tracks the cumulative drift between assigned per-frame
	// targets and actual encoded bits, mirroring rc->vbr_bits_off_target.
	// libvpx: vp9/encoder/vp9_ratectrl.c rc->vbr_bits_off_target
	vbrBitsOffTarget  int64
	vbrBiasPct        int
	minPct            int
	maxPct            int
	minFrameBandwidth int
	// avgFrameBandwidth is the libvpx rc->avg_frame_bandwidth value used to
	// derive max_frame_bandwidth and vbr_max_bits clamps.
	// libvpx: vp9/encoder/vp9_ratectrl.c:2655
	avgFrameBandwidth int
	mbRows            int
}

func validateVP9TwoPassOptions(opts VP9EncoderOptions) error {
	if opts.TwoPassVBRBiasPct < 0 || opts.TwoPassMinPct < 0 ||
		opts.TwoPassMaxPct < 0 {
		return ErrInvalidConfig
	}
	if len(opts.TwoPassStats) == 0 {
		return nil
	}
	if !opts.RateControlModeSet ||
		(opts.RateControlMode != RateControlVBR &&
			opts.RateControlMode != RateControlCQ) {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP9Encoder) prepareVP9SecondPassFrameTarget(intraOnly bool, refreshFlags uint8) {
	e.vp9TwoPassFrameTarget = 0
	if e.twoPass.enabled() {
		if target := e.twoPass.frameTargetBits(e.rc.frameTargetBits); target > 0 {
			e.rc.frameTargetBits = target
			e.vp9TwoPassFrameTarget = target
			return
		}
	}
	e.rc.setOnePassVBRFrameTarget(intraOnly, refreshFlags)
}

func (t *vp9TwoPassState) configure(stats []VP9FirstPassFrameStats, bitsPerFrame int,
	biasPct int, minPct int, maxPct int, height int,
) {
	*t = vp9TwoPassState{}
	if len(stats) == 0 || bitsPerFrame <= 0 {
		return
	}
	t.stats, t.totalStats = normalizeVP9TwoPassStats(stats)
	if len(t.stats) == 0 {
		return
	}
	t.vbrBiasPct = biasPct
	if t.vbrBiasPct <= 0 {
		t.vbrBiasPct = vp9DefaultTwoPassVBRBiasPct
	}
	t.minPct = minPct
	t.maxPct = maxPct
	if t.maxPct <= 0 {
		t.maxPct = vp9DefaultVBRMaxSectionPct
	}
	t.minFrameBandwidth = vbrMinFrameBandwidthBits(bitsPerFrame, t.minPct)
	t.avgFrameBandwidth = bitsPerFrame
	// libvpx: vp9/encoder/vp9_firstpass.c:1702 — bits_left =
	// stats->duration * target_bandwidth / 10000000. We don't carry the
	// 10us tick basis, so substitute bitsPerFrame*frameCount which equals
	// the libvpx value when the source framerate is constant. Each frame
	// row in libvpx contributes one tick × duration; in govpx every frame
	// has duration=1 in tick units, matching the libvpx steady state.
	t.bitsLeft = int64(bitsPerFrame) * int64(len(t.stats))
	t.mbRows = (height + 15) >> 4
	if t.mbRows <= 0 {
		t.mbRows = 1
	}

	// libvpx: vp9/encoder/vp9_firstpass.c:1642 — two-scan mean_mod_score
	// followed by normalized_score_left under the clamped scoring.
	avErr := t.distributionAverageError()
	rawTotal := 0.0
	for i := range t.stats {
		rawTotal += t.modifiedFrameScore(t.stats[i], avErr)
	}
	t.meanModScore = rawTotal / nonZeroFloat(t.totalStats.Count)
	if t.meanModScore <= 0 {
		t.meanModScore = 1
	}
	for i := range t.stats {
		t.normalizedScoreLeft += t.normalizedFrameScore(t.stats[i], avErr)
	}
	if t.normalizedScoreLeft <= 0 {
		t.normalizedScoreLeft = float64(len(t.stats))
	}
}

func (t *vp9TwoPassState) enabled() bool {
	return len(t.stats) > 0
}

func (t *vp9TwoPassState) statsForFrame() VP9FirstPassFrameStats {
	if !t.enabled() || t.frameIndex >= uint64(len(t.stats)) {
		return VP9FirstPassFrameStats{}
	}
	return t.stats[t.frameIndex]
}

// frameTargetBits returns the libvpx-style second-pass per-frame target
// (base_frame_target after vbr_rate_correction).
//
// libvpx parity references:
//   - vp9/encoder/vp9_firstpass.c calculate_total_gf_group_bits + the
//     per-group bit_allocation populated in allocate_gf_group_bits;
//     when we treat each frame as its own normal-frame GOP cell, the
//     allocated target is bits_left * norm_score / norm_score_left.
//   - vp9/encoder/vp9_ratectrl.c:218 vp9_rc_clamp_pframe_target_size
//     (min/max clamps using avg_frame_bandwidth).
//   - vp9/encoder/vp9_ratectrl.c:2683 vbr_rate_correction
//     (vbr_bits_off_target feedback over a frame_window).
//
// defaultTargetBits is the libvpx one-pass per-frame target the caller
// would have used; it's retained as a per-frame default-cap reference so
// CQ/CBR-style upstreams continue to govern when no two-pass score
// dominates.
func (t *vp9TwoPassState) frameTargetBits(defaultTargetBits int) int {
	t.currentTargetBits = 0
	t.baseFrameTarget = 0
	if !t.enabled() || t.frameIndex >= uint64(len(t.stats)) ||
		defaultTargetBits <= 0 {
		return 0
	}
	score := t.normalizedFrameScore(t.stats[t.frameIndex],
		t.distributionAverageError())
	if score <= 0 || t.normalizedScoreLeft <= 0 || t.bitsLeft <= 0 {
		return 0
	}
	// libvpx: bit_allocation = total_group_bits * norm_score / tot_norm_score
	// (the corpus-vbr branch in allocate_gf_group_bits and the simple
	// section_target_bandwidth path in vp9_rc_get_second_pass_params).
	target := int64(float64(t.bitsLeft) * score / t.normalizedScoreLeft)

	// libvpx: vp9_rc_clamp_pframe_target_size — min_frame_target =
	// max(min_frame_bandwidth, avg_frame_bandwidth >> 5).
	avgFB := int64(t.avgFrameBandwidth)
	if avgFB <= 0 {
		avgFB = int64(defaultTargetBits)
	}
	minFloor := int64(t.minFrameBandwidth)
	if shift := avgFB >> 5; shift > minFloor {
		minFloor = shift
	}
	if target < minFloor {
		target = minFloor
	}

	// libvpx: vp9_rc_update_framerate — vbr_max_bits =
	// avg_frame_bandwidth * two_pass_vbrmax_section / 100. This is the
	// canonical libvpx cap. Earlier govpx versions used defaultTargetBits
	// for the cap which underestimates on key/boost frames; using
	// avgFrameBandwidth (the libvpx average) keeps the cap stable.
	if t.maxPct > 0 {
		maxBits := avgFB * int64(t.maxPct) / 100
		if maxBits > 0 && target > maxBits {
			target = maxBits
		}
	}

	// libvpx: vbr_rate_correction (vp9_ratectrl.c:2683). Apply the
	// running vbr_bits_off_target feedback so cumulative over/undershoot
	// is bled back into subsequent frame targets over a 16-frame window
	// (capped to VBR_PCT_ADJUSTMENT_LIMIT% of the current target).
	t.baseFrameTarget = int(target)
	target = max(t.applyVBRRateCorrection(target), int64(vp9FrameOverhead))
	if target > int64(maxInt()) {
		target = int64(maxInt())
	}
	t.currentTargetBits = int(target)
	return t.currentTargetBits
}

// applyVBRRateCorrection mirrors libvpx vbr_rate_correction.
// libvpx: vp9/encoder/vp9_ratectrl.c:2683-2723.
func (t *vp9TwoPassState) applyVBRRateCorrection(target int64) int64 {
	remaining := int64(len(t.stats)) - int64(t.frameIndex)
	frameWindow := min(remaining, vp9TwoPassVBRFrameWindowMax)
	if frameWindow <= 0 {
		return target
	}
	off := t.vbrBitsOffTarget
	var maxDelta int64
	if off > 0 {
		maxDelta = off / frameWindow
	} else {
		maxDelta = -off / frameWindow
	}
	limit := target * int64(vp9TwoPassVBRPctAdjustmentLimit) / 100
	if maxDelta > limit {
		maxDelta = limit
	}
	if off > 0 {
		applied := min(off, maxDelta)
		target += applied
	} else {
		applied := min(-off, maxDelta)
		target -= applied
	}
	return target
}

// finishFrame advances the second-pass cursor without an actual-bits
// observation. Used for dropped frames or when the encoder front-end
// hasn't wired postencodeFrameSize through yet.
func (t *vp9TwoPassState) finishFrame() {
	t.finishFrameWithActual(0)
}

// finishFrameWithActual updates bits_left and vbr_bits_off_target using
// the actual encoded frame size, mirroring vp9_twopass_postencode_update.
// libvpx: vp9/encoder/vp9_firstpass.c:3733 vp9_twopass_postencode_update.
//
// projectedFrameSize is the encoded bitstream size in bits; pass 0 from
// the front-end if it's not threaded through. With actualBits=0 we
// fall back to the assigned target which matches libvpx's behavior
// before the postencode call.
func (t *vp9TwoPassState) finishFrameWithActual(projectedFrameSize int) {
	if !t.enabled() || t.frameIndex >= uint64(len(t.stats)) {
		return
	}
	score := t.normalizedFrameScore(t.stats[t.frameIndex],
		t.distributionAverageError())
	t.normalizedScoreLeft -= score
	if t.normalizedScoreLeft < 0 {
		t.normalizedScoreLeft = 0
	}
	// libvpx: bits_used = rc->base_frame_target (the pre-correction
	// target). bits_left is reduced by bits_used; vbr_bits_off_target
	// accumulates (base_frame_target - projected_frame_size).
	bitsUsed := int64(t.baseFrameTarget)
	if bitsUsed <= 0 {
		bitsUsed = int64(t.currentTargetBits)
	}
	if bitsUsed <= 0 {
		bitsUsed = int64(vp9FrameOverhead)
	}
	if projectedFrameSize > 0 {
		t.vbrBitsOffTarget += bitsUsed - int64(projectedFrameSize)
	}
	t.bitsLeft -= bitsUsed
	if t.bitsLeft < 0 {
		t.bitsLeft = 0
	}
	t.frameIndex++
	t.currentTargetBits = 0
	t.baseFrameTarget = 0
}

func (t *vp9TwoPassState) distributionAverageError() float64 {
	if t.totalStats.Count <= 0 {
		return 1
	}
	// libvpx: vp9/encoder/vp9_firstpass.c:251 get_distribution_av_err.
	avgWeight := t.totalStats.Weight / t.totalStats.Count
	if avgWeight <= 0 {
		avgWeight = 1
	}
	avErr := (t.totalStats.CodedError * avgWeight) / t.totalStats.Count
	if avErr <= 0 {
		return 1
	}
	return avErr
}

func (t *vp9TwoPassState) modifiedFrameScore(row VP9FirstPassFrameStats, avErr float64) float64 {
	// libvpx: vp9/encoder/vp9_firstpass.c:265 calculate_mod_frame_score.
	err := row.CodedError
	if err < 1 {
		err = 1
	}
	weight := row.Weight
	if weight <= 0 {
		weight = 1
	}
	score := avErr * math.Pow((err*weight)/nonZeroFloat(avErr),
		float64(t.vbrBiasPct)/100.0)
	score *= math.Pow(t.activeArea(row), vp9ActiveAreaCorrection)
	if score <= 0 || math.IsNaN(score) || math.IsInf(score, 0) {
		return 1
	}
	return score
}

func (t *vp9TwoPassState) normalizedFrameScore(row VP9FirstPassFrameStats, avErr float64) float64 {
	// libvpx: vp9/encoder/vp9_firstpass.c:285 calc_norm_frame_score —
	// modified_score / mean_mod_score, then clamped to [min_pct/100,
	// max_pct/100].
	score := t.modifiedFrameScore(row, avErr) / nonZeroFloat(t.meanModScore)
	minScore := float64(t.minPct) / 100.0
	maxScore := float64(t.maxPct) / 100.0
	if maxScore <= 0 {
		maxScore = float64(vp9DefaultVBRMaxSectionPct) / 100.0
	}
	if score < minScore {
		score = minScore
	}
	if score > maxScore {
		score = maxScore
	}
	if score <= 0 || math.IsNaN(score) || math.IsInf(score, 0) {
		return 1
	}
	return score
}

func (t *vp9TwoPassState) activeArea(row VP9FirstPassFrameStats) float64 {
	// libvpx: vp9/encoder/vp9_firstpass.c:239 calculate_active_area.
	active := 1.0 - ((row.IntraSkipPct / 2.0) +
		((row.InactiveZoneRows * 2.0) / float64(t.mbRows)))
	if active < vp9MinActiveArea {
		return vp9MinActiveArea
	}
	if active > vp9MaxActiveArea {
		return vp9MaxActiveArea
	}
	return active
}

func normalizeVP9TwoPassStats(stats []VP9FirstPassFrameStats) ([]VP9FirstPassFrameStats, VP9FirstPassFrameStats) {
	if len(stats) == 0 {
		return nil, VP9FirstPassFrameStats{}
	}
	if len(stats) > 1 {
		last := stats[len(stats)-1]
		if last.IsTotal {
			return stats[:len(stats)-1], last
		}
	}
	var total VP9FirstPassFrameStats
	for i := range stats {
		accumulateVP9FirstPassStats(&total, stats[i])
	}
	return stats, total
}

func nonZeroFloat(v float64) float64 {
	if v < 1e-12 && v > -1e-12 {
		return 1
	}
	return v
}
