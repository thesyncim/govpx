package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) resetGoldenFrameStats() {
	e.framesSinceGolden = 0
	if !e.sourceAltRefPending {
		e.sourceAltRefActive = false
	}
	if e.framesTillAltRefFrame > 0 {
		e.framesTillAltRefFrame--
	}
}

// clearAltRefSchedule drops any pending auto-ARF schedule, mirroring the
// libvpx `cpi->source_alt_ref_pending=0; cpi->alt_ref_source=NULL` reset that
// runs from `vp8_create_compressor` and on explicit reconfiguration paths
// (encoder init, Reset(), error-resilient enable). It is the lifecycle
// counterpart to `resetGoldenFrameStats`, which is the libvpx-faithful
// per-keyframe stats update and intentionally preserves the schedule.
func (e *VP8Encoder) clearAltRefSchedule() {
	e.sourceAltRefPending = false
	e.altRefSourceValid = false
	e.framesTillAltRefFrame = 0
}

// scheduleAltRefSource ports the libvpx
// vp8/encoder/onyx_if.c automatic ARF scheduling decision: when an ARF
// is pending and the lookahead has the future source available, the
// encoder marks the lookahead entry as the alt_ref_source and arms the
// hidden-frame insertion. This helper just records the schedule; the
// actual hidden-frame encode path is a follow-up.
func (e *VP8Encoder) scheduleAltRefSource(altRefSourcePTS uint64, framesTillUpdate int) {
	e.sourceAltRefPending = true
	e.altRefSourcePTS = altRefSourcePTS
	e.altRefSourceValid = true
	e.framesTillAltRefFrame = framesTillUpdate
}

// isSrcFrameAltRef ports the libvpx is_src_frame_alt_ref check:
// after popping a lookahead entry, the encoder marks it as the ARF
// source frame if its PTS matches the previously scheduled
// alt_ref_source. The check is gated on altRefSourceValid because
// scheduleAltRefSource has not yet been called for the first ARF
// section (libvpx's `cpi->alt_ref_source != NULL` guard).
func (e *VP8Encoder) isSrcFrameAltRef(framePTS uint64) bool {
	return e.altRefSourceValid && framePTS == e.altRefSourcePTS
}

func (e *VP8Encoder) interFrameSignBias() [vp8common.MaxRefFrames]bool {
	signBias := [vp8common.MaxRefFrames]bool{}
	signBias[vp8common.AltRefFrame] = e.sourceAltRefActive
	return signBias
}

func interFrameStateConfigSignBias(cfg vp8enc.InterFrameStateConfig) [vp8common.MaxRefFrames]bool {
	return [vp8common.MaxRefFrames]bool{
		vp8common.GoldenFrame: cfg.GoldenSignBias,
		vp8common.AltRefFrame: cfg.AltRefSignBias,
	}
}

func interFrameDroppable(cfg vp8enc.InterFrameStateConfig) bool {
	if cfg.RefreshLast || cfg.RefreshGolden || cfg.RefreshAltRef ||
		cfg.CopyBufferToGolden != 0 || cfg.CopyBufferToAltRef != 0 ||
		cfg.RefreshEntropyProbs {
		return false
	}
	if cfg.Segmentation.Enabled && (cfg.Segmentation.UpdateMap || cfg.Segmentation.UpdateData) {
		return false
	}
	return true
}

func (e *VP8Encoder) matchingZeroInterFrameReference(source vp8enc.SourceImage, flags EncodeFlags) (vp8common.MVReferenceFrame, *vp8common.Image, bool) {
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	if lastEnabled && sourceImageMatchesReference(source, &e.lastRef.Img) {
		return vp8common.LastFrame, &e.lastRef.Img, true
	}
	if goldenEnabled && sourceImageMatchesReference(source, &e.goldenRef.Img) {
		return vp8common.GoldenFrame, &e.goldenRef.Img, true
	}
	if altEnabled && sourceImageMatchesReference(source, &e.altRef.Img) {
		return vp8common.AltRefFrame, &e.altRef.Img, true
	}
	return vp8common.IntraFrame, nil, false
}

func fillZeroInterFrameModes(modes []vp8enc.InterFrameMacroblockMode, refFrame vp8common.MVReferenceFrame) {
	for i := range modes {
		modes[i] = vp8enc.InterFrameMacroblockMode{
			MBSkipCoeff: true,
			RefFrame:    refFrame,
			Mode:        vp8common.ZeroMV,
		}
	}
}

func countLastZeroMVInterFrameModes(modes []vp8enc.InterFrameMacroblockMode) int {
	count := 0
	for _, mode := range modes {
		if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
			count++
		}
	}
	return count
}

func countSkippedInterFrameModes(modes []vp8enc.InterFrameMacroblockMode) int {
	count := 0
	for _, mode := range modes {
		if mode.MBSkipCoeff {
			count++
		}
	}
	return count
}

// countInterFrameRefUsage mirrors libvpx's count_mb_ref_frame_usage: a
// per-MB tally of which reference each MB selected (intra, last, golden,
// altref). The four return values match the libvpx INTRA/LAST/GOLDEN/ALTREF
// indexing of cpi->mb.count_mb_ref_frame_usage. Modes with intra
// prediction (Mode < NearestMV, i.e. DCPred/VPred/HPred/TMPred/BPred)
// count as INTRA_FRAME in libvpx; otherwise the MB carries an inter ref.
func countInterFrameRefUsage(modes []vp8enc.InterFrameMacroblockMode) (intra, last, golden, alt int) {
	for _, mode := range modes {
		if mode.Mode < vp8common.NearestMV {
			intra++
			continue
		}
		switch mode.RefFrame {
		case vp8common.LastFrame:
			last++
		case vp8common.GoldenFrame:
			golden++
		case vp8common.AltRefFrame:
			alt++
		default:
			intra++
		}
	}
	return intra, last, golden, alt
}

func (e *VP8Encoder) resetGFActiveMap(macroblocks int) {
	if macroblocks <= 0 || len(e.gfActiveMap) == 0 {
		e.rc.gfActiveCount = 0
		return
	}
	if macroblocks > len(e.gfActiveMap) {
		macroblocks = len(e.gfActiveMap)
	}
	for i := 0; i < macroblocks; i++ {
		e.gfActiveMap[i] = true
	}
	for i := macroblocks; i < len(e.gfActiveMap); i++ {
		e.gfActiveMap[i] = false
	}
	e.rc.gfActiveCount = macroblocks
}

func (e *VP8Encoder) updateGFActiveMap(refreshGolden bool, modes []vp8enc.InterFrameMacroblockMode) {
	if e.libvpxTemporalLayerCount() > 1 {
		return
	}
	if refreshGolden {
		e.resetGFActiveMap(len(modes))
		return
	}
	if len(modes) == 0 || len(e.gfActiveMap) == 0 {
		e.rc.gfActiveCount = 0
		return
	}
	limit := len(modes)
	if limit > len(e.gfActiveMap) {
		limit = len(e.gfActiveMap)
	}
	active := 0
	for i := 0; i < limit; i++ {
		mode := modes[i]
		isActive := e.gfActiveMap[i]
		if mode.RefFrame == vp8common.GoldenFrame || mode.RefFrame == vp8common.AltRefFrame {
			isActive = true
		} else if mode.Mode != vp8common.ZeroMV {
			isActive = false
		}
		e.gfActiveMap[i] = isActive
		if isActive {
			active++
		}
	}
	for i := limit; i < len(e.gfActiveMap); i++ {
		e.gfActiveMap[i] = false
	}
	e.rc.gfActiveCount = active
}

func (e *VP8Encoder) shouldRecodeInterAttemptAsKeyFrame(required int, refreshGoldenFrame bool, temporalEnabled bool, invisible bool) (int, bool) {
	if !e.opts.AdaptiveKeyFrames ||
		e.twoPass.enabled() ||
		temporalEnabled ||
		invisible ||
		e.interAnalysisCompressorSpeed() == 2 ||
		required <= 0 ||
		len(e.interFrameModes) < required {
		return 0, false
	}
	intra, _, _, _ := countInterFrameRefUsage(e.interFrameModes[:required])
	thisFramePercentIntra := (100 * intra) / required
	return thisFramePercentIntra, libvpxDecideKeyFrame(thisFramePercentIntra, e.lastFramePercentIntra, refreshGoldenFrame)
}

func validateEncodeFlags(flags EncodeFlags) error {
	if flags&EncodeForceGoldenFrame != 0 && flags&EncodeNoUpdateGolden != 0 {
		return ErrInvalidConfig
	}
	if flags&EncodeForceAltRefFrame != 0 && flags&EncodeNoUpdateAltRef != 0 {
		return ErrInvalidConfig
	}
	return nil
}

func boostedReferenceRateControlFrame(goldenCBRRefresh bool, flags EncodeFlags) bool {
	return goldenCBRRefresh || flags&(EncodeForceGoldenFrame|EncodeForceAltRefFrame) != 0
}

// externalRefreshFlagsPending mirrors libvpx
// vp8/vp8_cx_iface.c:vp8e_set_frame_flags: whenever the user sets any of
// VP8_EFLAG_NO_UPD_LAST / VP8_EFLAG_NO_UPD_GF / VP8_EFLAG_NO_UPD_ARF /
// VP8_EFLAG_FORCE_GF / VP8_EFLAG_FORCE_ARF on a frame, libvpx routes
// the request through vp8_update_reference which arms
// cpi->ext_refresh_frame_flags_pending and rewrites the three
// cpi->common.refresh_*_frame fields from an explicit "update" mask
// rather than the encoder-internal defaults. The mask starts at 7 (all
// three buffers refreshed) and is XOR'd by each VP8_EFLAG_NO_UPD_*
// bit, so e.g. NO_UPD_LAST alone yields {LAST=0, GOLDEN=1, ALTREF=1} —
// surprising at first read but the libvpx-documented behaviour every
// downstream consumer (WebRTC, vpx_temporal_svc_encoder) relies on.
func externalRefreshFlagsPending(flags EncodeFlags) bool {
	return flags&(EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef|EncodeForceGoldenFrame|EncodeForceAltRefFrame) != 0
}

// libvpxExternalRefreshMask returns the (RefreshLast, RefreshGolden,
// RefreshAltRef) tuple produced by the libvpx vp8_update_reference
// mask. Callers must only invoke this when externalRefreshFlagsPending
// returns true; the libvpx default (no user flags) is encoded
// elsewhere because it depends on encoder-internal state
// (goldenCBRRefresh, temporal SVC scoreboard, etc.).
func libvpxExternalRefreshMask(flags EncodeFlags) (refreshLast bool, refreshGolden bool, refreshAltRef bool) {
	const (
		vp8LastFrame  = 1
		vp8GoldFrame  = 2
		vp8AltrFrame  = 4
		allRefreshSet = vp8LastFrame | vp8GoldFrame | vp8AltrFrame
	)
	upd := allRefreshSet
	if flags&EncodeNoUpdateLast != 0 {
		upd ^= vp8LastFrame
	}
	if flags&EncodeNoUpdateGolden != 0 {
		upd ^= vp8GoldFrame
	}
	if flags&EncodeNoUpdateAltRef != 0 {
		upd ^= vp8AltrFrame
	}
	refreshLast = upd&vp8LastFrame != 0
	refreshGolden = upd&vp8GoldFrame != 0
	refreshAltRef = upd&vp8AltrFrame != 0
	return refreshLast, refreshGolden, refreshAltRef
}

func (e *VP8Encoder) armExternalRefreshMask(flags EncodeFlags) {
	if e == nil || !externalRefreshFlagsPending(flags) {
		return
	}
	e.carriedExternalRefreshLast, e.carriedExternalRefreshGolden, e.carriedExternalRefreshAltRef = libvpxExternalRefreshMask(flags)
	e.carriedExternalRefresh = true
}

func (e *VP8Encoder) currentExternalRefreshMask() (refreshLast bool, refreshGolden bool, refreshAltRef bool, ok bool) {
	if e == nil || !e.carriedExternalRefresh {
		return false, false, false, false
	}
	return e.carriedExternalRefreshLast, e.carriedExternalRefreshGolden, e.carriedExternalRefreshAltRef, true
}

func (e *VP8Encoder) clearExternalRefreshMaskAfterPacket() {
	if e == nil {
		return
	}
	e.carriedExternalRefresh = false
	e.carriedExternalRefreshLast = false
	e.carriedExternalRefreshGolden = false
	e.carriedExternalRefreshAltRef = false
}

func externalReferenceFlagsPending(flags EncodeFlags) bool {
	return flags&(EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef) != 0
}

func libvpxExternalReferenceMask(flags EncodeFlags) (last bool, golden bool, alt bool) {
	last = flags&EncodeNoReferenceLast == 0
	golden = flags&EncodeNoReferenceGolden == 0
	alt = flags&EncodeNoReferenceAltRef == 0
	return last, golden, alt
}

func (e *VP8Encoder) armExternalReferenceMask(flags EncodeFlags) {
	if e == nil || !externalReferenceFlagsPending(flags) {
		return
	}
	e.carriedExternalReferenceLast, e.carriedExternalReferenceGolden, e.carriedExternalReferenceAltRef = libvpxExternalReferenceMask(flags)
	e.carriedExternalReference = true
}

func (e *VP8Encoder) currentExternalReferenceMask() (last bool, golden bool, alt bool, ok bool) {
	if e == nil || !e.carriedExternalReference {
		return false, false, false, false
	}
	return e.carriedExternalReferenceLast, e.carriedExternalReferenceGolden, e.carriedExternalReferenceAltRef, true
}

func (e *VP8Encoder) clearExternalReferenceMaskAfterPacket() {
	if e == nil {
		return
	}
	e.carriedExternalReference = false
	e.carriedExternalReferenceLast = false
	e.carriedExternalReferenceGolden = false
	e.carriedExternalReferenceAltRef = false
}

func shouldCopyOldGoldenToAltRefOnGoldenRefresh(errorResilient bool, goldenCBRRefresh bool, flags EncodeFlags) bool {
	if errorResilient || !goldenCBRRefresh {
		return false
	}
	return flags&(EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef|EncodeForceGoldenFrame|EncodeForceAltRefFrame) == 0
}

// suppressInterFrameCopyBuffersOnAltRefEdges enforces libvpx
// onyx_if.c update_reference_frames invariants for the ARF edge cases:
// hidden ARF frames assert `!cm->copy_buffer_to_arf` (the ARF buffer
// is populated by the frame itself), and the deferred show frame
// after a hidden ARF (is_src_frame_alt_ref) leaves both
// copy_buffer_to_arf and copy_buffer_to_gf at their zero default
// because the references are already correctly populated.
func suppressInterFrameCopyBuffersOnAltRefEdges(cfg *vp8enc.InterFrameStateConfig, isSrcFrameAltRef bool) {
	if cfg == nil {
		return
	}
	if cfg.RefreshAltRef {
		cfg.CopyBufferToAltRef = 0
	}
	if isSrcFrameAltRef {
		cfg.CopyBufferToAltRef = 0
		cfg.CopyBufferToGolden = 0
	}
}

func (e *VP8Encoder) anyInterReferenceAvailable(flags EncodeFlags) bool {
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	return lastEnabled || goldenEnabled || altEnabled
}

func (e *VP8Encoder) interReferenceAvailability(flags EncodeFlags) (last bool, golden bool, alt bool) {
	if last, golden, alt, ok := e.currentExternalReferenceMask(); ok {
		return last, golden, alt
	}
	last = flags&EncodeNoReferenceLast == 0
	golden = flags&EncodeNoReferenceGolden == 0
	alt = flags&EncodeNoReferenceAltRef == 0
	// libvpx routes explicit VP8_EFLAG_NO_REF_* user masks through
	// vp8_use_as_reference, which overwrites cpi->ref_frame_flags with the
	// user-derived mask and bypasses the gold_is_last / alt_is_last /
	// gold_is_alt alias filters from update_reference_frames. Temporal-SVC
	// layer flags rely on that: a post-keyframe L1 frame may intentionally
	// allow LAST and GOLDEN even though both buffers still alias the keyframe.
	if externalReferenceFlagsPending(flags) {
		return last, golden, alt
	}
	// Reference-alias deduplication: when two reference buffers hold
	// the same pixel data the picker only needs to consider the
	// primary one. The primary is LAST when reachable; otherwise we
	// fall through to GOLDEN as the surviving slot. Mirrors libvpx's
	// vp8/encoder/onyx_if.c picker behaviour for the NO_REF_LAST
	// boundary right after a keyframe: cpi->ref_frame_flags masks LAST
	// out but GOLDEN/ALTREF (which point at the just-refreshed KF
	// reconstruction) remain valid picker candidates, so libvpx codes
	// an inter frame whose MBs naturally fall back to zero-MV against
	// the KF-aliased GOLDEN. Disabling the aliased slot unconditionally
	// here would mask every reference and push govpx onto the
	// keyframe-promotion path, which diverges from libvpx.
	if e.goldenRefAliasesLast && last {
		golden = false
	}
	if (e.altRefAliasesLast && last) || (e.goldenRefAliasesAlt && golden) {
		alt = false
	}
	return last, golden, alt
}

func (e *VP8Encoder) shouldEncodeKeyFrame(flags EncodeFlags) bool {
	if e.frameCount == 0 || e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	if e.opts.KeyFrameInterval <= 0 {
		return false
	}
	if e.opts.AdaptiveKeyFrames {
		keyFrameFrequency := e.keyFrameFrequency
		if keyFrameFrequency <= 0 {
			return false
		}
		framesSinceKey := e.rc.framesSinceKeyframe + 1
		return framesSinceKey%keyFrameFrequency == 0
	}
	return e.rc.framesSinceKeyframe+1 >= e.opts.KeyFrameInterval
}

func (e *VP8Encoder) forceKeyFrameRequested(flags EncodeFlags) bool {
	if e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	return false
}

func (e *VP8Encoder) goldenFrameCBROpportunity(keyFrame bool, temporalActive bool, flags EncodeFlags) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.rc.mode != RateControlCBR ||
		e.rc.onePassAutoGold ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateGolden) != 0 {
		return false
	}
	return e.rc.framesTillGFUpdateDue == 0
}

func (e *VP8Encoder) shouldRefreshGoldenFrameCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if !e.goldenFrameCBROpportunity(keyFrame, temporalActive, flags) {
		return false
	}
	if required := rows * cols; required <= 0 || e.lastInterZeroMVCount <= required/2 {
		return false
	}
	return e.goldenFrameCBRInterval(rows, cols) > 0
}

// shouldRefreshGoldenFrameOnePassNonCBR ports the libvpx auto_gold
// one-pass non-CBR GF refresh trigger from
// vp8/encoder/ratectrl.c calc_pframe_target_size:
//
//	if (cpi->oxcf.error_resilient_mode == 0 &&
//	    (cpi->frames_till_gf_update_due == 0) && !cpi->drop_frame) {
//	    if (!cpi->gf_update_onepass_cbr) {
//	        ... compute gf_frame_usage ...
//	        if (cpi->auto_gold) {
//	            if ((cpi->pass == 0) &&
//	                (cpi->this_frame_percent_intra < 15 ||
//	                 gf_frame_usage >= 5)) {
//	                cpi->common.refresh_golden_frame = 1;
//	            }
//	        }
//	    }
//	}
//
// govpx routes CBR through `shouldRefreshGoldenFrameCBR`; this method
// covers the one-pass auto-golden path seeded at compressor creation.
// Returns true when libvpx would force a GF refresh on this frame.
func (e *VP8Encoder) shouldRefreshGoldenFrameOnePassNonCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.twoPass.enabled() ||
		!e.rc.onePassAutoGold ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateGolden) != 0 {
		return false
	}
	if e.rc.framesTillGFUpdateDue > 0 {
		return false
	}
	required := rows * cols
	if required <= 0 {
		return false
	}
	return libvpxAutoGoldOnePassRefreshDecision(
		e.rc.thisFramePercentIntra,
		e.rc.recentRefFrameUsageIntra,
		e.rc.recentRefFrameUsageLast,
		e.rc.recentRefFrameUsageGolden,
		e.rc.recentRefFrameUsageAltRef,
		e.rc.gfActiveCount,
		required,
	)
}

func (e *VP8Encoder) goldenFrameCBRInterval(rows int, cols int) int {
	interval := 10
	if refreshCount := e.cyclicRefreshMaxMBsPerFrame(rows, cols); refreshCount > 0 {
		interval = (2 * rows * cols) / refreshCount
	}
	return min(max(interval, 6), 40)
}

func (e *VP8Encoder) libvpxMaxGFInterval() int {
	maxInterval := int(outputFrameRate(e.timing)/2.0) + 2
	if maxInterval < 12 {
		maxInterval = 12
	}
	if staticSceneMax := e.opts.KeyFrameInterval >> 1; staticSceneMax > 0 && maxInterval > staticSceneMax {
		maxInterval = staticSceneMax
	}
	// libvpx vp8/encoder/onyx_if.c vp8_new_framerate applies the
	// `play_alternate && lag_in_frames` cap to `cpi->max_gf_interval`, but
	// vp8/vp8_cx_iface.c set_vp8e_config forces `oxcf->lag_in_frames = 0`
	// when `g_pass == VPX_RC_ONE_PASS`. The result: in one-pass mode the
	// lag cap is never applied even when the application requested
	// `--lag-in-frames=4`, so `max_gf_interval` retains the framerate /
	// static-scene cap (e.g. 17 at 30fps). Only the two-pass setup keeps
	// the user-visible lag_in_frames available to `vp8_new_framerate`, so
	// mirror that gating here.
	if e.twoPass.enabled() && e.opts.AutoAltRef && e.opts.LookaheadFrames > 0 {
		lagCap := e.opts.LookaheadFrames - 1
		if maxInterval > lagCap {
			maxInterval = lagCap
		}
	}
	return maxInterval
}

func (e *VP8Encoder) libvpxStaticSceneMaxGFInterval() int {
	staticSceneMax := e.opts.KeyFrameInterval >> 1
	if e.twoPass.enabled() && e.opts.AutoAltRef && e.opts.LookaheadFrames > 0 {
		lagCap := e.opts.LookaheadFrames - 1
		if staticSceneMax > lagCap {
			staticSceneMax = lagCap
		}
	}
	return staticSceneMax
}

// libvpxKeyFrameSetupGFInterval returns the value libvpx's vp8_setup_key_frame
// would assign to cpi->frames_till_gf_update_due (== baseline_gf_interval) at
// the time the next key frame is being encoded.
//
// One-pass: libvpx onyx_if.c vp8_create_compressor sets baseline_gf_interval =
// gf_interval_onepass_cbr for any (Mode <= 2 && CBR && !error_resilient)
// compressor (line ~1886); vp8_change_config later resets baseline_gf_interval
// to DEFAULT_GF_INTERVAL for non-realtime modes (line ~1542) and only re-arms
// the gf_interval_onepass_cbr value for realtime CBR (line ~1547). vpxenc
// invokes vp8_change_config after vp8_create_compressor, so the effective
// value at first-keyframe time is:
//   - realtime CBR: gf_interval_onepass_cbr (cyclic-refresh derived, [6,40])
//   - good/best quality CBR: DEFAULT_GF_INTERVAL == 7
//   - non-CBR (one-pass): DEFAULT_GF_INTERVAL == 7
//
// Two-pass: libvpx vp8/encoder/firstpass.c find_next_key_frame zeroes
// frames_till_gf_update_due (line ~2521); define_gf_group then runs and
// derives baseline_gf_interval from the per-frame motion stats walk
// (line ~1860/1906/1910). calc_iframe_target_size finally seeds
// frames_till_gf_update_due = baseline_gf_interval (vp8/encoder/ratectrl.c
// line ~513). Govpx's twoPassState mirrors that derivation: prepareKFGroup
// + defineGFGroup populate t.framesTillGFUpdate with the two-pass-derived
// baseline_gf_interval before this function is consulted. Returning the
// libvpx-derived value here (instead of the one-pass DEFAULT_GF_INTERVAL
// fallback) avoids a spurious mid-section GF refresh at frame
// DEFAULT_GF_INTERVAL when libvpx's section runs longer.
func (e *VP8Encoder) libvpxKeyFrameSetupGFInterval(rows int, cols int) int {
	if e.opts.Deadline == DeadlineRealtime && e.rc.mode == RateControlCBR && !e.opts.ErrorResilient {
		if e.rc.onePassAutoGold {
			return 1
		}
		return e.goldenFrameCBRInterval(rows, cols)
	}
	if e.twoPass.enabled() && e.twoPass.gfGroupValid && e.twoPass.framesTillGFUpdate > 0 {
		return e.twoPass.framesTillGFUpdate
	}
	return libvpxDefaultGFInterval
}
