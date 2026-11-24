package encoder

import (
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
