package govpx

import "image"

const vp9MaxLookaheadFrames = 25

type vp9LookaheadEntry struct {
	img   image.YCbCr
	flags EncodeFlags
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
	e.lookaheadRead = 0
	e.lookaheadWrite = 0
	e.lookaheadCount = 0
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
	if err := validateVP9EncodeFlags(flags); err != nil {
		return VP9EncodeResult{}, err
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return VP9EncodeResult{}, err
	}
	if len(dst) < vp9MinEncodeIntoBuffer {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	if e.autoAltRefPendingSet {
		return e.encodeVP9AutoAltRefPendingAndQueueInto(img, dst, flags)
	}
	if err := e.pushVP9Lookahead(img, flags); err != nil {
		return VP9EncodeResult{}, err
	}
	if e.vp9LookaheadSize() < e.opts.LookaheadFrames {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	if result, ok, err := e.maybeEncodeVP9AutoAltRefInto(dst); ok || err != nil {
		return result, err
	}
	entry, ok := e.popVP9Lookahead(false)
	if !ok {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&entry.img, dst, entry.flags,
		false, temporalFrame{LayerCount: 1})
	entry.flags = 0
	return result, err
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
	if e.autoAltRefPendingSet {
		return e.encodeVP9AutoAltRefPendingInto(dst)
	}
	if !e.vp9LookaheadEnabled() || e.vp9LookaheadSize() == 0 {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	entry, ok := e.popVP9Lookahead(true)
	if !ok {
		return VP9EncodeResult{}, ErrFrameNotReady
	}
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&entry.img, dst, entry.flags,
		false, temporalFrame{LayerCount: 1})
	entry.flags = 0
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
		e.autoAltRefPendingSet = true
		entry.flags = 0
	}
	return result, nil
}

func (e *VP9Encoder) encodeVP9AutoAltRefPendingInto(dst []byte) (VP9EncodeResult, error) {
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&e.autoAltRefPending.img, dst,
		e.autoAltRefPending.flags, false, temporalFrame{LayerCount: 1})
	if err != nil {
		return result, err
	}
	e.autoAltRefPending.flags = 0
	e.autoAltRefPendingSet = false
	return result, nil
}

func (e *VP9Encoder) maybeEncodeVP9AutoAltRefInto(dst []byte) (VP9EncodeResult, bool, error) {
	if !e.opts.AutoAltRef || e.autoAltRefEmitted || e.autoAltRefPendingSet ||
		e.frameIndex == 0 || e.vp9LookaheadSize() < e.opts.LookaheadFrames {
		return VP9EncodeResult{}, false, nil
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		return VP9EncodeResult{}, false, nil
	}
	result, err := e.encodeVP9FrameIntoWithFlagsResult(&future.img, dst,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden,
		false, temporalFrame{LayerCount: 1})
	if err != nil {
		return result, true, err
	}
	visible, ok := e.popVP9Lookahead(false)
	if !ok {
		return result, true, nil
	}
	copyVP9LookaheadImage(&e.autoAltRefPending.img, &visible.img, e.opts.Width, e.opts.Height)
	e.autoAltRefPending.flags = visible.flags
	e.autoAltRefPendingSet = true
	e.autoAltRefEmitted = true
	visible.flags = 0
	return result, true, nil
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
	for y := 0; y < height; y++ {
		copy(dst.Y[y*dst.YStride:][:width], src.Y[y*src.YStride:][:width])
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		copy(dst.Cb[y*dst.CStride:][:uvWidth], src.Cb[y*src.CStride:][:uvWidth])
		copy(dst.Cr[y*dst.CStride:][:uvWidth], src.Cr[y*src.CStride:][:uvWidth])
	}
}
