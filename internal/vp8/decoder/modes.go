package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodemv.c mode probability
// initialization, update parsing, and keyframe mode decoding. Block mode
// context helpers mirror vp8/common/findnearmv.h.

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

func DecodeKeyFrameMacroblockMode(br *boolcoder.Decoder, above *MacroblockMode, left *MacroblockMode, out *MacroblockMode) {
	*out = MacroblockMode{}
	decodeKeyFrameMacroblockMode(br, above, left, out)
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
