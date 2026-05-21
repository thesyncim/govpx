// VP9 TPL (Temporal Prediction Loop) quality pass.
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
//   - govpx applies the TPL rdmult delta in keyframe and inter mode scoring.
//     The pass still differs from libvpx by using source-order frames and a
//     32x32 SB aggregation rather than libvpx's full scheduler and 8x8 MI grid.
package encoder

import (
	"image"
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

const (
	// TPLSBSizeLog2 is log2 of the TPL super-block size in pixels.
	// govpx aggregates TPL stats at BLOCK_32X32 to keep the propagation
	// loop cheap; libvpx tracks at BLOCK_8X8.
	TPLSBSizeLog2 = 5
	// TPLSBSize is the TPL super-block size in pixels.
	TPLSBSize = 1 << TPLSBSizeLog2
	// TPLMinLookaheadFrames is the smallest lookahead window TPL accepts.
	// Mirrors libvpx's MIN_LOOKAHEAD_FRAMES_TPL.
	TPLMinLookaheadFrames = 8
	// TPLMaxLookaheadFrames bounds the TPL planning window. libvpx caps
	// the practical window at MAX_LAG_BUFFERS=25.
	TPLMaxLookaheadFrames = 25
	// tplMVSearchRange is the half-window for the coarse motion search.
	// Mirrors libvpx's tpl_mv_search_range default of 16.
	tplMVSearchRange = 16
)

// TPLStats holds the per-SB TPL bookkeeping.  Field layout mirrors
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
type TPLStats struct {
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

// TPLFrameStats is the per-frame TPL slab.  Field layout mirrors libvpx's
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
type TPLFrameStats struct {
	SBRows int
	SBCols int
	Stats  []TPLStats
	// R0 is the per-frame intra/dependency-cost ratio used by
	// get_rdmult_delta.  libvpx stores this on cpi->rd.r0 rather than on
	// the slab; govpx keeps it on the slab so the next-to-encode slab
	// carries the right ratio without referencing global encoder state.
	//
	// libvpx: vp9/encoder/vp9_encodeframe.c:5707-5708
	//   cpi->rd.r0 = (double)intra_cost_base / mc_dep_cost_base;
	R0 float64
	// Valid is set once the slab has been populated by
	// [TPLState.Populate].  Mirrors libvpx's TplDepFrame.is_valid.
	Valid bool
}

// TPLState holds the per-encoder TPL planning state.  It lives across the
// life of a VP9Encoder when EnableTPL is set and is rebuilt on resolution
// changes.
type TPLState struct {
	Enabled bool
	// width/height pin the size the SB grid was sized for; a mismatch
	// triggers a rebuild on the next Populate call.
	width  int
	height int
	sbRows int
	sbCols int

	// frames holds one slab per source-order future frame.  Index 0 is the
	// current display-order frame; subsequent indices are the lookahead
	// distance.  Slabs are zero-initialised until Populate is called.
	frames []TPLFrameStats
}

func TPLSBGridDims(width, height int) (rows, cols int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	rows = (height + TPLSBSize - 1) >> TPLSBSizeLog2
	cols = (width + TPLSBSize - 1) >> TPLSBSizeLog2
	return rows, cols
}

// FrameR0 returns the per-frame intra/mc_dep_cost ratio for the next frame
// to encode, or zero if no slab is ready. Mirrors libvpx's cpi->rd.r0 lookup
// at the start of get_rdmult_delta (vp9_encodeframe.c:3651).
func (s *TPLState) FrameR0() float64 {
	slab := s.FrameSlab()
	if slab == nil {
		return 0
	}
	return slab.R0
}

// FrameSlab returns the active per-SB slab for the next frame to encode, or
// nil if no slab is ready.
func (s *TPLState) FrameSlab() *TPLFrameStats {
	if s == nil || !s.Enabled || len(s.frames) == 0 {
		return nil
	}
	slab := &s.frames[0]
	if !slab.Valid {
		return nil
	}
	return slab
}

// FrameCount reports how many lookahead slabs are allocated.
func (s *TPLState) FrameCount() int {
	if s == nil {
		return 0
	}
	return len(s.frames)
}

// Dimensions returns the resolution and SB grid pinned to the current slabs.
func (s *TPLState) Dimensions() (width, height, sbRows, sbCols int) {
	if s == nil {
		return 0, 0, 0, 0
	}
	return s.width, s.height, s.sbRows, s.sbCols
}

// Configure sizes the TPL state for the encoder's resolution.  It is safe to
// call repeatedly; the slabs are reallocated only when the dimensions change.
func (s *TPLState) Configure(enabled bool, width, height, lookahead int) {
	s.Enabled = enabled
	if !enabled {
		s.frames = nil
		s.width = 0
		s.height = 0
		s.sbRows = 0
		s.sbCols = 0
		return
	}
	sbRows, sbCols := TPLSBGridDims(width, height)
	if s.width == width && s.height == height && len(s.frames) >= lookahead {
		s.InvalidateAll()
		return
	}
	s.width = width
	s.height = height
	s.sbRows = sbRows
	s.sbCols = sbCols
	if lookahead <= 0 {
		lookahead = 1
	}
	if lookahead > TPLMaxLookaheadFrames {
		lookahead = TPLMaxLookaheadFrames
	}
	s.frames = make([]TPLFrameStats, lookahead)
	cells := sbRows * sbCols
	for i := range s.frames {
		s.frames[i] = TPLFrameStats{
			SBRows: sbRows,
			SBCols: sbCols,
			Stats:  make([]TPLStats, cells),
		}
	}
}

// InvalidateAll marks every slab as needing recomputation without freeing the
// backing slices.
func (s *TPLState) InvalidateAll() {
	for i := range s.frames {
		s.frames[i].Valid = false
		s.frames[i].R0 = 0
		for j := range s.frames[i].Stats {
			s.frames[i].Stats[j] = TPLStats{}
		}
	}
}

// ShiftAndInvalidate consumes the head slab (frame just encoded) by rotating
// the buffer left and marking the new tail as stale.  This is called after
// each TPL-tracked frame to keep slab 0 aligned with the next pending source.
func (s *TPLState) ShiftAndInvalidate() {
	if len(s.frames) <= 1 {
		s.InvalidateAll()
		return
	}
	head := s.frames[0]
	copy(s.frames[:len(s.frames)-1], s.frames[1:])
	// Reuse the head slab as the new tail to avoid a fresh allocation.
	head.Valid = false
	head.R0 = 0
	for j := range head.Stats {
		head.Stats[j] = TPLStats{}
	}
	s.frames[len(s.frames)-1] = head
}

// Populate runs the TPL pass against the supplied lookahead-source frames.
// frames[0] is the frame currently being encoded; frames[i] for i>0 are the
// source-order future frames that govpx will encode next.  The window must
// contain at least TPLMinLookaheadFrames entries; shorter windows leave the
// state un-populated (callers must fall back to the regulated qindex).
//
// The function is deliberately self-contained and does not touch encoder
// reconstruction state — it operates purely on the lookahead sources, so it
// can run before the per-tile encode without coordinating with the row-MT or
// frame-parallel encoder paths.
func (s *TPLState) Populate(frames []*image.YCbCr) {
	if !s.Enabled || len(frames) < TPLMinLookaheadFrames || len(s.frames) == 0 {
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
func (s *TPLState) computeIntraStats(src *image.YCbCr, slab *TPLFrameStats) {
	if src == nil || slab == nil {
		return
	}
	for row := 0; row < slab.SBRows; row++ {
		for col := 0; col < slab.SBCols; col++ {
			intraCost := int64(TPLBlockSelfVariance(src, row, col))
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
func (s *TPLState) computeFrameStats(src, ref *image.YCbCr,
	slab *TPLFrameStats) {
	if src == nil || ref == nil || slab == nil {
		return
	}
	srcW := src.Rect.Dx()
	srcH := src.Rect.Dy()
	for row := 0; row < slab.SBRows; row++ {
		for col := 0; col < slab.SBCols; col++ {
			intraCost := int64(TPLBlockSelfVariance(src, row, col))
			interCostRaw, mvRow, mvCol := TPLBlockMotionSearch(src, ref,
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
			// frames.  Reset McFlow/McRefCost so a re-Populate on
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
func (s *TPLState) propagateFrame(slab, next *TPLFrameStats) {
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
			nextRow := row + int(st.MVRow)>>TPLSBSizeLog2
			nextCol := col + int(st.MVCol)>>TPLSBSizeLog2
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
func (s *TPLState) deriveFrameR0(slab *TPLFrameStats) {
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

func tplClampCoord(v, limit int) int {
	switch {
	case limit <= 0:
		return 0
	case v < 0:
		return 0
	case v >= limit:
		return limit - 1
	default:
		return v
	}
}

// TPLBlockSelfVariance returns the simple per-SB luma variance proxy.  We
// use sum-of-squares minus mean*mean so the metric remains stable on flat
// constant SBs (returns zero).
func TPLBlockSelfVariance(src *image.YCbCr, sbRow, sbCol int) uint32 {
	w := src.Rect.Dx()
	h := src.Rect.Dy()
	y0 := sbRow << TPLSBSizeLog2
	x0 := sbCol << TPLSBSizeLog2
	var sum, sse int
	for r := range TPLSBSize {
		yy := tplClampCoord(y0+r, h)
		row := src.Y[yy*src.YStride:]
		for c := range TPLSBSize {
			xx := tplClampCoord(x0+c, w)
			v := int(row[xx])
			sum += v
			sse += v * v
		}
	}
	const pixels = TPLSBSize * TPLSBSize
	// variance = sse - (sum*sum)/pixels.
	mean := sum / pixels
	v := min(max(int64(sse-mean*sum), 0), int64(math.MaxUint32))
	return uint32(v)
}

// TPLBlockMotionSearch performs a coarse integer-pel diamond-style motion
// search and returns the matched SAD plus the matched MV.  The search range
// is tplMVSearchRange on each axis, matching libvpx's
// tpl_mv_search_range default.
func TPLBlockMotionSearch(src, ref *image.YCbCr, sbRow, sbCol,
	srcW, srcH int) (uint32, int, int) {
	refW := ref.Rect.Dx()
	refH := ref.Rect.Dy()
	y0 := sbRow << TPLSBSizeLog2
	x0 := sbCol << TPLSBSizeLog2
	bestSAD := uint32(math.MaxUint32)
	bestMVRow := 0
	bestMVCol := 0
	for dy := -tplMVSearchRange; dy <= tplMVSearchRange; dy += 4 {
		for dx := -tplMVSearchRange; dx <= tplMVSearchRange; dx += 4 {
			sad := TPLBlockSAD(src, ref, y0, x0, y0+dy, x0+dx,
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
			if dy < -tplMVSearchRange || dy > tplMVSearchRange ||
				dx < -tplMVSearchRange || dx > tplMVSearchRange {
				continue
			}
			sad := TPLBlockSAD(src, ref, y0, x0, y0+dy, x0+dx,
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

// TPLBlockSAD returns the sum-of-absolute-differences between a 32x32 SB
// in src at (srcY, srcX) and an offset SB in ref at (refY, refX).  Coordinates
// outside the frame are clamped at the edge, matching libvpx's edge-mirror
// behavior under motion search.
func TPLBlockSAD(src, ref *image.YCbCr, srcY, srcX, refY, refX,
	srcW, srcH, refW, refH int) uint32 {
	var sad uint32
	for r := range TPLSBSize {
		sy := tplClampCoord(srcY+r, srcH)
		ry := tplClampCoord(refY+r, refH)
		srcRow := src.Y[sy*src.YStride:]
		refRow := ref.Y[ry*ref.YStride:]
		for c := range TPLSBSize {
			sx := tplClampCoord(srcX+c, srcW)
			rx := tplClampCoord(refX+c, refW)
			diff := int(srcRow[sx]) - int(refRow[rx])
			if diff < 0 {
				diff = -diff
			}
			sad += uint32(diff)
		}
	}
	return sad
}

// RDMultDelta returns the per-SB rdmult that libvpx's TPL would apply at the
// given MI coordinates, falling back to origRdmult when TPL is inactive or no
// slab is populated.
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
// the encoder.KeyframeRDMul value it already computed; the result is therefore
// orig_rdmult / beta with the libvpx clamp applied.
func (s *TPLState) RDMultDelta(miRow, miCol, blockMiHigh, blockMiWide,
	origRdmult int) int {
	if origRdmult <= 0 {
		return 1
	}
	slab := s.FrameSlab()
	if slab == nil || slab.R0 <= 0 {
		return origRdmult
	}
	// Sum intra_cost / mc_dep_cost over the SBs the block touches.  govpx
	// tracks stats at 32x32 SB granularity; libvpx tracks at 8x8 MI.  We
	// convert the (mi_row, mi_col, mi_high, mi_wide) tuple into the SB
	// range that overlaps the block.
	miPerSB := TPLSBSize / common.MiSize
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
	return dr
}
