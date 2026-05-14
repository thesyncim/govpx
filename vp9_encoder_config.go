package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

// SetRealtimeTarget applies a sparse WebRTC-style runtime target update to the
// VP9 profile 0 encoder.
//
// VP9 consumes BitrateKbps and FPS in explicit CBR mode, MinQuantizer /
// MaxQuantizer as public VP9 Q bounds, and Width / Height as a caller-driven
// coded-size change. When the encoder was created with VP9 CBR rate control
// enabled, FrameDrop updates the VP9 CBR drop toggle. A changed size
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
	e.forceKeyFrame = true
	e.resetVP9EncoderFrameContexts()
	e.prevFrameMvsValid = false
	e.prevFrameMvRows = 0
	e.prevFrameMvCols = 0
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
