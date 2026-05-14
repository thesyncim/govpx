package govpx

// libvpx vp8/encoder/onyx_int.h DEFAULT_GF_INTERVAL.
const autoAltRefDefaultSectionInterval = 7

// autoAltRefHiddenFlags is the libvpx hidden alt-ref encode flag combination:
// refresh_alt_ref_frame=1, show_frame=0, no LAST/GOLDEN refresh, and no entropy
// update so the deferred show frame can still drive entropy. See
// vp8/encoder/onyx_if.c vp8_get_compressed_data: the auto-ARF branch sets
// cm->refresh_alt_ref_frame=1, cm->refresh_golden_frame=0,
// cm->refresh_last_frame=0, cm->show_frame=0.
const autoAltRefHiddenFlags = EncodeForceAltRefFrame |
	EncodeInvisibleFrame |
	EncodeNoUpdateLast |
	EncodeNoUpdateGolden |
	EncodeNoUpdateEntropy

// autoAltRefDriverEnabled mirrors libvpx's `cpi->oxcf.play_alternate &&
// !cpi->oxcf.error_resilient_mode && cpi->oxcf.lag_in_frames` guard. The driver
// also requires LookaheadFrames > 1 because LookaheadFrames == 1 leaves no
// future entries to peek at the schedule's offset.
//
// Additionally, libvpx vp8/encoder/ratectrl.c `calc_pframe_target_size`
// explicitly sets `cpi->source_alt_ref_pending = 0` on every one-pass
// (`cpi->pass != 2`) frame, with the only one-pass arming path
// (`update_golden_frame_stats`) gated on `cpi->oxcf.fixed_q >= 0`. Govpx does
// not support libvpx's fixed-Q mode, so in one-pass mode the hidden ARF stream
// must stay empty for byte parity with vpxenc. The schedule+emit cycle is
// therefore gated on `twoPass.enabled()`; the two-pass arming path lives in
// `pass2MaybeArmAltRefPending`.
func (e *VP8Encoder) autoAltRefDriverEnabled() bool {
	if !e.opts.AutoAltRef {
		return false
	}
	if e.opts.ErrorResilient {
		return false
	}
	if e.opts.LookaheadFrames <= 1 {
		return false
	}
	if !e.twoPass.enabled() {
		return false
	}
	return e.lookaheadEnabled()
}

// autoAltRefSectionInterval returns the libvpx-aligned distance between the
// schedule frame and the hidden ARF source. This is libvpx's
// DEFAULT_GF_INTERVAL=7 clamped to the largest forward peek the lookahead
// queue can currently satisfy. peekLookahead requires both
// `index < max_sz - 1` (LookaheadFrames - 1) and `index < count`, so the
// reachable upper bound is the smaller of the two.
func (e *VP8Encoder) autoAltRefSectionInterval() int {
	interval := autoAltRefDefaultSectionInterval
	if maxOffset := e.opts.LookaheadFrames - 1; interval > maxOffset {
		interval = maxOffset
	}
	if maxOffset := e.lookaheadSize() - 1; interval > maxOffset {
		interval = maxOffset
	}
	if interval < 1 {
		return 0
	}
	return interval
}

// autoAltRefMaybeSchedule arms the next hidden alt-ref insertion. It is the
// govpx counterpart to the libvpx `source_alt_ref_pending = 1` decision that
// happens inside `calc_pframe_target_size` on a GF/auto-ARF section boundary.
// govpx schedules eagerly on any inter-frame commit when the lookahead has at
// least one section interval of future frames available; libvpx's full
// boost-driven `select_arf_period` decision is gated on first-pass stats not
// yet ported here.
func (e *VP8Encoder) autoAltRefMaybeSchedule() {
	if !e.autoAltRefDriverEnabled() {
		return
	}
	if e.twoPass.enabled() {
		// In two-pass mode the GF/ARF section decision is made from
		// FIRSTPASS_STATS in pass2MaybeArmAltRefPending. Do not let the
		// one-pass default interval fallback arm an ARF that libvpx's
		// second-pass planner rejected.
		return
	}
	if e.sourceAltRefPending {
		return
	}
	if e.sourceAltRefActive {
		// libvpx only schedules a fresh ARF after the previous section's
		// `source_alt_ref_active` lifecycle clears. Wait for that.
		return
	}
	interval := e.autoAltRefSectionInterval()
	if interval <= 0 {
		return
	}
	// libvpx's auto-ARF peek index is `frames_till_gf_update_due`; the
	// reachable future entry is at offset `interval` from the next pop. The
	// queue must hold at least `interval+1` entries (head + `interval`
	// future).
	future := e.lookaheadFutureEntry(interval)
	if future == nil {
		return
	}
	e.scheduleAltRefSource(future.pts, interval)
}

// autoAltRefShouldEmitHidden reports whether the next encoder call should
// emit a hidden alt-ref instead of advancing the normal pop. Mirrors libvpx
// `vp8_get_compressed_data`:
//
//	if (cpi->oxcf.error_resilient_mode == 0 && cpi->oxcf.play_alternate &&
//	    cpi->source_alt_ref_pending) {
//	    if ((cpi->source = vp8_lookahead_peek(
//	             cpi->lookahead, cpi->frames_till_gf_update_due, PEEK_FORWARD))) {
//	        ...
//	    }
//	}
//
// libvpx fires the hidden ARF on the first call after `source_alt_ref_pending`
// is set, peeking the lookahead at the schedule offset. The earlier govpx
// model deferred emission until the ARF source reached the head of the queue;
// matching libvpx's emission timing requires that the schedule offset already
// be reachable inside the lookahead, which is what `peekLookahead` validates.
func (e *VP8Encoder) autoAltRefShouldEmitHidden() bool {
	if !e.autoAltRefDriverEnabled() {
		return false
	}
	if !e.sourceAltRefPending {
		return false
	}
	if e.lookaheadSize() == 0 {
		return false
	}
	offset := e.altRefPeekOffset()
	if offset < 0 {
		return false
	}
	if e.peekLookahead(offset, true) == nil {
		return false
	}
	return true
}

// altRefPeekOffset returns the lookahead offset at which the hidden ARF
// source lives. It mirrors libvpx's `cpi->frames_till_gf_update_due` at the
// moment of `vp8_get_compressed_data`'s ARF block: that counter is decremented
// once during the keyframe encode (vp8/encoder/onyx_if.c
// update_golden_frame_stats: `if (frames_till_gf_update_due > 0)
// frames_till_gf_update_due--`), so by the time the immediately following
// frame call enters the ARF block the peek offset is `baseline_gf_interval -
// 1`. govpx's `framesTillAltRefFrame` is updated through the same lifecycle
// (see `updateGoldenFrameStats` and the keyframe-path decrement inside
// `resetGoldenFrameStats`), so we can use it directly as the peek index.
func (e *VP8Encoder) altRefPeekOffset() int {
	if e.framesTillAltRefFrame < 0 {
		return -1
	}
	return e.framesTillAltRefFrame
}

// autoAltRefStashInput tucks the caller's input frame into the single-slot
// stash so the lookahead queue can stay at capacity while a hidden ARF is
// emitted. The next driver-aware EncodeInto call drains the stash before
// pushing the new source. Returns ErrFrameNotReady if the stash is already
// occupied; callers should treat that as an internal invariant violation.
func (e *VP8Encoder) autoAltRefStashInput(src Image, pts uint64, duration uint64, flags EncodeFlags) error {
	if e.autoAltRefStashValid {
		return ErrFrameNotReady
	}
	if e.autoAltRefStashFrame.Img.YStride == 0 ||
		e.autoAltRefStashFrame.Img.Width != e.opts.Width ||
		e.autoAltRefStashFrame.Img.Height != e.opts.Height {
		if err := e.autoAltRefStashFrame.Resize(e.opts.Width, e.opts.Height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	flags = e.consumeForceKeyFrameForInput(flags)
	copySourceToFrameBuffer(&e.autoAltRefStashFrame, sourceImageFromImage(src))
	e.autoAltRefStashPTS = pts
	e.autoAltRefStashDuration = duration
	if e.autoAltRefStashDuration == 0 {
		e.autoAltRefStashDuration = 1
	}
	e.autoAltRefStashFlags = flags
	e.autoAltRefStashForceLF = e.consumePendingLFDeltaUpdate()
	e.autoAltRefStashValid = true
	return nil
}

// autoAltRefDrainStash flushes the pending stashed input (if any) into the
// lookahead queue. Called at the top of every auto-ARF aware EncodeInto call
// before the new caller input is processed.
func (e *VP8Encoder) autoAltRefDrainStash() error {
	if !e.autoAltRefStashValid {
		return nil
	}
	src := sourceImageFromVP8(&e.autoAltRefStashFrame.Img)
	pts := e.autoAltRefStashPTS
	duration := e.autoAltRefStashDuration
	flags := e.autoAltRefStashFlags
	forceLFDeltaUpdate := e.autoAltRefStashForceLF
	e.autoAltRefStashValid = false
	e.autoAltRefStashPTS = 0
	e.autoAltRefStashDuration = 0
	e.autoAltRefStashFlags = 0
	e.autoAltRefStashForceLF = false
	return e.pushLookaheadWithForce(src, pts, duration, flags, forceLFDeltaUpdate)
}

// autoAltRefMaybeEncode is the EncodeInto hook for the automatic ARF driver.
// It executes one of three actions depending on the driver state:
//
//  1. Emit a hidden alt-ref for the lookahead head, stash the caller's input,
//     and return the hidden packet (when the driver is armed and ready).
//  2. Drain a previously stashed caller input into the lookahead, encode the
//     head as a normal show frame, stash the new caller input, and return
//     the visible packet (the steady-state libvpx-faithful "shifted by one"
//     mode the driver enters after the first hidden ARF, mirroring how
//     vp8_get_compressed_data alternates between hidden ARF emission and
//     normal pop while the application keeps pushing source frames).
//  3. Return (_, false, nil) so the caller's normal lookahead path handles
//     the call (driver disabled or no work to do).
func (e *VP8Encoder) autoAltRefMaybeEncode(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, bool, error) {
	if !e.autoAltRefDriverEnabled() {
		return EncodeResult{}, false, nil
	}
	hadStash := e.autoAltRefStashValid
	// Drain a previously stashed caller input first; that frame logically
	// queued before `src`, and pushing it now keeps the lookahead FIFO
	// ordering consistent. After the first hidden ARF emission the queue
	// will reach capacity once the stash drains, so subsequent EncodeInto
	// calls are handled here rather than falling through to
	// encodeLookaheadInto (which would attempt a second push and overflow).
	if err := e.autoAltRefDrainStash(); err != nil {
		return EncodeResult{}, true, err
	}
	if e.autoAltRefShouldEmitHidden() {
		if err := validateEncodeFlags(flags); err != nil {
			return EncodeResult{}, true, err
		}
		offset := e.altRefPeekOffset()
		peeked := e.peekLookahead(offset, true)
		if peeked == nil {
			return EncodeResult{}, false, nil
		}
		hiddenSource := sourceImageFromVP8(&peeked.frame.Img)
		hiddenPTS := peeked.pts
		hiddenDuration := peeked.duration
		if hiddenDuration == 0 {
			hiddenDuration = 1
		}
		// libvpx vp8/encoder/onyx_if.c sets cpi->alt_ref_source = cpi->source
		// at the moment of ARF emission. govpx uses the source PTS as the
		// alt-ref identifier so the deferred show frame's
		// `is_src_frame_alt_ref` check matches when the lookahead later pops
		// this entry. Re-anchor altRefSourcePTS to the actual peeked entry
		// in case the schedule was armed with an extrapolated PTS that does
		// not exactly match the queued frame (e.g. variable-duration input).
		e.altRefSourcePTS = hiddenPTS
		e.altRefSourceValid = true
		if err := e.autoAltRefStashInput(src, pts, duration, flags); err != nil {
			return EncodeResult{}, true, err
		}
		meta := encodeSourceMetadata{
			lookaheadDepth:     e.lookaheadSize(),
			forceLFDeltaUpdate: peeked.forceLFDeltaUpdate,
		}
		result, err := e.encodeSourceInto(dst, hiddenSource, hiddenPTS, hiddenDuration, autoAltRefHiddenFlags, meta)
		if err != nil {
			return EncodeResult{}, true, err
		}
		peeked.forceLFDeltaUpdate = false
		return result, true, nil
	}
	if !hadStash {
		// No stashed input means the lookahead is at its normal pre-push
		// level; the standard encodeLookaheadInto path handles this call.
		return EncodeResult{}, false, nil
	}
	// Stash drain pushed the deferred input into the lookahead and the queue
	// is now at capacity. Encode the head as the matching deferred show
	// frame, then stash the caller's new input for the next EncodeInto call.
	if err := validateEncodeFlags(flags); err != nil {
		return EncodeResult{}, true, err
	}
	if e.lookaheadSize() < e.opts.LookaheadFrames {
		// Defensive: if the queue is not full something else is wrong;
		// fall through to the normal path so the standard error path
		// applies.
		return EncodeResult{}, false, nil
	}
	entry, ok := e.popLookahead(false)
	if !ok {
		return EncodeResult{}, false, nil
	}
	visibleSource := sourceImageFromVP8(&entry.frame.Img)
	visiblePTS := entry.pts
	visibleDuration := entry.duration
	visibleFlags := entry.flags
	if err := e.autoAltRefStashInput(src, pts, duration, flags); err != nil {
		// Restore the popped entry by re-pushing it would violate FIFO
		// ordering; instead surface the stash error after clearing it so
		// callers do not see a permanently lagged encoder.
		e.clearPoppedLookahead(entry)
		return EncodeResult{}, true, err
	}
	meta := encodeSourceMetadata{
		lookaheadDepth:     e.lookaheadSize(),
		forceLFDeltaUpdate: entry.forceLFDeltaUpdate,
	}
	result, err := e.encodeSourceInto(dst, visibleSource, visiblePTS, visibleDuration, visibleFlags, meta)
	e.clearPoppedLookahead(entry)
	if err != nil {
		return EncodeResult{}, true, err
	}
	e.autoAltRefMaybeSchedule()
	return result, true, nil
}

// autoAltRefMaybeEmitHiddenOnFlush handles end-of-stream hidden ARF emission.
// FlushInto drains the lookahead frame-by-frame; when a hidden ARF is armed
// and the queue still has entries, emit the ARF first (without popping) so
// the matching show frame follows on the next FlushInto call.
func (e *VP8Encoder) autoAltRefMaybeEmitHiddenOnFlush(dst []byte) (EncodeResult, bool, error) {
	if !e.autoAltRefDriverEnabled() {
		return EncodeResult{}, false, nil
	}
	if err := e.autoAltRefDrainStash(); err != nil {
		return EncodeResult{}, true, err
	}
	if !e.autoAltRefShouldEmitHidden() {
		return EncodeResult{}, false, nil
	}
	offset := e.altRefPeekOffset()
	peeked := e.peekLookahead(offset, true)
	if peeked == nil {
		return EncodeResult{}, false, nil
	}
	hiddenSource := sourceImageFromVP8(&peeked.frame.Img)
	hiddenPTS := peeked.pts
	hiddenDuration := peeked.duration
	if hiddenDuration == 0 {
		hiddenDuration = 1
	}
	e.altRefSourcePTS = hiddenPTS
	e.altRefSourceValid = true
	meta := encodeSourceMetadata{
		lookaheadDepth:     e.lookaheadSize(),
		forceLFDeltaUpdate: peeked.forceLFDeltaUpdate || e.consumePendingLFDeltaUpdate(),
	}
	result, err := e.encodeSourceInto(dst, hiddenSource, hiddenPTS, hiddenDuration, autoAltRefHiddenFlags, meta)
	if err != nil {
		return EncodeResult{}, true, err
	}
	peeked.forceLFDeltaUpdate = false
	return result, true, nil
}
