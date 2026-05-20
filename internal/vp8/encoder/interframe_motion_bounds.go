package encoder

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c and
// vp8/encoder/rdopt.c full-pixel motion-search bounds.

const (
	InterFrameFullPixelSearchRadius = 16
	InterFrameMVFullPixelStep       = 8
	InterFrameMaxMVSearchSteps      = 8
	InterFrameMaxFirstStep          = 1 << (InterFrameMaxMVSearchSteps - 1)
	InterFrameMaxFullPelVal         = mvFullPixelMax
	InterFrameUMVBorderPixels       = 32
)

type InterFrameFullPixelBounds struct {
	RowMin int
	RowMax int
	ColMin int
	ColMax int
}

func InterFrameFullPixelSearchBounds(bestRefMV MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) InterFrameFullPixelBounds {
	bounds := InterFrameFullPixelBounds{
		RowMin: ((int(bestRefMV.Row) + 7) >> 3) - InterFrameMaxFullPelVal,
		RowMax: (int(bestRefMV.Row) >> 3) + InterFrameMaxFullPelVal,
		ColMin: ((int(bestRefMV.Col) + 7) >> 3) - InterFrameMaxFullPelVal,
		ColMax: (int(bestRefMV.Col) >> 3) + InterFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.RowMin = max(bounds.RowMin, -((mbRow * 16) + umv))
		bounds.RowMax = min(bounds.RowMax, ((mbRows-1-mbRow)*16)+umv)
	}
	if mbCols > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.ColMin = max(bounds.ColMin, -((mbCol * 16) + umv))
		bounds.ColMax = min(bounds.ColMax, ((mbCols-1-mbCol)*16)+umv)
	}
	return bounds
}

// InterFrameUMVOnlyFullPixelSearchBounds mirrors libvpx's MB-scope UMV
// window without the [bestRefMV +/- MAX_FULL_PEL_VAL] intersection. libvpx's
// SPLITMV picker (vp8/encoder/rdopt.c:1230 vp8_rd_pick_best_mbsegmentation)
// runs the first BLOCK_8X8 segmentation with x->mv_col_min/x->mv_col_max
// set to the wide MB-scope UMV window. Only the secondary segmentations
// (BLOCK_8X16/BLOCK_16X8/BLOCK_4X4 at rdopt.c:1245-1248) intersect that
// window with [best_ref_mv +/- MAX_FULL_PEL_VAL], and the window is restored
// at rdopt.c:1297-1301 after the secondary calls return. This helper feeds
// the BLOCK_8X8 path so its per-sub-block diamond_search_sad sees the
// full MB-scope UMV reach and can find MVs farther from best_ref_mv than
// MAX_FULL_PEL_VAL, matching libvpx byte-for-byte.
func InterFrameUMVOnlyFullPixelSearchBounds(mbRow int, mbCol int, mbRows int, mbCols int) InterFrameFullPixelBounds {
	bounds := InterFrameFullPixelBounds{
		RowMin: -InterFrameMaxFullPelVal,
		RowMax: InterFrameMaxFullPelVal,
		ColMin: -InterFrameMaxFullPelVal,
		ColMax: InterFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.RowMin = -((mbRow * 16) + umv)
		bounds.RowMax = ((mbRows - 1 - mbRow) * 16) + umv
	}
	if mbCols > 0 {
		umv := InterFrameUMVBorderPixels - 16
		bounds.ColMin = -((mbCol * 16) + umv)
		bounds.ColMax = ((mbCols - 1 - mbCol) * 16) + umv
	}
	return bounds
}

func InterFrameUMVFullPixelInRange(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if min(mbRows, mbCols) <= 0 {
		return true
	}
	umv := InterFrameUMVBorderPixels - 16
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	rowMin := -((mbRow * 16) + umv)
	rowMax := ((mbRows - 1 - mbRow) * 16) + umv
	colMin := -((mbCol * 16) + umv)
	colMax := ((mbCols - 1 - mbCol) * 16) + umv
	return row >= rowMin && row <= rowMax && col >= colMin && col <= colMax
}

func ClampInterMotionVectorToModeEdges(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) MotionVector {
	return clampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
}

func (b InterFrameFullPixelBounds) ContainsFullPel(row int, col int) bool {
	return row >= b.RowMin && row <= b.RowMax && col >= b.ColMin && col <= b.ColMax
}

func (b InterFrameFullPixelBounds) ContainsFullPelStrict(row int, col int) bool {
	return row > b.RowMin && row < b.RowMax && col > b.ColMin && col < b.ColMax
}

func (b InterFrameFullPixelBounds) ClampEighth(mv MotionVector) MotionVector {
	row := min(max(int(mv.Row)>>3, b.RowMin), b.RowMax)
	col := min(max(int(mv.Col)>>3, b.ColMin), b.ColMax)
	return MotionVector{Row: int16(row * InterFrameMVFullPixelStep), Col: int16(col * InterFrameMVFullPixelStep)}
}
