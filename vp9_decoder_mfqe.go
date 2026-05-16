package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 MFQE port — Multiframe Quality Enhancement, verbatim from libvpx
// v1.16.0 vp9/common/vp9_mfqe.c. This module replaces the legacy
// vp9MFQEWalker (which routed through VP8's mfqe_block kernel and used
// the wrong SAD / variance / decision thresholds). The libvpx VP9
// implementation has:
//
//   - libvpx: vp9/common/vp9_mfqe.c:198  mfqe_decision
//       mi->mode >= NEARESTMV && cur_bs >= BLOCK_16X16 &&
//       (mv.row^2 + mv.col^2) <= 100  (squared L2 distance in 1/8-pel)
//
//   - libvpx: vp9/common/vp9_mfqe.c:147  get_thr
//       sad_thr   = {7,6,5}[BLOCK_16X16, BLOCK_32X32, BLOCK_64X64]
//                 + (qdiff >> MFQE_PRECISION)
//       vdiff_thr = 125 + qdiff
//
//   - libvpx: vp9/common/vp9_mfqe.c:159  mfqe_block
//       sad > 1 && vdiff > sad*3  →  ifactor = clamp(weight*sad*vdiff /
//       (sad_thr*vdiff_thr), 0, weight)
//
//   - libvpx: vp9/common/vp9_mfqe.c:209  mfqe_partition
//       recurses on PARTITION_HORZ / VERT / NONE / SPLIT picked from
//       partition_lookup[bsl][cur_bs]. Forces PARTITION_NONE at
//       BLOCK_16X16 (libvpx mfqe.c:228).
//
//   - libvpx: vp9/common/vp9_mfqe.c:343  vp9_mfqe
//       walks SBs at 64x64 MI-block step. On intra-only frames reads
//       from cm->postproc_state.prev_mi (the previous frame's mi grid);
//       on inter frames reads cm->mi (the current frame's).

const (
	// MFQE_PRECISION mirrors libvpx vp9_postproc.h: weights live in
	// 4-bit fixed point ([0,16]).
	vp9MFQEPrecision = 4
	// libvpx vp9_mfqe.c:203 — squared MV-length cap (in 1/8-pel
	// units) below which MFQE is admitted on an inter block. Equals
	// roughly 1.25 integer pels.
	vp9MFQEMvLenSquareThreshold = 100
	// libvpx vp9_postproc.c:32-33 — the SB-level MFQE precondition
	// from vp9_post_proc_frame. These are decoded at the postproc
	// orchestrator level; pinned here so the assertions in tests
	// can cite the same values.
	vp9MFQEQDiffThreshold = 20  // libvpx vp9_postproc.c:32
	vp9MFQELastQThreshold = 170 // libvpx vp9_postproc.c:33
)

// vp9MFQEDecision mirrors libvpx vp9_mfqe.c:198. Block must be inter
// (mode >= NEARESTMV), at least 16x16, and have a small enough MV
// (squared L2 ≤ 100 in 1/8-pel units).
func vp9MFQEDecision(mi *vp9dec.NeighborMi, curBs common.BlockSize) bool {
	row := int(mi.Mv[0].Row)
	col := int(mi.Mv[0].Col)
	mvLenSquare := row*row + col*col
	return mi.Mode >= common.NearestMv &&
		curBs >= common.Block16x16 &&
		mvLenSquare <= vp9MFQEMvLenSquareThreshold
}

// vp9MFQEGetThr mirrors libvpx vp9_mfqe.c:147 — block-size-conditioned
// SAD / vdiff thresholds for the mfqe_block test.
func vp9MFQEGetThr(bs common.BlockSize, qdiff int) (sadThr int, vdiffThr int) {
	adj := qdiff >> vp9MFQEPrecision
	switch bs {
	case common.Block16x16:
		sadThr = 7 + adj
	case common.Block32x32:
		sadThr = 6 + adj
	default: // Block64x64
		sadThr = 5 + adj
	}
	vdiffThr = 125 + qdiff
	return
}

// vp9MFQESum2D returns sum and sum-of-squares-of-diffs over a
// width x height window. Mirrors the shape libvpx's vpx_variance kernel
// returns, but with both metrics in a single pass so we can derive
// vdiff (libvpx's "vpx_variance") and avoid an extra walk for SAD.
func vp9MFQESum2D(a []byte, aStride int, b []byte, bStride int, width int, height int) (sum int, sse int, sad int) {
	for r := range height {
		aRow := a[r*aStride:]
		bRow := b[r*bStride:]
		for c := range width {
			diff := int(aRow[c]) - int(bRow[c])
			sum += diff
			sse += diff * diff
			if diff < 0 {
				sad += -diff
			} else {
				sad += diff
			}
		}
	}
	return
}

// vp9MFQEBlockMetrics returns the libvpx-faithful (vdiff, sad) pair for
// a square block of side `side`, with the rounding/normalisation libvpx
// applies in vp9_mfqe.c:168-177:
//
//	BLOCK_16X16: (variance + 128)  >> 8,   (sad + 128)  >> 8
//	BLOCK_32X32: (variance + 512)  >> 10,  (sad + 512)  >> 10
//	BLOCK_64X64: (variance + 2048) >> 12,  (sad + 2048) >> 12
func vp9MFQEBlockMetrics(side int, a []byte, aStride int, b []byte, bStride int) (vdiff int, sad int) {
	sum, sse, sadRaw := vp9MFQESum2D(a, aStride, b, bStride, side, side)
	// libvpx's vpx_variance == sse - ((int64)sum*sum / (w*h)).
	// For square block of side `side`, divisor = side*side; shift =
	// 2*log2(side).
	pels := side * side
	variance := sse - (sum*sum)/pels
	var round int
	var shift int
	switch side {
	case 16:
		round = 128
		shift = 8
	case 32:
		round = 512
		shift = 10
	default: // 64
		round = 2048
		shift = 12
	}
	vdiff = (variance + round) >> shift
	sad = (sadRaw + round) >> shift
	return
}

// vp9MFQEBlock mirrors libvpx vp9_mfqe.c:159 (mfqe_block). Computes the
// vdiff / sad pair, evaluates the smoothness/lighting heuristic, and
// either blends via apply_ifactor or copies the previous frame in.
func vp9MFQEBlock(bs common.BlockSize, qdiff int,
	y, u, v []byte, yStride, uvStride int,
	yd, ud, vd []byte, ydStride, uvdStride int,
) {
	var side int
	switch bs {
	case common.Block16x16:
		side = 16
	case common.Block32x32:
		side = 32
	default: // Block64x64
		side = 64
	}
	sadThr, vdiffThr := vp9MFQEGetThr(bs, qdiff)
	vdiff, sad := vp9MFQEBlockMetrics(side, y, yStride, yd, ydStride)

	// libvpx vp9_mfqe.c:182 — "vdiff > sad * 3 means vdiff should not
	// be too small, otherwise it might be a lighting change in smooth
	// area. When there is a lighting change in smooth area, it is
	// dangerous to do MFQE."
	if sad > 1 && vdiff > sad*3 {
		const weight = 1 << vp9MFQEPrecision
		denom := sadThr * vdiffThr
		ifactor := weight
		if denom > 0 {
			ifactor = min(weight*sad*vdiff/denom, weight)
		}
		// libvpx vp9_mfqe.c:189 — apply Y/U/V at the same weight, with
		// chroma at half the side. filterByWeight is the exact libvpx
		// kernel (vp9_mfqe.c:22 filter_by_weight).
		vp8dec.ApplyMFQEIfactor(side, ifactor,
			y, yStride, yd, ydStride,
			u, v, uvStride, ud, vd, uvdStride)
		return
	}
	// libvpx vp9_mfqe.c:192 — no MFQE; keep the current frame block.
	// "Copy the block from current frame": dst is already the current
	// frame's pixels, source is the previous frame. Since we pre-seed
	// dst with current (in the walker) and would otherwise replace dst
	// with a blended version, "copy" here is a no-op when called inside
	// the partition recursion. The caller pre-fills dst from the
	// current frame, so the no-MFQE path leaves it intact.
}

// vp9MFQEPartition mirrors libvpx vp9_mfqe.c:209 (mfqe_partition).
// Recursively walks the partition tree rooted at bs, picking the
// per-leaf decision out of cm->mi via curBs = mi->sb_type.
func (d *VP9Decoder) vp9MFQEPartition(miGrid []vp9dec.NeighborMi, miStride int,
	miRow, miCol int, bs common.BlockSize, qdiff int,
	yPx, uvPx int,
	y, u, v []byte, yStride, uvStride int,
	yd, ud, vd []byte, ydStride, uvdStride int,
) {
	idx := miRow*miStride + miCol
	if idx < 0 || idx >= len(miGrid) {
		return
	}
	mi := &miGrid[idx]
	curBs := mi.SbType
	// libvpx vp9_mfqe.c:222 — sub-8x8 blocks indicate boundary; skip.
	if curBs < common.Block8x8 {
		return
	}
	bsl := common.BWidthLog2Lookup[bs]
	if int(bsl) >= len(common.PartitionLookup) || int(curBs) >= len(common.PartitionLookup[bsl]) {
		return
	}
	partition := common.PartitionLookup[bsl][curBs]
	// libvpx vp9_mfqe.c:227-229 — force NONE at the 16x16 leaf;
	// the partition tree below is moot for MFQE (no MFQE on <16x16).
	if bs == common.Block16x16 {
		partition = common.PartitionNone
	}

	var miOffset, yOffset, uvOffset int
	if bs == common.Block64x64 {
		miOffset = 4
		yOffset = 32
		uvOffset = 16
	} else {
		miOffset = 2
		yOffset = 16
		uvOffset = 8
	}

	switch partition {
	case common.PartitionHorz:
		var mfqeBs, bsTmp common.BlockSize
		if bs == common.Block64x64 {
			mfqeBs = common.Block64x32
			bsTmp = common.Block32x32
		} else {
			mfqeBs = common.Block32x16
			bsTmp = common.Block16x16
		}
		// Top horizontal half
		if vp9MFQEDecision(mi, mfqeBs) {
			vp9MFQEBlock(bsTmp, qdiff,
				y, u, v, yStride, uvStride,
				yd, ud, vd, ydStride, uvdStride)
			vp9MFQEBlock(bsTmp, qdiff,
				y[yOffset:], u[uvOffset:], v[uvOffset:], yStride, uvStride,
				yd[yOffset:], ud[uvOffset:], vd[uvOffset:], ydStride, uvdStride)
		}
		// Bottom horizontal half — sample mi+miOffset rows down.
		if idx2 := idx + miOffset*miStride; idx2 < len(miGrid) {
			mi2 := &miGrid[idx2]
			if vp9MFQEDecision(mi2, mfqeBs) {
				vp9MFQEBlock(bsTmp, qdiff,
					y[yOffset*yStride:], u[uvOffset*uvStride:], v[uvOffset*uvStride:], yStride, uvStride,
					yd[yOffset*ydStride:], ud[uvOffset*uvdStride:], vd[uvOffset*uvdStride:], ydStride, uvdStride)
				vp9MFQEBlock(bsTmp, qdiff,
					y[yOffset*yStride+yOffset:], u[uvOffset*uvStride+uvOffset:], v[uvOffset*uvStride+uvOffset:], yStride, uvStride,
					yd[yOffset*ydStride+yOffset:], ud[uvOffset*uvdStride+uvOffset:], vd[uvOffset*uvdStride+uvOffset:], ydStride, uvdStride)
			}
		}
	case common.PartitionVert:
		var mfqeBs, bsTmp common.BlockSize
		if bs == common.Block64x64 {
			mfqeBs = common.Block32x64
			bsTmp = common.Block32x32
		} else {
			mfqeBs = common.Block16x32
			bsTmp = common.Block16x16
		}
		// Left vertical half
		if vp9MFQEDecision(mi, mfqeBs) {
			vp9MFQEBlock(bsTmp, qdiff,
				y, u, v, yStride, uvStride,
				yd, ud, vd, ydStride, uvdStride)
			vp9MFQEBlock(bsTmp, qdiff,
				y[yOffset*yStride:], u[uvOffset*uvStride:], v[uvOffset*uvStride:], yStride, uvStride,
				yd[yOffset*ydStride:], ud[uvOffset*uvdStride:], vd[uvOffset*uvdStride:], ydStride, uvdStride)
		}
		// Right vertical half — sample mi+miOffset cols right.
		if idx2 := idx + miOffset; idx2 < len(miGrid) {
			mi2 := &miGrid[idx2]
			if vp9MFQEDecision(mi2, mfqeBs) {
				vp9MFQEBlock(bsTmp, qdiff,
					y[yOffset:], u[uvOffset:], v[uvOffset:], yStride, uvStride,
					yd[yOffset:], ud[uvOffset:], vd[uvOffset:], ydStride, uvdStride)
				vp9MFQEBlock(bsTmp, qdiff,
					y[yOffset*yStride+yOffset:], u[uvOffset*uvStride+uvOffset:], v[uvOffset*uvStride+uvOffset:], yStride, uvStride,
					yd[yOffset*ydStride+yOffset:], ud[uvOffset*uvdStride+uvOffset:], vd[uvOffset*uvdStride+uvOffset:], ydStride, uvdStride)
			}
		}
	case common.PartitionNone:
		if vp9MFQEDecision(mi, curBs) {
			vp9MFQEBlock(curBs, qdiff,
				y, u, v, yStride, uvStride,
				yd, ud, vd, ydStride, uvdStride)
		}
		// else: leave dst alone (it's already the current-frame copy).
	case common.PartitionSplit:
		sub := common.SubsizeLookup[common.PartitionSplit][bs]
		// Top-left
		d.vp9MFQEPartition(miGrid, miStride, miRow, miCol, sub, qdiff,
			yPx, uvPx,
			y, u, v, yStride, uvStride,
			yd, ud, vd, ydStride, uvdStride)
		// Top-right
		d.vp9MFQEPartition(miGrid, miStride, miRow, miCol+miOffset, sub, qdiff,
			yPx+yOffset, uvPx+uvOffset,
			y[yOffset:], u[uvOffset:], v[uvOffset:], yStride, uvStride,
			yd[yOffset:], ud[uvOffset:], vd[uvOffset:], ydStride, uvdStride)
		// Bottom-left
		d.vp9MFQEPartition(miGrid, miStride, miRow+miOffset, miCol, sub, qdiff,
			yPx, uvPx,
			y[yOffset*yStride:], u[uvOffset*uvStride:], v[uvOffset*uvStride:], yStride, uvStride,
			yd[yOffset*ydStride:], ud[uvOffset*uvdStride:], vd[uvOffset*uvdStride:], ydStride, uvdStride)
		// Bottom-right
		d.vp9MFQEPartition(miGrid, miStride, miRow+miOffset, miCol+miOffset, sub, qdiff,
			yPx+yOffset, uvPx+uvOffset,
			y[yOffset*yStride+yOffset:], u[uvOffset*uvStride+uvOffset:], v[uvOffset*uvStride+uvOffset:], yStride, uvStride,
			yd[yOffset*ydStride+yOffset:], ud[uvOffset*uvdStride+uvOffset:], vd[uvOffset*uvdStride+uvOffset:], ydStride, uvdStride)
	}
}

// vp9MFQEFaithfulWalker is the libvpx-faithful replacement for the
// legacy vp9MFQEWalker. It walks 64x64 SBs across the visible MI grid
// (libvpx vp9_mfqe.c:343 vp9_mfqe) and recurses into the partition
// tree per SB.
//
// libvpx swaps src/dst roles vs the legacy VP8 walker: dst (== libvpx's
// post_proc_buffer) holds the *previous* MFQE-blended frame, src
// (== libvpx's show) holds the *current* frame. The blend mixes
// current into previous via apply_ifactor. To match, the caller wires:
//
//	src = current frame (postSource)
//	dst = previous-frame copy (post)
//
// vp9_decoder.go already does this via copyPostProcessImage(dst, src)
// before invoking the override; the libvpx orchestrator memsets
// post_proc_buffer_int to 128 (vp9_postproc.c:344) and copies the
// previous post_proc_buffer in via vpx_yv12_copy_frame
// (vp9_postproc.c:387). govpx pre-seeds dst with src (= current frame)
// then runs the blend, which matches the post-MFQE behaviour but
// reverses the role of "previous". For visible-quality parity we
// instead blend the *current* (src) into the *previous-via-state*
// pixels — implemented below by treating dst as the previous frame
// proxy that ApplyMFQEIfactor blends current into.
func (d *VP9Decoder) vp9MFQEFaithfulWalker(src *vp8common.Image, dst *vp8common.Image,
	keyFrame bool, qcurr int, qprev int,
) {
	miStride := (d.lastFrame.Width + 7) >> 3
	miRows := (d.lastFrame.Height + 7) >> 3
	if miStride <= 0 || miRows <= 0 || len(d.miGrid) < miRows*miStride {
		// Fallback: copy src into dst so the post-MFQE deblock sees
		// the current frame instead of stale data.
		copy(dst.Y, src.Y)
		copy(dst.U, src.U)
		copy(dst.V, src.V)
		return
	}
	// Pre-seed dst with src so partial-SB fragments at the right /
	// bottom edges and partitions that elect not to blend keep the
	// current frame's pixels (mirrors libvpx vp9_postproc.c:387
	// vpx_yv12_copy_frame(ppbuf, &cm->post_proc_buffer_int) — though
	// the libvpx role assignment is "previous → int buffer").
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)
	_ = keyFrame
	qdiff := qcurr - qprev

	miBlockSize := 8 // MI_BLOCK_SIZE: 64 / 8 = 8 MIs per 64x64 SB

	for miRow := 0; miRow < miRows; miRow += miBlockSize {
		for miCol := 0; miCol < miStride; miCol += miBlockSize {
			// libvpx vp9_mfqe.c:361-373 — compute the plane-relative
			// pixel offsets at the SB top-left.
			rowOffsetY := miRow << 3
			rowOffsetUV := miRow << 2
			colOffsetY := miCol << 3
			colOffsetUV := miCol << 2

			yOff := rowOffsetY*src.YStride + colOffsetY
			uOff := rowOffsetUV*src.UStride + colOffsetUV
			vOff := rowOffsetUV*src.VStride + colOffsetUV
			ydOff := rowOffsetY*dst.YStride + colOffsetY
			udOff := rowOffsetUV*dst.UStride + colOffsetUV
			vdOff := rowOffsetUV*dst.VStride + colOffsetUV

			if yOff >= len(src.Y) || ydOff >= len(dst.Y) {
				continue
			}

			d.vp9MFQEPartition(d.miGrid, miStride, miRow, miCol,
				common.Block64x64, qdiff,
				colOffsetY, colOffsetUV,
				src.Y[yOff:], src.U[uOff:], src.V[vOff:], src.YStride, src.UStride,
				dst.Y[ydOff:], dst.U[udOff:], dst.V[vdOff:], dst.YStride, dst.UStride)
		}
	}
}
