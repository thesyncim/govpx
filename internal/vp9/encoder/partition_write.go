package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 partition-tree walker + per-block dispatcher. Ported from
// libvpx v1.16.0 vp9/encoder/vp9_bitstream.c — write_partition,
// write_modes_sb, write_modes_b. The leaf block-mode emission is
// kept abstract behind a callback so callers can plug in either
// WriteKeyframeBlock or WriteInterBlock + the per-leaf coefficient
// writers without WriteModesSb itself knowing the frame type.
//
// The walker mirrors the C source's "partition then dispatch"
// recursion exactly: each call emits the partition bits for the
// current (mi_row, mi_col, bsize) cell and either descends (split)
// or invokes the leaf callback at the geometry chosen by the
// partition. The partition context is updated in place against the
// caller-owned above/left segmentation context arrays so the next
// neighbor read sees the same state libvpx's update_partition_context
// would have left.

// WriteModesSbArgs bundles the inputs WriteModesSb consults across
// recursion levels. Fields stay constant for the duration of a single
// super-block walk; per-level state (miRow, miCol, bsize) is passed
// as explicit arguments to keep WriteModesSb itself stack-only.
type WriteModesSbArgs struct {
	// AboveSegCtx + LeftSegCtx are the partition-history arrays used
	// by PartitionPlaneContext. Sized cm->mi_cols and MiBlockSize
	// respectively. Both are written in place when a partition decision
	// is committed.
	AboveSegCtx []int8
	LeftSegCtx  []int8

	// Frame extents in mi units; the recursion gates on (miRow + bs <
	// MiRows) and (miCol + bs < MiCols) to mirror libvpx's edge handling.
	MiRows int
	MiCols int

	// PartitionProbs is the FrameContext PartitionProb table (per-ctx
	// 3-prob rows). Indexed by PartitionPlaneContext result.
	PartitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8

	// GetMi returns the per-cell NeighborMi at (miRow, miCol). For the
	// partition walker only mi.SbType is consulted (to recover the
	// partition decision at this level via PartitionLookup).
	GetMi func(miRow, miCol int) *vp9dec.NeighborMi

	// WriteB is invoked at every partition leaf with (miRow, miCol,
	// subsize). The caller drives the actual mode + coefficient
	// emission — typically WriteKeyframeBlock or WriteInterBlock
	// followed by the per-plane coefficient walk.
	WriteB func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize)
}

// WritePartitionForBlock mirrors libvpx's write_partition. Computes
// the partition_plane_context for (miRow, miCol, bsize), picks the
// matching probability row, and emits the partition decision using
// the (hasRows, hasCols) edge-gated wire format already handled by
// WritePartition.
func WritePartitionForBlock(bw *bitstream.Writer, a WriteModesSbArgs,
	miRow, miCol int, p common.PartitionType, bsize common.BlockSize, hbs int,
) {
	ctx := vp9dec.PartitionPlaneContext(a.AboveSegCtx, a.LeftSegCtx,
		miRow, miCol, bsize)
	probs := a.PartitionProbs[ctx][:]
	hasRows := (miRow + hbs) < a.MiRows
	hasCols := (miCol + hbs) < a.MiCols
	WritePartition(bw, p, probs, hasRows, hasCols)
}

// WriteModesSb mirrors libvpx's write_modes_sb. Recursively walks the
// partition tree rooted at (miRow, miCol, bsize), emitting the
// partition bits at each node and dispatching to a.WriteB at every
// leaf. The recursion + partition-context update mirror the C source
// step for step.
//
// The recursion runs without heap allocation: all per-level state
// lives on the stack and a.WriteB is the only branch out.
func WriteModesSb(bw *bitstream.Writer, a WriteModesSbArgs,
	miRow, miCol int, bsize common.BlockSize,
) {
	if miRow >= a.MiRows || miCol >= a.MiCols {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4

	mi := a.GetMi(miRow, miCol)
	partition := common.PartitionLookup[bsl][mi.SbType]
	WritePartitionForBlock(bw, a, miRow, miCol, partition, bsize, bs)
	subsize := common.SubsizeLookup[partition][bsize]

	if subsize < common.Block8x8 {
		a.WriteB(bw, miRow, miCol, subsize)
	} else {
		switch partition {
		case common.PartitionNone:
			a.WriteB(bw, miRow, miCol, subsize)
		case common.PartitionHorz:
			a.WriteB(bw, miRow, miCol, subsize)
			if miRow+bs < a.MiRows {
				a.WriteB(bw, miRow+bs, miCol, subsize)
			}
		case common.PartitionVert:
			a.WriteB(bw, miRow, miCol, subsize)
			if miCol+bs < a.MiCols {
				a.WriteB(bw, miRow, miCol+bs, subsize)
			}
		default: // PartitionSplit
			WriteModesSb(bw, a, miRow, miCol, subsize)
			WriteModesSb(bw, a, miRow, miCol+bs, subsize)
			WriteModesSb(bw, a, miRow+bs, miCol, subsize)
			WriteModesSb(bw, a, miRow+bs, miCol+bs, subsize)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(a.AboveSegCtx, a.LeftSegCtx,
			miRow, miCol, subsize, bs)
	}
}
