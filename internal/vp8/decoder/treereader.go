package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/treereader.h
// - vp8/common/treecoder.h

func ReadTree(br *boolcoder.Decoder, tree []int16, probs []uint8) int {
	i := int16(0)
	for {
		prob := probs[i>>1]
		next := tree[i+int16(br.ReadBool(prob))]
		if next <= 0 {
			return int(-next)
		}
		i = next
	}
}

func ReadCoefToken(br *boolcoder.Decoder, probs []uint8) int {
	return ReadTree(br, tables.CoefTree[:], probs)
}

func ReadYMode(br *boolcoder.Decoder, probs []uint8) int {
	return ReadTree(br, tables.YModeTree[:], probs)
}

func ReadKeyFrameYMode(br *boolcoder.Decoder, probs []uint8) int {
	return ReadTree(br, tables.KeyFrameYModeTree[:], probs)
}

func ReadUVMode(br *boolcoder.Decoder, probs []uint8) int {
	return ReadTree(br, tables.UVModeTree[:], probs)
}

func ReadBMode(br *boolcoder.Decoder, probs []uint8) int {
	return ReadTree(br, tables.BModeTree[:], probs)
}
