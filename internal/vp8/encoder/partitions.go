package encoder

import "github.com/thesyncim/gopvx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c token partition
// sizing and row-to-partition emission order.

func tokenPartitionCount(tokenPartition common.TokenPartition) (int, bool) {
	if tokenPartition < common.OnePartition || tokenPartition > common.EightPartition {
		return 0, false
	}
	return 1 << uint(tokenPartition), true
}

func writePartitionedTokenPayload(dst []byte, tokenStart int, tokenPartition common.TokenPartition, write func(partitions int, writers *[8]BoolWriter) error) (int, error) {
	partitionCount, ok := tokenPartitionCount(tokenPartition)
	if !ok || partitionCount <= 1 {
		return 0, ErrInvalidPacketConfig
	}
	sizeTableBytes := 3 * (partitionCount - 1)
	if tokenStart < 0 || tokenStart > len(dst) || len(dst)-tokenStart < sizeTableBytes {
		return 0, ErrBufferTooSmall
	}

	var writers [8]BoolWriter
	var buffers [8][]byte
	for i := 0; i < partitionCount; i++ {
		buffers[i] = make([]byte, len(dst))
		writers[i].Init(buffers[i])
	}

	if err := write(partitionCount, &writers); err != nil {
		return 0, err
	}

	offset := tokenStart + sizeTableBytes
	for i := 0; i < partitionCount; i++ {
		writers[i].Finish()
		if err := writers[i].Err(); err != nil {
			return 0, err
		}
		size := writers[i].BytesWritten()
		if size > MaxPartitionSize {
			return 0, ErrInvalidPacketConfig
		}
		if i < partitionCount-1 {
			if err := PutPartitionSize(dst[tokenStart+i*3:], size); err != nil {
				return 0, err
			}
		}
		if len(dst)-offset < size {
			return 0, ErrBufferTooSmall
		}
		copy(dst[offset:], buffers[i][:size])
		offset += size
	}
	return offset, nil
}
