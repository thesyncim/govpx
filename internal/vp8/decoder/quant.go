package decoder

import "github.com/thesyncim/govpx/internal/vp8/boolcoder"

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c quantizer header
// parsing.

type QuantHeader struct {
	BaseQIndex uint8

	Y1DCDelta int8
	Y2DCDelta int8
	Y2ACDelta int8
	UVDCDelta int8
	UVACDelta int8

	Updated bool
}

func parseQuantHeader(br *boolcoder.Decoder, prev QuantHeader) QuantHeader {
	h := prev
	h.BaseQIndex = uint8(br.ReadLiteral(7))
	h.Updated = false

	h.Y1DCDelta = readDeltaQ(br, prev.Y1DCDelta, &h.Updated)
	h.Y2DCDelta = readDeltaQ(br, prev.Y2DCDelta, &h.Updated)
	h.Y2ACDelta = readDeltaQ(br, prev.Y2ACDelta, &h.Updated)
	h.UVDCDelta = readDeltaQ(br, prev.UVDCDelta, &h.Updated)
	h.UVACDelta = readDeltaQ(br, prev.UVACDelta, &h.Updated)
	return h
}

func readDeltaQ(br *boolcoder.Decoder, prev int8, updated *bool) int8 {
	value := int8(0)
	if br.ReadBit() != 0 {
		value = readSignedLiteral(br, 4)
	}
	if value != prev {
		*updated = true
	}
	return value
}

func readSignedLiteral(br *boolcoder.Decoder, bits int) int8 {
	value := int8(br.ReadLiteral(bits))
	if br.ReadBit() != 0 {
		value = -value
	}
	return value
}
