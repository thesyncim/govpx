package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodemv.c mode probability
// initialization, update parsing, and keyframe mode decoding. Block mode
// context helpers mirror vp8/common/findnearmv.h.

var (
	ErrModeBufferTooSmall = errors.New("libgopx: VP8 mode buffer too small")
)

type ModeProbs struct {
	YMode  [tables.YModeProbCount]uint8
	UVMode [tables.UVModeProbCount]uint8
	BMode  [tables.BModeProbCount]uint8
	MV     [2][tables.MVPCount]uint8
}

type ModeHeader struct {
	MBNoCoeffSkip bool
	ProbSkipFalse uint8

	ProbIntra  uint8
	ProbLast   uint8
	ProbGolden uint8

	YModeUpdated  bool
	UVModeUpdated bool
	MVUpdateCount int
}

type InterModeCounts struct {
	Intra   uint8
	Nearest uint8
	Near    uint8
	Split   uint8
}

func ResetModeProbs(probs *ModeProbs) {
	probs.YMode = tables.DefaultYModeProbs
	probs.UVMode = tables.DefaultUVModeProbs
	probs.BMode = tables.DefaultBModeProbs
	probs.MV = tables.DefaultMVContext
}

func parseModeHeaderInto(br *boolcoder.Decoder, keyFrame bool, probs *ModeProbs) ModeHeader {
	var h ModeHeader
	h.MBNoCoeffSkip = br.ReadBit() != 0
	if h.MBNoCoeffSkip {
		h.ProbSkipFalse = uint8(br.ReadLiteral(8))
	}

	if keyFrame {
		return h
	}

	h.ProbIntra = uint8(br.ReadLiteral(8))
	h.ProbLast = uint8(br.ReadLiteral(8))
	h.ProbGolden = uint8(br.ReadLiteral(8))

	if br.ReadBit() != 0 {
		h.YModeUpdated = true
		for i := 0; i < tables.YModeProbCount; i++ {
			value := uint8(br.ReadLiteral(8))
			if probs != nil {
				probs.YMode[i] = value
			}
		}
	}

	if br.ReadBit() != 0 {
		h.UVModeUpdated = true
		for i := 0; i < tables.UVModeProbCount; i++ {
			value := uint8(br.ReadLiteral(8))
			if probs != nil {
				probs.UVMode[i] = value
			}
		}
	}

	for component := 0; component < 2; component++ {
		for i := 0; i < tables.MVPCount; i++ {
			if br.ReadBool(tables.MVUpdateProbs[component][i]) == 0 {
				continue
			}
			value := uint8(br.ReadLiteral(7))
			if value != 0 {
				value <<= 1
			} else {
				value = 1
			}
			if probs != nil {
				probs.MV[component][i] = value
			}
			h.MVUpdateCount++
		}
	}
	return h
}

func DecodeKeyFrameMacroblock(br *boolcoder.Decoder, segmentation *SegmentationHeader, modeHeader ModeHeader, above *MacroblockMode, left *MacroblockMode, out *MacroblockMode) {
	*out = MacroblockMode{}
	if segmentation != nil && segmentation.Enabled && segmentation.UpdateMap {
		out.SegmentID = readMacroblockSegmentID(br, segmentation.TreeProbs)
	}
	if modeHeader.MBNoCoeffSkip {
		out.MBSkipCoeff = br.ReadBool(modeHeader.ProbSkipFalse) != 0
	}
	decodeKeyFrameMacroblockMode(br, above, left, out)
}

func DecodeKeyFrameModeGrid(br *boolcoder.Decoder, rows int, cols int, segmentation *SegmentationHeader, modeHeader ModeHeader, modes []MacroblockMode) error {
	if _, err := validateModeGrid(rows, cols, modes); err != nil {
		return err
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			var above *MacroblockMode
			var left *MacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			DecodeKeyFrameMacroblock(br, segmentation, modeHeader, above, left, &modes[index])
		}
	}
	return nil
}

func DecodeInterModeGrid(br *boolcoder.Decoder, rows int, cols int, segmentation *SegmentationHeader, modeHeader ModeHeader, probs *ModeProbs, signBias [common.MaxRefFrames]bool, modes []MacroblockMode) error {
	if _, err := validateModeGrid(rows, cols, modes); err != nil {
		return err
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			var above *MacroblockMode
			var left *MacroblockMode
			var aboveLeft *MacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			if err := decodeInterMacroblockAt(br, row, col, rows, cols, segmentation, modeHeader, probs, above, left, aboveLeft, signBias, &modes[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func DecodeKeyFrameMacroblockMode(br *boolcoder.Decoder, above *MacroblockMode, left *MacroblockMode, out *MacroblockMode) {
	*out = MacroblockMode{}
	decodeKeyFrameMacroblockMode(br, above, left, out)
}

func DecodeInterIntraMacroblockMode(br *boolcoder.Decoder, probs *ModeProbs, out *MacroblockMode) {
	*out = MacroblockMode{}
	decodeInterIntraMacroblockMode(br, probs, out)
}

func DecodeInterMacroblock(br *boolcoder.Decoder, segmentation *SegmentationHeader, modeHeader ModeHeader, probs *ModeProbs, above *MacroblockMode, left *MacroblockMode, aboveLeft *MacroblockMode, signBias [common.MaxRefFrames]bool, out *MacroblockMode) error {
	return decodeInterMacroblockAt(br, 0, 0, 1, 1, segmentation, modeHeader, probs, above, left, aboveLeft, signBias, out)
}

func decodeInterMacroblockAt(br *boolcoder.Decoder, mbRow int, mbCol int, mbRows int, mbCols int, segmentation *SegmentationHeader, modeHeader ModeHeader, probs *ModeProbs, above *MacroblockMode, left *MacroblockMode, aboveLeft *MacroblockMode, signBias [common.MaxRefFrames]bool, out *MacroblockMode) error {
	*out = MacroblockMode{}
	if segmentation != nil && segmentation.Enabled && segmentation.UpdateMap {
		out.SegmentID = readMacroblockSegmentID(br, segmentation.TreeProbs)
	}
	if modeHeader.MBNoCoeffSkip {
		out.MBSkipCoeff = br.ReadBool(modeHeader.ProbSkipFalse) != 0
	}

	refFrame := ReadInterReferenceFrame(br, modeHeader)
	if refFrame == common.IntraFrame {
		decodeInterIntraMacroblockMode(br, probs, out)
		return nil
	}

	nearest, near, best, counts := FindNearMotionVectors(above, left, aboveLeft, refFrame, signBias)
	out.RefFrame = refFrame
	out.Mode = ReadInterPredictionMode(br, counts)
	switch out.Mode {
	case common.ZeroMV:
		out.MV = MotionVector{}
	case common.NearestMV:
		out.MV = clampMotionVectorToModeEdges(nearest, mbRow, mbCol, mbRows, mbCols)
	case common.NearMV:
		out.MV = clampMotionVectorToModeEdges(near, mbRow, mbCol, mbRows, mbCols)
	case common.NewMV:
		best = clampMotionVectorToModeEdges(best, mbRow, mbCol, mbRows, mbCols)
		out.MV = addMotionVectors(best, DecodeMotionVector(br, &probs.MV))
	case common.SplitMV:
		best = clampMotionVectorToModeEdges(best, mbRow, mbCol, mbRows, mbCols)
		decodeSplitMotionVectors(br, probs, left, above, best, out)
	}
	return nil
}

func ReadInterReferenceFrame(br *boolcoder.Decoder, modeHeader ModeHeader) common.MVReferenceFrame {
	if br.ReadBool(modeHeader.ProbIntra) == 0 {
		return common.IntraFrame
	}
	if br.ReadBool(modeHeader.ProbLast) != 0 {
		return common.MVReferenceFrame(2 + br.ReadBool(modeHeader.ProbGolden))
	}
	return common.LastFrame
}

func ReadInterPredictionMode(br *boolcoder.Decoder, counts InterModeCounts) common.MBPredictionMode {
	if br.ReadBool(tables.InterModeContexts[counts.Intra][0]) == 0 {
		return common.ZeroMV
	}
	if br.ReadBool(tables.InterModeContexts[counts.Nearest][1]) == 0 {
		return common.NearestMV
	}
	if br.ReadBool(tables.InterModeContexts[counts.Near][2]) == 0 {
		return common.NearMV
	}
	if br.ReadBool(tables.InterModeContexts[counts.Split][3]) != 0 {
		return common.SplitMV
	}
	return common.NewMV
}

func decodeKeyFrameMacroblockMode(br *boolcoder.Decoder, above *MacroblockMode, left *MacroblockMode, out *MacroblockMode) {
	out.RefFrame = common.IntraFrame
	out.Is4x4 = false
	out.BModes = [16]common.BPredictionMode{}
	out.Mode = common.MBPredictionMode(ReadKeyFrameYMode(br, tables.KeyFrameYModeProbs[:]))

	if out.Mode == common.BPred {
		out.Is4x4 = true
		for i := 0; i < 16; i++ {
			a := keyFrameAboveBlockMode(out, above, i)
			l := keyFrameLeftBlockMode(out, left, i)
			out.BModes[i] = common.BPredictionMode(ReadBMode(br, tables.KeyFrameBModeProbs[int(a)][int(l)][:]))
		}
	}

	out.UVMode = common.MBPredictionMode(ReadUVMode(br, tables.KeyFrameUVModeProbs[:]))
}

func decodeInterIntraMacroblockMode(br *boolcoder.Decoder, probs *ModeProbs, out *MacroblockMode) {
	out.RefFrame = common.IntraFrame
	out.Is4x4 = false
	out.BModes = [16]common.BPredictionMode{}
	out.Mode = common.MBPredictionMode(ReadYMode(br, probs.YMode[:]))

	if out.Mode == common.BPred {
		out.Is4x4 = true
		for i := 0; i < 16; i++ {
			out.BModes[i] = common.BPredictionMode(ReadBMode(br, probs.BMode[:]))
		}
	}

	out.UVMode = common.MBPredictionMode(ReadUVMode(br, probs.UVMode[:]))
}

func readMacroblockSegmentID(br *boolcoder.Decoder, probs [common.MBFeatureTreeProbs]uint8) uint8 {
	if br.ReadBool(probs[0]) != 0 {
		return uint8(2 + br.ReadBool(probs[2]))
	}
	return br.ReadBool(probs[1])
}

func keyFrameLeftBlockMode(cur *MacroblockMode, left *MacroblockMode, block int) common.BPredictionMode {
	if block&3 == 0 {
		if left == nil {
			return common.BDCPred
		}
		if left.Mode == common.BPred {
			return left.BModes[block+3]
		}
		return blockModeFromMacroblockMode(left.Mode)
	}
	return cur.BModes[block-1]
}

func keyFrameAboveBlockMode(cur *MacroblockMode, above *MacroblockMode, block int) common.BPredictionMode {
	if block>>2 == 0 {
		if above == nil {
			return common.BDCPred
		}
		if above.Mode == common.BPred {
			return above.BModes[block+12]
		}
		return blockModeFromMacroblockMode(above.Mode)
	}
	return cur.BModes[block-4]
}

func blockModeFromMacroblockMode(mode common.MBPredictionMode) common.BPredictionMode {
	switch mode {
	case common.VPred:
		return common.BVEPred
	case common.HPred:
		return common.BHEPred
	case common.TMPred:
		return common.BTMPred
	default:
		return common.BDCPred
	}
}

func addMotionVectors(a MotionVector, b MotionVector) MotionVector {
	return MotionVector{Row: a.Row + b.Row, Col: a.Col + b.Col}
}

func decodeSplitMotionVectors(br *boolcoder.Decoder, probs *ModeProbs, left *MacroblockMode, above *MacroblockMode, best MotionVector, out *MacroblockMode) {
	partition := 3
	partitions := 16
	if br.ReadBool(tables.MBSplitProbs[0]) != 0 {
		partition = 2
		partitions = 4
		if br.ReadBool(tables.MBSplitProbs[1]) != 0 {
			partition = int(br.ReadBool(tables.MBSplitProbs[2]))
			partitions = 2
		}
	}

	for subset := 0; subset < partitions; subset++ {
		block := int(tables.MBSplitOffset[partition][subset])
		leftMV := splitLeftMV(out, left, block)
		aboveMV := splitAboveMV(out, above, block)
		prob := subMVRefProbs(leftMV, aboveMV)
		blockMV := leftMV
		if br.ReadBool(prob[0]) != 0 {
			if br.ReadBool(prob[1]) != 0 {
				blockMV = MotionVector{}
				if br.ReadBool(prob[2]) != 0 {
					blockMV = addMotionVectors(best, DecodeMotionVector(br, &probs.MV))
				}
			} else {
				blockMV = aboveMV
			}
		}

		fillCount := int(tables.MBSplitFillCount[partition])
		fillStart := subset * fillCount
		for i := 0; i < fillCount; i++ {
			out.BlockMV[tables.MBSplitFillOffset[partition][fillStart+i]] = blockMV
		}
	}

	out.Partition = uint8(partition)
	out.MV = out.BlockMV[15]
	out.Is4x4 = true
}

func splitLeftMV(cur *MacroblockMode, left *MacroblockMode, block int) MotionVector {
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

func splitAboveMV(cur *MacroblockMode, above *MacroblockMode, block int) MotionVector {
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

func validateModeGrid(rows int, cols int, modes []MacroblockMode) (int, error) {
	if rows < 0 || cols < 0 {
		return 0, ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return 0, ErrModeBufferTooSmall
	}
	required := rows * cols
	if len(modes) < required {
		return 0, ErrModeBufferTooSmall
	}
	return required, nil
}
