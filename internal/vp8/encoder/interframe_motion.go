package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c and nearby interframe
// motion-vector context handling.

type InterModeCounts struct {
	Intra   uint8
	Nearest uint8
	Near    uint8
	Split   uint8
}

func interFrameSignBias(cfg *InterFrameStateConfig) [common.MaxRefFrames]bool {
	return [common.MaxRefFrames]bool{
		common.GoldenFrame: cfg.GoldenSignBias,
		common.AltRefFrame: cfg.AltRefSignBias,
	}
}

func interModeCounts(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) InterModeCounts {
	_, _, _, counts := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	return counts
}

func InterFrameModeCounts(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) InterModeCounts {
	return interModeCounts(above, left, aboveLeft, refFrame, signBias)
}

func interBestMotionVector(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) MotionVector {
	_, _, best, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	return best
}

func interBestMotionVectorAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) MotionVector {
	return clampInterMotionVectorToModeEdges(interBestMotionVector(above, left, aboveLeft, refFrame, signBias), mbRow, mbCol, mbRows, mbCols)
}

func InterFrameBestMotionVectorAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) MotionVector {
	return interBestMotionVectorAt(above, left, aboveLeft, refFrame, mbRow, mbCol, mbRows, mbCols, signBias)
}

func InterFrameNearMotionVectorsAt(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) (MotionVector, MotionVector) {
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	return nearest, near
}

func InterFrameMotionModeForVector(refFrame common.MVReferenceFrame, mv MotionVector, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, signBias [common.MaxRefFrames]bool) InterFrameMacroblockMode {
	return InterFrameMotionModeForVectorAt(refFrame, mv, above, left, aboveLeft, 0, 0, 1, 1, signBias)
}

func InterFrameMotionModeForVectorAt(refFrame common.MVReferenceFrame, mv MotionVector, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) InterFrameMacroblockMode {
	if mv.IsZero() {
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.ZeroMV}
	}
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	switch mv {
	case nearest:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NearestMV, MV: mv}
	case near:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NearMV, MV: mv}
	default:
		return InterFrameMacroblockMode{RefFrame: refFrame, Mode: common.NewMV, MV: mv}
	}
}

func findNearInterMotionVectors(above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) (MotionVector, MotionVector, MotionVector, InterModeCounts) {
	var nearMVs [4]MotionVector
	var counts [4]uint8
	mvIndex := 0
	countIndex := 0

	if aboveRef := interFrameReference(above); aboveRef != common.IntraFrame {
		mv := signBiasMotionVector(above.MV, aboveRef, refFrame, signBias)
		if !mv.IsZero() {
			mvIndex++
			nearMVs[mvIndex] = mv
			countIndex++
		}
		counts[countIndex] += 2
	}
	if leftRef := interFrameReference(left); leftRef != common.IntraFrame {
		mv := signBiasMotionVector(left.MV, leftRef, refFrame, signBias)
		if !mv.IsZero() {
			if mv != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = mv
				countIndex++
			}
			counts[countIndex] += 2
		} else {
			counts[0] += 2
		}
	}
	if aboveLeftRef := interFrameReference(aboveLeft); aboveLeftRef != common.IntraFrame {
		mv := signBiasMotionVector(aboveLeft.MV, aboveLeftRef, refFrame, signBias)
		if !mv.IsZero() {
			if mv != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = mv
				countIndex++
			}
			counts[countIndex]++
		} else {
			counts[0]++
		}
	}
	if counts[3] != 0 && nearMVs[mvIndex] == nearMVs[1] {
		counts[1]++
	}
	counts[3] = splitModeCount(above)*2 + splitModeCount(left)*2 + splitModeCount(aboveLeft)
	if counts[2] > counts[1] {
		counts[1], counts[2] = counts[2], counts[1]
		nearMVs[1], nearMVs[2] = nearMVs[2], nearMVs[1]
	}
	if counts[1] >= counts[0] {
		nearMVs[0] = nearMVs[1]
	}
	return nearMVs[1], nearMVs[2], nearMVs[0], InterModeCounts{
		Intra:   counts[0],
		Nearest: counts[1],
		Near:    counts[2],
		Split:   counts[3],
	}
}

func signBiasMotionVector(mv MotionVector, srcRefFrame common.MVReferenceFrame, targetRefFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) MotionVector {
	if uint(srcRefFrame) < uint(len(signBias)) &&
		uint(targetRefFrame) < uint(len(signBias)) &&
		signBias[srcRefFrame] != signBias[targetRefFrame] {
		return MotionVector{Row: -mv.Row, Col: -mv.Col}
	}
	return mv
}

func (mv MotionVector) IsZero() bool {
	return mv.Row == 0 && mv.Col == 0
}

func validInterFrameMacroblockModeAt(mode *InterFrameMacroblockMode, above *InterFrameMacroblockMode, left *InterFrameMacroblockMode, aboveLeft *InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, signBias [common.MaxRefFrames]bool) bool {
	if mode == nil {
		return false
	}
	refFrame := interFrameReference(mode)
	if refFrame == common.IntraFrame {
		return validInterIntraMacroblockMode(mode)
	}
	if refFrame != common.LastFrame && refFrame != common.GoldenFrame && refFrame != common.AltRefFrame {
		return false
	}
	nearest, near, _, _ := findNearInterMotionVectors(above, left, aboveLeft, refFrame, signBias)
	nearest = clampInterMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	near = clampInterMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	switch mode.Mode {
	case common.ZeroMV:
		return mode.MV.IsZero()
	case common.NearestMV:
		return mode.MV == nearest
	case common.NearMV:
		return mode.MV == near
	case common.NewMV:
		return true
	case common.SplitMV:
		return validSplitMVModeWithContext(mode, left, above)
	default:
		return false
	}
}

func validSplitMVMode(mode *InterFrameMacroblockMode) bool {
	if mode == nil || mode.Mode != common.SplitMV || mode.Partition >= tables.NumMBSplits {
		return false
	}
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	fillCount := int(tables.MBSplitFillCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		mv := mode.BlockMV[block]
		if mv.Row&1 != 0 || mv.Col&1 != 0 {
			return false
		}
		if mode.BModes[block] < common.Left4x4 || mode.BModes[block] > common.New4x4 {
			return false
		}
		fillStart := subset * fillCount
		for i := range fillCount {
			if mode.BlockMV[tables.MBSplitFillOffset[mode.Partition][fillStart+i]] != mv {
				return false
			}
		}
	}
	return mode.MV == mode.BlockMV[15]
}

func validSplitMVModeWithContext(mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode) bool {
	if !validSplitMVMode(mode) {
		return false
	}
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		if !splitSubMotionLabelMatchesMV(mode.BModes[block], mode.BlockMV[block], leftMV, aboveMV) {
			return false
		}
	}
	return true
}

func clampInterMotionVectorToModeEdges(mv MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) MotionVector {
	if mbRows <= 0 || mbCols <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return MotionVector{
		Row: int16(clampInterModeMVComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterModeMVComponent(int(mv.Col), left, right)),
	}
}

func clampInterModeMVComponent(v int, lowEdge int, highEdge int) int {
	return min(max(v, lowEdge-(16<<3)), highEdge+(16<<3))
}

func interFrameReference(mode *InterFrameMacroblockMode) common.MVReferenceFrame {
	if mode == nil {
		return common.IntraFrame
	}
	if isInterIntraMacroblockMode(mode.Mode) {
		return common.IntraFrame
	}
	if mode.RefFrame == common.IntraFrame {
		return common.LastFrame
	}
	return mode.RefFrame
}

func splitModeCount(mode *InterFrameMacroblockMode) uint8 {
	if mode != nil && mode.Mode == common.SplitMV {
		return 1
	}
	return 0
}

func splitLeftMV(cur *InterFrameMacroblockMode, left *InterFrameMacroblockMode, block int) MotionVector {
	if block&3 == 0 {
		if left == nil {
			return MotionVector{}
		}
		if left.Mode == common.SplitMV {
			return left.BlockMV[block+3]
		}
		return left.MV
	}
	return cur.BlockMV[block-1]
}

func splitAboveMV(cur *InterFrameMacroblockMode, above *InterFrameMacroblockMode, block int) MotionVector {
	if block>>2 == 0 {
		if above == nil {
			return MotionVector{}
		}
		if above.Mode == common.SplitMV {
			return above.BlockMV[block+12]
		}
		return above.MV
	}
	return cur.BlockMV[block-4]
}

func subMVRefProbs(left MotionVector, above MotionVector) [3]uint8 {
	lez := 0
	if left.IsZero() {
		lez = 1
	}
	aez := 0
	if above.IsZero() {
		aez = 1
	}
	lea := 0
	if left == above {
		lea = 1
	}
	return tables.SubMVRefProb3[(aez<<2)|(lez<<1)|lea]
}

func countSplitMotionVectorBranches(counts *[2][tables.MVPCount][2]int, mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode, best MotionVector) error {
	if counts == nil || !validSplitMVModeWithContext(mode, left, above) {
		return ErrInvalidPacketConfig
	}
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return ErrInvalidPacketConfig
		}
		if bMode != common.New4x4 {
			continue
		}
		delta := MotionVector{Row: target.Row - best.Row, Col: target.Col - best.Col}
		if err := countMotionVectorBranches(counts, delta); err != nil {
			return err
		}
	}
	return nil
}

func countSplitMotionVectorEvents(events *motionVectorEventCounts, mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode, above *InterFrameMacroblockMode, best MotionVector) error {
	if events == nil || !validSplitMVModeWithContext(mode, left, above) {
		return ErrInvalidPacketConfig
	}
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := splitLeftMV(mode, left, block)
		aboveMV := splitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return ErrInvalidPacketConfig
		}
		if bMode != common.New4x4 {
			continue
		}
		delta := MotionVector{Row: target.Row - best.Row, Col: target.Col - best.Col}
		if err := countMotionVectorEvents(events, delta); err != nil {
			return err
		}
	}
	return nil
}

func splitSubMotionLabelMatchesMV(mode common.BPredictionMode, target MotionVector, left MotionVector, above MotionVector) bool {
	switch mode {
	case common.Left4x4:
		return target == left
	case common.Above4x4:
		return above != left && target == above
	case common.Zero4x4:
		return target.IsZero()
	case common.New4x4:
		return true
	default:
		return false
	}
}

func validInterIntraMacroblockMode(mode *InterFrameMacroblockMode) bool {
	// uint range check: DCPred=0, TMPred=3, so (UVMode < DCPred ||
	// > TMPred) collapses to uint(UVMode) > uint(TMPred).
	if mode.RefFrame != common.IntraFrame || !isInterIntraMacroblockMode(mode.Mode) || uint(mode.UVMode) > uint(common.TMPred) {
		return false
	}
	if mode.Mode != common.BPred {
		return true
	}
	for _, bMode := range mode.BModes {
		if bMode < common.BDCPred || bMode > common.BHUPred {
			return false
		}
	}
	return true
}

func isInterIntraMacroblockMode(mode common.MBPredictionMode) bool {
	// DCPred==0, BPred==4; single uint compare covers the dual bound.
	return uint(mode) <= uint(common.BPred)
}

func initInterFrameYModeTokens() [common.VP8YModes]TreeToken {
	var out [common.VP8YModes]TreeToken
	for i := range out {
		BuildTreeToken(tables.YModeTree[:], i, &out[i])
	}
	return out
}
