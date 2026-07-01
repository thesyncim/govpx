package govpx

// SetReferenceFrame replaces a VP9 encoder reference slot with src. ref must
// be [ReferenceLast], [ReferenceGolden], or [ReferenceAltRef]; src must match
// the encoder dimensions and provide valid I420 strides.
//
// This mirrors libvpx's VP8_SET_REFERENCE control on the VP9 encoder: it
// replaces reference pixels without resetting rate-control history, frame
// contexts, motion-vector history, or reference sign-bias state.
func (e *VP9Encoder) SetReferenceFrame(ref ReferenceFrame, src Image) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	slot, ok := vp9PublicReferenceFrameSlot(ref)
	if !ok || !src.validForEncode(e.opts.Width, e.opts.Height) {
		return ErrInvalidConfig
	}
	e.refFrames[slot].store(src)
	e.refWidth[slot] = uint32(e.opts.Width)
	e.refHeight[slot] = uint32(e.opts.Height)
	e.refValid[slot] = true
	e.nextRefMapID++
	e.refMap[slot] = e.nextRefMapID
	e.invalidateVP9SubpelRefBordered()
	if slot == vp9LastRefSlot {
		e.ensureLastBordered()
	}
	return nil
}

// CopyReferenceFrame copies a VP9 encoder reference slot into dst. ref must be
// [ReferenceLast], [ReferenceGolden], or [ReferenceAltRef]; dst must be non-nil,
// match the encoder dimensions, and provide valid I420 strides.
func (e *VP9Encoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if dst == nil {
		return ErrInvalidConfig
	}
	slot, ok := vp9PublicReferenceFrameSlot(ref)
	if !ok || !dst.validForEncode(e.opts.Width, e.opts.Height) ||
		!e.refValid[slot] || !e.refFrames[slot].valid {
		return ErrInvalidConfig
	}
	copyVP9ImageToPublic(dst, e.refFrames[slot].img)
	return nil
}

// SetReferenceFrame replaces a VP9 decoder reference slot with src. ref must
// be [ReferenceLast], [ReferenceGolden], or [ReferenceAltRef]; src must match
// the stream dimensions established by a successfully decoded frame and provide
// valid I420 strides.
func (d *VP9Decoder) SetReferenceFrame(ref ReferenceFrame, src Image) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	slot, ok := vp9PublicReferenceFrameSlot(ref)
	if !ok || !d.vp9ReferenceFramesInitialized() ||
		!src.validForEncode(d.width, d.height) {
		return ErrInvalidConfig
	}
	d.releaseVP9ReferenceFrame(slot)
	d.refFrames[slot].store(src)
	return nil
}

// CopyReferenceFrame copies a VP9 decoder reference slot into dst. ref must be
// [ReferenceLast], [ReferenceGolden], or [ReferenceAltRef]; dst must be
// non-nil, match the active stream dimensions, and provide valid I420 strides.
func (d *VP9Decoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if dst == nil {
		return ErrInvalidConfig
	}
	slot, ok := vp9PublicReferenceFrameSlot(ref)
	if !ok || !d.vp9ReferenceFramesInitialized() ||
		!dst.validForEncode(d.width, d.height) ||
		!d.refFrames[slot].valid {
		return ErrInvalidConfig
	}
	copyVP9ImageToPublic(dst, d.refFrames[slot].img)
	return nil
}

// CopyCurrentFrame copies the most recently shown VP9 frame into dst without
// consuming the decoder's NextFrame queue. This mirrors libvpx's
// VP9_GET_REFERENCE control, which exposes the current show-frame buffer rather
// than one of the LAST/GOLDEN/ALTREF reference slots.
func (d *VP9Decoder) CopyCurrentFrame(dst *Image) error {
	if d == nil || d.closed {
		return ErrClosed
	}
	if dst == nil || !d.lastInfoValid || !d.lastInfo.ShowFrame ||
		!dst.validForEncode(d.lastFrame.Width, d.lastFrame.Height) {
		return ErrInvalidConfig
	}
	copyVP9ImageToPublic(dst, d.lastFrame)
	return nil
}

func (d *VP9Decoder) vp9ReferenceFramesInitialized() bool {
	return d.initialized && d.width > 0 && d.height > 0
}

func vp9PublicReferenceFrameSlot(ref ReferenceFrame) (int, bool) {
	switch ref {
	case ReferenceLast:
		return vp9LastRefSlot, true
	case ReferenceGolden:
		return vp9GoldenRefSlot, true
	case ReferenceAltRef:
		return vp9AltRefSlot, true
	default:
		return 0, false
	}
}
