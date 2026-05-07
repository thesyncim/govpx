package decoder

import "github.com/thesyncim/govpx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/decoder/error_concealment.c.

const maxErrorConcealmentOverlaps = 16

type errorConcealmentOverlapNode struct {
	mv      MotionVector
	overlap int
	used    bool
}

type errorConcealmentBlockOverlap struct {
	overlaps [maxErrorConcealmentOverlaps]errorConcealmentOverlapNode
}

type errorConcealmentMacroblockOverlap struct {
	blocks [16]errorConcealmentBlockOverlap
}

func PrepareErrorConcealmentModes(modes []MacroblockMode) {
	for i := range modes {
		mode := &modes[i]
		if mode.RefFrame == common.IntraFrame || mode.Mode == common.SplitMV {
			continue
		}
		for block := range mode.BlockMV {
			mode.BlockMV[block] = mode.MV
		}
	}
}

func EstimateMissingMotionVectors(modes []MacroblockMode, prevModes []MacroblockMode, rows int, cols int, firstCorrupt int) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if len(modes) < required || len(prevModes) < required {
		return ErrModeBufferTooSmall
	}
	if firstCorrupt < 0 {
		firstCorrupt = 0
	}
	if firstCorrupt >= required {
		return nil
	}

	overlaps := make([]errorConcealmentMacroblockOverlap, required)
	for mbRow := 0; mbRow < rows; mbRow++ {
		for mbCol := 0; mbCol < cols; mbCol++ {
			index := mbRow*cols + mbCol
			prev := &prevModes[index]
			if prev.RefFrame == common.LastFrame {
				calcPrevMacroblockOverlaps(overlaps, prev, mbRow, mbCol, rows, cols)
			}
		}
	}

	for index := firstCorrupt; index < required; index++ {
		mode := &modes[index]
		*mode = MacroblockMode{
			Mode:      common.SplitMV,
			UVMode:    common.DCPred,
			RefFrame:  common.LastFrame,
			Is4x4:     true,
			Partition: 3,
		}
		estimateMacroblockMotionVectors(&overlaps[index], mode)
	}
	return nil
}

func calcPrevMacroblockOverlaps(overlaps []errorConcealmentMacroblockOverlap, prev *MacroblockMode, mbRow int, mbCol int, rows int, cols int) {
	for subRow := 0; subRow < 4; subRow++ {
		for subCol := 0; subCol < 4; subCol++ {
			calculateErrorConcealmentOverlaps(overlaps, rows, cols, prev.BlockMV[subRow*4+subCol], 4*mbRow+subRow, 4*mbCol+subCol)
		}
	}
}

func calculateErrorConcealmentOverlaps(overlaps []errorConcealmentMacroblockOverlap, rows int, cols int, mv MotionVector, bRow int, bCol int) {
	row := (4 * bRow) << 3
	col := (4 * bCol) << 3
	newRow := row - int(mv.Row)
	newCol := col - int(mv.Col)

	if newRow >= ((16*rows)<<3) || newCol >= ((16*cols)<<3) {
		return
	}
	if newRow <= -32 || newCol <= -32 {
		return
	}

	overlapBRow := floorErrorConcealment(newRow/4, 3) >> 3
	overlapBCol := floorErrorConcealment(newCol/4, 3) >> 3
	overlapMBRow := floorErrorConcealment((overlapBRow<<3)/4, 3) >> 3
	overlapMBCol := floorErrorConcealment((overlapBCol<<3)/4, 3) >> 3

	endRow := minInt(rows-overlapMBRow, 2)
	endCol := minInt(cols-overlapMBCol, 2)
	if absInt(newRow-((16*overlapMBRow)<<3)) < ((3 * 4) << 3) {
		endRow = 1
	}
	if absInt(newCol-((16*overlapMBCol)<<3)) < ((3 * 4) << 3) {
		endCol = 1
	}

	for relRow := 0; relRow < endRow; relRow++ {
		for relCol := 0; relCol < endCol; relCol++ {
			mbRow := overlapMBRow + relRow
			mbCol := overlapMBCol + relCol
			if mbRow < 0 || mbCol < 0 || mbRow >= rows || mbCol >= cols {
				continue
			}
			mb := &overlaps[mbRow*cols+mbCol]
			calculateErrorConcealmentOverlapsMB(mb, mv, newRow, newCol, mbRow, mbCol, overlapBRow+relRow, overlapBCol+relCol)
		}
	}
}

func calculateErrorConcealmentOverlapsMB(mb *errorConcealmentMacroblockOverlap, mv MotionVector, newRow int, newCol int, mbRow int, mbCol int, firstBlockRow int, firstBlockCol int) {
	relBlockRow := firstBlockRow - mbRow*4
	relBlockCol := firstBlockCol - mbCol*4
	blockIndex := maxInt(relBlockRow, 0)*4 + maxInt(relBlockCol, 0)
	if blockIndex < 0 || blockIndex >= len(mb.blocks) {
		return
	}

	endRow := minInt(4+mbRow*4-firstBlockRow, 2)
	endCol := minInt(4+mbCol*4-firstBlockCol, 2)
	if newRow >= 0 && (newRow&0x1f) == 0 {
		endRow = 1
	}
	if newCol >= 0 && (newCol&0x1f) == 0 {
		endCol = 1
	}
	if newRow < (mbRow*16)<<3 {
		endRow = 1
	}
	if newCol < (mbCol*16)<<3 {
		endCol = 1
	}

	for row := 0; row < endRow; row++ {
		for col := 0; col < endCol; col++ {
			target := blockIndex + row*4 + col
			if target < 0 || target >= len(mb.blocks) {
				continue
			}
			overlap := errorConcealmentBlockOverlapArea(
				newRow,
				newCol,
				((firstBlockRow+row)*4)<<3,
				((firstBlockCol+col)*4)<<3,
			)
			assignErrorConcealmentOverlap(&mb.blocks[target], mv, overlap)
		}
	}
}

func assignErrorConcealmentOverlap(block *errorConcealmentBlockOverlap, mv MotionVector, overlap int) {
	if overlap <= 0 {
		return
	}
	for i := range block.overlaps {
		if !block.overlaps[i].used {
			block.overlaps[i] = errorConcealmentOverlapNode{mv: mv, overlap: overlap, used: true}
			return
		}
	}
}

func errorConcealmentBlockOverlapArea(aRow int, aCol int, bRow int, bCol int) int {
	top := maxInt(aRow, bRow)
	left := maxInt(aCol, bCol)
	right := minInt(aCol+(4<<3), bCol+(4<<3))
	bottom := minInt(aRow+(4<<3), bRow+(4<<3))
	return (bottom - top) * (right - left)
}

func estimateMacroblockMotionVectors(overlap *errorConcealmentMacroblockOverlap, mode *MacroblockMode) {
	nonZero := 0
	rowSum := 0
	colSum := 0
	for block := 0; block < 16; block++ {
		mv := estimateBlockMotionVector(&overlap.blocks[block])
		mode.BlockMV[block] = mv
		if !mv.IsZero() {
			nonZero++
			rowSum += int(mv.Row)
			colSum += int(mv.Col)
		}
	}
	if nonZero > 0 {
		mode.MV.Row = int16(rowSum / nonZero)
		mode.MV.Col = int16(colSum / nonZero)
	}
}

func estimateBlockMotionVector(overlap *errorConcealmentBlockOverlap) MotionVector {
	overlapSum := 0
	rowSum := 0
	colSum := 0
	for i := range overlap.overlaps {
		node := overlap.overlaps[i]
		if !node.used {
			break
		}
		rowSum += node.overlap * int(node.mv.Row)
		colSum += node.overlap * int(node.mv.Col)
		overlapSum += node.overlap
	}
	if overlapSum == 0 {
		return MotionVector{}
	}
	return MotionVector{
		Row: int16(rowSum / overlapSum),
		Col: int16(colSum / overlapSum),
	}
}

func floorErrorConcealment(x int, q int) int {
	return x & -(1 << q)
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
