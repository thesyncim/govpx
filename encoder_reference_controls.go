package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

// SetReferenceFrame replaces ref with src. ref must be ReferenceLast,
// ReferenceGolden, or ReferenceAltRef; src must match the encoder
// dimensions and provide valid I420 strides. The encoder pads coded
// edges and extends borders. Like libvpx's VP8_SET_REFERENCE control,
// this replaces reference pixels without resetting cross-frame motion or
// rate-control history.
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
	if e.deferReferenceSetToLookaheadEntry() {
		e.queueLookaheadReferenceSet(ref, src)
		return nil
	}
	e.setReferenceFrameNow(ref, src)
	return nil
}

func (e *VP8Encoder) setReferenceFrameNow(ref ReferenceFrame, src Image) {
	refs := e.referenceAliasGroup(ref)
	for _, aliasedRef := range refs {
		fb, _ := e.referenceFrameBuffer(aliasedRef)
		copyPublicImageToVP8(&fb.Img, src)
		padFrameVisibleToCoded(&fb.Img)
		fb.ExtendBorders()
	}
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
	if latest, ok := e.latestLookaheadReferenceSet(ref); ok {
		copyPublicToPublicImage(dst, latest)
		return nil
	}
	copyVP8ImageToPublic(dst, &fb.Img)
	return nil
}

func (e *VP8Encoder) deferReferenceSetToLookaheadEntry() bool {
	return e.lookaheadEnabled() && (e.lookaheadSize() > 0 || e.autoAltRefStashValid)
}

func (e *VP8Encoder) queueLookaheadReferenceSet(ref ReferenceFrame, src Image) {
	e.nextReferenceSetSeq++
	set := queuedReferenceSet{
		ref: ref,
		img: clonePublicImage(src),
		seq: e.nextReferenceSetSeq,
	}
	e.pendingLookaheadSetReferences = upsertQueuedReferenceSet(e.pendingLookaheadSetReferences, set)
	e.latestLookaheadSetReferences = upsertQueuedReferenceSet(e.latestLookaheadSetReferences, set)
}

func (e *VP8Encoder) applyQueuedReferenceSets(sets []queuedReferenceSet) {
	for _, set := range sets {
		e.setReferenceFrameNow(set.ref, set.img)
		e.clearLatestLookaheadReferenceSet(set)
	}
}

func (e *VP8Encoder) latestLookaheadReferenceSet(ref ReferenceFrame) (Image, bool) {
	for i := len(e.latestLookaheadSetReferences) - 1; i >= 0; i-- {
		if e.latestLookaheadSetReferences[i].ref == ref {
			return e.latestLookaheadSetReferences[i].img, true
		}
	}
	return Image{}, false
}

func appendLookaheadReferenceSets(dst []queuedReferenceSet, src []queuedReferenceSet) []queuedReferenceSet {
	for _, set := range src {
		dst = append(dst, queuedReferenceSet{
			ref: set.ref,
			img: clonePublicImage(set.img),
			seq: set.seq,
		})
	}
	return dst
}

func upsertQueuedReferenceSet(dst []queuedReferenceSet, set queuedReferenceSet) []queuedReferenceSet {
	for i := range dst {
		if dst[i].ref == set.ref {
			clearQueuedReferenceSet(&dst[i])
			dst[i] = set
			return dst
		}
	}
	return append(dst, set)
}

func (e *VP8Encoder) clearPendingLookaheadReferenceSets() {
	clearQueuedReferenceSets(e.pendingLookaheadSetReferences)
	e.pendingLookaheadSetReferences = e.pendingLookaheadSetReferences[:0]
}

func (e *VP8Encoder) clearLatestLookaheadReferenceSets() {
	clearQueuedReferenceSets(e.latestLookaheadSetReferences)
	e.latestLookaheadSetReferences = e.latestLookaheadSetReferences[:0]
}

func (e *VP8Encoder) clearLatestLookaheadReferenceFrame(ref ReferenceFrame) {
	for i := 0; i < len(e.latestLookaheadSetReferences); {
		if e.latestLookaheadSetReferences[i].ref != ref {
			i++
			continue
		}
		clearQueuedReferenceSet(&e.latestLookaheadSetReferences[i])
		copy(e.latestLookaheadSetReferences[i:], e.latestLookaheadSetReferences[i+1:])
		last := len(e.latestLookaheadSetReferences) - 1
		clearQueuedReferenceSet(&e.latestLookaheadSetReferences[last])
		e.latestLookaheadSetReferences = e.latestLookaheadSetReferences[:last]
	}
}

func (e *VP8Encoder) clearLatestLookaheadReferenceSet(applied queuedReferenceSet) {
	for i := range e.latestLookaheadSetReferences {
		set := &e.latestLookaheadSetReferences[i]
		if set.ref != applied.ref || set.seq != applied.seq {
			continue
		}
		clearQueuedReferenceSet(set)
		copy(e.latestLookaheadSetReferences[i:], e.latestLookaheadSetReferences[i+1:])
		last := len(e.latestLookaheadSetReferences) - 1
		clearQueuedReferenceSet(&e.latestLookaheadSetReferences[last])
		e.latestLookaheadSetReferences = e.latestLookaheadSetReferences[:last]
		return
	}
}

func clearQueuedReferenceSets(sets []queuedReferenceSet) {
	for i := range sets {
		clearQueuedReferenceSet(&sets[i])
	}
}

func clearQueuedReferenceSet(set *queuedReferenceSet) {
	if set == nil {
		return
	}
	set.ref = 0
	set.img = Image{}
	set.seq = 0
}

func clonePublicImage(src Image) Image {
	dst := Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       make([]byte, len(src.Y)),
		U:       make([]byte, len(src.U)),
		V:       make([]byte, len(src.V)),
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)
	return dst
}

func copyPublicToPublicImage(dst *Image, src Image) {
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
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
