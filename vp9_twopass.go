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
// PORTED (see vp9_gf_group.go / vp9_rc_pick_q_two_pass.go):
//   - define_gf_group               libvpx: vp9/encoder/vp9_firstpass.c:2761
//   - get_active_gf_inverval_range  libvpx: vp9/encoder/vp9_firstpass.c:2701
//   - get_gop_coding_frame_num      libvpx: vp9/encoder/vp9_firstpass.c:2587
//   - calculate_total_gf_group_bits libvpx: vp9/encoder/vp9_firstpass.c:2057
//   - calculate_boost_bits          libvpx: vp9/encoder/vp9_firstpass.c:2102
//   - find_arf_order (single-ARF)   libvpx: vp9/encoder/vp9_firstpass.c:2146
//   - define_gf_group_structure     libvpx: vp9/encoder/vp9_firstpass.c:2218
//   - allocate_gf_group_bits        libvpx: vp9/encoder/vp9_firstpass.c:2391
//   - adjust_group_arnr_filter      libvpx: vp9/encoder/vp9_firstpass.c:2541
//   - vp9_rc_pick_q_and_bounds_two_pass
//                                   libvpx: vp9/encoder/vp9_ratectrl.c:1468
//   - compute_arf_boost             libvpx: vp9/encoder/vp9_firstpass.c:1936
//     (already ported in 54d68f7; re-exported through vp9DefineGFGroup)
//
// rc.gfuBoost is now fed at every GF boundary by refreshVP9GFGroupIfDue
// (this file), which activates the AltRef adaptive-strength path in
// vp9_arnr.go::applyVP9ARNRFilter.
//
// Deferred — these require state govpx does not yet carry; the libvpx
// citations below pin where to port them when the surrounding feature
// gates land.
//
//   TODO: multi-ARF recursion past depth=1.
//   libvpx: vp9/encoder/vp9_firstpass.c:2191 / :2200 (find_arf_order
//   self-recursive case). Requires lookahead fan-out and
//   cpi->multi_layer_arf. govpx today emits the base ARF + leaf
//   P-frames; deeper ALTREF layers (gf_group layer_depth > 1) are out
//   of scope until the lookahead supports multi-source ARF buffers.
//
//   TODO: kf_zeromotion_pct accumulator and the STATIC_MOTION_THRESH
//   path in pick_kf_q_bound_two_pass.
//   libvpx: vp9/encoder/vp9_firstpass.c find_next_key_frame +
//          vp9/encoder/vp9_ratectrl.c:1598 STATIC_MOTION_THRESH path.
//   The Q picker port currently surfaces LastKFGroupZeroMotionPct as an
//   input but the encoder always passes 0 because find_next_key_frame
//   isn't yet ported.
//
//   PORTED: vbr_corpus_complexity consumer.
//   libvpx: vp9/encoder/vp9_firstpass.c:1647-1682 (init_second_pass
//   corpus branch), :2503-2516 (allocate_gf_group_bits corpus branch),
//   vp9/encoder/vp9_ratectrl.c:2734 (vp9_set_target_rate skip), and
//   vp9/encoder/vp9_speed_features.c:321-324 (recode-loop fork). The
//   govpx surface is VP9EncoderOptions.VBRCorpusComplexity.
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
	// gfGroup is the currently-active vp9GFGroup decision produced by
	// vp9DefineGFGroup. framesTillGFUpdate counts down to the next
	// boundary; when it hits zero we recompute the GF group and refresh
	// rc.gfuBoost.
	//
	// libvpx: vp9/encoder/vp9_firstpass.c:2761 define_gf_group +
	//        vp9/encoder/vp9_ratectrl.h RATE_CONTROL::gfu_boost
	gfGroup             vp9GFGroup
	gfGroupActive       bool
	framesTillGFUpdate  int
	gfGroupStartShowIdx int
	// baseFrameTarget is the assigned target before vbr_rate_correction.
	// libvpx: vp9/encoder/vp9_ratectrl.c rc->base_frame_target
	baseFrameTarget int
	// vbrBitsOffTarget tracks the cumulative drift between assigned per-frame
	// targets and actual encoded bits, mirroring rc->vbr_bits_off_target.
	// libvpx: vp9/encoder/vp9_ratectrl.c rc->vbr_bits_off_target
	vbrBitsOffTarget int64
	vbrBiasPct       int
	minPct           int
	maxPct           int
	// vbrCorpusComplexity mirrors oxcf->vbr_corpus_complexity. When
	// non-zero, twopass init forces mean_mod_score = value/10.0 and
	// scales the clip target bandwidth by normalized_score_left/count.
	// libvpx: vp9/encoder/vp9_firstpass.c:1647-1682.
	vbrCorpusComplexity int
	minFrameBandwidth   int
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
	// libvpx: vp9/vp9_cx_iface.c:206 RANGE_CHECK(cfg,
	// rc_2pass_vbr_corpus_complexity, 0, 10000).
	if opts.VBRCorpusComplexity < 0 || opts.VBRCorpusComplexity > 10000 {
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
		// libvpx: vp9_rc_get_second_pass_params calls define_gf_group at
		// each GF boundary (frames_till_gf_update_due == 0). The boost
		// it produces (rc->gfu_boost) feeds adjust_arnr_filter and the
		// gf_active_quality picker. Mirror that boundary here so the
		// AltRef adaptive-strength path (vp9_arnr.go) sees a non-zero
		// boost when the two-pass stats are available.
		e.refreshVP9GFGroupIfDue(intraOnly)
		if target := e.twoPass.frameTargetBits(e.rc.frameTargetBits); target > 0 {
			e.rc.frameTargetBits = target
			e.vp9TwoPassFrameTarget = target
			return
		}
	}
	e.rc.setOnePassVBRFrameTarget(intraOnly, refreshFlags)
}

// refreshVP9GFGroupIfDue (re)runs vp9DefineGFGroup at every GF boundary
// and refreshes rc.gfuBoost so the downstream ARNR adaptive-strength
// path is fed.
//
// libvpx: vp9/encoder/vp9_firstpass.c:3696 (call site inside
// vp9_rc_get_second_pass_params).
func (e *VP9Encoder) refreshVP9GFGroupIfDue(isKey bool) {
	if !e.twoPass.enabled() {
		return
	}
	due := !e.twoPass.gfGroupActive || isKey || e.twoPass.framesTillGFUpdate <= 0
	if !due {
		e.twoPass.framesTillGFUpdate--
		return
	}
	in := e.buildVP9GFGroupInputs(isKey)
	gf := vp9DefineGFGroup(in)
	e.twoPass.gfGroup = gf
	e.twoPass.gfGroupActive = true
	e.twoPass.gfGroupStartShowIdx = in.GFStartShowIdx
	interval := gf.BaselineGFInterval
	if interval <= 0 {
		interval = int(e.rc.baselineGFInterval)
	}
	if interval <= 0 {
		interval = vp9MinGFInterval
	}
	e.twoPass.framesTillGFUpdate = interval - 1
	if gf.GFUBoostScalar > 0 {
		boost := min(gf.GFUBoostScalar, 0xFFFF)
		e.rc.gfuBoost = uint16(boost)
	}
}

// buildVP9GFGroupInputs snapshots the encoder + RC state into the pure
// inputs vp9DefineGFGroup consumes. Mirrors libvpx's VP9_COMP / RATE_CONTROL
// / TWO_PASS field reads at the define_gf_group call site.
func (e *VP9Encoder) buildVP9GFGroupInputs(isKey bool) vp9GFGroupInputs {
	mbRows := (e.opts.Height + 15) >> 4
	if mbRows <= 0 {
		mbRows = 1
	}
	minGF := int(e.rc.minGFInterval)
	if minGF <= 0 {
		minGF = vp9MinGFInterval
	}
	maxGF := int(e.rc.maxGFInterval)
	if maxGF <= 0 {
		maxGF = vp9MaxGFInterval
	}
	staticMax := maxGF
	if staticMax < vp9MaxStaticGFGroupLength {
		staticMax = min(maxGF*4, vp9MaxStaticGFGroupLength)
	}
	framesToKey := e.opts.MaxKeyframeInterval - int(e.framesSinceKey)
	if framesToKey <= 0 {
		framesToKey = e.opts.MaxKeyframeInterval
		if framesToKey <= 0 {
			framesToKey = vp9MaxGFInterval
		}
	}
	startShowIdx := int(e.twoPass.frameIndex)
	avErr := e.twoPass.distributionAverageError()
	return vp9GFGroupInputs{
		IsKeyFrame:               isKey,
		SourceAltRefActive:       false,
		FramesToKey:              framesToKey,
		FramesSinceKey:           int(e.framesSinceKey),
		MinGFInterval:            minGF,
		MaxGFInterval:            maxGF,
		StaticSceneMaxGFInterval: staticMax,
		ActiveWorstQuality:       int(e.rc.worstQuality),
		LastBoostedQIndex:        int(e.rc.lastBoostedQIndex),
		AvgFrameQIndexInter:      int(e.rc.avgFrameQIndexInter),
		AvgFrameBandwidth:        e.rc.bitsPerFrame,
		LagInFrames:              max(e.opts.LookaheadFrames, 1),
		PerceptualAQ:             e.opts.AQMode == VP9AQPerceptual,
		Lossless:                 false,
		AllowAltRef:              e.opts.LookaheadFrames > 0,
		EnableAutoARF:            1,
		MultiLayerARF:            false,
		FrameHeight:              e.opts.Height,
		FrameWidth:               e.opts.Width,
		MBRows:                   mbRows,
		KFGroupBits:              int64(e.rc.bitsPerFrame) * int64(framesToKey),
		KFGroupErrorLeft:         e.twoPass.normalizedScoreLeft,
		FrameMaxBits:             e.rc.maxFrameBandwidth,
		GFMaxTotalBoost:          vp9MaxGFBoost,
		CurrentVideoFrame:        e.frameIndex,
		MeanModScore:             e.twoPass.meanModScore,
		AvErr:                    avErr,
		Stats:                    e.twoPass.stats,
		GFStartShowIdx:           startShowIdx,
		BoostParams:              VP9DefaultARFBoostParams(mbRows),
		VBRCorpusComplexity:      e.opts.VBRCorpusComplexity,
		TwoPassVBRBiasPct:        e.twoPass.vbrBiasPct,
		TwoPassVBRMinSection:     e.twoPass.minPct,
		TwoPassVBRMaxSection:     e.twoPass.maxPct,
	}
}

func (t *vp9TwoPassState) configure(stats []VP9FirstPassFrameStats, bitsPerFrame int,
	biasPct int, minPct int, maxPct int, height int,
) {
	t.configureWithCorpus(stats, bitsPerFrame, biasPct, minPct, maxPct, height, 0)
}

// configureWithCorpus mirrors libvpx vp9_init_second_pass with the
// vbr_corpus_complexity branch in vp9_firstpass.c:1647-1682 enabled when
// vbrCorpusComplexity != 0.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1621 vp9_init_second_pass.
func (t *vp9TwoPassState) configureWithCorpus(stats []VP9FirstPassFrameStats,
	bitsPerFrame int, biasPct int, minPct int, maxPct int, height int,
	vbrCorpusComplexity int,
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
	t.vbrCorpusComplexity = vbrCorpusComplexity
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

	// libvpx: vp9/encoder/vp9_firstpass.c:1642-1662 — when
	// oxcf->vbr_corpus_complexity is non-zero, mean_mod_score is forced
	// to vbr_corpus_complexity/10.0 first and then av_err is recomputed
	// via get_distribution_av_err which switches to the corpus-weighted
	// branch (`av_weight * mean_mod_score`). The raw mod-score scan is
	// skipped on this branch; otherwise the per-clip raw scan derives
	// mean_mod_score using the non-corpus av_err.
	if t.vbrCorpusComplexity != 0 {
		t.meanModScore = float64(t.vbrCorpusComplexity) / 10.0
	}
	avErr := t.distributionAverageError()
	if t.vbrCorpusComplexity == 0 {
		rawTotal := 0.0
		for i := range t.stats {
			rawTotal += t.modifiedFrameScore(t.stats[i], avErr)
		}
		t.meanModScore = rawTotal / nonZeroFloat(t.totalStats.Count)
	}
	if t.meanModScore <= 0 {
		t.meanModScore = 1
	}
	for i := range t.stats {
		t.normalizedScoreLeft += t.normalizedFrameScore(t.stats[i], avErr)
	}
	if t.normalizedScoreLeft <= 0 {
		t.normalizedScoreLeft = float64(len(t.stats))
	}

	// libvpx vp9_firstpass.c:1678-1682 — when corpus VBR is enabled the
	// effective clip target bandwidth is scaled by the clip's overall
	// complexity score relative to the corpus mean. bits_left is the
	// encoder-visible budget for the remainder of the clip, so scale it
	// here so frameTargetBits picks up the corpus-relative budget on the
	// first call.
	if t.vbrCorpusComplexity != 0 && t.totalStats.Count > 0 {
		scale := t.normalizedScoreLeft / t.totalStats.Count
		if scale > 0 {
			t.bitsLeft = int64(float64(t.bitsLeft) * scale)
		}
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
	//
	// libvpx vp9_ratectrl.c:2734 — vp9_set_target_rate skips
	// vbr_rate_correction when oxcf->vbr_corpus_complexity is non-zero
	// (corpus VBR relies on its own pre-scan budget scaling and does
	// not apply the per-frame drift feedback loop).
	t.baseFrameTarget = int(target)
	if t.vbrCorpusComplexity == 0 {
		target = t.applyVBRRateCorrection(target)
	}
	target = min(max(target, int64(vp9FrameOverhead)), int64(maxInt()))
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
	// libvpx: vp9/encoder/vp9_firstpass.c:251-260 get_distribution_av_err.
	// The corpus-VBR branch returns `av_weight * twopass->mean_mod_score`
	// (vp9_firstpass.c:255-256); the default branch returns
	// `(total_stats.coded_error * av_weight) / total_stats.count`.
	avgWeight := t.totalStats.Weight / t.totalStats.Count
	if avgWeight <= 0 {
		avgWeight = 1
	}
	if t.vbrCorpusComplexity != 0 {
		avErr := avgWeight * t.meanModScore
		if avErr <= 0 {
			return 1
		}
		return avErr
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
