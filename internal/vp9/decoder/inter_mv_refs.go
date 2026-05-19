package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// MvRef stores the previous/current-frame reference and MV pair tracked by
// libvpx's MODE_INFO grid for find_mv_refs_idx.
type MvRef struct {
	RefFrame [2]int8
	Mv       [2]MV
}

// ModeInfoDecodeBSize returns the reconstruction block size used by VP9 mode
// decoding. Sub-8x8 blocks still reconstruct through an 8x8 mode-info cell.
func ModeInfoDecodeBSize(bsize common.BlockSize) common.BlockSize {
	if bsize < common.Block8x8 {
		return common.Block8x8
	}
	return bsize
}

// FindInterMvRefsFields is the no-receiver entry point for the MV-ref scan.
// It takes only the fields the search actually reads so encoder and decoder
// code can share the libvpx v1.16.0 find_mv_refs_idx mechanics without a
// synthetic decoder state.
func FindInterMvRefsFields(miGrid []NeighborMi,
	usePrevFrameMvs bool, prevFrameMvs []MvRef,
	prevFrameMvRows, prevFrameMvCols int,
	tile TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
	mode common.PredictionMode,
	refFrame int8,
	signBias [MaxRefFrames]uint8,
	block int,
) ([2]MV, int) {
	var out [2]MV
	count := 0
	normalizeCount := false
	differentRefFound := false
	earlyBreak := mode != common.NearMv
	search := tables.MvRefBlocks[bsize]
	var prevFrameMvsRef *MvRef
	if usePrevFrameMvs && miRow >= 0 && miCol >= 0 &&
		miRow < prevFrameMvRows && miCol < prevFrameMvCols {
		idx := miRow*prevFrameMvCols + miCol
		if idx >= 0 && idx < len(prevFrameMvs) {
			prevFrameMvsRef = &prevFrameMvs[idx]
		}
	}

	i := 0
	if block >= 0 {
		for ; i < 2 && i < len(search); i++ {
			pos := search[i]
			if !IsInside(tile, miRows, miRow, miCol, int(pos.Row), int(pos.Col)) {
				continue
			}
			r := miRow + int(pos.Row)
			c := miCol + int(pos.Col)
			if r < 0 || c < 0 || r >= miRows || c >= miCols {
				continue
			}
			cand := &miGrid[r*miCols+c]
			differentRefFound = true
			if cand.RefFrame[0] == refFrame {
				mv := subBlockMv(cand, 0, int(pos.Col), block)
				if appendMvRef(&out, &count, mv, earlyBreak) {
					goto done
				}
			} else if cand.RefFrame[1] == refFrame {
				mv := subBlockMv(cand, 1, int(pos.Col), block)
				if appendMvRef(&out, &count, mv, earlyBreak) {
					goto done
				}
			}
		}
	}

	for ; i < len(search); i++ {
		pos := search[i]
		if !IsInside(tile, miRows, miRow, miCol, int(pos.Row), int(pos.Col)) {
			continue
		}
		r := miRow + int(pos.Row)
		c := miCol + int(pos.Col)
		if r < 0 || c < 0 || r >= miRows || c >= miCols {
			continue
		}
		cand := &miGrid[r*miCols+c]
		differentRefFound = true
		if cand.RefFrame[0] == refFrame {
			if appendMvRef(&out, &count, cand.Mv[0], earlyBreak) {
				goto done
			}
		} else if cand.RefFrame[1] == refFrame {
			if appendMvRef(&out, &count, cand.Mv[1], earlyBreak) {
				goto done
			}
		}
	}

	if prevFrameMvsRef != nil {
		if prevFrameMvsRef.RefFrame[0] == refFrame {
			if appendMvRef(&out, &count, prevFrameMvsRef.Mv[0], earlyBreak) {
				goto done
			}
		} else if prevFrameMvsRef.RefFrame[1] == refFrame {
			if appendMvRef(&out, &count, prevFrameMvsRef.Mv[1], earlyBreak) {
				goto done
			}
		}
	}

	if differentRefFound {
		for i := range search {
			pos := search[i]
			if !IsInside(tile, miRows, miRow, miCol, int(pos.Row), int(pos.Col)) {
				continue
			}
			r := miRow + int(pos.Row)
			c := miCol + int(pos.Col)
			if r < 0 || c < 0 || r >= miRows || c >= miCols {
				continue
			}
			cand := &miGrid[r*miCols+c]
			if cand.RefFrame[0] > IntraFrame && cand.RefFrame[0] != refFrame {
				mv := scaleDiffRefMv(cand.Mv[0], cand.RefFrame[0], refFrame, signBias)
				if appendMvRef(&out, &count, mv, earlyBreak) {
					goto done
				}
			}
			if cand.RefFrame[1] > IntraFrame && cand.RefFrame[1] != refFrame &&
				cand.Mv[1] != cand.Mv[0] {
				mv := scaleDiffRefMv(cand.Mv[1], cand.RefFrame[1], refFrame, signBias)
				if appendMvRef(&out, &count, mv, earlyBreak) {
					goto done
				}
			}
		}
	}

	if prevFrameMvsRef != nil {
		if prevFrameMvsRef.RefFrame[0] != refFrame &&
			prevFrameMvsRef.RefFrame[0] > IntraFrame {
			mv := scaleDiffRefMv(prevFrameMvsRef.Mv[0],
				prevFrameMvsRef.RefFrame[0], refFrame, signBias)
			if appendMvRef(&out, &count, mv, earlyBreak) {
				goto done
			}
		}
		if prevFrameMvsRef.RefFrame[1] > IntraFrame &&
			prevFrameMvsRef.RefFrame[1] != refFrame &&
			prevFrameMvsRef.Mv[1] != prevFrameMvsRef.Mv[0] {
			mv := scaleDiffRefMv(prevFrameMvsRef.Mv[1],
				prevFrameMvsRef.RefFrame[1], refFrame, signBias)
			if appendMvRef(&out, &count, mv, earlyBreak) {
				goto done
			}
		}
	}

	normalizeCount = true

done:
	if normalizeCount {
		if mode == common.NearMv {
			count = 2
		} else {
			count = 1
		}
	}
	for i := 0; i < count; i++ {
		ClampMvRef(&out[i], miRows, miCols, miRow, miCol, bsize)
	}
	return out, count
}

var idxNColumnToSubblock = [4][2]int{
	{1, 2}, {1, 3}, {3, 2}, {3, 3},
}

func subBlockMv(candidate *NeighborMi, refIdx, searchCol, block int) MV {
	if block >= 0 && block < len(idxNColumnToSubblock) &&
		candidate != nil && candidate.SbType < common.Block8x8 {
		colIdx := 0
		if searchCol == 0 {
			colIdx = 1
		}
		return candidate.Bmi[idxNColumnToSubblock[block][colIdx]].AsMv[refIdx]
	}
	return candidate.Mv[refIdx]
}

// ClampMvRef mirrors libvpx's clamp_mv_ref for a mode-info block.
func ClampMvRef(mv *MV, miRows, miCols, miRow, miCol int, bsize common.BlockSize) {
	const mvBorder = 16 << 3
	miW := int(common.Num8x8BlocksWideLookup[bsize])
	miH := int(common.Num8x8BlocksHighLookup[bsize])
	left := -((miCol * common.MiSize) * 8)
	right := ((miCols - miW - miCol) * common.MiSize) * 8
	top := -((miRow * common.MiSize) * 8)
	bottom := ((miRows - miH - miRow) * common.MiSize) * 8
	ClampMv(mv,
		int32(left-mvBorder), int32(right+mvBorder),
		int32(top-mvBorder), int32(bottom+mvBorder))
}

func appendMvRef(out *[2]MV, count *int, mv MV, earlyBreak bool) bool {
	if *count == 0 {
		out[0] = mv
		*count = 1
		return earlyBreak
	}
	if mv != out[0] {
		out[1] = mv
		*count = 2
		return true
	}
	return false
}

func scaleDiffRefMv(mv MV, candRef, refFrame int8,
	signBias [MaxRefFrames]uint8,
) MV {
	if candRef >= 0 && int(candRef) < len(signBias) &&
		refFrame >= 0 && int(refFrame) < len(signBias) &&
		signBias[candRef] != signBias[refFrame] {
		mv.Row = -mv.Row
		mv.Col = -mv.Col
	}
	return mv
}

// InterModeMvCandidate selects the NEARESTMV/NEARMV candidate returned by the
// VP9 MV-ref scan.
func InterModeMvCandidate(refs [2]MV, count int,
	mode common.PredictionMode,
) MV {
	if mode == common.NearMv {
		if count > 1 {
			return refs[1]
		}
		return MV{}
	}
	if count > 0 {
		return refs[0]
	}
	return MV{}
}

// CanReconstructInterBlock reports whether the inter mode info has a
// reconstruction path in this Profile 0 implementation.
func CanReconstructInterBlock(mi *NeighborMi) bool {
	if mi == nil {
		return false
	}
	if mi.RefFrame[0] <= IntraFrame || mi.RefFrame[0] > AltrefFrame {
		return false
	}
	if mi.RefFrame[1] != NoRefFrame &&
		(mi.RefFrame[1] <= IntraFrame || mi.RefFrame[1] > AltrefFrame) {
		return false
	}
	return mi.Mode == common.ZeroMv || mi.Mode == common.NearestMv ||
		mi.Mode == common.NearMv || mi.Mode == common.NewMv
}

// InterPredictSourceInBounds checks whether the prediction filter taps can read
// directly from the reference frame without first extending a border window.
func InterPredictSourceInBounds(srcX, srcY, bw, bh int,
	srcStride, srcRows int,
	subpelX, subpelY int,
) bool {
	left, right, top, bottom := InterPredictSourceMargins(subpelX, subpelY)
	return srcX >= left && srcY >= top &&
		srcX+bw+right <= srcStride &&
		srcY+bh+bottom <= srcRows
}

// InterPredictSourceMargins returns the border pixels needed by subpel
// interpolation on each side of an inter predictor source window.
func InterPredictSourceMargins(subpelX, subpelY int) (left, right, top, bottom int) {
	if subpelX != 0 {
		left = tables.SubpelTaps/2 - 1
		right = tables.SubpelTaps - left - 1
	}
	if subpelY != 0 {
		top = tables.SubpelTaps/2 - 1
		bottom = tables.SubpelTaps - top - 1
	}
	return left, right, top, bottom
}

// ClampInt returns v saturated to [lo, hi].
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// BoolInt returns 1 for true and 0 for false, matching libvpx count updates.
func BoolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// BlockBoundsEdgesForMI converts a mode-info position into the edge offsets
// used by VP9 inter and intra reconstruction helpers.
func BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) BlockBoundsEdges {
	return BlockBoundsEdges{
		MbToLeftEdge:   -((miCol * common.MiSize) * 8),
		MbToRightEdge:  ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) * common.MiSize) * 8,
		MbToTopEdge:    -((miRow * common.MiSize) * 8),
		MbToBottomEdge: ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) * common.MiSize) * 8,
	}
}

// PlaneMaxBlocks4x4 returns the in-frame 4x4 block span for a plane at a
// possibly clipped frame edge.
func PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, pd *MacroblockdPlane,
	planeBsize common.BlockSize,
) (int, int) {
	edges := BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	w := int(common.Num4x4BlocksWideLookup[planeBsize])
	h := int(common.Num4x4BlocksHighLookup[planeBsize])
	if edges.MbToRightEdge < 0 {
		w += edges.MbToRightEdge >> (5 + pd.SubsamplingX)
	}
	if edges.MbToBottomEdge < 0 {
		h += edges.MbToBottomEdge >> (5 + pd.SubsamplingY)
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return w, h
}

// InterReferenceSlot maps a LAST/GOLDEN/ALTREF reference enum to the active
// reference-buffer slot in the uncompressed frame header.
func InterReferenceSlot(hdr *UncompressedHeader, ref int8) (int, bool) {
	if ref < LastFrame || ref > AltrefFrame {
		return 0, false
	}
	idx := int(ref - LastFrame)
	slot := int(hdr.InterRef.RefIndex[idx])
	if slot < 0 || slot >= common.RefFrames {
		return 0, false
	}
	return slot, true
}
