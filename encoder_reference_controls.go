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
	if _, ok := e.referenceFrameBuffer(ref); !ok || !src.validForEncode(e.opts.Width, e.opts.Height) {
		return ErrInvalidConfig
	}
	refs := e.referenceAliasGroup(ref)
	for _, aliasedRef := range refs {
		fb, _ := e.referenceFrameBuffer(aliasedRef)
		copyPublicImageToVP8(&fb.Img, src)
		padFrameVisibleToCoded(&fb.Img)
		fb.ExtendBorders()
	}
	e.invalidateReferenceFrameState()
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

// referenceAliasGroup returns the public references that currently share the
// same libvpx reference-buffer identity as ref. VP8_SET_REFERENCE writes the
// underlying YV12 buffer, not just the named public slot, so replacing an
// aliased reference must update every govpx buffer in that alias group while
// leaving the alias metadata itself unchanged.
func (e *VP8Encoder) referenceAliasGroup(ref ReferenceFrame) []ReferenceFrame {
	refs := []ReferenceFrame{ref}
	add := func(candidate ReferenceFrame) {
		for _, existing := range refs {
			if existing == candidate {
				return
			}
		}
		refs = append(refs, candidate)
	}
	switch ref {
	case ReferenceLast:
		if e.goldenRefAliasesLast {
			add(ReferenceGolden)
		}
		if e.altRefAliasesLast {
			add(ReferenceAltRef)
		}
	case ReferenceGolden:
		if e.goldenRefAliasesLast {
			add(ReferenceLast)
		}
		if e.goldenRefAliasesAlt {
			add(ReferenceAltRef)
		}
	case ReferenceAltRef:
		if e.altRefAliasesLast {
			add(ReferenceLast)
		}
		if e.goldenRefAliasesAlt {
			add(ReferenceGolden)
		}
	}
	return refs
}

// invalidateReferenceFrameState clears encoder state that assumes reference
// pixels only change through the normal VP8 refresh/copy path.
func (e *VP8Encoder) invalidateReferenceFrameState() {
	e.lastFrameInterModesValid = false
	e.interRDFrameRefSearchOrderValid = false
	clearUint8Map(e.consecZeroLast)
	clearUint8Map(e.consecZeroLastMVBias)
	e.lastInterZeroMVCount = 0
	e.mbsZeroLastDotSuppress = 0
	e.sourceAltRefActive = false
	e.clearAltRefSchedule()
}
