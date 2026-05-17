package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// vp9_int_pro_motion.go ports the integer-projection motion-search
// helper used by the realtime / ML_BASED_PARTITION path. Verbatim port
// of libvpx v1.16.0:
//
//   - vector_match                       (vp9/encoder/vp9_mcomp.c:2192-2257)
//   - vp9_int_pro_motion_estimation      (vp9/encoder/vp9_mcomp.c:2264-2399)
//   - vp9_set_subpel_mv_search_range     (vp9/encoder/vp9_mcomp.c:51-67)
//   - clamp_mv                           (vp9/common/vp9_mv.h:47-51)
//
// Phase C (vp9_nonrd_pick_partition.go) wires this helper into
// pickVP9InterPartitionBlockSize through vp9MLPickPartitionEntry +
// vp9GetEstimatedPred for the ML_BASED_PARTITION dispatch (libvpx
// vp9/encoder/vp9_encodeframe.c:5313-5321). With Phase C landed the
// no-alt-ref lookahead byte-parity oracle
// (TestVP9EncoderVpxencOracleLookaheadNoAltRefScoreboard) now matches
// libvpx 4/4 packets — the previous EIGHTTAP_SMOOTH vs EIGHTTAP
// interp-filter-literal drift at uncompressed-header byte 4 closed
// once the recursive ML picker began contributing the correct per-block
// filter histogram that drives fix_interp_filter's SWITCHABLE -> concrete
// demotion (libvpx vp9_bitstream.c:864-885).

// vp9MvLimits mirrors libvpx's MvLimits struct
// (vp9/encoder/vp9_block.h:50-55). All values are in 1/8-pel units
// at the subpel-clamp call site and full-pel units at the diamond /
// step-search call sites — see vp9_mv.h MV_LOW / MV_UPP / MV unit
// commentary.
type vp9MvLimits struct {
	ColMin int
	ColMax int
	RowMin int
	RowMax int
}

// vp9MV mirrors libvpx's MV struct (vp9/common/vp9_mv.h). Same
// (row, col) int16 pair as internal/vp9/decoder.MV; aliased here so
// the encoder-side helpers don't import the decoder package.
type vp9MV = decoder.MV

// vp9_mv.h: MV_IN_USE_BITS = 14.
//
//	MV_UPP = (1 << 14) - 1 =  16383.
//	MV_LOW = -(1 << 14)    = -16384.
const (
	vp9MvInUseBits = 14
	vp9MvUpp       = (1 << vp9MvInUseBits) - 1
	vp9MvLow       = -(1 << vp9MvInUseBits)
)

// vp9_mcomp.h: MAX_MVSEARCH_STEPS = 11.
//
//	MAX_FULL_PEL_VAL = (1 << (MAX_MVSEARCH_STEPS - 1)) - 1 = 1023.
const (
	vp9MaxMvSearchSteps = 11
	vp9MaxFullPelVal    = (1 << (vp9MaxMvSearchSteps - 1)) - 1
)

// vp9SetSubpelMvSearchRange ports vp9_set_subpel_mv_search_range
// (vp9/encoder/vp9_mcomp.c:51-67). Intersects the |umvWindow| limits
// (in full-pel units) scaled to 1/8-pel against a [refMv ± MAX_FULL_PEL_VAL*8]
// box, then clamps the result to [MV_LOW+1, MV_UPP-1].
func vp9SetSubpelMvSearchRange(
	out *vp9MvLimits, umvWindow *vp9MvLimits, refMV *vp9MV,
) {
	out.ColMin = max(umvWindow.ColMin*8, int(refMV.Col)-vp9MaxFullPelVal*8)
	out.ColMax = min(umvWindow.ColMax*8, int(refMV.Col)+vp9MaxFullPelVal*8)
	out.RowMin = max(umvWindow.RowMin*8, int(refMV.Row)-vp9MaxFullPelVal*8)
	out.RowMax = min(umvWindow.RowMax*8, int(refMV.Row)+vp9MaxFullPelVal*8)

	out.ColMin = max(vp9MvLow+1, out.ColMin)
	out.ColMax = min(vp9MvUpp-1, out.ColMax)
	out.RowMin = max(vp9MvLow+1, out.RowMin)
	out.RowMax = min(vp9MvUpp-1, out.RowMax)
}

// vp9ClampMV ports the clamp_mv inline (vp9/common/vp9_mv.h:47-51).
func vp9ClampMV(mv *vp9MV, minCol, maxCol, minRow, maxRow int) {
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

// vp9_mcomp.c:2261 — search_pos[4] is the 4-point cross neighbourhood
// (top, left, right, bottom) consulted after the 1-D vector search.
var vp9IntProSearchPos = [4]vp9MV{
	{Row: -1, Col: 0},
	{Row: 0, Col: -1},
	{Row: 0, Col: 1},
	{Row: 1, Col: 0},
}

// vp9VectorMatch ports vector_match (vp9/encoder/vp9_mcomp.c:2192-2257).
// A 5-stage hierarchical 1-D search: starting from offset 0 it tries
// every 16th position across a (bw+1)-wide window, then narrows the
// step around the best by 16 -> 8 -> 4 -> 2 -> 1. Returns the offset
// of the best match in pixels relative to the centre (bw/2).
func vp9VectorMatch(ref, src []int16, bwl int) int {
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

// vp9IntProSadFunc is the function signature for the per-bsize SAD
// helper that cpi->fn_ptr[bsize].sdf points to in libvpx. Caller
// supplies it so the int-pro core stays decoupled from the bsize
// dispatch table.
type vp9IntProSadFunc func(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32

// vp9SADForBsize returns the standard cpi->fn_ptr[bsize].sdf for one
// of the four bsizes (16x16, 16x32 / 32x16, 32x32, 32x64 / 64x32,
// 64x64) get_estimated_pred and the realtime non-rd partition picker
// consult. Only the BLOCK_*x* sizes int-pro motion search uses are
// listed (libvpx vp9/encoder/vp9_encoder.c:2687-2735).
func vp9SADForBsize(bsize common.BlockSize) vp9IntProSadFunc {
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

// vp9IntProSadX4 mirrors cpi->fn_ptr[bsize].sdx4df — the 4-reference
// dispatch. libvpx's _x4d_c is literally four sdf calls
// (vpx_dsp/sad.c:55-74). We replicate that pattern here so callers
// don't need a separate dispatch table.
func vp9IntProSadX4(
	sdf vp9IntProSadFunc,
	src []uint8, srcOff, srcStride int,
	ref []uint8, refOffsets [4]int, refStride int,
	out *[4]uint32,
) {
	for i := range 4 {
		out[i] = sdf(src, srcOff, srcStride, ref, refOffsets[i], refStride)
	}
}

// vp9IntProEstimateInput is the per-call substrate the
// vp9_int_pro_motion_estimation port needs from the caller. It
// mirrors libvpx's MACROBLOCK / MACROBLOCKD reach into
// x->plane[0].src and xd->plane[0].pre[0] — pure pixel buffers plus
// strides — together with the caller-owned full-pel MV limits.
//
// The result lands in OutMV (libvpx writes xd->mi[0]->mv[0]) and the
// best-SAD return value mirrors the libvpx return.
type vp9IntProEstimateInput struct {
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
	RefMV vp9MV

	// MvLimits are the SB-level full-pel UMV-window limits libvpx
	// caches on the MACROBLOCK. Subpel clamp scales them by 8 and
	// intersects with the (MV_LOW+1, MV_UPP-1) box.
	MvLimits vp9MvLimits
}

// vp9IntProEstimate ports vp9_int_pro_motion_estimation
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
func vp9IntProEstimate(in *vp9IntProEstimateInput) (bestSad uint32, mv vp9MV) {
	bsize := in.Bsize
	bw := 4 << uint(common.BWidthLog2Lookup[bsize])
	bh := 4 << uint(common.BHeightLog2Lookup[bsize])
	searchWidth := bw << 1
	searchHeight := bh << 1
	srcStride := in.SrcStride
	refStride := in.RefStride
	normFactor := 3 + (bw >> 5)

	sdf := vp9SADForBsize(bsize)

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
	for idx := 0; idx < searchWidth; idx += 16 {
		dsp.VpxIntProRow(hbuf[idx:idx+16], in.Ref, refOffH, refStride, bh)
		refOffH += 16
	}

	// Vertical 1-D reference set. libvpx:
	//   ref_buf = pre[0].buf - (bh >> 1) * ref_stride;
	//   for idx in 0..search_height:
	//     vbuf[idx] = vpx_int_pro_col(ref_buf, bw) >> norm_factor;
	//     ref_buf += ref_stride;
	refOffV := in.RefOff - (bh>>1)*refStride
	for idx := range searchHeight {
		vbuf[idx] = dsp.VpxIntProCol(in.Ref, refOffV, bw) >> uint(normFactor)
		refOffV += refStride
	}

	// Set up src 1-D reference set across both axes.
	for idx := 0; idx < bw; idx += 16 {
		dsp.VpxIntProRow(srcHbuf[idx:idx+16], in.Src, in.SrcOff+idx, srcStride, bh)
	}
	srcOffV := in.SrcOff
	for idx := range bh {
		srcVbuf[idx] = dsp.VpxIntProCol(in.Src, srcOffV, bw) >> uint(normFactor)
		srcOffV += srcStride
	}

	// Find the best match per 1-D search.
	colMatch := vp9VectorMatch(hbuf[:], srcHbuf[:], int(common.BWidthLog2Lookup[bsize]))
	rowMatch := vp9VectorMatch(vbuf[:], srcVbuf[:], int(common.BHeightLog2Lookup[bsize]))
	tmpMV := vp9MV{Row: int16(rowMatch), Col: int16(colMatch)}

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
	vp9IntProSadX4(sdf, in.Src, in.SrcOff, srcStride, in.Ref, refOffsets, refStride, &thisSAD)

	for idx := range 4 {
		if thisSAD[idx] < bestSad {
			bestSad = thisSAD[idx]
			tmpMV.Row = vp9IntProSearchPos[idx].Row + thisMV.Row
			tmpMV.Col = vp9IntProSearchPos[idx].Col + thisMV.Col
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
	var subpelLimits vp9MvLimits
	vp9SetSubpelMvSearchRange(&subpelLimits, &in.MvLimits, &in.RefMV)
	vp9ClampMV(&tmpMV, subpelLimits.ColMin, subpelLimits.ColMax, subpelLimits.RowMin, subpelLimits.RowMax)

	return bestSad, tmpMV
}
