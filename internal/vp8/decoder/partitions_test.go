package decoder

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
)

func TestParsePartitionLayoutOnePartition(t *testing.T) {
	packet, frame := partitionPacket(t, common.OnePartition, []byte{1, 2, 3}, [][]byte{{4, 5, 6, 7}})
	var layout PartitionLayout

	if err := ParsePartitionLayout(packet, frame, common.OnePartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	if layout.TokenCount != 1 {
		t.Fatalf("TokenCount = %d, want 1", layout.TokenCount)
	}
	if !bytes.Equal(layout.First, []byte{1, 2, 3}) || !bytes.Equal(layout.Tokens[0], []byte{4, 5, 6, 7}) {
		t.Fatalf("layout = %+v, want first/token slices", layout)
	}
}

func TestParsePartitionLayoutFourPartitions(t *testing.T) {
	tokens := [][]byte{{10}, {11, 12}, {13, 14, 15}, {16, 17, 18, 19}}
	packet, frame := partitionPacket(t, common.FourPartition, []byte{1, 2}, tokens)
	var layout PartitionLayout

	if err := ParsePartitionLayout(packet, frame, common.FourPartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	if layout.TokenCount != 4 {
		t.Fatalf("TokenCount = %d, want 4", layout.TokenCount)
	}
	for i, want := range tokens {
		if !bytes.Equal(layout.Tokens[i], want) {
			t.Fatalf("token[%d] = %v, want %v", i, layout.Tokens[i], want)
		}
	}
}

func TestParsePartitionLayoutRejectsMalformed(t *testing.T) {
	packet, frame := partitionPacket(t, common.TwoPartition, []byte{1, 2}, [][]byte{{3}, {4}})
	tests := []struct {
		name  string
		frame FrameHeader
		data  []byte
		part  common.TokenPartition
	}{
		{name: "bad partition enum", frame: frame, data: packet, part: common.TokenPartition(4)},
		{name: "missing first partition", frame: FrameHeader{HeaderSize: frame.HeaderSize, FirstPartitionSize: len(packet)}, data: packet, part: common.OnePartition},
		{name: "truncated size table", frame: frame, data: packet[:frame.HeaderSize+frame.FirstPartitionSize+2], part: common.TwoPartition},
		{name: "partition length too long", frame: frame, data: corruptTokenSize(packet, frame, 10), part: common.TwoPartition},
		{name: "zero explicit partition", frame: frame, data: corruptTokenSize(packet, frame, 0), part: common.TwoPartition},
		{name: "empty implicit partition", frame: frame, data: packet[:len(packet)-1], part: common.TwoPartition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var layout PartitionLayout
			err := ParsePartitionLayout(tt.data, tt.frame, tt.part, &layout)
			if !errors.Is(err, ErrInvalidPartitionLayout) {
				t.Fatalf("error = %v, want ErrInvalidPartitionLayout", err)
			}
			if layout.First != nil || layout.TokenCount != 0 || layout.Tokens[0] != nil {
				t.Fatalf("layout = %+v, want zero after error", layout)
			}
		})
	}
}

func TestParsePartitionLayoutAllocatesZero(t *testing.T) {
	packet, frame := partitionPacket(t, common.FourPartition, []byte{1, 2}, [][]byte{{3}, {4}, {5}, {6}})
	var layout PartitionLayout
	allocs := testing.AllocsPerRun(1000, func() {
		_ = ParsePartitionLayout(packet, frame, common.FourPartition, &layout)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func partitionPacket(t *testing.T, tokenPartition common.TokenPartition, first []byte, tokens [][]byte) ([]byte, FrameHeader) {
	t.Helper()
	count := 1 << uint(tokenPartition)
	if len(tokens) != count {
		t.Fatalf("token count = %d, want %d", len(tokens), count)
	}

	packet := keyFramePacket(16, 16, 0, 0, len(first), 0, true)
	packet = append(packet, first...)
	for i := 0; i < count-1; i++ {
		size := len(tokens[i])
		packet = append(packet, byte(size), byte(size>>8), byte(size>>16))
	}
	for i := 0; i < count; i++ {
		packet = append(packet, tokens[i]...)
	}

	frame, err := ParseFrameHeader(packet)
	if err != nil {
		t.Fatalf("ParseFrameHeader returned error: %v", err)
	}
	return packet, frame
}

func corruptTokenSize(packet []byte, frame FrameHeader, size int) []byte {
	out := append([]byte(nil), packet...)
	offset := frame.HeaderSize + frame.FirstPartitionSize
	out[offset] = byte(size)
	out[offset+1] = byte(size >> 8)
	out[offset+2] = byte(size >> 16)
	return out
}
