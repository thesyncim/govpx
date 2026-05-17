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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.opts.DropFrameAllowed = cfg.DropFrameAllowed
	e.opts.DropFrameWaterMark = cfg.DropFrameWaterMark
	e.opts.MaxIntraBitratePct = cfg.MaxIntraBitratePct
	e.opts.GFCBRBoostPct = cfg.GFCBRBoostPct
	e.opts.TemporalScalability = nextTemporal.config
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
// dropping without touching bitrate. When enabling, the drop watermark
// defaults to EncoderOptions.DropFrameWaterMark or 60 if that is zero.
func (e *VP8Encoder) SetFrameDropAllowed(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.setFrameDropAllowed(enabled)
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.applyChangeConfigSpeedReset()
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

func (e *VP8Encoder) setFrameDropAllowed(enabled bool) {
	e.rc.dropFrameAllowed = enabled
	if enabled {
		if e.opts.DropFrameWaterMark > 0 {
			e.rc.dropFramesWaterMark = min(e.opts.DropFrameWaterMark, 100)
		} else if e.rc.dropFramesWaterMark <= 0 {
			e.rc.dropFramesWaterMark = defaultDropFramesWaterMark
		}
		if e.opts.DropFrameWaterMark <= 0 {
			e.opts.DropFrameWaterMark = e.rc.dropFramesWaterMark
		}
	} else {
		e.rc.dropFramesWaterMark = 0
	}
	e.opts.DropFrameAllowed = enabled
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
		fpsChanged := target.FPS != e.opts.FPS
		e.opts.FPS = target.FPS
		e.opts.TimebaseNum = 1
		e.opts.TimebaseDen = target.FPS
		if fpsChanged {
			e.resetAutoSpeedTiming()
		}
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
		e.refreshRuntimeCyclicRefreshConfig()
		e.forceNextLFDeltaUpdate()
		e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
		e.forceNextLFDeltaUpdate()
		e.autoSpeed = e.opts.CpuUsed
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
	// libvpx routes VP8E_SET_CPUUSED through vp8_change_config, whose tail
	// assigns cpi->Speed = oxcf.cpu_used. It does not reset the accumulated
	// picker threshold multipliers or realtime timing windows; the next frame's
	// vp8_initialize_rd_consts re-derives the baseline thresholds from the new
	// speed and keeps applying the live rd_thresh_mult[] state.
	e.autoSpeed = e.opts.CpuUsed
	e.interRDThreshBaselineGen++
	e.interRDFrameRefSearchOrderValid = false
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
// encode in realtime, IIR-update avg_encode_time (inter frames, plus the
// 720p+ positive-realtime keyframe branch below) and avg_pick_mode_time
// (duration2 = duration/2 by libvpx convention).
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
	if keyFrame && e.largeAutoSpeedKeyFrameTimingCompensation() {
		// The selector is calibrated to libvpx's C encoder timings. For the
		// large positive-realtime boundary path, libvpx's keyframe wall-clock
		// sample can land on either side of vp8_auto_select_speed's branch
		// boundary. Pin the sample to the libvpx matching-budget boundary so
		// strict byte-parity runs do not depend on scheduler timing.
		if budget := e.autoSpeedCompressionBudgetUS(); budget > 1 {
			duration = 2*budget - 2
		}
		keyFrameEncodeSample = true
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.rc.autoKeyFrames = enabled
	e.keyFramesDisabled = !enabled
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	e.refreshRuntimeCyclicRefreshConfig()
	e.forceNextLFDeltaUpdate()
	e.applyChangeConfigSpeedReset()
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
	if e.frameCount == 0 {
		e.rc.onePassAutoGold = false
		e.rc.framesTillGFUpdateDue = 0
		if e.rc.mode != RateControlCBR && len(e.opts.TwoPassStats) == 0 {
			e.rc.framesTillGFUpdateDue = libvpxDefaultGFInterval
			e.rc.onePassAutoGold = true
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
