package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c reference refresh
// header parsing.

type RefreshHeader struct {
	RefreshLast   bool
	RefreshGolden bool
	RefreshAltRef bool

	CopyBufferToGolden int
	CopyBufferToAltRef int

	GoldenSignBias bool
	AltRefSignBias bool

	RefreshEntropyProbs bool
}

func parseRefreshHeader(br *boolcoder.Decoder, frame FrameHeader) RefreshHeader {
	if frame.FrameType == common.KeyFrame {
		return RefreshHeader{
			RefreshLast:         true,
			RefreshGolden:       true,
			RefreshAltRef:       true,
			RefreshEntropyProbs: br.ReadBit() != 0,
		}
	}

	var h RefreshHeader
	h.RefreshGolden = br.ReadBit() != 0
	h.RefreshAltRef = br.ReadBit() != 0
	if !h.RefreshGolden {
		h.CopyBufferToGolden = int(br.ReadLiteral(2))
	}
	if !h.RefreshAltRef {
		h.CopyBufferToAltRef = int(br.ReadLiteral(2))
	}
	h.GoldenSignBias = br.ReadBit() != 0
	h.AltRefSignBias = br.ReadBit() != 0
	h.RefreshEntropyProbs = br.ReadBit() != 0
	h.RefreshLast = br.ReadBit() != 0
	return h
}
