package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c
// - vp8/vp8_dx_iface.c

var ErrInvalidFrameHeader = errors.New("govpx: invalid VP8 frame header")

var KeyFrameStartCode = [3]byte{0x9d, 0x01, 0x2a}

type FrameHeader struct {
	FrameType common.FrameType
	Profile   int

	ShowFrame bool

	FirstPartitionSize int
	HeaderSize         int

	Width      int
	Height     int
	HorizScale int
	VertScale  int
}

func (h FrameHeader) KeyFrame() bool {
	return h.FrameType == common.KeyFrame
}

func ParseFrameHeader(packet []byte) (FrameHeader, error) {
	if len(packet) < 3 {
		return FrameHeader{}, ErrInvalidFrameHeader
	}

	tag := uint32(packet[0]) | uint32(packet[1])<<8 | uint32(packet[2])<<16
	header := FrameHeader{
		FrameType:          common.FrameType(tag & 1),
		Profile:            int((tag >> 1) & 7),
		ShowFrame:          ((tag >> 4) & 1) != 0,
		FirstPartitionSize: int(tag >> 5),
		HeaderSize:         3,
	}

	if !header.KeyFrame() {
		return header, nil
	}
	if len(packet) < 10 {
		return FrameHeader{}, ErrInvalidFrameHeader
	}
	if packet[3] != KeyFrameStartCode[0] || packet[4] != KeyFrameStartCode[1] || packet[5] != KeyFrameStartCode[2] {
		return FrameHeader{}, ErrInvalidFrameHeader
	}

	widthRaw := uint16(packet[6]) | uint16(packet[7])<<8
	heightRaw := uint16(packet[8]) | uint16(packet[9])<<8
	header.Width = int(widthRaw & 0x3fff)
	header.HorizScale = int(widthRaw >> 14)
	header.Height = int(heightRaw & 0x3fff)
	header.VertScale = int(heightRaw >> 14)
	header.HeaderSize = 10
	if header.Width <= 0 || header.Height <= 0 {
		return FrameHeader{}, ErrInvalidFrameHeader
	}
	return header, nil
}
