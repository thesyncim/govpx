package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c token partition
// sizing and validation.

var ErrInvalidPartitionLayout = errors.New("libgopx: invalid VP8 partition layout")

type PartitionLayout struct {
	First      []byte
	Tokens     [8][]byte
	TokenCount int
}

func ParsePartitionLayout(packet []byte, frame FrameHeader, tokenPartition common.TokenPartition, out *PartitionLayout) error {
	*out = PartitionLayout{}
	if tokenPartition < common.OnePartition || tokenPartition > common.EightPartition {
		return ErrInvalidPartitionLayout
	}
	if frame.FirstPartitionSize <= 0 || frame.HeaderSize < 0 || frame.HeaderSize > len(packet) {
		return ErrInvalidPartitionLayout
	}
	firstEnd := frame.HeaderSize + frame.FirstPartitionSize
	if firstEnd < frame.HeaderSize || firstEnd > len(packet) {
		return ErrInvalidPartitionLayout
	}

	tokenCount := 1 << uint(tokenPartition)
	sizeTableBytes := 3 * (tokenCount - 1)
	if len(packet)-firstEnd < sizeTableBytes {
		return ErrInvalidPartitionLayout
	}

	out.First = packet[frame.HeaderSize:firstEnd]
	out.TokenCount = tokenCount

	sizeTable := packet[firstEnd : firstEnd+sizeTableBytes]
	offset := firstEnd + sizeTableBytes
	for i := 0; i < tokenCount-1; i++ {
		size := readTokenPartitionSize(sizeTable[i*3:])
		if size <= 0 || size > len(packet)-offset {
			*out = PartitionLayout{}
			return ErrInvalidPartitionLayout
		}
		out.Tokens[i] = packet[offset : offset+size]
		offset += size
	}

	lastSize := len(packet) - offset
	if lastSize <= 0 {
		*out = PartitionLayout{}
		return ErrInvalidPartitionLayout
	}
	out.Tokens[tokenCount-1] = packet[offset:]
	return nil
}

func readTokenPartitionSize(src []byte) int {
	return int(src[0]) | int(src[1])<<8 | int(src[2])<<16
}
