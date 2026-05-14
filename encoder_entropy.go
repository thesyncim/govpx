package govpx

import (
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func (e *VP8Encoder) commitKeyFrameEntropy(attempt keyFrameEncodeAttempt) {
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	e.resetMotionVectorCostTablesFromModeProbs()
	if attempt.RefreshEntropyProbs {
		e.coefProbs = attempt.FrameCoefProbs
	}
	// Mirror libvpx vp8/encoder/ratectrl.c vp8_setup_key_frame and the
	// end-of-frame lfc_X saves in onyx_if.c: setup seeds lfc_a/lfc_g/lfc_n
	// from the default coef probs before the keyframe encode adapts cm->fc.
	// The final save only refreshes the LAST entropy context on a keyframe
	// (`cm->refresh_last_frame = 1`); GOLDEN and ALTREF remain at the default
	// seed until a later visible frame actually refreshes those references.
	e.coefProbsLast = e.coefProbs
	e.coefProbsGolden = vp8tables.DefaultCoefProbs
	e.coefProbsAltRef = vp8tables.DefaultCoefProbs
	e.coefProbsSnapshotsValid = true
	e.updateRefFrameProbsFromKeyFrame()
	// Mirror libvpx vp8/encoder/bitstream.c pack_lf_deltas: after a frame
	// is packed, last_*_lf_deltas mirror the just-signaled deltas so the
	// next frame's send_update bit reflects whether anything actually
	// changed. The keyframe is the first packed frame in a clip, so this
	// is also where lfDeltasSignaledOnce flips to true.
	e.updateLastSignaledLFDeltas(attempt.LFDeltaEnabled, attempt.RefLFDeltas, attempt.ModeLFDeltas)
}

// updateRefFrameProbsFromKeyFrame mirrors the key-frame branch of
// libvpx update_rd_ref_frame_probs.
func (e *VP8Encoder) updateRefFrameProbsFromKeyFrame() {
	e.refProbIntra = 255
	e.refProbLast = 128
	e.refProbGolden = 128
	e.refProbUseDefaultOnNextInterRD = true
	if !e.opts.TemporalScalability.Enabled {
		e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	}
}

func (e *VP8Encoder) commitInterFrameAttempt(attempt interFrameEncodeAttempt) {
	e.commitInterFrameEntropy(attempt)
	e.commitInterFrameSkipFalseProb(attempt)
	e.updateRefFrameProbsFromPackedAttempt()
	// Mirror libvpx vp8/encoder/bitstream.c pack_lf_deltas: after a frame
	// is packed, last_*_lf_deltas mirror the just-signaled deltas so the
	// next frame's send_update bit reflects whether anything actually
	// changed. We snapshot the accepted attempt's deltas to match.
	e.updateLastSignaledLFDeltas(attempt.Config.LFDeltaEnabled, attempt.Config.RefLFDeltas, attempt.Config.ModeLFDeltas)
	// Track libvpx update_golden_frame_stats / update_alt_ref_frame_stats
	// counters used by applyLibvpxRdRefFrameProbRefreshAdjustments next frame.
	//
	// libvpx vp8/encoder/onyx_if.c encode_frame_to_data_rate gates BOTH
	// branches (update_alt_ref_frame_stats and update_golden_frame_stats) on
	// `if (!cpi->oxcf.error_resilient_mode)` at line 4724. When either
	// VPX_ERROR_RESILIENT_DEFAULT or VPX_ERROR_RESILIENT_PARTITIONS is set,
	// neither function runs, so `cpi->frames_since_golden` is frozen at 0 for
	// the entire clip (it is zero-initialized by vp8_create_compressor and
	// never reset). The next frame's update_rd_ref_frame_probs therefore
	// takes the `frames_since_golden == 0` branch on every inter frame and
	// forces prob_last_coded = 214 in the picker's vp8_calc_ref_frame_costs
	// dispatch. Without this gate, govpx incremented framesSinceGolden every
	// inter frame and the picker saw the post-rfct-derived prob_last_coded
	// (typically much smaller than 214 once LAST dominated), which biased
	// the ref_frame_cost in favor of GOLDEN on knife-edge mb decisions and
	// surfaced as a frame-3 1-byte first_partition diff on the
	// realtime-cbr-cpu-3-64x64-error-resilient3 panning fixture.
	if !e.opts.ErrorResilient && !e.opts.ErrorResilientPartitions {
		e.updateGoldenFrameStats(attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef)
	}
	if attempt.ZeroReference {
		e.refreshZeroInterFrameReferences(attempt.Config, attempt.Ref, attempt.RefFrame)
	} else {
		e.refreshInterFrameReferencesFromAnalysis(attempt.Config)
	}
	// Mirror libvpx onyx_if.c update_reference_frames denoiser branch: copy
	// the denoised running_avg[INTRA] into LAST/GOLDEN/ALTREF running_avg
	// buffers per the frame's refresh/copy policy.
	e.copyDenoiserAvgForRefresh(attempt.Config)
	e.rememberLastFrameInterModes(interFrameStateConfigSignBias(attempt.Config))
	// Once an inter frame has been encoded under the post-drop max-Q gate,
	// clear it; libvpx leaves force_maxqp set only until the next frame
	// consumes it.
	e.forceMaxQuantizer = false
}

// updateRefFrameProbsFromAttempt mirrors the gated vp8_convert_rfct_to_prob
// call at the end of libvpx vp8_encode_frame. This path feeds recode/drop
// decisions before packet packing; single-layer GF/ARF refresh frames skip it.
func (e *VP8Encoder) updateRefFrameProbsFromAttempt(attempt interFrameEncodeAttempt) {
	if e.refProbUseDefaultOnNextInterRD {
		e.resetRefFrameProbsToDefaultInterRD()
	}
	e.refProbUseDefaultOnNextInterRD = false
	if !libvpxShouldConvertRefCountsToProb(e.libvpxTemporalLayerCount(), attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef) {
		return
	}
	e.convertRefFrameCountsToProbs()
}

func (e *VP8Encoder) updateRDRefFrameProbsForDroppedFrame(refreshAltRef bool) {
	if e.shouldResetRDRefFrameProbsToDefaultInterRD() {
		e.resetRefFrameProbsToDefaultInterRD()
	}
	e.refProbUseDefaultOnNextInterRD = false
	if !e.opts.TemporalScalability.Enabled {
		e.applyLibvpxRdRefFrameProbRefreshAdjustments(refreshAltRef)
	}
}

func (e *VP8Encoder) shouldResetRDRefFrameProbsToDefaultInterRD() bool {
	if e.refProbUseDefaultOnNextInterRD {
		return true
	}
	if !e.opts.TemporalScalability.Enabled {
		return false
	}
	for _, count := range e.temporalLayerRefUsage {
		if count != 0 {
			return false
		}
	}
	return true
}

func (e *VP8Encoder) snapshotDroppedFrameCoefProbs(refreshLast bool, refreshGolden bool, refreshAltRef bool) {
	if !e.coefProbsSnapshotsValid {
		e.coefProbsLast = e.coefProbs
		e.coefProbsGolden = e.coefProbs
		e.coefProbsAltRef = e.coefProbs
		e.coefProbsSnapshotsValid = true
	}
	if refreshAltRef {
		e.coefProbsAltRef = e.coefProbs
	}
	if refreshGolden {
		e.coefProbsGolden = e.coefProbs
	}
	if refreshLast {
		e.coefProbsLast = e.coefProbs
	}
}

// updateRefFrameProbsFromPackedAttempt mirrors libvpx bitstream.c
// pack_inter_mode_mvs, which unconditionally calls vp8_convert_rfct_to_prob
// immediately before writing the inter-mode header. The converted values stay
// live after packet write and seed the next frame's update_rd_ref_frame_probs,
// including after single-layer GF/ARF refresh packets.
func (e *VP8Encoder) updateRefFrameProbsFromPackedAttempt() {
	e.refProbUseDefaultOnNextInterRD = false
	e.convertRefFrameCountsToProbs()
}

func (e *VP8Encoder) convertRefFrameCountsToProbs() {
	intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes)
	probIntra, probLast, probGolden, ok := refFrameProbsFromUsage(intra, last, golden, alt)
	if !ok {
		return
	}
	e.refProbIntra = probIntra
	e.refProbLast = probLast
	e.refProbGolden = probGolden
}

func refFrameProbsFromUsage(intra int, last int, golden int, alt int) (probIntra uint8, probLast uint8, probGolden uint8, ok bool) {
	rfInter := last + golden + alt
	total := intra + rfInter
	if total == 0 {
		return 0, 0, 0, false
	}
	newIntra := intra * 255 / total
	if newIntra == 0 {
		newIntra = 1
	}
	newLast := 128
	if rfInter > 0 {
		newLast = last * 255 / rfInter
		if newLast == 0 {
			newLast = 1
		}
	}
	newGarf := 128
	if golden+alt > 0 {
		newGarf = golden * 255 / (golden + alt)
		if newGarf == 0 {
			newGarf = 1
		}
	}
	return uint8(newIntra), uint8(newLast), uint8(newGarf), true
}

func (e *VP8Encoder) resetRefFrameProbsToDefaultInterRD() {
	e.refProbIntra = 63
	e.refProbLast = 128
	e.refProbGolden = 128
}

func libvpxShouldConvertRefCountsToProb(temporalLayerCount int, refreshGolden bool, refreshAltRef bool) bool {
	return temporalLayerCount > 1 || (!refreshGolden && !refreshAltRef)
}

// pickerCoefProbs returns the coefficient prob table the inter-frame RD picker
// should feed into rate estimation. When the per-reference snapshot stack is
// valid AND a picker pass is active (rdPickerCoefProbsActive set by
// encodeInterFrameAttempt), returns that snapshot; otherwise falls back to
// the live encoder coefProbs (used for key frames, committed-encode paths,
// and pre-snapshot transient state).
func (e *VP8Encoder) pickerCoefProbs() *vp8tables.CoefficientProbs {
	if e.rdPickerCoefProbsActive != nil {
		return e.rdPickerCoefProbsActive
	}
	return &e.coefProbs
}

// rdPickerCoefProbs returns the coefficient-prob table the inter-frame RD
// picker should feed into fill_token_costs (the rate side of every
// coefficientBlockTokenRate call inside the picker), mirroring libvpx
// vp8/encoder/rdopt.c vp8_initialize_rd_consts for single-layer encodes:
//
//	l = refresh_alt_ref_frame ? &cpi->lfc_a
//	  : refresh_golden_frame  ? &cpi->lfc_g
//	  : &cpi->lfc_n
//
// Temporal multilayer realtime encodes restore per-layer rate-control state
// before RD setup, but vp8_initialize_rd_consts still selects lfc_a/lfc_g/lfc_n
// directly from the current frame's refresh flags. That means default and
// LAST-refresh temporal frames score against lfc_n, not the live frame context
// left by the previously encoded higher layer.
//
// Returns nil before the first commitKeyFrameEntropy seeds the snapshots
// (which on a keyframe-led clip is impossible to hit on an inter frame), or
// when none of the per-reference snapshots have been valid yet — in which
// case the caller falls back to e.coefProbs.
func (e *VP8Encoder) rdPickerCoefProbs(refreshGolden, refreshAltRef bool) *vp8tables.CoefficientProbs {
	if !e.coefProbsSnapshotsValid {
		return nil
	}
	switch {
	case refreshAltRef:
		return &e.coefProbsAltRef
	case refreshGolden:
		return &e.coefProbsGolden
	default:
		return &e.coefProbsLast
	}
}

func (e *VP8Encoder) commitInterFrameEntropy(attempt interFrameEncodeAttempt) {
	e.updateMotionVectorCostTablesFromInterAttempt(attempt)
	// Mirror libvpx onyx_if.c encode_frame_to_data_rate
	// `if (refresh_entropy_probs == 0) cm->fc = cm->lfc;` rollback: when the
	// bitstream did NOT carry a refresh, e.coefProbs already reflects the
	// pre-frame snapshot, so only the refresh=true branch commits new probs.
	if attempt.Config.RefreshEntropyProbs {
		e.coefProbs = attempt.FrameCoefProbs
		e.modeProbs.YMode = attempt.FrameYModeProbs
		e.modeProbs.UVMode = attempt.FrameUVModeProbs
		e.modeProbs.MV = attempt.FrameMVProbs
	}
	// Mirror libvpx onyx_if.c lines 5151-5157: the per-reference frame-context
	// snapshots are updated independently from each refresh flag, AFTER the
	// (optional) `cm->fc = cm->lfc` rollback above. Together with the keyframe
	// seed in commitKeyFrameEntropy, this gives the RD picker a stable
	// `last refresh of {alt,golden,last}` view of cm->fc to feed
	// fill_token_costs from on the NEXT frame.
	if !e.coefProbsSnapshotsValid {
		e.coefProbsLast = e.coefProbs
		e.coefProbsGolden = e.coefProbs
		e.coefProbsAltRef = e.coefProbs
		e.coefProbsSnapshotsValid = true
	}
	if attempt.Config.RefreshAltRef {
		e.coefProbsAltRef = e.coefProbs
	}
	// libvpx onyx_if.c: `update_golden_frame_stats` (line 2629) sets
	// `cm->refresh_golden_frame = 0` BEFORE the `if (refresh_golden) lfc_g
	// = cm->fc` snapshot at line 5155 runs. update_golden_frame_stats is
	// itself gated on `!error_resilient_mode` at line 4741, so in error
	// resilient encodes the flag survives and lfc_g IS refreshed. govpx
	// mirrors the same gate (see commitInterFrameAttempt's
	// updateGoldenFrameStats call) so the picker's coefProbsGolden snapshot
	// stays frozen at the keyframe-seeded default in non-resilient mode,
	// matching libvpx's lfc_g. Without this gate the SPLITMV picker on the
	// next golden-refresh inter frame fed fill_token_costs the adapted
	// post-keyframe table, which inflated label costs by ~10000 and let
	// LEFT4X4 beat NEW4X4 (see close-splitmv-frame14).
	if attempt.Config.RefreshGolden && (e.opts.ErrorResilient || e.opts.ErrorResilientPartitions) {
		e.coefProbsGolden = e.coefProbs
	}
	if attempt.Config.RefreshLast {
		e.coefProbsLast = e.coefProbs
	}
}

// applyLibvpxRdRefFrameProbRefreshAdjustments ports the refresh-policy
// adjustments in libvpx vp8/encoder/onyx_if.c update_rd_ref_frame_probs that
// bias prob_intra/prob_last/prob_gf for the *current* inter frame's RD scoring.
// The base probabilities themselves are kept fresh by updateRefFrameProbsFromAttempt
// (the equivalent of vp8_convert_rfct_to_prob) at packet write time, so this
// function only stamps the per-frame refresh adjustments on top.
//
// In libvpx these bumps are gated by `oxcf.number_of_layers == 1`; govpx's
// temporal-scalability path runs through interReferenceFrameRatesForFlags
// special cases instead, so the layer guard is enforced by the call site.
func (e *VP8Encoder) applyLibvpxRdRefFrameProbRefreshAdjustments(refreshAltRef bool) {
	if refreshAltRef {
		probIntra := min(int(e.refProbIntra)+40, 255)
		e.refProbIntra = uint8(probIntra)
		e.refProbLast = 200
		e.refProbGolden = 1
	} else if e.framesSinceGolden == 0 {
		e.refProbLast = 214
	} else if e.framesSinceGolden == 1 {
		e.refProbLast = 192
		e.refProbGolden = 220
	} else if e.sourceAltRefActive {
		probGolden := max(int(e.refProbGolden)-20, 10)
		e.refProbGolden = uint8(probGolden)
	}
	if !e.sourceAltRefActive {
		e.refProbGolden = 255
	}
}

// updateGoldenFrameStats tracks libvpx's frames_since_golden /
// source_alt_ref_active counters used by update_rd_ref_frame_probs. It is the
// govpx counterpart to vp8/encoder/onyx_if.c update_golden_frame_stats minus
// the auto-arf bookkeeping that govpx's flag-driven alt-ref does not exercise.
func (e *VP8Encoder) updateGoldenFrameStats(refreshGolden bool, refreshAltRef bool) {
	// libvpx vp8/encoder/onyx_if.c dispatches between update_alt_ref_frame_stats
	// and update_golden_frame_stats at lines 4724-4732:
	//
	//   if (!cpi->oxcf.error_resilient_mode) {
	//       if (cpi->oxcf.play_alternate && cm->refresh_alt_ref_frame &&
	//           (cm->frame_type != KEY_FRAME)) {
	//           update_alt_ref_frame_stats(cpi);
	//       } else {
	//           update_golden_frame_stats(cpi);
	//       }
	//   }
	//
	// `update_alt_ref_frame_stats` is the only path that asserts
	// `cpi->source_alt_ref_active = 1`; when play_alternate (AutoAltRef) is
	// disabled libvpx routes refresh_alt_ref_frame=1 through
	// `update_golden_frame_stats`, whose non-GF branch explicitly skips the
	// plain-inter counter update while refresh_alt_ref_frame is set.
	if refreshAltRef && e.opts.AutoAltRef {
		e.framesSinceGolden = 0
		e.sourceAltRefActive = true
		// libvpx vp8/encoder/onyx_if.c update_alt_ref_frame_stats clears
		// source_alt_ref_pending after the hidden ARF is encoded.
		e.sourceAltRefPending = false
		return
	}
	if refreshGolden {
		e.framesSinceGolden = 0
		// libvpx onyx_if.c: `if (!source_alt_ref_pending)
		// source_alt_ref_active = 0`. Refreshing golden in the absence of
		// a pending alt-ref schedule clears the active alt-ref.
		if !e.sourceAltRefPending {
			e.sourceAltRefActive = false
		}
		return
	}
	if refreshAltRef {
		return
	}
	if e.framesSinceGolden < int(^uint(0)>>1) {
		e.framesSinceGolden++
	}
	// libvpx onyx_if.c counts down frames_till_alt_ref_frame on every
	// non-refresh inter frame; when it hits 0 the encoder consumes the
	// pending ARF on the next frame.
	if e.framesTillAltRefFrame > 0 {
		e.framesTillAltRefFrame--
	}
}

// resetGoldenFrameStats mirrors libvpx vp8/encoder/onyx_if.c
// `update_golden_frame_stats`'s `cm->refresh_golden_frame` branch, which is
// the routine that runs after every keyframe (vp8_setup_key_frame asserts
// `cm->refresh_golden_frame=1`). The libvpx update:
//
//   - frames_since_golden = 0
//   - if (!source_alt_ref_pending) source_alt_ref_active = 0
//   - if (frames_till_gf_update_due > 0) frames_till_gf_update_due--
//
// It leaves `source_alt_ref_pending` and `alt_ref_source` intact so that
// `define_gf_group` can arm a fresh ARF schedule from inside `vp8_second_pass`
// (which runs at the top of Pass2Encode for the keyframe) and have it survive
// the post-encode lifecycle bookkeeping. govpx mirrors that here so a
// pass2-armed ARF schedule produced during keyframe encoding is not clobbered
// before the next frame's `autoAltRefMaybeEncode` reads it.
//
// For full state reset (Reset(), encoder init), use `clearAltRefSchedule` to
// also drop `source_alt_ref_pending`/`altRefSourceValid`/`framesTillAltRefFrame`.
