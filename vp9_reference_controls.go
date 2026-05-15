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
