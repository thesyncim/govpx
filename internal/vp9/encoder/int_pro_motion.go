package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// Integer projection motion search is used by the realtime /
// ML_BASED_PARTITION path. This file ports the relevant libvpx v1.16.0
// helpers:
//
//   - vector_match                       (vp9/encoder/vp9_mcomp.c:2192-2257)
//   - vp9_int_pro_motion_estimation      (vp9/encoder/vp9_mcomp.c:2264-2399)
//   - vp9_set_subpel_mv_search_range     (vp9/encoder/vp9_mcomp.c:51-67)
//   - clamp_mv                           (vp9/common/vp9_mv.h:47-51)
//
// The ML_BASED_PARTITION picker uses this helper through get_estimated_pred
// before per-block mode picking. The predicted 64x64 luma buffer contributes
// to the partition neural-network input and the selected integer-projection
// MV seeds later NEWMV search.

// MvLimits mirrors libvpx's MvLimits struct
// (vp9/encoder/vp9_block.h:50-55). All values are in 1/8-pel units
// at the subpel-clamp call site and full-pel units at the diamond /
// step-search call sites — see vp9_mv.h MV_LOW / MV_UPP / MV unit
// commentary.
type MvLimits struct {
	ColMin int
	ColMax int
	RowMin int
	RowMax int
}

func EncoderMvLimits(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) MvLimits {
	miW := int(common.Num8x8BlocksWideLookup[bsize])
	miH := int(common.Num8x8BlocksHighLookup[bsize])
	return MvLimits{
		RowMin: -(((miRow + miH) * common.MiSize) + common.VP9InterpExtend),
		ColMin: -(((miCol + miW) * common.MiSize) + common.VP9InterpExtend),
		RowMax: (miRows-miRow)*common.MiSize + common.VP9InterpExtend,
		ColMax: (miCols-miCol)*common.MiSize + common.VP9InterpExtend,
	}
}

// MV mirrors libvpx's MV struct (vp9/common/vp9_mv.h). It is the same
// (row, col) int16 pair as internal/vp9/decoder.MV, so callers can pass
// decoder motion vectors without conversion.
type MV = decoder.MV

// vp9_mv.h: MV_IN_USE_BITS = 14.
//
//	MV_UPP = (1 << 14) - 1 =  16383.
//	MV_LOW = -(1 << 14)    = -16384.
const (
	mvInUseBits = 14
	mvUpp       = (1 << mvInUseBits) - 1
	mvLow       = -(1 << mvInUseBits)
)

// vp9_mcomp.h: MAX_MVSEARCH_STEPS = 11.
//
//	MAX_FULL_PEL_VAL = (1 << (MAX_MVSEARCH_STEPS - 1)) - 1 = 1023.
const (
	MaxMvSearchSteps = 11
	MaxFullPelVal    = (1 << (MaxMvSearchSteps - 1)) - 1
)

// SetSubpelMvSearchRange ports vp9_set_subpel_mv_search_range
// (vp9/encoder/vp9_mcomp.c:51-67). Intersects the |umvWindow| limits
// (in full-pel units) scaled to 1/8-pel against a [refMv ± MAX_FULL_PEL_VAL*8]
// box, then clamps the result to [MV_LOW+1, MV_UPP-1].
func SetSubpelMvSearchRange(
	out *MvLimits, umvWindow *MvLimits, refMV *MV,
) {
	out.ColMin = max(umvWindow.ColMin*8, int(refMV.Col)-MaxFullPelVal*8)
	out.ColMax = min(umvWindow.ColMax*8, int(refMV.Col)+MaxFullPelVal*8)
	out.RowMin = max(umvWindow.RowMin*8, int(refMV.Row)-MaxFullPelVal*8)
	out.RowMax = min(umvWindow.RowMax*8, int(refMV.Row)+MaxFullPelVal*8)

	out.ColMin = max(mvLow+1, out.ColMin)
	out.ColMax = min(mvUpp-1, out.ColMax)
	out.RowMin = max(mvLow+1, out.RowMin)
	out.RowMax = min(mvUpp-1, out.RowMax)
}

// ClampMV ports the clamp_mv inline (vp9/common/vp9_mv.h:47-51).
func ClampMV(mv *MV, minCol, maxCol, minRow, maxRow int) {
	c := int(mv.Col)
	if c < minCol {
		c = minCol
	} else if c > maxCol {
		c = maxCol
	}
	r := int(mv.Row)
	if r < minRow {
		r = minRow
	} else if r > maxRow {
		r = maxRow
	}
	mv.Col = int16(c)
	mv.Row = int16(r)
}

// SetFullpelSearchRange intersects limits with libvpx's full-pel search box
// around ref. The receiver limits are in full-pel units.
func (limits *MvLimits) SetFullpelSearchRange(ref decoder.MV) {
	if limits == nil {
		return
	}
	colMin := (int(ref.Col) >> 3) - MaxFullPelVal
	if int(ref.Col)&7 != 0 {
		colMin++
	}
	rowMin := (int(ref.Row) >> 3) - MaxFullPelVal
	if int(ref.Row)&7 != 0 {
		rowMin++
	}
	colMax := (int(ref.Col) >> 3) + MaxFullPelVal
	rowMax := (int(ref.Row) >> 3) + MaxFullPelVal

	colMin = max(colMin, (mvLow>>3)+1)
	rowMin = max(rowMin, (mvLow>>3)+1)
	colMax = min(colMax, (mvUpp>>3)-1)
	rowMax = min(rowMax, (mvUpp>>3)-1)

	if limits.ColMin < colMin {
		limits.ColMin = colMin
	}
	if limits.ColMax > colMax {
		limits.ColMax = colMax
	}
	if limits.RowMin < rowMin {
		limits.RowMin = rowMin
	}
	if limits.RowMax > rowMax {
		limits.RowMax = rowMax
	}
}

// InFullpelRange reports whether row/col is inside the full-pel limits.
func (limits *MvLimits) InFullpelRange(row, col int) bool {
	if limits == nil {
		return true
	}
	return col >= limits.ColMin && col <= limits.ColMax &&
		row >= limits.RowMin && row <= limits.RowMax
}

// FullpelBoundsOK reports whether a square full-pel search window fits.
func (limits *MvLimits) FullpelBoundsOK(row, col, searchRange int) bool {
	if limits == nil {
		return true
	}
	return row-searchRange >= limits.RowMin &&
		row+searchRange <= limits.RowMax &&
		col-searchRange >= limits.ColMin &&
		col+searchRange <= limits.ColMax
}

// ClampFullpel clamps row/col to the full-pel limits.
func (limits *MvLimits) ClampFullpel(row, col int) (int, int) {
	if limits == nil {
		return row, col
	}
	if row < limits.RowMin {
		row = limits.RowMin
	} else if row > limits.RowMax {
		row = limits.RowMax
	}
	if col < limits.ColMin {
		col = limits.ColMin
	} else if col > limits.ColMax {
		col = limits.ColMax
	}
	return row, col
}

// vp9_mcomp.c:2261 — search_pos[4] is the 4-point cross neighbourhood
// (top, left, right, bottom) consulted after the 1-D vector search.
var intProSearchPos = [4]MV{
	{Row: -1, Col: 0},
	{Row: 0, Col: -1},
	{Row: 0, Col: 1},
	{Row: 1, Col: 0},
}

// VectorMatch ports vector_match (vp9/encoder/vp9_mcomp.c:2192-2257).
// A 5-stage hierarchical 1-D search: starting from offset 0 it tries
// every 16th position across a (bw+1)-wide window, then narrows the
// step around the best by 16 -> 8 -> 4 -> 2 -> 1. Returns the offset
// of the best match in pixels relative to the centre (bw/2).
func VectorMatch(ref, src []int16, bwl int) int {
	bw := 4 << bwl // pixels per axis at the bsize's full-pel width.

	// libvpx: int best_sad = INT_MAX (vp9_mcomp.c:2193).
	const intMax = int(^uint(0) >> 1)
	bestSad := intMax
	center := 0
	offset := 0

	// Stage 0: 16-pel granularity across the full search window.
	for d := 0; d <= bw; d += 16 {
		thisSad := dsp.VpxVectorVar(ref[d:d+(4<<bwl)], src, bwl)
		if thisSad < bestSad {
			bestSad = thisSad
			offset = d
		}
	}
	center = offset

	// Stages 1..4: ± d around the current centre with d in {8, 4, 2, 1}.
	for _, d := range [...]int{8, 4, 2, 1} {
		for _, dd := range [...]int{-d, d} {
			thisPos := offset + dd
			if thisPos < 0 || thisPos > bw {
				continue
			}
			thisSad := dsp.VpxVectorVar(ref[thisPos:thisPos+(4<<bwl)], src, bwl)
			if thisSad < bestSad {
				bestSad = thisSad
				center = thisPos
			}
		}
		offset = center
	}

	return center - (bw >> 1)
}

// IntProSadFunc is the function signature for the per-bsize SAD
// helper that cpi->fn_ptr[bsize].sdf points to in libvpx. Caller
// supplies it so the int-pro core stays decoupled from the bsize
// dispatch table.
type IntProSadFunc func(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32

// SADForBsize returns the standard cpi->fn_ptr[bsize].sdf for one
// of the four bsizes (16x16, 16x32 / 32x16, 32x32, 32x64 / 64x32,
// 64x64) get_estimated_pred and the realtime non-rd partition picker
// consult. Only the BLOCK_*x* sizes int-pro motion search uses are
// listed (libvpx vp9/encoder/vp9_encoder.c:2687-2735).
func SADForBsize(bsize common.BlockSize) IntProSadFunc {
	switch bsize {
	case common.Block64x64:
		return dsp.VpxSad64x64
	case common.Block64x32:
		return dsp.VpxSad64x32
	case common.Block32x64:
		return dsp.VpxSad32x64
	case common.Block32x32:
		return dsp.VpxSad32x32
	case common.Block32x16:
		return dsp.VpxSad32x16
	case common.Block16x32:
		return dsp.VpxSad16x32
	case common.Block16x16:
		return dsp.VpxSad16x16
	default:
		return nil
	}
}

// IntProSadX4 mirrors cpi->fn_ptr[bsize].sdx4df — the 4-reference
// dispatch. libvpx's _x4d_c is literally four sdf calls
// (vpx_dsp/sad.c:55-74). We replicate that pattern here so callers
// don't need a separate dispatch table.
func IntProSadX4(
	sdf IntProSadFunc,
	src []uint8, srcOff, srcStride int,
	ref []uint8, refOffsets [4]int, refStride int,
	out *[4]uint32,
) {
	for i := range 4 {
		out[i] = sdf(src, srcOff, srcStride, ref, refOffsets[i], refStride)
	}
}

// IntProEstimateInput is the per-call substrate the
// vp9_int_pro_motion_estimation port needs from the caller. It
// mirrors libvpx's MACROBLOCK / MACROBLOCKD reach into
// x->plane[0].src and xd->plane[0].pre[0] — pure pixel buffers plus
// strides — together with the caller-owned full-pel MV limits.
//
// The result lands in OutMV (libvpx writes xd->mi[0]->mv[0]) and the
// best-SAD return value mirrors the libvpx return.
type IntProEstimateInput struct {
	// Bsize is the per-SB sub-block being searched. Must be one of
	// BLOCK_16x16, 32x16, 16x32, 32x32, 64x32, 32x64, 64x64.
	Bsize common.BlockSize

	// Src is the 64x64 source-luma window starting at the current
	// SB origin, indexed at (mi_row, mi_col). SrcStride is the row
	// pitch of the underlying frame buffer.
	Src       []uint8
	SrcOff    int
	SrcStride int

	// Ref is the 64x64 reference-luma window in the chosen ref
	// frame (LAST / GOLDEN / ALTREF). RefStride is the row pitch of
	// the underlying frame buffer. RefOff points at the (mi_row,
	// mi_col)-aligned origin within the reference buffer.
	Ref       []uint8
	RefOff    int
	RefStride int

	// RefMV is the reference MV in 1/8-pel units used by the subpel
	// clamp (libvpx ref_mv argument).
	RefMV MV

	// MvLimits are the SB-level full-pel UMV-window limits libvpx
	// caches on the MACROBLOCK. Subpel clamp scales them by 8 and
	// intersects with the (MV_LOW+1, MV_UPP-1) box.
	MvLimits MvLimits
}

// IntProEstimate ports vp9_int_pro_motion_estimation
// (vp9/encoder/vp9_mcomp.c:2264-2399) — only the 8-bit
// non-highbd path. Returns (bestSad, mv) where mv is in 1/8-pel
// units after clamping.
//
// The libvpx control flow:
//  1. Project the reference and source onto 1-D horizontal / vertical
//     vectors via vpx_int_pro_row / vpx_int_pro_col.
//  2. Run vector_match independently on each axis -> coarse MV.
//  3. Refine with a 4-direction cross-step SAD around the coarse MV.
//  4. Run one ±1 fallback (the "this_mv +/-1" step) for the second
//     refinement pass; commit if it beats the coarse-cross winner.
//  5. Convert the result from full-pel to 1/8-pel (multiply by 8) and
//     clamp to the subpel MV window.
//
// The scaled-ref-frame swap (libvpx vp9_mcomp.c:2287-2294) is not
// modeled here because the int-pro path runs on the unscaled buffers
// — the caller is responsible for picking the right reference frame
// (LAST / GOLDEN / ALTREF) and supplying its pre-plane buffer.
func IntProEstimate(in *IntProEstimateInput) (bestSad uint32, mv MV) {
	bsize := in.Bsize
	bw := 4 << uint(common.BWidthLog2Lookup[bsize])
	bh := 4 << uint(common.BHeightLog2Lookup[bsize])
	searchWidth := bw << 1
	searchHeight := bh << 1
	srcStride := in.SrcStride
	refStride := in.RefStride
	normFactor := 3 + (bw >> 5)

	sdf := SADForBsize(bsize)

	// Scratch buffers — libvpx declares these on the stack at 16-byte
	// alignment via DECLARE_ALIGNED. We use plain slices.
	var hbuf [128]int16
	var vbuf [128]int16
	var srcHbuf [64]int16
	var srcVbuf [64]int16

	// Set up the prediction 1-D reference set across the horizontal
	// axis. libvpx walks 16 columns at a time:
	//   ref_buf = pre[0].buf - (bw >> 1);
	//   for idx in 0..search_width step 16:
	//     vpx_int_pro_row(&hbuf[idx], ref_buf, ref_stride, bh);
	//     ref_buf += 16;
	refOffH := in.RefOff - (bw >> 1)
	dsp.IntProRowStrips(hbuf[:], in.Ref, refOffH, refStride, bh, searchWidth>>4)

	// Vertical 1-D reference set. libvpx:
	//   ref_buf = pre[0].buf - (bh >> 1) * ref_stride;
	//   for idx in 0..search_height:
	//     vbuf[idx] = vpx_int_pro_col(ref_buf, bw) >> norm_factor;
	//     ref_buf += ref_stride;
	refOffV := in.RefOff - (bh>>1)*refStride
	dsp.IntProCols(vbuf[:], in.Ref, refOffV, refStride, bw, searchHeight, normFactor)

	// Set up src 1-D reference set across both axes.
	dsp.IntProRowStrips(srcHbuf[:], in.Src, in.SrcOff, srcStride, bh, bw>>4)
	dsp.IntProCols(srcVbuf[:], in.Src, in.SrcOff, srcStride, bw, bh, normFactor)

	// Find the best match per 1-D search.
	colMatch := VectorMatch(hbuf[:], srcHbuf[:], int(common.BWidthLog2Lookup[bsize]))
	rowMatch := VectorMatch(vbuf[:], srcVbuf[:], int(common.BHeightLog2Lookup[bsize]))
	tmpMV := MV{Row: int16(rowMatch), Col: int16(colMatch)}

	// Coarse-MV SAD probe + 4-direction cross step.
	thisMV := tmpMV
	refOffBest := in.RefOff + int(thisMV.Row)*refStride + int(thisMV.Col)
	bestSad = sdf(in.Src, in.SrcOff, srcStride, in.Ref, refOffBest, refStride)

	var thisSAD [4]uint32
	refOffsets := [4]int{
		refOffBest - refStride,
		refOffBest - 1,
		refOffBest + 1,
		refOffBest + refStride,
	}
	IntProSadX4(sdf, in.Src, in.SrcOff, srcStride, in.Ref, refOffsets, refStride, &thisSAD)

	for idx := range 4 {
		if thisSAD[idx] < bestSad {
			bestSad = thisSAD[idx]
			tmpMV.Row = intProSearchPos[idx].Row + thisMV.Row
			tmpMV.Col = intProSearchPos[idx].Col + thisMV.Col
		}
	}

	// ±1 fallback refinement: libvpx pushes thisMV one cell along
	// the axis that lost the cross probe (top vs bottom, left vs
	// right) and commits if the new probe beats best_sad.
	if thisSAD[0] < thisSAD[3] {
		thisMV.Row -= 1
	} else {
		thisMV.Row += 1
	}
	if thisSAD[1] < thisSAD[2] {
		thisMV.Col -= 1
	} else {
		thisMV.Col += 1
	}

	refOffProbe := in.RefOff + int(thisMV.Row)*refStride + int(thisMV.Col)
	tmpSad := sdf(in.Src, in.SrcOff, srcStride, in.Ref, refOffProbe, refStride)
	if bestSad > tmpSad {
		tmpMV = thisMV
		bestSad = tmpSad
	}

	// Convert from full-pel to 1/8-pel.
	tmpMV.Row *= 8
	tmpMV.Col *= 8

	// Subpel clamp against the (MV_LOW+1, MV_UPP-1) window
	// intersected with refMV ± MAX_FULL_PEL_VAL*8.
	var subpelLimits MvLimits
	SetSubpelMvSearchRange(&subpelLimits, &in.MvLimits, &in.RefMV)
	ClampMV(&tmpMV, subpelLimits.ColMin, subpelLimits.ColMax, subpelLimits.RowMin, subpelLimits.RowMax)

	return bestSad, tmpMV
}
