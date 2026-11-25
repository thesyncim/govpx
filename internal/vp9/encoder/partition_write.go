package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// SquareInterPartitionSizes returns the horizontal, vertical, and split
// children considered by the square-block inter partition picker.
func SquareInterPartitionSizes(root common.BlockSize) (common.BlockSize, common.BlockSize, common.BlockSize, bool) {
	switch root {
	case common.Block64x64, common.Block32x32, common.Block16x16:
		return common.SubsizeLookup[common.PartitionHorz][root],
			common.SubsizeLookup[common.PartitionVert][root],
			common.SubsizeLookup[common.PartitionSplit][root],
			true
	default:
		return common.BlockInvalid, common.BlockInvalid, common.BlockInvalid, false
	}
}

// InterRDPartitionSizes returns the partition children considered by recursive
// inter RD search. Block8x8 is included because the RD picker can still score
// sub-8x8 leaf partitions.
func InterRDPartitionSizes(root common.BlockSize) (common.BlockSize, common.BlockSize, common.BlockSize, bool) {
	switch root {
	case common.Block64x64, common.Block32x32, common.Block16x16:
		return common.SubsizeLookup[common.PartitionHorz][root],
			common.SubsizeLookup[common.PartitionVert][root],
			common.SubsizeLookup[common.PartitionSplit][root],
			true
	case common.Block8x8:
		return common.Block8x4, common.Block4x8, common.Block4x4, true
	default:
		return common.BlockInvalid, common.BlockInvalid, common.BlockInvalid, false
	}
}

// VisibleBlockFits reports whether a visible block rectangle is fully inside
// the supplied plane dimensions.
func VisibleBlockFits(x0, y0, blockW, blockH, width, height int) bool {
	if x0 < 0 || y0 < 0 {
		return false
	}
	if blockW <= 0 || blockH <= 0 {
		return false
	}
	return x0+blockW <= width && y0+blockH <= height
}

// CBRVariancePartitionThreshold mirrors the CBR/realtime variance
// choose-partitioning threshold ladder used by VP9 inter partitioning.
func CBRVariancePartitionThreshold(yAcDequant int16, width, height int,
	bsize common.BlockSize, avgInterQ uint8,
) uint64 {
	if yAcDequant <= 0 {
		return 0
	}
	base := uint64(yAcDequant)
	if width <= 640 && height <= 480 {
		base = (5 * base) >> 2
	}
	switch {
	case width <= 352 && height <= 288:
		switch bsize {
		case common.Block64x64:
			return base >> 3
		case common.Block32x32:
			return base >> 1
		case common.Block16x16:
			threshold := base << 3
			if avgInterQ > 220 {
				return threshold << 2
			}
			if avgInterQ > 200 {
				return threshold << 1
			}
			return threshold
		}
	case width < 1280 && height < 720:
		if bsize == common.Block32x32 {
			return (5 * base) >> 2
		}
	case width < 1920 && height < 1080:
		if bsize == common.Block32x32 {
			return base << 1
		}
	default:
		if bsize == common.Block32x32 {
			return (5 * base) >> 1
		}
	}
	if bsize == common.Block16x16 {
		return base << 8
	}
	return base
}

// CBRVariancePartitionSADThreshold returns the CBR variance partition SAD
// threshold for the supplied luma AC dequant and frame size.
func CBRVariancePartitionSADThreshold(yAcDequant int16, width, height int) uint64 {
	if width <= 352 && height <= 288 {
		return 10
	}
	threshold := max(int(yAcDequant)<<1, 1000)
	return uint64(threshold)
}

// RealtimeVariancePartitionThreshold64 returns the realtime speed-8 64x64
// variance threshold for flat LAST-frame temporal deltas.
func RealtimeVariancePartitionThreshold64(yAcDequant int16, width, height int) uint64 {
	if yAcDequant <= 0 {
		return 0
	}
	base := uint64(yAcDequant)
	if width <= 640 && height <= 480 {
		base = (5 * base) >> 2
	}
	return base
}

// PartitionRateCost returns the cost of coding one partition token at a
// partition-tree edge.
func PartitionRateCost(
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	ctx int, partition common.PartitionType, hasRows, hasCols bool,
) int {
	if partitionProbs == nil || ctx < 0 || ctx >= common.PartitionContexts {
		return 0
	}
	probs := partitionProbs[ctx]
	switch {
	case hasRows && hasCols:
		switch partition {
		case common.PartitionNone:
			return VP9CostBit(probs[0], 0)
		case common.PartitionHorz:
			return VP9CostBit(probs[0], 1) +
				VP9CostBit(probs[1], 0)
		case common.PartitionVert:
			return VP9CostBit(probs[0], 1) +
				VP9CostBit(probs[1], 1) +
				VP9CostBit(probs[2], 0)
		case common.PartitionSplit:
			return VP9CostBit(probs[0], 1) +
				VP9CostBit(probs[1], 1) +
				VP9CostBit(probs[2], 1)
		}
	case !hasRows && hasCols:
		bit := 0
		if partition == common.PartitionSplit {
			bit = 1
		}
		return VP9CostBit(probs[1], bit)
	case hasRows && !hasCols:
		bit := 0
		if partition == common.PartitionSplit {
			bit = 1
		}
		return VP9CostBit(probs[2], bit)
	}
	return 0
}

// SwitchableInterpRateCost returns the VP9 tree cost for a switchable
// interpolation filter at the supplied context.
func SwitchableInterpRateCost(fc *vp9dec.FrameContext, ctx int,
	filter vp9dec.InterpFilter,
) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.SwitchableInterpProb) ||
		filter >= vp9dec.InterpSwitchable {
		return 0
	}
	probs := fc.SwitchableInterpProb[ctx]
	switch filter {
	case vp9dec.InterpEighttap:
		return VP9CostBit(probs[0], 0)
	case vp9dec.InterpEighttapSmooth:
		return VP9CostBit(probs[0], 1) +
			VP9CostBit(probs[1], 0)
	case vp9dec.InterpEighttapSharp:
		return VP9CostBit(probs[0], 1) +
			VP9CostBit(probs[1], 1)
	default:
		return 0
	}
}

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
		// libvpx's dec_update_partition_context stamps the full block
		// width (num_8x8_wh = 1 << n8x8_l2 = 2*bs in our indexing), not
		// the half-step bs the recursion uses for partition geometry.
		// Passing bs here matches the SUB-block coverage and leaves
		// the [miCol+bs..miCol+2bs) range stale, which the decoder
		// observes on the NEXT SB's partition_plane_context lookup
		// and rejects the frame.
		vp9dec.UpdatePartitionContext(a.AboveSegCtx, a.LeftSegCtx,
			miRow, miCol, subsize, 2*bs)
	}
}
