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
		rowMin := -((mbRow * 16) + umv)
		rowMax := ((mbRows - 1 - mbRow) * 16) + umv
		if bounds.rowMin < rowMin {
			bounds.rowMin = rowMin
		}
		if bounds.rowMax > rowMax {
			bounds.rowMax = rowMax
		}
	}
	if mbCols > 0 {
		umv := interFrameUMVBorderPixels - 16
		colMin := -((mbCol * 16) + umv)
		colMax := ((mbCols - 1 - mbCol) * 16) + umv
		if bounds.colMin < colMin {
			bounds.colMin = colMin
		}
		if bounds.colMax > colMax {
			bounds.colMax = colMax
		}
	}
	return bounds
}

func interFrameUMVFullPixelInRange(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) bool {
	if mbRows <= 0 || mbCols <= 0 {
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
	if mbRows <= 0 || mbCols <= 0 {
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
	if v < lowEdge-(16<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(16<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func (b interFrameFullPixelBounds) containsFullPel(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

func (b interFrameFullPixelBounds) containsFullPelStrict(row int, col int) bool {
	return row > b.rowMin && row < b.rowMax && col > b.colMin && col < b.colMax
}

func (b interFrameFullPixelBounds) clampEighth(mv vp8enc.MotionVector) vp8enc.MotionVector {
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	if row < b.rowMin {
		row = b.rowMin
	} else if row > b.rowMax {
		row = b.rowMax
	}
	if col < b.colMin {
		col = b.colMin
	} else if col > b.colMax {
		col = b.colMax
	}
	return vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
}
