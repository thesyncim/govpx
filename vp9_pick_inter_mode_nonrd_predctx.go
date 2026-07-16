package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
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

	// Chroma (plane 1 = U, plane 2 = V) prepared state for the picker's UV
	// skip / color-sensitivity / encode-breakout checks. libvpx points
	// pd[1..2].pre[0] at the reference chroma planes once per ref and
	// convolves candidates into the live pd->dst chroma rect
	// (vp9_build_inter_predictors_sbp). govpx has no persistent padded
	// chroma plane, so edge-crossing tap windows keep the replicated-edge
	// staging fallback — hoisted here to one prepared decision per block.
	uvX0, uvY0   int
	uvBw, uvBh   int
	ssX, ssY     int
	uvRefW       int
	uvRefH       int
	uvValid      bool
	uvPlane      [2]vp9NonrdChromaPlane
	uvRef        [vp9dec.MaxRefFrames][2]vp9NonrdChromaRefPlane
	uvPlaneBsize common.BlockSize
}

// vp9NonrdChromaPlane is the block-level source/destination state for one
// chroma plane (index 0 = U, 1 = V).
type vp9NonrdChromaPlane struct {
	src       []byte
	srcStride int
	srcW      int
	srcH      int
	dst       []byte
	dstStride int
	valid     bool
}

// vp9NonrdChromaRefPlane is the per-(ref, chroma plane) visible reference
// plane pointer.
type vp9NonrdChromaRefPlane struct {
	ref       []byte
	refStride int
	refRows   int
	valid     bool
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
	// originX/originY locate the visible-frame origin inside the padded
	// plane; preRows caches len(pre)/preStride. The mv-pred prepass and
	// pred_mv_sad refresh reuse these instead of refetching the plane.
	originX, originY int
	preRows          int
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
		c.uvRef[r][0] = vp9NonrdChromaRefPlane{}
		c.uvRef[r][1] = vp9NonrdChromaRefPlane{}
	}
	c.initChroma(e, inter, bsize)
	return true
}

// initChroma prepares the block-level chroma source/destination state; on any
// unsupported geometry the picker's UV checks keep the legacy route.
func (c *vp9NonrdPredBlockCtx) initChroma(e *VP9Encoder,
	inter *vp9InterEncodeState, bsize common.BlockSize,
) {
	c.uvValid = false
	pdU := &e.planes[1]
	pdV := &e.planes[2]
	if pdU.SubsamplingX != pdV.SubsamplingX ||
		pdU.SubsamplingY != pdV.SubsamplingY {
		return
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pdU)
	if planeBsize >= common.BlockSizes {
		return
	}
	uvBw := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
	uvBh := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
	if uvBw <= 0 || uvBh <= 0 {
		return
	}
	ssX := int(pdU.SubsamplingX)
	ssY := int(pdU.SubsamplingY)
	uvX0 := c.x0 >> uint(ssX)
	uvY0 := c.y0 >> uint(ssY)
	for plane := 1; plane <= 2; plane++ {
		src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
		dst, dstStride := e.vp9EncoderReconPlane(plane)
		if len(src) == 0 || srcStride <= 0 || len(dst) == 0 || dstStride <= 0 {
			return
		}
		dstRows := len(dst) / dstStride
		// Hoisted legacy per-candidate gates: the variance reader
		// (vp9NonrdUVPlaneDiffVarianceSSE) requires the full block visible
		// in both source and destination, and the direct predictor requires
		// the destination window.
		if !encoder.VisibleBlockFits(uvX0, uvY0, uvBw, uvBh, srcW, srcH) ||
			!encoder.VisibleBlockFits(uvX0, uvY0, uvBw, uvBh, dstStride,
				dstRows) {
			return
		}
		c.uvPlane[plane-1] = vp9NonrdChromaPlane{
			src:       src,
			srcStride: srcStride,
			srcW:      srcW,
			srcH:      srcH,
			dst:       dst,
			dstStride: dstStride,
			valid:     true,
		}
	}
	c.uvPlaneBsize = planeBsize
	c.uvBw = uvBw
	c.uvBh = uvBh
	c.ssX = ssX
	c.ssY = ssY
	c.uvX0 = uvX0
	c.uvY0 = uvY0
	c.uvRefW = (e.opts.Width + (1 << uint(ssX)) - 1) >> uint(ssX)
	c.uvRefH = (e.opts.Height + (1 << uint(ssY)) - 1) >> uint(ssY)
	if c.uvRefW <= 0 || c.uvRefH <= 0 {
		return
	}
	c.uvValid = true
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
		originX:   originX,
		originY:   originY,
		preRows:   preRows,
		dims8: (ref.img.Width&0x7) == 0 &&
			(ref.img.Height&0x7) == 0,
		valid: true,
	}
	if c.uvValid {
		for plane := 1; plane <= 2; plane++ {
			refPlane, refStride := vp9ReferencePlane(ref, plane)
			if len(refPlane) == 0 || refStride <= 0 {
				continue
			}
			refRows := len(refPlane) / refStride
			// Hoisted legacy gates: the block origin must be inside the
			// reference plane, and the zero-MV copy window must fit (the
			// legacy direct predictor rejected both per candidate).
			if c.uvX0+c.uvBw > refStride || c.uvY0+c.uvBh > refRows {
				continue
			}
			c.uvRef[refFrame][plane-1] = vp9NonrdChromaRefPlane{
				ref:       refPlane,
				refStride: refStride,
				refRows:   refRows,
				valid:     true,
			}
		}
	}
	return true
}

// vp9NonrdCtxPredictChromaPlaneInner is the per-candidate chroma predictor
// leaf: the tail of the legacy direct chroma path
// (predictVP9InterBlockPlaneDirectTo) with every block/ref-invariant lookup
// hoisted into the prepared context. It writes the candidate's chroma
// prediction into the live recon plane rect, exactly like libvpx
// vp9_build_inter_predictors_sbp writing pd->dst.
func (e *VP9Encoder) vp9NonrdCtxPredictChromaPlaneInner(c *vp9NonrdPredBlockCtx,
	refFrame int8, plane int, mv vp9dec.MV, filter vp9dec.InterpFilter,
) bool {
	filterIdx := int(filter)
	if filterIdx < 0 || filterIdx >= int(vp9dec.InterpSwitchable) {
		return false
	}
	rp := &c.uvRef[refFrame][plane-1]
	pp := &c.uvPlane[plane-1]
	src := rp.ref
	srcStride := rp.refStride
	srcRows := rp.refRows
	dstOff := c.uvY0*pp.dstStride + c.uvX0
	if mv == (vp9dec.MV{}) && (c.uvRefW&0x7) == 0 && (c.uvRefH&0x7) == 0 {
		buffers.CopyPlane(pp.dst[dstOff:], pp.dstStride,
			src[c.uvY0*srcStride+c.uvX0:], srcStride, c.uvBw, c.uvBh)
		return true
	}
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(c.edges, mv, c.uvBw, c.uvBh,
		c.ssX, c.ssY)
	srcX := c.uvX0
	srcY := c.uvY0
	srcX16 := srcX << vp9dec.SubpelBitsConst
	srcY16 := srcY << vp9dec.SubpelBitsConst
	subpelX := int(mvQ4.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(mvQ4.Row) & (vp9dec.SubpelShifts - 1)
	srcX += int(mvQ4.Col) >> vp9dec.SubpelBitsConst
	srcY += int(mvQ4.Row) >> vp9dec.SubpelBitsConst
	srcX16 += int(mvQ4.Col)
	srcY16 += int(mvQ4.Row)

	srcOffset := srcY*srcStride + srcX
	d := &e.interPredictor
	d.unsupportedReconstruct = false
	d.interPredictScratch = e.interPredictScratch
	if mvQ4.Col != 0 || mvQ4.Row != 0 ||
		(c.uvRefW&0x7) != 0 || (c.uvRefH&0x7) != 0 {
		x1 := ((srcX16 + (c.uvBw-1)*vp9dec.SubpelShifts) >> vp9dec.SubpelBitsConst) + 1
		y1 := ((srcY16 + (c.uvBh-1)*vp9dec.SubpelShifts) >> vp9dec.SubpelBitsConst) + 1
		extX0, extY0 := srcX, srcY
		xPad, yPad := 0, 0
		if subpelX != 0 {
			extX0 -= vp9dec.VP9InterpExtend - 1
			x1 += vp9dec.VP9InterpExtend
			xPad = 1
		}
		if subpelY != 0 {
			extY0 -= vp9dec.VP9InterpExtend - 1
			y1 += vp9dec.VP9InterpExtend
			yPad = 1
		}
		if extX0 < 0 || extX0 > c.uvRefW-1 || x1 < 0 || x1 > c.uvRefW-1 ||
			extY0 < 0 || extY0 > c.uvRefH-1 || y1 < 0 || y1 > c.uvRefH-1 {
			extW := x1 - extX0 + 1
			extH := y1 - extY0 + 1
			if extW <= 0 || extH <= 0 {
				return false
			}
			borderOffset := yPad*(vp9dec.VP9InterpExtend-1)*extW +
				xPad*(vp9dec.VP9InterpExtend-1)
			src, srcStride, srcOffset = d.vp9ExtendInterPredictSource(src,
				srcStride, c.uvRefW, c.uvRefH, extX0, extY0, extW, extH,
				borderOffset)
			srcRows = extH
		}
	}
	e.interPredictScratch = d.interPredictScratch
	if srcOffset < 0 || srcOffset >= len(src) || srcRows <= 0 {
		return false
	}
	if vp9PhaseStatsEnabled {
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
	vp9dec.InterPredictorWithScratch(src, srcStride,
		pp.dst[dstOff:], pp.dstStride,
		subpelX, subpelY, tables.FilterKernels[filterIdx],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, c.uvBw, c.uvBh, 0,
		srcOffset, &d.convolveScratch)
	return true
}

// vp9NonrdCtxPredMVSAD is the prepared-context sibling of vp9NonrdPredMVSAD:
// the pred_mv_sad[LAST] refresh at vp9_pickmode.c:2284-2293 is a full-pel SAD
// read directly off the prepared pre[0] pointer
// (pre_buf = pre[0].buf + (mv.row>>3)*stride + (mv.col>>3)).
func (e *VP9Encoder) vp9NonrdCtxPredMVSAD(c *vp9NonrdPredBlockCtx,
	inter *vp9InterEncodeState, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, mv vp9dec.MV,
) (uint64, bool) {
	if c == nil || refFrame < 0 || int(refFrame) >= len(c.ref) ||
		!c.ref[refFrame].valid {
		return e.vp9NonrdPredMVSAD(inter, miRow, miCol, bsize, refFrame, mv)
	}
	rp := &c.ref[refFrame]
	// Legacy gates, hoistable but kept for exact ok-parity: fully visible
	// source window and an in-plane full-pel reference window.
	if c.x0+c.blockW > c.srcW || c.y0+c.blockH > c.srcH {
		return 0, false
	}
	refX := rp.originX + c.x0 + (int(mv.Col) >> 3)
	refY := rp.originY + c.y0 + (int(mv.Row) >> 3)
	if refX < 0 || refY < 0 || refX+c.blockW > rp.preStride ||
		refY+c.blockH > rp.preRows {
		return 0, false
	}
	return encoder.BlockSADOffsets(c.src, c.srcOff, c.srcStride,
		rp.pre, refY*rp.preStride+refX, rp.preStride,
		c.blockW, c.blockH, ^uint64(0)), true
}

// vp9NonrdCtxUVVariancePlaneSSE is the prepared-context sibling of
// vp9NonrdUVVariancePlaneSSE: build one chroma plane's candidate prediction
// and score (variance, sse) against the source
// (vp9_build_inter_predictors_sbp + fn_ptr[uv_bsize].vf,
// vp9_pickmode.c:578-599 and :2392-2400). Falls back to the legacy route for
// shapes outside the prepared envelope.
func (e *VP9Encoder) vp9NonrdCtxUVVariancePlaneSSE(c *vp9NonrdPredBlockCtx,
	inter *vp9InterEncodeState, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mode common.PredictionMode, refFrame int8,
	mv vp9dec.MV, filter vp9dec.InterpFilter, plane int,
) (variance, sse uint64, ok bool) {
	if c == nil || !c.uvValid || plane < 1 || plane > 2 ||
		refFrame < 0 || int(refFrame) >= len(c.uvRef) ||
		!c.uvRef[refFrame][plane-1].valid {
		return e.vp9NonrdUVVariancePlaneSSE(inter, miRows, miCols, miRow,
			miCol, bsize, mode, refFrame, mv, filter, plane)
	}
	if vp9PhaseStatsEnabled {
		e.vp9PhaseIncInterPredictionBlock()
	}
	if !e.vp9NonrdCtxPredictChromaPlaneInner(c, refFrame, plane, mv, filter) {
		return 0, 0, false
	}
	pp := &c.uvPlane[plane-1]
	variance, sse = encoder.BlockDiffVarianceSSE(pp.src, pp.srcStride,
		pp.dst, pp.dstStride, c.uvX0, c.uvY0, c.uvX0, c.uvY0, c.uvBw, c.uvBh)
	return variance, sse, true
}

// vp9NonrdCtxUVVarianceSSE is the prepared-context sibling of
// vp9NonrdUVVarianceSSE (both chroma planes, encode_breakout_test's UV leg,
// vp9_pickmode.c:1008-1022).
func (e *VP9Encoder) vp9NonrdCtxUVVarianceSSE(c *vp9NonrdPredBlockCtx,
	inter *vp9InterEncodeState, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mode common.PredictionMode, refFrame int8,
	mv vp9dec.MV, filter vp9dec.InterpFilter,
) (varU, sseU, varV, sseV uint64, ok bool) {
	if c == nil || !c.uvValid ||
		refFrame < 0 || int(refFrame) >= len(c.uvRef) ||
		!c.uvRef[refFrame][0].valid || !c.uvRef[refFrame][1].valid {
		return e.vp9NonrdUVVarianceSSE(inter, miRows, miCols, miRow, miCol,
			bsize, mode, refFrame, mv, filter)
	}
	if vp9PhaseStatsEnabled {
		e.vp9PhaseIncInterPredictionBlock()
	}
	if !e.vp9NonrdCtxPredictChromaPlaneInner(c, refFrame, 1, mv, filter) ||
		!e.vp9NonrdCtxPredictChromaPlaneInner(c, refFrame, 2, mv, filter) {
		return 0, 0, 0, 0, false
	}
	ppU := &c.uvPlane[0]
	varU, sseU = encoder.BlockDiffVarianceSSE(ppU.src, ppU.srcStride,
		ppU.dst, ppU.dstStride, c.uvX0, c.uvY0, c.uvX0, c.uvY0, c.uvBw, c.uvBh)
	ppV := &c.uvPlane[1]
	varV, sseV = encoder.BlockDiffVarianceSSE(ppV.src, ppV.srcStride,
		ppV.dst, ppV.dstStride, c.uvX0, c.uvY0, c.uvX0, c.uvY0, c.uvBw, c.uvBh)
	return varU, sseU, varV, sseV, true
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
