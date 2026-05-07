package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const (
	maxLookaheadFrames = 25
	maxARNRFrames      = 15
)

type lookaheadEntry struct {
	frame    vp8common.FrameBuffer
	pts      uint64
	duration uint64
	flags    EncodeFlags
}

type encodeSourceMetadata struct {
	lookaheadDepth int
	arnrFiltered   bool
	denoised       bool
}

func (e *VP8Encoder) initPreprocessFrames(width int, height int) error {
	if err := e.preprocess.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.arnrScratch.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.arnrLastSource.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassLastRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassGoldenRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP8Encoder) initLookahead(width int, height int, depth int) error {
	if depth <= 0 {
		return nil
	}
	e.lookahead = make([]lookaheadEntry, depth+1)
	for i := range e.lookahead {
		if err := e.lookahead[i].frame.Resize(width, height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	return nil
}

func (e *VP8Encoder) lookaheadEnabled() bool {
	return e != nil && e.opts.LookaheadFrames > 0 && len(e.lookahead) > 0
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
	if err := e.pushLookahead(sourceImageFromImage(src), pts, duration, flags); err != nil {
		return EncodeResult{}, err
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
		if index < 0 || index >= e.lookaheadCount {
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

// copySourceToFrameBufferActive performs the active-map-aware partial frame
// copy from libvpx vp8_lookahead_push. activeMap is a row-major mb_rows*mb_cols
// array; non-zero cells mark active macroblocks. For each active run within a
// row, a 16-pixel-tall band of luma plus the colocated chroma is copied from
// src into dst; inactive macroblocks retain whatever the destination buffer
// already held. Border extension follows the full copy path.
func copySourceToFrameBufferActive(dst *vp8common.FrameBuffer, src vp8enc.SourceImage, activeMap []uint8, mbRows int, mbCols int) {
	if len(activeMap) < mbRows*mbCols {
		copySourceToFrameBuffer(dst, src)
		return
	}
	for row := 0; row < mbRows; row++ {
		col := 0
		for col < mbCols {
			// Skip leading inactive cells.
			for col < mbCols && activeMap[row*mbCols+col] == 0 {
				col++
			}
			if col >= mbCols {
				break
			}
			runStart := col
			for col < mbCols && activeMap[row*mbCols+col] != 0 {
				col++
			}
			runEnd := col
			copyActiveLumaRect(dst.Img.Y, dst.Img.YStride, src.Y, src.YStride, src.Width, src.Height, row<<4, runStart<<4, 16, (runEnd-runStart)<<4)
			copyActiveChromaRect(dst.Img.U, dst.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1, row<<3, runStart<<3, 8, (runEnd-runStart)<<3)
			copyActiveChromaRect(dst.Img.V, dst.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1, row<<3, runStart<<3, 8, (runEnd-runStart)<<3)
		}
	}
	padFrameVisibleToCoded(&dst.Img)
	dst.ExtendBorders()
}

func copyActiveLumaRect(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, y0 int, x0 int, h int, w int) {
	copyActivePlaneRect(dst, dstStride, src, srcStride, width, height, y0, x0, h, w)
}

func copyActiveChromaRect(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, y0 int, x0 int, h int, w int) {
	copyActivePlaneRect(dst, dstStride, src, srcStride, width, height, y0, x0, h, w)
}

func copyActivePlaneRect(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, y0 int, x0 int, h int, w int) {
	yEnd := y0 + h
	if yEnd > height {
		yEnd = height
	}
	xEnd := x0 + w
	if xEnd > width {
		xEnd = width
	}
	for y := y0; y < yEnd; y++ {
		copy(dst[y*dstStride+x0:y*dstStride+xEnd], src[y*srcStride+x0:y*srcStride+xEnd])
	}
}

func (e *VP8Encoder) preprocessSource(source vp8enc.SourceImage, flags EncodeFlags, meta encodeSourceMetadata) (vp8enc.SourceImage, encodeSourceMetadata) {
	src := source
	if e.opts.ARNRMaxFrames > 1 && e.lookaheadEnabled() {
		if e.applyARNRFilter(src, flags) {
			src = sourceImageFromVP8(&e.arnrScratch.Img)
			meta.arnrFiltered = true
		}
	}
	copySourceToFrameBuffer(&e.arnrLastSource, source)
	e.arnrLastReady = true
	if e.opts.NoiseSensitivity > 0 {
		// Allocate the libvpx-style running average buffers and per-MB
		// state map on first inter frame; the actual filter runs per-MB
		// after mode decision in buildReconstructingInterFrameCoefficients.
		_ = e.denoiser.ensureAllocated(e.opts.Width, e.opts.Height)
		mode := denoiserModeForSensitivity(e.opts.NoiseSensitivity)
		e.denoiser.mode = mode
		_, e.denoiser.params = denoiserSetParameters(mode)
		meta.denoised = true
	}
	return src, meta
}

func (e *VP8Encoder) applyARNRFilter(center vp8enc.SourceImage, flags EncodeFlags) bool {
	maxFrames := e.opts.ARNRMaxFrames
	if maxFrames > maxARNRFrames {
		maxFrames = maxARNRFrames
	}
	if maxFrames <= 1 {
		return false
	}
	arnrType := e.opts.ARNRType
	backward := 0
	forward := 0
	switch arnrType {
	case 1:
		if e.arnrLastReady {
			backward = 1
		}
	case 2:
		forward = min(e.lookaheadSize(), maxFrames-1)
	case 3:
		if e.arnrLastReady {
			backward = 1
		}
		forward = min(e.lookaheadSize(), maxFrames-1-backward)
	default:
		return false
	}
	if backward+forward == 0 {
		return false
	}
	strength := e.opts.ARNRStrength
	if strength <= 0 {
		strength = 3
	}
	copySourceToFrameBuffer(&e.arnrScratch, center)
	temporalFilterPlane(e.arnrScratch.Img.Y, e.arnrScratch.Img.YStride, center.Y, center.YStride, center.Width, center.Height, strength, e.arnrLastSource.Img.Y, e.arnrLastSource.Img.YStride, backward > 0, e.lookaheadFutureEntry, forward, 0)
	if flags&EncodeInvisibleFrame != 0 || e.opts.ARNRStrength > 4 {
		temporalFilterPlane(e.arnrScratch.Img.U, e.arnrScratch.Img.UStride, center.U, center.UStride, (center.Width+1)>>1, (center.Height+1)>>1, strength, e.arnrLastSource.Img.U, e.arnrLastSource.Img.UStride, backward > 0, e.lookaheadFutureEntry, forward, 1)
		temporalFilterPlane(e.arnrScratch.Img.V, e.arnrScratch.Img.VStride, center.V, center.VStride, (center.Width+1)>>1, (center.Height+1)>>1, strength, e.arnrLastSource.Img.V, e.arnrLastSource.Img.VStride, backward > 0, e.lookaheadFutureEntry, forward, 2)
	}
	e.arnrScratch.ExtendBorders()
	return true
}

// lookaheadFutureEntry mirrors libvpx vp8_lookahead_peek with PEEK_FORWARD,
// returning the entry index frames into the future from the next pop. nil is
// returned for out-of-range indices. ARNR uses this to walk the alt-ref
// candidate window starting at the current frame's successor.
func (e *VP8Encoder) lookaheadFutureEntry(index int) *lookaheadEntry {
	return e.peekLookahead(index, true)
}

func temporalFilterPlane(dst []byte, dstStride int, center []byte, centerStride int, width int, height int, strength int, back []byte, backStride int, useBack bool, future func(int) *lookaheadEntry, forward int, planeID int) {
	threshold := 8 + strength*8
	if planeID != 0 {
		threshold += 8
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := int(center[y*centerStride+x])
			sum := c * threshold
			weightSum := threshold
			if useBack {
				v := int(back[y*backStride+x])
				w := temporalFilterWeight(c, v, threshold)
				sum += v * w
				weightSum += w
			}
			for i := 0; i < forward; i++ {
				entry := future(i)
				if entry == nil {
					continue
				}
				var plane []byte
				stride := 0
				switch planeID {
				case 1:
					plane = entry.frame.Img.U
					stride = entry.frame.Img.UStride
				case 2:
					plane = entry.frame.Img.V
					stride = entry.frame.Img.VStride
				default:
					plane = entry.frame.Img.Y
					stride = entry.frame.Img.YStride
				}
				v := int(plane[y*stride+x])
				w := temporalFilterWeight(c, v, threshold)
				sum += v * w
				weightSum += w
			}
			dst[y*dstStride+x] = byte((sum + weightSum/2) / weightSum)
		}
	}
}

func temporalFilterWeight(center int, sample int, threshold int) int {
	diff := center - sample
	if diff < 0 {
		diff = -diff
	}
	if diff >= threshold {
		return 0
	}
	return threshold - diff
}

func copySourceToFrameBuffer(dst *vp8common.FrameBuffer, src vp8enc.SourceImage) {
	copyPlane(dst.Img.Y, dst.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	copyPlane(dst.Img.U, dst.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	copyPlane(dst.Img.V, dst.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	padFrameVisibleToCoded(&dst.Img)
	dst.ExtendBorders()
}

func padFrameVisibleToCoded(img *vp8common.Image) {
	padPlaneVisibleToCoded(img.Y, img.YStride, img.Width, img.Height, img.CodedWidth, img.CodedHeight)
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	codedUVWidth := (img.CodedWidth + 1) >> 1
	codedUVHeight := (img.CodedHeight + 1) >> 1
	padPlaneVisibleToCoded(img.U, img.UStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight)
	padPlaneVisibleToCoded(img.V, img.VStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight)
}

func padPlaneVisibleToCoded(plane []byte, stride int, width int, height int, codedWidth int, codedHeight int) {
	if width <= 0 || height <= 0 {
		return
	}
	for y := 0; y < height; y++ {
		row := plane[y*stride:]
		last := row[width-1]
		for x := width; x < codedWidth; x++ {
			row[x] = last
		}
	}
	lastRow := plane[(height-1)*stride:]
	for y := height; y < codedHeight; y++ {
		copy(plane[y*stride:y*stride+codedWidth], lastRow[:codedWidth])
	}
}

func sourceImageFromVP8(src *vp8common.Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
}
