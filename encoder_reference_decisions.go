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
	if e == nil {
		return [vp8common.MaxRefFrames]bool{}
	}
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

func (e *VP8Encoder) shouldRecodeInterAttemptAsKeyFrame(required int, refreshGoldenFrame bool, temporalEnabled bool, invisible bool) (int, bool) {
	if e == nil ||
		!e.opts.AdaptiveKeyFrames ||
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
	last = flags&EncodeNoReferenceLast == 0
	golden = flags&EncodeNoReferenceGolden == 0
	alt = flags&EncodeNoReferenceAltRef == 0
	if e == nil {
		return last, golden, alt
	}
	if e.goldenRefAliasesLast {
		golden = false
	}
	if e.altRefAliasesLast || e.goldenRefAliasesAlt {
		alt = false
	}
	return last, golden, alt
}

func (e *VP8Encoder) shouldEncodeKeyFrame(flags EncodeFlags) bool {
	if e.frameCount == 0 || e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	if !e.anyInterReferenceAvailable(flags) {
		return true
	}
	if e.opts.KeyFrameInterval > 0 && e.frameCount%uint64(e.opts.KeyFrameInterval) == 0 {
		return true
	}
	return false
}

func (e *VP8Encoder) forceKeyFrameRequested(flags EncodeFlags) bool {
	if e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	return !e.anyInterReferenceAvailable(flags)
}

func (e *VP8Encoder) shouldRefreshGoldenFrameCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.rc.mode != RateControlCBR ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateGolden) != 0 {
		return false
	}
	if required := rows * cols; required <= 0 || e.lastInterZeroMVCount <= required/2 {
		return false
	}
	interval := e.goldenFrameCBRInterval(rows, cols)
	return interval > 0 && e.rc.framesSinceKeyframe > 0 && e.rc.framesSinceKeyframe%interval == 0
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
// covers VBR and CQ. Returns true when libvpx would force a GF
// refresh on this frame.
func (e *VP8Encoder) shouldRefreshGoldenFrameOnePassNonCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.rc.mode == RateControlCBR ||
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
	refreshCount := cyclicRefreshMaxMBsPerFrame(rows, cols)
	if refreshCount > 0 {
		interval = (2 * rows * cols) / refreshCount
	}
	if interval < 6 {
		return 6
	}
	if interval > 40 {
		return 40
	}
	return interval
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
		return e.goldenFrameCBRInterval(rows, cols)
	}
	if e.twoPass.enabled() && e.twoPass.gfGroupValid && e.twoPass.framesTillGFUpdate > 0 {
		return e.twoPass.framesTillGFUpdate
	}
	return libvpxDefaultGFInterval
}
