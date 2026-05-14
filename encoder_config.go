package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
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
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.TemporalScalability = nextTemporal.config
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
	return nil
}

// SetRTCExternalRateControl enables libvpx's VP8 RTC external-rate-control
// mode. Like libvpx's VP8E_SET_RTC_EXTERNAL_RATECTRL control, enabling is
// sticky: a later false call is accepted but does not re-enable cyclic refresh
// or overshoot recode. See EncoderOptions.RTCExternalRateControl.
func (e *VP8Encoder) SetRTCExternalRateControl(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if enabled {
		e.rtcExternalPreserveSegmentation = e.segmentationHeaderEnabled
		if e.segmentationHeaderEnabled {
			e.rtcExternalPreservedSegmentation = e.lastSegmentationConfig
		} else {
			e.rtcExternalPreservedSegmentation = vp8enc.SegmentationConfig{}
		}
		e.opts.RTCExternalRateControl = true
	}
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
	e.forceNextLFDeltaUpdate()
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
		e.timing = timingState{timebaseNum: 1, timebaseDen: target.FPS, frameDuration: 1}
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
	e.rc.minQuantizer = libvpxPublicQuantizerToQIndex(nextMinQuantizer)
	e.rc.maxQuantizer = libvpxPublicQuantizerToQIndex(nextMaxQuantizer)
	e.opts.MinQuantizer = nextMinQuantizer
	e.opts.MaxQuantizer = nextMaxQuantizer
	e.rc.clampQuantizer()
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
		if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
			return err
		}
		e.rc = nextRC
		e.temporal = nextTemporal
		e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
		e.opts.TemporalScalability = nextTemporal.config
		e.forceNextLFDeltaUpdate()
		return nil
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(e.rc.targetBitrateKbps, e.timing); err != nil {
		return err
	}
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	e.forceNextLFDeltaUpdate()
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
	nextTemporal := temporalState{}
	if err := nextTemporal.configure(cfg, e.rc.targetBitrateKbps); err != nil {
		return err
	}
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	e.initializeTemporalLayerCodingStates()
	e.forceNextLFDeltaUpdate()
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
// re-derives the effective CPU-used preset from the current
// EncoderOptions.CpuUsed, since the realtime path interprets that value
// differently than good/best quality.
func (e *VP8Encoder) SetDeadline(deadline Deadline) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if deadline < DeadlineBestQuality || deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	previousDeadline := e.opts.Deadline
	e.opts.Deadline = deadline
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(deadline, e.opts.CpuUsed)
	if deadline != DeadlineRealtime {
		e.runtimePinnedCPUUsed = false
	}
	if deadline != previousDeadline {
		e.resetInterRDThresholdMultipliers()
		e.forceNextLFDeltaUpdate()
		if deadline == DeadlineRealtime {
			e.resetAutoSpeedTiming()
		} else {
			e.autoSpeed = e.opts.CpuUsed
		}
	}
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
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(e.opts.Deadline, cpuUsed)
	e.runtimePinnedCPUUsed = e.opts.Deadline == DeadlineRealtime && cpuUsed < 0
	// libvpx routes VP8E_SET_CPUUSED through vp8_change_config, whose tail
	// assigns cpi->Speed = oxcf.cpu_used and resets realtime auto-speed timing.
	// This matters when switching from a pinned realtime speed (negative
	// cpu_used) back to auto-speed: the next vp8_auto_select_speed starts from
	// the new config value with a cold timing window.
	e.autoSpeed = e.opts.CpuUsed
	e.avgPickModeTime = 0
	e.avgEncodeTime = 0
	e.autoSpeedFrameStartNS = 0
	e.resetInterRDThresholdMultipliers()
	if e.opts.Deadline == DeadlineRealtime && e.opts.CpuUsed >= 0 {
		// vp8_set_speed_features runs inside vp8_change_config. In realtime
		// mode its continuous-speed branch clears mb.error_bins when the
		// configured speed is nonnegative (RT(speed) > 6), before the next
		// frame's auto-selected speed consumes the histogram.
		e.interModeErrorBins = [1024]uint32{}
		e.interModeSpeedErrorBins = [1024]uint32{}
	}
	e.forceNextLFDeltaUpdate()
	return nil
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
	e.forceNextLFDeltaUpdate()
	return nil
}

func (e *VP8Encoder) libvpxCPUUsed() int {
	// libvpx encodeframe.c:685-691: realtime mode runs vp8_auto_select_speed
	// which evolves cpi->Speed. Mirror that: for realtime+positive-cpu_used,
	// return the adaptive autoSpeed (seeded to 4 at cold start, cf.
	// libvpxAutoSelectSpeed). For realtime+negative-cpu_used and other
	// deadlines, fall back to the static formula.
	if e.opts.Deadline == DeadlineRealtime {
		cpuUsed := libvpxEffectiveCPUUsed(e.opts.Deadline, e.opts.CpuUsed)
		if cpuUsed >= 0 {
			if e.autoSpeed == 0 {
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
	if e.autoSpeed == 0 {
		// libvpx onyx_if.c:1706 seeds cpi->Speed = cpi->oxcf.cpu_used at
		// vp8_change_config. vp8_auto_select_speed then iterates from
		// that starting value. At cpu_used=16 the "else" branch
		// (avg_pick_mode_time >= msForCompress=0) keeps Speed pinned at
		// 16 rather than collapsing it to 4 as the cold-start reset in
		// the matching-budget branch would.
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
		if e.avgPickModeTime == 0 {
			e.autoSpeed = 4
		} else {
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
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	return rows*cols >= 3600
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
// encode in realtime, IIR-update avg_encode_time (inter frames only) and
// avg_pick_mode_time (duration2 = duration/2 by libvpx convention).
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
		// The selector is calibrated to libvpx's C encoder timings. On large
		// keyframes govpx can spend longer in Go-side reconstruction while
		// libvpx still stays just inside the next-frame budget, so cap the
		// effective keyframe sample at the branch boundary used by
		// vp8_auto_select_speed before feeding the next-frame selector.
		if budget := e.autoSpeedCompressionBudgetUS(); budget > 1 && duration >= 2*budget-1 {
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

// SetKeyFrameInterval changes the maximum GOP distance in frames. Zero
// disables interval-forced key frames; content-driven and explicitly
// forced key frames are unaffected. See EncoderOptions.KeyFrameInterval.
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
	e.forceNextLFDeltaUpdate()
	return nil
}

// SetAdaptiveKeyFrames enables or disables libvpx-compatible one-pass
// auto-key recode. See EncoderOptions.AdaptiveKeyFrames.
func (e *VP8Encoder) SetAdaptiveKeyFrames(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AdaptiveKeyFrames = enabled
	e.rc.autoKeyFrames = enabled
	e.forceNextLFDeltaUpdate()
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
// [0, 6]: 0 disables and tears down the denoiser; 1 denoises luma only;
// 2..6 denoise luma and chroma with increasing aggressiveness. See
// EncoderOptions.NoiseSensitivity.
func (e *VP8Encoder) SetNoiseSensitivity(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(level) > 6 {
		return ErrInvalidConfig
	}
	e.opts.NoiseSensitivity = level
	if level == 0 {
		e.denoiser.reset()
	}
	e.forceNextLFDeltaUpdate()
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
	e.forceNextLFDeltaUpdate()
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
