package govpx

import (
	"image"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

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
		target.MinQuantizer > encoder.MaxPublicQuantizer ||
		target.MaxQuantizer > encoder.MaxPublicQuantizer {
		return ErrInvalidQuantizer
	}
	frameDropRequested := target.FrameDrop != RealtimeFrameDropUnchanged
	if frameDropRequested && (!e.rc.enabled || e.opts.RateControlMode != RateControlCBR) {
		return ErrInvalidConfig
	}
	if target.Width > 0 || target.Height > 0 {
		if !validVP9Dimension(target.Width) || !validVP9Dimension(target.Height) {
			return ErrInvalidConfig
		}
		if e.spatialScalabilityLocked &&
			(target.Width != e.opts.Width || target.Height != e.opts.Height) {
			return ErrInvalidConfig
		}
		if err := validateVP9TileRowOptions(target.Width, target.Height, e.opts.Log2TileRows); err != nil {
			return err
		}
		if _, err := normalizeVP9SpatialScalabilityConfig(e.opts.SpatialScalability,
			target.Width, target.Height); err != nil {
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
	nextWidth := e.opts.Width
	nextHeight := e.opts.Height
	if target.Width > 0 {
		nextWidth = target.Width
		nextHeight = target.Height
	}
	nextTiming := e.vp9TimingState()
	if target.FPS > 0 {
		nextTiming = timingState{timebaseNum: 1, timebaseDen: target.FPS, frameDuration: 1}
	}
	nextRC := e.rc
	rateModelChanged := false
	if nextRC.enabled {
		if target.Width > 0 {
			nextRC.setFrameSize(nextWidth, nextHeight)
		}
		nextBitrateKbps := nextRC.targetBitrateKbps
		if target.BitrateKbps > 0 {
			nextBitrateKbps = target.BitrateKbps
		}
		if target.BitrateKbps > 0 || target.FPS > 0 || target.Width > 0 {
			if err := nextRC.setBitrateKbps(nextBitrateKbps, nextTiming); err != nil {
				return err
			}
			nextOpts := e.opts
			nextOpts.Width = nextWidth
			nextOpts.Height = nextHeight
			nextRC.setGFIntervalsFromOptions(nextOpts)
			nextRC.initOnePassVBRState(nextTiming)
			rateModelChanged = true
		}
		switch target.FrameDrop {
		case RealtimeFrameDropEnabled:
			nextRC.setFrameDropAllowed(true, e.opts.DropFrameWaterMark)
		case RealtimeFrameDropDisabled:
			nextRC.setFrameDropAllowed(false, e.opts.DropFrameWaterMark)
		}
	}
	nextTemporal := e.temporal
	if target.BitrateKbps > 0 && nextTemporal.enabled {
		nextTemporalBitrateKbps := target.BitrateKbps
		if nextRC.enabled {
			nextTemporalBitrateKbps = nextRC.targetBitrateKbps
		}
		if err := nextTemporal.refreshBitrate(nextTemporalBitrateKbps); err != nil {
			return err
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
		if nextRC.enabled {
			e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
		}
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
	if nextRC.enabled && rateModelChanged {
		e.twoPass.configureWithCorpus(e.opts.TwoPassStats, e.rc.bitsPerFrame,
			e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct,
			e.opts.TwoPassMaxPct, e.opts.Height, e.opts.VBRCorpusComplexity)
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
	e.twoPass.configureWithCorpus(e.opts.TwoPassStats, e.rc.bitsPerFrame,
		e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct,
		e.opts.TwoPassMaxPct, e.opts.Height, e.opts.VBRCorpusComplexity)
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
	nextTwoPass.configureWithCorpus(nextOpts.TwoPassStats, nextRC.bitsPerFrame,
		nextOpts.TwoPassVBRBiasPct, nextOpts.TwoPassMinPct,
		nextOpts.TwoPassMaxPct, nextOpts.Height, nextOpts.VBRCorpusComplexity)
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

// SetAQMode changes the VP9 adaptive quantization mode before the first coded
// frame. Valid values are [VP9AQNone], [VP9AQVariance], [VP9AQComplexity], and
// [VP9AQCyclicRefresh]; the same mode-specific option constraints as
// [VP9EncoderOptions.AQMode] apply. The control is rejected after encoding has
// started because libvpx's VP9E_SET_AQ_MODE does not reconfigure active AQ
// segmentation state mid-stream. Enabling cyclic-refresh AQ allocates or
// resizes its segment map during this control call, keeping the encode hot path
// allocation-free after the update.
func (e *VP9Encoder) SetAQMode(mode VP9AQMode) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if e.frameIndex != 0 || e.vp9LookaheadSize() != 0 {
		return ErrInvalidConfig
	}
	nextOpts := e.opts
	nextOpts.AQMode = mode
	if err := validateVP9EncoderOptions(nextOpts); err != nil {
		return err
	}
	e.opts = nextOpts
	e.cyclicAQ.Configure(mode == VP9AQCyclicRefresh, e.opts.Width, e.opts.Height)
	e.perceptualAQ.Configure(mode == VP9AQPerceptual)
	return nil
}

// SetAutoAltRef enables or disables VP9 automatic alternate-reference
// scheduling for subsequent lookahead frames. Mirrors libvpx's
// VP8E_SET_ENABLEAUTOALTREF control for VP9 while preserving govpx's queued
// lookahead contract: pending lookahead frames or staged frame-parallel results
// must be drained before toggling because those frames were queued under the
// previous scheduling policy.
func (e *VP9Encoder) SetAutoAltRef(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if e.opts.AutoAltRef == enabled {
		return nil
	}
	if e.vp9LookaheadSize() != 0 || e.autoAltRefPendingSet ||
		(e.frameParallel != nil && e.frameParallel.hasPendingResults()) {
		return ErrFrameNotReady
	}
	nextOpts := e.opts
	nextOpts.AutoAltRef = enabled
	if err := validateVP9EncoderOptions(nextOpts); err != nil {
		return err
	}
	e.opts = nextOpts
	if !enabled {
		e.autoAltRefPending = vp9LookaheadEntry{}
		return nil
	}
	if e.vp9LookaheadEnabled() && len(e.autoAltRefPending.img.Y) == 0 {
		rect := image.Rect(0, 0, e.opts.Width, e.opts.Height)
		e.autoAltRefPending.img = *image.NewYCbCr(rect,
			image.YCbCrSubsampleRatio420)
	}
	if e.opts.ARNRMaxFrames > 1 && e.vp9LookaheadEnabled() &&
		len(e.vp9ARNRScratch.Y) == 0 {
		e.ensureVP9ARNRScratch()
	}
	return nil
}

// SetLossless enables or disables VP9 profile 0 lossless coding for subsequent
// frames. Enabling lossless forces base qindex 0, 4x4 transforms, WHT
// reconstruction, and the lossless loop-filter path.
func (e *VP9Encoder) SetLossless(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextOpts := e.opts
	nextOpts.Lossless = enabled
	if err := validateVP9EncoderOptions(nextOpts); err != nil {
		return err
	}
	e.opts = nextOpts
	return nil
}

// SetFrameParallelDecoding enables or disables VP9 frame-parallel decodability
// mode for subsequent frames. Error-resilient frames always signal the VP9
// frame-parallel mode required by the bitstream. When disabled, the encoder
// maintains the non-frame-parallel adapted probability context so later frames
// stay decodable by libvpx-style decoders.
func (e *VP9Encoder) SetFrameParallelDecoding(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.FrameParallelDecodingSet = true
	e.opts.FrameParallelDecoding = enabled
	return nil
}

// SetFrameParallelEncoderThreads configures the maximum number of source
// frames encoded concurrently. Mirrors libvpx's --frame-parallel-mt scheduling
// knob at the encoder side. Zero or one disable the batched scheduler;
// higher values lead the next lookahead-driven encode to dispatch up to N
// frames in parallel. Requires LookaheadFrames > 0 (frame-parallel scheduling
// is meaningful only when the lookahead queue accumulates future sources).
// Mutually exclusive with AutoAltRef since the hidden alt-ref source spans
// future frames that would otherwise be consumed by the batch.
//
// In-flight batches that have not yet been drained block the configuration
// change with ErrFrameNotReady; drain the encoder with FlushIntoWithResult
// before retrying.
func (e *VP9Encoder) SetFrameParallelEncoderThreads(threads int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if threads < 0 {
		return ErrInvalidConfig
	}
	if threads > vp9MaxLookaheadFrames {
		return ErrInvalidConfig
	}
	nextOpts := e.opts
	nextOpts.FrameParallelEncoderThreads = threads
	if err := validateVP9FrameParallelEncoderOptions(nextOpts); err != nil {
		return err
	}
	if e.frameParallel != nil && e.frameParallel.hasPendingResults() {
		return ErrFrameNotReady
	}
	e.opts.FrameParallelEncoderThreads = threads
	if threads <= 1 && e.frameParallel != nil {
		e.frameParallel.release()
		e.frameParallel = nil
	}
	return nil
}

// GetActiveMap snapshots the encoder's current 16x16 activity map into the
// caller-supplied buffer.  Mirrors libvpx's vp9_get_active_map
// (vp9/encoder/vp9_encoder.c:777) and the VP9E_GET_ACTIVEMAP codec-control
// dispatch (vp9/vp9_cx_iface.c:1795).
//
// libvpx semantics:
//
//   - rows, cols must equal the encoder's 16x16 macroblock dimensions.
//   - When the active map is disabled, every output byte is 1.
//   - When the active map is enabled, output byte (mbR, mbC) is the OR of
//     the four covered 8x8 MI cells: 1 if any MI is NOT
//     AM_SEGMENT_ID_INACTIVE, 0 otherwise.  Cyclic-refresh segments
//     therefore appear as active even though the AM segmentation does
//     not assign them AM_SEGMENT_ID_ACTIVE explicitly.
//
// Returns ErrInvalidConfig when rows/cols mismatch the encoder dimensions
// or activeMap is too short for rows*cols bytes.  Returns ErrClosed if
// the encoder has been closed.
func (e *VP9Encoder) GetActiveMap(activeMap []uint8, rows int, cols int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	expectedRows := encoderMacroblockRows(e.opts.Height)
	expectedCols := encoderMacroblockCols(e.opts.Width)
	if rows != expectedRows || cols != expectedCols {
		return ErrInvalidConfig
	}
	if len(activeMap) < rows*cols {
		return ErrInvalidConfig
	}
	dst := activeMap[:rows*cols]
	if !e.activeMapEnabled {
		// libvpx: memset(new_map_16x16, !cpi->active_map.enabled,
		// rows * cols) — disabled active map reports every MB as
		// active (byte == 1).
		for i := range dst {
			dst[i] = 1
		}
		return nil
	}
	// libvpx walks every 8x8 MI cell and OR's it into the covering 16x16
	// MB; govpx's per-MI map lives in e.activeMap and tags inactive MIs
	// with vp9ActiveMapSegmentInactive.  Initialise the output to 0
	// (libvpx's !enabled byte) so the OR below builds up the active mask.
	for i := range dst {
		dst[i] = 0
	}
	miRows := e.activeMapMiRows
	miCols := e.activeMapMiCols
	if miRows <= 0 || miCols <= 0 {
		return nil
	}
	if len(e.activeMap) < miRows*miCols {
		return nil
	}
	for r := range miRows {
		dstRowBase := (r >> 1) * cols
		srcRowBase := r * miCols
		for c := range miCols {
			seg := e.activeMap[srcRowBase+c]
			if seg != vp9ActiveMapSegmentInactive {
				// libvpx: new_map_16x16[(r >> 1) * cols + (c >> 1)] |=
				//   seg_map_8x8[r * mi_cols + c] != AM_SEGMENT_ID_INACTIVE
				dst[dstRowBase+(c>>1)] |= 1
			}
		}
	}
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
	for miRow := range miRows {
		srcRow := (miRow >> 1) * cols
		dstRow := miRow * miCols
		for miCol := range miCols {
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
	// libvpx: vp9_encoder.c:3754 — speed-feature recompute on speed/mode
	// changes.
	e.vp9ApplySpeedFeatures(e.vp9DefaultSpeedFrameContext())
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
	// libvpx: vp9_encoder.c:3754 — speed-feature recompute on speed/mode
	// changes.
	e.vp9ApplySpeedFeatures(e.vp9DefaultSpeedFrameContext())
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

// SetRowMT toggles VP9 row-wavefront multithreading for subsequent frames. It
// mirrors libvpx's VP9E_SET_ROW_MT control. Enabling it requires Threads > 1
// because the wavefront primitive is meaningful only with a multi-column tile
// layout driven by the persistent tile worker pool. Disabling tears down any
// allocated VP9RowMTSync state on the next encode so steady-state allocations
// stay bounded. The bitstream output is byte-identical to the serial path.
func (e *VP9Encoder) SetRowMT(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if enabled && e.opts.Threads <= 1 {
		return ErrInvalidConfig
	}
	e.opts.RowMT = enabled
	if !enabled && e.vp9TilePool != nil {
		e.vp9TilePool.releaseRowMTSync()
		e.vp9TilePool.releaseRowWorkers()
	}
	return nil
}

// SetScreenContentMode changes VP9 content tuning for subsequent frames.
// Valid values match the libvpx VP9E_SET_TUNE_CONTENT enum and the
// VP9ScreenContent* constants: 0 for VP9ScreenContentDefault, 1 for
// VP9ScreenContentScreen, and 2 for VP9ScreenContentFilm. Screen
// content expands the realtime no-reference intra search; film
// content suppresses the variance-AQ Q-up bias on high-variance
// blocks so grain texture survives quantization.
func (e *VP9Encoder) SetScreenContentMode(mode int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if mode < int(VP9ScreenContentDefault) || mode > int(VP9ScreenContentFilm) {
		return ErrInvalidConfig
	}
	e.opts.ScreenContentMode = int8(mode)
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

// SetSharpness changes the VP9 loop-filter sharpness level used for subsequent
// frames. Valid values are [0, 7].
func (e *VP9Encoder) SetSharpness(sharpness uint8) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if sharpness > 7 {
		return ErrInvalidConfig
	}
	e.opts.Sharpness = sharpness
	return nil
}

// SetStaticThreshold changes the VP9 static-block breakout threshold used for
// subsequent inter frames. Non-negative values are accepted; zero disables the
// breakout.
func (e *VP9Encoder) SetStaticThreshold(threshold int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if threshold < 0 {
		return ErrInvalidConfig
	}
	e.opts.StaticThreshold = threshold
	return nil
}

// SetKeyFrameInterval changes the VP9 maximum GOP distance in frames while
// leaving the current minimum distance unchanged. Zero restores libvpx's
// default VP9 max key-frame cadence. Explicitly forced key frames are
// unaffected.
func (e *VP9Encoder) SetKeyFrameInterval(frames int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9KeyFrameIntervalOptions(
		e.opts.MinKeyframeInterval, frames); err != nil {
		return err
	}
	e.opts.MaxKeyframeInterval = frames
	return nil
}

// SetKeyFrameIntervalRange changes the VP9 minimum and maximum key-frame
// distances. Zero for min leaves libvpx's default kf_min_dist=0; zero for max
// restores libvpx's default kf_max_dist=128. Explicitly forced key frames are
// unaffected.
func (e *VP9Encoder) SetKeyFrameIntervalRange(minFrames, maxFrames int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9KeyFrameIntervalOptions(minFrames, maxFrames); err != nil {
		return err
	}
	e.opts.MinKeyframeInterval = minFrames
	e.opts.MaxKeyframeInterval = maxFrames
	return nil
}

// SetAdaptiveKeyFrames enables or disables VP9 one-pass scene-cut keyframe
// promotion for subsequent frames. Disabling preserves explicit and
// MaxKeyframeInterval keyframes; it only turns off content-driven automatic
// promotions.
func (e *VP9Encoder) SetAdaptiveKeyFrames(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AdaptiveKeyFrames = enabled
	return nil
}

// SetRTCExternalRateControl mirrors libvpx's VP9E_SET_RTC_EXTERNAL_RATECTRL
// control. Forwards to [VP9EncoderOptions.RTCExternalRateControl].
func (e *VP9Encoder) SetRTCExternalRateControl(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.RTCExternalRateControl = enabled
	return nil
}

// SetColorSpace mirrors libvpx's VP9E_SET_COLOR_SPACE control. The
// value tags the bitstream's color space in the keyframe / intra-only
// uncompressed header. Profile-0 streams cannot carry SRGB.
func (e *VP9Encoder) SetColorSpace(cs VP9ColorSpace) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cs > VP9ColorSpaceSRGB {
		return ErrInvalidConfig
	}
	if cs == VP9ColorSpaceSRGB {
		return ErrInvalidConfig
	}
	e.opts.ColorSpace = cs
	return nil
}

// SetColorRange mirrors libvpx's VP9E_SET_COLOR_RANGE control. The
// 1-bit color_range tag follows the color space in the uncompressed
// header on keyframes (and profile>0 intra-only frames).
func (e *VP9Encoder) SetColorRange(cr VP9ColorRange) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cr > VP9ColorRangeFull {
		return ErrInvalidConfig
	}
	e.opts.ColorRange = cr
	return nil
}

// SetRenderSize mirrors libvpx's VP9E_SET_RENDER_SIZE control. The
// caller passes the desired display (width, height); passing (0, 0)
// clears the hint and the bitstream emits render_and_frame_size
// _different=0 so the decoder inherits the coded dimensions.
func (e *VP9Encoder) SetRenderSize(width, height int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9RenderSizeOptions(VP9EncoderOptions{
		RenderWidth:  width,
		RenderHeight: height,
	}); err != nil {
		return err
	}
	e.opts.RenderWidth = width
	e.opts.RenderHeight = height
	return nil
}

// SetTargetLevel mirrors libvpx's VP9E_SET_TARGET_LEVEL control. level must
// be one of the canonical VP9 level codes (10, 11, 20, 21, 30, 31, 40, 41,
// 50, 51, 52, 60, 61, 62), [VP9TargetLevelUnspecified],
// [VP9TargetLevelAuto], or [VP9TargetLevelUnconstrained]. Fixed levels adapt
// internal rate-control, quantizer, GF interval, and tile-column limits; they
// do not reject otherwise valid dimensions or bitrates.
func (e *VP9Encoder) SetTargetLevel(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9TargetLevel(level); err != nil {
		return err
	}
	nextOpts := e.opts
	nextOpts.TargetLevel = level
	nextRC := e.rc
	rateModelChanged := false
	if nextRC.enabled {
		nextRC.applyBitrateBoundsFromOptions(nextOpts)
		nextRC.setQuantizerBoundsFromOptions(nextOpts)
		if err := nextRC.setBitrateKbps(nextRC.targetBitrateKbps,
			e.vp9TimingState()); err != nil {
			return err
		}
		rateModelChanged = true
	}
	e.opts = nextOpts
	e.rc = nextRC
	if rateModelChanged {
		e.twoPass.configureWithCorpus(e.opts.TwoPassStats, e.rc.bitsPerFrame,
			e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct,
			e.opts.TwoPassMaxPct, e.opts.Height, e.opts.VBRCorpusComplexity)
	}
	return nil
}

// SetDisableLoopfilter mirrors libvpx's VP9E_SET_DISABLE_LOOPFILTER
// control. mode 0 leaves the in-loop filter enabled; mode 1 disables
// it for non-keyframes; mode 2 disables it on every frame.
func (e *VP9Encoder) SetDisableLoopfilter(mode VP9DisableLoopfilter) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if mode > VP9LoopfilterDisableAll {
		return ErrInvalidConfig
	}
	e.opts.DisableLoopfilter = mode
	return nil
}

// SetDeltaQUV mirrors libvpx's VP9E_SET_DELTA_Q_UV control. delta must be
// in [-15, 15]; non-zero values disable Profile 0 lossless even at
// base_qindex == 0. Forwards to [VP9EncoderOptions.DeltaQUV].
func (e *VP9Encoder) SetDeltaQUV(delta int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if delta < -15 || delta > 15 {
		return ErrInvalidQuantizer
	}
	if e.opts.Lossless && delta != 0 {
		return ErrInvalidQuantizer
	}
	e.opts.DeltaQUV = delta
	return nil
}

// SetMaxInterBitratePct mirrors libvpx's VP9E_SET_MAX_INTER_BITRATE_PCT
// control. pct caps inter-frame target bits at pct% of the per-frame
// bandwidth budget. Zero disables the cap. Forwards to
// [VP9EncoderOptions.MaxInterBitratePct].
func (e *VP9Encoder) SetMaxInterBitratePct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.opts.MaxInterBitratePct = pct
	e.rc.maxInterBitratePct = pct
	return nil
}

// SetMaxIntraBitratePct mirrors libvpx's VP8E_SET_MAX_INTRA_BITRATE_PCT
// control as consumed by the VP9 encoder. pct caps key-frame target bits at
// pct% of the per-frame bandwidth budget. Zero disables the cap. Forwards to
// [VP9EncoderOptions.MaxIntraBitratePct].
func (e *VP9Encoder) SetMaxIntraBitratePct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.opts.MaxIntraBitratePct = pct
	e.rc.maxIntraBitratePct = pct
	return nil
}

// SetGFCBRBoostPct mirrors libvpx's VP8E_SET_GF_CBR_BOOST_PCT control as
// consumed by the VP9 encoder. pct boosts golden-frame target bits in CBR mode
// by pct% of the per-frame bandwidth budget. Zero disables the boost. Forwards
// to [VP9EncoderOptions.GFCBRBoostPct].
func (e *VP9Encoder) SetGFCBRBoostPct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	if pct != 0 && (!e.rc.enabled || e.opts.RateControlMode != RateControlCBR) {
		return ErrInvalidConfig
	}
	e.opts.GFCBRBoostPct = pct
	e.rc.gfCBRBoostPct = pct
	return nil
}

// SetMinGFInterval mirrors libvpx's VP9E_SET_MIN_GF_INTERVAL control.
// interval must be in [0, 24]; zero restores libvpx's
// framerate-derived default. Forwards to
// [VP9EncoderOptions.MinGFInterval].
func (e *VP9Encoder) SetMinGFInterval(interval int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9GFIntervalBounds(interval,
		e.opts.MaxGFInterval); err != nil {
		return err
	}
	e.opts.MinGFInterval = interval
	if e.rc.enabled {
		e.rc.setGFIntervalsFromOptions(e.opts)
		e.rc.initOnePassVBRState(e.vp9TimingState())
	}
	return nil
}

// SetMaxGFInterval mirrors libvpx's VP9E_SET_MAX_GF_INTERVAL control.
// interval must be zero or in [2, 24]; zero restores libvpx's
// framerate-derived default. Forwards to
// [VP9EncoderOptions.MaxGFInterval].
func (e *VP9Encoder) SetMaxGFInterval(interval int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9GFIntervalBounds(e.opts.MinGFInterval,
		interval); err != nil {
		return err
	}
	e.opts.MaxGFInterval = interval
	if e.rc.enabled {
		e.rc.setGFIntervalsFromOptions(e.opts)
		e.rc.initOnePassVBRState(e.vp9TimingState())
	}
	return nil
}

// SetFramePeriodicBoost mirrors libvpx's VP9E_SET_FRAME_PERIODIC_BOOST
// control. When enabled, periodic golden-frame refreshes receive a
// stronger active-best-Q reduction. Forwards to
// [VP9EncoderOptions.FramePeriodicBoost].
func (e *VP9Encoder) SetFramePeriodicBoost(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.FramePeriodicBoost = enabled
	e.rc.framePeriodicBoost = enabled
	return nil
}

// SetAltRefAQ mirrors libvpx's VP9E_SET_ALT_REF_AQ control. In libvpx
// v1.16.0 the VP9 alt-ref AQ implementation is a no-op, so this records
// the control without changing coding decisions.
func (e *VP9Encoder) SetAltRefAQ(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.AltRefAQ = enabled
	e.rc.altRefAQ = enabled
	return nil
}

// SetPostEncodeDrop mirrors libvpx's VP9E_SET_POSTENCODE_DROP_CBR
// control. Requires CBR rate control. When enabled, visible inter frames
// whose packed size would underflow the CBR buffer are dropped from the
// output after encoding. This is separate from [VP9Encoder.SetFrameDropAllowed],
// which controls the pre-encode watermark dropper. Forwards to
// [VP9EncoderOptions.PostEncodeDrop].
func (e *VP9Encoder) SetPostEncodeDrop(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if enabled && (!e.rc.enabled || e.opts.RateControlMode != RateControlCBR) {
		return ErrInvalidConfig
	}
	e.opts.PostEncodeDrop = enabled
	e.rc.postEncodeDrop = enabled
	return nil
}

// SetDisableOvershootMaxQCBR mirrors libvpx's
// VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR control. Requires CBR rate
// control. When enabled, the CBR active-worst-Q promotion to
// worstQuality in the critical buffer region is suppressed. Forwards to
// [VP9EncoderOptions.DisableOvershootMaxQCBR].
func (e *VP9Encoder) SetDisableOvershootMaxQCBR(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if enabled && (!e.rc.enabled || e.opts.RateControlMode != RateControlCBR) {
		return ErrInvalidConfig
	}
	e.opts.DisableOvershootMaxQCBR = enabled
	e.rc.disableOvershootMaxQCBR = enabled
	return nil
}

// SetNextFrameQIndex mirrors libvpx's VP9E_SET_QUANTIZER_ONE_PASS
// control. qindex must lie in [0, 255]. The override is consumed by the
// next encode call and then cleared. Mutually exclusive with
// cyclic-refresh AQ and perceptual AQ, which already rewrite the qindex
// through segmentation. Forwards to
// [VP9EncoderOptions.NextFrameQIndex] / NextFrameQIndexSet.
func (e *VP9Encoder) SetNextFrameQIndex(qindex int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if err := validateVP9NextFrameQIndex(qindex, true, e.opts.AQMode); err != nil {
		return err
	}
	e.opts.NextFrameQIndexSet = true
	e.opts.NextFrameQIndex = qindex
	e.rc.nextFrameQIndexSet = true
	e.rc.nextFrameQIndex = uint8(qindex)
	return nil
}

// SetARNR changes VP9 auto-alt-ref temporal filtering controls at runtime.
// maxFrames is the ARNR window length in [0, 15], where 0 or 1 disables ARNR
// filtering; strength is in [0, 6]; filterType selects 1=backward, 2=forward,
// or 3=centered. Passing filterType 0 restores the centered default.
func (e *VP9Encoder) SetARNR(maxFrames int, strength int, filterType int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if maxFrames < 0 || maxFrames > maxARNRFrames ||
		strength < 0 || strength > 6 ||
		filterType < 0 || filterType > 3 {
		return ErrInvalidConfig
	}
	if filterType == 0 {
		filterType = 3
	}
	e.opts.ARNRMaxFrames = maxFrames
	e.opts.ARNRStrength = strength
	e.opts.ARNRType = filterType
	if maxFrames > 1 && e.opts.AutoAltRef && e.vp9LookaheadEnabled() &&
		len(e.vp9ARNRScratch.Y) == 0 {
		e.ensureVP9ARNRScratch()
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
// maximum buffer size. Passing zero for sizeMs or optimalMs uses libvpx's
// target_bandwidth/8 fallback; passing zero for initialMs sets an empty
// starting buffer.
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
	e.twoPass.configureWithCorpus(stats, e.rc.bitsPerFrame,
		e.opts.TwoPassVBRBiasPct, e.opts.TwoPassMinPct,
		e.opts.TwoPassMaxPct, e.opts.Height, e.opts.VBRCorpusComplexity)
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
	if e.temporalScalabilityLocked {
		return ErrInvalidConfig
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
	if e.temporalScalabilityLocked {
		return ErrInvalidConfig
	}
	return e.temporal.setLayerID(layerID)
}

// SetSpatialScalability replaces the VP9 spatial-SVC layer signaling
// configuration. This controls encoded result metadata and RTP payload
// descriptors for packets produced by this encoder; it does not synthesize
// additional coded spatial layers.
func (e *VP9Encoder) SetSpatialScalability(cfg VP9SpatialScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if e.spatialScalabilityLocked {
		return ErrInvalidConfig
	}
	next, err := normalizeVP9SpatialScalabilityConfig(cfg, e.opts.Width,
		e.opts.Height)
	if err != nil {
		return err
	}
	e.opts.SpatialScalability = next
	return nil
}

// SetSpatialLayerID changes the VP9 spatial layer ID signaled for subsequent
// packets. Spatial scalability must already be enabled unless layerID is zero.
func (e *VP9Encoder) SetSpatialLayerID(layerID uint8) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if e.spatialScalabilityLocked {
		return ErrInvalidConfig
	}
	if !e.opts.SpatialScalability.Enabled {
		if layerID == 0 {
			return nil
		}
		return ErrInvalidConfig
	}
	next := e.opts.SpatialScalability
	next.LayerID = layerID
	cfg, err := normalizeVP9SpatialScalabilityConfig(next, e.opts.Width,
		e.opts.Height)
	if err != nil {
		return err
	}
	e.opts.SpatialScalability = cfg
	return nil
}

func (e *VP9Encoder) applyVP9ResolutionChange(width, height int) {
	e.opts.Width = width
	e.opts.Height = height
	e.rc.setFrameSize(width, height)
	if e.vp9LookaheadEnabled() {
		e.initVP9Lookahead(width, height, e.opts.LookaheadFrames)
	}
	e.cyclicAQ.Configure(e.opts.AQMode == VP9AQCyclicRefresh, width, height)
	e.perceptualAQ.Configure(e.opts.AQMode == VP9AQPerceptual)
	e.tpl.Configure(e.opts.EnableTPL, width, height, e.opts.LookaheadFrames)
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
		e.refMap[slot] = 0
	}
	e.nextRefMapID = 0
	e.lfRefDeltas = [vp9dec.MaxRefLfDeltas]int8{}
	e.lfModeDeltas = [vp9dec.MaxModeLfDeltas]int8{}
	if e.opts.AQMode == VP9AQCyclicRefresh {
		e.cyclicResizePending = true
	}
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

// vp9CoeffProbAppxStep reads SPEED_FEATURES.coeff_prob_appx_step. libvpx
// initialises it to 1 at best quality and sets it to 4 in the realtime speed-5
// branch (vp9_speed_features.c:610). Consumers route through e.sf rather than
// re-deriving the gate.
//
// libvpx: vp9_speed_features.c:937 / vp9_speed_features.c:610.
func (e *VP9Encoder) vp9CoeffProbAppxStep() int {
	if e == nil {
		return 1
	}
	if e.sf.CoeffProbAppxStep == 0 {
		return 1
	}
	return e.sf.CoeffProbAppxStep
}

// vp9SkipTx16PlusCoefUpdates mirrors libvpx's update_coef_probs tx_size gate:
//
//	if (cpi->td.counts->tx.tx_totals[tx_size] <= 20 ||
//	    (tx_size >= TX_16X16 && cpi->sf.tx_size_search_method == USE_TX_8X8))
//	  vpx_write_bit(w, 0);
//
// (libvpx vp9_bitstream.c:691-693). The gate is keyed strictly on
// SPEED_FEATURES.tx_size_search_method == USE_TX_8X8, NOT on
// use_fast_coef_updates. The two speed features are independent: they happen
// to coincide at REALTIME speed >= 4 / >= 5 non-key frames
// (vp9_speed_features.c:579+581 / 611+613) where both fire, but at GOOD
// speed >= 4 only use_fast_coef_updates is flipped to ONE_LOOP_REDUCED
// (vp9_speed_features.c:395) while tx_size_search_method stays at
// USE_LARGESTALL (vp9_speed_features.c:387). Consult e.sf directly so the
// per-frame vp9ApplySpeedFeatures dispatch (vp9_encoder.go:2593) drives the
// gate.
//
// libvpx: vp9_bitstream.c:691-693 (the tx_size gate inside update_coef_probs).
func (e *VP9Encoder) vp9SkipTx16PlusCoefUpdates() bool {
	if e == nil {
		return false
	}
	return e.sf.TxSizeSearchMethod == UseTx8x8
}

// vp9RealtimeVariancePartitionEnabled returns true when SPEED_FEATURES picks
// VAR_BASED_PARTITION. libvpx selects this in the realtime speed >= 4 branch
// (vp9_speed_features.c:582) and again at speed >= 6 (line 667). The good-mode
// path never picks VAR_BASED_PARTITION.
//
// libvpx: vp9_speed_features.c:582 / 667.
func (e *VP9Encoder) vp9RealtimeVariancePartitionEnabled() bool {
	if e == nil {
		return false
	}
	return e.sf.PartitionSearchType == VarBasedPartition
}
