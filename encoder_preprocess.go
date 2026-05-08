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
// The full-pixel motion search uses libvpx's hex search (vp8_hex_search) with
// the prior frame's per-MB MV as the seed; the first reference frame in the
// window seeds at (0,0). Subpel refinement walks libvpx's 1/2-, 1/4- and
// 1/8-pel diamond around the integer-pel MV using 16x16 sixtap-filtered SAD
// and adopts the lowest-SAD position. The synthesized predictor uses the
// 6-tap sixtap filter on luma and chroma (chroma's MV is the halved subpel
// MV per libvpx's `mv_offset = (1<<3 - 1) & mvR/mvC` dispatch).
//
// Simplification (vs. libvpx vp8_temporal_filter_iterate_c):
//   - The center frame is read from the input SourceImage's visible region;
//     accordingly the search position is clamped so the predictor 16x16
//     stays inside the visible area of every reference frame, and out-of-
//     visible taps fall back to gatherBlock's edge-replication rather than
//     libvpx's mirrored 16-pixel source-border extension.
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

	// Per-(reference, MB) MV history. The hex search for reference frame
	// fi at macroblock (mbRow, mbCol) is seeded with the MV chosen at the
	// same (mbRow, mbCol) for reference frame fi-1; the first reference
	// in the window seeds at (0,0). This mirrors libvpx's
	// motion_filter_buffer behavior of carrying the prior frame's MV into
	// the next per-frame search.
	mvHistory := make([]arnrMV, len(refs)*mbRows*mbCols)

	for mbRow := 0; mbRow < mbRows; mbRow++ {
		mbY := mbRow << 4
		for mbCol := 0; mbCol < mbCols; mbCol++ {
			mbX := mbCol << 4
			processARNRMacroblock(&dst, refs, centerIdx, mbRow, mbCol, mbRows, mbCols, mbX, mbY, strength, doChroma, accumulator[:], count[:], mvHistory)
		}
	}
}

// arnrMV is the integer-pixel motion vector recorded per (reference, MB)
// during the temporal filter sweep. The hex search for the next reference
// uses the prior reference's MV at the same (mbRow, mbCol) as its seed.
type arnrMV struct {
	x int
	y int
}

// processARNRMacroblock corresponds to the inner mb_col loop body in
// vp8_temporal_filter_iterate_c: zero accumulators, search/weight every
// reference, then normalize accumulator/count back into the output frame.
// The mvHistory slice carries the integer-pel MV chosen by each prior
// reference at this (mbRow, mbCol), so the hex search for the next
// reference can seed at the prior MV.
func processARNRMacroblock(dst *arnrFrameView, refs []arnrFrameView, centerIdx int, mbRow int, mbCol int, mbRows int, mbCols int, mbX, mbY, strength int, doChroma bool, accumulator []uint32, count []uint32, mvHistory []arnrMV) {
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

	mbHistory := mbRow*mbCols + mbCol
	for fi, ref := range refs {
		// Choose per-frame filter weight. The center frame always uses
		// libvpx's filter_weight=2; adjacent frames are graded by the
		// 16x16 luma SAD against fixed thresholds, matching
		// vp8_temporal_filter_iterate_c's THRESH_LOW/THRESH_HIGH.
		var filterWeight int
		// Subpel MV components in 1/8-pel units. libvpx's
		// vp8_temporal_filter_predictors_mb_c expects mv_row/mv_col
		// scaled by 8 (so the integer-pel components are mvSubY>>3).
		var mvSubX, mvSubY int
		var fullX, fullY int
		if fi == centerIdx {
			filterWeight = 2
		} else {
			// Seed the hex search at the prior reference's MV for
			// this MB, falling back to (0,0) for the first
			// reference in the window. libvpx's
			// vp8_temporal_filter_find_matching_mb_c forwards
			// best_ref_mv1 = 0 to vp8_hex_search; we extend that
			// to chain successive references through the same MB
			// so a panning sequence's MV propagates instead of
			// being lost.
			seed := arnrMV{}
			if fi > 0 {
				seed = mvHistory[(fi-1)*mbRows*mbCols+mbHistory]
			}
			_, sx, sy := arnrFindMatchingMB(srcY[:], 16, ref, mbRow, mbCol, mbRows, mbCols, mbX, mbY, seed.x, seed.y)
			fullX, fullY = sx, sy
			// Subpixel refinement around the full-pel MV using the
			// 6-tap sixtap predictor and 16x16 SAD; mirrors
			// libvpx's find_fractional_mv_step diamond walk over
			// 1/2-, 1/4- and 1/8-pel offsets. The returned MV is
			// in 1/8-pel units; the final 16x16 SAD on the chosen
			// subpel position drives the THRESH_LOW/THRESH_HIGH
			// classification (matching vp8_hex_search returning
			// the subpel SAD via find_fractional_mv_step).
			subErr, sx8, sy8 := arnrSubpelRefine(srcY[:], 16, ref, mbRow, mbCol, mbRows, mbCols, mbX, mbY, fullX, fullY)
			mvSubX, mvSubY = sx8, sy8
			switch {
			case subErr < arnrThreshLow:
				filterWeight = 2
			case subErr < arnrThreshHigh:
				filterWeight = 1
			default:
				filterWeight = 0
			}
		}
		// Persist the integer-pel search outcome so the next reference's
		// search at this MB can seed from it. Center frames carry their
		// (0,0) implicit MV forward. Storing the full-pel MV (rather
		// than the subpel-refined MV) keeps the seed legal for the next
		// hex search, which itself works in integer-pel units before
		// subpel refinement runs.
		mvHistory[fi*mbRows*mbCols+mbHistory] = arnrMV{x: fullX, y: fullY}
		if filterWeight == 0 {
			continue
		}

		var predY [256]byte
		arnrPredictLuma16x16(predY[:], 16, ref, mbX, mbY, mvSubX, mvSubY)
		applyTemporalFilter(srcY[:], 16, predY[:], 16, strength, filterWeight, accumulator[:256], count[:256])

		if doChroma {
			// Chroma uses the half-resolution colocated subpel MV.
			// libvpx's vp8_temporal_filter_predictors_mb_c does
			// `mv_row >>= 1; mv_col >>= 1;` on the 1/8-pel MV
			// then dispatches subpixel_predict8x8 with
			// (mv_col & 7, mv_row & 7).
			mvSubUVX := mvSubX >> 1
			mvSubUVY := mvSubY >> 1
			var predU, predV [64]byte
			arnrPredictChroma8x8(predU[:], 8, ref.u, ref.uStride, (ref.width+1)>>1, (ref.height+1)>>1, mbUVX, mbUVY, mvSubUVX, mvSubUVY)
			arnrPredictChroma8x8(predV[:], 8, ref.v, ref.vStride, (ref.width+1)>>1, (ref.height+1)>>1, mbUVX, mbUVY, mvSubUVX, mvSubUVY)
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
// THRESH_LOW/THRESH_HIGH (10000/20000 for a 16x16 SAD). The hex/diamond
// step counts mirror vp8_hex_search's hex_range=127 and dia_range=8.
const (
	arnrThreshLow  = 10000
	arnrThreshHigh = 20000
	arnrHexRange   = 127
	arnrDiaRange   = 8
	// arnrBorderSlack matches libvpx's mv_row_min/mv_col_min derivation
	// in vp8_temporal_filter_iterate_c: -((mb*16) + (16 - 5)). The 11
	// pixel slack reflects libvpx's 16-pixel source-border extension
	// minus the 5-pixel 6-tap subpel filter overhang on the trailing
	// edge. govpx's gatherBlock clamps reads, so out-of-visible
	// candidates remain well-defined.
	arnrBorderSlack = 11
)

// arnrFindMatchingMB performs libvpx's hex search (vp8_hex_search with
// NULL mvsadcost, i.e. pure 16x16 SAD) to locate the matching predictor
// block in an adjacent frame. It returns the best 16x16 SAD plus the
// integer-pixel MV (relative to the colocated position). The search is
// seeded at (seedX, seedY); callers thread the prior reference's MV at
// the same MB through this argument so a panning sequence propagates the
// search start instead of restarting at (0,0) every frame.
func arnrFindMatchingMB(src []byte, srcStride int, ref arnrFrameView, mbRow int, mbCol int, mbRows int, mbCols int, mbX, mbY int, seedX, seedY int) (int, int, int) {
	// Compute the libvpx-shaped MV bounds in pixel units. These permit
	// the predictor to overhang the visible region by 5 pixels on each
	// side; govpx's gatherBlock clamps any out-of-range read so the math
	// stays well-defined while preserving libvpx's reachable search set.
	mvColMin := -(mbCol*16 + (16 - 5))
	mvColMax := (mbCols-1-mbCol)*16 + (16 - 5)
	mvRowMin := -(mbRow*16 + (16 - 5))
	mvRowMax := (mbRows-1-mbRow)*16 + (16 - 5)

	// Clamp the seed into the legal window (libvpx's vp8_clamp_mv).
	br := arnrClamp(seedY, mvRowMin, mvRowMax)
	bc := arnrClamp(seedX, mvColMin, mvColMax)

	hex := [6][2]int{
		{-1, -2}, {1, -2}, {2, 0}, {1, 2}, {-1, 2}, {-2, 0},
	}
	nextChkpts := [6][3][2]int{
		{{-2, 0}, {-1, -2}, {1, -2}},
		{{-1, -2}, {1, -2}, {2, 0}},
		{{1, -2}, {2, 0}, {1, 2}},
		{{2, 0}, {1, 2}, {-1, 2}},
		{{1, 2}, {-1, 2}, {-2, 0}},
		{{-1, 2}, {-2, 0}, {-1, -2}},
	}
	neighbors := [4][2]int{{0, -1}, {-1, 0}, {1, 0}, {0, 1}}

	bestSAD := arnrSADAt(src, srcStride, ref, mbX, mbY, bc, br)

	// 6-vertex hexagon scan around the seed (libvpx's first hex iter).
	bestSite := -1
	for i, step := range hex {
		row := br + step[0]
		col := bc + step[1]
		if !arnrInBounds(col, row, mvColMin, mvColMax, mvRowMin, mvRowMax) {
			continue
		}
		sad := arnrSADAt(src, srcStride, ref, mbX, mbY, col, row)
		if sad < bestSAD {
			bestSAD = sad
			bestSite = i
		}
	}

	// Iterative 3-checkpoint walk along the hex's last-best edge.
	if bestSite >= 0 {
		br += hex[bestSite][0]
		bc += hex[bestSite][1]
		k := bestSite
		for j := 1; j < arnrHexRange; j++ {
			bestSite = -1
			for i, step := range nextChkpts[k] {
				row := br + step[0]
				col := bc + step[1]
				if !arnrInBounds(col, row, mvColMin, mvColMax, mvRowMin, mvRowMax) {
					continue
				}
				sad := arnrSADAt(src, srcStride, ref, mbX, mbY, col, row)
				if sad < bestSAD {
					bestSAD = sad
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br += nextChkpts[k][bestSite][0]
			bc += nextChkpts[k][bestSite][1]
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	// 4-neighbor diamond refinement (libvpx's cal_neighbors loop).
	for j := 0; j < arnrDiaRange; j++ {
		bestSite = -1
		for i, step := range neighbors {
			row := br + step[0]
			col := bc + step[1]
			if !arnrInBounds(col, row, mvColMin, mvColMax, mvRowMin, mvRowMax) {
				continue
			}
			sad := arnrSADAt(src, srcStride, ref, mbX, mbY, col, row)
			if sad < bestSAD {
				bestSAD = sad
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br += neighbors[bestSite][0]
		bc += neighbors[bestSite][1]
	}

	return bestSAD, bc, br
}

func arnrClamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func arnrInBounds(col, row, colMin, colMax, rowMin, rowMax int) bool {
	return col >= colMin && col <= colMax && row >= rowMin && row <= rowMax
}

// arnrSADAt computes the 16x16 SAD between the contiguous source block
// (already gathered into a 16-stride scratch) and the reference block at
// (mbX+mvX, mbY+mvY). When the candidate position straddles the visible
// boundary the read is routed through gatherBlock so libvpx's source-
// border-extension reads stay well-defined.
func arnrSADAt(src []byte, srcStride int, ref arnrFrameView, mbX, mbY, mvX, mvY int) int {
	x := mbX + mvX
	y := mbY + mvY
	if x >= 0 && y >= 0 && x+16 <= ref.width && y+16 <= ref.height {
		return dsp.SAD16x16(src, srcStride, ref.y[y*ref.yStride+x:], ref.yStride)
	}
	var pred [256]byte
	gatherBlock(pred[:], 16, ref.y, ref.yStride, x, y, ref.width, ref.height, 16)
	return dsp.SAD16x16(src, srcStride, pred[:], 16)
}

// arnrSubpelRange constrains the subpel search to the same legal MV window
// the hex search obeys. Each component is in 1/8-pel units.
const arnrSubpelMaxStep = 8 // = 1 full pel

// arnrSubpelRefine implements libvpx's find_fractional_mv_step diamond walk
// for the temporal filter. Inputs are the full-pel MV (fullX, fullY) chosen
// by the hex search; the output is the refined MV in 1/8-pel units along
// with the 16x16 SAD at that subpel position. The walk visits the four
// neighboring subpel offsets (left/right/up/down) at successively finer
// granularities (1/2 -> 1/4 -> 1/8 pel) and adopts the lowest-SAD position;
// libvpx also tests one diagonal per iteration which we mirror.
func arnrSubpelRefine(src []byte, srcStride int, ref arnrFrameView, mbRow, mbCol, mbRows, mbCols int, mbX, mbY, fullX, fullY int) (int, int, int) {
	// Subpel MV bounds (1/8-pel units). The integer-pel search has
	// already clamped to libvpx's mv_row_min/mv_col_min derivation; the
	// subpel walk stays within ±1 full pel of that integer position so
	// the sixtap predictor's 6-tap reach (5 pixels overhang) lines up
	// with the same pixel envelope the hex search reached.
	mvColMinPel := -(mbCol*16 + (16 - 5))
	mvColMaxPel := (mbCols-1-mbCol)*16 + (16 - 5)
	mvRowMinPel := -(mbRow*16 + (16 - 5))
	mvRowMaxPel := (mbRows-1-mbRow)*16 + (16 - 5)
	minCol := mvColMinPel << 3
	maxCol := mvColMaxPel << 3
	minRow := mvRowMinPel << 3
	maxRow := mvRowMaxPel << 3

	bestRow := fullY << 3
	bestCol := fullX << 3
	bestSAD := arnrSADAtSubpel(src, srcStride, ref, mbX, mbY, bestCol, bestRow)

	// libvpx does up to 4 half-pel iters then up to 4 quarter-pel iters
	// in find_fractional_mv_step_iteratively. We extend the same pattern
	// down to 1/8 pel because govpx's MV grid is 1/8 (libvpx's vp8 final
	// MV is also 1/8-pel after multiplying the *4 subpel result by 2).
	steps := [3]int{4, 2, 1} // 1/2-, 1/4-, 1/8-pel deltas in 1/8-pel units
	for _, step := range steps {
		iters := 4
		for it := 0; it < iters; it++ {
			startRow := bestRow
			startCol := bestCol
			// Test the 4 axis-aligned neighbors.
			leftSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow, startCol-step, minRow, maxRow, minCol, maxCol)
			rightSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow, startCol+step, minRow, maxRow, minCol, maxCol)
			upSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow-step, startCol, minRow, maxRow, minCol, maxCol)
			downSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow+step, startCol, minRow, maxRow, minCol, maxCol)
			if leftSAD < bestSAD {
				bestSAD = leftSAD
				bestRow = startRow
				bestCol = startCol - step
			}
			if rightSAD < bestSAD {
				bestSAD = rightSAD
				bestRow = startRow
				bestCol = startCol + step
			}
			if upSAD < bestSAD {
				bestSAD = upSAD
				bestRow = startRow - step
				bestCol = startCol
			}
			if downSAD < bestSAD {
				bestSAD = downSAD
				bestRow = startRow + step
				bestCol = startCol
			}
			// One diagonal probe in the direction of the better
			// horizontal+vertical neighbors, mirroring libvpx's
			// `whichdir` switch.
			dr := -step
			dc := -step
			if downSAD < upSAD {
				dr = step
			}
			if rightSAD < leftSAD {
				dc = step
			}
			diagSAD := arnrSubpelProbe(src, srcStride, ref, mbX, mbY, startRow+dr, startCol+dc, minRow, maxRow, minCol, maxCol)
			if diagSAD < bestSAD {
				bestSAD = diagSAD
				bestRow = startRow + dr
				bestCol = startCol + dc
			}
			if bestRow == startRow && bestCol == startCol {
				break
			}
		}
	}

	return bestSAD, bestCol, bestRow
}

// arnrSubpelProbe checks bounds and returns INT_MAX-equivalent on out-of-
// range so the caller's < bestSAD compare always rejects illegal positions.
func arnrSubpelProbe(src []byte, srcStride int, ref arnrFrameView, mbX, mbY, row, col, minRow, maxRow, minCol, maxCol int) int {
	if row < minRow || row > maxRow || col < minCol || col > maxCol {
		const big = 1<<30 - 1
		return big
	}
	return arnrSADAtSubpel(src, srcStride, ref, mbX, mbY, col, row)
}

// arnrSADAtSubpel computes the 16x16 SAD between the source block (in a
// contiguous 16-stride scratch) and a sixtap-filtered predictor at
// (mbX + col/8 + col%8 fractional, mbY + row/8 + row%8 fractional). When
// the subpel offset is zero this collapses to the integer-pel SAD path.
func arnrSADAtSubpel(src []byte, srcStride int, ref arnrFrameView, mbX, mbY, col, row int) int {
	if (row|col)&7 == 0 {
		return arnrSADAt(src, srcStride, ref, mbX, mbY, col>>3, row>>3)
	}
	var pred [256]byte
	arnrSubpelLuma16x16(pred[:], 16, ref, mbX, mbY, col, row)
	return dsp.SAD16x16(src, srcStride, pred[:], 16)
}

// arnrSubpelLuma16x16 fills a 16x16 predictor block with the sixtap-
// filtered luma reference at integer position (mbX + col>>3, mbY + row>>3)
// plus the (col&7, row&7) 1/8-pel fractional offset. Out-of-visible reads
// are routed through gatherBlock so the predictor stays defined when the
// MV pushes the 6-tap filter footprint past the visible edge.
func arnrSubpelLuma16x16(dst []byte, dstStride int, ref arnrFrameView, mbX, mbY, col, row int) {
	intCol := col >> 3
	intRow := row >> 3
	fracCol := col & 7
	fracRow := row & 7
	// 6-tap reads 2 pixels before and 3 pixels after the prediction
	// origin in each axis. Gather a (16+5)x(16+5)=21x21 region whose
	// origin sits 2 pixels above/left of the integer prediction origin
	// so SixTapPredict16x16 sees a contiguous 21-stride neighborhood.
	const pad = 2
	const gathered = 16 + 5 // 21
	var scratch [gathered * gathered]byte
	gatherBlock(scratch[:], gathered, ref.y, ref.yStride, mbX+intCol-pad, mbY+intRow-pad, ref.width, ref.height, gathered)
	dsp.SixTapPredict16x16(scratch[:], gathered, fracCol, fracRow, dst, dstStride)
}

// arnrPredictLuma16x16 synthesizes the 16x16 luma predictor at the given
// 1/8-pel MV. When the MV is integer-aligned this is gatherBlock; otherwise
// the sixtap filter runs on a clamped 21x21 neighborhood.
func arnrPredictLuma16x16(dst []byte, dstStride int, ref arnrFrameView, mbX, mbY, mvSubX, mvSubY int) {
	if (mvSubX|mvSubY)&7 == 0 {
		gatherBlock(dst, dstStride, ref.y, ref.yStride, mbX+(mvSubX>>3), mbY+(mvSubY>>3), ref.width, ref.height, 16)
		return
	}
	arnrSubpelLuma16x16(dst, dstStride, ref, mbX, mbY, mvSubX, mvSubY)
}

// arnrPredictChroma8x8 synthesizes an 8x8 chroma predictor at the chroma
// 1/8-pel MV. libvpx halves the luma MV before dispatching the chroma
// subpel predictor (vp8_temporal_filter_predictors_mb_c does
// `mv_row >>= 1; mv_col >>= 1`); callers pass the already-halved MV.
func arnrPredictChroma8x8(dst []byte, dstStride int, plane []byte, planeStride int, planeW, planeH int, mbUVX, mbUVY, mvSubX, mvSubY int) {
	intCol := mvSubX >> 3
	intRow := mvSubY >> 3
	fracCol := mvSubX & 7
	fracRow := mvSubY & 7
	if (fracCol | fracRow) == 0 {
		gatherBlock(dst, dstStride, plane, planeStride, mbUVX+intCol, mbUVY+intRow, planeW, planeH, 8)
		return
	}
	const pad = 2
	const gathered = 8 + 5 // 13
	var scratch [gathered * gathered]byte
	gatherBlock(scratch[:], gathered, plane, planeStride, mbUVX+intCol-pad, mbUVY+intRow-pad, planeW, planeH, gathered)
	dsp.SixTapPredict8x8(scratch[:], gathered, fracCol, fracRow, dst, dstStride)
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
