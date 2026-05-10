package encoder

import "github.com/thesyncim/govpx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c token partition
// sizing and row-to-partition emission order.

func tokenPartitionCount(tokenPartition common.TokenPartition) (int, bool) {
	if tokenPartition < common.OnePartition || tokenPartition > common.EightPartition {
		return 0, false
	}
	return 1 << uint(tokenPartition), true
}

// PartitionScratch holds reusable per-token-partition buffers used by the
// multi-token-partition packet writers. Callers that want to amortise the
// per-frame partition-buffer allocation across encodes pass a pointer to a
// long-lived PartitionScratch into the *WithScratch variants of the
// frame-level Write* helpers; passing nil retains the historical
// allocate-on-each-call behaviour for ad-hoc test paths.
//
// The buffers grow lazily to len(dst); they are never shrunk so steady-state
// encoders see zero allocations from the partition stage after the first
// frame. The slice headers are kept distinct per-partition (no aliasing) so
// the BoolWriters initialised against them do not race or stomp each other
// when the row scheduler eventually drives them in parallel.
type PartitionScratch struct {
	buffers [8][]byte
}

func (s *PartitionScratch) Reset() {
	if s == nil {
		return
	}
	*s = PartitionScratch{}
}

// ensureCapacity grows the first partitionCount entries to at least size
// bytes. The active slice length is set exactly to size so callers can pass
// the slice into BoolWriter.Init without a separate cap/length check.
func (s *PartitionScratch) ensureCapacity(partitionCount int, size int) {
	for i := range partitionCount {
		if cap(s.buffers[i]) < size {
			s.buffers[i] = make([]byte, size)
		} else {
			s.buffers[i] = s.buffers[i][:size]
		}
	}
}

// preparePartitionWriters validates the destination and initialises the
// per-partition BoolWriters against scratch buffers. Returns the resolved
// scratch (heap-allocated when input was nil) and the partition count so
// the caller can finalize after driving the writers.
func preparePartitionWriters(scratch *PartitionScratch, writers *[8]BoolWriter, dst []byte, tokenStart int, tokenPartition common.TokenPartition) (*PartitionScratch, int, error) {
	pc, ok := tokenPartitionCount(tokenPartition)
	if !ok || pc <= 1 {
		return nil, 0, ErrInvalidPacketConfig
	}
	sizeTable := 3 * (pc - 1)
	if tokenStart < 0 || tokenStart > len(dst) || len(dst)-tokenStart < sizeTable {
		return nil, 0, ErrBufferTooSmall
	}
	if scratch == nil {
		// Heap-allocate a per-call PartitionScratch so the buffers
		// outlive this function frame; preserves the historical
		// allocate-on-each-call behaviour for legacy callers.
		scratch = &PartitionScratch{}
	}
	scratch.ensureCapacity(pc, len(dst))
	for i := range pc {
		writers[i].Init(scratch.buffers[i])
	}
	return scratch, pc, nil
}

// finalizePartitionedTokenPayload finishes the BoolWriters, writes the
// inter-partition size table into dst at tokenStart, and copies each
// partition's bytes into dst after the size table. Mirrors the trailing
// half of the original writePartitionedTokenPayload helper.
func finalizePartitionedTokenPayload(scratch *PartitionScratch, writers *[8]BoolWriter, dst []byte, tokenStart int, partitionCount int) (int, error) {
	sizeTableBytes := 3 * (partitionCount - 1)
	offset := tokenStart + sizeTableBytes
	for i := range partitionCount {
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
		// scratch.buffers[i] is the underlying buffer the writer wrote into.
		copy(dst[offset:], scratch.buffers[i][:size])
		offset += size
	}
	return offset, nil
}
