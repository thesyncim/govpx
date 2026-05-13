package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 partition-tree decode primitives. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c (read_partition,
// dec_partition_plane_context) and vp9/common/vp9_onyxc_int.h
// (partition_plane_context).

// PartitionPlaneContext computes the 4-state partition context from
// the partition-history bits stored in the above-row and left-column
// segmentation contexts. Mirrors partition_plane_context.
//
// `aboveSegCtx` and `leftSegCtx` are caller-owned arrays sized to the
// frame's mi-column count (above) and MI_BLOCK_SIZE (= 8, left).
// `bsize` selects the bit position; the result is in
// [0, PARTITION_CONTEXTS).
func PartitionPlaneContext(aboveSegCtx, leftSegCtx []int8, miRow, miCol int, bsize common.BlockSize) int {
	bsl := common.MiWidthLog2Lookup[bsize]
	above := (int(aboveSegCtx[miCol]) >> bsl) & 1
	left := (int(leftSegCtx[miRow&common.MiMask]) >> bsl) & 1
	return left*2 + above + int(bsl)*common.PartitionPloffset
}

// ReadPartition mirrors read_partition. The (hasRows, hasCols) flags
// describe whether the inner halves of the current block fit inside
// the frame; when one or both don't fit, the partition decode falls
// back to a 1-bit branch or is forced to SPLIT.
//
// `probs` is the 3-element partition-prob row for the active context
// (caller-loaded from FrameContext.PartitionProb[ctx]).
func ReadPartition(r *bitstream.Reader, probs []uint8, hasRows, hasCols bool) common.PartitionType {
	switch {
	case hasRows && hasCols:
		return common.PartitionType(r.ReadTree(common.PartitionTree[:], probs))
	case !hasRows && hasCols:
		if r.Read(uint32(probs[1])) != 0 {
			return common.PartitionSplit
		}
		return common.PartitionHorz
	case hasRows && !hasCols:
		if r.Read(uint32(probs[2])) != 0 {
			return common.PartitionSplit
		}
		return common.PartitionVert
	default:
		return common.PartitionSplit
	}
}
