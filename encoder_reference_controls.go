package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

// SetReferenceFrame replaces ref with src. ref must be ReferenceLast,
// ReferenceGolden, or ReferenceAltRef; src must match the encoder
// dimensions and provide valid I420 strides. The encoder pads coded
// edges, extends borders, and invalidates cached inter-prediction state
// tied to the previous reference identity.
//
// Returns [ErrClosed] on a nil or closed encoder, or [ErrInvalidConfig]
// when ref is not a single valid selector or src does not match the
// encoder's shape or strides.
func (e *VP8Encoder) SetReferenceFrame(ref ReferenceFrame, src Image) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	fb, ok := e.referenceFrameBuffer(ref)
	if !ok || !src.validForEncode(e.opts.Width, e.opts.Height) {
		return ErrInvalidConfig
	}
	copyPublicImageToVP8(&fb.Img, src)
	padFrameVisibleToCoded(&fb.Img)
	fb.ExtendBorders()
	e.syncDenoiserReferenceFrame(ref, src)
	e.invalidateReferenceFrameState(ref)
	return nil
}

// CopyReferenceFrame copies ref into dst. ref must be ReferenceLast,
// ReferenceGolden, or ReferenceAltRef; dst must be non-nil, match the
// encoder dimensions, and provide valid I420 strides.
//
// Returns [ErrClosed] on a nil or closed encoder, or [ErrInvalidConfig]
// when dst is nil, ref is not a single valid selector, or dst does not
// match the encoder's shape or strides.
func (e *VP8Encoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if dst == nil {
		return ErrInvalidConfig
	}
	fb, ok := e.referenceFrameBuffer(ref)
	if !ok || !dst.validForEncode(e.opts.Width, e.opts.Height) {
		return ErrInvalidConfig
	}
	copyVP8ImageToPublic(dst, &fb.Img)
	return nil
}

// referenceFrameBuffer maps the public reference selector to the encoder-owned
// bordered buffer. Invalid selectors include combined ReferenceFlags values.
func (e *VP8Encoder) referenceFrameBuffer(ref ReferenceFrame) (*vp8common.FrameBuffer, bool) {
	switch ref {
	case ReferenceLast:
		return &e.lastRef, true
	case ReferenceGolden:
		return &e.goldenRef, true
	case ReferenceAltRef:
		return &e.altRef, true
	default:
		return nil, false
	}
}

// invalidateReferenceFrameState clears encoder state that assumes reference
// identity only changes through the normal VP8 refresh/copy path.
func (e *VP8Encoder) invalidateReferenceFrameState(ref ReferenceFrame) {
	switch ref {
	case ReferenceLast:
		e.goldenRefAliasesLast = false
		e.altRefAliasesLast = false
		e.referenceFrameNumbers[vp8common.LastFrame] = e.frameCount
	case ReferenceGolden:
		e.goldenRefAliasesLast = false
		e.goldenRefAliasesAlt = false
		e.referenceFrameNumbers[vp8common.GoldenFrame] = e.frameCount
	case ReferenceAltRef:
		e.altRefAliasesLast = false
		e.goldenRefAliasesAlt = false
		e.referenceFrameNumbers[vp8common.AltRefFrame] = e.frameCount
	}
	e.lastFrameInterModesValid = false
	e.interRDFrameRefSearchOrderValid = false
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	e.lastInterZeroMVCount = 0
	e.mbsZeroLastDotSuppress = 0
	e.sourceAltRefActive = false
	e.clearAltRefSchedule()
}

// syncDenoiserReferenceFrame keeps the denoiser's parallel reference stream in
// step with externally replaced encoder references.
func (e *VP8Encoder) syncDenoiserReferenceFrame(ref ReferenceFrame, src Image) {
	if !e.denoiser.allocated {
		return
	}
	index, ok := denoiserReferenceAvgIndex(ref)
	if !ok {
		return
	}
	avg := &e.denoiser.runningAvg[index]
	copyPublicImageToVP8(&avg.Img, src)
	padFrameVisibleToCoded(&avg.Img)
	avg.ExtendBorders()
}

func denoiserReferenceAvgIndex(ref ReferenceFrame) (int, bool) {
	switch ref {
	case ReferenceLast:
		return denoiserAvgLast, true
	case ReferenceGolden:
		return denoiserAvgGolden, true
	case ReferenceAltRef:
		return denoiserAvgAltRef, true
	default:
		return 0, false
	}
}
