package govpx

// Ported from libvpx v1.16.0 vp8/encoder/onyx_if.c (vp8_get_compressed_data
// auto-ARF branch) plus the ARF pending bookkeeping in
// update_alt_ref_frame_stats / update_golden_frame_stats. govpx mirrors the
// hidden-frame insertion shape: when source_alt_ref_pending is set, the next
// encoder step peeks the future lookahead window and emits the peeked frame
// as a hidden ARF (show_frame=0, refresh_alt_ref=1, no LAST/GOLDEN refresh).
// The deferred original source pops normally on the next call and is flagged
// is_src_frame_alt_ref when the popped entry matches the alt_ref_source the
// hidden frame was peeked from.
//
// API impedance note: libvpx splits raw-frame intake (vp8_receive_raw_frame)
// from packet production (vp8_get_compressed_data). govpx's EncodeInto fuses
// the two: each call provides one raw input and consumes/produces at most one
// packet. Because a hidden ARF peeks but does not pop, the lookahead queue
// would be unable to absorb the caller's next input without a stash slot.
// autoAltRefPendingPush carries that input across the hidden-ARF boundary;
// the next auto-ARF call drains the stash before pushing the new caller
// input, mirroring the libvpx pattern of "push raw frame, then maybe peek
// for hidden ARF".

// defaultAutoAltRefInterval mirrors libvpx onyx_int.h DEFAULT_GF_INTERVAL.
const defaultAutoAltRefInterval = 7

// autoAltRefEnabled reports whether automatic alt-ref scheduling should run.
// Mirrors libvpx's gating in vp8_get_compressed_data:
//
//	cpi->oxcf.error_resilient_mode == 0 && cpi->oxcf.play_alternate
//
// plus a positive lookahead lag (auto-ARF needs future frames to peek).
func (e *VP8Encoder) autoAltRefEnabled() bool {
	if e == nil {
		return false
	}
	if !e.opts.AutoAltRef || e.opts.ErrorResilient {
		return false
	}
	if !e.lookaheadEnabled() {
		return false
	}
	return true
}

// autoAltRefBaselineInterval mirrors libvpx's baseline_gf_interval clamp:
// the ARF peek offset must be < lookahead depth (libvpx asserts
// index < max_sz - 1 in vp8_lookahead_peek). DEFAULT_GF_INTERVAL is 7;
// shorter lookaheads collapse to (lag - 1).
func (e *VP8Encoder) autoAltRefBaselineInterval() int {
	interval := defaultAutoAltRefInterval
	maxAllowed := e.opts.LookaheadFrames - 1
	if maxAllowed < 1 {
		maxAllowed = 1
	}
	if interval > maxAllowed {
		interval = maxAllowed
	}
	return interval
}

// schedulePendingAltRef sets source_alt_ref_pending and frames_till_gf_update_due
// when we are not already in the middle of an ARF group and the queue has
// enough future frames to satisfy a non-zero peek. Mirrors the cadence libvpx
// derives from update_alt_ref_frame_stats / update_golden_frame_stats: after a
// hidden ARF the encoder must wait for the deferred show frame to surface
// before re-arming pending; the inter-only countdown then drains by 1 per
// committed inter frame and re-arms when it reaches 0.
func (e *VP8Encoder) schedulePendingAltRef() {
	if e == nil || !e.autoAltRefEnabled() {
		return
	}
	if e.sourceAltRefPending {
		return
	}
	if e.altRefSourcePTSValid {
		// Hidden ARF emitted; waiting for the deferred show frame to pop.
		// Do not drain framesTilArf during the deferred-window so the
		// next ARF is scheduled at least one full interval after the
		// previous group's deferred show frame.
		return
	}
	if e.framesTilArf > 0 {
		e.framesTilArf--
		return
	}
	interval := e.autoAltRefBaselineInterval()
	if interval <= 0 {
		return
	}
	if e.lookaheadSize() <= interval {
		// Need at least interval+1 entries so peek(interval) is valid and
		// the deferred near frames remain in the queue.
		return
	}
	e.sourceAltRefPending = true
	e.framesTilArf = interval
}

// encodeAutoAltRefInto orchestrates the libvpx auto-ARF branch. It first
// drains the pending-push stash (if any), then pushes the new caller input
// and decides whether to emit a hidden ARF or pop a deferred frame as a
// normal show frame.
func (e *VP8Encoder) encodeAutoAltRefInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error) {
	if err := validateEncodeFlags(flags); err != nil {
		return EncodeResult{}, err
	}
	// Drain any input held across a previous hidden-ARF boundary.
	if err := e.drainAutoAltRefPendingPush(); err != nil {
		return EncodeResult{}, err
	}
	// If the lookahead is already at capacity (because a hidden ARF was
	// emitted previously without popping), stash the new caller input
	// instead of failing the push, then emit the hidden ARF / deferred
	// show using the existing queue contents.
	if e.lookaheadSize() >= e.opts.LookaheadFrames {
		if err := e.stashAutoAltRefPendingPush(src, pts, duration, flags); err != nil {
			return EncodeResult{}, err
		}
	} else {
		if err := e.pushLookahead(sourceImageFromImage(src), pts, duration, flags); err != nil {
			return EncodeResult{}, err
		}
	}
	if e.lookaheadSize() < e.opts.LookaheadFrames {
		return EncodeResult{}, ErrFrameNotReady
	}
	if result, ok, err := e.tryEmitHiddenAltRef(dst); ok {
		return result, err
	}
	return e.encodeNextDeferredAutoAltRef(dst, false)
}

// flushAutoAltRefInto drains the lookahead at end-of-stream while honoring a
// pending hidden ARF. libvpx clears any outstanding pending ARF before the
// drain completes by either (a) emitting it if peek succeeds or (b) abandoning
// the pending flag if the queue no longer reaches the peek offset.
func (e *VP8Encoder) flushAutoAltRefInto(dst []byte) (EncodeResult, error) {
	if err := e.drainAutoAltRefPendingPush(); err != nil {
		return EncodeResult{}, err
	}
	if result, ok, err := e.tryEmitHiddenAltRef(dst); ok {
		return result, err
	}
	if e.sourceAltRefPending {
		// We can no longer satisfy the peek for the pending ARF (lookahead
		// shorter than frames_till_gf_update_due). Abandon the request so
		// the rest of the queue drains as normal show frames.
		e.sourceAltRefPending = false
	}
	return e.encodeNextDeferredAutoAltRef(dst, true)
}

// stashAutoAltRefPendingPush deep-copies the caller's source into the stash
// slot, allocating the slot's frame buffer on first use. The stash holds a
// single in-flight input that could not be pushed because the lookahead is
// at capacity after a peek-only hidden-ARF call.
func (e *VP8Encoder) stashAutoAltRefPendingPush(src Image, pts uint64, duration uint64, flags EncodeFlags) error {
	if e.autoAltRefHasPendingPush {
		// At most one in-flight stash entry: hidden ARFs do not chain
		// in libvpx (the deferred show frame must pop before the next
		// pending ARF is scheduled), so a second stash request indicates
		// a logic error in the auto-ARF state machine.
		return ErrInvalidConfig
	}
	if e.autoAltRefPendingPush.frame.Img.Y == nil {
		if err := e.autoAltRefPendingPush.frame.Resize(e.opts.Width, e.opts.Height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	copySourceToFrameBuffer(&e.autoAltRefPendingPush.frame, sourceImageFromImage(src))
	e.autoAltRefPendingPush.pts = pts
	e.autoAltRefPendingPush.duration = duration
	if e.autoAltRefPendingPush.duration == 0 {
		e.autoAltRefPendingPush.duration = 1
	}
	e.autoAltRefPendingPush.flags = flags
	e.autoAltRefHasPendingPush = true
	return nil
}

// drainAutoAltRefPendingPush flushes the stash slot back into the lookahead
// queue. Called at the start of each auto-ARF encode/flush call so the queue
// state reflects the caller's input order.
func (e *VP8Encoder) drainAutoAltRefPendingPush() error {
	if !e.autoAltRefHasPendingPush {
		return nil
	}
	stash := &e.autoAltRefPendingPush
	if err := e.pushLookahead(sourceImageFromVP8(&stash.frame.Img), stash.pts, stash.duration, stash.flags); err != nil {
		return err
	}
	stash.pts, stash.duration, stash.flags = 0, 0, 0
	e.autoAltRefHasPendingPush = false
	return nil
}

// tryEmitHiddenAltRef peeks the future window at offset framesTilArf when an
// ARF is pending and emits the peeked entry as a hidden frame. Mirrors
// libvpx's branch in vp8_get_compressed_data:
//
//	cpi->source = vp8_lookahead_peek(lookahead, frames_till_gf_update_due, PEEK_FORWARD);
//	cpi->alt_ref_source = cpi->source;
//	cm->refresh_alt_ref_frame = 1;
//	cm->refresh_golden_frame = 0;
//	cm->refresh_last_frame = 0;
//	cm->show_frame = 0;
//	cpi->source_alt_ref_pending = 0;
//
// The boolean result reports whether an ARF was emitted (true) so the caller
// can skip the regular show-frame path. err is propagated from the underlying
// encodeSourceInto.
func (e *VP8Encoder) tryEmitHiddenAltRef(dst []byte) (EncodeResult, bool, error) {
	if !e.sourceAltRefPending {
		return EncodeResult{}, false, nil
	}
	entry := e.peekLookahead(e.framesTilArf, true)
	if entry == nil {
		return EncodeResult{}, false, nil
	}
	source := sourceImageFromVP8(&entry.frame.Img)
	hiddenFlags := autoAltRefHiddenFrameFlags()
	meta := encodeSourceMetadata{lookaheadDepth: e.lookaheadSize()}
	e.altRefSourcePTS = entry.pts
	e.altRefSourcePTSValid = true
	e.isSrcFrameAltRef = false
	result, err := e.encodeSourceInto(dst, source, entry.pts, entry.duration, hiddenFlags, meta)
	if err != nil {
		// Roll back the alt-ref source bookkeeping; the caller may retry.
		e.altRefSourcePTSValid = false
		return EncodeResult{}, true, err
	}
	// Successful hidden-ARF emit. Clear pending flag (libvpx's
	// cpi->source_alt_ref_pending = 0) and leave framesTilArf untouched as
	// the per-frame countdown to the deferred show frame.
	e.sourceAltRefPending = false
	return result, true, nil
}

// encodeNextDeferredAutoAltRef pops the next lookahead entry and encodes it
// as a normal show frame. When the popped entry matches alt_ref_source
// (PTS comparison mirrors libvpx's pointer compare cpi->source ==
// cpi->alt_ref_source), is_src_frame_alt_ref is set so future hooks may skip
// processing. drain controls the libvpx-style end-of-stream path.
func (e *VP8Encoder) encodeNextDeferredAutoAltRef(dst []byte, drain bool) (EncodeResult, error) {
	entry, ok := e.popLookahead(drain)
	if !ok {
		return EncodeResult{}, ErrFrameNotReady
	}
	e.isSrcFrameAltRef = false
	if e.altRefSourcePTSValid && entry.pts == e.altRefSourcePTS {
		e.isSrcFrameAltRef = true
		e.altRefSourcePTSValid = false
	}
	meta := encodeSourceMetadata{lookaheadDepth: e.lookaheadSize()}
	result, err := e.encodeSourceInto(dst, sourceImageFromVP8(&entry.frame.Img), entry.pts, entry.duration, entry.flags, meta)
	e.clearPoppedLookahead(entry)
	if err != nil {
		return EncodeResult{}, err
	}
	if !result.Dropped && !result.KeyFrame {
		e.schedulePendingAltRef()
	}
	if result.KeyFrame {
		// Key frame resets the auto-ARF state machine, mirroring libvpx's
		// onyx_if.c key-frame branch (cpi->source_alt_ref_pending = 0,
		// cpi->source_alt_ref_active = 0). sourceAltRefActive is already
		// cleared by resetGoldenFrameStats; we clear the pending flag and
		// the deferred-show tracker here.
		e.sourceAltRefPending = false
		e.altRefSourcePTSValid = false
		e.framesTilArf = 0
		e.isSrcFrameAltRef = false
	}
	return result, nil
}

// autoAltRefHiddenFrameFlags builds the EncodeFlags used to encode a hidden
// alt-ref frame. Mirrors libvpx's setup:
//
//	cm->refresh_alt_ref_frame = 1;
//	cm->refresh_golden_frame = 0;
//	cm->refresh_last_frame   = 0;
//	cm->show_frame           = 0;
//	cm->refresh_entropy_probs = 0; // hidden ARF does not commit entropy
//
// The original lookahead-entry flags (e.g. user-supplied EncodeForceKeyFrame)
// are intentionally suppressed for the hidden frame; libvpx never honors
// caller flags on the auto-inserted ARF.
func autoAltRefHiddenFrameFlags() EncodeFlags {
	return EncodeForceAltRefFrame |
		EncodeInvisibleFrame |
		EncodeNoUpdateLast |
		EncodeNoUpdateGolden |
		EncodeNoUpdateEntropy
}

