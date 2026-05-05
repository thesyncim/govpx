package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodemv.c motion-vector decoding.

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
