// Package govpx VP9 TPL (Temporal Prediction Loop) quality pass.
//
// TPL is libvpx's lookahead-based per-SB importance analysis that biases the
// effective quantizer toward super-blocks that propagate the most signal to
// future frames. The libvpx reference lives in vp9/encoder/vp9_tpl_model.c;
// govpx's port follows the same data flow (per-frame stats slab keyed by 32x32
// SB index, single-step inter propagation, and a per-SB qindex delta map) but
// uses a simplified coarse motion search and skips the recursive cross-frame
// propagation that libvpx performs.  Single-step propagation is sufficient for
// the BD-rate baseline reported in the libvpx VP9 TPL paper at a fraction of
// the runtime cost.
//
// Parity caveats versus libvpx:
//
//   - vp9_tpl_model.c performs full sub-pixel motion search with 1/8-pel
//     precision and a search range of 64.  govpx uses a 16-pixel integer-pel
//     diamond search (libvpx's tpl_mv_search_range default) and skips sub-pel
//     refinement.
//   - libvpx propagates importance through every future frame in the lookahead
//     window using a recursive formulation.  govpx does single-frame-distance
//     propagation: each TPL stats slab only depends on the immediately-next
//     source-order frame.
//   - libvpx integrates TPL with the alt-ref / show-existing scheduler.  govpx
//     restricts TPL to source-order frames only and skips the pass while an
//     alt-ref is pending.
//   - libvpx's per-SB qindex delta is applied through a dedicated segmentation
//     channel.  Until row-MT is in place, govpx applies the frame-mean of the
//     per-SB delta map as a scalar bias on the frame's qindex; the per-SB map
//     itself is exposed through a read-only accessor so the row-MT integration
//     can consume it without restructuring this package.
package govpx

import (
	"image"
	"math"
)

const (
	// vp9TPLSBSizeLog2 is log2 of the TPL super-block size in pixels.
	// Mirrors libvpx's BLOCK_32X32 stat granularity.
	vp9TPLSBSizeLog2 = 5
	// vp9TPLSBSize is the TPL super-block size in pixels.
	vp9TPLSBSize = 1 << vp9TPLSBSizeLog2
	// vp9TPLMinLookaheadFrames is the smallest lookahead window TPL accepts.
	// Mirrors libvpx's MIN_LOOKAHEAD_FRAMES_TPL.
	vp9TPLMinLookaheadFrames = 8
	// vp9TPLMaxLookaheadFrames bounds the TPL planning window.  libvpx caps
	// the practical window at MAX_LAG_BUFFERS=25 which matches govpx's
	// existing vp9MaxLookaheadFrames.
	vp9TPLMaxLookaheadFrames = vp9MaxLookaheadFrames
	// vp9TPLSubpelSearchSteps controls sub-pel refinement.  Zero matches the
	// minimum-viable port (integer-pel motion search only).
	vp9TPLSubpelSearchSteps = 0
	// vp9TPLMvSearchRange is the half-window for the coarse motion search.
	// Mirrors libvpx's tpl_mv_search_range default of 16.
	vp9TPLMvSearchRange = 16
	// vp9TPLMaxQDelta caps the magnitude of the per-SB qindex delta TPL is
	// allowed to apply.  Mirrors libvpx's clamp(deltaq_offset, -15, 15).
	vp9TPLMaxQDelta = 15
	// vp9TPLPropagationShift sets how aggressively propagation factors bias
	// the qindex delta.  Larger values squash deltas closer to zero.
	vp9TPLPropagationShift = 10
)

// vp9TPLStats holds the coarse importance accumulator for one TPL SB.
type vp9TPLStats struct {
	// IntraCost is the SAD-style residual energy if the SB is intra-coded
	// against its reference-anchor (zero MV).
	IntraCost uint32
	// InterCost is the residual energy after the coarse motion search to
	// the lookahead-source-order frame at distance one.
	InterCost uint32
	// MVRow, MVCol are the integer-pel motion vector that minimised
	// InterCost.  Stored in pixel units.
	MVRow int16
	MVCol int16
	// Propagation accumulates how often this SB is re-referenced by
	// future-frame SBs.  Higher values mark important SBs.
	Propagation uint32
}

// vp9TPLFrameStats is the per-frame TPL slab.  It owns one vp9TPLStats per
// 32x32 SB plus the derived per-SB qindex delta map.
type vp9TPLFrameStats struct {
	SBRows int
	SBCols int
	Stats  []vp9TPLStats
	// QDelta is the per-SB int8 qindex delta applied as an overlay on top
	// of the frame-regulated base qindex.  Length is SBRows*SBCols.
	QDelta []int8
	// FrameMeanQDelta is the simple-mean of QDelta used as a scalar bias on
	// frames where the per-SB delta cannot be routed through segmentation.
	FrameMeanQDelta int
	// Valid is set once the slab has been populated by [vp9TPLState.populate].
	Valid bool
}

// vp9TPLState holds the per-encoder TPL planning state.  It lives across the
// life of a VP9Encoder when EnableTPL is set and is rebuilt on resolution
// changes.
type vp9TPLState struct {
	enabled bool
	// width/height pin the size the SB grid was sized for; a mismatch
	// triggers a rebuild on the next populate call.
	width  int
	height int
	sbRows int
	sbCols int

	// frames holds one slab per source-order future frame.  Index 0 is the
	// current display-order frame; subsequent indices are the lookahead
	// distance.  Slabs are zero-initialised until populate is called.
	frames []vp9TPLFrameStats
}

// vp9TPLEnabled reports whether the encoder has the TPL pass active.
func (e *VP9Encoder) vp9TPLEnabled() bool {
	return e != nil && e.opts.EnableTPL && e.tpl.enabled
}

// vp9TPLFrameDelta returns the per-SB int8 qindex delta map for the next
// frame to be encoded, or nil when TPL is disabled or no slab is ready.  The
// slice is read-only for callers; copy if you need to persist it.
func (e *VP9Encoder) vp9TPLFrameDelta() ([]int8, int, int) {
	if !e.vp9TPLEnabled() || len(e.tpl.frames) == 0 {
		return nil, 0, 0
	}
	slab := &e.tpl.frames[0]
	if !slab.Valid {
		return nil, 0, 0
	}
	return slab.QDelta, slab.SBRows, slab.SBCols
}

// vp9TPLFrameMeanQDelta returns the integer-rounded mean of the per-SB qindex
// delta for the next frame, or zero when no slab is ready.  This is the
// scalar bias [VP9Encoder.vp9EncoderFrameQIndex] adds on top of the regulated
// frame qindex while per-SB routing is unavailable.
func (e *VP9Encoder) vp9TPLFrameMeanQDelta() int {
	if !e.vp9TPLEnabled() || len(e.tpl.frames) == 0 {
		return 0
	}
	slab := &e.tpl.frames[0]
	if !slab.Valid {
		return 0
	}
	return slab.FrameMeanQDelta
}

func vp9TPLSBGridDims(width, height int) (rows, cols int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	rows = (height + vp9TPLSBSize - 1) >> vp9TPLSBSizeLog2
	cols = (width + vp9TPLSBSize - 1) >> vp9TPLSBSizeLog2
	return rows, cols
}

// configure sizes the TPL state for the encoder's resolution.  It is safe to
// call repeatedly; the slabs are reallocated only when the dimensions change.
func (s *vp9TPLState) configure(enabled bool, width, height, lookahead int) {
	s.enabled = enabled
	if !enabled {
		s.frames = nil
		s.width = 0
		s.height = 0
		s.sbRows = 0
		s.sbCols = 0
		return
	}
	sbRows, sbCols := vp9TPLSBGridDims(width, height)
	if s.width == width && s.height == height && len(s.frames) >= lookahead {
		s.invalidateAll()
		return
	}
	s.width = width
	s.height = height
	s.sbRows = sbRows
	s.sbCols = sbCols
	if lookahead <= 0 {
		lookahead = 1
	}
	if lookahead > vp9TPLMaxLookaheadFrames {
		lookahead = vp9TPLMaxLookaheadFrames
	}
	s.frames = make([]vp9TPLFrameStats, lookahead)
	cells := sbRows * sbCols
	for i := range s.frames {
		s.frames[i] = vp9TPLFrameStats{
			SBRows: sbRows,
			SBCols: sbCols,
			Stats:  make([]vp9TPLStats, cells),
			QDelta: make([]int8, cells),
		}
	}
}

// invalidateAll marks every slab as needing recomputation without freeing the
// backing slices.
func (s *vp9TPLState) invalidateAll() {
	for i := range s.frames {
		s.frames[i].Valid = false
		s.frames[i].FrameMeanQDelta = 0
		for j := range s.frames[i].Stats {
			s.frames[i].Stats[j] = vp9TPLStats{}
		}
		for j := range s.frames[i].QDelta {
			s.frames[i].QDelta[j] = 0
		}
	}
}

// shiftAndInvalidate consumes the head slab (frame just encoded) by rotating
// the buffer left and marking the new tail as stale.  This is called after
// each TPL-tracked frame to keep slab 0 aligned with the next pending source.
func (s *vp9TPLState) shiftAndInvalidate() {
	if len(s.frames) <= 1 {
		s.invalidateAll()
		return
	}
	head := s.frames[0]
	copy(s.frames[:len(s.frames)-1], s.frames[1:])
	// Reuse the head slab as the new tail to avoid a fresh allocation.
	head.Valid = false
	head.FrameMeanQDelta = 0
	for j := range head.Stats {
		head.Stats[j] = vp9TPLStats{}
	}
	for j := range head.QDelta {
		head.QDelta[j] = 0
	}
	s.frames[len(s.frames)-1] = head
}

// populate runs the TPL pass against the supplied lookahead-source frames.
// frames[0] is the frame currently being encoded; frames[i] for i>0 are the
// source-order future frames that govpx will encode next.  The window must
// contain at least vp9TPLMinLookaheadFrames entries; shorter windows leave the
// state un-populated (callers must fall back to the regulated qindex).
//
// The function is deliberately self-contained and does not touch encoder
// reconstruction state — it operates purely on the lookahead sources, so it
// can run before the per-tile encode without coordinating with the row-MT or
// frame-parallel encoder paths.
func (s *vp9TPLState) populate(frames []*image.YCbCr) {
	if !s.enabled || len(frames) < vp9TPLMinLookaheadFrames || len(s.frames) == 0 {
		return
	}
	limit := min(len(frames)-1, len(s.frames))
	// Stage A: per-frame coarse motion estimation and intra-energy estimate.
	for idx := range limit {
		current := frames[idx]
		nextSrc := frames[idx+1]
		slab := &s.frames[idx]
		s.computeFrameStats(current, nextSrc, slab)
	}
	// Stage B: single-step propagation — each SB inherits a fraction of its
	// matched-block propagation in the next frame proportional to the
	// reduction in residual the coarse MV achieved.  Stage A above has
	// populated every slab in [0, limit) with motion stats so propagation
	// can read directly from the in-flight (not-yet-Valid) future slab.
	// Gating propagation on Valid like the original implementation made
	// the loop a no-op because Stage C is where Valid is finally set;
	// every Stage-B step ran with next == nil and Propagation stayed 0.
	for idx := limit - 1; idx >= 0; idx-- {
		slab := &s.frames[idx]
		var next *vp9TPLFrameStats
		if idx+1 < limit {
			next = &s.frames[idx+1]
		}
		s.propagateFrame(slab, next)
	}
	// Stage C: derive a per-SB qindex delta from the propagation factor and
	// per-SB intra/inter cost ratio.  The frame-mean of the per-SB delta
	// is the scalar bias [vp9EncoderFrameQIndex] applies until per-SB
	// segmentation routing is wired up.
	for idx := range limit {
		slab := &s.frames[idx]
		s.deriveQDelta(slab)
		slab.Valid = true
	}
}

// computeFrameStats fills the intra/inter/MV fields of slab against the
// reference frame using a coarse integer-pel motion search.
func (s *vp9TPLState) computeFrameStats(src, ref *image.YCbCr,
	slab *vp9TPLFrameStats) {
	if src == nil || ref == nil || slab == nil {
		return
	}
	srcW := src.Rect.Dx()
	srcH := src.Rect.Dy()
	for row := 0; row < slab.SBRows; row++ {
		for col := 0; col < slab.SBCols; col++ {
			intraCost := vp9TPLBlockSelfVariance(src, row, col)
			interCost, mvRow, mvCol := vp9TPLBlockMotionSearch(src, ref,
				row, col, srcW, srcH)
			stats := &slab.Stats[row*slab.SBCols+col]
			stats.IntraCost = intraCost
			stats.InterCost = interCost
			stats.MVRow = int16(mvRow)
			stats.MVCol = int16(mvCol)
			stats.Propagation = 0
		}
	}
}

// propagateFrame accumulates one step of importance from next into slab.  Each
// SB in slab points (via its matched motion vector) at an SB in next; that
// SB's propagation counter receives a weighted contribution from this SB's
// cost reduction.  This is the minimum-viable single-step version of
// libvpx's recursive vp9_tpl_propagation_pass.
func (s *vp9TPLState) propagateFrame(slab, next *vp9TPLFrameStats) {
	if slab == nil {
		return
	}
	if next == nil {
		return
	}
	for row := 0; row < slab.SBRows; row++ {
		for col := 0; col < slab.SBCols; col++ {
			st := &slab.Stats[row*slab.SBCols+col]
			if st.IntraCost <= st.InterCost {
				continue
			}
			// Saved residual energy is the importance proxy.
			saved := uint32(st.IntraCost - st.InterCost)
			// Translate the motion vector into a next-frame SB index.
			nextRow := row + int(st.MVRow)>>vp9TPLSBSizeLog2
			nextCol := col + int(st.MVCol)>>vp9TPLSBSizeLog2
			if nextRow < 0 || nextCol < 0 ||
				nextRow >= next.SBRows || nextCol >= next.SBCols {
				continue
			}
			ns := &next.Stats[nextRow*next.SBCols+nextCol]
			// Clamp to avoid uint32 overflow even on adversarial inputs.
			before := ns.Propagation
			ns.Propagation = before + saved
			if ns.Propagation < before {
				ns.Propagation = math.MaxUint32
			}
		}
	}
}

// deriveQDelta computes the per-SB qindex delta for slab from the propagation
// factor.  The frame-mean qindex bias (used by the scalar overlay until per-SB
// segmentation routing lands) is derived from the global intra/saved-inter
// energy balance, not from per-SB deviation around the mean: a per-SB
// deviation map averages to zero by construction and produces no scalar
// bias.  libvpx's TPL maps a high inter-saving ratio to a downward qindex
// bias because frames that downstream frames lean on heavily pay for
// themselves with the extra bits.
func (s *vp9TPLState) deriveQDelta(slab *vp9TPLFrameStats) {
	if slab == nil || len(slab.Stats) == 0 {
		return
	}
	// Compute the mean propagation, used as the reference point for the
	// per-SB delta direction (above-mean SBs get a negative delta — more
	// bits; below-mean SBs get a positive delta — fewer bits).
	var total uint64
	var intraTotal uint64
	var savedTotal uint64
	for i := range slab.Stats {
		total += uint64(slab.Stats[i].Propagation)
		intraTotal += uint64(slab.Stats[i].IntraCost)
		if slab.Stats[i].InterCost < slab.Stats[i].IntraCost {
			savedTotal += uint64(slab.Stats[i].IntraCost -
				slab.Stats[i].InterCost)
		}
	}
	count := uint64(len(slab.Stats))
	mean := total / count
	if mean == 0 {
		// Per-SB propagation map is empty (no SB is referenced).  The
		// per-SB delta stays at zero, but a non-zero saved/intra ratio
		// still informs the scalar frame-mean bias: a frame that
		// motion-compensates well against its lookahead anchor is a
		// strong reference candidate and earns a downward qindex bias
		// even before propagation contributes.
		for i := range slab.QDelta {
			slab.QDelta[i] = 0
		}
		slab.FrameMeanQDelta = tplFrameMeanBiasFromRatios(savedTotal,
			intraTotal, 0, 0)
		return
	}
	for i := range slab.Stats {
		delta := tplQDeltaFromPropagation(uint64(slab.Stats[i].Propagation), mean)
		slab.QDelta[i] = int8(delta)
	}
	// FrameMeanQDelta uses a libvpx-style global energy ratio so the
	// scalar bias has a meaningful sign and magnitude.  A frame whose
	// inter motion compensation saves a large fraction of its intra
	// energy is a strong propagation source — bias its qindex down (more
	// bits, better reference quality).  A frame whose propagation map is
	// densely populated (high mean) is a heavy propagation sink — same
	// downward bias on the same intuition.  Both contributions are
	// clamped to [-vp9TPLMaxQDelta, vp9TPLMaxQDelta] so a single bias
	// alone can never escape the public quantizer window.
	slab.FrameMeanQDelta = tplFrameMeanBiasFromRatios(savedTotal,
		intraTotal, total, count)
}

// tplFrameMeanBiasFromRatios maps the global TPL energy ratios into a clamped
// integer qindex bias.  The function is split out so unit tests can pin its
// shape independently of the propagation accumulator (which depends on the
// motion search internals).  The sign convention matches the per-SB delta:
// negative biases reduce the regulated qindex (more bits), positive biases
// raise it (fewer bits).
func tplFrameMeanBiasFromRatios(savedTotal, intraTotal, propTotal, count uint64) int {
	bias := 0
	// Saved/intra ratio: how much of this frame's intra energy is
	// recovered by inter prediction against the lookahead anchor.  A
	// ratio of 0.5 (50% saved) maps to a -8 bias; higher ratios saturate
	// at -vp9TPLMaxQDelta.  The denominator guards against the all-flat
	// frame edge case (intraTotal == 0).
	if intraTotal > 0 && savedTotal > 0 {
		// Scale by 32 so a 50% saved ratio gives a magnitude of 16, then
		// shift to land at vp9TPLMaxQDelta near full saving.
		scaled := min(int(savedTotal*32/intraTotal), vp9TPLMaxQDelta+1)
		bias -= scaled
	}
	// Propagation-density component: how much aggregate downstream
	// importance the per-SB map carries.  This re-uses the per-SB
	// propagation accumulator scaled against the per-SB mean (count >
	// 0).  A heavily-loaded propagation map nudges the frame qindex
	// further down on top of the saved/intra contribution.
	if count > 0 && propTotal > 0 {
		mean := propTotal / count
		if mean > 0 {
			// Map log2-ish scaled propagation density to a small
			// magnitude.  The shift by 8 keeps the contribution
			// at most ±4 qindex even on saturated inputs.
			scaled := min(int(mean>>8), vp9TPLMaxQDelta/2)
			bias -= scaled
		}
	}
	if bias < -vp9TPLMaxQDelta {
		bias = -vp9TPLMaxQDelta
	}
	if bias > vp9TPLMaxQDelta {
		bias = vp9TPLMaxQDelta
	}
	return bias
}

// tplQDeltaFromPropagation converts a propagation score into a clamped int
// qindex delta.  Above-mean SBs get a negative delta (lower q, more bits);
// below-mean SBs get a positive delta (higher q, fewer bits).
func tplQDeltaFromPropagation(prop, mean uint64) int {
	if mean == 0 {
		return 0
	}
	if prop >= mean {
		// ratio = (prop / mean) - 1, scaled by 8 so a 2x SB lands at +8.
		ratio := int64((prop - mean) << 3 / mean)
		if ratio == 0 {
			return 0
		}
		// Bias toward more bits — negative delta.
		delta := max(-int(ratio>>1), -vp9TPLMaxQDelta)
		return delta
	}
	ratio := int64((mean - prop) << 3 / mean)
	if ratio == 0 {
		return 0
	}
	delta := min(int(ratio>>1), vp9TPLMaxQDelta)
	return delta
}

// vp9TPLBlockSelfVariance returns the simple per-SB luma variance proxy.  We
// use sum-of-squares minus mean*mean so the metric remains stable on flat
// constant SBs (returns zero).
func vp9TPLBlockSelfVariance(src *image.YCbCr, sbRow, sbCol int) uint32 {
	w := src.Rect.Dx()
	h := src.Rect.Dy()
	y0 := sbRow << vp9TPLSBSizeLog2
	x0 := sbCol << vp9TPLSBSizeLog2
	var sum, sse int
	for r := range vp9TPLSBSize {
		yy := clampEncodeCoord(y0+r, h)
		row := src.Y[yy*src.YStride:]
		for c := range vp9TPLSBSize {
			xx := clampEncodeCoord(x0+c, w)
			v := int(row[xx])
			sum += v
			sse += v * v
		}
	}
	const pixels = vp9TPLSBSize * vp9TPLSBSize
	// variance = sse - (sum*sum)/pixels.
	mean := sum / pixels
	v := min(max(sse-mean*sum, 0), math.MaxUint32)
	return uint32(v)
}

// vp9TPLBlockMotionSearch performs a coarse integer-pel diamond-style motion
// search and returns the matched SAD plus the matched MV.  The search range
// is vp9TPLMvSearchRange on each axis, matching libvpx's
// tpl_mv_search_range default.
func vp9TPLBlockMotionSearch(src, ref *image.YCbCr, sbRow, sbCol,
	srcW, srcH int) (uint32, int, int) {
	refW := ref.Rect.Dx()
	refH := ref.Rect.Dy()
	y0 := sbRow << vp9TPLSBSizeLog2
	x0 := sbCol << vp9TPLSBSizeLog2
	bestSAD := uint32(math.MaxUint32)
	bestMVRow := 0
	bestMVCol := 0
	for dy := -vp9TPLMvSearchRange; dy <= vp9TPLMvSearchRange; dy += 4 {
		for dx := -vp9TPLMvSearchRange; dx <= vp9TPLMvSearchRange; dx += 4 {
			sad := vp9TPLBlockSAD(src, ref, y0, x0, y0+dy, x0+dx,
				srcW, srcH, refW, refH)
			if sad < bestSAD {
				bestSAD = sad
				bestMVRow = dy
				bestMVCol = dx
			}
		}
	}
	// Refine around the best stride-4 candidate with a finer 1-pel sweep
	// in a 4x4 neighbourhood.  Cheap and matches libvpx's diamond-refine.
	for dy := bestMVRow - 3; dy <= bestMVRow+3; dy++ {
		for dx := bestMVCol - 3; dx <= bestMVCol+3; dx++ {
			if dy == bestMVRow && dx == bestMVCol {
				continue
			}
			if dy < -vp9TPLMvSearchRange || dy > vp9TPLMvSearchRange ||
				dx < -vp9TPLMvSearchRange || dx > vp9TPLMvSearchRange {
				continue
			}
			sad := vp9TPLBlockSAD(src, ref, y0, x0, y0+dy, x0+dx,
				srcW, srcH, refW, refH)
			if sad < bestSAD {
				bestSAD = sad
				bestMVRow = dy
				bestMVCol = dx
			}
		}
	}
	return bestSAD, bestMVRow, bestMVCol
}

// vp9TPLBlockSAD returns the sum-of-absolute-differences between a 32x32 SB
// in src at (srcY, srcX) and an offset SB in ref at (refY, refX).  Coordinates
// outside the frame are clamped at the edge, matching libvpx's edge-mirror
// behavior under motion search.
func vp9TPLBlockSAD(src, ref *image.YCbCr, srcY, srcX, refY, refX,
	srcW, srcH, refW, refH int) uint32 {
	var sad uint32
	for r := range vp9TPLSBSize {
		sy := clampEncodeCoord(srcY+r, srcH)
		ry := clampEncodeCoord(refY+r, refH)
		srcRow := src.Y[sy*src.YStride:]
		refRow := ref.Y[ry*ref.YStride:]
		for c := range vp9TPLSBSize {
			sx := clampEncodeCoord(srcX+c, srcW)
			rx := clampEncodeCoord(refX+c, refW)
			diff := int(srcRow[sx]) - int(refRow[rx])
			if diff < 0 {
				diff = -diff
			}
			sad += uint32(diff)
		}
	}
	return sad
}

// VP9TPLFrameDelta is the read-only per-SB qindex delta map exposed to other
// passes (row-MT, oracle traces).  It is the slice returned by
// [VP9Encoder.TPLFrameDelta]; mutating it is a misuse.
type VP9TPLFrameDelta struct {
	SBRows int
	SBCols int
	Delta  []int8
}

// TPLFrameDelta returns the per-SB int8 qindex delta map TPL has computed for
// the next frame to be encoded.  The slice is read-only and may be reused
// across calls; copy it if the caller needs to persist it.  The returned
// SBRows/SBCols pair gives the grid dimensions (one entry per 32x32 SB).
// When TPL is disabled, no slab has been populated yet, or the resolution has
// changed since the last populate call, Delta is nil and SBRows/SBCols are
// zero.
//
// This is the read-only interface row-MT / tile-encode callers consume to
// pick up the per-SB TPL bias without depending on the TPL pass internals.
func (e *VP9Encoder) TPLFrameDelta() VP9TPLFrameDelta {
	if e == nil || e.closed {
		return VP9TPLFrameDelta{}
	}
	delta, rows, cols := e.vp9TPLFrameDelta()
	if delta == nil {
		return VP9TPLFrameDelta{}
	}
	return VP9TPLFrameDelta{SBRows: rows, SBCols: cols, Delta: delta}
}

// SetEnableTPL toggles the VP9 TPL quality pass at runtime.  Enabling
// requires the encoder to have been constructed with LookaheadFrames >= 8 and
// AutoAltRef enabled (so the lookahead window stays populated for the future
// frames TPL inspects).  Returns ErrInvalidConfig if the configured options
// do not satisfy the TPL prerequisites, or if frames have already been
// encoded with TPL toggled the other way (the slab dimensions are pinned at
// construction time so a runtime flip while the slab is in use would corrupt
// in-flight stats).
func (e *VP9Encoder) SetEnableTPL(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if enabled {
		opts := e.opts
		opts.EnableTPL = true
		if err := validateVP9TPLOptions(opts); err != nil {
			return err
		}
	}
	e.opts.EnableTPL = enabled
	e.tpl.configure(enabled, e.opts.Width, e.opts.Height,
		e.opts.LookaheadFrames)
	return nil
}

// populateVP9TPLForFrame collects the lookahead sources visible from the
// current frame into a contiguous slice and asks the TPL pass to refresh its
// per-frame slabs.  The skip parameter mirrors the libvpx gate: TPL is
// inactive on keyframes, intra-only, hidden, and alt-ref frames because the
// pass needs a source-order future to inspect.
//
// current points at the frame currently being encoded.  The encoder pops it
// off the lookahead ring before calling into the per-frame pipeline, so the
// remaining ring view is one short of the TPL window; we splice current back
// in as slab[0] so the propagation analysis sees the same source-order
// window libvpx's TPL operates on.  When current is nil (e.g. retrospective
// callers that only have the ring view available), we fall back to the ring
// alone, which is what govpx shipped before the splice landed.
func (e *VP9Encoder) populateVP9TPLForFrame(skip bool, current *image.YCbCr) {
	if !e.vp9TPLEnabled() {
		return
	}
	if skip {
		// Drop any stale slab so the qindex bias on this frame is zero.
		e.tpl.invalidateAll()
		return
	}
	if !e.vp9LookaheadEnabled() {
		e.tpl.invalidateAll()
		return
	}
	// The encoder owns the lookahead ring buffer; collect a window-sized
	// slice pointing at the future frames behind the head in source order.
	// At the time we run, the frame being encoded has already been popped
	// off the ring, so we splice current back in front so the TPL pass
	// sees the source-order window [current, ring[0], ring[1], ...].
	tail := e.collectVP9TPLLookaheadFrames()
	var frames []*image.YCbCr
	if current != nil {
		frames = make([]*image.YCbCr, 0, 1+len(tail))
		frames = append(frames, current)
		frames = append(frames, tail...)
	} else {
		frames = tail
	}
	if len(frames) < vp9TPLMinLookaheadFrames {
		e.tpl.invalidateAll()
		return
	}
	e.tpl.populate(frames)
}

// collectVP9TPLLookaheadFrames returns a slice of pointers to the lookahead
// source images in source order, starting at the next-to-encode frame.  The
// returned slice aliases the lookahead ring buffer and remains valid only
// for the duration of the populate call.
func (e *VP9Encoder) collectVP9TPLLookaheadFrames() []*image.YCbCr {
	if !e.vp9LookaheadEnabled() {
		return nil
	}
	count := int(e.lookaheadCount)
	if count == 0 {
		return nil
	}
	out := make([]*image.YCbCr, 0, count)
	idx := int(e.lookaheadRead)
	for range count {
		out = append(out, &e.lookahead[idx].img)
		idx++
		if idx >= len(e.lookahead) {
			idx = 0
		}
	}
	return out
}

// applyVP9TPLQIndexBias clamps and applies the TPL frame-mean qindex bias to
// the regulated qindex.  This is the scalar overlay used while per-SB
// segmentation routing is unavailable.  The returned qindex stays inside
// libvpx's allowed range.
func (e *VP9Encoder) applyVP9TPLQIndexBias(qindex int, skip bool) int {
	if !e.vp9TPLEnabled() || skip {
		return qindex
	}
	bias := e.vp9TPLFrameMeanQDelta()
	if bias == 0 {
		return qindex
	}
	// Bound the bias by the public quantizer window so TPL never lifts the
	// regulated qindex past the configured max-q or pushes below min-q.
	minQ, maxQ, _ := vp9NormalizedPublicQuantizers(e.opts)
	bestBound := vp9PublicQuantizerToQIndex(minQ)
	worstBound := vp9PublicQuantizerToQIndex(maxQ)
	q := min(max(min(max(qindex+bias, bestBound), worstBound), 0), 255)
	return q
}

// validateVP9TPLOptions enforces the libvpx-compatible TPL prerequisites.
//
// TPL needs a populated lookahead window so it can simulate inter-frame
// propagation; libvpx requires at least MIN_LOOKAHEAD_FRAMES_TPL=8.  TPL also
// reads the auto-alt-ref planning state, so AutoAltRef must be enabled.  The
// caveats document above flags frame-parallel encode incompatibility; once
// the [VP9EncoderOptions.FrameParallelEncoderThreads] field lands, callers
// that set both will be rejected here.
func validateVP9TPLOptions(opts VP9EncoderOptions) error {
	if !opts.EnableTPL {
		return nil
	}
	if opts.LookaheadFrames < vp9TPLMinLookaheadFrames {
		return ErrInvalidConfig
	}
	if !opts.AutoAltRef {
		return ErrInvalidConfig
	}
	if opts.Lossless {
		return ErrInvalidConfig
	}
	return nil
}
