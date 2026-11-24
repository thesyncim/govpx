package encoder

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c sub-pixel motion-search
// range and bounds checks.

const subpelMVQuarterPelLongLimit = (1 << 10) - 1 // libvpx mvlong_width = 10.

func InterFrameSubpixelMotionVectorInRange(mv MotionVector, bestRefMV MotionVector) bool {
	maxFullPelEighths := InterFrameMaxFullPelVal << 3
	rowDelta := int(mv.Row) - int(bestRefMV.Row)
	colDelta := int(mv.Col) - int(bestRefMV.Col)
	rMask := rowDelta >> intSignShift
	cMask := colDelta >> intSignShift
	rowDelta = (rowDelta ^ rMask) - rMask
	colDelta = (colDelta ^ cMask) - cMask
	return rowDelta <= maxFullPelEighths && colDelta <= maxFullPelEighths
}

// InterFrameSubpelSearchBounds mirrors the minc/maxc/minr/maxr clamps libvpx
// computes at the head of vp8_find_best_sub_pixel_step_iteratively (and
// _step). The bounds are the intersection of the UMV window (in 1/4-pel:
// x->mv_col_min*4, x->mv_col_max*4) and a per-component window of size
// `(1 << mvlong_width) - 1` 1/4-pel sites around the 1/4-pel-aligned ref_mv
// (`ref_mv->as_mv.col >> 1`). CHECK_BETTER's IFMVCV macro short-circuits any
// candidate outside this rectangle to UINT_MAX, which the govpx iter searches
// previously skipped, letting the iter chase variance gradients past the UMV
// edge into the replicated border.
type InterFrameSubpelSearchBounds struct {
	RowMin int
	RowMax int
	ColMin int
	ColMax int
}

func InterFrameSubpelSearchBoundsFor(bestRefMV MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) InterFrameSubpelSearchBounds {
	// libvpx mv_col_min / mv_col_max are in integer-pel; *4 converts to 1/4-pel.
	// The UMV window: -(mb_col*16 + (UMV_BORDER - 16)) ... ((mb_cols-1-mb_col)*16 + (UMV_BORDER - 16)).
	umv := InterFrameUMVBorderPixels - 16
	rowMinIPel := -((mbRow * 16) + umv)
	rowMaxIPel := ((mbRows - 1 - mbRow) * 16) + umv
	colMinIPel := -((mbCol * 16) + umv)
	colMaxIPel := ((mbCols - 1 - mbCol) * 16) + umv

	rrQuarter := int(bestRefMV.Row) >> 1
	rcQuarter := int(bestRefMV.Col) >> 1
	rowMin := max(rrQuarter-subpelMVQuarterPelLongLimit, rowMinIPel*4)
	rowMax := min(rrQuarter+subpelMVQuarterPelLongLimit, rowMaxIPel*4)
	colMin := max(rcQuarter-subpelMVQuarterPelLongLimit, colMinIPel*4)
	colMax := min(rcQuarter+subpelMVQuarterPelLongLimit, colMaxIPel*4)
	return InterFrameSubpelSearchBounds{RowMin: rowMin, RowMax: rowMax, ColMin: colMin, ColMax: colMax}
}

func (b InterFrameSubpelSearchBounds) Contains(row int, col int) bool {
	return row >= b.RowMin && row <= b.RowMax && col >= b.ColMin && col <= b.ColMax
}
