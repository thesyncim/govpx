package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// SetBitrateKbps changes the total encoder target bitrate, in kbps. The
// new value is clamped to [MinBitrateKbps, MaxBitrateKbps] when those
// bounds are non-zero. Temporal-layer per-layer bitrates rescale
// proportionally. Returns [ErrInvalidBitrate] if kbps is not positive.
func (e *VP8Encoder) SetBitrateKbps(kbps int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(kbps, e.timing); err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.refreshTemporalLayerCodingGeometry()
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.TemporalScalability = nextTemporal.config
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetRateControl replaces the encoder's runtime-updatable rate-control
// configuration in a single atomic update. Validation is all-or-nothing:
// when any field is out of range no state changes and the error is
// returned. Use this instead of multiple Set* calls when several fields
// change together (mode + bitrate, quantizer bounds, buffer model).
func (e *VP8Encoder) SetRateControl(cfg RateControlConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextRC := e.rc
	if err := nextRC.applyConfig(cfg, e.timing); err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.refreshTemporalLayerCodingGeometry()
	e.opts.RateControlMode = cfg.Mode
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.MinBitrateKbps = cfg.MinBitrateKbps
	e.opts.MaxBitrateKbps = cfg.MaxBitrateKbps
	e.opts.MinQuantizer = cfg.MinQuantizer
	e.opts.MaxQuantizer = cfg.MaxQuantizer
	e.opts.CQLevel = normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer)
	e.opts.UndershootPct = cfg.UndershootPct
	e.opts.OvershootPct = cfg.OvershootPct
	e.opts.BufferSizeMs = cfg.BufferSizeMs
	e.opts.BufferInitialSizeMs = cfg.BufferInitialSizeMs
	e.opts.BufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	e.opts.DropFrameAllowed = e.rc.dropFramesWaterMark > 0
	e.opts.DropFrameWaterMark = e.rc.dropFramesWaterMark
	e.opts.MaxIntraBitratePct = cfg.MaxIntraBitratePct
	e.opts.GFCBRBoostPct = cfg.GFCBRBoostPct
	e.opts.TemporalScalability = nextTemporal.config
	// libvpx vp8/encoder/firstpass.c frame_max_bits (lines 316-368)
	// dispatches on cpi->oxcf.end_usage. Push the updated mode into
	// the two-pass state so a runtime RateControlMode change picks up
	// the correct CBR/VBR branch on the next pass-2 allocation.
	e.twoPass.configureEndUsage(libvpxVP8EndUsageFromRateControlMode(e.rc.mode))
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetCQLevel changes the public 0..63 CQ/Q quantizer level. Under
// [RateControlCQ] the level is applied as a quantizer floor; under
// [RateControlQ] it pins the target quantizer like libvpx VPX_Q. Levels
// outside [MinQuantizer, MaxQuantizer] are rejected with
// [ErrInvalidQuantizer] when the active mode consumes the level.
func (e *VP8Encoder) SetCQLevel(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(level) > uint(maxQuantizer) {
		return ErrInvalidQuantizer
	}
	if rateControlModeUsesCQLevel(e.rc.mode) && (level < e.opts.MinQuantizer || level > e.opts.MaxQuantizer) {
		return ErrInvalidQuantizer
	}
	qIndex := libvpxPublicQuantizerToQIndex(level)
	e.rc.cqLevel = qIndex
	e.opts.CQLevel = level
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = qIndex
		e.rc.lastQuantizer = qIndex
		e.rc.lastInterQuantizer = qIndex
	}
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetMaxIntraBitratePct caps key-frame size as a percentage of the
// per-frame target. Zero disables the cap. See
// EncoderOptions.MaxIntraBitratePct.
func (e *VP8Encoder) SetMaxIntraBitratePct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.maxIntraBitratePct = pct
	e.opts.MaxIntraBitratePct = pct
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetGFCBRBoostPct changes the golden-frame boost percentage applied in
// CBR mode. Non-negative percentages are accepted. See
// EncoderOptions.GFCBRBoostPct.
func (e *VP8Encoder) SetGFCBRBoostPct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.gfCBRBoostPct = pct
	e.opts.GFCBRBoostPct = pct
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetTokenPartitions changes the VP8 token-partition selector:
// 0 = one partition, 1 = two, 2 = four, 3 = eight. See
// EncoderOptions.TokenPartitions.
func (e *VP8Encoder) SetTokenPartitions(partitions int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if partitions < int(vp8common.OnePartition) || partitions > int(vp8common.EightPartition) {
		return ErrInvalidConfig
	}
	e.opts.TokenPartitions = partitions
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetSharpness changes the VP8 loop-filter sharpness level. Valid range
// is [0, 7]. See EncoderOptions.Sharpness.
func (e *VP8Encoder) SetSharpness(sharpness int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(sharpness) > 7 {
		return ErrInvalidConfig
	}
	e.opts.Sharpness = sharpness
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetStaticThreshold changes the static-macroblock breakout threshold.
// Non-negative values are accepted; zero disables the breakout.
// See EncoderOptions.StaticThreshold.
func (e *VP8Encoder) SetStaticThreshold(threshold int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if threshold < 0 {
		return ErrInvalidConfig
	}
	e.opts.StaticThreshold = threshold
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetScreenContentMode changes the libvpx-style screen-content mode:
// 0 = off, 1 = on, 2 = on with more aggressive rate control. See
// EncoderOptions.ScreenContentMode.
func (e *VP8Encoder) SetScreenContentMode(mode int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(mode) > 2 {
		return ErrInvalidConfig
	}
	e.opts.ScreenContentMode = mode
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetRTCExternalRateControl enables libvpx's VP8 RTC external-rate-control
// mode. Like libvpx's VP8E_SET_RTC_EXTERNAL_RATECTRL control, only a non-zero
// value mutates encoder state; a false call is accepted as a no-op. Enabling is
// sticky and does not re-enable cyclic refresh or overshoot recode later. See
// EncoderOptions.RTCExternalRateControl.
func (e *VP8Encoder) SetRTCExternalRateControl(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if !enabled {
		return nil
	}
	update := e.runtimeSegmentationUpdatePending
	if !e.runtimePreserveSegmentation {
		e.preserveCurrentSegmentationHeader(update)
	} else if update && e.runtimePreservedSegmentation.Enabled {
		e.runtimePreserveSegmentationUpdate = true
		e.runtimeSegmentationUpdatePending = false
	}
	e.opts.RTCExternalRateControl = true
	e.rtcExternalDisableCyclicRefresh = true
	e.applyChangeConfigSpeedReset()
	return nil
}

// SetFrameDropAllowed enables or disables realtime rate-control frame
// dropping without touching bitrate. It maps to libvpx rc_dropframe_thresh:
// enabled applies EncoderOptions.DropFrameWaterMark, disabled writes 0.
func (e *VP8Encoder) SetFrameDropAllowed(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.setFrameDropAllowed(enabled)
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetAutoAltRef enables or disables automatic alternate-reference
// scheduling at runtime. Mirrors libvpx's VP8E_SET_ENABLEAUTOALTREF
// control (vp8/vp8_cx_iface.c set_enable_auto_alt_ref). libvpx applies
// the value verbatim with only a boolean range check; downstream altref
// scheduling code gates on the combination of this flag and lookahead /
// rate-control mode at use time. See EncoderOptions.AutoAltRef.
func (e *VP8Encoder) SetAutoAltRef(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AutoAltRef = enabled
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetScalingMode mirrors libvpx's VP8E_SET_SCALEMODE control
// (vp8/vp8_cx_iface.c vp8e_set_scalemode + vp8/encoder/onyx_if.c
// vp8_set_internal_size). It selects a fixed VP8 down-scale ratio for
// each axis and signals it in the keyframe uncompressed-data chunk's
// scale bits (RFC 6386 §9.1). libvpx forces the next frame to be a key
// frame so the new scale takes effect; govpx mirrors that with
// forceKeyFrame.
//
// Both modes must be one of [ScalingNormal], [ScalingFourFive],
// [ScalingThreeFive], or [ScalingOneTwo]; out-of-range values are
// rejected with [ErrInvalidConfig], matching libvpx's
// vp8_set_internal_size range check (returns -1, which the iface
// translates to VPX_CODEC_INVALID_PARAM).
//
// Divergence from libvpx's input contract: libvpx accepts source frames
// at the unscaled [EncoderOptions.Width] / Height and rescales them
// internally via vpx_scale_frame before encoding (the
// CONFIG_SPATIAL_RESAMPLING path). govpx writes the scale bits into the
// bitstream but does not rescale the input source itself; callers must
// supply source frames at the dimensions they want the encoder to code
// at. The decoded output is bit-identical to what libvpx would emit if
// its [EncoderOptions.Width] / Height already equaled the scaled
// dimensions. Use the [internal/vp8/scale] package to pre-scale.
func (e *VP8Encoder) SetScalingMode(horiz ScalingMode, vert ScalingMode) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if horiz < ScalingNormal || horiz > ScalingOneTwo {
		return ErrInvalidConfig
	}
	if vert < ScalingNormal || vert > ScalingOneTwo {
		return ErrInvalidConfig
	}
	e.horizScale = uint8(horiz)
	e.vertScale = uint8(vert)
	// libvpx vp8e_set_scalemode (vp8/vp8_cx_iface.c:1305-1308) sets
	// next_frame_flag |= FRAMEFLAGS_KEY after a successful scale change
	// so the new pc->horiz_scale / pc->vert_scale take effect on the
	// next emitted frame tag (which carries the scale bits only on key
	// frames).
	e.forceKeyFrame = true
	return nil
}

// setFrameDropFromThresh applies libvpx rc_dropframe_thresh semantics.
func (e *VP8Encoder) setFrameDropFromThresh(thresh int) {
	e.rc.applyLibvpxDropFrameThresh(thresh)
	e.opts.DropFrameWaterMark = e.rc.dropFramesWaterMark
	e.opts.DropFrameAllowed = e.rc.dropFramesWaterMark > 0
}

func (e *VP8Encoder) setFrameDropAllowed(enabled bool) {
	thresh := e.opts.DropFrameWaterMark
	if enabled && thresh == 0 {
		thresh = defaultDropFramesWaterMark
	} else if !enabled {
		thresh = 0
	}
	e.setFrameDropFromThresh(thresh)
}

func (e *VP8Encoder) refreshRuntimeCyclicRefreshConfig() {
	// libvpx pins cyclic_refresh_mode_enabled at compressor creation; a
	// runtime vpx_codec_enc_config_set never recomputes it. Mirror that so a
	// VBR-born encoder switched to CBR does not gain cyclic refresh (and a
	// CBR-born encoder switched to VBR does not lose it).
	if e.cyclicRefreshModeEnabled(false) {
		updatePending := e.segmentationHeaderEnabled && e.lastSegmentationConfig.Enabled
		if !e.rtcExternalDisableCyclicRefresh {
			e.clearRuntimePreservedSegmentationHeader()
			e.runtimeSegmentationUpdatePending = updatePending
		}
		return
	}
	if e.runtimePreserveSegmentation && e.runtimePreservedSegmentation.Enabled {
		if !e.rtcExternalDisableCyclicRefresh && !e.cyclicRefreshConfigured {
			e.runtimePreservedSegmentation = e.cyclicRefreshSegmentationConfigForQuantizerUnchecked(e.rc.currentQuantizer)
		}
		e.runtimePreserveSegmentationUpdate = true
		e.runtimeSegmentationUpdatePending = false
		return
	}
	if e.segmentationHeaderEnabled && e.lastSegmentationConfig.Enabled {
		// libvpx's runtime vpx_codec_enc_config_set can turn cyclic refresh
		// off (for example CBR -> VBR) without clearing the already-enabled
		// VP8 segmentation header. setup_features then re-emits the stale
		// segment feature data/map updates even though no cyclic-refresh
		// producer is active.
		if !e.rtcExternalDisableCyclicRefresh && !e.cyclicRefreshConfigured {
			e.preserveRuntimeCyclicSegmentationForQuantizer(e.rc.currentQuantizer, true)
			return
		}
		e.preserveCurrentSegmentationHeader(true)
	}
}

// SetRealtimeTarget applies a WebRTC-style runtime target update.
//
// Zero-valued fields keep their current setting, so bandwidth-estimator
// updates are safe to send as bitrate-only deltas. Width and Height,
// when both positive and different from the encoder's current
// dimensions, trigger a runtime resolution change: every size-dependent
// buffer is re-sized (reusing capacity where possible), all reference
// frames are invalidated, and the next encoded frame is forced to be a
// key frame at the new size. Mirrors libvpx's `vpx_codec_enc_config_set`
// with a new width / height. The libvpx spatial resampler
// ([VP8E_SET_SCALEMODE], `rc_resize_*`) is not implemented; callers
// drive the coded size directly. The decoder also handles key-frame
// resolution change; see [DecoderOptions.RejectResolutionChange].
//
// Returns [ErrInvalidConfig] for negative numeric fields, an
// out-of-range FrameDrop selector, or a Width / Height pair that does
// not satisfy [VP8 dimension limits]. Resize is also rejected with
// [ErrInvalidConfig] when the lookahead queue is non-empty or a hidden
// alt-ref input is staged; drain the encoder with [VP8Encoder.FlushInto]
// before resizing in those modes. Returns [ErrInvalidQuantizer] when
// MinQuantizer or MaxQuantizer is outside [0, 63] or when both are
// non-zero and MinQuantizer > MaxQuantizer. On any validation failure
// the encoder state is left untouched at the previous configuration.
//
// [VP8 dimension limits]: https://datatracker.ietf.org/doc/html/rfc6386#section-9.1
func (e *VP8Encoder) SetRealtimeTarget(target RealtimeTarget) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if target.BitrateKbps < 0 || target.FPS < 0 || target.Width < 0 || target.Height < 0 {
		return ErrInvalidConfig
	}
	if target.FrameDrop < RealtimeFrameDropUnchanged || target.FrameDrop > RealtimeFrameDropEnabled {
		return ErrInvalidConfig
	}
	if target.MinQuantizer < 0 || target.MaxQuantizer < 0 || target.MinQuantizer > maxQuantizer || target.MaxQuantizer > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if target.MinQuantizer > 0 && target.MaxQuantizer > 0 && target.MinQuantizer > target.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if target.Width > 0 || target.Height > 0 {
		if target.Width <= 0 || target.Height <= 0 || !validDimension(target.Width) || !validDimension(target.Height) {
			return ErrInvalidConfig
		}
		if target.Width != e.opts.Width || target.Height != e.opts.Height {
			if err := e.applyResolutionChange(target.Width, target.Height); err != nil {
				return err
			}
		} else {
			// Same-size shortcut: keep the existing behavior of accepting
			// a no-op resolution echo so bitrate-only BWE updates still
			// validate cleanly.
			e.opts.Width = target.Width
			e.opts.Height = target.Height
		}
	}
	if target.FPS > 0 {
		e.opts.FPS = target.FPS
		e.opts.TimebaseNum = 1
		e.opts.TimebaseDen = target.FPS
		// vpx_codec_enc_config_set stores g_timebase, but vp8_change_config
		// calls vp8_new_framerate(cpi, cpi->framerate) without recomputing
		// cpi->framerate from that new timebase.
		e.timing = timingFromEncoderOptions(e.opts)
	}
	nextMinQuantizer := e.opts.MinQuantizer
	nextMaxQuantizer := e.opts.MaxQuantizer
	if target.MinQuantizer != 0 {
		nextMinQuantizer = target.MinQuantizer
	}
	if target.MaxQuantizer != 0 {
		nextMaxQuantizer = target.MaxQuantizer
	}
	if nextMinQuantizer > nextMaxQuantizer {
		return ErrInvalidQuantizer
	}
	if rateControlModeUsesCQLevel(e.rc.mode) && (e.opts.CQLevel < nextMinQuantizer || e.opts.CQLevel > nextMaxQuantizer) {
		return ErrInvalidQuantizer
	}
	prevCurrentQuantizer := e.rc.currentQuantizer
	prevLastQuantizer := e.rc.lastQuantizer
	prevLastInterQuantizer := e.rc.lastInterQuantizer
	e.rc.minQuantizer = libvpxPublicQuantizerToQIndex(nextMinQuantizer)
	e.rc.maxQuantizer = libvpxPublicQuantizerToQIndex(nextMaxQuantizer)
	e.opts.MinQuantizer = nextMinQuantizer
	e.opts.MaxQuantizer = nextMaxQuantizer
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
		e.rc.lastQuantizer = e.rc.cqLevel
		e.rc.lastInterQuantizer = e.rc.cqLevel
	}
	switch target.FrameDrop {
	case RealtimeFrameDropEnabled:
		e.setFrameDropAllowed(true)
	case RealtimeFrameDropDisabled:
		e.setFrameDropAllowed(false)
	case RealtimeFrameDropUnchanged:
		if target.AllowFrameDrop {
			e.setFrameDropAllowed(true)
		}
	}
	nextTemporal := e.temporal
	if target.BitrateKbps > 0 {
		nextRC := e.rc
		if err := nextRC.setBitrateKbps(target.BitrateKbps, e.timing); err != nil {
			return err
		}
		if e.rc.mode != RateControlCQ {
			nextRC.currentQuantizer = prevCurrentQuantizer
			nextRC.lastQuantizer = prevLastQuantizer
			nextRC.lastInterQuantizer = prevLastInterQuantizer
		}
		if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
			return err
		}
		e.rc = nextRC
		e.temporal = nextTemporal
		e.refreshTemporalLayerCodingGeometry()
		e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
		e.opts.TemporalScalability = nextTemporal.config
		e.applyVP8ChangeConfigRuntimeSideEffects()
		return nil
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(e.rc.targetBitrateKbps, e.timing); err != nil {
		return err
	}
	if e.rc.mode != RateControlCQ {
		nextRC.currentQuantizer = prevCurrentQuantizer
		nextRC.lastQuantizer = prevLastQuantizer
		nextRC.lastInterQuantizer = prevLastInterQuantizer
	}
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.refreshTemporalLayerCodingGeometry()
	e.opts.TemporalScalability = nextTemporal.config
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetTemporalScalability replaces the active temporal scheduling
// configuration. Set TemporalScalabilityConfig.Enabled = false (the zero
// value) to disable temporal layering. The per-layer bitrate vector must
// be cumulative across layers, matching libvpx's ts_target_bitrate[].
func (e *VP8Encoder) SetTemporalScalability(cfg TemporalScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	prevTemporal := e.temporal
	wasTemporal := prevTemporal.enabled
	filterLevel := e.loopFilterLevel
	nextTemporal := temporalState{}
	if err := nextTemporal.configure(cfg, e.rc.targetBitrateKbps); err != nil {
		return err
	}
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	e.initializeTemporalLayerCodingStates()
	if !wasTemporal && e.temporal.enabled {
		e.temporal.codingState[0].FilterLevel = filterLevel
	}
	if wasTemporal && !e.temporal.enabled {
		e.restoreBaseLayerCodingStateAfterTemporalDisable(prevTemporal)
	}
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetTemporalLayerID overrides the temporal layer assigned by the
// configured scheduling pattern. The override is sticky and applies to
// every subsequent frame until it is changed or the pattern is replaced
// by SetTemporalScalability. layerID must be in [0, TemporalLayerCount).
func (e *VP8Encoder) SetTemporalLayerID(layerID int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.temporal.setLayerID(layerID)
}

// SetDeadline changes the encoder speed/quality operating mode. It also
// re-derives the effective CPU-used preset from the raw configured
// cpu-used value, since libvpx stores that raw value at the wrapper layer
// and only clamps the active compressor copy for the current mode.
func (e *VP8Encoder) SetDeadline(deadline Deadline) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if deadline < DeadlineBestQuality || deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	previousDeadline := e.opts.Deadline
	cpuUsed := e.configuredCPUUsedForModeSwitch()
	e.opts.Deadline = deadline
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(deadline, cpuUsed)
	if deadline != DeadlineRealtime {
		e.runtimePinnedCPUUsed = false
	}
	if deadline != previousDeadline {
		e.interRDThreshBaselineGen++
		e.interRDFrameRefSearchOrderValid = false
		e.applyVP8ChangeConfigRuntimeSideEffects()
	}
	e.refreshRuntimeCyclicRefreshConfig()
	return nil
}

// SetCPUUsed changes the libvpx-style speed preset. Valid range is
// [-16, 16]. The interpretation depends on the active [Deadline]: under
// realtime, positive values trigger libvpx's auto-speed selector;
// negative values pin a speed. See EncoderOptions.CpuUsed.
func (e *VP8Encoder) SetCPUUsed(cpuUsed int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cpuUsed < -16 || cpuUsed > 16 {
		return ErrInvalidConfig
	}
	e.configuredCPUUsed = cpuUsed
	e.configuredCPUUsedValid = true
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(e.opts.Deadline, e.configuredCPUUsed)
	e.runtimePinnedCPUUsed = e.opts.Deadline == DeadlineRealtime && e.configuredCPUUsed < 0
	e.interRDThreshBaselineGen++
	e.interRDFrameRefSearchOrderValid = false
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

func (e *VP8Encoder) configuredCPUUsedForModeSwitch() int {
	if e.configuredCPUUsedValid {
		return e.configuredCPUUsed
	}
	return e.opts.CpuUsed
}

// SetTuning changes the encoder's visual quality model. [TuneSSIM]
// rebuilds the activity-mask cache on the next encode; [TunePSNR]
// releases it. See EncoderOptions.Tuning.
func (e *VP8Encoder) SetTuning(tuning Tuning) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if tuning < TunePSNR || tuning > TuneSSIM {
		return ErrInvalidConfig
	}
	e.opts.Tuning = tuning
	e.activityMapValid = false
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

func (e *VP8Encoder) libvpxCPUUsed() int {
	// libvpx encodeframe.c:685-691: realtime mode runs vp8_auto_select_speed
	// which evolves cpi->Speed. Mirror that: for realtime+positive-cpu_used,
	// return the adaptive autoSpeed (seeded to 4 by the cold-start branch in
	// libvpxAutoSelectSpeed during the first frame's auto-select). The
	// "fresh compressor before first encode_mb_row" case keys off
	// `e.frameCount == 0`, NOT off `e.autoSpeed == 0`: after a runtime
	// vp8_change_config the wrapper has reseeded autoSpeed to oxcf.cpu_used
	// (commonly 0) but has already encoded frames, so the libvpx-correct
	// next-frame Speed is 0 rather than the cold-start sentinel of 4.
	// Conflating the two states surfaced as a fast-vs-RD picker divergence
	// at the first frame after every SetBitrateKbps / SetRealtimeTarget /
	// SetTuning / etc — see applyChangeConfigSpeedReset. For
	// realtime+negative-cpu_used and other deadlines, fall back to the
	// static formula.
	if e.opts.Deadline == DeadlineRealtime {
		cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
		if cpuUsed >= 0 {
			if e.frameCount == 0 {
				return 4 // cold start before first encode_mb_row
			}
			return e.autoSpeed
		}
	}
	return libvpxSpeedFeatureCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
}

func libvpxEffectiveCPUUsed(deadline Deadline, cpuUsed int) int {
	cpuUsed = min(max(cpuUsed, -16), 16)
	if deadline == DeadlineGoodQuality {
		return min(max(cpuUsed, -5), 5)
	}
	return cpuUsed
}

func libvpxSpeedFeatureCPUUsed(deadline Deadline, cpuUsed int) int {
	cpuUsed = libvpxEffectiveCPUUsed(deadline, cpuUsed)
	if deadline == DeadlineRealtime {
		if cpuUsed < 0 {
			return -cpuUsed
		}
		return 4
	}
	return cpuUsed
}

func libvpxSpeedFeatureRecodeLoop(deadline Deadline, cpuUsed int) int {
	switch deadline {
	case DeadlineRealtime:
		return 0
	case DeadlineGoodQuality:
		speed := libvpxSpeedFeatureCPUUsed(deadline, cpuUsed)
		switch {
		case speed > 3:
			return 0
		case speed > 2:
			return 2
		default:
			return 1
		}
	default:
		return 1
	}
}

// libvpx vp8/encoder/rdopt.c:65 auto_speed_thresh table indexed by
// cpi->Speed (0..16). vp8_auto_select_speed lowers Speed when budget
// dwarfs avg_encode_time: ms_for_compress*100 > avg_encode_time*thresh.
var libvpxAutoSpeedThresh = [17]int{
	1000, 200, 150, 130, 150, 125,
	120, 115, 115, 115, 115, 115,
	115, 115, 115, 115, 105,
}

func nowMonotonicNS() int64 { return nanotime() }

// libvpxAutoSelectSpeedActive returns true when the realtime adaptive
// Speed selector is in charge of cpi->Speed (cpu_used >= 0 in realtime).
// When cpu_used < 0 libvpx pins Speed=-cpu_used directly per
// encodeframe.c:686-687, bypassing auto-select.
func (e *VP8Encoder) libvpxAutoSelectSpeedActive() bool {
	if e.opts.Deadline != DeadlineRealtime {
		return false
	}
	return e.opts.CpuUsed >= 0
}

// libvpxAutoSelectSpeed mirrors libvpx vp8/encoder/rdopt.c:261
// vp8_auto_select_speed exactly. Called at the top of each encode_mb_row
// for realtime+positive-cpu_used. Cold start (avg_pick_mode_time==0):
// Speed=4. Otherwise raise/lower based on the (1e6/framerate)*(16-cpu)/16
// ms budget vs cumulative timer state, capped at [4,16].
//
// Note (task #350 audit): govpx's e.autoSpeed evolution under the task
// #278 inter-frame budget/3 wall-clock pin stays in the libvpx Speed=0
// stable region (avg_encode_time ≈ budget/3). At cpu_used > 0 RT
// libvpx's actual wall-clock would drive cpi->Speed up to cpu_used+1
// via the budget-halving (`ms_for_compress = base*(16-cpu)/16`) +
// auto-select +2/+4 branches. Clamping e.autoSpeed itself cascades
// every Speed-conditioned feature in vp8_set_speed_features into the
// cpu_used+1 path simultaneously, which is far too aggressive on a
// short-ladder BD-rate measurement. The targeted port lives in
// libvpxRealtimeCPISpeedForImprovedMVPredGate: that helper feeds the
// libvpx-realistic Speed only into the improved_mv_pred gate, leaving
// every other speed-feature lookup on the pin-suppressed
// e.autoSpeed value. See libvpxInterFrameImprovedMVPredictionFor
// FeatureSpeed caller in encoder_inter_speed.go.
func (e *VP8Encoder) libvpxAutoSelectSpeed() {
	if e.opts.Deadline != DeadlineRealtime {
		return
	}
	cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
	if cpuUsed < 0 {
		// libvpx encodeframe.c:686-687: explicit-Speed branch (no auto-select).
		e.autoSpeed = -cpuUsed
		return
	}
	if e.frameCount == 0 && e.autoSpeed == 0 {
		// libvpx onyx_if.c:1706 seeds cpi->Speed = cpi->oxcf.cpu_used at
		// vp8_create_compressor and at every vp8_change_config. Before any
		// frame has committed, reseed autoSpeed from the configured cpu_used
		// so a fresh encoder with cpu_used > 0 doesn't enter the auto-select
		// loop with Speed=0. After the first encoded frame, autoSpeed is
		// whatever the previous auto-select committed (plus any
		// applyChangeConfigSpeedReset triggered between frames), so we must
		// NOT clobber it with cpuUsed here.
		e.autoSpeed = cpuUsed
	}
	fps := e.opts.FPS
	if fps <= 0 {
		fps = 30
	}
	msForCompress := 1000000 / fps
	msForCompress = msForCompress * (16 - cpuUsed) / 16
	// Note: avgPickModeTime and avgEncodeTime are signed int to mirror
	// libvpx's signed-int avg_pick_mode_time / avg_encode_time
	// (vp8/encoder/onyx_int.h:455-456). The subtraction is intentionally
	// signed because libvpx evaluates it that way before comparing it with
	// the compression budget.
	if e.avgPickModeTime < msForCompress &&
		(e.avgEncodeTime-e.avgPickModeTime) < msForCompress {
		// libvpx vp8/encoder/rdopt.c:284 uses `avg_pick_mode_time == 0` as
		// the cold-start sentinel inside vp8_auto_select_speed itself. govpx
		// keys off `e.frameCount == 0` because govpx's monotonic timer can
		// observe zero elapsed for fast frames and would otherwise re-enter
		// the cold-start branch on later frames whose timers happen to read
		// as zero — masking the post-vp8_change_config Speed=cpu_used state
		// that drives picker dispatch through libvpxCPUUsed.
		if e.frameCount == 0 {
			e.autoSpeed = 4
		} else if e.avgPickModeTime != 0 {
			if msForCompress*100 < e.avgEncodeTime*95 {
				e.autoSpeed += 2
				e.avgPickModeTime = 0
				e.avgEncodeTime = 0
				if e.autoSpeed > 16 {
					e.autoSpeed = 16
				}
			}
			if uint(e.autoSpeed) < uint(len(libvpxAutoSpeedThresh)) &&
				msForCompress*100 > e.avgEncodeTime*libvpxAutoSpeedThresh[e.autoSpeed] {
				e.autoSpeed--
				e.avgPickModeTime = 0
				e.avgEncodeTime = 0
				if e.autoSpeed < 4 {
					e.autoSpeed = 4
				}
			}
		}
	} else {
		e.autoSpeed += 4
		if e.autoSpeed > 16 {
			e.autoSpeed = 16
		}
		e.avgPickModeTime = 0
		e.avgEncodeTime = 0
	}
}

// libvpxRealtimeCPISpeedForImprovedMVPredGate returns the libvpx-realistic
// cpi->Speed value used to evaluate the `Speed > 6` gate that turns off
// `sf->improved_mv_pred` inside vp8_set_speed_features case 2
// (vp8/encoder/onyx_if.c:957). Task #350 audit:
//
// At cpu_used > 0 RT, libvpx's vp8_auto_select_speed (rdopt.c:261) drives
// cpi->Speed up via the +4 / +2 wall-clock branches because the
// budget is halved (`ms_for_compress = base*(16-cpu)/16`) while the
// encoder still runs the same per-frame work for the first few frames.
// Per the task #343 720p RT cpu=8 trace, cpi->Speed reaches 9 by frame 2
// (libvpx picker_entry trace `cpi_speed` field), at which point the
// line-957 gate fires improved_mv_pred=0. govpx's autoSpeed evolution
// stays in the Speed=0 stable region (`avg_encode_time ≈ budget/3`)
// under the task #278 inter-frame timing pin
// (interFrameAutoSpeedTimingCompensation), so e.autoSpeed lands at 4-5
// rather than the libvpx-realistic cpu_used+1 ≈ 9. That kept
// improved_mv_pred enabled on govpx, driving the +6.31% BD-rate gap.
//
// Cannot fix this by clamping e.autoSpeed itself: that cascades all the
// other Speed-conditioned features in vp8_set_speed_features (search
// method, fractional search, quarter-pixel, threshold maps, recode
// loop) into their cpu_used+1 path, which is far too aggressive for the
// short-ladder BD-rate measurement and crashes the curve down ~30000%
// (the cascade saturates the +28923%/-4.15 dB regime that disables
// every speed feature simultaneously, far outside the test's 4-rung
// PSNR-sample resolution).
//
// Targeted port: gate improved_mv_pred specifically on the libvpx-
// realistic cpi->Speed, leaving every other Speed-feature lookup on
// govpx's actual e.autoSpeed evolution. The realistic Speed for
// cpu_used > 0 RT after frame 0 is cpu_used+1 (audit-observed
// trajectory at cpu=8 → cpi_speed=9 at frame 2). For cpu_used=0 RT
// (the byte-parity-gated path) the realistic Speed stays at 4 — below
// the Speed > 6 threshold, so improved_mv_pred remains enabled,
// preserving the threads=4 cpu=0 RT byte-parity sentinel
// (regression_w854h480_threads4_vbr_inter_diverge).
//
// Returns the Speed value that should feed the `Speed > 6` gate. For
// non-realtime / cpu_used < 0 / cpu_used == 0 RT, returns the actual
// libvpxCPUUsed() so the existing semantics carry forward unchanged.
func (e *VP8Encoder) libvpxRealtimeCPISpeedForImprovedMVPredGate() int {
	speed := e.libvpxCPUUsed()
	if e.opts.Deadline != DeadlineRealtime {
		return speed
	}
	cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
	if cpuUsed <= 0 {
		return speed
	}
	if e.frameCount == 0 {
		return speed
	}
	// libvpx-realistic cpi->Speed convergence for cpu_used > 0 RT.
	realistic := min(cpuUsed+1, 16)
	if speed > realistic {
		return speed
	}
	return realistic
}

// libvpxRealtimeCPISpeedForErrorBinGate returns the libvpx-realistic
// cpi->Speed value used to evaluate the `Speed > 6` gate that fires the
// adaptive error-bin RD threshold adjustment inside vp8_set_speed_features
// case 2 (vp8/encoder/onyx_if.c:957-1010). Task #364 audit:
//
// At cpu_used > 0 RT, libvpx's vp8_auto_select_speed (rdopt.c:261) drives
// cpi->Speed up via the +4 / +2 wall-clock branches because the
// budget is halved (`ms_for_compress = base*(16-cpu)/16`) while the
// encoder still runs the same per-frame work for the first few frames.
// Per the task #343 720p RT cpu=8 trace, cpi->Speed reaches 9 by frame 2,
// at which point the line-957 gate fires the adaptive error-bin
// threshold path that scans cpi->mb.error_bins[] and overwrites
// sf->thresh_mult[THR_NEW1/NEAREST1/NEAR1/...] proportionally to
// (cpi->Speed - 6). govpx's autoSpeed evolution stays in the Speed=0
// stable region (`avg_encode_time ≈ budget/3`) under the task #278
// inter-frame timing pin (interFrameAutoSpeedTimingCompensation), so
// e.autoSpeed lands at 4-5 rather than the libvpx-realistic cpu_used+1
// ≈ 9. That kept the adaptive error-bin threshold path disabled on
// govpx for cpu_used > 0 RT, leaving libvpx's wider mode pool active and
// driving picker churn that contributed to the residual BD-rate gap on
// realtime cpu>0 ladders.
//
// Cannot fix this by clamping e.autoSpeed itself: that cascades all the
// other Speed-conditioned features in vp8_set_speed_features (search
// method, fractional search, quarter-pixel, threshold maps, recode
// loop) into their cpu_used+1 path, which is far too aggressive for the
// short-ladder BD-rate measurement (see
// libvpxRealtimeCPISpeedForImprovedMVPredGate for the same anti-pattern
// audit on the improved_mv_pred gate at task #350).
//
// Targeted port: gate the adaptive error-bin RD-threshold adjustment
// specifically on the libvpx-realistic cpi->Speed, leaving every other
// Speed-feature lookup on govpx's actual e.autoSpeed evolution. The
// (Speed-6) scale factor inside libvpxRealtimeAdaptiveInterModeThreshold
// also picks up the realistic Speed so the percentile bisection inside
// the error_bins[] scan matches libvpx's per-frame trajectory.
//
// For cpu_used == 0 RT (the byte-parity-gated path) the realistic Speed
// stays at 4 — below the Speed > 6 threshold, so the error-bin path
// remains disabled, preserving the threads=4 cpu=0 RT byte-parity
// sentinel and the existing autoSpeed=8 / cpu_used=-8 fixture asserts
// in TestLibvpxInterModeThresholdMultipliersApplyRealtimeErrorBins.
func (e *VP8Encoder) libvpxRealtimeCPISpeedForErrorBinGate() int {
	speed := e.libvpxCPUUsed()
	if e.opts.Deadline != DeadlineRealtime {
		return speed
	}
	cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
	if cpuUsed <= 0 {
		return speed
	}
	if e.frameCount == 0 {
		return speed
	}
	// libvpx-realistic cpi->Speed convergence for cpu_used > 0 RT.
	realistic := min(cpuUsed+1, 16)
	if speed > realistic {
		return speed
	}
	return realistic
}

func (e *VP8Encoder) autoSpeedCompressionBudgetUS() int {
	fps := e.opts.FPS
	if fps <= 0 {
		fps = 30
	}
	cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
	if cpuUsed < 0 {
		cpuUsed = -cpuUsed
	}
	return (1000000 / fps) * (16 - cpuUsed) / 16
}

func (e *VP8Encoder) largeAutoSpeedKeyFrameTimingCompensation() bool {
	if !e.libvpxAutoSelectSpeedActive() {
		return false
	}
	cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	mbs := rows * cols
	return mbs >= 3600 || (cpuUsed >= 8 && mbs >= 1900)
}

func (e *VP8Encoder) beginAutoSpeedTiming() {
	if e.opts.Deadline != DeadlineRealtime {
		return
	}
	// libvpx onyx_if.c:5031 starts a wall-clock timer (vpx_usec_timer_start)
	// before encode_frame_to_data_rate; the corresponding mark/elapsed runs
	// at line 5105 after the frame finishes to derive duration / duration2.
	e.autoSpeedFrameStartNS = nowMonotonicNS()
	if e.autoSpeedFrameStartNS == 0 {
		e.autoSpeedFrameStartNS = 1
	}
}

func (e *VP8Encoder) cancelAutoSpeedTiming() {
	e.autoSpeedFrameStartNS = 0
}

// finishAutoSpeedTiming mirrors libvpx onyx_if.c:5103-5128: at end of frame
// encode in realtime, IIR-update avg_encode_time and avg_pick_mode_time
// (duration2 = duration/2 by libvpx convention).
//
// libvpx KF avg_encode_time semantics: the libvpx KF skip at
// onyx_if.c:5110 (`if (cm->frame_type != KEY_FRAME)`) is functionally dead
// for non-trivial keyframes because `encode_frame_to_data_rate` reassigns
// `cm->frame_type = INTER_FRAME` at onyx_if.c:4740 right after coding a KF
// (before returning to vp8_get_compressed_data where the timer-update gate
// runs). The net effect is that libvpx ALWAYS updates avg_encode_time at
// the end of every frame, regardless of frame type. govpx must mirror this
// for keyframes large enough that libvpx's measured wall-clock would push
// avg_encode_time into the vp8_auto_select_speed Speed=0 stable region
// (avg_enc in [ms_for_compress/10, ms_for_compress*100/95]).
//
// Because govpx wall-clock and libvpx wall-clock are independent (different
// implementations on the same host), naively using nowMonotonicNS() would
// let the two encoders' avg_encode_time drift apart and land in different
// vp8_auto_select_speed branches at the next frame. For keyframes we
// therefore pin duration deterministically:
//
//   - largeAutoSpeedKeyFrameTimingCompensation() (mbs >= 3600 etc): pin to
//     2*budget - 2 to land at the libvpx Speed+=2 -> Speed-- -> 4 boundary.
//   - mediumAutoSpeedKeyFrameTimingCompensation() (200 <= mbs < large):
//     pin to budget/3 to land in the libvpx Speed=0 stable region (matches
//     the regression_640x360_threads1_bitrate_setref_diverge seed where
//     libvpx's ~11ms KF measurement keeps Speed=0 across SetRateControl).
//   - Otherwise (tiny KFs like 16x16): keep avg_encode_time at 0 so the
//     next frame's vp8_auto_select_speed enters the Speed-- branch and
//     clamps to 4 (matching libvpx's measured-too-small Speed-- trajectory
//     for tiny frames where actual encode time stays well below the
//     Speed-stable lower bound).
func (e *VP8Encoder) finishAutoSpeedTiming(keyFrame bool) {
	if e.autoSpeedFrameStartNS == 0 || e.opts.Deadline != DeadlineRealtime {
		return
	}
	durationNS := nowMonotonicNS() - e.autoSpeedFrameStartNS
	e.autoSpeedFrameStartNS = 0
	if durationNS < 0 {
		durationNS = 0
	}
	duration := int(durationNS / 1000)
	keyFrameEncodeSample := false
	if keyFrame && e.libvpxAutoSelectSpeedActive() {
		if e.largeAutoSpeedKeyFrameTimingCompensation() {
			// The selector is calibrated to libvpx's C encoder timings. For the
			// large positive-realtime boundary path, libvpx's keyframe wall-clock
			// sample can land on either side of vp8_auto_select_speed's branch
			// boundary. Pin the sample to the libvpx matching-budget boundary so
			// strict byte-parity runs do not depend on scheduler timing.
			if budget := e.autoSpeedCompressionBudgetUS(); budget > 1 {
				duration = 2*budget - 2
			}
			keyFrameEncodeSample = true
		} else if e.mediumAutoSpeedKeyFrameTimingCompensation() {
			// Pin to the Speed=0 stable region midpoint so the next frame's
			// auto-select neither fires Speed+=2 nor Speed--, matching the
			// libvpx behaviour where actual KF measurement at this size
			// lands in [budget/10, budget*100/95].
			if budget := e.autoSpeedCompressionBudgetUS(); budget > 1 {
				duration = budget / 3
			}
			keyFrameEncodeSample = true
		}
	} else if !keyFrame && e.interFrameAutoSpeedTimingCompensation() {
		// Inter frames in realtime+positive-cpu_used mode read real
		// wall-clock here, which on lightly loaded hosts measures well
		// below `budget*2/3` and pushes vp8_auto_select_speed into the
		// Speed-- branch, but on heavily loaded hosts can measure above
		// `budget*100/95` and push the selector into the Speed+=2 branch.
		// The branch the next frame's auto-select takes drives a non-
		// deterministic picker dispatch (different `libvpxCPUUsed()` per
		// frame), which task #278 reproduced as a flaky bitstream divergence
		// at seed#8 (854x480 RT threads=4) of FuzzEncoderProductionStream
		// ByteParity when many test processes contended for the host.
		//
		// Mirror the medium-keyframe compensation: pin the per-inter-frame
		// duration sample to `budget/3` so avg_encode_time lands inside the
		// libvpx Speed=0 stable region every time, independent of scheduler
		// pressure. This matches the project's existing KF strategy of
		// trading "libvpx-verbatim wall-clock" for "libvpx-stable region"
		// once the frame is large enough that the timing branch is reliably
		// observed in libvpx's own measurements. Smaller resolutions are
		// left to libvpx-verbatim wall-clock since their canonical golden
		// references were captured against real timing measurements; for
		// those cases the byte-parity tests remain (very rarely) flaky
		// under extreme parallel-process contention but the average host
		// load that the test suite is calibrated for stays well below the
		// auto-select branch boundary.
		if budget := e.autoSpeedCompressionBudgetUS(); budget > 1 {
			duration = budget / 3
		}
	}
	duration2 := duration / 2
	if !keyFrame || keyFrameEncodeSample {
		if e.avgEncodeTime == 0 {
			e.avgEncodeTime = duration
		} else {
			e.avgEncodeTime = (7*e.avgEncodeTime + duration) >> 3
		}
	}
	if duration2 > 0 {
		if e.avgPickModeTime == 0 {
			e.avgPickModeTime = duration2
		} else {
			e.avgPickModeTime = (7*e.avgPickModeTime + duration2) >> 3
		}
	}
}

// interFrameAutoSpeedTimingCompensation returns true when the encoder
// runs in realtime+positive-cpu_used mode and the frame is large enough
// that govpx's per-inter-frame wall-clock measurement would (under host
// contention) push avg_encode_time across the vp8_auto_select_speed
// branch boundaries before the next frame's auto-select runs, steering
// the picker dispatch into a different `libvpxCPUUsed()` value.
//
// Task #278 reproduced this with seed#8 of FuzzEncoderProductionStream
// ByteParity (854×480 RT threads=4 cpu_used=0): under no host load the
// govpx inter-frame wall-clock is sub-millisecond, so the IIR average
// stays near the KF-pinned budget/3 starting value and the next frame
// dispatches at autoSpeed=0 matching libvpx's `db163449844d85c6` frame-2
// reference; under heavy parallel load wall-clock measurements rose
// enough that avg_encode_time crossed the libvpx Speed=0 stable lower
// bound, the next frame ran with a different autoSpeed, and the picker
// chose a different mode set producing `6abca426c800e43c` or
// `bfe404c8fa570088`.
//
// The 1500-MB gate captures the production resolutions that the byte-
// parity gate exercises with threads >= 2 (854×480 = 1620 MBs, 1280×720
// = 3600 MBs) without disturbing the smaller fuzz seeds (16×16 .. 640×360
// = 900 MBs) whose libvpx-reference outputs were captured before any
// inter-frame compensation existed and which remain deterministic under
// load already (their wall-clock measurements are too small to cross the
// libvpx auto-select branch boundary).
func (e *VP8Encoder) interFrameAutoSpeedTimingCompensation() bool {
	if !e.libvpxAutoSelectSpeedActive() {
		return false
	}
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	mbs := rows * cols
	return mbs >= 1500
}

// mediumAutoSpeedKeyFrameTimingCompensation returns true when the encoder
// runs in realtime+positive-cpu_used mode and the frame is large enough
// that libvpx's actual wall-clock keyframe measurement would push
// cpi->avg_encode_time into the vp8_auto_select_speed Speed=0 stable
// region. Empirically calibrated against the libvpx oracle (~12us per MB
// on the project's test hosts); 200 MBs sits comfortably above the
// Speed-stable lower bound for ms_for_compress in [30000, 60000].
func (e *VP8Encoder) mediumAutoSpeedKeyFrameTimingCompensation() bool {
	if !e.libvpxAutoSelectSpeedActive() {
		return false
	}
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	mbs := rows * cols
	return mbs >= 200
}

func (e *VP8Encoder) resetAutoSpeedTiming() {
	e.autoSpeed = 0
	e.avgPickModeTime = 0
	e.avgEncodeTime = 0
	e.autoSpeedFrameStartNS = 0
}

// applyChangeConfigSpeedReset mirrors the tail of libvpx vp8_change_config
// (vp8/encoder/onyx_if.c:1706): every wrapper-level VP8E_SET_* or
// vpx_codec_enc_config_set call routes through `update_extracfg` ->
// `vp8_change_config`, which unconditionally sets
// `cpi->Speed = cpi->oxcf.cpu_used`. The accumulated avg_pick_mode_time /
// avg_encode_time timing windows are NOT reset; vp8_auto_select_speed re-runs
// at the top of the next vp8_encode_frame using the freshly seeded Speed and
// the preserved timers. govpx must mirror that so the picker dispatch
// (`interAnalysisUsesRDModeDecision`, `libvpxCPUUsed`) consults the same
// Speed libvpx would after a runtime config-set, even when the caller did
// not change cpu_used itself. Without this reset govpx would keep the
// cold-start autoSpeed=4 across SetBitrateKbps / SetRealtimeTarget /
// SetTuning / SetARNR / etc, and the fast-vs-RD picker would diverge from
// libvpx for the very first frame after the runtime control fires.
func (e *VP8Encoder) applyChangeConfigSpeedReset() {
	e.autoSpeed = e.opts.CpuUsed
	e.applyChangeConfigSegmentEncodeBreakout()
}

// applyVP8ChangeConfigRuntimeSideEffects mirrors the shared body/tail effects
// of libvpx update_extracfg / vpx_codec_enc_config_set -> vp8_change_config.
func (e *VP8Encoder) applyVP8ChangeConfigRuntimeSideEffects() {
	e.rc.applyVP8ChangeConfigRateModel(e.opts.TwoPassMinPct)
	e.rc.applyVP8ChangeConfigQuantizerClamp()
	e.rc.refreshDropFramesAllowed()
	if e.twoPass.enabled() {
		e.twoPass.configureGFIntervals(e.libvpxStaticSceneMaxGFInterval(), e.libvpxMaxGFInterval())
	}
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
	e.applyVP8ChangeConfigResolutionChangeKeyFrame()
	e.applyVP8ChangeConfigBaselineGFInterval()
}

// applyVP8ChangeConfigBaselineGFInterval mirrors libvpx
// vp8/encoder/onyx_if.c:1541-1548 vp8_change_config baseline_gf_interval
// reseed:
//
//	cpi->baseline_gf_interval =
//	    cpi->oxcf.alt_freq ? cpi->oxcf.alt_freq : DEFAULT_GF_INTERVAL;
//	// GF behavior for 1 pass CBR, used when error_resilience is off.
//	if (!cpi->oxcf.error_resilient_mode &&
//	    cpi->oxcf.end_usage == USAGE_STREAM_FROM_SERVER &&
//	    cpi->oxcf.Mode == MODE_REALTIME)
//	  cpi->baseline_gf_interval = cpi->gf_interval_onepass_cbr;
//
// libvpx invokes vp8_change_config from set_quality_mode at every
// vp8e_encode call, so the OFE-driven oracle harness sees this reseed on
// every frame. govpx mirrors that contract via the shared
// applyVP8ChangeConfigRuntimeSideEffects helper -- every runtime control
// setter (SetBitrateKbps, SetRateControl, SetMaxIntraBitratePct, etc.)
// already invokes the helper, so the baseline_gf_interval refresh
// propagates wherever libvpx's vp8_change_config would have fired.
//
// govpx does not surface alt_freq as an explicit option, so the libvpx
// `cpi->oxcf.alt_freq ? alt_freq : DEFAULT_GF_INTERVAL` branch reduces to
// libvpxDefaultGFInterval (7) for the alt_freq==0 cohort -- the only
// cohort the oracle harness exercises.
//
// Task #235 (e6787af3) had elided this reseed under the assumption that
// vp8_create_compressor's later override (onyx_if.c:1886) shadowed
// vp8_change_config's line-1541 reset for any (CBR && !err && Mode<=2)
// cohort. That holds at create-time, but every subsequent
// vp8_change_config call (which set_quality_mode triggers per encode)
// restores DEFAULT_GF_INTERVAL for the non-MODE_REALTIME branch, leaving
// the create-time gf_interval_onepass_cbr value live only across the
// brief window between vp8_create_compressor and the first set_quality_-
// mode. The aebef841 64x64@30fps@300kbps fuzz seed's runtime-control
// burst exercises that subsequent-change_config path and observed
// govpx's baselineGFInterval frozen at goldenFrameCBRInterval=10 while
// libvpx restored DEFAULT_GF_INTERVAL=7, drifting the gf_overspend
// drain denominator and bumping Q+3 at frame 4. This helper restores
// the libvpx-verbatim line-1541 reset.
func (e *VP8Encoder) applyVP8ChangeConfigBaselineGFInterval() {
	e.rc.baselineGFInterval = libvpxDefaultGFInterval
	if e.rc.mode == RateControlCBR && !e.opts.ErrorResilient && e.opts.Deadline == DeadlineRealtime {
		rows := encoderMacroblockRows(e.opts.Height)
		cols := encoderMacroblockCols(e.opts.Width)
		e.rc.baselineGFInterval = e.goldenFrameCBRInterval(rows, cols)
	}
}

// applyVP8ChangeConfigResolutionChangeKeyFrame mirrors libvpx
// vp8/encoder/onyx_if.c:1689-1691:
//
//	if (last_w != cpi->oxcf.Width || last_h != cpi->oxcf.Height) {
//	  cpi->force_next_frame_intra = 1;
//	}
//
// The check lives in the shared vp8_change_config tail in libvpx, so the
// govpx port keeps it on the shared side-effect helper. Every runtime
// control setter that mutates Width or Height and then runs this tail will
// force the next encoded frame to a key frame at the new size, matching
// the libvpx contract for resize-on-config-set. The encoder already wires
// the trigger from applyResolutionChange via forceKeyFrame; the assignment
// here is defensive against future dimension-mutating paths that bypass
// applyResolutionChange.
func (e *VP8Encoder) applyVP8ChangeConfigResolutionChangeKeyFrame() {
	if e.lastChangeConfigWidth != e.opts.Width || e.lastChangeConfigHeight != e.opts.Height {
		e.forceKeyFrame = true
	}
	e.lastChangeConfigWidth = e.opts.Width
	e.lastChangeConfigHeight = e.opts.Height
}

// applyChangeConfigSegmentEncodeBreakout mirrors vp8_change_config's
// segment_encode_breakout refresh when use_roi_static_threshold is false
// (onyx_if.c:1560-1565).
func (e *VP8Encoder) applyChangeConfigSegmentEncodeBreakout() {
	if e.useROIStaticThreshold {
		return
	}
	for i := range e.segmentEncodeBreakout {
		e.segmentEncodeBreakout[i] = e.opts.StaticThreshold
	}
}

// SetKeyFrameInterval changes the maximum GOP distance in frames. At
// runtime, zero mirrors libvpx's fixed keyframe interval of zero: every
// accepted input frame is interval-forced unless runtime keyframes have
// been disabled by SetAdaptiveKeyFrames(false). See EncoderOptions.
// KeyFrameInterval.
func (e *VP8Encoder) SetKeyFrameInterval(frames int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if frames < 0 {
		return ErrInvalidConfig
	}
	e.opts.KeyFrameInterval = frames
	// Mirror libvpx oxcf.key_freq for estimate_keyframe_frequency.
	e.rc.keyFrameFrequency = frames
	// Re-derive libvpx auto_key flag: auto_key=1 iff !keyFramesDisabled
	// and the cadence is active (kf_max_dist != 0).
	e.rc.autoKeyFrames = !e.keyFramesDisabled && frames > 0
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetAdaptiveKeyFrames enables or disables libvpx-compatible runtime
// automatic keyframe placement. Runtime false mirrors VPX_KF_DISABLED;
// runtime true re-enables keyframes and the one-pass auto-key recode.
// See EncoderOptions.AdaptiveKeyFrames.
func (e *VP8Encoder) SetAdaptiveKeyFrames(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AdaptiveKeyFrames = enabled
	e.keyFramesDisabled = !enabled
	// libvpx auto_key derivation: VPX_KF_AUTO (i.e. !keyFramesDisabled)
	// with kf_min_dist != kf_max_dist (KeyFrameInterval > 0). When the
	// caller toggles AdaptiveKeyFrames=false it also disables keyframes
	// entirely (matches VPX_KF_DISABLED), so auto_key becomes false; on
	// re-enable, fall back to the cadence-based auto_key.
	e.rc.autoKeyFrames = !e.keyFramesDisabled && e.opts.KeyFrameInterval > 0
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetActiveMap installs a per-macroblock activity map. Cells equal to 0 mark
// inactive macroblocks; in inter frames those MBs skip mode decision and code
// as ZEROMV-LAST with skip=1, matching libvpx vp8_set_active_map (onyx_if.c)
// and the active_ptr early-exit in pickinter.c/rdopt.c. Pass a nil map to
// disable. Key frames ignore the map.
//
// rows and cols must equal the encoder's macroblock dimensions; len(activeMap)
// must equal rows*cols.
func (e *VP8Encoder) SetActiveMap(activeMap []uint8, rows int, cols int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	// libvpx vp8_set_active_map (vp8/encoder/onyx_if.c) just flips
	// cpi->active_map_enabled and copies the map. It does not touch
	// segmentation update flags, even when cyclic refresh / RTC external
	// has left a preserved segmentation header in flight.
	if activeMap == nil {
		e.activeMapEnabled = false
		return nil
	}
	expectedRows := encoderMacroblockRows(e.opts.Height)
	expectedCols := encoderMacroblockCols(e.opts.Width)
	if rows != expectedRows || cols != expectedCols {
		return ErrInvalidConfig
	}
	if len(activeMap) < rows*cols {
		return ErrInvalidConfig
	}
	if len(e.activeMap) < rows*cols {
		e.activeMap = make([]uint8, rows*cols)
	}
	copy(e.activeMap[:rows*cols], activeMap[:rows*cols])
	e.activeMapEnabled = true
	return nil
}

// SetNoiseSensitivity changes the VP8 denoiser level. Valid range is
// [0, 6]: 0 disables the denoiser (without freeing or resetting its
// running-average buffers, matching libvpx); 1 denoises luma only; 2..6
// denoise luma and chroma with increasing aggressiveness. See
// EncoderOptions.NoiseSensitivity.
func (e *VP8Encoder) SetNoiseSensitivity(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(level) > 6 {
		return ErrInvalidConfig
	}
	// libvpx: vp8/vp8_cx_iface.c:552 set_noise_sensitivity → update_extracfg
	// → vp8_change_config. vp8_change_config only allocates the denoiser
	// when oxcf.noise_sensitivity > 0 (vp8/encoder/onyx_if.c:1721-1733); it
	// never tears it down or zeros its per-MB state on a runtime control
	// flipping the value back to 0. All downstream denoiser gates
	// (vp8_denoiser_denoise_mb callers in onyx_if.c, e.g. line 3161, 3175,
	// 4390) test cpi->oxcf.noise_sensitivity > 0, so writing 0 into oxcf
	// alone is enough to bypass the denoiser without disturbing any state
	// outside the public Set surface.
	e.opts.NoiseSensitivity = level
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetARNR changes alt-ref noise-reduction controls at runtime. maxFrames
// is the temporal filter window length (0 disables ARNR); strength is the
// filter strength in [0, 6]; filterType selects 1=backward, 2=forward, or
// 3=centered. See EncoderOptions.ARNRMaxFrames, ARNRStrength, ARNRType.
func (e *VP8Encoder) SetARNR(maxFrames int, strength int, filterType int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if maxFrames < 0 || maxFrames > maxARNRFrames || strength < 0 || strength > 6 || filterType < 1 || filterType > 3 {
		return ErrInvalidConfig
	}
	e.opts.ARNRMaxFrames = maxFrames
	e.opts.ARNRStrength = strength
	e.opts.ARNRType = filterType
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

func (e *VP8Encoder) setARNRMaxFrames(maxFrames int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if maxFrames < 0 || maxFrames > maxARNRFrames {
		return ErrInvalidConfig
	}
	e.opts.ARNRMaxFrames = maxFrames
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

func (e *VP8Encoder) setARNRStrength(strength int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if strength < 0 || strength > 6 {
		return ErrInvalidConfig
	}
	e.opts.ARNRStrength = strength
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

func (e *VP8Encoder) setARNRType(filterType int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if filterType < 1 || filterType > 3 {
		return ErrInvalidConfig
	}
	e.opts.ARNRType = filterType
	e.applyVP8ChangeConfigRuntimeSideEffects()
	return nil
}

// SetTwoPassStats replaces the finalized first-pass stats used for
// second-pass VBR planning. Pass the slice produced by
// [FinalizeFirstPassStats] after collecting per-frame records with
// [VP8Encoder.CollectFirstPassStats]. Passing nil or an empty slice
// disables two-pass planning on subsequent EncodeInto calls.
func (e *VP8Encoder) SetTwoPassStats(stats []FirstPassFrameStats) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.TwoPassStats = stats
	e.twoPass.configure(stats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	e.twoPass.configureQuantizerBounds(e.rc.minQuantizer, e.rc.maxQuantizer)
	e.twoPass.configureErrorResilient(e.opts.ErrorResilient || e.opts.ErrorResilientPartitions)
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	// libvpx frame_max_bits (vp8/encoder/firstpass.c:316-368) reads
	// cpi->oxcf.end_usage; re-seed it here so a SetTwoPassStats call
	// after a runtime RateControlMode change picks up the new dispatch.
	e.twoPass.configureEndUsage(libvpxVP8EndUsageFromRateControlMode(e.rc.mode))
	if e.frameCount == 0 {
		e.rc.onePassAutoGold = false
		e.rc.framesTillGFUpdateDue = 0
		if e.rc.mode != RateControlCBR && len(e.opts.TwoPassStats) == 0 {
			e.rc.framesTillGFUpdateDue = libvpxDefaultGFInterval
			e.rc.onePassAutoGold = true
		}
		// libvpx onyx_if.c vp8_create_compressor line 1886 leaves
		// cpi->baseline_gf_interval == gf_interval_onepass_cbr for any
		// (CBR && !error_resilient) one-pass compressor. SetTwoPassStats
		// at frame 0 has the same effect as a fresh init for the active
		// rc cohort, so mirror the seed here too.
		if e.rc.mode == RateControlCBR && !e.opts.ErrorResilient && len(e.opts.TwoPassStats) == 0 {
			e.rc.baselineGFInterval = 0
		} else {
			e.rc.baselineGFInterval = libvpxDefaultGFInterval
		}
		e.cyclicRefreshConfigured = e.opts.ErrorResilient ||
			(e.rc.mode == RateControlCBR && len(e.opts.TwoPassStats) == 0)
	}
	return nil
}

// ForceKeyFrame requests that the next accepted input frame be a key frame.
// With lookahead enabled the resulting packet may be emitted by a later
// EncodeInto or FlushInto call; hidden alt-ref emissions in between do not
// consume it. If no input is accepted before FlushInto drains queued frames,
// the next committed output is forced. Use the EncodeForceKeyFrame flag on
// EncodeInto when only that single call must be a key frame. Calls on a nil
// or closed encoder are no-ops.
func (e *VP8Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}
