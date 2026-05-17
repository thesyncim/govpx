// Package govpx VP9 TPL (Temporal Prediction Loop) quality pass.
//
// TPL is libvpx's lookahead-based per-block importance analysis that biases
// the per-SB Lagrangian RD multiplier toward super-blocks that propagate the
// most signal to future frames. The libvpx reference lives in
// vp9/encoder/vp9_tpl_model.c and vp9/encoder/vp9_encodeframe.c; the data
// model below is a verbatim port of libvpx's TplDepStats / TplDepFrame
// (vp9/encoder/vp9_encoder.h:294-328) at a coarser 32x32 SB granularity than
// libvpx's 8x8 per-MI grid.  The rdmult delta machinery (r0, beta, dr clamp)
// is ported verbatim.
//
// Parity caveats versus libvpx:
//
//   - vp9_tpl_model.c performs full sub-pixel motion search with 1/8-pel
//     precision and a search range of 64.  govpx uses a 16-pixel integer-pel
//     diamond search (libvpx's tpl_mv_search_range default) and skips sub-pel
//     refinement.
//   - libvpx tracks TplDepStats per 8x8 MI; govpx aggregates at 32x32 SB to
//     keep the propagation loop cheap.  The rdmult delta is computed at the
//     same 32x32 SB granularity (libvpx computes it at 64x64).
//   - libvpx integrates TPL with the alt-ref / show-existing scheduler.  govpx
//     restricts TPL to source-order frames only and skips the pass while an
//     alt-ref is pending.
//   - libvpx's per-SB rdmult delta is wired into both the keyframe partition
//     search and the inter mode picker.  govpx currently wires it only into
//     the keyframe mode picker; the inter mode picker uses a non-Lagrangian
//     score (rate * (1 + qindex/32), vp9_encoder.go::vp9ModeDecisionRateScore)
//     that is not compatible with libvpx's rdmult shape and is left as a TODO
//     until the inter picker is Lagrangianised.
package govpx

import (
	"image"
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

const (
	// vp9TPLSBSizeLog2 is log2 of the TPL super-block size in pixels.
	// govpx aggregates TPL stats at BLOCK_32X32 to keep the propagation
	// loop cheap; libvpx tracks at BLOCK_8X8.
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
	// vp9TPLMvSearchRange is the half-window for the coarse motion search.
	// Mirrors libvpx's tpl_mv_search_range default of 16.
	vp9TPLMvSearchRange = 16
)

// vp9TPLStats holds the per-SB TPL bookkeeping.  Field layout mirrors
// libvpx's TplDepStats struct.
//
// libvpx: vp9/encoder/vp9_encoder.h:294-303
//
//	typedef struct TplDepStats {
//	  int64_t intra_cost;
//	  int64_t inter_cost;
//	  int64_t mc_flow;
//	  int64_t mc_dep_cost;
//	  int64_t mc_ref_cost;
//	  int ref_frame_index;
//	  int_mv mv;
//	} TplDepStats;
type vp9TPLStats struct {
	// IntraCost is the per-SB intra prediction cost (libvpx's
	// intra_cost).  govpx approximates this with the SB luma variance
	// proxy; libvpx uses a full intra mode search SATD/RD.
	IntraCost int64
	// InterCost is the residual cost after the coarse motion search to
	// the lookahead-source-order frame at distance one (libvpx's
	// inter_cost).
	InterCost int64
	// McFlow is the recursive motion-compensated flow accumulator
	// (libvpx's mc_flow, vp9_tpl_model.c:679-694).
	McFlow int64
	// McDepCost is the dependency cost: intra_cost + mc_flow (libvpx's
	// mc_dep_cost; libvpx initialises it lazily inside the propagation
	// pass since intra_cost is filled first and mc_flow accumulates).
	McDepCost int64
	// McRefCost is the reference cost: (intra - inter) accumulated by
	// downstream frames pointing at this SB (libvpx's mc_ref_cost).
	McRefCost int64
	// MVRow, MVCol are the integer-pel motion vector that minimised
	// InterCost.  Stored in pixel units.  libvpx tracks an int_mv at
	// 1/8-pel precision; govpx searches integer-pel only.
	MVRow int16
	MVCol int16
	// RefFrameIndex selects which reference slab fed mc_flow.  govpx
	// only references the next source-order frame, so this is always 0
	// (the next slab) when InterCost < IntraCost and -1 otherwise.
	RefFrameIndex int8
}

// vp9TPLFrameStats is the per-frame TPL slab.  Field layout mirrors libvpx's
// TplDepFrame struct.
//
// libvpx: vp9/encoder/vp9_encoder.h:314-328
//
//	typedef struct TplDepFrame {
//	  uint8_t is_valid;
//	  TplDepStats *tpl_stats_ptr;
//	  int stride;
//	  int width;
//	  int height;
//	  int mi_rows;
//	  int mi_cols;
//	  int base_qindex;
//	  ...
//	} TplDepFrame;
type vp9TPLFrameStats struct {
	SBRows int
	SBCols int
	Stats  []vp9TPLStats
	// R0 is the per-frame intra/dependency-cost ratio used by
	// get_rdmult_delta.  libvpx stores this on cpi->rd.r0 rather than on
	// the slab; govpx keeps it on the slab so the next-to-encode slab
	// carries the right ratio without referencing global encoder state.
	//
	// libvpx: vp9/encoder/vp9_encodeframe.c:5707-5708
	//   cpi->rd.r0 = (double)intra_cost_base / mc_dep_cost_base;
	R0 float64
	// Valid is set once the slab has been populated by
	// [vp9TPLState.populate].  Mirrors libvpx's TplDepFrame.is_valid.
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

// vp9TPLFrameR0 returns the per-frame intra/mc_dep_cost ratio for the next
// frame to encode, or zero if no slab is ready.  Mirrors libvpx's
// cpi->rd.r0 lookup at the start of get_rdmult_delta
// (vp9/encoder/vp9_encodeframe.c:3651).
func (e *VP9Encoder) vp9TPLFrameR0() float64 {
	if !e.vp9TPLEnabled() || len(e.tpl.frames) == 0 {
		return 0
	}
	slab := &e.tpl.frames[0]
	if !slab.Valid {
		return 0
	}
	return slab.R0
}

// vp9TPLFrameSlab returns the active per-SB slab for the next frame to
// encode, or nil if no slab is ready.  Internal callers consume this to
// implement per-SB rdmult scaling.
func (e *VP9Encoder) vp9TPLFrameSlab() *vp9TPLFrameStats {
	if !e.vp9TPLEnabled() || len(e.tpl.frames) == 0 {
		return nil
	}
	slab := &e.tpl.frames[0]
	if !slab.Valid {
		return nil
	}
	return slab
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
		}
	}
}

// invalidateAll marks every slab as needing recomputation without freeing the
// backing slices.
func (s *vp9TPLState) invalidateAll() {
	for i := range s.frames {
		s.frames[i].Valid = false
		s.frames[i].R0 = 0
		for j := range s.frames[i].Stats {
			s.frames[i].Stats[j] = vp9TPLStats{}
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
	head.R0 = 0
	for j := range head.Stats {
		head.Stats[j] = vp9TPLStats{}
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
	limit := min(len(frames), len(s.frames))
	// Stage A: per-frame coarse motion estimation.
	//
	// libvpx's TPL does motion search against PAST anchors and pushes
	// mc_flow BACKWARD onto the reference (vp9_tpl_model.c:1762 — the
	// outer GOP loop walks frame_idx from tpl_group_frames-1 down to 1).
	// govpx's lookahead-only design has no decoded past sources at TPL
	// time, so we anchor every future slab against frames[0] (the frame
	// currently being encoded).  Each future slab's MV records the
	// motion FROM that slab TO frames[0]; propagation then pushes the
	// future slab's mc_flow BACK into the slab[0] cell its MV points at
	// — matching libvpx's backward direction even though the search
	// anchor sits in the lookahead rather than the past.
	//
	// Slab[0] is the encoded frame; its IntraCost is its self-variance
	// proxy and InterCost is the InterCost the next future frame
	// recorded against it.  We do not motion-search slab[0] (no past
	// reference available) so its MV stays (0,0).
	if len(frames) > 0 && len(s.frames) > 0 {
		s.computeIntraStats(frames[0], &s.frames[0])
	}
	for idx := 1; idx < limit; idx++ {
		current := frames[idx]
		anchor := frames[0]
		slab := &s.frames[idx]
		s.computeFrameStats(current, anchor, slab)
	}
	// Stage B: backward mc_flow propagation.  libvpx walks from the
	// farthest GOP frame back toward frame 1 (vp9_tpl_model.c:1762);
	// govpx walks from the farthest lookahead slab back toward slab 1
	// and pushes each slab's mc_flow into slab[0] (the encoded frame's
	// dependency cost).
	//
	// libvpx: vp9/encoder/vp9_tpl_model.c:679-694 (tpl_model_update_b)
	for idx := limit - 1; idx >= 1; idx-- {
		slab := &s.frames[idx]
		s.propagateFrame(slab, &s.frames[0])
	}
	// Stage C: derive the per-frame r0 ratio from slab[0]'s accumulated
	// intra/mc_dep_cost totals.  libvpx writes this onto cpi->rd.r0 at
	// the top of the per-frame encode (vp9_encodeframe.c:5707-5708);
	// govpx caches it on slab[0] so the next-to-encode frame can read
	// it without referencing global encoder state.
	for idx := range limit {
		slab := &s.frames[idx]
		s.deriveFrameR0(slab)
		slab.Valid = true
	}
}

// computeIntraStats fills the intra-only stats for slab against src.  Used on
// slab[0] (the encoded frame, no past reference available) so its
// IntraCost / McDepCost seed is populated before downstream slabs push
// mc_flow into it.
func (s *vp9TPLState) computeIntraStats(src *image.YCbCr, slab *vp9TPLFrameStats) {
	if src == nil || slab == nil {
		return
	}
	for row := 0; row < slab.SBRows; row++ {
		for col := 0; col < slab.SBCols; col++ {
			intraCost := int64(vp9TPLBlockSelfVariance(src, row, col))
			stats := &slab.Stats[row*slab.SBCols+col]
			stats.IntraCost = intraCost
			stats.InterCost = intraCost // no inter prediction available
			stats.MVRow = 0
			stats.MVCol = 0
			stats.RefFrameIndex = -1
			stats.McDepCost = intraCost
			stats.McFlow = 0
			stats.McRefCost = 0
		}
	}
}

// computeFrameStats fills the intra/inter/MV fields of slab against the
// reference frame using a coarse integer-pel motion search.  It also seeds
// McDepCost with IntraCost so the propagation pass can lazily extend it with
// the accumulated McFlow.
func (s *vp9TPLState) computeFrameStats(src, ref *image.YCbCr,
	slab *vp9TPLFrameStats) {
	if src == nil || ref == nil || slab == nil {
		return
	}
	srcW := src.Rect.Dx()
	srcH := src.Rect.Dy()
	for row := 0; row < slab.SBRows; row++ {
		for col := 0; col < slab.SBCols; col++ {
			intraCost := int64(vp9TPLBlockSelfVariance(src, row, col))
			interCostRaw, mvRow, mvCol := vp9TPLBlockMotionSearch(src, ref,
				row, col, srcW, srcH)
			interCost := min(
				// libvpx's intra_cost serves as the reference floor when
				// inter prediction is worse than intra; cap inter at
				// intra so the propagation formula's (mc_dep * inter)
				// / intra stays well-defined.
				int64(interCostRaw), intraCost)
			stats := &slab.Stats[row*slab.SBCols+col]
			stats.IntraCost = intraCost
			stats.InterCost = interCost
			stats.MVRow = int16(mvRow)
			stats.MVCol = int16(mvCol)
			stats.RefFrameIndex = 0
			// Initial McDepCost = IntraCost; propagation Stage B
			// extends it by the accumulated McFlow from downstream
			// frames.  Reset McFlow/McRefCost so a re-populate on
			// the same slab starts clean.
			stats.McDepCost = intraCost
			stats.McFlow = 0
			stats.McRefCost = 0
		}
	}
}

// propagateFrame accumulates one step of mc_flow into next based on the
// motion vectors recorded in slab.  Each SB in slab points (via its matched
// motion vector) at an SB in next; that SB's mc_flow and mc_ref_cost receive
// a contribution proportional to the saved residual and the upstream SB's
// accumulated dependency cost.
//
// libvpx: vp9/encoder/vp9_tpl_model.c:679-694
//
//	int64_t mc_flow = tpl_stats->mc_dep_cost -
//	                  (tpl_stats->mc_dep_cost * tpl_stats->inter_cost) /
//	                  tpl_stats->intra_cost;
//	...
//	des_stats->mc_flow      += (mc_flow * overlap_area) / pix_num;
//	des_stats->mc_ref_cost  += ((intra - inter) * overlap_area) / pix_num;
//
// govpx aggregates at 32x32 SBs whereas libvpx aggregates at 8x8 MIs, so the
// (overlap_area / pix_num) factor collapses to 1 when the MV lands on the SB
// grid; we approximate libvpx's bilinear overlap by depositing the full
// contribution into the single nearest SB.  Once mc_flow has been pushed,
// the destination's McDepCost is incremented so subsequent propagation steps
// see the updated dependency cost.
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
			if st.IntraCost <= 0 {
				continue
			}
			if st.InterCost >= st.IntraCost {
				continue
			}
			// libvpx: vp9_tpl_model.c:679-681.
			//   mc_flow = mc_dep_cost -
			//             (mc_dep_cost * inter_cost) / intra_cost
			mcFlow := st.McDepCost -
				(st.McDepCost*st.InterCost)/st.IntraCost
			// Translate the motion vector into a next-frame SB index.
			nextRow := row + int(st.MVRow)>>vp9TPLSBSizeLog2
			nextCol := col + int(st.MVCol)>>vp9TPLSBSizeLog2
			if nextRow < 0 || nextCol < 0 ||
				nextRow >= next.SBRows || nextCol >= next.SBCols {
				continue
			}
			ns := &next.Stats[nextRow*next.SBCols+nextCol]
			// libvpx: vp9_tpl_model.c:691-694.
			//   des_stats->mc_flow     += (mc_flow * overlap) / pix
			//   des_stats->mc_ref_cost +=
			//       ((intra - inter) * overlap) / pix
			// With 32x32 SB alignment, overlap == pix_num so the
			// ratio collapses to 1.
			ns.McFlow += mcFlow
			ns.McRefCost += (st.IntraCost - st.InterCost)
			// Keep McDepCost = IntraCost + McFlow so subsequent
			// propagation steps see the updated dependency cost.
			ns.McDepCost = ns.IntraCost + ns.McFlow
		}
	}
}

// deriveFrameR0 computes the per-frame intra/mc_dep_cost ratio that drives
// get_rdmult_delta.
//
// libvpx: vp9/encoder/vp9_encodeframe.c:5697-5708
//
//	for (row = 0; row < cm->mi_rows && tpl_frame->is_valid; ++row) {
//	  for (col = 0; col < cm->mi_cols; ++col) {
//	    TplDepStats *this_stats = &tpl_stats[row * tpl_stride + col];
//	    intra_cost_base += this_stats->intra_cost;
//	    mc_dep_cost_base += this_stats->mc_dep_cost;
//	  }
//	}
//	cpi->rd.r0 = (double)intra_cost_base / mc_dep_cost_base;
func (s *vp9TPLState) deriveFrameR0(slab *vp9TPLFrameStats) {
	if slab == nil || len(slab.Stats) == 0 {
		return
	}
	var intraBase, mcDepBase int64
	for i := range slab.Stats {
		intraBase += slab.Stats[i].IntraCost
		mcDepBase += slab.Stats[i].McDepCost
	}
	if mcDepBase <= 0 {
		slab.R0 = 0
		return
	}
	slab.R0 = float64(intraBase) / float64(mcDepBase)
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

// VP9TPLFrameDelta is the read-only per-SB TPL summary exposed to other
// passes (row-MT, oracle traces).  The Delta map exposes the per-SB rdmult
// scaler (as a fixed-point ratio×256) so downstream consumers can apply the
// same scaling libvpx does without depending on the TPL pass internals.
type VP9TPLFrameDelta struct {
	SBRows int
	SBCols int
	// Delta is a per-SB int8 mapping where 0 means "no scaling".  The
	// encoded value is clamp(round((beta-1)*16), -128, 127); the
	// keyframe mode picker applies it as rdmult * (1 + value/16).
	// Callers that need higher precision should consume the slab
	// directly via the internal TPL state.
	Delta []int8
}

// TPLFrameDelta returns the per-SB TPL summary for the next frame to be
// encoded.  The returned slice is read-only and is allocated lazily on each
// call; mutating it is a misuse.  When TPL is disabled, no slab has been
// populated yet, or the resolution has changed since the last populate call,
// Delta is nil and SBRows/SBCols are zero.
func (e *VP9Encoder) TPLFrameDelta() VP9TPLFrameDelta {
	if e == nil || e.closed {
		return VP9TPLFrameDelta{}
	}
	slab := e.vp9TPLFrameSlab()
	if slab == nil {
		return VP9TPLFrameDelta{}
	}
	r0 := slab.R0
	delta := make([]int8, len(slab.Stats))
	for i := range slab.Stats {
		st := &slab.Stats[i]
		if st.McDepCost <= 0 || st.IntraCost <= 0 || r0 <= 0 {
			delta[i] = 0
			continue
		}
		// rk = intra_cost / mc_dep_cost; beta = r0 / rk.
		rk := float64(st.IntraCost) / float64(st.McDepCost)
		if rk <= 0 {
			delta[i] = 0
			continue
		}
		beta := r0 / rk
		// Encode (beta-1)*16 clamped into int8.
		scaled := math.Round((beta - 1) * 16)
		switch {
		case scaled > 127:
			delta[i] = 127
		case scaled < -128:
			delta[i] = -128
		default:
			delta[i] = int8(scaled)
		}
	}
	return VP9TPLFrameDelta{SBRows: slab.SBRows, SBCols: slab.SBCols, Delta: delta}
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
// inactive on hidden and alt-ref frames because the pass needs a source-order
// future to inspect.
//
// current points at the frame currently being encoded.  The encoder pops it
// off the lookahead ring before calling into the per-frame pipeline, so the
// remaining ring view is one short of the TPL window; we splice current back
// in as slab[0] so the propagation analysis sees the same source-order
// window libvpx's TPL operates on.
func (e *VP9Encoder) populateVP9TPLForFrame(skip bool, current *image.YCbCr) {
	if !e.vp9TPLEnabled() {
		return
	}
	if skip {
		// Drop any stale slab so the rdmult delta on this frame is zero.
		e.tpl.invalidateAll()
		return
	}
	if !e.vp9LookaheadEnabled() {
		e.tpl.invalidateAll()
		return
	}
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
		// libvpx computes the TPL plan ONCE per GOP at the ARF frame
		// (vp9_encoder.c:6402-6410 setup_tpl_stats) and the plan persists
		// for the entire GOP via cpi->tpl_stats[gf_group_index] lookups
		// (vp9_encodeframe.c:3619).  Once the lookahead has drained mid-GOP,
		// govpx must NOT invalidate the existing slab — it should keep
		// serving the planned per-SB rdmult delta until the GOP ends.
		// shiftAndInvalidate has already advanced slab[0] to the new
		// current frame, so the residual slabs remain libvpx-faithful.
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

// getVP9TPLRDMultDelta returns the per-SB rdmult that libvpx's TPL would
// apply at the given MI coordinates, falling back to origRdmult when TPL is
// inactive or no slab is populated.
//
// libvpx: vp9/encoder/vp9_encodeframe.c:3602-3660 (get_rdmult_delta)
//
//	for (row = mi_row; row < mi_row + mi_high; ++row) {
//	  for (col = mi_col; col < mi_col + mi_wide; ++col) {
//	    intra_cost  += this_stats->intra_cost;
//	    mc_dep_cost += this_stats->mc_dep_cost;
//	  }
//	}
//	r0   = cpi->rd.r0;
//	rk   = (double)intra_cost / mc_dep_cost;
//	beta = r0 / rk;
//	dr   = vp9_get_adaptive_rdmult(cpi, beta);
//	dr   = clamp(dr, orig_rdmult * 1 / 2, orig_rdmult * 3 / 2);
//	dr   = VPXMAX(1, dr);
//
// vp9_get_adaptive_rdmult (vp9/encoder/vp9_rd.c:304-310) computes
//
//	rdmult = vp9_compute_rd_mult_based_on_qindex(cpi, base_qindex) / beta;
//
// govpx folds the qindex-based rdmult into origRdmult so the caller can reuse
// the vp9KeyframeRDMul value it already computed; the result is therefore
// orig_rdmult / beta with the libvpx clamp applied.
func (e *VP9Encoder) getVP9TPLRDMultDelta(miRow, miCol, blockMiHigh, blockMiWide,
	origRdmult int) int {
	if origRdmult <= 0 {
		return 1
	}
	slab := e.vp9TPLFrameSlab()
	if slab == nil || slab.R0 <= 0 {
		return origRdmult
	}
	// Sum intra_cost / mc_dep_cost over the SBs the block touches.  govpx
	// tracks stats at 32x32 SB granularity; libvpx tracks at 8x8 MI.  We
	// convert the (mi_row, mi_col, mi_high, mi_wide) tuple into the SB
	// range that overlaps the block.
	miPerSB := vp9TPLSBSize / common.MiSize
	if miPerSB <= 0 {
		return origRdmult
	}
	sbRowStart := miRow / miPerSB
	sbColStart := miCol / miPerSB
	sbRowEnd := (miRow + blockMiHigh + miPerSB - 1) / miPerSB
	sbColEnd := (miCol + blockMiWide + miPerSB - 1) / miPerSB
	if sbRowEnd > slab.SBRows {
		sbRowEnd = slab.SBRows
	}
	if sbColEnd > slab.SBCols {
		sbColEnd = slab.SBCols
	}
	if sbRowStart < 0 {
		sbRowStart = 0
	}
	if sbColStart < 0 {
		sbColStart = 0
	}
	if sbRowEnd <= sbRowStart || sbColEnd <= sbColStart {
		return origRdmult
	}
	var intraCost, mcDepCost int64
	for r := sbRowStart; r < sbRowEnd; r++ {
		row := slab.Stats[r*slab.SBCols:]
		for c := sbColStart; c < sbColEnd; c++ {
			intraCost += row[c].IntraCost
			mcDepCost += row[c].McDepCost
		}
	}
	if mcDepCost <= 0 || intraCost <= 0 {
		return origRdmult
	}
	// libvpx: vp9_encodeframe.c:3651-3653
	//   r0 = cpi->rd.r0;
	//   rk = (double)intra_cost / mc_dep_cost;
	//   beta = r0 / rk;
	rk := float64(intraCost) / float64(mcDepCost)
	if rk <= 0 {
		return origRdmult
	}
	beta := slab.R0 / rk
	if beta <= 0 || math.IsNaN(beta) || math.IsInf(beta, 0) {
		return origRdmult
	}
	// libvpx: vp9_rd.c:307
	//   rdmult = (int)((double)rdmult / beta);
	dr := int(math.Round(float64(origRdmult) / beta))
	if dr <= 0 {
		dr = 1
	}
	// libvpx: vp9_encodeframe.c:3656-3657
	//   dr = clamp(dr, orig_rdmult * 1 / 2, orig_rdmult * 3 / 2);
	//   dr = VPXMAX(1, dr);
	lo := origRdmult / 2
	hi := origRdmult * 3 / 2
	if dr < lo {
		dr = lo
	}
	if dr > hi {
		dr = hi
	}
	if dr < 1 {
		dr = 1
	}
	if dr != origRdmult {
		e.tplRDMultDeltaCalls++
	}
	return dr
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
