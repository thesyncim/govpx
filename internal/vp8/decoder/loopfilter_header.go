package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c loop-filter header
// parsing.

type LoopFilterType int

const (
	NormalLoopFilter LoopFilterType = iota
	SimpleLoopFilter
)

type LoopFilterHeader struct {
	Type           LoopFilterType
	Level          uint8
	SharpnessLevel uint8

	DeltaEnabled bool
	DeltaUpdate  bool

	RefDeltas  [common.MaxRefLFDeltas]int8
	ModeDeltas [common.MaxModeLFDeltas]int8
}

//lint:ignore U1000 libvpx parity helper, retained for future ports of fresh-state loop-filter header parsing
func parseLoopFilterHeader(br *boolcoder.Decoder) LoopFilterHeader {
	return parseLoopFilterHeaderWithPrevious(br, LoopFilterHeader{})
}

func parseLoopFilterHeaderWithPrevious(br *boolcoder.Decoder, previous LoopFilterHeader) LoopFilterHeader {
	var h LoopFilterHeader
	h.RefDeltas = previous.RefDeltas
	h.ModeDeltas = previous.ModeDeltas
	h.Type = LoopFilterType(br.ReadBit())
	h.Level = uint8(br.ReadLiteral(6))
	h.SharpnessLevel = uint8(br.ReadLiteral(3))

	h.DeltaEnabled = br.ReadBit() != 0
	if !h.DeltaEnabled {
		return h
	}

	h.DeltaUpdate = br.ReadBit() != 0
	if !h.DeltaUpdate {
		return h
	}

	for i := range h.RefDeltas {
		if br.ReadBit() != 0 {
			h.RefDeltas[i] = readSignedLiteral(br, 6)
		}
	}
	for i := range h.ModeDeltas {
		if br.ReadBit() != 0 {
			h.ModeDeltas[i] = readSignedLiteral(br, 6)
		}
	}
	return h
}
