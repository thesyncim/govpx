package govpx

import "image"

const (
	vp9MaxLookaheadFrames  = 25
	vp9MinLookaheadForARFs = 4
)

type vp9LookaheadEntry struct {
	img            image.YCbCr
	flags          EncodeFlags
	isAltRefSource bool
}

func (e *VP9Encoder) initVP9Lookahead(width int, height int, depth int) {
	e.autoAltRefPendingSet = false
	e.autoAltRefEmitted = false
	if depth <= 0 {
		e.lookahead = nil
		e.lookaheadRead = 0
		e.lookaheadWrite = 0
		e.lookaheadCount = 0
		e.autoAltRefPending = vp9LookaheadEntry{}
		e.vp9ARNRScratch = image.YCbCr{}
		return
	}
	e.lookahead = make([]vp9LookaheadEntry, depth+1)
	rect := image.Rect(0, 0, width, height)
	for i := range e.lookahead {
		e.lookahead[i].img = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	}
	if e.opts.AutoAltRef {
		e.autoAltRefPending.img = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	}
	if (e.opts.AutoAltRef || e.opts.EnableKeyFrameFiltering) &&
		e.opts.ARNRMaxFrames > 1 {
		// libvpx allocates cpi->tf_buffer whenever either ARNR or
		// keyframe filtering may run; mirror that so the runtime
		// SetEnableKeyFrameFiltering toggle has a destination buffer
		// to write into.
		e.ensureVP9ARNRScratch()
	} else {
		e.vp9ARNRScratch = image.YCbCr{}
	}
	e.lookaheadRead = 0
	e.lookaheadWrite = 0
	e.lookaheadCount = 0
}

func (e *VP9Encoder) ensureVP9ARNRScratch() {
	if e == nil || e.opts.Width <= 0 || e.opts.Height <= 0 {
		return
	}
	if len(e.vp9ARNRScratch.Y) != 0 &&
		e.vp9ARNRScratch.Rect.Dx() == e.opts.Width &&
		e.vp9ARNRScratch.Rect.Dy() == e.opts.Height {
		return
	}
	rect := image.Rect(0, 0, e.opts.Width, e.opts.Height)
	e.vp9ARNRScratch = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
}

func (e *VP9Encoder) vp9LookaheadEnabled() bool {
	return e.opts.LookaheadFrames > 0 && len(e.lookahead) > 0
}

func (e *VP9Encoder) vp9LookaheadSize() int {
	if !e.vp9LookaheadEnabled() {
		return 0
	}
	return int(e.lookaheadCount)
}

func (e *VP9Encoder) encodeVP9LookaheadIntoWithFlagsResult(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	flags = normalizeVP9EncodeFlags(flags)
	if err := validateVP9EncodeFlags(flags); err != nil {
		return VP9EncodeResult{}, err
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return VP9EncodeResult{}, err
	}
	if len(dst) < vp9MinEncodeIntoBuffer {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	if result, ok, err := e.maybeDrainVP9TwoPassLookaheadAndQueueInto(img, dst, flags); ok || err != nil {
		return result, err
	}
	if e.autoAltRefPendingSet {
		return e.encodeVP9AutoAltRefPendingAndQueueInto(img, dst, flags)
	}
	if err := e.pushVP9Lookahead(img, flags); err != nil {
		return VP9EncodeResult{}, err
	}
	// Drain any packet that an earlier parallel batch left staged.
	if e.frameParallel != nil && e.frameParallel.hasPendingResults() {
		if out, ok, err := e.vp9PopFrameParallelResultInto(dst); ok {
			return out, err
		}
	}
	if e.vp9LookaheadSize() < e.opts.LookaheadFrames {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	// Frame-parallel scheduling fires before the auto-altref path because
	// the option pair is mutually exclusive (validated at construction).
	if e.vp9FrameParallelEnabled() {
		if result, ok, err := e.vp9RunFrameParallelBatch(dst, false); ok || err != nil {
			return result, err
		}
	}
	if result, ok, err := e.maybeEncodeVP9TwoPassARFInto(dst, false); ok || err != nil {
		return result, err
	}
	if result, ok, err := e.maybeEncodeVP9AutoAltRefInto(dst); ok || err != nil {
		return result, err
	}
	entry, ok := e.popVP9Lookahead(false)
	if !ok {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	emitFlags, temporalFrame := e.vp9LookaheadEmitFlags(entry.flags)
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&entry.img, dst, emitFlags,
		false, temporalFrame, entry.isAltRefSource)
	entry.flags = 0
	entry.isAltRefSource = false
	return result, err
}

// vp9LookaheadEmitFlags merges the queued source flags with the next
// temporal-layer schedule slot when temporal SVC is enabled, mirroring
// the realtime path's flag/temporal handshake in EncodeIntoWithFlagsResult.
func (e *VP9Encoder) vp9LookaheadEmitFlags(callerFlags EncodeFlags) (EncodeFlags, temporalFrame) {
	tFrame := e.temporal.nextFrame(e.vp9TimingState())
	flags := callerFlags | tFrame.Flags
	flags = normalizeVP9EncodeFlags(flags)
	if e.vp9ShouldEncodeKeyFrame(flags) {
		flags &^= (tFrame.Flags & vp9NoUpdateRefFlags) &^ callerFlags
	}
	return flags, tFrame
}

// FlushInto drains one queued VP9 lookahead frame into dst. Call repeatedly
// until ErrFrameNotReady to empty the delayed source queue.
func (e *VP9Encoder) FlushInto(dst []byte) (int, error) {
	result, err := e.FlushIntoWithResult(dst)
	return len(result.Data), err
}

// FlushIntoWithResult drains one queued VP9 lookahead frame into dst and
// returns packet metadata. It returns ErrFrameNotReady when no queued frame is
// ready or lookahead is disabled.
func (e *VP9Encoder) FlushIntoWithResult(dst []byte) (VP9EncodeResult, error) {
	if e == nil || e.closed {
		return VP9EncodeResult{}, ErrClosed
	}
	if len(dst) < vp9MinEncodeIntoBuffer {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	// Drain any packet staged by a prior parallel batch first.
	if e.frameParallel != nil && e.frameParallel.hasPendingResults() {
		if out, ok, err := e.vp9PopFrameParallelResultInto(dst); ok {
			return out, err
		}
	}
	if e.autoAltRefPendingSet {
		return e.encodeVP9AutoAltRefPendingInto(dst)
	}
	if !e.vp9LookaheadEnabled() || e.vp9LookaheadSize() == 0 {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	// If frame-parallel encoding is enabled, drain remaining queued frames
	// through a (possibly under-full) parallel batch so packet emission
	// stays in display order even when the source stream ends mid-batch.
	if e.vp9FrameParallelEnabled() {
		if result, ok, err := e.vp9RunFrameParallelBatch(dst, true); ok || err != nil {
			return result, err
		}
	}
	if result, ok, err := e.maybeEncodeVP9TwoPassARFInto(dst, true); ok || err != nil {
		return result, err
	}
	entry, ok := e.popVP9Lookahead(true)
	if !ok {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	emitFlags, temporalFrame := e.vp9LookaheadEmitFlags(entry.flags)
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&entry.img, dst, emitFlags,
		false, temporalFrame, entry.isAltRefSource)
	entry.flags = 0
	entry.isAltRefSource = false
	return result, err
}

func (e *VP9Encoder) encodeVP9AutoAltRefPendingAndQueueInto(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, error) {
	if int(e.lookaheadCount)+2 > len(e.lookahead) {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	result, err := e.encodeVP9AutoAltRefPendingInto(dst)
	if err != nil {
		return result, err
	}
	if err := e.pushVP9Lookahead(img, flags); err != nil {
		return VP9EncodeResult{}, err
	}
	if entry, ok := e.popVP9Lookahead(false); ok {
		copyVP9LookaheadImage(&e.autoAltRefPending.img, &entry.img, e.opts.Width, e.opts.Height)
		e.autoAltRefPending.flags = entry.flags
		e.autoAltRefPending.isAltRefSource = entry.isAltRefSource
		e.autoAltRefPendingSet = true
		entry.flags = 0
		entry.isAltRefSource = false
	}
	return result, nil
}

func (e *VP9Encoder) encodeVP9AutoAltRefPendingInto(dst []byte) (VP9EncodeResult, error) {
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&e.autoAltRefPending.img, dst,
		e.autoAltRefPending.flags, false, temporalFrame{LayerCount: 1},
		e.autoAltRefPending.isAltRefSource)
	if err != nil {
		return result, err
	}
	e.autoAltRefPending.flags = 0
	e.autoAltRefPending.isAltRefSource = false
	e.autoAltRefPendingSet = false
	return result, nil
}

func (e *VP9Encoder) maybeEncodeVP9AutoAltRefInto(dst []byte) (VP9EncodeResult, bool, error) {
	if !e.vp9AutoAltRefOnePassEnabled() ||
		e.autoAltRefEmitted || e.autoAltRefPendingSet ||
		e.frameIndex == 0 || e.vp9LookaheadSize() < e.opts.LookaheadFrames {
		return VP9EncodeResult{}, false, nil
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		return VP9EncodeResult{}, false, nil
	}
	future.isAltRefSource = true
	e.rc.altRefGFGroup = true
	hiddenSrc := e.vp9AutoAltRefSourceImage(future)
	result, err := e.encodeVP9FrameIntoWithFlagsResult(hiddenSrc, dst,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden,
		false, temporalFrame{LayerCount: 1}, false)
	if err != nil {
		return result, true, err
	}
	visible, ok := e.popVP9Lookahead(false)
	if !ok {
		return result, true, nil
	}
	copyVP9LookaheadImage(&e.autoAltRefPending.img, &visible.img, e.opts.Width, e.opts.Height)
	e.autoAltRefPending.flags = visible.flags
	e.autoAltRefPending.isAltRefSource = visible.isAltRefSource
	e.autoAltRefPendingSet = true
	e.autoAltRefEmitted = true
	visible.flags = 0
	visible.isAltRefSource = false
	return result, true, nil
}

func (e *VP9Encoder) maybeEncodeVP9TwoPassARFInto(dst []byte, drain bool) (VP9EncodeResult, bool, error) {
	if e == nil || !e.twoPass.enabled() || !e.rc.enabled ||
		e.rc.mode == RateControlCBR || !e.vp9LookaheadEnabled() ||
		e.vp9FrameParallelEnabled() || e.autoAltRefPendingSet ||
		e.frameIndex == 0 || !e.twoPass.gfGroupActive ||
		!e.twoPass.currentFrameIsARFUpdate() {
		return VP9EncodeResult{}, false, nil
	}
	offset := e.twoPass.currentARFSrcOffset()
	if offset >= e.vp9LookaheadSize() {
		if drain {
			return VP9EncodeResult{}, false, nil
		}
		return VP9EncodeResult{}, false, ErrFrameNotReady
	}
	future, ok := e.peekVP9LookaheadAt(offset)
	if !ok {
		if drain {
			return VP9EncodeResult{}, false, nil
		}
		return VP9EncodeResult{}, false, ErrFrameNotReady
	}
	future.isAltRefSource = true
	e.rc.altRefGFGroup = true
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&future.img, dst,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden,
		false, temporalFrame{LayerCount: 1}, false)
	return result, true, err
}

func (e *VP9Encoder) maybeDrainVP9TwoPassLookaheadAndQueueInto(img *image.YCbCr, dst []byte, flags EncodeFlags) (VP9EncodeResult, bool, error) {
	if e == nil || !e.twoPass.enabled() || !e.vp9LookaheadEnabled() ||
		int(e.lookaheadCount)+2 <= len(e.lookahead) {
		return VP9EncodeResult{}, false, nil
	}
	entry, ok := e.popVP9Lookahead(true)
	if !ok {
		return VP9EncodeResult{}, false, ErrFrameNotReady
	}
	emitFlags, temporalFrame := e.vp9LookaheadEmitFlags(entry.flags)
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&entry.img, dst,
		emitFlags, false, temporalFrame, entry.isAltRefSource)
	entry.flags = 0
	entry.isAltRefSource = false
	if err != nil {
		return result, true, err
	}
	if err := e.pushVP9Lookahead(img, flags); err != nil {
		return VP9EncodeResult{}, true, err
	}
	return result, true, nil
}

func (e *VP9Encoder) vp9AutoAltRefOnePassEnabled() bool {
	if e == nil || !e.opts.AutoAltRef || e.twoPass.enabled() {
		return false
	}
	// libvpx: vp9_encoder.h:1149-1152 is_altref_enabled():
	//   !(mode == REALTIME && rc_mode == VPX_CBR) &&
	//   lag_in_frames >= MIN_LOOKAHEAD_FOR_ARFS &&
	//   enable_auto_arf
	if vp9ResolveDeadlineMode(e.opts.Deadline) == vp9ModeRealtime &&
		e.opts.RateControlModeSet && e.opts.RateControlMode == RateControlCBR {
		return false
	}
	if e.opts.LookaheadFrames < vp9MinLookaheadForARFs {
		return false
	}
	if !e.opts.RateControlModeSet || e.opts.RateControlMode != RateControlVBR {
		return false
	}
	// libvpx one-pass scheduling sets rc->source_alt_ref_pending only when
	// sf.use_altref_onepass is live; public-Q/good cpu-used 2 leaves it off.
	return e.sf.UseAltrefOnepass != 0
}

func (e *VP9Encoder) vp9AltRefEnabledForRateControlStats() bool {
	if e == nil || !e.opts.AutoAltRef {
		return false
	}
	// libvpx: vp9_encoder.h:1148 is_altref_enabled().
	if vp9ResolveDeadlineMode(e.opts.Deadline) == vp9ModeRealtime &&
		e.opts.RateControlModeSet && e.opts.RateControlMode == RateControlCBR {
		return false
	}
	return e.opts.LookaheadFrames >= vp9MinLookaheadForARFs
}

func (e *VP9Encoder) pushVP9Lookahead(img *image.YCbCr, flags EncodeFlags) error {
	if !e.vp9LookaheadEnabled() {
		return ErrInvalidConfig
	}
	if int(e.lookaheadCount)+2 > len(e.lookahead) {
		return ErrFrameNotReady
	}
	entry := &e.lookahead[int(e.lookaheadWrite)]
	copyVP9LookaheadImage(&entry.img, img, e.opts.Width, e.opts.Height)
	entry.flags = flags
	entry.isAltRefSource = false
	e.lookaheadWrite++
	if int(e.lookaheadWrite) >= len(e.lookahead) {
		e.lookaheadWrite = 0
	}
	e.lookaheadCount++
	return nil
}

func (e *VP9Encoder) popVP9Lookahead(drain bool) (*vp9LookaheadEntry, bool) {
	if !e.vp9LookaheadEnabled() {
		return nil, false
	}
	if e.lookaheadCount == 0 ||
		(!drain && int(e.lookaheadCount) != len(e.lookahead)-1) {
		return nil, false
	}
	entry := &e.lookahead[int(e.lookaheadRead)]
	e.lookaheadRead++
	if int(e.lookaheadRead) >= len(e.lookahead) {
		e.lookaheadRead = 0
	}
	e.lookaheadCount--
	return entry, true
}

// peekVP9LookaheadAt returns a pointer to the lookahead entry at logical
// offset i (0 == oldest queued frame, lookaheadCount-1 == newest) without
// removing it. The caller must not retain the pointer past the next push or
// pop operation. Returns false when i is out of range or lookahead is
// disabled.
func (e *VP9Encoder) peekVP9LookaheadAt(i int) (*vp9LookaheadEntry, bool) {
	if !e.vp9LookaheadEnabled() {
		return nil, false
	}
	if i < 0 || i >= int(e.lookaheadCount) {
		return nil, false
	}
	idx := (int(e.lookaheadRead) + i) % len(e.lookahead)
	return &e.lookahead[idx], true
}

func (e *VP9Encoder) newestVP9LookaheadEntry() (*vp9LookaheadEntry, bool) {
	if !e.vp9LookaheadEnabled() || e.lookaheadCount == 0 {
		return nil, false
	}
	idx := int(e.lookaheadWrite)
	if idx == 0 {
		idx = len(e.lookahead)
	}
	return &e.lookahead[idx-1], true
}

func copyVP9LookaheadImage(dst *image.YCbCr, src *image.YCbCr, width int, height int) {
	for y := range height {
		copy(dst.Y[y*dst.YStride:][:width], src.Y[y*src.YStride:][:width])
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		copy(dst.Cb[y*dst.CStride:][:uvWidth], src.Cb[y*src.CStride:][:uvWidth])
		copy(dst.Cr[y*dst.CStride:][:uvWidth], src.Cr[y*src.CStride:][:uvWidth])
	}
}
