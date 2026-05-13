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

// UpdatePartitionContext mirrors libvpx's
// dec_update_partition_context. After a partition decision is made
// at a given (miRow, miCol, subsize, bw), the partition-history bits
// above and to the left of the current tile slot are stamped with
// the per-subsize "above" / "left" partition_context_lookup values.
// Mirrors the dual-memset libvpx does at the end of read_partition's
// caller.
//
// `bw` is the count of 8x8 columns the subsize spans (1, 2, 4, or 8).
func UpdatePartitionContext(aboveSegCtx, leftSegCtx []int8, miRow, miCol int, subsize common.BlockSize, bw int) {
	ent := common.PartitionContextLookup[subsize]
	for i := 0; i < bw; i++ {
		aboveSegCtx[miCol+i] = int8(ent.Above)
		leftSegCtx[(miRow&common.MiMask)+i] = int8(ent.Left)
	}
}

// TileBounds carries the per-tile mi-coordinate bounds the recursive
// partition walk consults. Mirrors libvpx's TileInfo.mi_col_start /
// mi_col_end pair plus the frame-level mi_rows / mi_cols fallback
// row gate.
type TileBounds struct {
	MiColStart int
	MiColEnd   int
	MiRowStart int
	MiRowEnd   int
}

// IsInside mirrors libvpx's is_inside in vp9_mvref_common.h. Returns
// true iff (miRow + miPos.Row, miCol + miPos.Col) lands inside the
// tile + frame bounds — the gate the MV-ref candidate scan and the
// recursive partition walk use to skip out-of-tile neighbors.
//
// `miRows` is the frame-level cm->mi_rows; libvpx also bounds with
// cm->mi_rows above and the tile-relative miColStart/miColEnd below.
func IsInside(tile TileBounds, miRows, miRow, miCol int, posRow, posCol int) bool {
	r := miRow + posRow
	c := miCol + posCol
	return r >= 0 && c >= tile.MiColStart && r < miRows && c < tile.MiColEnd
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
