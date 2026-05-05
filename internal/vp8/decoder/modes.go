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
	ErrModeBufferTooSmall   = errors.New("libgopx: VP8 mode buffer too small")
	ErrUnsupportedInterMode = errors.New("libgopx: unsupported VP8 inter macroblock mode")
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
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if len(modes) < required {
		return ErrModeBufferTooSmall
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

func DecodeKeyFrameMacroblockMode(br *boolcoder.Decoder, above *MacroblockMode, left *MacroblockMode, out *MacroblockMode) {
	*out = MacroblockMode{}
	decodeKeyFrameMacroblockMode(br, above, left, out)
}

func DecodeInterIntraMacroblockMode(br *boolcoder.Decoder, probs *ModeProbs, out *MacroblockMode) {
	*out = MacroblockMode{}
	decodeInterIntraMacroblockMode(br, probs, out)
}

func DecodeInterMacroblock(br *boolcoder.Decoder, segmentation *SegmentationHeader, modeHeader ModeHeader, probs *ModeProbs, above *MacroblockMode, left *MacroblockMode, aboveLeft *MacroblockMode, signBias [common.MaxRefFrames]bool, out *MacroblockMode) error {
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
		out.MV = nearest
	case common.NearMV:
		out.MV = near
	case common.NewMV:
		out.MV = addMotionVectors(best, DecodeMotionVector(br, &probs.MV))
	case common.SplitMV:
		out.Is4x4 = true
		return ErrUnsupportedInterMode
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
