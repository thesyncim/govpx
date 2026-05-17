package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type interFrameFullPixelBounds struct {
	rowMin int
	rowMax int
	colMin int
	colMax int
}

func interFrameFullPixelSearchBounds(bestRefMV vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) interFrameFullPixelBounds {
	bounds := interFrameFullPixelBounds{
		rowMin: ((int(bestRefMV.Row) + 7) >> 3) - interFrameMaxFullPelVal,
		rowMax: (int(bestRefMV.Row) >> 3) + interFrameMaxFullPelVal,
		colMin: ((int(bestRefMV.Col) + 7) >> 3) - interFrameMaxFullPelVal,
		colMax: (int(bestRefMV.Col) >> 3) + interFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := interFrameUMVBorderPixels - 16
		bounds.rowMin = max(bounds.rowMin, -((mbRow * 16) + umv))
		bounds.rowMax = min(bounds.rowMax, ((mbRows-1-mbRow)*16)+umv)
	}
	if mbCols > 0 {
		umv := interFrameUMVBorderPixels - 16
		bounds.colMin = max(bounds.colMin, -((mbCol * 16) + umv))
		bounds.colMax = min(bounds.colMax, ((mbCols-1-mbCol)*16)+umv)
	}
	return bounds
}

// interFrameUMVOnlyFullPixelSearchBounds mirrors libvpx's MB-scope UMV
// window without the [bestRefMV ± MAX_FULL_PEL_VAL] intersection. libvpx's
// SPLITMV picker (vp8/encoder/rdopt.c:1230 vp8_rd_pick_best_mbsegmentation)
// runs the first BLOCK_8X8 segmentation with x->mv_col_min/x->mv_col_max
// set to the wide MB-scope UMV window. Only the secondary segmentations
// (BLOCK_8X16/BLOCK_16X8/BLOCK_4X4 at rdopt.c:1245-1248) intersect that
// window with [best_ref_mv ± MAX_FULL_PEL_VAL], and the window is restored
// at rdopt.c:1297-1301 after the secondary calls return. This helper feeds
// the BLOCK_8X8 path so its per-sub-block diamond_search_sad sees the
// full MB-scope UMV reach and can find MVs farther from best_ref_mv than
// MAX_FULL_PEL_VAL, matching libvpx byte-for-byte.
func interFrameUMVOnlyFullPixelSearchBounds(mbRow int, mbCol int, mbRows int, mbCols int) interFrameFullPixelBounds {
	bounds := interFrameFullPixelBounds{
		rowMin: -interFrameMaxFullPelVal,
		rowMax: interFrameMaxFullPelVal,
		colMin: -interFrameMaxFullPelVal,
		colMax: interFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := interFrameUMVBorderPixels - 16
		bounds.rowMin = -((mbRow * 16) + umv)
		bounds.rowMax = ((mbRows - 1 - mbRow) * 16) + umv
	}
	if mbCols > 0 {
		umv := interFrameUMVBorderPixels - 16
		bounds.colMin = -((mbCol * 16) + umv)
		bounds.colMax = ((mbCols - 1 - mbCol) * 16) + umv
	}
	return bounds
}

func interFrameUMVFullPixelInRange(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if min(mbRows, mbCols) <= 0 {
		return true
	}
	umv := interFrameUMVBorderPixels - 16
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	rowMin := -((mbRow * 16) + umv)
	rowMax := ((mbRows - 1 - mbRow) * 16) + umv
	colMin := -((mbCol * 16) + umv)
	colMax := ((mbCols - 1 - mbCol) * 16) + umv
	return row >= rowMin && row <= rowMax && col >= colMin && col <= colMax
}

func clampInterMotionVectorToModeEdges(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) vp8enc.MotionVector {
	if min(mbRows, mbCols) <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return vp8enc.MotionVector{
		Row: int16(clampInterMotionVectorComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterMotionVectorComponent(int(mv.Col), left, right)),
	}
}

func clampInterMotionVectorComponent(v int, lowEdge int, highEdge int) int {
	return min(max(v, lowEdge-(16<<3)), highEdge+(16<<3))
}

func (b interFrameFullPixelBounds) containsFullPel(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

func (b interFrameFullPixelBounds) containsFullPelStrict(row int, col int) bool {
	return row > b.rowMin && row < b.rowMax && col > b.colMin && col < b.colMax
}

func (b interFrameFullPixelBounds) clampEighth(mv vp8enc.MotionVector) vp8enc.MotionVector {
	row := min(max(int(mv.Row)>>3, b.rowMin), b.rowMax)
	col := min(max(int(mv.Col)>>3, b.colMin), b.colMax)
	return vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
}
