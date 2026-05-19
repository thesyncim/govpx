package govpx

import "math"

// VP9 adaptive GF/ARF group analyzer ported verbatim from libvpx v1.16.0.
//
// libvpx parity references (every constant/branch has a citation):
//   - vp9/encoder/vp9_firstpass.h:30-112 GF_GROUP struct -> vp9GFGroup
//   - vp9/encoder/vp9_firstpass.c:2701 get_active_gf_inverval_range ->
//     vp9GetActiveGFIntervalRange
//   - vp9/encoder/vp9_firstpass.c:2587 get_gop_coding_frame_num ->
//     vp9GetGOPCodingFrameNum
//   - vp9/encoder/vp9_firstpass.c:2057 calculate_total_gf_group_bits ->
//     vp9CalculateTotalGFGroupBits
//   - vp9/encoder/vp9_firstpass.c:2102 calculate_boost_bits ->
//     vp9CalculateBoostBits
//   - vp9/encoder/vp9_firstpass.c:2146 find_arf_order ->
//     vp9FindARFOrder
//   - vp9/encoder/vp9_firstpass.c:2218 define_gf_group_structure ->
//     vp9DefineGFGroupStructure
//   - vp9/encoder/vp9_firstpass.c:2391 allocate_gf_group_bits ->
//     vp9AllocateGFGroupBits
//   - vp9/encoder/vp9_firstpass.c:2541 adjust_group_arnr_filter ->
//     vp9AdjustGroupARNRFilter
//   - vp9/encoder/vp9_firstpass.c:2761 define_gf_group -> vp9DefineGFGroup
//
// Deferred (require state govpx does not yet carry; cited so they can be
// promoted later):
//
//   - Multi-ARF recursion past depth=1 (libvpx vp9_firstpass.c:2191 +
//     vp9_firstpass.c:2200 the find_arf_order self-recursive case). govpx
//     emits the base ARF + leaf P-frames; deeper ALTREF layers (gf_group
//     layer_depth > 1) require cpi->multi_layer_arf and the lookahead
//     buffer fan-out that govpx's single-ARF lookahead does not yet
//     model. The vp9FindARFOrder closure for `calc_arf_boost` therefore
//     ignores the libvpx `mid`-frame stats_in advance
//     (vp9_firstpass.c:2180-2185). This divergence is currently
//     unreachable: libvpx's vp9DefineGFGroupStructure (mirroring
//     vp9_firstpass.c:2218-2255) enters find_arf_order at depth=1 when
//     source_alt_ref_pending=false (with allowed_max_layer_depth pinned
//     at 0, so `depth(1) > 0` selects the leaf branch) and at depth=2
//     when source_alt_ref_pending=true (with allowed_max_layer_depth =
//     oxcf.enable_auto_arf). govpx pins EnableAutoARF=1
//     unconditionally (vp9_twopass.go:272) and exposes no option to
//     raise it; in libvpx, the recursive ARF case is only entered when
//     enable_auto_arf >= 2, which also flips cpi->multi_layer_arf=1
//     (vp9_encoder.c:6157). Audited at task #126: no fuzz seed or test
//     fixture sets EnableAutoARF != 1 or MultiLayerARF=true; the
//     C-oracle command lines use --auto-alt-ref in {0,1} only. Port
//     when govpx surfaces a multi-layer ARF option.
//   - kf_zeromotion_pct STATIC_MOTION_THRESH consumer in
//     pick_kf_q_bound_two_pass (vp9_ratectrl.c:1378). The
//     find_next_key_frame accumulator that feeds this is not yet
//     populated; until then the Q picker treats every KF as
//     non-static.
//   - GF_GROUP ext_rc fields (vp9_firstpass.h:100-111): arf_index_stack,
//     top_arf_idx, stack_size, ext_rc_ref[], ref_frame_list[][]. Only
//     consumed by libvpx's multi-layer ARF and external rate-control
//     paths (vp9_encoder.c:3286-3304, vp9_firstpass.c:2328
//     ext_rc_define_gf_group_structure). govpx does not yet ship
//     ext_ratectrl or multi-layer ARF, so these slots are intentionally
//     absent from vp9GFGroup. Promote when multi-ARF lands.
//   - PORTED: vbr_corpus_complexity branch in allocate_gf_group_bits
//     (vp9_firstpass.c:2503-2516). Toggled via
//     VP9EncoderOptions.VBRCorpusComplexity threaded through
//     vp9GFGroupInputs.

// vp9GFGroup mirrors libvpx GF_GROUP field-for-field. Slice lengths use
// vp9MaxStaticGFGroupLength+2 to match libvpx's MAX_STATIC_GF_GROUP_LENGTH+2.
//
// libvpx: vp9/encoder/vp9_firstpass.h:86
type vp9GFGroup struct {
	// Index is libvpx gf_group.index — the running position into the
	// per-slot arrays. libvpx initializes this to 0 at start-of-GOP.
	Index uint8
	// RFLevel mirrors libvpx gf_group.rf_level[].
	RFLevel [vp9MaxStaticGFGroupLength + 2]uint8
	// UpdateType mirrors libvpx gf_group.update_type[].
	UpdateType [vp9MaxStaticGFGroupLength + 2]uint8
	// ArfSrcOffset mirrors libvpx gf_group.arf_src_offset[].
	ArfSrcOffset [vp9MaxStaticGFGroupLength + 2]uint8
	// LayerDepth mirrors libvpx gf_group.layer_depth[].
	LayerDepth [vp9MaxStaticGFGroupLength + 2]uint8
	// FrameGOPIndex mirrors libvpx gf_group.frame_gop_index[].
	FrameGOPIndex [vp9MaxStaticGFGroupLength + 2]uint8
	// BitAllocation mirrors libvpx gf_group.bit_allocation[].
	BitAllocation [vp9MaxStaticGFGroupLength + 2]int
	// GFUBoost mirrors libvpx gf_group.gfu_boost[].
	GFUBoost [vp9MaxStaticGFGroupLength + 2]int
	// UpdateRefIdx mirrors libvpx gf_group.update_ref_idx[].
	UpdateRefIdx [vp9MaxStaticGFGroupLength + 2]int

	FrameStart            int
	FrameEnd              int
	GFGroupSize           int
	MaxLayerDepth         int
	AllowedMaxLayerDepth  int
	GroupNoiseEnergy      int
	BaselineGFInterval    int
	SourceAltRefPending   bool
	SourceAltRefActive    bool
	GFUBoostScalar        int // == rc->gfu_boost output
	ConstrainedGFGroup    bool
	ARFActiveBestQAdjustF float64 // rc->arf_active_best_quality_adjustment_factor
	ARFIncreaseActiveBest int     // rc->arf_increase_active_best_quality (-1,0,+1)
	ARNRStrengthAdjust    int     // twopass->arnr_strength_adjustment

	// Telemetry produced for test / diagnostic use; libvpx doesn't carry
	// these but the test harness needs them to assert parity against the
	// C dump.
	UseAltRef       bool
	GOPCodingFrames int
	ActiveGFMin     int
	ActiveGFMax     int
}

// libvpx constants used by define_gf_group.
const (
	// libvpx: vp9/encoder/vp9_firstpass.h:27
	vp9MaxARFLayers = 6
	// libvpx: vp9/encoder/vp9_lookahead.h:22
	vp9MaxLagBuffers = 25
	// libvpx: vp9/common/vp9_onyxc_int.h:34
	vp9RefsPerFrame = 3
	// libvpx: vp9/encoder/vp9_ratectrl.h:46
	vp9MaxStaticGFGroupLength = 250
	// libvpx: vp9/encoder/vp9_firstpass.c:2559
	vp9ARFAbsZoomThresh = 4.0
	// libvpx: vp9/encoder/vp9_firstpass.c:2561
	vp9MaxGFBoost = 5400
	// libvpx: vp9/encoder/vp9_firstpass.c:2880
	vp9LastALRActiveBestQAdjustFactor = 0.2
	// libvpx: vp9/encoder/vp9_ratectrl.h:30
	vp9DefaultKFBoost = 2000
	// libvpx: vp9/encoder/vp9_ratectrl.h:31
	vp9DefaultGFBoost = 2000
)

// libvpx FRAME_UPDATE_TYPE values. vp9/encoder/vp9_firstpass.h:62.
const (
	vp9KFUpdate         uint8 = 0
	vp9LFUpdate         uint8 = 1
	vp9GFUpdate         uint8 = 2
	vp9ARFUpdate        uint8 = 3
	vp9OverlayUpdate    uint8 = 4
	vp9MIDOverlayUpdate uint8 = 5
	vp9UseBufFrame      uint8 = 6
)

// libvpx RATE_FACTOR_LEVEL values. vp9/encoder/vp9_ratectrl.h:48.
const (
	vp9RFLInterNormal uint8 = 0
	vp9RFLInterHigh   uint8 = 1
	vp9RFLGFARFLow    uint8 = 2
	vp9RFLGFARFStd    uint8 = 3
	vp9RFLKFStd       uint8 = 4
)

// vp9GFGroupInputs aggregates every libvpx state field define_gf_group
// reads. The struct lets us call the analyzer as a pure function from
// tests / oracle scoreboards while threading the same data through the
// real encoder path.
//
// libvpx parity references for each field are inline.
type vp9GFGroupInputs struct {
	// libvpx: cpi->common.frame_type == KEY_FRAME at call time.
	IsKeyFrame bool
	// libvpx: rc->source_alt_ref_active (overlay slot of previous ARF).
	SourceAltRefActive bool
	// libvpx: rc->frames_to_key.
	FramesToKey int
	// libvpx: rc->frames_since_key.
	FramesSinceKey int
	// libvpx: rc->min_gf_interval / rc->max_gf_interval.
	MinGFInterval int
	MaxGFInterval int
	// libvpx: rc->static_scene_max_gf_interval.
	StaticSceneMaxGFInterval int
	// libvpx: twopass->active_worst_quality.
	ActiveWorstQuality int
	// libvpx: rc->last_boosted_qindex.
	LastBoostedQIndex int
	// libvpx: rc->avg_frame_qindex[INTER_FRAME].
	AvgFrameQIndexInter int
	// libvpx: rc->avg_frame_bandwidth.
	AvgFrameBandwidth int
	// libvpx: oxcf->lag_in_frames.
	LagInFrames int
	// libvpx: oxcf->aq_mode == PERCEPTUAL_AQ.
	PerceptualAQ bool
	// libvpx: is_lossless_requested(&cpi->oxcf).
	Lossless bool
	// libvpx: is_altref_enabled(cpi).
	AllowAltRef bool
	// libvpx: oxcf->enable_auto_arf (0 / 1).
	EnableAutoARF int
	// libvpx: cpi->multi_layer_arf.
	MultiLayerARF bool
	// libvpx: cpi->frame_info.frame_height / frame_width / mb_rows.
	FrameHeight int
	FrameWidth  int
	MBRows      int
	// libvpx: twopass->kf_group_bits / kf_group_error_left.
	KFGroupBits      int64
	KFGroupErrorLeft float64
	// libvpx: rc->avg_frame_bandwidth-derived frame_max_bits clamp
	// (vp9_ratectrl.c frame_max_bits).
	FrameMaxBits int
	// libvpx: twopass->gf_max_total_boost.
	GFMaxTotalBoost int
	// libvpx: VP9_COMP::current_video_frame.
	CurrentVideoFrame int
	// libvpx: twopass->mean_mod_score / cpi->twopass distribution_av_err.
	MeanModScore float64
	AvErr        float64
	// First-pass per-show-frame stats slice (display order, no terminal
	// total row). Indexed by show_idx as libvpx does via
	// fps_get_frame_stats.
	Stats []VP9FirstPassFrameStats
	// gf_start_show_idx — the libvpx start-of-GF show index argument.
	GFStartShowIdx int
	// Two-pass tuning parameters (VP9ARFBoostParams).
	BoostParams VP9ARFBoostParams
	// VBRCorpusComplexity mirrors oxcf->vbr_corpus_complexity. Non-zero
	// activates the corpus-VBR branch in allocate_gf_group_bits, which
	// distributes intra-GOP bits proportional to per-frame normalized
	// modified scores instead of an even split.
	// libvpx: vp9/encoder/vp9_firstpass.c:2503-2516.
	VBRCorpusComplexity int
	// TwoPassVBRBiasPct mirrors oxcf->two_pass_vbrbias. Used as the bias
	// exponent inside calculate_norm_frame_score; needed by the corpus
	// branch of allocate_gf_group_bits. libvpx vp9/encoder/vp9_firstpass.c:272.
	TwoPassVBRBiasPct int
	// TwoPassVBRMinSection mirrors oxcf->two_pass_vbrmin_section. libvpx
	// vp9_firstpass.c:294.
	TwoPassVBRMinSection int
	// TwoPassVBRMaxSection mirrors oxcf->two_pass_vbrmax_section. libvpx
	// vp9_firstpass.c:295.
	TwoPassVBRMaxSection int
}

// vp9DefineGFGroup is a verbatim Go port of libvpx's define_gf_group. It
// returns the populated GF_GROUP plus the produced rc->gfu_boost and
// constrained-group flag.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2761 define_gf_group
func vp9DefineGFGroup(in vp9GFGroupInputs) vp9GFGroup {
	var gf vp9GFGroup

	// libvpx: const int arf_active_or_kf =
	//   is_key_frame || rc->source_alt_ref_active;
	arfActiveOrKF := in.IsKeyFrame || in.SourceAltRefActive

	// libvpx: vp9_zero(twopass->gf_group); ++rc->gop_global_index OR
	// rc->gop_global_index = 0;  (no state we own outside the returned gf).

	// libvpx: active_gf_interval = get_active_gf_inverval_range(...)
	activeGF := vp9GetActiveGFIntervalRange(in, arfActiveOrKF)
	gf.ActiveGFMin = activeGF.Min
	gf.ActiveGFMax = activeGF.Max

	// libvpx: gop_intra_factor =
	//   cpi->multi_layer_arf ? 1.0 + 0.25 * get_arf_layers(...) : 1.0.
	gopIntraFactor := 1.0
	if in.MultiLayerARF {
		gopIntraFactor = 1.0 + 0.25*float64(vp9GetARFLayers(in.MultiLayerARF, in.EnableAutoARF, activeGF.Max))
	}

	// libvpx: gop_coding_frames = get_gop_coding_frame_num(...)
	useAltRef := false
	endOfSequence := false
	gopCodingFrames := vp9GetGOPCodingFrameNum(&useAltRef, in, &activeGF,
		gopIntraFactor, &endOfSequence)
	if !in.AllowAltRef {
		useAltRef = false
	}
	gf.UseAltRef = useAltRef
	gf.GOPCodingFrames = gopCodingFrames

	// libvpx: rc->constrained_gf_group =
	//   (gop_coding_frames >= rc->frames_to_key) ? 1 : 0;
	gf.ConstrainedGFGroup = gopCodingFrames >= in.FramesToKey

	// libvpx: compute_arf_boost / non-alt-ref boost computation.
	var gfuBoost int
	if useAltRef {
		// libvpx: f_frames =
		//   (rc->frames_to_key - gop_coding_frames >= gop_coding_frames - 1)
		//       ? gop_coding_frames - 1
		//       : VPXMAX(0, rc->frames_to_key - gop_coding_frames);
		var fFrames int
		if in.FramesToKey-gopCodingFrames >= gopCodingFrames-1 {
			fFrames = gopCodingFrames - 1
		} else {
			fFrames = max(0, in.FramesToKey-gopCodingFrames)
		}
		bFrames := gopCodingFrames - 1
		arfShowIdx := min(in.GFStartShowIdx+gopCodingFrames+1, len(in.Stats))
		gfuBoost = VP9ComputeARFBoost(in.Stats, arfShowIdx, fFrames, bFrames,
			in.AvgFrameQIndexInter, in.BoostParams)
		gf.SourceAltRefPending = true
	} else {
		fFrames := gopCodingFrames - 1
		bFrames := 0
		gldShowIdx := min(in.GFStartShowIdx+1, len(in.Stats))
		arfBoost := VP9ComputeARFBoost(in.Stats, gldShowIdx, fFrames, bFrames,
			in.AvgFrameQIndexInter, in.BoostParams)
		gfuBoost = arfBoost
		if in.GFMaxTotalBoost > 0 && gfuBoost > in.GFMaxTotalBoost {
			gfuBoost = in.GFMaxTotalBoost
		}
		gf.SourceAltRefPending = false
	}

	// libvpx: rc->arf_active_best_quality_adjustment_factor = 1.0;
	gf.ARFActiveBestQAdjustF = 1.0
	gf.ARFIncreaseActiveBest = 0
	if !in.Lossless {
		// libvpx vp9_firstpass.c:2884-2904. Two branches: post-mid-KF and
		// pre-mid-KF. Each linearly interpolates the active-best
		// adjustment factor between LAST_ALR_ACTIVE_BEST_QUALITY_ADJUSTMENT_FACTOR
		// and 1.0.
		if in.FramesSinceKey >= in.FramesToKey {
			denom := max(1, (in.FramesToKey+in.FramesSinceKey)/2-gopCodingFrames)
			gf.ARFActiveBestQAdjustF = vp9LastALRActiveBestQAdjustFactor +
				(1.0-vp9LastALRActiveBestQAdjustFactor)*
					float64(in.FramesToKey-gopCodingFrames)/float64(denom)
			gf.ARFIncreaseActiveBest = 1
		} else if in.FramesToKey-gopCodingFrames > 0 {
			denom := max(1, (in.FramesToKey+in.FramesSinceKey)/2+gopCodingFrames)
			gf.ARFActiveBestQAdjustF = vp9LastALRActiveBestQAdjustFactor +
				(1.0-vp9LastALRActiveBestQAdjustFactor)*
					float64(in.FramesSinceKey+gopCodingFrames)/float64(denom)
			gf.ARFIncreaseActiveBest = -1
		}
	}

	// libvpx vp9_firstpass.c:2907-2912 (non-AGGRESSIVE_VBR branch).
	if cap := gopCodingFrames * 200; gfuBoost > cap {
		gfuBoost = cap
	}
	// libvpx vp9_firstpass.c:2918-2919 perceptual AQ cap.
	if in.PerceptualAQ && gfuBoost > vp9MinARFGFBoost {
		gfuBoost = vp9MinARFGFBoost
	}
	gf.GFUBoostScalar = gfuBoost

	// libvpx: rc->baseline_gf_interval =
	//   gop_coding_frames - rc->source_alt_ref_pending;
	baselineGFInterval := gopCodingFrames
	if gf.SourceAltRefPending {
		baselineGFInterval--
	}
	gf.BaselineGFInterval = baselineGFInterval

	// libvpx vp9_firstpass.c:2926-2966 — accumulate per-frame errors /
	// noise / inter / motion for the GF group.
	gfGroupErr := 0.0
	gfGroupNoise := 0.0
	gfGroupInter := 0.0
	gfGroupMotion := 0.0
	startIdx := 0
	if arfActiveOrKF {
		startIdx = 1
	}
	for j := startIdx; j < gopCodingFrames; j++ {
		showIdx := in.GFStartShowIdx + j
		if showIdx < 0 || showIdx >= len(in.Stats) {
			break
		}
		fs := in.Stats[showIdx]
		// libvpx vp9_firstpass.c:2957 calls calc_norm_frame_score with the
		// configured oxcf bias/min/max; honour those here too instead of
		// the libvpx-default 50% / 0 / 2000 captured in
		// vp9CalcNormFrameScore.
		gfGroupErr += vp9CalcNormFrameScoreFromInputs(fs, in)
		gfGroupNoise += fs.FrameNoiseEnergy
		gfGroupInter += fs.PcntInter
		gfGroupMotion += fs.PcntMotion
	}

	// libvpx: gf_group_bits = calculate_total_gf_group_bits(cpi, gf_group_err)
	gfGroupBits := vp9CalculateTotalGFGroupBits(in, gf, arfActiveOrKF, gfGroupErr)

	// libvpx vp9_firstpass.c:2971-2980 — gop_frames + group_noise_energy.
	gopFrames := baselineGFInterval
	if gf.SourceAltRefPending {
		gopFrames++
	}
	if arfActiveOrKF {
		gopFrames--
	}
	if gopFrames > 0 {
		gf.GroupNoiseEnergy = int(gfGroupNoise / float64(gopFrames))
	}

	// libvpx vp9_firstpass.c:3010-3016 — adjust_group_arnr_filter.
	if baselineGFInterval > 1 && gopFrames > 0 {
		gf.ARNRStrengthAdjust = vp9AdjustGroupARNRFilter(
			gfGroupNoise/float64(gopFrames),
			gfGroupInter/float64(gopFrames),
			gfGroupMotion/float64(gopFrames))
	}

	// libvpx: gf_arf_bits = calculate_boost_bits(...)
	gfARFBits := vp9CalculateBoostBits(baselineGFInterval-1, gfuBoost, gfGroupBits)

	// libvpx: define_gf_group_structure(cpi) -> populates slot arrays.
	vp9DefineGFGroupStructure(&gf, in)

	// libvpx: allocate_gf_group_bits(cpi, gf_group_bits, gf_arf_bits)
	vp9AllocateGFGroupBits(&gf, in, gfGroupBits, gfARFBits, arfActiveOrKF)

	return gf
}

// vp9Range mirrors libvpx's RANGE typedef.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2563
type vp9Range struct {
	Min int
	Max int
}

// vp9GetActiveGFIntervalRange ports libvpx get_active_gf_inverval_range.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2701
func vp9GetActiveGFIntervalRange(in vp9GFGroupInputs, arfActiveOrKF bool) vp9Range {
	arfBool := 0
	if arfActiveOrKF {
		arfBool = 1
	}
	intMaxQ := int(vp9ConvertQIndexToQ(in.ActiveWorstQuality))
	var qTerm int
	if in.GFStartShowIdx == 0 {
		qTerm = intMaxQ / 32
	} else {
		qTerm = int(vp9ConvertQIndexToQ(in.LastBoostedQIndex)) / 6
	}
	r := vp9Range{}
	r.Min = in.MinGFInterval + arfBool + min(2, intMaxQ/200)
	r.Min = min(r.Min, in.MaxGFInterval+arfBool)
	r.Max = 11 + arfBool + min(5, qTerm)
	// libvpx: Force max GF interval to be odd.
	r.Max |= 0x01
	if r.Max < r.Min {
		r.Max = r.Min
	} else {
		r.Max = min(r.Max, in.MaxGFInterval+arfBool)
	}
	// libvpx: Would the active max drop us out just before the near the next kf?
	if r.Max <= in.FramesToKey && r.Max >= (in.FramesToKey-in.MinGFInterval) {
		r.Max = in.FramesToKey / 2
	}
	if r.Max < r.Min {
		r.Max = r.Min
	}
	return r
}

// vp9GetGOPCodingFrameNum ports libvpx get_gop_coding_frame_num.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2587
func vp9GetGOPCodingFrameNum(useAltRef *bool, in vp9GFGroupInputs,
	activeGFInterval *vp9Range, gopIntraFactor float64,
	endOfSequence *bool,
) int {
	loopDecayRate := 1.0
	mvRatioAccumulator := 0.0
	thisFrameMVInOut := 0.0
	mvInOutAccumulator := 0.0
	absMVInOutAccumulator := 0.0
	srAccumulator := 0.0
	// libvpx: mv_ratio_accumulator_thresh =
	//   (frame_height + frame_width) / 4.0.
	mvRatioAccumulatorThresh := float64(in.FrameHeight+in.FrameWidth) / 4.0
	zeroMotionAccumulator := 1.0

	*useAltRef = true
	gopCodingFrames := 0
	for gopCodingFrames < in.StaticSceneMaxGFInterval &&
		gopCodingFrames < in.FramesToKey {
		gopCodingFrames++
		nextIdx := in.GFStartShowIdx + gopCodingFrames
		if nextIdx < 0 || nextIdx >= len(in.Stats) {
			*endOfSequence = gopCodingFrames == 1 && in.SourceAltRefActive
			break
		}
		nextFrame := in.Stats[nextIdx]
		// libvpx: detect_flash_from_frame_stats(next_next_frame).
		var nextNext *VP9FirstPassFrameStats
		nnIdx := in.GFStartShowIdx + gopCodingFrames + 1
		if nnIdx >= 0 && nnIdx < len(in.Stats) {
			nnf := in.Stats[nnIdx]
			nextNext = &nnf
		}
		flashDetected := vp9DetectFlashFromFrameStats(nextNext)

		// libvpx: accumulate_frame_motion_stats(next_frame, ...)
		vp9AccumulateFrameMotionStats(nextFrame, &thisFrameMVInOut,
			&mvInOutAccumulator, &absMVInOutAccumulator, &mvRatioAccumulator)

		// libvpx: Monitor for static sections.
		if (in.FramesSinceKey + gopCodingFrames - 1) > 1 {
			zm := vp9GetZeroMotionFactor(nextFrame, in.BoostParams.ZMFactor)
			if zm < zeroMotionAccumulator {
				zeroMotionAccumulator = zm
			}
		}

		// libvpx: Accumulate the effect of prediction quality decay.
		if !flashDetected {
			lastLoopDecayRate := loopDecayRate
			loopDecayRate = vp9GetPredictionDecayRate(nextFrame,
				in.BoostParams.SRDiffFactor, in.BoostParams.SRDefaultDecayLimit,
				in.BoostParams.ZMFactor)
			// libvpx: still-section detection breaker.
			if gopCodingFrames > in.MinGFInterval &&
				loopDecayRate >= 0.999 && lastLoopDecayRate < 0.9 {
				stillInterval := 5
				if vp9CheckTransitionToStill(in.Stats,
					in.GFStartShowIdx+gopCodingFrames, stillInterval,
					in.BoostParams) {
					*useAltRef = false
					break
				}
			}
			// libvpx: sr_accumulator update.
			if gopCodingFrames == 1 {
				srAccumulator += nextFrame.CodedError
			} else {
				srAccumulator += nextFrame.SRCodedError - nextFrame.CodedError
			}
		}

		// libvpx: Break out conditions (vp9_firstpass.c:2679-2693).
		if gopCodingFrames >= activeGFInterval.Max &&
			(zeroMotionAccumulator < 0.995 || in.SourceAltRefActive) {
			break
		}
		if gopCodingFrames >= activeGFInterval.Min &&
			(in.FramesToKey-gopCodingFrames) >= in.MinGFInterval &&
			(gopCodingFrames&0x01) == 1 && !flashDetected &&
			(mvRatioAccumulator > mvRatioAccumulatorThresh ||
				absMVInOutAccumulator > vp9ARFAbsZoomThresh ||
				srAccumulator > gopIntraFactor*nextFrame.IntraError) {
			break
		}
	}
	// libvpx: *use_alt_ref &= zero_motion_accumulator < 0.995;
	if zeroMotionAccumulator >= 0.995 {
		*useAltRef = false
	}
	// libvpx: *use_alt_ref &= gop_coding_frames < lag_in_frames;
	if gopCodingFrames >= in.LagInFrames {
		*useAltRef = false
	}
	// libvpx: *use_alt_ref &= gop_coding_frames >= rc->min_gf_interval;
	if gopCodingFrames < in.MinGFInterval {
		*useAltRef = false
	}
	return gopCodingFrames
}

// vp9CheckTransitionToStill ports libvpx check_transition_to_still.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1810 check_transition_to_still (this
// helper inspects subsequent frames to confirm we're in a still section).
func vp9CheckTransitionToStill(stats []VP9FirstPassFrameStats, startIdx, stillInterval int, params VP9ARFBoostParams) bool {
	// libvpx requires at least still_interval frames remaining; if we
	// can't see that far ahead, treat as not-still.
	if startIdx+stillInterval >= len(stats) {
		return false
	}
	// libvpx: loop forward still_interval frames; if any frame's
	// prediction decay rate falls below 0.999, the transition isn't
	// still.
	for i := range stillInterval {
		idx := startIdx + i
		if idx < 0 || idx >= len(stats) {
			return false
		}
		dr := vp9GetPredictionDecayRate(stats[idx], params.SRDiffFactor,
			params.SRDefaultDecayLimit, params.ZMFactor)
		if dr < 0.999 {
			return false
		}
		if stats[idx].PcntInter-stats[idx].PcntMotion < 0.999 {
			return false
		}
	}
	return true
}

// vp9GetZeroMotionFactor ports libvpx get_zero_motion_factor.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1797 get_zero_motion_factor
func vp9GetZeroMotionFactor(frame VP9FirstPassFrameStats, zmFactor float64) float64 {
	zm := zmFactor * (frame.PcntInter - frame.PcntMotion)
	if zm < 0 {
		return 0
	}
	if zm > 1.0 {
		return 1.0
	}
	return zm
}

// vp9GetARFLayers ports libvpx get_arf_layers.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2745
func vp9GetARFLayers(multiLayerARF bool, maxLayers, codingFrameNum int) int {
	if !multiLayerARF {
		return 1
	}
	layers := 0
	for i := codingFrameNum; i > 0; i >>= 1 {
		layers++
	}
	if layers > maxLayers {
		return maxLayers
	}
	return layers
}

// vp9CalcNormFrameScoreConfig ports libvpx calc_norm_frame_score
// (vp9_firstpass.c:285) for one row, including oxcf->two_pass_vbrbias,
// vbrmin_section, and vbrmax_section.
// When vbrBiasPct <= 0 the libvpx default (50) is used; when
// vbrMaxSection <= 0 the libvpx default (2000) is used. vbrMinSection is
// taken as-is (libvpx's default is 0, meaning no lower clamp).
func vp9CalcNormFrameScoreConfig(row VP9FirstPassFrameStats,
	meanModScore, avErr float64, mbRows int,
	vbrBiasPct, vbrMinSection, vbrMaxSection int,
) float64 {
	if meanModScore <= 0 {
		meanModScore = 1
	}
	if avErr <= 0 {
		avErr = 1
	}
	err := row.CodedError
	if err < 1 {
		err = 1
	}
	weight := row.Weight
	if weight <= 0 {
		weight = 1
	}
	// libvpx vp9_firstpass.c:289-292 — modified_score =
	//   av_err * pow(err*weight / av_err, oxcf->two_pass_vbrbias / 100).
	bias := vbrBiasPct
	if bias <= 0 {
		bias = vp9DefaultTwoPassVBRBiasPct
	}
	score := avErr * vp9PowSafe((err*weight)/avErr, float64(bias)/100.0)
	score *= vp9PowSafe(vp9CalculateActiveArea(mbRows, row), vp9ActiveAreaCorrection)
	// libvpx vp9_firstpass.c:306-307 normalize and clamp to
	// [min_section/100, max_section/100].
	normalized := score / meanModScore
	maxSection := vbrMaxSection
	if maxSection <= 0 {
		maxSection = vp9DefaultVBRMaxSectionPct
	}
	minScore := float64(vbrMinSection) / 100.0
	maxScore := float64(maxSection) / 100.0
	if minScore < 0 {
		minScore = 0
	}
	if normalized < minScore {
		normalized = minScore
	}
	if normalized > maxScore {
		normalized = maxScore
	}
	return normalized
}

// vp9CalcNormFrameScoreFromInputs threads the configured VBR bias / min /
// max from vp9GFGroupInputs into vp9CalcNormFrameScoreConfig. This is what
// vp9DefineGFGroup uses when accumulating gf_group_err so the value matches
// libvpx under non-default oxcf->two_pass_vbr* settings.
func vp9CalcNormFrameScoreFromInputs(row VP9FirstPassFrameStats,
	in vp9GFGroupInputs,
) float64 {
	return vp9CalcNormFrameScoreConfig(row, in.MeanModScore, in.AvErr,
		in.MBRows, in.TwoPassVBRBiasPct, in.TwoPassVBRMinSection,
		in.TwoPassVBRMaxSection)
}

func vp9PowSafe(base, exp float64) float64 {
	if base <= 0 {
		return 0
	}
	return math.Pow(base, exp)
}

// vp9CalculateTotalGFGroupBits ports libvpx calculate_total_gf_group_bits.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2057
func vp9CalculateTotalGFGroupBits(in vp9GFGroupInputs, gf vp9GFGroup, arfActiveOrKF bool, gfGroupErr float64) int64 {
	maxBits := int64(in.FrameMaxBits)
	if maxBits <= 0 {
		// Fallback if caller didn't precompute frame_max_bits.
		maxBits = int64(in.AvgFrameBandwidth) * int64(in.MaxGFInterval)
	}
	gopFrames := gf.BaselineGFInterval
	if gf.SourceAltRefPending {
		gopFrames++
	}
	if arfActiveOrKF {
		gopFrames--
	}
	if gopFrames < 0 {
		gopFrames = 0
	}

	var total int64
	if in.KFGroupBits > 0 && in.KFGroupErrorLeft > 0 {
		keyFrameInterval := in.FramesSinceKey + in.FramesToKey
		distFromNextKF := in.FramesToKey - (gf.BaselineGFInterval + boolToInt(gf.SourceAltRefPending))
		maxGFBitsBias := in.AvgFrameBandwidth
		gfIntervalBiasNorm := float64(gf.BaselineGFInterval) / 16.0
		total = int64(float64(in.KFGroupBits) * (gfGroupErr / in.KFGroupErrorLeft))
		total += int64(float64(distFromNextKF) / float64(keyFrameInterval) *
			float64(maxGFBitsBias) * gfIntervalBiasNorm)
	}
	if total < 0 {
		total = 0
	} else if total > in.KFGroupBits {
		total = in.KFGroupBits
	}
	if total > maxBits*int64(gopFrames) {
		total = maxBits * int64(gopFrames)
	}
	return total
}

// vp9CalculateBoostBits ports libvpx calculate_boost_bits.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2102
func vp9CalculateBoostBits(frameCount, boost int, totalGroupBits int64) int {
	if boost == 0 || totalGroupBits <= 0 || frameCount < 0 {
		return 0
	}
	allocationChunks := frameCount*vp9NormalBoost + boost
	// libvpx: Prevent overflow.
	if boost > 1023 {
		divisor := boost >> 10
		boost /= divisor
		allocationChunks /= divisor
	}
	v := int(int64(boost) * totalGroupBits / int64(allocationChunks))
	if v < 0 {
		return 0
	}
	return v
}

// vp9FindARFOrder ports libvpx find_arf_order — recursive multi-ARF
// placement. The recursion bottoms out at depth > allowed_max_layer_depth
// or sub-interval < min_frame_interval, emitting LF_UPDATE leaves. Mid-
// frame becomes an ARF_UPDATE with a USE_BUF_FRAME mid-overlay slot.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2146
func vp9FindARFOrder(gf *vp9GFGroup, indexCounter *int, depth, start, end int, calcArfBoost func(fFrames, bFrames int) int) {
	const minFrameInterval = 2
	mid := (start + end + 1) >> 1
	if end-start < minFrameInterval || depth > gf.AllowedMaxLayerDepth {
		for idx := start; idx <= end; idx++ {
			i := *indexCounter
			gf.UpdateType[i] = vp9LFUpdate
			gf.ArfSrcOffset[i] = 0
			gf.FrameGOPIndex[i] = uint8(idx)
			gf.RFLevel[i] = vp9RFLInterNormal
			gf.LayerDepth[i] = uint8(depth)
			gf.GFUBoost[i] = vp9NormalBoost
			*indexCounter++
		}
		if depth > gf.MaxLayerDepth {
			gf.MaxLayerDepth = depth
		}
		return
	}
	i := *indexCounter
	gf.LayerDepth[i] = uint8(depth)
	gf.UpdateType[i] = vp9ARFUpdate
	gf.ArfSrcOffset[i] = uint8(mid - start)
	gf.FrameGOPIndex[i] = uint8(mid)
	gf.RFLevel[i] = vp9RFLGFARFLow
	boost := max(calcArfBoost(end-mid+1, mid-start)>>depth, vp9MinARFGFBoost)
	gf.GFUBoost[i] = boost
	*indexCounter++

	vp9FindARFOrder(gf, indexCounter, depth+1, start, mid-1, calcArfBoost)

	i = *indexCounter
	gf.UpdateType[i] = vp9UseBufFrame
	gf.ArfSrcOffset[i] = 0
	gf.FrameGOPIndex[i] = uint8(mid)
	gf.RFLevel[i] = vp9RFLInterNormal
	gf.LayerDepth[i] = uint8(depth)
	*indexCounter++

	vp9FindARFOrder(gf, indexCounter, depth+1, mid+1, end, calcArfBoost)
}

func vp9SetGFOverlayFrameType(gf *vp9GFGroup, frameIdx int, sourceAltRefActive bool) {
	if sourceAltRefActive {
		gf.UpdateType[frameIdx] = vp9OverlayUpdate
		gf.RFLevel[frameIdx] = vp9RFLInterNormal
		gf.LayerDepth[frameIdx] = vp9MaxARFLayers - 1
		gf.GFUBoost[frameIdx] = vp9NormalBoost
	} else {
		gf.UpdateType[frameIdx] = vp9GFUpdate
		gf.RFLevel[frameIdx] = vp9RFLGFARFStd
		gf.LayerDepth[frameIdx] = 0
	}
}

// vp9DefineGFGroupStructure ports libvpx define_gf_group_structure.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2218
func vp9DefineGFGroupStructure(gf *vp9GFGroup, in vp9GFGroupInputs) {
	frameIndex := 0
	keyFrame := in.IsKeyFrame
	layerDepth := 1
	gopFrames := gf.BaselineGFInterval - boolToInt(keyFrame || gf.SourceAltRefPending)

	gf.FrameStart = in.CurrentVideoFrame
	gf.FrameEnd = gf.FrameStart + gf.BaselineGFInterval
	gf.MaxLayerDepth = 0
	gf.AllowedMaxLayerDepth = 0

	if !keyFrame {
		vp9SetGFOverlayFrameType(gf, frameIndex, gf.SourceAltRefActive)
	}
	frameIndex++

	if gf.SourceAltRefPending {
		gf.UpdateType[frameIndex] = vp9ARFUpdate
		gf.RFLevel[frameIndex] = vp9RFLGFARFStd
		gf.LayerDepth[frameIndex] = uint8(layerDepth)
		gf.ArfSrcOffset[frameIndex] = uint8(gf.BaselineGFInterval - 1)
		gf.FrameGOPIndex[frameIndex] = uint8(gf.BaselineGFInterval)
		gf.MaxLayerDepth = 1
		frameIndex++
		layerDepth++
		gf.AllowedMaxLayerDepth = in.EnableAutoARF
	}

	// libvpx: find_arf_order(cpi, gf_group, &frame_index, layer_depth, 1, gop_frames)
	// Multi-ARF deeper recursion deferred (see file header). We provide a
	// per-call closure for calc_arf_boost when allowed_max_layer_depth>1.
	// Audited at task #126: with EnableAutoARF=1 pinned and
	// MultiLayerARF=false, both find_arf_order entries (depth=1 with
	// allowed_max=0 when SourceAltRefPending=false, depth=2 with
	// allowed_max=1 when SourceAltRefPending=true) hit
	// `depth > allowed_max_layer_depth` and take the leaf branch. The
	// closure below is never invoked under current govpx options; left in
	// place so the recursion lights up cleanly when multi-layer ARF lands.
	calcArfBoost := func(fFrames, bFrames int) int {
		// libvpx call site (find_arf_order line ~2185) passes
		// arfShowIdx == twopass_stats_in advanced by `mid` frames from
		// start_pos; here we use the GFStartShowIdx + (mid - start + 1)
		// frame as anchor when computing boost.
		return VP9ComputeARFBoost(in.Stats, in.GFStartShowIdx+1, fFrames,
			bFrames, in.AvgFrameQIndexInter, in.BoostParams)
	}
	vp9FindARFOrder(gf, &frameIndex, layerDepth, 1, gopFrames, calcArfBoost)

	vp9SetGFOverlayFrameType(gf, frameIndex, gf.SourceAltRefPending)
	gf.ArfSrcOffset[frameIndex] = 0
	gf.FrameGOPIndex[frameIndex] = uint8(gf.BaselineGFInterval)

	gf.GFGroupSize = frameIndex
}

// vp9AllocateGFGroupBits ports libvpx allocate_gf_group_bits.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2391
func vp9AllocateGFGroupBits(gf *vp9GFGroup, in vp9GFGroupInputs,
	gfGroupBits int64, gfARFBits int, arfActiveOrKF bool,
) {
	frameIndex := 0
	totalGroupBits := gfGroupBits
	maxBits := in.FrameMaxBits
	if maxBits <= 0 {
		maxBits = in.AvgFrameBandwidth * 8
	}

	keyFrame := in.IsKeyFrame

	// libvpx vp9_firstpass.c:2420-2423.
	if !keyFrame {
		if gf.SourceAltRefActive {
			gf.BitAllocation[frameIndex] = 0
		} else {
			gf.BitAllocation[frameIndex] = gfARFBits
		}
	}
	// libvpx vp9_firstpass.c:2427.
	if gf.SourceAltRefPending || !keyFrame {
		totalGroupBits -= int64(gfARFBits)
	}
	frameIndex++

	// libvpx vp9_firstpass.c:2433-2437.
	if gf.SourceAltRefPending {
		gf.BitAllocation[frameIndex] = gfARFBits
		frameIndex++
	}

	// libvpx vp9_firstpass.c:2440.
	midFrameIdx := frameIndex + (gf.BaselineGFInterval >> 1) - 1

	// libvpx vp9_firstpass.c:2442-2446.
	normalFrames := gf.BaselineGFInterval - 1
	var normalFrameBits int
	if normalFrames > 1 {
		normalFrameBits = int(totalGroupBits / int64(normalFrames))
	} else {
		normalFrameBits = int(totalGroupBits)
	}

	// libvpx: gf_group->gfu_boost[1] = rc->gfu_boost.
	gf.GFUBoost[1] = gf.GFUBoostScalar

	// libvpx vp9_firstpass.c:2503-2506 — corpus-VBR branch precomputes
	// the distribution mean error and the group-level normalized-score
	// sum used to renormalize each frame's complexity-weighted share.
	corpus := in.VBRCorpusComplexity != 0
	var avScore, totNormFrameScore float64
	if corpus {
		avScore = vp9DistributionAverageError(in.MeanModScore, in.AvErr)
		totNormFrameScore = vp9CalculateGroupScore(in, avScore, normalFrames)
	}

	// Multi-layer ARF branch deferred — see file-header TODO. We always
	// take the single-ARF branch (allocate normal-frame bits uniformly
	// across leaves, with the last-frame reduction redistributed to the
	// middle slot per libvpx's bitstream-parity rule).
	lastFrameReduction := 0
	for i := range normalFrames {
		// libvpx vp9_firstpass.c:2509-2516 — input_stats() advance plus
		// per-frame normal_frame_bits override when corpus VBR is active.
		if corpus {
			showIdx := in.GFStartShowIdx + i
			if showIdx < 0 || showIdx >= len(in.Stats) {
				break
			}
			thisFrameScore := vp9CalcNormFrameScoreCorpus(in.Stats[showIdx], avScore,
				in.MeanModScore, in.TwoPassVBRBiasPct,
				in.TwoPassVBRMinSection, in.TwoPassVBRMaxSection, in.MBRows)
			if totNormFrameScore > 0 {
				normalFrameBits = int(float64(totalGroupBits) *
					(thisFrameScore / totNormFrameScore))
			}
		}
		target := normalFrameBits
		if i == normalFrames-1 && i >= 1 {
			lastFrameReduction = normalFrameBits / 16
			target -= lastFrameReduction
		}
		clampMax := min(int(totalGroupBits), maxBits)
		if target < 0 {
			target = 0
		} else if target > clampMax {
			target = clampMax
		}
		if frameIndex < len(gf.BitAllocation) {
			gf.BitAllocation[frameIndex] = target
		}
		frameIndex++
	}

	// libvpx: gf_group->bit_allocation[mid_frame_idx] += last_frame_reduction.
	if midFrameIdx >= 0 && midFrameIdx < len(gf.BitAllocation) {
		gf.BitAllocation[midFrameIdx] += lastFrameReduction
	}
}

// vp9DistributionAverageError mirrors libvpx get_distribution_av_err for the
// corpus-VBR branch. When the encoder configures corpus VBR, libvpx returns
// `av_weight * twopass->mean_mod_score` (vp9_firstpass.c:255-256); we already
// have those two scalars stored on the inputs as MeanModScore and AvErr
// (AvErr is the libvpx pre-corpus av_weight average for the clip).
//
// libvpx: vp9/encoder/vp9_firstpass.c:251-260 get_distribution_av_err.
func vp9DistributionAverageError(meanModScore, avErr float64) float64 {
	if meanModScore <= 0 {
		if avErr > 0 {
			return avErr
		}
		return 1
	}
	if avErr > 0 {
		return avErr * meanModScore
	}
	return meanModScore
}

// vp9CalcNormFrameScoreCorpus ports libvpx's calc_norm_frame_score helper
// used by the corpus-VBR branch of allocate_gf_group_bits. Unlike the
// existing vp9CalcNormFrameScore helper (which hardcodes the libvpx default
// 50% bias and the default [0.01, 20] clamp), this variant honours the
// configured `oxcf->two_pass_vbrbias`, `oxcf->two_pass_vbrmin_section`, and
// `oxcf->two_pass_vbrmax_section` so the corpus branch matches libvpx
// byte-for-byte under non-default settings.
//
// libvpx: vp9/encoder/vp9_firstpass.c:285 calc_norm_frame_score.
func vp9CalcNormFrameScoreCorpus(row VP9FirstPassFrameStats, avErr, meanModScore float64,
	vbrBiasPct, vbrMinSection, vbrMaxSection, mbRows int,
) float64 {
	bias := vbrBiasPct
	if bias <= 0 {
		bias = vp9DefaultTwoPassVBRBiasPct
	}
	maxSection := vbrMaxSection
	if maxSection <= 0 {
		maxSection = vp9DefaultVBRMaxSectionPct
	}
	err := row.CodedError
	if err < 1 {
		err = 1
	}
	weight := row.Weight
	if weight <= 0 {
		weight = 1
	}
	// libvpx vp9_firstpass.c:289-292.
	modifiedScore := avErr * math.Pow((err*weight)/nonZeroFloat(avErr),
		float64(bias)/100.0)
	// libvpx vp9_firstpass.c:302-303 active-area correction.
	if mbRows <= 0 {
		mbRows = 1
	}
	active := 1.0 - ((row.IntraSkipPct / 2.0) +
		((row.InactiveZoneRows * 2.0) / float64(mbRows)))
	if active < vp9MinActiveArea {
		active = vp9MinActiveArea
	} else if active > vp9MaxActiveArea {
		active = vp9MaxActiveArea
	}
	modifiedScore *= math.Pow(active, vp9ActiveAreaCorrection)
	// libvpx vp9_firstpass.c:306-307 normalize + clamp to [min, max].
	modifiedScore /= nonZeroFloat(meanModScore)
	minScore := float64(vbrMinSection) / 100.0
	maxScore := float64(maxSection) / 100.0
	if modifiedScore < minScore {
		modifiedScore = minScore
	}
	if modifiedScore > maxScore {
		modifiedScore = maxScore
	}
	if modifiedScore <= 0 || math.IsNaN(modifiedScore) || math.IsInf(modifiedScore, 0) {
		return 1
	}
	return modifiedScore
}

// vp9CalculateGroupScore mirrors libvpx calculate_group_score, summing the
// normalized modified scores for the next `frameCount` show frames starting
// at GFStartShowIdx.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2126 calculate_group_score.
func vp9CalculateGroupScore(in vp9GFGroupInputs, avScore float64, frameCount int) float64 {
	if frameCount <= 0 {
		// libvpx vp9_firstpass.c:2135 — never return 0.
		return 1.0
	}
	scoreTotal := 0.0
	for i := range frameCount {
		showIdx := in.GFStartShowIdx + i
		if showIdx < 0 || showIdx >= len(in.Stats) {
			break
		}
		scoreTotal += vp9CalcNormFrameScoreCorpus(in.Stats[showIdx], avScore,
			in.MeanModScore, in.TwoPassVBRBiasPct,
			in.TwoPassVBRMinSection, in.TwoPassVBRMaxSection, in.MBRows)
	}
	if scoreTotal <= 0 {
		return 1.0
	}
	return scoreTotal
}

// vp9AdjustGroupARNRFilter ports libvpx adjust_group_arnr_filter.
//
// libvpx: vp9/encoder/vp9_firstpass.c:2541
func vp9AdjustGroupARNRFilter(sectionNoise, sectionInter, sectionMotion float64) int {
	sectionZeroMV := sectionInter - sectionMotion
	adjust := 0
	if sectionNoise < 150 {
		adjust -= 1
		if sectionNoise < 75 {
			adjust -= 1
		}
	} else if sectionNoise > 250 {
		adjust += 1
	}
	if sectionZeroMV > 0.50 {
		adjust += 1
	}
	return adjust
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
