package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

// SetRealtimeTarget applies a sparse WebRTC-style runtime target update to the
// VP9 profile 0 encoder.
//
// VP9 currently consumes BitrateKbps as a target hint, FPS as the caller
// timebase, and Width / Height as a caller-driven coded-size change. A changed
// size invalidates every VP9 reference slot and forces the next encoded packet
// to be a keyframe at the new dimensions. VP8-only realtime rate-control
// fields on RealtimeTarget are rejected when explicitly set.
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
	if target.MinQuantizer != 0 || target.MaxQuantizer != 0 {
		return ErrInvalidQuantizer
	}
	if target.FrameDrop != RealtimeFrameDropUnchanged || target.AllowFrameDrop {
		return ErrInvalidConfig
	}
	if target.Width > 0 || target.Height > 0 {
		if !validVP9Dimension(target.Width) || !validVP9Dimension(target.Height) {
			return ErrInvalidConfig
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
	}
	return nil
}

func (e *VP9Encoder) applyVP9ResolutionChange(width, height int) {
	e.opts.Width = width
	e.opts.Height = height
	e.forceKeyFrame = true
	vp9dec.ResetFrameContext(&e.fc)
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
