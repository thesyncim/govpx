package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c token partition
// sizing and validation.

var ErrInvalidPartitionLayout = errors.New("govpx: invalid VP8 partition layout")

type PartitionLayout struct {
	First      []byte
	Tokens     [8][]byte
	TokenCount int
}

func ParsePartitionLayout(packet []byte, frame FrameHeader, tokenPartition common.TokenPartition, out *PartitionLayout) error {
	return parsePartitionLayout(packet, frame, tokenPartition, false, out)
}

func ParsePartitionLayoutWithErrorConcealment(packet []byte, frame FrameHeader, tokenPartition common.TokenPartition, out *PartitionLayout) error {
	return parsePartitionLayout(packet, frame, tokenPartition, true, out)
}

func parsePartitionLayout(packet []byte, frame FrameHeader, tokenPartition common.TokenPartition, errorConcealment bool, out *PartitionLayout) error {
	*out = PartitionLayout{}
	if tokenPartition < common.OnePartition || tokenPartition > common.EightPartition {
		return ErrInvalidPartitionLayout
	}
	// libvpx accepts FirstPartitionSize == 0 (no compressed first partition;
	// state-header bits come from the token partitions instead). Only treat
	// negative sizes or out-of-range HeaderSize as malformed.
	if frame.FirstPartitionSize < 0 || frame.HeaderSize < 0 || frame.HeaderSize > len(packet) {
		return ErrInvalidPartitionLayout
	}
	firstEnd := frame.HeaderSize + frame.FirstPartitionSize
	if firstEnd < frame.HeaderSize || firstEnd > len(packet) {
		if !errorConcealment || firstEnd < frame.HeaderSize {
			return ErrInvalidPartitionLayout
		}
		firstEnd = len(packet)
	}

	tokenCount := 1 << uint(tokenPartition)
	sizeTableBytes := 3 * (tokenCount - 1)
	if len(packet)-firstEnd < sizeTableBytes {
		if !errorConcealment {
			return ErrInvalidPartitionLayout
		}
		sizeTableBytes = len(packet) - firstEnd
	}

	out.First = packet[frame.HeaderSize:firstEnd]
	out.TokenCount = tokenCount

	sizeTable := packet[firstEnd : firstEnd+sizeTableBytes]
	offset := firstEnd + sizeTableBytes
	for i := 0; i < tokenCount-1; i++ {
		if len(sizeTable) < (i+1)*3 {
			out.Tokens[i] = packet[offset:]
			return nil
		}
		size := readTokenPartitionSize(sizeTable[i*3:])
		if size <= 0 || size > len(packet)-offset {
			if errorConcealment {
				out.Tokens[i] = packet[offset:]
				return nil
			}
			*out = PartitionLayout{}
			return ErrInvalidPartitionLayout
		}
		out.Tokens[i] = packet[offset : offset+size]
		offset += size
	}

	lastSize := len(packet) - offset
	if lastSize <= 0 {
		if errorConcealment {
			return nil
		}
		*out = PartitionLayout{}
		return ErrInvalidPartitionLayout
	}
	out.Tokens[tokenCount-1] = packet[offset:]
	return nil
}

func readTokenPartitionSize(src []byte) int {
	return int(src[0]) | int(src[1])<<8 | int(src[2])<<16
}
