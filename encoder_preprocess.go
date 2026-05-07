package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
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
	if err := e.firstPassLastSource.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassNewRef.Resize(width, height, 32, 32); err != nil {
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

// applyARNRFilter implements libvpx's VP8 motion-compensated temporal filter
// (see vp8/encoder/temporal_filter.c). For every 16x16 luma macroblock (and
// the colocated 8x8 chroma blocks) in the center frame it searches the
// adjacent backward/forward source frames for a matching block and weights
// each predictor pixel by libvpx's
//
//	modifier = clamp((3*(src-pred)^2 + rounding) >> strength, 0, 16)
//	weight   = (16 - modifier) * filter_weight
//
// The center frame contributes with filter_weight=2 (as in libvpx); adjacent
// frames pick filter_weight in {2,1,0} based on the 16x16 SAD threshold pair
// THRESH_LOW=10000 / THRESH_HIGH=20000.
//
// Simplification (vs. libvpx vp8_temporal_filter_iterate_c):
//   - Motion search is a small full-pixel local exhaustive search around
//     (0,0) instead of libvpx's hex/diamond search seeded from the prior MV
//     and refined to subpixel; subpixel predictors are not used. The chosen
//     MV is reused unchanged for the colocated chroma block, matching
//     libvpx's vp8_temporal_filter_predictors_mb_c integer-pel branch.
//   - The center frame is read from the input SourceImage's visible region;
//     accordingly the search position is clamped so the predictor 16x16
//     stays inside the visible area of every reference frame.
//
// Everything else (per-pixel weighting, accumulator/count normalization with
// (acc + count/2)/count, separate luma/chroma blocks, and the 384-element
// per-MB scratch layout) follows libvpx exactly.
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
	// The center frame is the alt-ref source. Copy it into the scratch
	// buffer first so we have a stable read source and an output in the
	// same place (libvpx writes filtered pixels into cpi->alt_ref_buffer).
	copySourceToFrameBuffer(&e.arnrScratch, center)
	// Whether the chroma planes participate matches the legacy gating
	// (invisible alt-ref or strong filter strength). Luma always runs.
	doChroma := flags&EncodeInvisibleFrame != 0 || e.opts.ARNRStrength > 4
	e.iterateTemporalFilter(center, strength, backward > 0, forward, doChroma)
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

// arnrFrameView is the minimal frame description vp8_temporal_filter_iterate_c
// needs: visible-area pointers per plane plus their strides and dimensions.
// Adjacent frames are exposed as views over their owning vp8common.Image.
type arnrFrameView struct {
	width, height int
	y             []byte
	u             []byte
	v             []byte
	yStride       int
	uStride       int
	vStride       int
}

func arnrViewFromImage(img *vp8common.Image) arnrFrameView {
	return arnrFrameView{
		width:   img.Width,
		height:  img.Height,
		y:       img.Y,
		u:       img.U,
		v:       img.V,
		yStride: img.YStride,
		uStride: img.UStride,
		vStride: img.VStride,
	}
}

func arnrViewFromSource(src vp8enc.SourceImage) arnrFrameView {
	return arnrFrameView{
		width:   src.Width,
		height:  src.Height,
		y:       src.Y,
		u:       src.U,
		v:       src.V,
		yStride: src.YStride,
		uStride: src.UStride,
		vStride: src.VStride,
	}
}

// iterateTemporalFilter mirrors vp8_temporal_filter_iterate_c. It walks every
// 16x16 luma macroblock (with colocated 8x8 chroma blocks) in the alt-ref
// frame, picks per-frame filter weights by SAD-based error, and accumulates
// libvpx's per-pixel weighted average across the included frames.
func (e *VP8Encoder) iterateTemporalFilter(center vp8enc.SourceImage, strength int, useBack bool, forward int, doChroma bool) {
	mbCols := (center.Width + 15) >> 4
	mbRows := (center.Height + 15) >> 4
	if mbCols == 0 || mbRows == 0 {
		return
	}

	// Collect the references that participate. The center is always
	// included with filter_weight=2 (libvpx's alt_ref_index path). Frames
	// that fail to qualify are skipped per-MB inside processARNRMacroblock.
	refs := make([]arnrFrameView, 0, 1+1+forward)
	centerIdx := -1
	if useBack {
		refs = append(refs, arnrViewFromImage(&e.arnrLastSource.Img))
	}
	centerIdx = len(refs)
	refs = append(refs, arnrViewFromSource(center))
	for i := 0; i < forward; i++ {
		entry := e.lookaheadFutureEntry(i)
		if entry == nil {
			continue
		}
		refs = append(refs, arnrViewFromImage(&entry.frame.Img))
	}

	dst := arnrFrameView{
		width:   e.arnrScratch.Img.Width,
		height:  e.arnrScratch.Img.Height,
		y:       e.arnrScratch.Img.Y,
		u:       e.arnrScratch.Img.U,
		v:       e.arnrScratch.Img.V,
		yStride: e.arnrScratch.Img.YStride,
		uStride: e.arnrScratch.Img.UStride,
		vStride: e.arnrScratch.Img.VStride,
	}

	// Reuse a single scratch across MBs (libvpx allocates 384 entries on
	// the stack). 16x16 luma + 8x8 U + 8x8 V = 256 + 64 + 64 = 384.
	var accumulator [384]uint32
	var count [384]uint32

	for mbRow := 0; mbRow < mbRows; mbRow++ {
		mbY := mbRow << 4
		for mbCol := 0; mbCol < mbCols; mbCol++ {
			mbX := mbCol << 4
			processARNRMacroblock(&dst, refs, centerIdx, mbX, mbY, strength, doChroma, accumulator[:], count[:])
		}
	}
}

// processARNRMacroblock corresponds to the inner mb_col loop body in
// vp8_temporal_filter_iterate_c: zero accumulators, search/weight every
// reference, then normalize accumulator/count back into the output frame.
func processARNRMacroblock(dst *arnrFrameView, refs []arnrFrameView, centerIdx int, mbX, mbY, strength int, doChroma bool, accumulator []uint32, count []uint32) {
	for i := range accumulator {
		accumulator[i] = 0
		count[i] = 0
	}

	// Pull the source 16x16 luma block (and 8x8 chroma blocks if they
	// will be filtered) into contiguous scratch arrays so SAD and the
	// per-pixel apply step both see a clean 16-byte stride. libvpx
	// avoids this copy because cpi->frames[alt_ref_index] is stored in
	// a contiguous YV12 buffer; for govpx this keeps the math identical
	// while accommodating arbitrary input strides.
	var srcY [256]byte
	gatherBlock(srcY[:], 16, dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16)
	mbUVX := mbX >> 1
	mbUVY := mbY >> 1
	uvW := (dst.width + 1) >> 1
	uvH := (dst.height + 1) >> 1
	var srcU, srcV [64]byte
	if doChroma {
		gatherBlock(srcU[:], 8, dst.u, dst.uStride, mbUVX, mbUVY, uvW, uvH, 8)
		gatherBlock(srcV[:], 8, dst.v, dst.vStride, mbUVX, mbUVY, uvW, uvH, 8)
	}

	for fi, ref := range refs {
		// Choose per-frame filter weight. The center frame always uses
		// libvpx's filter_weight=2; adjacent frames are graded by the
		// 16x16 luma SAD against fixed thresholds, matching
		// vp8_temporal_filter_iterate_c's THRESH_LOW/THRESH_HIGH.
		var filterWeight int
		var mvX, mvY int
		if fi == centerIdx {
			filterWeight = 2
		} else {
			err, sx, sy := arnrFindMatchingMB(srcY[:], 16, ref, mbX, mbY)
			mvX, mvY = sx, sy
			switch {
			case err < arnrThreshLow:
				filterWeight = 2
			case err < arnrThreshHigh:
				filterWeight = 1
			default:
				filterWeight = 0
			}
		}
		if filterWeight == 0 {
			continue
		}

		var predY [256]byte
		gatherBlock(predY[:], 16, ref.y, ref.yStride, mbX+mvX, mbY+mvY, ref.width, ref.height, 16)
		applyTemporalFilter(srcY[:], 16, predY[:], 16, strength, filterWeight, accumulator[:256], count[:256])

		if doChroma {
			// Chroma uses the half-resolution colocated MV (libvpx
			// halves mv_row/mv_col before feeding chroma predictors).
			mvUVX := mvX >> 1
			mvUVY := mvY >> 1
			refUVW := (ref.width + 1) >> 1
			refUVH := (ref.height + 1) >> 1
			var predU, predV [64]byte
			gatherBlock(predU[:], 8, ref.u, ref.uStride, mbUVX+mvUVX, mbUVY+mvUVY, refUVW, refUVH, 8)
			gatherBlock(predV[:], 8, ref.v, ref.vStride, mbUVX+mvUVX, mbUVY+mvUVY, refUVW, refUVH, 8)
			applyTemporalFilter(srcU[:], 8, predU[:], 8, strength, filterWeight, accumulator[256:320], count[256:320])
			applyTemporalFilter(srcV[:], 8, predV[:], 8, strength, filterWeight, accumulator[320:384], count[320:384])
		}
	}

	// Normalize accumulator/count into the output. libvpx uses a
	// per-count fixed-divide LUT; the math here is the equivalent
	// (accumulator + count/2 + count/2) / count which biases the result
	// toward libvpx's rounded division. The center frame always
	// contributes count >= 16, so divisions are well defined.
	writeARNRBlock(dst.y, dst.yStride, mbX, mbY, dst.width, dst.height, 16, accumulator[:256], count[:256])
	if doChroma {
		writeARNRBlock(dst.u, dst.uStride, mbUVX, mbUVY, uvW, uvH, 8, accumulator[256:320], count[256:320])
		writeARNRBlock(dst.v, dst.vStride, mbUVX, mbUVY, uvW, uvH, 8, accumulator[320:384], count[320:384])
	}
}

// gatherBlock copies a (size x size) block at (srcX,srcY) from a planar
// surface (with arbitrary stride and visible width/height) into a tightly
// packed scratch buffer with stride dstStride. Reads outside the visible
// area are clamped to the nearest in-bounds pixel - the libvpx encoder
// extends source borders so all intra-MB reads are valid; in govpx the
// SourceImage has only the visible area and clamping replicates that
// effect when the search picks an MB that straddles the edge.
func gatherBlock(dst []byte, dstStride int, src []byte, srcStride, srcX, srcY, srcW, srcH, size int) {
	for j := 0; j < size; j++ {
		yy := srcY + j
		if yy < 0 {
			yy = 0
		} else if yy >= srcH {
			yy = srcH - 1
		}
		row := src[yy*srcStride:]
		for i := 0; i < size; i++ {
			xx := srcX + i
			if xx < 0 {
				xx = 0
			} else if xx >= srcW {
				xx = srcW - 1
			}
			dst[j*dstStride+i] = row[xx]
		}
	}
}

// writeARNRBlock writes the (size x size) accumulated/count pair back into
// the destination plane, clipping to the visible area.
func writeARNRBlock(dst []byte, dstStride, dstX, dstY, dstW, dstH, size int, accumulator []uint32, count []uint32) {
	for j := 0; j < size; j++ {
		yy := dstY + j
		if yy < 0 || yy >= dstH {
			continue
		}
		row := dst[yy*dstStride:]
		for i := 0; i < size; i++ {
			xx := dstX + i
			if xx < 0 || xx >= dstW {
				continue
			}
			k := j*size + i
			c := count[k]
			if c == 0 {
				continue
			}
			pval := (accumulator[k] + c/2) / c
			if pval > 255 {
				pval = 255
			}
			row[xx] = byte(pval)
		}
	}
}

// arnr motion search constants. The thresholds match libvpx's
// THRESH_LOW/THRESH_HIGH (10000/20000 for a 16x16 SAD). The search radius
// is the full-pixel motion-search window we sweep around (0,0); libvpx
// performs a hex search with subpel refinement, govpx does a small
// exhaustive integer-pel search starting from the colocated block.
const (
	arnrThreshLow    = 10000
	arnrThreshHigh   = 20000
	arnrSearchRadius = 7
)

// arnrFindMatchingMB performs the simplified motion search vp8 ARNR uses to
// locate the matching predictor block in an adjacent frame. It returns the
// best 16x16 SAD plus the integer-pixel MV (relative to the colocated
// position). Search positions are clamped so the candidate predictor stays
// inside the reference frame's visible area.
func arnrFindMatchingMB(src []byte, srcStride int, ref arnrFrameView, mbX, mbY int) (int, int, int) {
	bestSAD := -1
	bestX, bestY := 0, 0
	// Compute the legal MV range so the 16x16 predictor stays inside
	// the visible region.
	minDX := -mbX
	if minDX < -arnrSearchRadius {
		minDX = -arnrSearchRadius
	}
	maxDX := ref.width - 16 - mbX
	if maxDX > arnrSearchRadius {
		maxDX = arnrSearchRadius
	}
	minDY := -mbY
	if minDY < -arnrSearchRadius {
		minDY = -arnrSearchRadius
	}
	maxDY := ref.height - 16 - mbY
	if maxDY > arnrSearchRadius {
		maxDY = arnrSearchRadius
	}
	if minDX > maxDX || minDY > maxDY {
		// Block straddles the right/bottom edge so far that even the
		// colocated predictor would step outside the visible area. Use
		// (0,0) and let gatherBlock's clamping handle the read.
		sad := dsp.SAD16x16(src, srcStride, ref.y[mbY*ref.yStride+mbX:], ref.yStride)
		return sad, 0, 0
	}
	// First evaluate the colocated position (libvpx seeds the search
	// with MV(0,0)); this is also the natural fallback if every other
	// position turns out to be worse.
	if 0 >= minDX && 0 <= maxDX && 0 >= minDY && 0 <= maxDY {
		bestSAD = dsp.SAD16x16(src, srcStride, ref.y[mbY*ref.yStride+mbX:], ref.yStride)
	}
	for dy := minDY; dy <= maxDY; dy++ {
		py := mbY + dy
		for dx := minDX; dx <= maxDX; dx++ {
			if dx == 0 && dy == 0 && bestSAD >= 0 {
				continue
			}
			px := mbX + dx
			refOff := py*ref.yStride + px
			var sad int
			if bestSAD >= 0 {
				sad = dsp.SAD16x16Limit(src, srcStride, ref.y[refOff:], ref.yStride, bestSAD)
				if sad >= bestSAD {
					continue
				}
			} else {
				sad = dsp.SAD16x16(src, srcStride, ref.y[refOff:], ref.yStride)
			}
			bestSAD = sad
			bestX = dx
			bestY = dy
		}
	}
	if bestSAD < 0 {
		return 0, 0, 0
	}
	return bestSAD, bestX, bestY
}

// applyTemporalFilter is a direct port of libvpx's
// vp8_temporal_filter_apply_c. The integer formula approximates
//
//	coeff = (3 * (src - pred)^2) / 2^strength
//	modifier = clamp(round(coeff), 0, 16)
//	weight   = (16 - modifier) * filter_weight
//
// and accumulates count/accumulator for downstream normalization.
func applyTemporalFilter(src []byte, srcStride int, pred []byte, predStride int, strength int, filterWeight int, accumulator []uint32, count []uint32) {
	rounding := 0
	if strength > 0 {
		rounding = 1 << (strength - 1)
	}
	blockSize := 16
	if len(accumulator) == 64 {
		blockSize = 8
	}
	k := 0
	for j := 0; j < blockSize; j++ {
		srcRow := src[j*srcStride:]
		predRow := pred[j*predStride:]
		for i := 0; i < blockSize; i++ {
			diff := int(srcRow[i]) - int(predRow[i])
			modifier := diff*diff*3 + rounding
			modifier >>= uint(strength)
			if modifier > 16 {
				modifier = 16
			}
			modifier = (16 - modifier) * filterWeight
			count[k] += uint32(modifier)
			accumulator[k] += uint32(modifier) * uint32(predRow[i])
			k++
		}
	}
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
