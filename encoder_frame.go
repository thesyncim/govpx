package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// EncodeInto encodes one input frame into dst. The returned Data slice aliases
// dst and is invalidated by caller reuse of that buffer.
func (e *VP8Encoder) EncodeInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error) {
	if e == nil || e.closed {
		return EncodeResult{}, ErrClosed
	}
	if !src.validForEncode(e.opts.Width, e.opts.Height) {
		return EncodeResult{}, ErrInvalidConfig
	}
	if len(dst) == 0 {
		return EncodeResult{}, ErrBufferTooSmall
	}
	if e.lookaheadEnabled() {
		if result, ok, err := e.autoAltRefMaybeEncode(dst, src, pts, duration, flags); ok {
			return result, err
		}
		result, err := e.encodeLookaheadInto(dst, src, pts, duration, flags)
		if err == nil {
			e.autoAltRefMaybeSchedule()
		}
		return result, err
	}
	return e.encodeSourceInto(dst, sourceImageFromImage(src), pts, duration, flags, encodeSourceMetadata{})
}

// FlushInto drains a lookahead encoder at end of stream. It returns
// ErrFrameNotReady when no queued frame can be emitted.
func (e *VP8Encoder) FlushInto(dst []byte) (EncodeResult, error) {
	if e == nil || e.closed {
		return EncodeResult{}, ErrClosed
	}
	if len(dst) == 0 {
		return EncodeResult{}, ErrBufferTooSmall
	}
	if !e.lookaheadEnabled() {
		return EncodeResult{}, ErrFrameNotReady
	}
	if result, ok, err := e.autoAltRefMaybeEmitHiddenOnFlush(dst); ok {
		return result, err
	}
	if e.lookaheadSize() == 0 {
		return EncodeResult{}, ErrFrameNotReady
	}
	entry, ok := e.popLookahead(true)
	if !ok {
		return EncodeResult{}, ErrFrameNotReady
	}
	meta := encodeSourceMetadata{lookaheadDepth: e.lookaheadSize()}
	result, err := e.encodeSourceInto(dst, sourceImageFromVP8(&entry.frame.Img), entry.pts, entry.duration, entry.flags, meta)
	e.clearPoppedLookahead(entry)
	if err == nil {
		e.autoAltRefMaybeSchedule()
	}
	return result, err
}

func (e *VP8Encoder) encodeSourceInto(dst []byte, source vp8enc.SourceImage, pts uint64, duration uint64, flags EncodeFlags, meta encodeSourceMetadata) (EncodeResult, error) {
	e.currentSourcePTS = pts
	// libvpx vp8/encoder/encodeframe.c:685-691 -- vp8_auto_select_speed runs
	// once at the top of vp8_encode_frame for realtime+positive-cpu_used,
	// evolving cpi->Speed from the prior frame's encode timer.
	e.libvpxAutoSelectSpeed()
	temporalFrame := e.temporal.nextFrame(e.timing)
	flags |= temporalFrame.Flags
	if err := validateEncodeFlags(flags); err != nil {
		return EncodeResult{}, err
	}
	e.currentTemporalLayer = 0
	if temporalFrame.Enabled {
		e.currentTemporalLayer = temporalFrame.LayerID
	}
	e.mbsZeroLastDotSuppress = 0
	forcedKeyFrame := e.forceKeyFrameRequested(flags)
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	required := rows * cols
	preprocessed, preprocessMeta := e.preprocessSource(source, flags, meta)
	source = preprocessed
	keyFrame := e.shouldEncodeKeyFrame(flags)
	sceneCutKeyFrame := false
	twoPassSceneCut := false
	if !keyFrame && e.twoPass.shouldKeyFrame(e.frameCount, e.rc.framesSinceKeyframe, e.opts.KeyFrameInterval) {
		keyFrame = true
		sceneCutKeyFrame = true
		twoPassSceneCut = true
	}
	if !keyFrame && e.shouldEncodeSceneCutKeyFrame(source, flags, temporalFrame.Enabled, rows, cols) {
		keyFrame = true
		sceneCutKeyFrame = true
	}
	temporalReferenceControl := temporalFrame.Enabled && temporalFrame.LayerCount > 1
	goldenCBRRefresh := e.shouldRefreshGoldenFrameCBR(keyFrame, temporalReferenceControl, flags, rows, cols)
	// libvpx auto_gold one-pass non-CBR refresh decision: VBR/CQ
	// triggers GF refresh when frames_till_gf_update_due==0 and
	// pct_intra<15 || gf_frame_usage>=5. govpx funnels it through the
	// same goldenCBRRefresh local so the existing CBR-shaped code path
	// (rc bookkeeping, header copy, and post-encode GF accounting)
	// applies uniformly.
	if !goldenCBRRefresh && e.shouldRefreshGoldenFrameOnePassNonCBR(keyFrame, temporalReferenceControl, flags, rows, cols) {
		goldenCBRRefresh = true
	}
	invisible := flags&EncodeInvisibleFrame != 0
	hiddenAltRefFrame := flags&(EncodeInvisibleFrame|EncodeForceAltRefFrame) == EncodeInvisibleFrame|EncodeForceAltRefFrame
	sourceIsAltRef := !temporalFrame.Enabled && !invisible && e.isSrcFrameAltRef(pts)
	finishSourceAltRef := func() {
		if sourceIsAltRef {
			e.altRefSourceValid = false
			e.altRefSourcePTS = 0
		}
	}
	boostedReferenceFrame := boostedReferenceRateControlFrame(goldenCBRRefresh, flags)
	// libvpx vp8/encoder/ratectrl.c calc_pframe_target_size sets
	// frames_till_gf_update_due=baseline_gf_interval (== gf_interval_onepass_cbr)
	// and current_gf_interval before update_golden_frame_stats accumulates
	// gf_overspend_bits. Mirror that for govpx's CBR refresh.
	// calc_gf_params populates last_boost AFTER the per-frame target
	// (and small +/- last_boost section adjustment) has been computed,
	// so we defer the calcGFParams call until pickGoldenFrameBoost
	// runs below — populating last_boost early would feed the small
	// +/- branch with this frame's boost instead of the prior GF's.
	if goldenCBRRefresh {
		gfInterval := e.goldenFrameCBRInterval(rows, cols)
		e.rc.framesTillGFUpdateDue = gfInterval
		e.rc.currentGFInterval = gfInterval
	}
	// libvpx vp8/encoder/onyx_if.c vp8_check_drop_buffer adjusts
	// cpi->decimation_factor from the post-encode buffer level of the
	// previous frame BEFORE vp8_pick_frame_size / vp8_regulate_q runs, then
	// boosts cpi->per_frame_bandwidth (1->3/2, 2->5/4, 3->5/4) so the
	// boosted target flows through calc_pframe_target_size into
	// vp8_regulate_q. Mirror that ordering here: refresh the decimation
	// factor first, then feed the boosted bits-per-frame into
	// beginFrameWithTargetAndContext so the rate-control regulator sees the
	// same target-size baseline as libvpx on frames that follow a
	// decimation drop.
	e.rc.prepareDecimationForFrame()
	// Decimation drop check runs BEFORE beginFrameWithTargetAndContext to
	// mirror libvpx's encode_frame_to_data_rate ordering exactly: libvpx
	// calls vp8_check_drop_buffer at the top of the function (line 3561 in
	// vp8/encoder/onyx_if.c) and returns BEFORE vp8_pick_frame_size /
	// calc_pframe_target_size run. calc_pframe_target_size is what drains
	// kf_overspend_bits / gf_overspend_bits via the
	// kf_bitrate_adjustment / non_gf_bitrate_adjustment per-frame
	// drains; if we drained those before deciding to drop, libvpx does
	// not, and the post-drop frames see a depleted overspend pool, which
	// pulls this_frame_target up (because applyOnePassPFrameOverspendRecovery
	// has less left to subtract) and pulls the regulated Q down. Closing
	// this gap is what fixes post_drop_q_max_drift on the 30f tight-buffer
	// CBR fixture (govpx Q ran 8-10 indices below libvpx because
	// kf_overspend was draining on every dropped frame too).
	if !invisible && e.rc.checkDropBuffer(keyFrame) {
		e.rc.postDecimationDropFrame()
		e.twoPass.finishFrame(0)
		e.forceKeyFrame = false
		// libvpx's decimation drop does NOT set force_maxqp: only the
		// post-encode overshoot drop does that. Mirror that exactly so
		// the next inter frame's Q regulation runs through the normal
		// path instead of being clamped at max-Q. cyclicRefresh
		// suppression also belongs to overshoot drops only.
		droppedResult := EncodeResult{
			Dropped:                            true,
			BufferLevelBits:                    e.rc.bufferLevelBits,
			FrameTargetBits:                    e.rc.frameTargetBits,
			TargetBitrateKbps:                  e.rc.targetBitrateKbps,
			PTS:                                pts,
			Duration:                           duration,
			TemporalLayerID:                    temporalFrame.LayerID,
			TemporalLayerCount:                 temporalFrame.LayerCount,
			TemporalLayerSync:                  temporalFrame.LayerSync,
			TL0PICIDX:                          temporalFrame.TL0PICIDX,
			TemporalLayerTargetBitrateKbps:     temporalFrame.LayerTargetBitrateKbps,
			TemporalLayerCumulativeBitrateKbps: temporalFrame.LayerCumulativeBitrateKbps,
		}
		e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
		e.populateTemporalLayerBufferResult(&droppedResult, temporalFrame)
		if oracleTraceBuild {
			e.emitOracleDroppedFrameTrace("decimation")
		}
		e.frameCount++
		finishSourceAltRef()
		return droppedResult, nil
	}
	if temporalFrame.Enabled && !keyFrame {
		e.rc.beginFrameWithTargetAndContext(false, temporalFrame.LayerFrameTargetBits, rateControlFrameContext{
			temporalLayerCount: temporalFrame.LayerCount,
			timing:             e.timing,
		})
	} else {
		e.rc.beginFrameWithTargetAndContext(keyFrame, e.rc.decimationBoostedBitsPerFrame(), rateControlFrameContext{
			firstFrame:         e.frameCount == 0,
			forcedKeyFrame:     forcedKeyFrame,
			temporalLayerCount: temporalFrame.LayerCount,
			timing:             e.timing,
		})
	}
	twoPassTargetBits := e.twoPass.frameTargetBits(e.frameCount, keyFrame, e.rc.frameTargetBits)
	if twoPassTargetBits > 0 {
		e.rc.frameTargetBits = twoPassTargetBits
		// libvpx vp8/encoder/firstpass.c Pass2Encode re-clamps the per-frame
		// target through the buffer-state adjustment for CBR
		// (USAGE_STREAM_FROM_SERVER); apply that here so the two-pass
		// override does not erase the buffer-aware shaping.
		e.rc.frameTargetBits = e.rc.applyPass2CBRBufferAdjustment(e.rc.frameTargetBits, keyFrame)
	}
	// libvpx vp8/encoder/firstpass.c vp8_second_pass first-frame branch:
	// estimate_max_q sets cpi->active_worst_quality. Push the seeded
	// override into the rate controller so the regulator's worst-Q
	// ceiling matches libvpx for the upcoming Q regulation. Without
	// this, the regulator picks Q values much lower than libvpx for
	// the same per-frame target on real-content pass-2 fixtures
	// (q_match=8% on desktopqvga while target_match=100%).
	if q, ok := e.twoPass.pass2ActiveWorstQOverride(); ok {
		e.rc.pass2ActiveWorstQOverride = q
		e.rc.pass2ActiveWorstQValid = true
	}
	// libvpx vp8/encoder/firstpass.c define_gf_group ARF-pending decision:
	// when second-pass stats indicate the upcoming GF section is high
	// motion / high-quality predicted, arm a hidden alt-ref so the
	// auto-ARF driver can emit it at the predicted offset.
	e.pass2MaybeArmAltRefPending(e.frameCount, pts, keyFrame)
	if goldenCBRRefresh {
		// libvpx vp8/encoder/ratectrl.c calc_pframe_target_size: when the
		// GF refresh fires, calc_gf_params runs FIRST (auto_adjust_gold_quantizer=1
		// is the default) and updates cpi->last_boost AND
		// cpi->frames_till_gf_update_due. Then the GF target formula
		// consumes those just-computed values. Mirror that order here so
		// the non-CBR boost path below sees the new boost / interval.
		gfOut := calcGFParams(gfParamsInput{
			Q:                     e.rc.lastInterQuantizer,
			RecentRefIntra:        e.rc.recentRefFrameUsageIntra,
			RecentRefLast:         e.rc.recentRefFrameUsageLast,
			RecentRefGolden:       e.rc.recentRefFrameUsageGolden,
			RecentRefAltRef:       e.rc.recentRefFrameUsageAltRef,
			GFActiveCount:         e.rc.gfActiveCount,
			Macroblocks:           required,
			ThisFramePercentIntra: e.rc.thisFramePercentIntra,
			BaselineGFInterval:    e.rc.framesTillGFUpdateDue,
			MaxGFInterval:         e.rc.framesTillGFUpdateDue,
		})
		e.rc.lastBoost = gfOut.Boost
		if e.rc.mode == RateControlCBR {
			// One-pass CBR: libvpx multiplies this_frame_target by
			// (100 + gf_cbr_boost_pct) / 100 (vp8/encoder/ratectrl.c
			// gf_update_onepass_cbr branch).
			e.rc.frameTargetBits = boostedFrameTargetBits(e.rc.frameTargetBits, e.rc.gfCBRBoostPct)
		} else {
			// One-pass VBR/CQ: libvpx splits the upcoming GF section
			// across (frames_till_gf_update_due+1) frames, weighting the
			// GF by `last_boost`. See libvpxGoldenFrameTargetBits for the
			// exact formula. Falls back to the previous boostPct path if
			// inter_frame_target was not yet recorded (i.e. the first
			// inter frame after a key) - in that case the small +/- branch
			// has not yet seeded interFrameTarget so use bitsPerFrame.
			interFrameTarget := e.rc.interFrameTarget
			if interFrameTarget <= 0 {
				interFrameTarget = e.rc.bitsPerFrame
			}
			boosted := libvpxGoldenFrameTargetBits(gfOut.Boost, gfOut.FramesTillUpdate, interFrameTarget)
			if boosted > 0 {
				e.rc.frameTargetBits = boosted
			}
		}
		// Propagate the just-computed GF interval into rc state so the
		// next non-GF frame's small +/- branch sees the right value.
		// Mirrors libvpx's calc_gf_params tail (cpi->frames_till_gf_update_due
		// = baseline_gf_interval; cpi->current_gf_interval = ...).
		e.rc.framesTillGFUpdateDue = gfOut.FramesTillUpdate
		e.rc.currentGFInterval = gfOut.FramesTillUpdate
	}
	e.rc.selectQuantizerForFrameKindWithScreenContent(keyFrame, boostedReferenceFrame, required, e.opts.ScreenContentMode)
	// libvpx vp8/encoder/ratectrl.c vp8_regulate_q forces Q to
	// `cpi->worst_quality` (the configured maxQuantizer) on the next frame
	// after vp8_drop_encodedframe_overshoot fires - the post-encode
	// overshoot drop signals the next frame to ramp Q to the floor of
	// quality so the buffer can recover. govpx must mirror that override
	// after the regulator has settled, otherwise the overshoot-drop signal
	// is observed (cyclic refresh suppression) but the next frame's Q is
	// still picked from the rate model and undoes the buffer recovery.
	if e.forceMaxQuantizer {
		e.rc.currentQuantizer = e.rc.maxQuantizer
		e.rc.currentZbinOverQuant = 0
	}
	// libvpx vp8/encoder/firstpass.c estimate_max_q applies a CQ floor
	// (`USAGE_CONSTRAINED_QUALITY -> Q = max(Q, cq_target_quality)`)
	// AFTER the second-pass Q regulation. Re-assert it here so the
	// regulated quantizer never falls below the configured CQ target.
	e.rc.applyCQFloor()

	result := EncodeResult{
		KeyFrame:                           keyFrame,
		SceneCut:                           sceneCutKeyFrame,
		LookaheadDepth:                     preprocessMeta.lookaheadDepth,
		ARNRFiltered:                       preprocessMeta.arnrFiltered,
		Denoised:                           preprocessMeta.denoised,
		FirstPassStats:                     e.twoPass.statsForFrame(e.frameCount),
		TwoPassFrameTargetBits:             twoPassTargetBits,
		PTS:                                pts,
		Duration:                           duration,
		Quantizer:                          libvpxQIndexToPublicQuantizer(e.rc.currentQuantizer),
		TargetBitrateKbps:                  e.rc.targetBitrateKbps,
		FrameTargetBits:                    e.rc.frameTargetBits,
		BufferLevelBits:                    e.rc.bufferLevelBits,
		TemporalLayerID:                    temporalFrame.LayerID,
		TemporalLayerCount:                 temporalFrame.LayerCount,
		TemporalLayerSync:                  temporalFrame.LayerSync,
		TL0PICIDX:                          temporalFrame.TL0PICIDX,
		TemporalLayerTargetBitrateKbps:     temporalFrame.LayerTargetBitrateKbps,
		TemporalLayerCumulativeBitrateKbps: temporalFrame.LayerCumulativeBitrateKbps,
	}
	// Decimation drop check moved earlier (before beginFrameWithTargetAndContext)
	// to mirror libvpx's vp8_check_drop_buffer ordering. The buffer-underrun
	// drop below stays here because libvpx checks it INSIDE
	// calc_pframe_target_size (i.e. after the kf_overspend drain).
	if !keyFrame && !invisible && e.rc.shouldDropInterFrame() {
		e.rc.postDropFrame()
		e.twoPass.finishFrame(0)
		result.Dropped = true
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
		// libvpx's buffer-underrun drop in vp8/encoder/ratectrl.c
		// calc_pframe_target_size only sets cpi->drop_frame=1 and updates
		// the buffer level - it does NOT touch cpi->force_maxqp. force_maxqp
		// is the post-encode-overshoot signal from vp8_drop_encodedframe_overshoot
		// (a different drop path with screen_content_mode==2 / drop_frames_allowed
		// gating). Setting forceMaxQuantizer here on the buffer-underrun
		// branch therefore spuriously disables cyclic refresh on the frame
		// after a buffer-underrun drop (cyclicRefreshModeEnabled gates on
		// !forceMaxQuantizer, mirroring libvpx's force_maxqp==0 check).
		e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
		e.populateTemporalLayerBufferResult(&result, temporalFrame)
		// Oracle trace: emit a dropped-frame row before frameCount advances.
		// libvpx's parity oracle emits the same row from
		// build_vpxenc_oracle.sh at the buffer-underrun return path inside
		// encode_frame_to_data_rate. govpx's drop trigger
		// (rc.shouldDropInterFrame) gates on bufferLevelBits<0 which is the
		// libvpx-equivalent calc_pframe_target_size buffer-underrun branch,
		// so the reason is "buffer_underrun".
		if oracleTraceBuild {
			e.emitOracleDroppedFrameTrace("buffer_underrun")
		}
		e.frameCount++
		finishSourceAltRef()
		return result, nil
	}

	if e.opts.Tuning == TuneSSIM {
		if err := e.prepareTuningActivityMap(source, rows, cols); err != nil {
			return EncodeResult{}, err
		}
	} else if e.activityMapValid {
		e.activityMapValid = false
	}
	staticSegmentationAllowed := !temporalFrame.Enabled || temporalFrame.LayerID == 0
	e.beginAutoSpeedTiming()
	if !keyFrame {
		attempt, err := e.encodeInterFrameWithQuantizerFeedback(dst, source, rows, cols, required, flags, temporalReferenceControl, goldenCBRRefresh, boostedReferenceFrame, staticSegmentationAllowed, sourceIsAltRef)
		if err != nil {
			e.cancelAutoSpeedTiming()
			return EncodeResult{}, err
		}
		// libvpx vp8/encoder/onyx_if.c:3970-3982 runs
		// vp8_drop_encodedframe_overshoot after vp8_encode_frame on
		// one-pass CBR. When it returns 1 the encoded frame is discarded
		// and the next frame is forced to max-Q via cpi->force_maxqp.
		// The function only fires under screen_content_mode==2 or with
		// drop_frames_allowed plus a starved rate-correction-factor; for
		// the common non-screen-content / drop-disabled config it just
		// advances frames_since_last_drop_overshoot so the rcf-watchdog
		// branch can arm next time.
		if !invisible && e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, attempt.Size, required, false) {
			e.finishAutoSpeedTiming(false)
			e.twoPass.finishFrame(0)
			result.Dropped = true
			result.SizeBytes = 0
			result.BufferLevelBits = e.rc.bufferLevelBits
			result.FrameTargetBits = e.rc.frameTargetBits
			e.forceKeyFrame = false
			// libvpx: cpi->frames_since_key++ on overshoot drop; mirror
			// it so the next-keyframe distance heuristic stays aligned.
			e.rc.framesSinceKeyframe++
			e.temporal.finishDroppedFrame(temporalFrame, e.temporalBufferConfig())
			e.populateTemporalLayerBufferResult(&result, temporalFrame)
			if oracleTraceBuild {
				e.emitOracleDroppedFrameTrace("overshoot")
			}
			e.frameCount++
			finishSourceAltRef()
			return result, nil
		}
		if !invisible {
			e.lastPredErrorMB = e.currentPredictionErrorMB(required)
		}
		if thisFramePercentIntra, recodeKeyFrame := e.shouldRecodeInterAttemptAsKeyFrame(required, attempt.Config.RefreshGolden, temporalFrame.Enabled, invisible); recodeKeyFrame {
			keyFrame = true
			sceneCutKeyFrame = true
			e.rc.thisFramePercentIntra = thisFramePercentIntra
			// libvpx clears source_alt_ref_active before restarting the
			// encode as a key frame; the normal key-frame commit below will
			// reset the rest of the golden-frame/alt-ref lifecycle.
			e.sourceAltRefActive = false
			e.resetOracleMBTraceBuffer()
			e.rc.beginFrameWithTargetAndContext(true, e.rc.decimationBoostedBitsPerFrame(), rateControlFrameContext{
				temporalLayerCount: temporalFrame.LayerCount,
				timing:             e.timing,
			})
			twoPassTargetBits = e.twoPass.frameTargetBits(e.frameCount, true, e.rc.frameTargetBits)
			if twoPassTargetBits > 0 {
				e.rc.frameTargetBits = twoPassTargetBits
				e.rc.frameTargetBits = e.rc.applyPass2CBRBufferAdjustment(e.rc.frameTargetBits, true)
			}
			e.rc.selectQuantizerForFrameKindWithScreenContent(true, false, required, e.opts.ScreenContentMode)
			// Same force_maxqp regulator gate as the primary path
			// above: if the prior frame's overshoot drop set the flag,
			// libvpx vp8_regulate_q honors it on the next frame
			// regardless of frame type, including a scene-cut KF
			// promoted from this auto-key recode path.
			if e.forceMaxQuantizer {
				e.rc.currentQuantizer = e.rc.maxQuantizer
				e.rc.currentZbinOverQuant = 0
			}
			e.rc.applyCQFloor()
			result.KeyFrame = true
			result.SceneCut = true
			result.TwoPassFrameTargetBits = twoPassTargetBits
			result.FrameTargetBits = e.rc.frameTargetBits
			result.BufferLevelBits = e.rc.bufferLevelBits
			result.Quantizer = libvpxQIndexToPublicQuantizer(e.rc.currentQuantizer)
			result.InternalQuantizer = e.rc.currentQuantizer
		} else {
			finalQuantizer := e.rc.currentQuantizer
			e.commitInterFrameAttempt(attempt)
			e.loopFilterLevel = attempt.Config.LoopFilterLevel
			result.Data = dst[:attempt.Size]
			result.SizeBytes = attempt.Size
			e.setEncodeResultQuantizer(&result, finalQuantizer)
			result.Droppable = interFrameDroppable(attempt.Config)
			if oracleTraceBuild {
				e.emitOracleRateAndRecodeTrace(vp8common.InterFrame, finalQuantizer, attempt.Size, attempt.ProjectedSizeBits, attempt.CoefSavingsBits, attempt.RefFrameSavingsBits)
			}
			e.rc.postEncodeFrameWithPacketContext(attempt.Size, rateControlPostEncodeContext{
				goldenFrame:           attempt.Config.RefreshGolden,
				altRefFrame:           attempt.Config.RefreshAltRef,
				macroblocks:           required,
				showFrame:             !invisible,
				skipPostPackOverspend: e.twoPass.enabled(),
				alwaysUpdateFactor:    e.opts.RTCExternalRateControl,
			})
			if hiddenAltRefFrame {
				e.twoPass.chargeAltRefFrameBits(encodedSizeBits(attempt.Size))
			} else {
				e.twoPass.finishFrame(encodedSizeBits(attempt.Size))
			}
			e.rc.clampScreenContentBufferDebt(e.opts.ScreenContentMode)
			result.BufferLevelBits = e.rc.bufferLevelBits
			e.forceKeyFrame = false
			if attempt.CyclicRefresh {
				e.commitCyclicRefresh(rows, cols, attempt.CyclicRefreshNextIndex, e.interFrameModes[:required])
			}
			e.lastInterZeroMVCount = countLastZeroMVInterFrameModes(e.interFrameModes[:required])
			e.lastInterSkipCount = countSkippedInterFrameModes(e.interFrameModes[:required])
			e.updateConsecutiveZeroLast(e.interFrameModes[:required])
			// libvpx vp8/encoder/onyx_if.c update_golden_frame_stats: track
			// per-frame ref usage so calc_gf_params and the auto_gold
			// refresh decision read the same `recent_ref_frame_usage`
			// libvpx would. On GF refresh the encoder resets the counters
			// to {1,1,1,1} via resetRecentRefFrameUsage; otherwise the
			// counts accumulate (skipping the immediate post-GF frame).
			intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes[:required])
			if attempt.Config.RefreshGolden {
				e.rc.resetRecentRefFrameUsage(required)
			} else {
				e.rc.updateRecentRefFrameUsage(intra, last, golden, alt)
			}
			if required > 0 {
				e.rc.thisFramePercentIntra = (100 * intra) / required
			}
			// libvpx vp8/encoder/onyx_if.c rolls last_frame_percent_intra
			// AFTER decide_key_frame consumes this_frame_percent_intra.
			// Keep that ordering here: lastFramePercentIntra captures the
			// just-encoded frame's value for the next frame's heuristic.
			e.lastFramePercentIntra = e.rc.thisFramePercentIntra
			e.temporal.finishFrame(temporalFrame, false, !invisible, temporalReferenceRefresh{
				Last:   attempt.Config.RefreshLast,
				Golden: attempt.Config.RefreshGolden,
				AltRef: attempt.Config.RefreshAltRef,
			}, encodedSizeBits(attempt.Size), e.temporalBufferConfig())
			e.populateTemporalLayerBufferResult(&result, temporalFrame)
			if oracleTraceBuild {
				e.emitOracleFrameTrace(oracleTraceFrameSummary{
					FrameType:            vp8common.InterFrame,
					BaseQIndex:           int(attempt.Config.BaseQIndex),
					LoopFilter:           int(attempt.Config.LoopFilterLevel),
					SharpnessLevel:       int(attempt.Config.SharpnessLevel),
					RefLFDeltas:          attempt.Config.RefLFDeltas,
					ModeLFDeltas:         attempt.Config.ModeLFDeltas,
					ModeRefLFDeltaEnable: attempt.Config.LFDeltaEnabled,
					ModeRefLFDeltaUpdate: attempt.Config.LFDeltaUpdate,
					RefreshLast:          attempt.Config.RefreshLast,
					RefreshGolden:        attempt.Config.RefreshGolden,
					RefreshAltRef:        attempt.Config.RefreshAltRef,
					GoldenSignBias:       attempt.Config.GoldenSignBias,
					AltRefSignBias:       attempt.Config.AltRefSignBias,
					SegEnabled:           attempt.Config.Segmentation.Enabled,
					SizeBytes:            attempt.Size,
				})
				e.flushOracleMBTraceBuffer()
			}
			// libvpx onyx_if.c end-of-encode: record ambient_err if the next
			// frame will be a forced KF so the forced-KF recode branch has a
			// baseline to compare against.
			e.updateNextKeyFrameForcedAfterCommit(source, rows, cols)
			if !hiddenAltRefFrame {
				e.finishAutoSpeedTiming(false)
				e.frameCount++
			}
			finishSourceAltRef()
			return result, nil
		}
	}

	// libvpx vp8/encoder/onyx_if.c sets cpi->this_key_frame_forced when the
	// key frame is timing-driven (max-interval forced) rather than content-
	// driven. The recode loop reads it to engage the SS-error feedback Q
	// adjustment branch around line 4065.
	e.thisKeyFrameForced = forcedKeyFrame && !sceneCutKeyFrame && e.frameCount > 0
	// libvpx vp8/encoder/ratectrl.c vp8_setup_key_frame seeds the next GF
	// section countdown to baseline_gf_interval and asserts
	// refresh_golden_frame=1 / refresh_alt_ref_frame=1 on every key frame
	// before encoding. update_golden_frame_stats reads this on the
	// post-encode path to compute non_gf_bitrate_adjustment =
	// gf_overspend_bits / frames_till_gf_update_due, which the next inter
	// frame's calc_pframe_target_size drains. Without seeding it here,
	// govpx's CBR / multi-keyframe paths leave frames_till_gf_update_due at
	// 0 across the keyframe boundary, so non_gf_bitrate_adjustment stays at
	// 0 and the gf_overspend_bits drain never fires - causing per-frame
	// target bits to drift higher than libvpx's, which lowers Q on the
	// inter-recode path at good-quality cpu5 128x128.
	//
	// libvpx onyx_if.c sets baseline_gf_interval to gf_interval_onepass_cbr
	// (==goldenFrameCBRInterval below) for realtime CBR but resets it back
	// to DEFAULT_GF_INTERVAL on subsequent vp8_change_config invocations
	// that don't take the realtime branch (line 1547). vpxenc invokes
	// vp8_change_config after vp8_create_compressor, so good-quality CBR
	// observes baseline_gf_interval=DEFAULT_GF_INTERVAL=7 at first-keyframe
	// time while realtime CBR observes the cyclic-refresh gf_interval.
	e.rc.framesTillGFUpdateDue = e.libvpxKeyFrameSetupGFInterval(rows, cols)
	keyAttempt, err := e.encodeKeyFrameWithQuantizerFeedback(dst, source, rows, cols, required, invisible, staticSegmentationAllowed)
	if err != nil {
		e.cancelAutoSpeedTiming()
		return EncodeResult{}, err
	}
	finalQuantizer := e.rc.currentQuantizer
	e.commitKeyFrameEntropy(keyAttempt)
	// Mirror libvpx onyx_if.c key-frame branch: zero frames_since_golden,
	// drop source_alt_ref_active when no ARF schedule is pending, and
	// decrement frames_till_alt_ref_frame. Carried out by
	// `refreshKeyFrameReferencesFromAnalysis -> resetGoldenFrameStats`,
	// which is the single keyframe-path call point. Calling it twice
	// (legacy code did) would double-decrement framesTillAltRefFrame and
	// silently shorten any pass2-armed ARF schedule.
	e.refreshKeyFrameReferencesFromAnalysis()
	// Seed denoiser running averages from the key-frame source (libvpx
	// onyx_if.c update_reference_frames key-frame branch).
	e.initDenoiserAvgFromKeyFrame(source)
	// Key frames consume any pending force_maxqp gate without applying it
	// (cyclic refresh is already keyframe-reset).
	e.forceMaxQuantizer = false
	e.loopFilterLevel = keyAttempt.LoopFilterLevel
	result.Data = dst[:keyAttempt.Size]
	result.SizeBytes = keyAttempt.Size
	e.setEncodeResultQuantizer(&result, finalQuantizer)
	if oracleTraceBuild {
		e.emitOracleRateAndRecodeTrace(vp8common.KeyFrame, finalQuantizer, keyAttempt.Size, keyAttempt.ProjectedSizeBits, keyAttempt.CoefSavingsBits, keyAttempt.RefFrameSavingsBits)
	}
	e.rc.postEncodeFrameWithPacketContext(keyAttempt.Size, rateControlPostEncodeContext{
		keyFrame:              true,
		macroblocks:           required,
		showFrame:             !invisible,
		skipPostPackOverspend: e.twoPass.enabled(),
		alwaysUpdateFactor:    e.opts.RTCExternalRateControl,
	})
	if twoPassSceneCut {
		e.twoPass.markKeyFrame(e.frameCount)
	}
	e.twoPass.finishFrame(encodedSizeBits(keyAttempt.Size))
	e.rc.clampScreenContentBufferDebt(e.opts.ScreenContentMode)
	result.BufferLevelBits = e.rc.bufferLevelBits
	e.forceKeyFrame = false
	// libvpx vp8/encoder/onyx_if.c does NOT reset cyclic_refresh_mode_index
	// on key frames — only on init/resize (see lines 1213/1870 vs the
	// frame_type != KEY_FRAME gate around the loop at line 534). The
	// persistent index is preserved so the first inter frame after each
	// keyframe continues the rolling refresh from where it left off.
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	clearBoolMap(e.dotArtifactChecked)
	e.lastInterZeroMVCount = 0
	e.lastInterSkipCount = 0
	// libvpx vp8/encoder/onyx_if.c key-frame path resets the rolling
	// recent_ref_frame_usage counters to 1 each (the same as a GF
	// refresh) so the next GF section starts with a clean baseline.
	e.rc.resetRecentRefFrameUsage(required)
	if e.rc.framesTillGFUpdateDue > 0 {
		e.rc.currentGFInterval = e.rc.framesTillGFUpdateDue
		e.rc.framesTillGFUpdateDue--
	}
	e.rc.thisFramePercentIntra = 100
	// libvpx vp8/encoder/onyx_if.c sets last_frame_percent_intra=100
	// after every key frame, mirroring the encoder's expectation that
	// the next inter frame starts from an "all-intra" baseline.
	e.lastFramePercentIntra = 100
	e.resetInterRDThresholdMultipliers()
	e.interRDFrameActive = false
	e.temporal.finishFrame(temporalFrame, true, !invisible, temporalReferenceRefresh{Last: true, Golden: true, AltRef: true}, encodedSizeBits(keyAttempt.Size), e.temporalBufferConfig())
	e.populateTemporalLayerBufferResult(&result, temporalFrame)
	if oracleTraceBuild {
		e.emitOracleFrameTrace(oracleTraceFrameSummary{
			FrameType:            vp8common.KeyFrame,
			BaseQIndex:           e.rc.currentQuantizer,
			LoopFilter:           int(keyAttempt.LoopFilterLevel),
			SharpnessLevel:       int(keyAttempt.SharpnessLevel),
			RefLFDeltas:          keyAttempt.RefLFDeltas,
			ModeLFDeltas:         keyAttempt.ModeLFDeltas,
			ModeRefLFDeltaEnable: keyAttempt.LFDeltaEnabled,
			ModeRefLFDeltaUpdate: keyAttempt.LFDeltaUpdate,
			RefreshLast:          true,
			RefreshGolden:        true,
			RefreshAltRef:        true,
			SegEnabled:           keyAttempt.SegmentationEnabled,
			SizeBytes:            keyAttempt.Size,
		})
		e.flushOracleMBTraceBuffer()
	}
	// libvpx onyx_if.c, end-of-encode: clear this_key_frame_forced after the
	// frame has been committed; the next forced KF will set it again. Update
	// the next_key_frame_forced bookkeeping for the following frame's
	// ambient_err capture.
	e.thisKeyFrameForced = false
	e.updateNextKeyFrameForcedAfterCommit(source, rows, cols)
	e.finishAutoSpeedTiming(true)
	e.frameCount++
	finishSourceAltRef()
	return result, nil
}

// updateNextKeyFrameForcedAfterCommit ports the libvpx
// vp8/encoder/onyx_if.c `if (cpi->next_key_frame_forced && frames_to_key == 0)`
// branch at the end of encode_frame_to_data_rate (around line 4282). When the
// just-encoded frame is the one *immediately before* a forced KF, the encoder
// stores the SS error of its reconstruction so the upcoming forced-KF recode
// loop can compare against it via forcedKeyFrameRecodeQuantizer.
func (e *VP8Encoder) updateNextKeyFrameForcedAfterCommit(source vp8enc.SourceImage, rows int, cols int) {
	interval := e.opts.KeyFrameInterval
	if interval <= 0 {
		return
	}
	// For govpx one-pass, the "next frame is a forced KF" predicate matches
	// libvpx's twopass.frames_to_key == 0 hand-off: with a fixed
	// KeyFrameInterval, frames at indices that are multiples of the interval
	// (after the bootstrap) are forced key frames. So the *current* frame's
	// frameCount being one less than such an index means we should capture
	// ambient_err now.
	nextIndex := e.frameCount + 1
	if nextIndex == 0 || nextIndex%uint64(interval) != 0 {
		return
	}
	e.ambientErr = calcKeyFrameSSError(source, &e.current.Img, rows, cols)
}

func (e *VP8Encoder) populateTemporalLayerBufferResult(result *EncodeResult, meta temporalFrame) {
	if result == nil || !meta.Enabled || meta.LayerID < 0 || meta.LayerID >= meta.LayerCount || meta.LayerID >= MaxTemporalLayers {
		return
	}
	accounting := e.temporal.accounting[meta.LayerID]
	result.TemporalLayerFrameBandwidthBits = accounting.FrameBandwidthBits
	result.TemporalLayerBufferLevelBits = accounting.BufferLevelBits
	result.TemporalLayerMaximumBufferBits = accounting.MaximumBufferBits
	result.TemporalLayerInputFrames = accounting.InputFrames
	result.TemporalLayerEncodedFrames = accounting.EncodedFrames
	result.TemporalLayerTotalEncodedFrames = accounting.TotalEncodedFrames
	result.TemporalLayerEncodedBits = accounting.EncodedBits
}

func (e *VP8Encoder) temporalBufferConfig() temporalBufferConfig {
	return temporalBufferConfig{
		timing:              e.timing,
		bufferInitialSizeMs: e.rc.bufferInitialSizeMs,
		bufferSizeMs:        e.rc.bufferSizeMs,
	}
}
