package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// InterModeRateCost returns the cost of coding one VP9 inter mode and its
// motion vector against the supplied frame context.
func InterModeRateCost(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv vp9dec.MV, allowHP bool,
) int {
	return InterModeRateCostN(fc, ctx, mode,
		[2]vp9dec.MV{mv}, [2]vp9dec.MV{refMv}, 1, allowHP)
}

// InterModeRateCostN returns the cost of coding one VP9 inter mode and one or
// two motion vectors against the supplied frame context.
func InterModeRateCostN(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv [2]vp9dec.MV, nrefs int, allowHP bool,
) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.InterModeProbs) {
		return 0
	}
	if nrefs < 1 {
		nrefs = 1
	}
	if nrefs > len(mv) {
		nrefs = len(mv)
	}
	probs := fc.InterModeProbs[ctx]
	cost := 0
	switch mode {
	case common.ZeroMv:
		cost = VP9CostBit(probs[0], 0)
	case common.NearestMv:
		cost = VP9CostBit(probs[0], 1) +
			VP9CostBit(probs[1], 0)
	case common.NearMv:
		cost = VP9CostBit(probs[0], 1) +
			VP9CostBit(probs[1], 1) +
			VP9CostBit(probs[2], 0)
	case common.NewMv:
		cost = VP9CostBit(probs[0], 1) +
			VP9CostBit(probs[1], 1) +
			VP9CostBit(probs[2], 1)
		for ref := 0; ref < nrefs; ref++ {
			cost += MvBitCost(mv[ref], refMv[ref], &fc.Nmvc, allowHP)
		}
	default:
		return 0
	}
	return cost
}

// MvBitCost mirrors libvpx's vp9_mv_bit_cost weighting.
func MvBitCost(mv, ref vp9dec.MV, ctx *vp9dec.NmvContext, allowHP bool) int {
	// libvpx vp9_mcomp.c:80-84:
	//   vp9_mv_bit_cost(..., MV_COST_WEIGHT)
	//   ROUND_POWER_OF_TWO(mv_cost(diff) * 108, 7)
	const mvCostWeight = 108
	raw := MvCostWithHP(mv, ref, ctx, allowHP)
	return (raw*mvCostWeight + 64) >> 7
}

// MvBitCostSub mirrors libvpx's vp9_mv_bit_cost with MV_COST_WEIGHT_SUB (120),
// the weight set_and_cost_bmi_mvs uses for sub-8x8 segment MVs
// (vp9/encoder/vp9_rdopt.c:1574,1578; vp9/encoder/vp9_rd.h:39).
func MvBitCostSub(mv, ref vp9dec.MV, ctx *vp9dec.NmvContext, allowHP bool) int {
	const mvCostWeightSub = 120
	raw := MvCostWithHP(mv, ref, ctx, allowHP)
	return (raw*mvCostWeightSub + 64) >> 7
}

// IntraInterRateCost returns the probability cost of coding the intra/inter
// decision bit for the supplied neighbors.
func IntraInterRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, isInter int,
) int {
	if fc == nil {
		return 0
	}
	if isInter != 0 {
		isInter = 1
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	return VP9CostBit(fc.IntraInterProb[ctx], isInter)
}

// ReferenceModeRateCost returns the cost of the single/compound reference
// decision bit when the frame allows reference-mode selection.
func ReferenceModeRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, isCompound bool,
) int {
	if fc == nil || frameMode != vp9dec.ReferenceModeSelect {
		return 0
	}
	ctx := vp9dec.GetReferenceModeContext(above, left, refs)
	bit := 0
	if isCompound {
		bit = 1
	}
	return VP9CostBit(fc.ReferenceModeProbs.CompInterProb[ctx], bit)
}

// SingleRefModeRateCost returns the full inter-reference signaling cost for a
// single-reference VP9 inter block.
func SingleRefModeRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, refFrame int8,
) int {
	return IntraInterRateCost(fc, above, left, 1) +
		ReferenceModeRateCost(fc, above, left, frameMode, refs, false) +
		SingleRefRateCost(fc, above, left, refFrame)
}

// SingleRefRateCost returns the LAST/GOLDEN/ALTREF reference signaling cost.
func SingleRefRateCost(fc *vp9dec.FrameContext, above, left *vp9dec.NeighborMi,
	refFrame int8,
) int {
	if fc == nil || refFrame <= vp9dec.IntraFrame {
		return 0
	}
	ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
	bit0 := 0
	if refFrame != vp9dec.LastFrame {
		bit0 = 1
	}
	cost := VP9CostBit(fc.ReferenceModeProbs.SingleRefProb[ctx0][0], bit0)
	if bit0 == 0 {
		return cost
	}
	ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
	bit1 := 0
	if refFrame != vp9dec.GoldenFrame {
		bit1 = 1
	}
	return cost + VP9CostBit(fc.ReferenceModeProbs.SingleRefProb[ctx1][1], bit1)
}

// CompoundRefRateCost returns the compound-reference signaling cost and false
// when the supplied reference pair is not a legal VP9 compound pair.
func CompoundRefRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, signBias [vp9dec.MaxRefFrames]uint8,
	refFrame [2]int8,
) (int, bool) {
	if fc == nil || frameMode == vp9dec.SingleReference {
		return 0, false
	}
	idx := int(signBias[refs.CompFixedRef])
	if idx < 0 || idx > 1 || refFrame[idx] != refs.CompFixedRef {
		return 0, false
	}
	varRef := refFrame[1-idx]
	bit := 0
	switch varRef {
	case refs.CompVarRef[0]:
	case refs.CompVarRef[1]:
		bit = 1
	default:
		return 0, false
	}
	ctx := vp9dec.GetPredContextCompRefP(above, left, refs, signBias)
	cost := ReferenceModeRateCost(fc, above, left, frameMode, refs, true)
	cost += VP9CostBit(fc.ReferenceModeProbs.CompRefProb[ctx], bit)
	return cost, true
}
