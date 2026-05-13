package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const maxLookaheadFrames = 25

type lookaheadEntry struct {
	frame    vp8common.FrameBuffer
	pts      uint64
	duration uint64
	flags    EncodeFlags
}

func (e *VP8Encoder) initLookahead(width int, height int, depth int) error {
	if depth <= 0 {
		return nil
	}
	e.lookahead = make([]lookaheadEntry, depth+1)
	return e.resizeLookaheadFrames(width, height)
}

func (e *VP8Encoder) resizeLookaheadFrames(width int, height int) error {
	for i := range e.lookahead {
		if err := e.lookahead[i].frame.Resize(width, height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	if e.autoAltRefStashFrame.Img.YStride != 0 {
		if err := e.autoAltRefStashFrame.Resize(width, height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	return nil
}

func (e *VP8Encoder) lookaheadEnabled() bool {
	return e.opts.LookaheadFrames > 0 && len(e.lookahead) > 0
}

func (e *VP8Encoder) lookaheadSize() int {
	if !e.lookaheadEnabled() {
		return 0
	}
	return e.lookaheadCount
}

func (e *VP8Encoder) encodeLookaheadInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error) {
	if err := validateEncodeFlags(flags); err != nil {
		return EncodeResult{}, err
	}
	pushFlags := flags
	consumeForceKey := e.forceKeyFrame
	if consumeForceKey {
		pushFlags |= EncodeForceKeyFrame
	}
	if err := e.pushLookahead(sourceImageFromImage(src), pts, duration, pushFlags); err != nil {
		return EncodeResult{}, err
	}
	if consumeForceKey {
		e.forceKeyFrame = false
	}
	if e.lookaheadSize() < e.opts.LookaheadFrames {
		return EncodeResult{}, ErrFrameNotReady
	}
	entry, ok := e.popLookahead(false)
	if !ok {
		return EncodeResult{}, ErrFrameNotReady
	}
	meta := encodeSourceMetadata{lookaheadDepth: e.lookaheadSize()}
	result, err := e.encodeSourceInto(dst, sourceImageFromVP8(&entry.frame.Img), entry.pts, entry.duration, entry.flags, meta)
	e.clearPoppedLookahead(entry)
	return result, err
}

func (e *VP8Encoder) consumeForceKeyFrameForInput(flags EncodeFlags) EncodeFlags {
	if e.forceKeyFrame {
		e.forceKeyFrame = false
		flags |= EncodeForceKeyFrame
	}
	return flags
}

// pushLookahead enqueues a source frame into the lookahead queue. It mirrors
// libvpx vp8_lookahead_push (vp8/encoder/lookahead.c): the queue is capped at
// max_sz - 1 entries (where max_sz = LookaheadFrames + 1, the extra slot keeps
// the most recently popped buffer addressable for backward peek), and overflow
// pushes are rejected with ErrFrameNotReady. When the queue is configured for a
// single buffer (max_sz == 1), the active map is enabled, and the frame carries
// no key/golden/alt-ref flags, only the active macroblock columns are copied
// into the destination buffer; otherwise the full frame is copied. Active-map
// driven partial copies follow libvpx's row-major mb_cols layout, with each
// active run copied as a 16-pixel-tall rectangle.
func (e *VP8Encoder) pushLookahead(src vp8enc.SourceImage, pts uint64, duration uint64, flags EncodeFlags) error {
	if !e.lookaheadEnabled() {
		return ErrInvalidConfig
	}
	// libvpx: if (ctx->sz + 2 > ctx->max_sz) return 1;
	// max_sz = depth + 1, so the largest accepted sz before push is depth - 1
	// (post-push max sz is depth = LookaheadFrames).
	if e.lookaheadCount+2 > len(e.lookahead) {
		return ErrFrameNotReady
	}
	entry := &e.lookahead[e.lookaheadWrite]
	useActiveMapPartialCopy := len(e.lookahead) == 1 && e.activeMapEnabled && flags == 0 && len(e.activeMap) > 0
	if useActiveMapPartialCopy {
		copySourceToFrameBufferActive(&entry.frame, src, e.activeMap, encoderMacroblockRows(src.Height), encoderMacroblockCols(src.Width))
	} else {
		copySourceToFrameBuffer(&entry.frame, src)
	}
	entry.pts = pts
	entry.duration = duration
	if entry.duration == 0 {
		entry.duration = 1
	}
	entry.flags = flags
	e.lookaheadWrite++
	if e.lookaheadWrite >= len(e.lookahead) {
		e.lookaheadWrite = 0
	}
	e.lookaheadCount++
	return nil
}

// popLookahead dequeues the next source frame, matching libvpx
// vp8_lookahead_pop. When drain is false a frame is only returned once the
// queue is full (sz == max_sz - 1), implementing the configured lag. When
// drain is true the queue is flushed entry-by-entry until empty (EOS path).
func (e *VP8Encoder) popLookahead(drain bool) (*lookaheadEntry, bool) {
	if !e.lookaheadEnabled() {
		return nil, false
	}
	// libvpx: if (ctx->sz && (drain || ctx->sz == ctx->max_sz - 1))
	if e.lookaheadCount == 0 || (!drain && e.lookaheadCount != len(e.lookahead)-1) {
		return nil, false
	}
	entry := &e.lookahead[e.lookaheadRead]
	e.lookaheadRead++
	if e.lookaheadRead >= len(e.lookahead) {
		e.lookaheadRead = 0
	}
	e.lookaheadCount--
	return entry, true
}

// peekLookahead returns the entry at the given offset from the head of the
// queue. When forward is true the offset is measured forward from the next
// frame to be popped (index 0 == next, index 1 == one frame in the future,
// etc.) and out-of-range indices return nil, matching libvpx
// vp8_lookahead_peek's PEEK_FORWARD branch. When forward is false only
// index == 1 is supported (libvpx asserts the same constraint) and the
// returned entry is the slot one position before the read index, i.e., the
// most recently popped buffer; this is the buffer first-pass uses as the
// previous-source reference. nil is returned for unsupported backward
// indices.
func (e *VP8Encoder) peekLookahead(index int, forward bool) *lookaheadEntry {
	if !e.lookaheadEnabled() || len(e.lookahead) == 0 {
		return nil
	}
	if forward {
		if uint(index) >= uint(e.lookaheadCount) {
			return nil
		}
		// libvpx: assert(index < ctx->max_sz - 1).
		if index >= len(e.lookahead)-1 {
			return nil
		}
		pos := e.lookaheadRead + index
		if pos >= len(e.lookahead) {
			pos -= len(e.lookahead)
		}
		return &e.lookahead[pos]
	}
	// Backward peek: libvpx only supports index == 1 (asserts this). The
	// returned slot is read_idx - 1 with wraparound; libvpx leaves popped
	// buffers in place, so this exposes the previous source frame.
	if index != 1 {
		return nil
	}
	pos := e.lookaheadRead - 1
	if pos < 0 {
		pos += len(e.lookahead)
	}
	return &e.lookahead[pos]
}

// lookaheadDepth mirrors libvpx vp8_lookahead_depth (number of frames
// currently buffered).
func (e *VP8Encoder) lookaheadDepth() int {
	if !e.lookaheadEnabled() {
		return 0
	}
	return e.lookaheadCount
}

func (e *VP8Encoder) clearPoppedLookahead(entry *lookaheadEntry) {
	if entry == nil {
		return
	}
	entry.pts, entry.duration, entry.flags = 0, 0, 0
}

// lookaheadFutureEntry mirrors libvpx vp8_lookahead_peek with PEEK_FORWARD,
// returning the entry index frames into the future from the next pop. nil is
// returned for out-of-range indices. ARNR uses this to walk the alt-ref
// candidate window starting at the current frame's successor.
func (e *VP8Encoder) lookaheadFutureEntry(index int) *lookaheadEntry {
	return e.peekLookahead(index, true)
}
