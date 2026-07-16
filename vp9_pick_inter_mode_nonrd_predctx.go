package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// vp9NonrdPredBlockCtx is the search-side equivalent of libvpx's prepared
// per-block prediction state in vp9_pick_inter_mode:
//
//   - find_predictors runs vp9_setup_pred_block once per usable reference
//     (vp9_pickmode.c:1286), leaving yv12_mb[ref][plane] buf_2d pointers into
//     the persistent padded reference frame;
//   - the candidate loop's "select prediction reference frames" step only
//     copies those prepared pointers (vp9_pickmode.c:2230-2234);
//   - vp9_build_inter_predictors_sby then clamps the candidate MV and
//     convolves directly from pd->pre[0] into pd->dst
//     (vp9_reconinter.c::build_inter_predictors, unscaled branch).
//
// govpx's per-candidate path previously re-validated the reference, re-derived
// the block geometry and UMV edges, and chose between a visible-plane read, a
// padded-plane read, and a decoder-recon fallback for every candidate. This
// context hoists all of that to one setup per (block, ref); the per-candidate
// leaf work is exactly libvpx's: clamp -> subpel decompose -> convolve/copy.
type vp9NonrdPredBlockCtx struct {
	src            []byte
	srcStride      int
	srcW, srcH     int
	x0, y0         int
	blockW, blockH int
	srcOff         int
	edges          vp9dec.BlockBoundsEdges
	ref            [vp9dec.MaxRefFrames]vp9NonrdRefPredPlane
}

// vp9NonrdRefPredPlane mirrors libvpx's buf_2d pre[0] for one reference: a
// pointer into the persistent padded (bordered) luma plane plus the offset of
// this block's origin at zero motion.
type vp9NonrdRefPredPlane struct {
	pre       []byte
	preStride int
	// baseOff is the offset of (x0, y0) inside pre at zero motion:
	// (originY+y0)*preStride + originX + x0.
	baseOff int
	// dims8 records whether the reference dimensions are multiples of 8;
	// only used to mirror the legacy zero-MV phase-stat counting shape.
	dims8 bool
	valid bool
}

// vp9NonrdPredBlockCtxInit prepares the ref-independent block state. Returns
// false when the block shape cannot use the prepared path (callers keep the
// legacy per-candidate route).
func (e *VP9Encoder) vp9NonrdPredBlockCtxInit(c *vp9NonrdPredBlockCtx,
	inter *vp9InterEncodeState, miRow, miCol int, bsize common.BlockSize,
) bool {
	if inter == nil || inter.img == nil ||
		bsize < common.Block8x8 || bsize >= common.BlockSizes {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 || srcW <= 0 || srcH <= 0 {
		return false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if blockW <= 0 || blockH <= 0 || x0 < 0 || y0 < 0 {
		return false
	}
	// Same MI-derived edge shape as the legacy direct predictor
	// (predictVP9InterBlockLumaToScratchDirect); libvpx's set_mi_row_col.
	miRows := (e.opts.Height + 7) >> 3
	miCols := (e.opts.Width + 7) >> 3
	c.src = src
	c.srcStride = srcStride
	c.srcW = srcW
	c.srcH = srcH
	c.x0 = x0
	c.y0 = y0
	c.blockW = blockW
	c.blockH = blockH
	c.srcOff = y0*srcStride + x0
	c.edges = vp9dec.BlockBoundsEdges{
		MbToLeftEdge:   -((miCol * common.MiSize) * 8),
		MbToRightEdge:  ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) * common.MiSize) * 8,
		MbToTopEdge:    -((miRow * common.MiSize) * 8),
		MbToBottomEdge: ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) * common.MiSize) * 8,
	}
	for r := range c.ref {
		c.ref[r] = vp9NonrdRefPredPlane{}
	}
	return true
}

// vp9NonrdPredBlockCtxAddRef prepares one reference's padded-plane pointer for
// the block, mirroring vp9_setup_pred_block. It proves ONCE that every
// UMV-clamped candidate window (including the 8-tap 3-before/4-after
// extension) stays inside the persistent padded plane, so the per-candidate
// path carries no window checks at all.
func (e *VP9Encoder) vp9NonrdPredBlockCtxAddRef(c *vp9NonrdPredBlockCtx,
	refFrame int8, ref *vp9ReferenceFrame,
) bool {
	if ref == nil || !ref.valid || ref.img.Width != e.opts.Width ||
		ref.img.Height != e.opts.Height || ref.img.Width <= 0 ||
		ref.img.Height <= 0 ||
		refFrame < vp9dec.LastFrame || refFrame >= vp9dec.MaxRefFrames {
		return false
	}
	pre, preStride, originX, originY, _, _, ok :=
		e.vp9SubpelReferencePlane(refFrame, ref)
	if !ok || len(pre) == 0 || preStride <= 0 {
		return false
	}
	preRows := len(pre) / preStride
	// Exact coverage proof: the clamp bounds from ClampMvToUmvBorderSb (luma
	// ss=0 doubles the 1/8-pel edges into q4 units) define the extreme
	// full-pel window origins; add the 8-tap extension on each side.
	spelLeft := (vp9dec.VP9InterpExtend + c.blockW) << vp9dec.SubpelBitsConst
	spelRight := spelLeft - vp9dec.SubpelShifts
	spelTop := (vp9dec.VP9InterpExtend + c.blockH) << vp9dec.SubpelBitsConst
	spelBottom := spelTop - vp9dec.SubpelShifts
	minCol := c.edges.MbToLeftEdge*2 - spelLeft
	maxCol := c.edges.MbToRightEdge*2 + spelRight
	minRow := c.edges.MbToTopEdge*2 - spelTop
	maxRow := c.edges.MbToBottomEdge*2 + spelBottom
	minReadX := originX + c.x0 + (minCol >> vp9dec.SubpelBitsConst) -
		(vp9dec.VP9InterpExtend - 1)
	maxReadX := originX + c.x0 + (maxCol >> vp9dec.SubpelBitsConst) +
		c.blockW - 1 + vp9dec.VP9InterpExtend
	minReadY := originY + c.y0 + (minRow >> vp9dec.SubpelBitsConst) -
		(vp9dec.VP9InterpExtend - 1)
	maxReadY := originY + c.y0 + (maxRow >> vp9dec.SubpelBitsConst) +
		c.blockH - 1 + vp9dec.VP9InterpExtend
	if minReadX < 0 || maxReadX >= preStride ||
		minReadY < 0 || maxReadY >= preRows {
		return false
	}
	c.ref[refFrame] = vp9NonrdRefPredPlane{
		pre:       pre,
		preStride: preStride,
		baseOff:   (originY+c.y0)*preStride + originX + c.x0,
		dims8: (ref.img.Width&0x7) == 0 &&
			(ref.img.Height&0x7) == 0,
		valid: true,
	}
	return true
}

// vp9NonrdCtxPredictLuma is the prepared-context candidate predictor: the
// verbatim unscaled build_inter_predictors leaf
// (vp9/common/vp9_reconinter.c): clamp the MV to the UMV border, split into
// full-pel offset plus q4 subpel phase, and convolve/copy directly from the
// persistent padded reference into dst.
func (e *VP9Encoder) vp9NonrdCtxPredictLuma(c *vp9NonrdPredBlockCtx,
	rp *vp9NonrdRefPredPlane, mv vp9dec.MV, filter vp9dec.InterpFilter,
	dst []byte, dstStride int,
) bool {
	filterIdx := int(filter)
	if filterIdx < 0 || filterIdx >= int(vp9dec.InterpSwitchable) {
		return false
	}
	if dstStride < c.blockW || (c.blockH-1)*dstStride+c.blockW > len(dst) {
		return false
	}
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(c.edges, mv, c.blockW, c.blockH, 0, 0)
	subpelX := int(mvQ4.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(mvQ4.Row) & (vp9dec.SubpelShifts - 1)
	off := rp.baseOff +
		(int(mvQ4.Row)>>vp9dec.SubpelBitsConst)*rp.preStride +
		(int(mvQ4.Col) >> vp9dec.SubpelBitsConst)
	if vp9PhaseStatsEnabled {
		e.vp9PhaseIncInterPredictionBlock()
		// Mirror the legacy counting shape: the zero-MV copy fast path
		// counted only the block, not a plane predictor.
		if !(mv == (vp9dec.MV{}) && rp.dims8) {
			d := &e.interPredictor
			d.setVP9PhaseStats(e.vp9PhaseStats())
			d.vp9PhaseIncInterPredictPlane()
			key := 0
			if subpelX != 0 {
				key |= 4
			}
			if subpelY != 0 {
				key |= 2
			}
			d.vp9PhaseCountInterPredictor(key)
			d.setVP9PhaseStats(nil)
		}
	}
	vp9dec.InterPredictorWithScratch(rp.pre, rp.preStride, dst, dstStride,
		subpelX, subpelY, tables.FilterKernels[filterIdx],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, c.blockW, c.blockH, 0, off,
		&e.interPredictor.convolveScratch)
	return true
}

// vp9NonrdPredictVarianceSSETo builds one candidate's luma predictor into dst
// and scores (variance, sse) against the source — libvpx's
// vp9_build_inter_predictors_sby + fn_ptr[bsize].vf pair
// (vp9_pickmode.c:2336 and model_rd_for_sb_y). When the prepared context does
// not cover the (block, ref) shape it falls back to the legacy per-candidate
// route, byte-for-byte.
func (e *VP9Encoder) vp9NonrdPredictVarianceSSETo(c *vp9NonrdPredBlockCtx,
	inter *vp9InterEncodeState, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mode common.PredictionMode, refFrame int8,
	mv vp9dec.MV, filter vp9dec.InterpFilter, dst []byte, dstStride int,
) (variance, sse uint64, ok bool) {
	if c == nil || refFrame < 0 || int(refFrame) >= len(c.ref) ||
		!c.ref[refFrame].valid {
		return e.vp9InterPredictionVarianceSSETo(inter, miRows, miCols, miRow,
			miCol, bsize, mode, refFrame, mv, filter, dst, dstStride)
	}
	if vp9PhaseStatsEnabled {
		e.vp9PhaseIncInterPredictionVariance()
	}
	if !e.vp9NonrdCtxPredictLuma(c, &c.ref[refFrame], mv, filter, dst,
		dstStride) {
		return 0, 0, false
	}
	return encoder.BlockDiffVarianceSSEClampedSource(c.src, c.srcStride,
		c.srcW, c.srcH, dst, dstStride, c.x0, c.y0, 0, 0, c.blockW, c.blockH)
}
