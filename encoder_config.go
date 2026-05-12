package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

// SetBitrateKbps changes the total encoder target bitrate.
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
	return nil
}

// SetRateControl replaces the encoder's rate-control configuration.
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
	return nil
}

// SetCQLevel changes the CQ/Q public quantizer level.
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
	return nil
}

// SetMaxIntraBitratePct changes the key-frame bitrate cap percentage.
func (e *VP8Encoder) SetMaxIntraBitratePct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.maxIntraBitratePct = pct
	e.opts.MaxIntraBitratePct = pct
	return nil
}

// SetGFCBRBoostPct changes the golden-frame boost percentage in CBR mode.
func (e *VP8Encoder) SetGFCBRBoostPct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.gfCBRBoostPct = pct
	e.opts.GFCBRBoostPct = pct
	return nil
}

// SetTokenPartitions changes the VP8 token partition count selector.
func (e *VP8Encoder) SetTokenPartitions(partitions int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if partitions < int(vp8common.OnePartition) || partitions > int(vp8common.EightPartition) {
		return ErrInvalidConfig
	}
	e.opts.TokenPartitions = partitions
	return nil
}

// SetSharpness changes the VP8 loop-filter sharpness level in [0, 7].
func (e *VP8Encoder) SetSharpness(sharpness int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(sharpness) > 7 {
		return ErrInvalidConfig
	}
	e.opts.Sharpness = sharpness
	return nil
}

// SetStaticThreshold changes the static macroblock breakout threshold.
func (e *VP8Encoder) SetStaticThreshold(threshold int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if threshold < 0 {
		return ErrInvalidConfig
	}
	e.opts.StaticThreshold = threshold
	return nil
}

// SetScreenContentMode changes the libvpx-style screen-content mode.
func (e *VP8Encoder) SetScreenContentMode(mode int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if uint(mode) > 2 {
		return ErrInvalidConfig
	}
	e.opts.ScreenContentMode = mode
	return nil
}

// SetRTCExternalRateControl changes the libvpx-style VP8 RTC external-rate-control mode.
func (e *VP8Encoder) SetRTCExternalRateControl(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.RTCExternalRateControl = enabled
	return nil
}

// SetFrameDropAllowed changes realtime frame dropping without touching bitrate.
func (e *VP8Encoder) SetFrameDropAllowed(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.setFrameDropAllowed(enabled)
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
			return ErrInvalidConfig
		}
		e.opts.Width = target.Width
		e.opts.Height = target.Height
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
	return nil
}

// SetTemporalScalability reconfigures automatic temporal-layer scheduling.
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
	return nil
}

// SetTemporalLayerID overrides the temporal layer for the next encoded frame.
func (e *VP8Encoder) SetTemporalLayerID(layerID int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.temporal.setLayerID(layerID)
}

// SetDeadline changes the encoder speed/quality operating mode.
func (e *VP8Encoder) SetDeadline(deadline Deadline) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if deadline < DeadlineBestQuality || deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	e.opts.Deadline = deadline
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(deadline, e.opts.CpuUsed)
	return nil
}

// SetCPUUsed changes the libvpx-style speed preset in [-16, 16].
func (e *VP8Encoder) SetCPUUsed(cpuUsed int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cpuUsed < -16 || cpuUsed > 16 {
		return ErrInvalidConfig
	}
	e.opts.CpuUsed = libvpxEffectiveCPUUsed(e.opts.Deadline, cpuUsed)
	return nil
}

// SetTuning changes the encoder visual quality model.
func (e *VP8Encoder) SetTuning(tuning Tuning) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if tuning < TunePSNR || tuning > TuneSSIM {
		return ErrInvalidConfig
	}
	e.opts.Tuning = tuning
	e.activityMapValid = false
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
	fps := e.opts.FPS
	if fps <= 0 {
		fps = 30
	}
	msForCompress := 1000000 / fps
	msForCompress = msForCompress * (16 - cpuUsed) / 16
	// Note: avgPickModeTime and avgEncodeTime are signed int to mirror
	// libvpx's signed-int avg_pick_mode_time / avg_encode_time
	// (vp8/encoder/onyx_int.h:455-456). The (avgEncodeTime - avgPickModeTime)
	// difference is signed-negative on the first inter frame after a key
	// frame (avg_encode_time is skipped for KFs while avg_pick_mode_time is
	// updated, so EncodeTime=0 < PickModeTime). Using uint64 here would let
	// the subtraction underflow to a huge unsigned value, fail the
	// "< msForCompress" guard, and force the bump branch (Speed += 4) on
	// frame 1, breaking the auto-select trajectory libvpx follows.
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
			if e.autoSpeed >= 0 && e.autoSpeed < len(libvpxAutoSpeedThresh) &&
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

func (e *VP8Encoder) beginAutoSpeedTiming() {
	if e.opts.Deadline != DeadlineRealtime {
		return
	}
	if e.autoSpeedFrameStartNS == 0 {
		e.autoSpeedFrameStartNS = nowMonotonicNS()
	}
}

func (e *VP8Encoder) cancelAutoSpeedTiming() {
	e.autoSpeedFrameStartNS = 0
}

// finishAutoSpeedTiming mirrors libvpx onyx_if.c:5103-5128: at end of frame
// encode in realtime, IIR-update avg_encode_time (skipped for keyframes) and
// avg_pick_mode_time (duration2 = duration/2 by libvpx convention).
func (e *VP8Encoder) finishAutoSpeedTiming(isKeyFrame bool) {
	if e.autoSpeedFrameStartNS == 0 || e.opts.Deadline != DeadlineRealtime {
		return
	}
	durationNS := nowMonotonicNS() - e.autoSpeedFrameStartNS
	e.autoSpeedFrameStartNS = 0
	if durationNS < 0 {
		durationNS = 0
	}
	duration := int(durationNS / 1000)
	duration2 := duration / 2
	if !isKeyFrame {
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
// disables interval-forced key frames.
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
	return nil
}

// SetAdaptiveKeyFrames enables or disables one-pass scene-cut key frames.
func (e *VP8Encoder) SetAdaptiveKeyFrames(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AdaptiveKeyFrames = enabled
	e.rc.autoKeyFrames = enabled
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

// SetNoiseSensitivity changes the VP8 denoiser level in [0, 6].
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
	return nil
}

// SetARNR changes automatic alt-ref noise-reduction controls.
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
	return nil
}

// SetTwoPassStats replaces the finalized first-pass stats used for second
// pass planning.
func (e *VP8Encoder) SetTwoPassStats(stats []FirstPassFrameStats) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.TwoPassStats = stats
	e.twoPass.configure(stats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct)
	e.twoPass.configureFrameDims(e.opts.Width, e.opts.Height)
	return nil
}

// ForceKeyFrame requests that the next encodable input become a key frame.
func (e *VP8Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

// Reset returns the encoder to a NewVP8Encoder-equivalent cold-start state
// without re-allocating the per-MB scratch buffers. This is what bench
// harnesses use to run a warmup encode followed by a measured one without
// repeating the allocation cost.
//
// R15-E note: previously Reset cleared a hand-curated subset of state, which
// meant fields touched only by the encode loop (rc.kfOverspendBits, the
// inter-RD threshold snapshots, the per-reference probabilities, etc.) leaked
// values from the warmup pass into the measured pass. At 320x240 that drove a
// 7% kbps undershoot vs stock libvpx (govpx 1017 kbps vs libvpx 1089 kbps
// against the same target) because rate-correction state was warmed-up before
// the measured run started. The fix: zero the rateControlState struct,
// re-apply applyConfig, and explicitly reset every encoder-level field that
// NewVP8Encoder seeds.
