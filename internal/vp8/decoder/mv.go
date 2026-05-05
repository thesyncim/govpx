package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodemv.c motion-vector decoding
// - vp8/common/findnearmv.c near motion-vector selection

const (
	mvProbIsShort = 0
	mvProbSign    = 1
	mvProbShort   = 2
	mvNumShort    = 8
	mvProbBits    = mvProbShort + mvNumShort - 1
	mvLongWidth   = 10
)

type MotionVector struct {
	Row int16
	Col int16
}

func DecodeMotionVector(br *boolcoder.Decoder, probs *[2][tables.MVPCount]uint8) MotionVector {
	row := readMVComponent(br, probs[0][:])
	col := readMVComponent(br, probs[1][:])
	return MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
}

func readMVComponent(br *boolcoder.Decoder, probs []uint8) int {
	x := 0
	if br.ReadBool(probs[mvProbIsShort]) != 0 {
		for i := 0; i < 3; i++ {
			x += int(br.ReadBool(probs[mvProbBits+i])) << i
		}
		for i := mvLongWidth - 1; i > 3; i-- {
			x += int(br.ReadBool(probs[mvProbBits+i])) << i
		}
		if x&0xfff0 == 0 || br.ReadBool(probs[mvProbBits+3]) != 0 {
			x += 8
		}
	} else {
		x = ReadTree(br, tables.SmallMVTree[:], probs[mvProbShort:])
	}

	if x != 0 && br.ReadBool(probs[mvProbSign]) != 0 {
		x = -x
	}
	return x
}

func FindNearMotionVectors(above *MacroblockMode, left *MacroblockMode, aboveLeft *MacroblockMode, refFrame common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) (MotionVector, MotionVector, MotionVector, InterModeCounts) {
	var nearMVs [4]MotionVector
	var counts [4]uint8
	mvIndex := 0
	countIndex := 0

	if above != nil && above.RefFrame != common.IntraFrame {
		if !above.MV.IsZero() {
			mvIndex++
			nearMVs[mvIndex] = biasMotionVector(above.MV, above.RefFrame, refFrame, signBias)
			countIndex++
		}
		counts[countIndex] += 2
	}

	if left != nil && left.RefFrame != common.IntraFrame {
		if !left.MV.IsZero() {
			thisMV := biasMotionVector(left.MV, left.RefFrame, refFrame, signBias)
			if thisMV != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = thisMV
				countIndex++
			}
			counts[countIndex] += 2
		} else {
			counts[0] += 2
		}
	}

	if aboveLeft != nil && aboveLeft.RefFrame != common.IntraFrame {
		if !aboveLeft.MV.IsZero() {
			thisMV := biasMotionVector(aboveLeft.MV, aboveLeft.RefFrame, refFrame, signBias)
			if thisMV != nearMVs[mvIndex] {
				mvIndex++
				nearMVs[mvIndex] = thisMV
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

func (mv MotionVector) IsZero() bool {
	return mv.Row == 0 && mv.Col == 0
}

func biasMotionVector(mv MotionVector, refFrame common.MVReferenceFrame, target common.MVReferenceFrame, signBias [common.MaxRefFrames]bool) MotionVector {
	if signBias[refFrame] == signBias[target] {
		return mv
	}
	return MotionVector{Row: -mv.Row, Col: -mv.Col}
}

func splitModeCount(mode *MacroblockMode) uint8 {
	if mode != nil && mode.Mode == common.SplitMV {
		return 1
	}
	return 0
}
