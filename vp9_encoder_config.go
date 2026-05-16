package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

const (
	vp9DefaultCPUUsed int8 = 8
	vp9MaxCPUUsed          = 9
)

func normalizeVP9SpeedOptions(opts *VP9EncoderOptions) error {
	if opts == nil {
		return ErrInvalidConfig
	}
	if opts.Deadline < DeadlineBestQuality || opts.Deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	cpuUsed := int(opts.CpuUsed)
	if cpuUsed < -vp9MaxCPUUsed || cpuUsed > vp9MaxCPUUsed {
		return ErrInvalidConfig
	}
	if opts.Deadline == DeadlineBestQuality && opts.CpuUsed == 0 {
		opts.Deadline = DeadlineRealtime
		opts.CpuUsed = vp9DefaultCPUUsed
	}
	return nil
}

// SetRealtimeTarget applies a sparse WebRTC-style runtime target update to the
// VP9 profile 0 encoder.
//
// VP9 consumes BitrateKbps and FPS when explicit VP9 rate control is enabled,
// MinQuantizer / MaxQuantizer as public VP9 Q bounds, and Width / Height as a
// caller-driven coded-size change. When the encoder was created with VP9 CBR
// rate control enabled, FrameDrop updates the VP9 CBR drop toggle. A changed size
// invalidates every VP9 reference slot and forces the next encoded packet to be
// a keyframe at the new dimensions.
func (e *VP9Encoder) SetRealtimeTarget(target RealtimeTarget) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if target.BitrateKbps < 0 || target.FPS < 0 ||
		target.Width < 0 || target.Height < 0 {
		return ErrInvalidConfig
	}
	if target.FrameDrop < RealtimeFrameDropUnchanged ||
		target.FrameDrop > RealtimeFrameDropEnabled {
		return ErrInvalidConfig
	}
	if target.MinQuantizer < 0 || target.MaxQuantizer < 0 ||
		target.MinQuantizer > maxQuantizer ||
		target.MaxQuantizer > maxQuantizer {
		return ErrInvalidQuantizer
	}
	frameDropRequested := target.FrameDrop != RealtimeFrameDropUnchanged ||
		target.AllowFrameDrop
	if frameDropRequested && (!e.rc.enabled || e.opts.RateControlMode != RateControlCBR) {
		return ErrInvalidConfig
	}
	if target.Width > 0 || target.Height > 0 {
		if !validVP9Dimension(target.Width) || !validVP9Dimension(target.Height) {
			return ErrInvalidConfig
		}
		if err := validateVP9TileRowOptions(target.Width, target.Height, e.opts.Log2TileRows); err != nil {
			return err
		}
		if e.vp9LookaheadSize() != 0 {
			return ErrFrameNotReady
		}
	}
	nextMinQuantizer, nextMaxQuantizer, _ := vp9NormalizedPublicQuantizers(e.opts)
	if target.MinQuantizer != 0 {
		nextMinQuantizer = target.MinQuantizer
	}
	if target.MaxQuantizer != 0 {
		nextMaxQuantizer = target.MaxQuantizer
	}
	if target.MinQuantizer != 0 || target.MaxQuantizer != 0 {
		if err := validateVP9PublicQuantizerOptions(VP9EncoderOptions{
			Width:        e.opts.Width,
			Height:       e.opts.Height,
			Quantizer:    e.opts.Quantizer,
			MinQuantizer: nextMinQuantizer,
			MaxQuantizer: nextMaxQuantizer,
			CQLevel:      e.opts.CQLevel,
		}); err != nil {
			return err
		}
	}
	nextTemporal := e.temporal
	if target.BitrateKbps > 0 && nextTemporal.enabled {
		if err := nextTemporal.refreshBitrate(target.BitrateKbps); err != nil {
			return err
		}
	}
	nextTiming := e.vp9TimingState()
	if target.FPS > 0 {
		nextTiming = timingState{timebaseNum: 1, timebaseDen: target.FPS, frameDuration: 1}
	}
	nextRC := e.rc
	if nextRC.enabled {
		nextBitrateKbps := nextRC.targetBitrateKbps
		if target.BitrateKbps > 0 {
			nextBitrateKbps = target.BitrateKbps
		}
		if target.BitrateKbps > 0 || target.FPS > 0 {
			if err := nextRC.setBitrateKbps(nextBitrateKbps, nextTiming); err != nil {
				return err
			}
		}
		switch target.FrameDrop {
		case RealtimeFrameDropEnabled:
			nextRC.setFrameDropAllowed(true, e.opts.DropFrameWaterMark)
		case RealtimeFrameDropDisabled:
			nextRC.setFrameDropAllowed(false, e.opts.DropFrameWaterMark)
		case RealtimeFrameDropUnchanged:
			if target.AllowFrameDrop {
				nextRC.setFrameDropAllowed(true, e.opts.DropFrameWaterMark)
			}
		}
	}

	if target.Width > 0 &&
		(target.Width != e.opts.Width || target.Height != e.opts.Height) {
		e.applyVP9ResolutionChange(target.Width, target.Height)
	} else if target.Width > 0 {
		e.opts.Width = target.Width
		e.opts.Height = target.Height
	}
	if target.FPS > 0 {
		e.opts.FPS = target.FPS
		e.opts.TimebaseNum = 1
		e.opts.TimebaseDen = target.FPS
	}
	if target.BitrateKbps > 0 {
		e.opts.TargetBitrateKbps = target.BitrateKbps
		if e.temporal.enabled {
			e.temporal = nextTemporal
			e.opts.TemporalScalability = e.temporal.config
		}
	}
	if nextRC.enabled {
		if target.MinQuantizer != 0 || target.MaxQuantizer != 0 {
			nextOpts := e.opts
			nextOpts.MinQuantizer = nextMinQuantizer
			nextOpts.MaxQuantizer = nextMaxQuantizer
			nextRC.setQuantizerBoundsFromOptions(nextOpts)
		}
		e.rc = nextRC
		e.opts.DropFrameAllowed = e.rc.dropFrameAllowed
		if e.rc.dropFrameAllowed && e.opts.DropFrameWaterMark <= 0 {
			e.opts.DropFrameWaterMark = int(e.rc.dropFramesWaterMark)
		}
	}
	if target.MinQuantizer != 0 || target.MaxQuantizer != 0 {
		e.opts.MinQuantizer = nextMinQuantizer
		e.opts.MaxQuantizer = nextMaxQuantizer
	}
	return nil
}

// SetBitrateKbps changes the VP9 explicit rate-control target bitrate, in
// kbps. The encoder must have been created with VP9 rate control enabled, or
// enabled later through [VP9Encoder.SetRateControl]. Temporal-layer bitrates
// rescale proportionally when they were auto-derived from the total target.
func (e *VP9Encoder) SetBitrateKbps(kbps int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if !e.rc.enabled {
		return ErrInvalidConfig
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(kbps, e.vp9TimingState()); err != nil {
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
	e.twoPass.configure(e.opts.TwoPassStats, e.rc.bitsPerFrame,
		e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct,
		e.opts.TwoPassMaxPct, e.opts.Height)
	return nil
}

// SetRateControl replaces the VP9 runtime-updatable rate-control
// configuration in a single atomic update. VP9 accepts Mode, TargetBitrateKbps,
// public quantizer bounds, CQLevel, CBR buffer geometry, and CBR frame-drop
// fields. VP8-only RateControlConfig fields are rejected until VP9 has matching
// modeled behavior.
func (e *VP9Encoder) SetRateControl(cfg RateControlConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextOpts, err := vp9RateControlOptionsFromConfig(e.opts, cfg)
	if err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(cfg.TargetBitrateKbps); err != nil {
		return err
	}
	nextRC := e.rc
	if err := nextRC.applyRuntimeConfig(nextOpts, e.vp9TimingState()); err != nil {
		return err
	}
	var nextTwoPass vp9TwoPassState
	nextTwoPass.configure(nextOpts.TwoPassStats, nextRC.bitsPerFrame,
		nextOpts.TwoPassVBRBiasPct, nextOpts.TwoPassMinPct,
		nextOpts.TwoPassMaxPct, nextOpts.Height)
	nextOpts.TemporalScalability = nextTemporal.config
	e.opts = nextOpts
	e.rc = nextRC
	e.temporal = nextTemporal
	e.twoPass = nextTwoPass
	return nil
}

// SetCQLevel changes the VP9 public 0..63 CQ/Q level used by public-Q,
// RateControlCQ, and RateControlQ selection. Passing zero restores VP9's
// normalized default for the current public quantizer range.
func (e *VP9Encoder) SetCQLevel(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextOpts := e.opts
	nextOpts.CQLevel = level
	if err := validateVP9EncoderOptions(nextOpts); err != nil {
		return err
	}
	nextRC := e.rc
	if nextRC.enabled {
		nextRC.setQuantizerBoundsFromOptions(nextOpts)
	}
	e.opts = nextOpts
	e.rc = nextRC
	return nil
}

// SetActiveMap installs a VP9 per-16x16 activity map. Cells equal to 0 mark
// inactive macroblocks; on inter frames, inactive 8x8 mode blocks code as
// ZEROMV-LAST with skip=1. Blocks that already match LAST may remain in the
// base segment to preserve VP9 temporal segment-map parity, while changed
// inactive blocks use VP9's active-map skip segment. Pass a nil map to disable.
// Key frames ignore the active map.
//
// rows and cols must equal the encoder's 16x16 macroblock dimensions;
// len(activeMap) must be at least rows*cols.
func (e *VP9Encoder) SetActiveMap(activeMap []uint8, rows int, cols int) error {
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
	miRows := (e.opts.Height + 7) >> 3
	miCols := (e.opts.Width + 7) >> 3
	miCount := miRows * miCols
	if len(e.activeMap) < miCount {
		e.activeMap = make([]uint8, miCount)
	}
	e.activeMap = e.activeMap[:miCount]
	for miRow := 0; miRow < miRows; miRow++ {
		srcRow := (miRow >> 1) * cols
		dstRow := miRow * miCols
		for miCol := 0; miCol < miCols; miCol++ {
			segID := vp9ActiveMapSegmentActive
			if activeMap[srcRow+(miCol>>1)] == 0 {
				segID = vp9ActiveMapSegmentInactive
			}
			e.activeMap[dstRow+miCol] = segID
		}
	}
	e.activeMapMiRows = miRows
	e.activeMapMiCols = miCols
	e.activeMapEnabled = true
	return nil
}

// SetDeadline changes the VP9 speed/quality operating mode used for subsequent
// frames. It mirrors libvpx's best/good/realtime deadline selector while keeping
// the current VP9 cpu-used value.
func (e *VP9Encoder) SetDeadline(deadline Deadline) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if deadline < DeadlineBestQuality || deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	e.opts.Deadline = deadline
	return nil
}

// SetCPUUsed changes the VP9 libvpx-style speed preset for subsequent frames.
// Valid values are [-9, 9]. VP9 maps the sign to abs(cpu-used) internally; govpx
// preserves the signed value so oracle control scripts can round-trip it.
func (e *VP9Encoder) SetCPUUsed(cpuUsed int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cpuUsed < -vp9MaxCPUUsed || cpuUsed > vp9MaxCPUUsed {
		return ErrInvalidConfig
	}
	e.opts.CpuUsed = int8(cpuUsed)
	return nil
}

// SetTuning changes the VP9 visual quality model used for subsequent frames.
// Valid values are [TunePSNR] and [TuneSSIM]. The current encoder stores the
// VP9 tuning control for libvpx-compatible runtime configuration; the default
// TunePSNR path has no extra per-frame work.
func (e *VP9Encoder) SetTuning(tuning Tuning) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if tuning < TunePSNR || tuning > TuneSSIM {
		return ErrInvalidConfig
	}
	e.opts.Tuning = tuning
	return nil
}

// SetNoiseSensitivity changes the VP9 luma/chroma temporal denoiser level used
// for subsequent frames. Valid values are [0, 6]. Zero disables the denoiser;
// non-zero values allocate or resize denoiser buffers on the next encode.
func (e *VP9Encoder) SetNoiseSensitivity(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if level < 0 || level > 6 {
		return ErrInvalidConfig
	}
	e.opts.NoiseSensitivity = int8(level)
	e.denoiser.setSensitivity(int8(level))
	if level == 0 {
		e.denoiser.disable()
	} else {
		e.closeVP9TileWorkerPool()
	}
	return nil
}

// SetFrameDropAllowed enables or disables VP9 CBR buffer-underrun frame
// dropping without changing bitrate. The encoder must have been created with
// VP9 CBR rate control enabled.
func (e *VP9Encoder) SetFrameDropAllowed(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if !e.rc.enabled || e.opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	e.rc.setFrameDropAllowed(enabled, e.opts.DropFrameWaterMark)
	e.opts.DropFrameAllowed = enabled
	if enabled && e.opts.DropFrameWaterMark <= 0 {
		e.opts.DropFrameWaterMark = int(e.rc.dropFramesWaterMark)
	}
	return nil
}

// SetRateControlBuffer changes the VP9 CBR virtual buffer geometry without
// changing bitrate. The encoder must have been created with VP9 CBR rate
// control enabled. Existing buffer level is preserved and clamped to the new
// maximum buffer size.
func (e *VP9Encoder) SetRateControlBuffer(sizeMs, initialMs, optimalMs int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if !e.rc.enabled || e.opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	nextRC := e.rc
	if err := nextRC.setBufferModel(sizeMs, initialMs, optimalMs); err != nil {
		return err
	}
	e.rc = nextRC
	e.opts.BufferSizeMs = sizeMs
	e.opts.BufferInitialSizeMs = initialMs
	e.opts.BufferOptimalSizeMs = optimalMs
	return nil
}

// SetTwoPassStats replaces the finalized VP9 first-pass stats used for
// second-pass VBR/CQ planning. Pass the slice produced by
// [FinalizeVP9FirstPassStats] after collecting per-frame records with
// [VP9Encoder.CollectFirstPassStats]. Passing nil or an empty slice disables
// VP9 second-pass planning on subsequent EncodeInto calls.
func (e *VP9Encoder) SetTwoPassStats(stats []VP9FirstPassFrameStats) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if len(stats) > 0 && (!e.rc.enabled ||
		(e.opts.RateControlMode != RateControlVBR &&
			e.opts.RateControlMode != RateControlCQ)) {
		return ErrInvalidConfig
	}
	e.opts.TwoPassStats = stats
	e.twoPass.configure(stats, e.rc.bitsPerFrame, e.opts.TwoPassVBRBiasPct,
		e.opts.TwoPassMinPct, e.opts.TwoPassMaxPct, e.opts.Height)
	return nil
}

// SetTemporalScalability replaces the active VP9 temporal-only scheduling
// configuration. Set TemporalScalabilityConfig.Enabled = false to disable
// temporal layering. The per-layer bitrate vector must be cumulative across
// layers and end at the encoder's TargetBitrateKbps.
func (e *VP9Encoder) SetTemporalScalability(cfg TemporalScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextTemporal := temporalState{}
	if err := nextTemporal.configure(cfg, e.opts.TargetBitrateKbps); err != nil {
		return err
	}
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

// SetTemporalLayerID overrides the temporal layer assigned by the configured
// VP9 temporal scheduling pattern. The override remains active until changed
// or until SetTemporalScalability replaces the pattern.
func (e *VP9Encoder) SetTemporalLayerID(layerID int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.temporal.setLayerID(layerID)
}

func (e *VP9Encoder) applyVP9ResolutionChange(width, height int) {
	e.opts.Width = width
	e.opts.Height = height
	e.rc.setFrameSize(width, height)
	if e.vp9LookaheadEnabled() {
		e.initVP9Lookahead(width, height, e.opts.LookaheadFrames)
	}
	e.cyclicAQ.configure(e.opts.AQMode == VP9AQCyclicRefresh, width, height)
	e.denoiser.disable()
	e.activeMapEnabled = false
	e.activeMapMiRows = 0
	e.activeMapMiCols = 0
	e.roi.disable()
	e.forceKeyFrame = true
	e.resetVP9EncoderFrameContexts()
	e.prevFrameMvsValid = false
	e.prevFrameMvRows = 0
	e.prevFrameMvCols = 0
	e.prevSegmentMapValid = false
	e.prevSegmentMapRows = 0
	e.prevSegmentMapCols = 0
	e.prevSegmentation = vp9dec.SegmentationParams{}
	e.prevSegmentationValid = false
	e.prevFrameActiveMapEnabled = false
	for slot := range e.refValid {
		e.refValid[slot] = false
		e.refWidth[slot] = 0
		e.refHeight[slot] = 0
		e.refFrames[slot].img = Image{}
		e.refFrames[slot].valid = false
	}
	e.lfRefDeltas = [vp9dec.MaxRefLfDeltas]int8{}
	e.lfModeDeltas = [vp9dec.MaxModeLfDeltas]int8{}
}

func validVP9Dimension(v int) bool {
	return v > 0 && v <= maxVP9Dimension
}

func (e *VP9Encoder) vp9SpeedFeatureCPUUsed() int {
	if e == nil {
		return int(vp9DefaultCPUUsed)
	}
	if e.opts.CpuUsed < 0 {
		return int(-e.opts.CpuUsed)
	}
	return int(e.opts.CpuUsed)
}

func (e *VP9Encoder) vp9CoeffProbAppxStep() int {
	if e == nil || e.opts.Deadline != DeadlineRealtime ||
		e.vp9SpeedFeatureCPUUsed() < 5 {
		return 1
	}
	return 4
}

func (e *VP9Encoder) vp9SkipTx16PlusCoefUpdates(isKey bool) bool {
	return !isKey && e != nil && e.opts.Deadline == DeadlineRealtime &&
		e.vp9SpeedFeatureCPUUsed() >= 4
}

func (e *VP9Encoder) vp9RealtimeVariancePartitionEnabled() bool {
	return e != nil && e.opts.Deadline == DeadlineRealtime &&
		e.vp9SpeedFeatureCPUUsed() >= 8
}
