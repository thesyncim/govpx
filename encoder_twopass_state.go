package govpx

type twoPassState struct {
	stats      []FirstPassFrameStats
	totalStats FirstPassFrameStats
	// totalLeftStats mirrors libvpx's `cpi->twopass.total_left_stats`
	// (vp8/encoder/firstpass.c). vp8_init_second_pass seeds
	// total_left_stats to a copy of total_stats (firstpass.c line 1230);
	// vp8_second_pass then drains the current frame's FIRSTPASS_STATS
	// via subtract_stats at the end of each pass-2 invocation
	// (firstpass.c line 2398). The resulting value at the top of frame
	// N is total - sum(frame[0..N-1]), i.e. the stats representing the
	// still-unencoded tail of the clip. estimate_modemvcost /
	// estimate_max_q / estimate_cq all read this pointer (firstpass.c
	// lines 2325, 2338, 2349, 2381), so per-frame overhead and worst-Q
	// estimates track the remaining-section averages rather than the
	// full-sequence totals.
	totalLeftStats    FirstPassFrameStats
	bitsLeft          int64
	errorLeft         float64
	frameIndex        uint64
	vbrBiasPct        int
	minPct            int
	maxPct            int
	minFrameBandwidth int
	lastKeySeen       uint64
	// errorResilient mirrors any non-zero libvpx error_resilient_mode bit.
	// In pass 2, libvpx treats this as a special case that disables normal
	// GF-group renewal and keeps assigning ordinary frames from the residual
	// keyframe group.
	errorResilient bool
	// libvpx vp8/encoder/firstpass.c kf_group / gf_group accounting.
	// kfGroupBits is the bit budget remaining within the current
	// keyframe-bounded group (set by find_next_key_frame, drained as
	// each gf-group within the kf-group is allocated and as the kf's
	// own kf_bits is taken). kfGroupErrorLeft tracks the same in error
	// units. gfGroupBits / gfGroupErrorLeft mirror the per-GF subgroup
	// budget that assign_std_frame_bits drains for each std P frame.
	// framesToKeyRemaining and framesTillGFUpdate count down per
	// finishFrame call so the caller's `keyFrame` flag drives KF-group
	// re-initialization and the GF-group is rebuilt at each boundary.
	// kfGroupValid / gfGroupValid gate whether the err-fraction target
	// path uses the gf_group_bits denominator (libvpx-parity) or the
	// legacy bits_left fallback (which we still use when the group
	// state was not initialized — e.g. the very first call before KF
	// processing has run).
	kfGroupBitsRemaining     int64
	kfGroupErrorLeft         float64
	gfGroupBits              int64
	gfGroupErrorLeft         float64
	framesToKeyRemaining     int
	framesTillGFUpdate       int
	framesSinceGolden        int
	altExtraBits             int
	staticSceneMaxGFInterval int
	maxGFInterval            int
	kfGroupValid             bool
	gfGroupValid             bool
	// gfRefreshTarget is the per-frame target libvpx's
	// define_gf_group sets for the visible GF/refresh frame at the
	// start of the GF section. Keyframe-started ARF groups have no
	// visible GF target in define_gf_group, so this stores the hidden
	// ARF target there as a harmless fallback; altRefTarget remains the
	// authoritative hidden-frame target.
	gfRefreshTarget int
	// altRefTarget mirrors libvpx's `cpi->twopass.gf_bits`, the hidden
	// ARF target consumed by encode_frame_to_data_rate when
	// refresh_alt_ref_frame is set in pass 2. On non-key ARF sections
	// this is distinct from gfRefreshTarget: define_gf_group computes
	// the hidden ARF target in loop slot 0, then a visible GF target in
	// loop slot 1.
	altRefTarget int
	// currentFrameIsGFRefresh marks the in-flight frame as a GF/KF
	// refresh frame so finishFrame can mirror libvpx's
	// update_golden_frame_stats behaviour: KF/GF refresh resets
	// frames_since_golden to 0 (without incrementing), while every
	// other visible frame increments it by 1.
	currentFrameIsGFRefresh bool
	// lastInterQ mirrors libvpx's `cpi->last_q[INTER_FRAME]`. It is
	// the Q used by `define_gf_group` to look up GFQ_ADJUSTMENT
	// (vp8_gf_boost_qadjustment[Q]) when scaling the gfu_boost for
	// the GF allocation chunks. libvpx initializes it to 0 (zeroed
	// by calloc), and updates it after each inter-frame encode at
	// `cpi->last_q[cm->frame_type] = cm->base_qindex`. govpx will
	// thread this once two-pass GF boost regulation needs it.
	lastInterQ int
	// gfIntraErrMin mirrors libvpx's `cpi->twopass.gf_intra_err_min`,
	// the per-frame floor on intra_error used by `calc_frame_boost`
	// when computing the per-frame boost contribution to gfu_boost.
	// libvpx sets it to `GF_MB_INTRA_MIN * cpi->common.MBs` in
	// vp8_init_second_pass. The encoder pushes this value via
	// `setGFIntraErrMin` after computing the MB count for the
	// configured frame size.
	gfIntraErrMin float64
	// frameWidth, frameHeight mirror the encoder's configured frame
	// dimensions. They are used by `kfBitsTarget` to derive the
	// `kf_intra_err_min` floor (KF_MB_INTRA_MIN * MBs) and the
	// size-dependent `kf_boost` adjustment libvpx applies in
	// find_next_key_frame.
	frameWidth  int
	frameHeight int
	// numMBs caches `(width/16) * (height/16)` so estimate_max_q does
	// not have to recompute it per frame. Set by configureFrameDims.
	numMBs int
	// bestQuality / worstQuality mirror libvpx's best_quality and
	// worst_quality q-index bounds, pushed from the encoder's rate control
	// config after the public 0..63 quantizer range has been translated.
	bestQuality  int
	worstQuality int
	// pass2ActiveWorstQ mirrors libvpx's `cpi->active_worst_quality`
	// after vp8_second_pass runs estimate_max_q (frame 0) or the
	// damped update branch (the early-portion-of-clip damped path).
	// govpx's regulator reads this in libvpxActiveWorstQuantizer to
	// substitute it for `maxQuantizer` when in pass-2 VBR mode. The
	// encoder pushes the value into rateControlState.pass2ActiveWorstQ
	// before each frame's selectQuantizerForFrameKind call.
	pass2ActiveWorstQ      int
	pass2ActiveWorstQValid bool
	// baselineGFInterval mirrors libvpx's `cpi->baseline_gf_interval`,
	// the most recent GF/ARF group length set by define_gf_group. It is
	// read by the early-portion-of-clip damped active_worst_quality
	// update branch in libvpx vp8/encoder/firstpass.c vp8_second_pass
	// (lines 2372-2393), where the window gate is
	// `(current_video_frame + baseline_gf_interval) < total_stats.count`.
	// libvpx's twopass struct is calloc'd, so the initial value is 0;
	// the first define_gf_group call after find_next_key_frame
	// overwrites it before the damped update can fire (the gate is also
	// false at frame 0). Updated by defineGFGroup and the
	// error-resilient GF seed.
	baselineGFInterval int
	// estMaxQCorrection mirrors libvpx's
	// `cpi->twopass.est_max_qcorrection_factor`. Initialized to 1.0
	// on the first pass-2 frame (libvpx vp8/encoder/firstpass.c
	// vp8_second_pass line 2329), then updated frame-to-frame from
	// rolling actual/target bits (estimate_max_q rolling-ratio
	// branch). The encoder pushes the rolling stats via
	// `setRollingBits` so this tracks libvpx within rounding.
	estMaxQCorrection float64
	// sectionMaxQFactor mirrors libvpx's
	// `cpi->twopass.section_max_qfactor`. Computed by find_next_key_frame
	// (KF group) and define_gf_group (GF group) from the section's
	// avg intra_error / coded_error. Used by estimate_max_q as a
	// multiplicative factor on the per-Q bit estimate.
	sectionMaxQFactor float64
	// sectionIntraRating mirrors libvpx's
	// `cpi->twopass.section_intra_rating`. The libvpx full-frame loop
	// filter picker (vp8cx_pick_filter_level) reads this to scale the
	// "prefer lower filter level" Bias term: `if (section_intra_rating <
	// 20) Bias = Bias * section_intra_rating / 20;`. The libvpx
	// twopass struct is calloc'd, so in one-pass / realtime / CBR (where
	// neither find_next_key_frame nor define_gf_group runs) it stays at
	// 0 and the unconditional VP8 guard then forces Bias = 0. govpx
	// previously omitted the scaling and used the unscaled bias, which
	// caused the realtime CBR full picker to converge on a different
	// filt_best than libvpx (e.g. on the 128x128 panning fixture
	// frames 2/3, govpx LF=2/1 vs libvpx LF=8/4). Two-pass branches
	// that compute this value must update it via setSectionIntraRating
	// before the next picker call; otherwise it stays 0 (matching
	// libvpx's calloc default).
	sectionIntraRating int
}

func (t *twoPassState) configure(stats []FirstPassFrameStats, bitsPerFrame int, biasPct int, minPct int, maxPct int) {
	*t = twoPassState{worstQuality: vp8MaxQIndex}
	if len(stats) == 0 || bitsPerFrame <= 0 {
		return
	}
	t.stats, t.totalStats = normalizeTwoPassStats(stats)
	if len(t.stats) == 0 {
		return
	}
	// libvpx vp8/encoder/firstpass.c vp8_init_second_pass line 1230
	// seeds total_left_stats to a copy of total_stats. Each pass-2 frame
	// then drains the current frame's stats out of total_left_stats via
	// subtract_stats (firstpass.c line 2398) so the section averages
	// reflect the still-unencoded tail.
	t.totalLeftStats = t.totalStats
	t.vbrBiasPct = biasPct
	if t.vbrBiasPct <= 0 {
		t.vbrBiasPct = 50
	}
	// libvpx vp8_cx_iface.c default config: rc_2pass_vbr_minsection_pct=0,
	// rc_2pass_vbr_maxsection_pct=400. Govpx zero-value EncoderOptions
	// historically substituted 50/200; that path inflated the per-frame
	// floor (sectionMin) and re-credited bits_left by the wrong amount
	// in finishFrame, so per-frame pass-2 targets ballooned over the
	// course of a short stream. Mirror libvpx's defaults so callers that
	// leave the knobs at zero match libvpx's bookkeeping.
	t.minPct = max(minPct, 0)
	t.minFrameBandwidth = vbrMinFrameBandwidthBits(bitsPerFrame, t.minPct)
	// libvpx vp8_init_second_pass seeds twopass.bits_left from the total
	// target bits minus the whole two_pass_vbrmin_section reserve. Pass2Encode
	// then credits one min_frame_bandwidth back after each visible frame.
	totalBits := int64(bitsPerFrame) * int64(len(t.stats))
	t.bitsLeft = max(totalBits-int64(t.minFrameBandwidth)*int64(len(t.stats)), 0)
	t.maxPct = maxPct
	if t.maxPct <= 0 {
		t.maxPct = 400
	}
	for i := range t.stats {
		t.errorLeft += t.modifiedError(t.stats[i])
	}
	// libvpx vp8/encoder/firstpass.c vp8_second_pass line 2329 seeds
	// est_max_qcorrection_factor=1.0 on the first frame; section_max_qfactor
	// also starts at 1.0 (libvpx's struct is calloced; the first
	// find_next_key_frame call overwrites it before estimate_max_q
	// reads it). Mirror those initial values here so the very first
	// estimate_max_q call sees libvpx-shaped state when the encoder
	// has not yet emitted any frames.
	t.estMaxQCorrection = 1.0
	t.sectionMaxQFactor = 1.0
}

func (t *twoPassState) enabled() bool {
	return len(t.stats) > 0
}

func (t *twoPassState) configureQuantizerBounds(bestQuality int, worstQuality int) {
	t.bestQuality = clampQuantizerValue(bestQuality, 0, vp8MaxQIndex)
	t.worstQuality = max(clampQuantizerValue(worstQuality, 0, vp8MaxQIndex), t.bestQuality)
}

func (t *twoPassState) configureErrorResilient(errorResilient bool) {
	t.errorResilient = errorResilient
}

// configureFrameDims pushes the encoder's configured frame size into
// the two-pass state. Used by `kfBitsTarget` for the size-dependent
// kf_boost adjustment and by `defineGFGroup` to derive
// `gf_intra_err_min` (libvpx GF_MB_INTRA_MIN * MBs).
func (t *twoPassState) configureFrameDims(width int, height int) {
	if min(width, height) > 0 {
		t.frameWidth = width
		t.frameHeight = height
		const gfMBIntraMin = 200 // libvpx GF_MB_INTRA_MIN
		mbCols := (width + 15) / 16
		mbRows := (height + 15) / 16
		t.numMBs = mbCols * mbRows
		t.gfIntraErrMin = float64(gfMBIntraMin * t.numMBs)
	}
}

func (t *twoPassState) configureGFIntervals(staticSceneMax int, maxGF int) {
	t.staticSceneMaxGFInterval = staticSceneMax
	t.maxGFInterval = maxGF
}

func (t *twoPassState) statsForFrame(frame uint64) FirstPassFrameStats {
	if !t.enabled() || frame >= uint64(len(t.stats)) {
		return FirstPassFrameStats{}
	}
	return t.stats[frame]
}

func (t *twoPassState) shouldKeyFrame(frame uint64, framesSinceKeyFrame int, keyFrameInterval int) bool {
	if !t.enabled() || frame == 0 || frame+1 >= uint64(len(t.stats)) {
		return false
	}
	if framesSinceKeyFrame < libvpxMinGFInterval {
		return false
	}
	if keyFrameInterval > 0 && framesSinceKeyFrame >= keyFrameInterval {
		return true
	}
	return libvpxTestCandidateKeyFrame(t.stats, int(frame))
}

// frameTargetBits returns the libvpx Pass2Encode per-frame target for the
// given frame. It mirrors the libvpx vp8/encoder/firstpass.c flow:
//   - At a KF (frame_type == KEY_FRAME) it runs the find_next_key_frame
//     KF-group allocator: kf_group_bits = bits_left * (kf_group_err /
//     modified_error_left); kf_bits is then derived as the maximum of the
//     boost-based formula and the err-fraction `bits_left * (kf_mod_err /
//     modified_error_left)`. For the test workloads we compare against,
//     the KF dominates the modified-error denominator and the
//     err-fraction branch wins, so govpx implements that branch here.
//     After the KF, kf_group_bits and kf_group_error_left are seeded for
//     the remaining frames in the group.
//   - At a non-KF frame at a GF boundary (framesTillGFUpdate==0), it
//     runs define_gf_group: gf_group_bits = kf_group_bits *
//     (gf_group_err / kf_group_error_left), then drains the GF-frame
//     allocation chunk. The GF interval spans the rest of the KF group
//     (libvpx caps it at static_scene_max_gf_interval, but for short
//     clips with no ARF the cap is the kf-group remainder).
//   - For std P frames it runs assign_std_frame_bits: target =
//     gf_group_bits * (mod_err / gf_group_error_left), clamped to
//     `max_bits` (frame_max_bits VBR), drained from gf_group_bits, plus
//     min_frame_bandwidth and (on alternating frames_since_golden)
//     alt_extra_bits.
//
// defaultTargetBits is the legacy one-pass per-frame target the rate
// controller would have produced; it is used as the fallback when the
// twopass state has not been seeded (e.g. the first frame before pass-1
// stats are available) and as the input to the section-min computation.
func (t *twoPassState) frameTargetBits(frame uint64, keyFrame bool, defaultTargetBits int) int {
	return t.frameTargetBitsWithAltRef(frame, keyFrame, defaultTargetBits, 0, false)
}

func (t *twoPassState) altRefFrameTargetBits(defaultTargetBits int) int {
	if !t.enabled() || t.altRefTarget <= 0 {
		return 0
	}
	return t.altRefTarget
}

func (t *twoPassState) frameTargetBitsWithAltRef(frame uint64, keyFrame bool, defaultTargetBits int, altRefInterval int, useAltRef bool) int {
	if !t.enabled() || frame >= uint64(len(t.stats)) || defaultTargetBits <= 0 {
		return 0
	}
	modErr := t.modifiedError(t.stats[frame])
	if min(modErr, t.errorLeft) <= 0 || t.bitsLeft <= 0 {
		return defaultTargetBits
	}
	var target int64
	_, sectionMax := t.pass2VBRSectionLimits(frame, defaultTargetBits)
	gfBoundary := false
	t.currentFrameIsGFRefresh = false
	// libvpx vp8_second_pass at firstpass.c line 2237 runs
	// find_next_key_frame ONLY when `cpi->twopass.frames_to_key == 0`
	// — i.e., the natural KF boundary the two-pass tracker computed.
	// User-forced mid-stream KFs (set via VPX_EFLAG_FORCE_KF in the
	// codec layer; cf. vp8_cx_iface.c lines 938-944) take the
	// KEY_FRAME branch in onyx_if.c:set_frame_type at line 3407 but
	// do NOT re-run find_next_key_frame. Their bit target is therefore
	// computed by the ordinary `assign_std_frame_bits` path (or
	// define_gf_group at a GF boundary), not by a fresh
	// find_next_key_frame re-seed.
	//
	// govpx previously ran prepareKFGroup on every `keyFrame=true`
	// call, which re-seeded the kf-group state on every forced
	// mid-stream KF. The fix below gates the re-seed on the libvpx
	// `frames_to_key == 0` predicate so forced KFs reuse the existing
	// kf-group accounting just like libvpx.
	naturalKF := keyFrame && (frame == 0 || t.framesToKeyRemaining == 0)
	if naturalKF {
		// libvpx vp8_second_pass at KF: find_next_key_frame runs first
		// (sets kf_group_bits / kf_bits / drains kf_group_bits by
		// kf_bits), THEN define_gf_group runs (which can re-seed
		// kf_group_bits to bits_left for the last KF group). We mirror
		// that ordering so the KF target is the err-fraction value
		// computed against the full bits_left budget, while the GF
		// allocator sees the post-find_next_key_frame residual budget
		// for the inter frames.
		t.prepareKFGroup(frame)
		t.currentFrameIsGFRefresh = true
		target = t.kfBitsTarget(frame, modErr)
		if framesLeft := int64(len(t.stats)) - int64(frame); framesLeft > 1 {
			expanded := sectionMax * framesLeft
			if expanded > sectionMax {
				sectionMax = expanded
			}
		}
		// define_gf_group seeds the GF section for the inter frames
		// that follow. Per_frame_bandwidth for the KF stays at kf_bits
		// (libvpx does not overwrite it because the inner GF loop's
		// per_frame_bandwidth assignment is gated on frame_type !=
		// KEY_FRAME). Error-resilient pass 2 is a libvpx special case:
		// it skips define_gf_group and uses the residual KF group as
		// the ordinary-frame assignment pool.
		if t.errorResilient {
			t.seedErrorResilientGFGroup()
		} else {
			t.defineGFGroup(frame, altRefInterval, useAltRef)
		}
		// libvpx vp8/encoder/firstpass.c vp8_second_pass lines 2328-2363:
		// on the very first frame of pass 2, estimate_max_q computes a
		// `tmp_q` and assigns it to cpi->active_worst_quality. This caps
		// the regulator's worst-Q ceiling at a value derived from the
		// per-MB error and the section target bandwidth, instead of
		// leaving it at oxcf.worst_allowed_q (e.g., 56). Without this
		// the govpx regulator picks Q values much lower than libvpx
		// for the same per-frame target — visible as q_match=8% on
		// desktopqvga while target_match=100%. We seed the active
		// worst Q here so subsequent frames in this pass-2 see the
		// same regulator ceiling libvpx uses.
		if frame == 0 {
			t.seedPass2ActiveWorstQ(defaultTargetBits)
		}
	} else if t.errorResilient {
		// libvpx firstpass.c vp8_second_pass special-cases any
		// non-zero error_resilient_mode: ordinary frames skip
		// define_gf_group and force frames_till_gf_update_due back
		// to twopass.frames_to_key before assign_std_frame_bits.
		t.framesTillGFUpdate = max(t.framesToKeyRemaining, 1)
	} else if t.framesTillGFUpdate == 0 {
		t.defineGFGroup(frame, altRefInterval, useAltRef)
		gfBoundary = true
		t.currentFrameIsGFRefresh = true
	}
	// Forced mid-stream KFs (keyFrame=true && !naturalKF) follow the
	// non-KF target path in libvpx: their target is computed by
	// assign_std_frame_bits (or the GF allocator at a GF boundary), not
	// by find_next_key_frame. Routing them through the !naturalKF
	// branch mirrors libvpx's vp8_second_pass control flow at
	// firstpass.c lines 2283-2303 (the `else` block when
	// frames_till_gf_update_due > 0).
	if !naturalKF {
		if gfBoundary && t.gfGroupValid {
			// libvpx vp8_second_pass: at a non-key ARF boundary,
			// define_gf_group computes both the hidden ARF target and
			// the visible GF target. It then calls assign_std_frame_bits
			// only to drain the residual GF budget/error, restoring the
			// boosted visible GF target afterward.
			if useAltRef {
				_ = t.assignStdFrameBits(modErr, sectionMax)
			}
			target = int64(t.gfRefreshTarget)
		} else if t.gfGroupValid {
			target = t.assignStdFrameBits(modErr, sectionMax)
		} else {
			// Fallback: legacy err-fraction-of-bits_left. Used when the
			// gf-group state has not been seeded (the keyframe was
			// emitted outside the two-pass driver, or stats were
			// swapped mid-stream).
			target = int64(float64(t.bitsLeft) * modErr / t.errorLeft)
			target += int64(t.minFrameBandwidth)
		}
	}
	if target > sectionMax {
		target = sectionMax
	}
	if target < 1 {
		target = 1
	}
	// libvpx vp8/encoder/firstpass.c vp8_second_pass lines 2367-2393
	// runs the early-portion-of-clip damped active_worst_quality update
	// AFTER find_next_key_frame / define_gf_group / assign_std_frame_bits
	// but BEFORE the trailing subtract_stats (line 2398). In govpx the
	// equivalent point is here, just before we return the per-frame
	// target — the GF/KF allocator state and baseline_gf_interval are
	// already up to date, and finishFrame (which drains
	// total_left_stats) has not yet run for this frame.
	t.dampedUpdatePass2ActiveWorstQ(frame)
	if target > int64(maxInt()) {
		return maxInt()
	}
	return int(target)
}

// prepareKFGroup mirrors the libvpx vp8/encoder/firstpass.c
// find_next_key_frame KF-group seeding, but only the bookkeeping that
// influences subsequent per-frame target allocation:
//
//	kf_group_err = sum(modified_err[frame .. frame+frames_to_key-1])
//	kf_group_bits = bits_left * (kf_group_err / modified_error_left)
//	kf_group_bits = clamp(kf_group_bits, 0, max_bits * frames_to_key)
//	kf_group_error_left = kf_group_err - kf_mod_err
//	modified_error_left -= kf_group_err  (handled in finishFrame via errorLeft)
//
// The actual KF target (kf_bits) is computed by kfBitsTarget at frame
// emit time from this seeded state. After this routine returns, the
// gf-group state is also seeded so the very next frame at a GF
// boundary picks up gf_group_bits = kf_group_bits.
func (t *twoPassState) prepareKFGroup(frame uint64) {
	framesToKey := len(t.stats) - int(frame)
	if framesToKey <= 0 {
		t.kfGroupValid = false
		t.gfGroupValid = false
		return
	}
	var kfGroupErr, kfModErr float64
	var sectionIntra, sectionCoded float64
	end := min(frame+uint64(framesToKey), uint64(len(t.stats)))
	for i := frame; i < end; i++ {
		kfGroupErr += t.modifiedError(t.stats[i])
		// Accumulate raw intra/coded error totals so we can compute
		// libvpx's section_max_qfactor (find_next_key_frame line 2778)
		// at the same time we seed the kf-group bit budget.
		sectionIntra += t.stats[i].IntraError
		sectionCoded += t.stats[i].CodedError
	}
	// section_max_qfactor uses avg per-frame intra/coded ratio. libvpx
	// runs avg_stats first (divides each accumulator by count) so the
	// ratio is identical with or without the divide; we can compute it
	// directly from the totals via libvpxSectionMaxQFactor which
	// handles the DOUBLE_DIVIDE_CHECK fallback.
	if framesToKey > 0 {
		t.sectionMaxQFactor = libvpxSectionMaxQFactor(sectionIntra, sectionCoded)
		// Mirror libvpx find_next_key_frame line 2772: alongside the
		// section_max_qfactor (which estimate_max_q reads), the same
		// avg intra/coded ratio drives section_intra_rating, which the
		// loop-filter full picker reads to scale its lower-level Bias.
		t.sectionIntraRating = libvpxSectionIntraRating(sectionIntra, sectionCoded)
	}
	kfModErr = t.modifiedError(t.stats[frame])
	t.framesToKeyRemaining = framesToKey
	t.framesSinceGolden = 0
	t.altExtraBits = 0
	if t.errorLeft <= 0 || t.bitsLeft <= 0 {
		t.kfGroupBitsRemaining = 0
		t.kfGroupErrorLeft = 0
		t.kfGroupValid = false
		t.gfGroupValid = false
		return
	}
	kfGroupBits := int64(float64(t.bitsLeft) * (kfGroupErr / t.errorLeft))
	maxBits := int64(libvpxFrameMaxBitsVBR(t.bitsLeft, int64(framesToKey), t.maxPctOrDefault()))
	if maxBits > 0 {
		if cap := maxBits * int64(framesToKey); kfGroupBits > cap {
			kfGroupBits = cap
		}
	}
	if kfGroupBits < 0 {
		kfGroupBits = 0
	}
	// kf_bits is taken out of kf_group_bits below in kfBitsTarget; but
	// for now we record the seeded values so the GF group can use them.
	t.kfGroupBitsRemaining = kfGroupBits
	t.kfGroupErrorLeft = kfGroupErr - kfModErr
	if t.kfGroupErrorLeft < 0 {
		t.kfGroupErrorLeft = 0
	}
	t.kfGroupValid = true
	// After the KF is consumed and the GF span is the rest of the
	// kf-group, define_gf_group will fire on the very next frame. Mark
	// the GF state invalid so that frame triggers re-seeding.
	t.gfGroupBits = 0
	t.gfGroupErrorLeft = 0
	t.gfGroupValid = false
	t.framesTillGFUpdate = 0
}

// seedErrorResilientGFGroup mirrors the error_resilient_mode branch in
// libvpx vp8/encoder/firstpass.c vp8_second_pass immediately after
// find_next_key_frame: use the residual KF group as the GF group, set the
// interval to frames_to_key, and do not arm alt-ref.
func (t *twoPassState) seedErrorResilientGFGroup() {
	if !t.kfGroupValid {
		t.gfGroupValid = false
		return
	}
	t.gfGroupBits = t.kfGroupBitsRemaining
	t.gfGroupErrorLeft = t.kfGroupErrorLeft
	t.gfGroupValid = true
	t.gfRefreshTarget = 0
	t.altRefTarget = 0
	t.framesTillGFUpdate = max(t.framesToKeyRemaining, 1)
	// libvpx vp8/encoder/firstpass.c vp8_second_pass line 2252
	// (error_resilient_mode branch) sets `cpi->baseline_gf_interval =
	// cpi->twopass.frames_to_key`. Mirror that so the damped
	// active_worst_quality window gate sees the libvpx
	// baseline_gf_interval in error-resilient pass-2 mode.
	t.baselineGFInterval = t.framesTillGFUpdate
}

// kfBitsTarget computes libvpx's kf_bits — the per-frame target for
// the KF — from the already-seeded kf-group state. It mirrors the
// libvpx vp8/encoder/firstpass.c find_next_key_frame KF allocation
// (lines 2814-2925):
//
//	kf_boost = boost_score from the IIKFACTOR2 prediction-decay walk
//	  over the next frames_to_key-1 frames, scaled by 100/16, with
//	  size-dependent adjustments and a 250 floor.
//	allocation_chunks = ((frames_to_key-1) * 100) + kf_boost
//	  (or *10 when decay_accumulator >= 0.99 — the "almost static"
//	  branch).
//	kf_bits = kf_boost * (kf_group_bits / allocation_chunks)
//	if kf_mod_err >= avg: alt_kf_bits = bits_left * kf_mod_err /
//	  modified_error_left; kf_bits = max(kf_bits, alt_kf_bits).
//	if kf_mod_err < avg:  alt_kf_bits computed from kf_boost via
//	  alt_kf_grp_bits; kf_bits = min(kf_bits, alt_kf_bits).
//	kf_bits += min_frame_bandwidth
//
// Govpx ports both the boost-based path and the alt-branch logic so
// the per-frame KF target tracks libvpx within rounding. The
// kf_group_bits state is then drained by kf_bits so the gf-group
// budget is the residual.
func (t *twoPassState) kfBitsTarget(frame uint64, kfModErr float64) int64 {
	if !t.kfGroupValid || t.errorLeft <= 0 {
		return int64(float64(t.bitsLeft) * kfModErr / t.errorLeft)
	}
	framesToKey := max(t.framesToKeyRemaining, 1)
	// Compute kf_boost via the libvpx prediction-quality walk over
	// frames [frame+1 .. frame+framesToKey-1]. Mirrors lines
	// 2722-2756 of find_next_key_frame.
	kfBoost, decayAccumulator := computeKFBoost(t.stats, frame, framesToKey, t.kfIntraErrMinForFrame())
	// Size-dependent kf_boost adjustment (lines 2837-2844). lst_yv12
	// is the "last" YUV buffer's size, which equals encoder
	// dimensions. govpx exposes the dimensions via t.frameWidth /
	// t.frameHeight (set by the encoder at configure time).
	if min(t.frameWidth, t.frameHeight) > 0 {
		size := t.frameWidth * t.frameHeight
		if size > 320*240 {
			kfBoost += 2 * size / (320 * 240)
		} else if size < 320*240 {
			kfBoost -= 4 * (320 * 240) / size
		}
	}
	// Min KF boost.
	kfBoost = max((kfBoost*100)>>4, 250)
	// allocation_chunks. The "almost static" branch uses *10
	// instead of *100.
	var allocationChunks int64
	if decayAccumulator >= 0.99 {
		allocationChunks = int64(framesToKey-1)*10 + int64(kfBoost)
	} else {
		allocationChunks = int64(framesToKey-1)*100 + int64(kfBoost)
	}
	for kfBoost > 1000 {
		kfBoost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			break
		}
	}
	if allocationChunks <= 0 {
		allocationChunks = 1
	}
	kfBits := int64(float64(kfBoost) * (float64(t.kfGroupBitsRemaining) / float64(allocationChunks)))
	// alt branch: compare kf_mod_err to group avg.
	groupAvg := 0.0
	if framesToKey > 0 {
		// kfGroupErrorLeft + kfModErr is the original kf_group_err
		// (before find_next_key_frame stored kfGroupErrorLeft =
		// kfGroupErr - kfModErr). Restore for the avg.
		groupAvg = (t.kfGroupErrorLeft + kfModErr) / float64(framesToKey)
	}
	if kfModErr < groupAvg {
		// Use min(kfBits, alt_kf_bits computed via alt_kf_grp_bits).
		// alt_kf_grp_bits = bits_left * (kfModErr * framesToKey) /
		//   modified_error_left; alt_kf_bits = kf_boost *
		//   alt_kf_grp_bits / allocation_chunks.
		altGrp := float64(t.bitsLeft) * (kfModErr * float64(framesToKey)) / t.errorLeft
		altKFBits := int64(float64(kfBoost) * (altGrp / float64(allocationChunks)))
		if kfBits > altKFBits {
			kfBits = altKFBits
		}
	} else {
		// Use max(kfBits, bits_left * kfModErr / modified_error_left).
		altKFBits := int64(float64(t.bitsLeft) * kfModErr / t.errorLeft)
		if altKFBits > kfBits {
			kfBits = altKFBits
		}
	}
	if kfBits > t.kfGroupBitsRemaining {
		kfBits = t.kfGroupBitsRemaining
	}
	if kfBits < 0 {
		kfBits = 0
	}
	// Drain kf_group_bits by kf_bits (libvpx: kf_group_bits -= kf_bits).
	t.kfGroupBitsRemaining -= kfBits
	if t.kfGroupBitsRemaining < 0 {
		t.kfGroupBitsRemaining = 0
	}
	// Add min_frame_bandwidth (libvpx: kf_bits += min_frame_bandwidth).
	kfBits += int64(t.minFrameBandwidth)
	return kfBits
}

// kfIntraErrMinForFrame returns libvpx's `cpi->twopass.kf_intra_err_min`
// equivalent for the configured encoder frame size. libvpx sets it to
// `KF_MB_INTRA_MIN * MBs` in vp8_init_second_pass; govpx derives MBs
// from the configured frame dimensions when available.
func (t *twoPassState) kfIntraErrMinForFrame() float64 {
	const kfMBIntraMin = 300 // libvpx KF_MB_INTRA_MIN
	// min(a, b) <= 0 collapses (a <= 0 || b <= 0) into one compare.
	if min(t.frameWidth, t.frameHeight) <= 0 {
		return 0
	}
	mbCols := (t.frameWidth + 15) / 16
	mbRows := (t.frameHeight + 15) / 16
	return float64(kfMBIntraMin * mbCols * mbRows)
}

// seedPass2ActiveWorstQ ports the libvpx vp8/encoder/firstpass.c
// vp8_second_pass first-frame branch (lines 2328-2363):
//
//	frames_left = total_stats.count - current_video_frame
//	section_target_bandwidth = bits_left / frames_left
//	section_err = total_left_stats.coded_error / total_left_stats.count
//	err_per_mb = section_err / num_mbs
//	tmp_q = estimate_max_q(...)
//	cpi->active_worst_quality = tmp_q
//
// When seeded, govpx's regulator reads the result via
// `pass2ActiveWorstQOverride` and substitutes it for `maxQuantizer` in
// `libvpxActiveWorstQuantizer`. This mirrors libvpx's behavior where
// the regulator's worst-Q ceiling is dialed down from the user-specified
// `worst_allowed_q` to a value derived from the per-MB error and
// section target bandwidth, which is the single biggest contributor to
// q_match parity on real-content pass-2 fixtures.
//
// `defaultTargetBits` is the encoder's per-frame target (typically
// `target_bitrate / fps`); we use `t.bitsLeft / framesLeft` instead so
// the value reflects the post-vbrmin_section budget when minPct > 0.
// The frame parameter is kept in the call site for clarity even though
// the computation only references frame 0 state.
func (t *twoPassState) seedPass2ActiveWorstQ(defaultTargetBits int) {
	if t.numMBs <= 0 {
		// Without configured frame dimensions we cannot compute
		// err_per_mb. Leave activeWorstQ unset; the regulator falls
		// back to oxcf.worst_allowed_q.
		return
	}
	framesLeft := max(int64(len(t.stats))-int64(t.frameIndex), 1)
	var sectionTargetBandwidth int64
	if t.bitsLeft > 0 {
		sectionTargetBandwidth = t.bitsLeft / framesLeft
	} else {
		sectionTargetBandwidth = int64(defaultTargetBits)
	}
	if sectionTargetBandwidth <= 0 {
		return
	}
	// libvpx vp8/encoder/firstpass.c vp8_second_pass passes
	// `&cpi->twopass.total_left_stats` to estimate_modemvcost
	// (line 2325) and to estimate_max_q (line 2349). govpx mirrors this
	// by reading t.totalLeftStats, which configure seeds equal to
	// totalStats and finishFrame drains per frame. On frame 0 the
	// rolled-down value still equals totalStats (no frame has been
	// subtracted).
	count := t.totalLeftStats.Count
	if count <= 0 {
		// Fall back to summing over the per-frame stats.
		count = float64(len(t.stats))
	}
	codedError := t.totalLeftStats.CodedError
	if codedError <= 0 {
		// Sum the per-frame coded_error if the rolled total is
		// missing. This guards against malformed pass-1 dumps.
		for i := range t.stats {
			codedError += t.stats[i].CodedError
		}
	}
	if min(codedError, count) <= 0 {
		return
	}
	sectionErr := codedError / count
	errPerMB := sectionErr / float64(t.numMBs)
	estCorrection := t.estMaxQCorrection
	if estCorrection <= 0 {
		estCorrection = 1.0
	}
	sectionMQF := t.sectionMaxQFactor
	if sectionMQF <= 0 {
		sectionMQF = 1.0
	}
	// libvpx feeds estimate_max_q with an overhead estimate for coding
	// modes/MVs, then searches within (best_quality, worst_quality).
	// The overhead term is normalized in bits*512 and decays with Q
	// inside libvpxEstimateMaxQ, matching firstpass.c
	// estimate_modemvcost / estimate_max_q.
	// libvpx vp8/encoder/firstpass.c vp8_second_pass line 2325 calls
	// estimate_modemvcost with &cpi->twopass.total_left_stats. Mirror
	// that pointer choice so overhead tracks the rolled-down totals.
	overheadBits := libvpxEstimateModeMVCost(t.totalLeftStats, t.numMBs)
	tmpQ := min(max(libvpxEstimateMaxQ(t.numMBs, int(sectionTargetBandwidth), overheadBits, errPerMB, 1.0, estCorrection, sectionMQF, t.bestQuality, t.worstQuality), 0), vp8MaxQIndex)
	t.pass2ActiveWorstQ = tmpQ
	t.pass2ActiveWorstQValid = true
}

// pass2ActiveWorstQOverride returns the libvpx-derived
// `active_worst_quality` value when the pass-2 driver has seeded it
// via seedPass2ActiveWorstQ. The boolean second return value is false
// when the override is not available (one-pass mode, or pass 2 before
// frame 0 has been processed). Read by ratecontrol.go's
// `libvpxActiveWorstQuantizer` to substitute for `maxQuantizer` in the
// VBR-pass2 path.
func (t *twoPassState) pass2ActiveWorstQOverride() (int, bool) {
	if !t.pass2ActiveWorstQValid {
		return 0, false
	}
	return t.pass2ActiveWorstQ, true
}

// dampedUpdatePass2ActiveWorstQ ports the libvpx vp8/encoder/firstpass.c
// vp8_second_pass early-portion-of-clip damped active_worst_quality
// update branch (firstpass.c lines 2367-2393):
//
//	/* The last few frames of a clip almost always have to few or too many
//	 * bits and for the sake of over exact rate control we don't want to make
//	 * radical adjustments to the allowed quantizer range just to use up a
//	 * few surplus bits or get beneath the target rate. */
//	else if ((cpi->common.current_video_frame <
//	          (((unsigned int)cpi->twopass.total_stats.count * 255) >> 8)) &&
//	         ((cpi->common.current_video_frame + cpi->baseline_gf_interval) <
//	          (unsigned int)cpi->twopass.total_stats.count)) {
//	  if (frames_left < 1) frames_left = 1;
//	  int64_t section_target_bandwidth = cpi->twopass.bits_left / frames_left;
//	  section_target_bandwidth = VPXMIN(section_target_bandwidth, INT_MAX);
//	  tmp_q = estimate_max_q(cpi, &cpi->twopass.total_left_stats,
//	                         (int)section_target_bandwidth, overhead_bits);
//	  /* Move active_worst_quality but in a damped way */
//	  if (tmp_q > cpi->active_worst_quality) cpi->active_worst_quality++;
//	  else if (tmp_q < cpi->active_worst_quality) cpi->active_worst_quality--;
//	  cpi->active_worst_quality =
//	      ((cpi->active_worst_quality * 3) + tmp_q + 2) / 4;
//	}
//
// This branch is reached only when current_video_frame > 0 (it is the
// `else if` from the `==0` seed branch at lines 2328-2365). It tracks
// the regulator's worst-Q ceiling toward the rolling estimate over the
// still-unencoded section so the regulator does not crash Q near the
// end of a clip where a small overspend or underspend would otherwise
// drive radical adjustments.
//
// Inputs mirror libvpx exactly:
//   - cpi->common.current_video_frame == t.frameIndex (== frame on entry,
//     since finishFrame for the previous frame has already advanced
//     frameIndex to the current frame's index).
//   - cpi->twopass.total_stats.count: rolled-up frame count from the
//     pass-1 totals packet. Falls back to len(t.stats) when the totals
//     packet was missing.
//   - cpi->baseline_gf_interval == t.baselineGFInterval (set by
//     defineGFGroup or seedErrorResilientGFGroup before this method is
//     invoked).
//   - cpi->twopass.bits_left == t.bitsLeft (live live).
//   - estimate_max_q's section pointer is &cpi->twopass.total_left_stats
//     (firstpass.c line 2381). govpx rolls down totalLeftStats per
//     frame in finishFrame via subtractFirstPassStats, so this matches.
//
// Returns silently on any precondition failure (the regulator keeps the
// previous active_worst_quality).
func (t *twoPassState) dampedUpdatePass2ActiveWorstQ(frame uint64) {
	if !t.pass2ActiveWorstQValid || t.numMBs <= 0 {
		return
	}
	// libvpx vp8_second_pass: the `==0` branch handles the seed; the
	// damped branch is the `else if` after it. Skip on frame 0 so the
	// seed is preserved.
	if frame == 0 {
		return
	}
	// total_stats.count: prefer the configurator-seeded totalStats.Count
	// (matches libvpx's accumulate_stats roll-up). Fall back to the
	// per-frame stats slice length when the totals packet is absent or
	// zero.
	totalCount := int(t.totalStats.Count)
	if totalCount <= 0 {
		totalCount = len(t.stats)
	}
	if totalCount <= 0 {
		return
	}
	// libvpx firstpass.c lines 2372-2375 window gate:
	//
	//   ((cpi->common.current_video_frame <
	//     (((unsigned int)cpi->twopass.total_stats.count * 255) >> 8)) &&
	//    ((cpi->common.current_video_frame + cpi->baseline_gf_interval) <
	//     (unsigned int)cpi->twopass.total_stats.count))
	//
	// The first half gates the early 255/256 portion of the clip; the
	// second half makes sure the current frame plus the upcoming GF
	// span still leaves frames after it (i.e., we are not in the
	// trailing GF group).
	upperGate := (uint64(totalCount) * 255) >> 8
	if frame >= upperGate {
		return
	}
	if frame+uint64(t.baselineGFInterval) >= uint64(totalCount) {
		return
	}
	// libvpx firstpass.c lines 2376-2382: section_target_bandwidth =
	// bits_left / frames_left (with the frames_left>=1 floor), then
	// estimate_max_q on total_left_stats. libvpx VPXMIN's the section
	// bandwidth at INT_MAX but does NOT guard against <=0 — passes it
	// directly to estimate_max_q, which then returns maxq_max_limit
	// (libvpx firstpass.c line 1326: `if (target_norm_bits_per_mb <=
	// 0) return MAXQ;`). Mirror that flow: do not short-circuit on
	// stb<=0 here; let libvpxEstimateMaxQ produce maxqMaxLimit.
	framesLeft := max(int64(totalCount)-int64(frame), 1)
	if t.bitsLeft < 0 {
		return
	}
	sectionTargetBandwidth := t.bitsLeft / framesLeft
	// libvpx feeds estimate_max_q with &cpi->twopass.total_left_stats:
	// rolled-down section totals reflecting the still-unencoded tail.
	count := t.totalLeftStats.Count
	if count <= 0 {
		count = float64(int64(len(t.stats)) - int64(frame))
	}
	codedError := t.totalLeftStats.CodedError
	if codedError <= 0 {
		end := uint64(len(t.stats))
		if frame < end {
			for i := frame; i < end; i++ {
				codedError += t.stats[i].CodedError
			}
		}
	}
	if min(codedError, count) <= 0 {
		return
	}
	sectionErr := codedError / count
	errPerMB := sectionErr / float64(t.numMBs)
	estCorrection := t.estMaxQCorrection
	if estCorrection <= 0 {
		estCorrection = 1.0
	}
	sectionMQF := t.sectionMaxQFactor
	if sectionMQF <= 0 {
		sectionMQF = 1.0
	}
	overheadBits := libvpxEstimateModeMVCost(t.totalLeftStats, t.numMBs)
	tmpQ := max(libvpxEstimateMaxQ(t.numMBs, int(sectionTargetBandwidth), overheadBits, errPerMB, 1.0, estCorrection, sectionMQF, t.bestQuality, t.worstQuality), 0)
	if tmpQ > vp8MaxQIndex {
		tmpQ = vp8MaxQIndex
	}
	// libvpx firstpass.c lines 2384-2392:
	//   /* Move active_worst_quality but in a damped way */
	//   if (tmp_q > cpi->active_worst_quality) cpi->active_worst_quality++;
	//   else if (tmp_q < cpi->active_worst_quality) cpi->active_worst_quality--;
	//   cpi->active_worst_quality =
	//       ((cpi->active_worst_quality * 3) + tmp_q + 2) / 4;
	aw := t.pass2ActiveWorstQ
	if tmpQ > aw {
		aw++
	} else if tmpQ < aw {
		aw--
	}
	aw = max((aw*3+tmpQ+2)/4, 0)
	if aw > vp8MaxQIndex {
		aw = vp8MaxQIndex
	}
	t.pass2ActiveWorstQ = aw
}

// computeKFBoost mirrors the libvpx vp8/encoder/firstpass.c
// find_next_key_frame inner walk (lines 2728-2756) that produces the
// raw `boost_score` used to seed `kf_boost` for the KF allocation.
//
//	r = IIKFACTOR2 * intra_error / coded_error  (with the
//	  kf_intra_err_min floor on intra), capped at RMAX=14.0.
//	decay_accumulator *= libvpxGetPredictionDecayRate(next_frame),
//	  clamped to [0.1, 1.0].
//	boost_score += decay_accumulator * r.
//	break when i>MIN_GF_INTERVAL && (boost_score-old_boost_score)<1.0.
//
// Returns the raw `boost_score` and the final `decay_accumulator`
// (both used by `kfBitsTarget` to compute the KF chunk allocation).
