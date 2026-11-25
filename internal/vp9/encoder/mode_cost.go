package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
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

// newMvDiscountFactor mirrors libvpx NEW_MV_DISCOUNT_FACTOR
// (vp9/encoder/vp9_rdopt.c:57).
const newMvDiscountFactor = 8

// CostMvRef returns libvpx's cost_mv_ref (vp9/encoder/vp9_rdopt.c:1551-1555):
// cpi->inter_mode_cost[mode_context][INTER_OFFSET(mode)], i.e. the inter-mode
// tree bits ONLY (no MV bit cost). For the predictor modes this is exactly the
// InterModeRateCost; for NEWMV it is InterModeRateCost minus the MV bit cost.
func CostMvRef(fc *vp9dec.FrameContext, ctx int, mode common.PredictionMode) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.InterModeProbs) {
		return 0
	}
	probs := fc.InterModeProbs[ctx]
	switch mode {
	case common.ZeroMv:
		return VP9CostBit(probs[0], 0)
	case common.NearestMv:
		return VP9CostBit(probs[0], 1) + VP9CostBit(probs[1], 0)
	case common.NearMv:
		return VP9CostBit(probs[0], 1) + VP9CostBit(probs[1], 1) +
			VP9CostBit(probs[2], 0)
	case common.NewMv:
		return VP9CostBit(probs[0], 1) + VP9CostBit(probs[1], 1) +
			VP9CostBit(probs[2], 1)
	default:
		return 0
	}
}

// DiscountNewMvTest ports libvpx's discount_newmv_test for the default
// (CONFIG_NON_GREEDY_MV == 0) build (vp9/encoder/vp9_rdopt.c:2798-2807):
//
//	return (!cpi->rc.is_src_frame_alt_ref && (this_mode == NEWMV) &&
//	        (this_mv.as_int != 0) &&
//	        ((mode_mv[NEARESTMV][ref_frame].as_int == 0) ||
//	         (mode_mv[NEARESTMV][ref_frame].as_int == INVALID_MV)) &&
//	        ((mode_mv[NEARMV][ref_frame].as_int == 0) ||
//	         (mode_mv[NEARMV][ref_frame].as_int == INVALID_MV)));
//
// The nearest/near MVs are passed as (mv, valid) pairs: valid==false models the
// INVALID_MV slot (an unavailable predictor), which the test treats the same as
// a zero MV.
func DiscountNewMvTest(isSrcFrameAltRef bool, mode common.PredictionMode,
	thisMv vp9dec.MV, nearestMv vp9dec.MV, nearestValid bool,
	nearMv vp9dec.MV, nearValid bool,
) bool {
	if isSrcFrameAltRef || mode != common.NewMv || thisMv == (vp9dec.MV{}) {
		return false
	}
	nearestZeroOrInvalid := !nearestValid || nearestMv == (vp9dec.MV{})
	nearZeroOrInvalid := !nearValid || nearMv == (vp9dec.MV{})
	return nearestZeroOrInvalid && nearZeroOrInvalid
}

// InterModeMvRateWithDiscount returns the combined cost_mv_ref + MV-bit-cost
// rate for one single-reference inter mode, applying libvpx's NEWMV discount
// when discount is true. It is the genuine handle_inter_mode rate2 contribution
// from the mode + MV (vp9/encoder/vp9_rdopt.c:2936-2941 for the MV cost and
// :2970-2977 for cost_mv_ref):
//
//	// MV cost (NEWMV only)
//	if (discount) *rate2 += VPXMAX(rate_mv / NEW_MV_DISCOUNT_FACTOR, 1);
//	else          *rate2 += rate_mv;
//	// cost_mv_ref
//	if (discount)
//	  *rate2 += VPXMIN(cost_mv_ref(this_mode), cost_mv_ref(NEARESTMV));
//	else
//	  *rate2 += cost_mv_ref(this_mode);
//
// For non-NEWMV modes there is no MV cost and no discount, so the result is
// exactly cost_mv_ref(mode) == InterModeRateCost(mode).
func InterModeMvRateWithDiscount(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv vp9dec.MV, allowHP bool, discount bool,
) int {
	if mode != common.NewMv {
		return CostMvRef(fc, ctx, mode)
	}
	rateMv := MvBitCost(mv, refMv, &fc.Nmvc, allowHP)
	costMvRef := CostMvRef(fc, ctx, common.NewMv)
	if discount {
		discMv := max(rateMv/newMvDiscountFactor, 1)
		discCostMvRef := min(costMvRef, CostMvRef(fc, ctx, common.NearestMv))
		return discMv + discCostMvRef
	}
	return rateMv + costMvRef
}

// VP9 per-block mode-info writers. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — write_intra_mode, write_inter_mode,
// write_selected_tx_size, write_skip, write_segment_id (already
// done), and the small tree-encoding tables intra/inter/partition/
// switchable_interp share.

// IntraModeEncodings mirrors intra_mode_encodings[INTRA_MODES]
// from libvpx. Indexed by PredictionMode in [DcPred, TmPred].
var IntraModeEncodings = [common.IntraModes]valLen{
	{0, 1},   // DcPred  "0"
	{6, 3},   // VPred   "110"
	{28, 5},  // HPred   "11100"
	{30, 5},  // D45Pred "11110"
	{58, 6},  // D135Pred"111010"
	{59, 6},  // D117Pred"111011"
	{126, 7}, // D153Pred"1111110"
	{127, 7}, // D207Pred"1111111"
	{62, 6},  // D63Pred "111110"
	{2, 2},   // TmPred  "10"
}

// InterModeEncodings mirrors inter_mode_encodings[INTER_MODES] —
// the sub-mode patterns walking InterModeTree to NEARESTMV/NEARMV/
// ZEROMV/NEWMV. The libvpx caller uses INTER_OFFSET(mode) to
// translate the absolute PredictionMode into the 0..3 sub-mode
// index before indexing here.
var InterModeEncodings = [common.InterModes]valLen{
	{2, 2}, // NEARESTMV  "10"
	{6, 3}, // NEARMV     "110"
	{0, 1}, // ZEROMV     "0"
	{7, 3}, // NEWMV      "111"
}

// PartitionEncodings mirrors partition_encodings[PARTITION_TYPES] —
// patterns walking PartitionTree to None / Horz / Vert / Split.
var PartitionEncodings = [common.PartitionTypes]valLen{
	{0, 1}, // PartitionNone  "0"
	{2, 2}, // PartitionHorz  "10"
	{6, 3}, // PartitionVert  "110"
	{7, 3}, // PartitionSplit "111"
}

// SwitchableInterpEncodings mirrors switchable_interp_encodings[3]
// — the three patterns walking SwitchableInterpTree to
// EIGHTTAP / EIGHTTAP_SMOOTH / EIGHTTAP_SHARP.
var SwitchableInterpEncodings = [3]valLen{
	{0, 1}, // EIGHTTAP        "0"
	{2, 2}, // EIGHTTAP_SMOOTH "10"
	{3, 2}, // EIGHTTAP_SHARP  "11"
}

// WriteIntraMode mirrors libvpx's write_intra_mode — walks
// IntraModeTree against the supplied 9-element probability row
// (size group's Y-mode probs for an inter-frame intra block, or
// kfYModeProb[above][left] for keyframes).
func WriteIntraMode(bw *bitstream.Writer, mode common.PredictionMode, probs []uint8) {
	enc := IntraModeEncodings[mode]
	writeTreeBits(bw, common.IntraModeTree[:], probs, enc.value, enc.length)
}

// WriteInterMode mirrors libvpx's write_inter_mode — takes the
// absolute PredictionMode (NearestMv..NewMv) and emits the
// matching sub-mode pattern against the 7-context probability row.
func WriteInterMode(bw *bitstream.Writer, mode common.PredictionMode, probs []uint8) {
	sub := int(mode) - int(common.NearestMv)
	enc := InterModeEncodings[sub]
	writeTreeBits(bw, common.InterModeTree[:], probs, enc.value, enc.length)
}

// WritePartition mirrors libvpx's write_partition_tree-and-encode
// pattern. When both (hasRows, hasCols) are true the full 4-way
// tree walks against `probs[0..2]`; the boundary forms emit a
// single bit (rows-only / cols-only) or no bit at all (neither).
func WritePartition(bw *bitstream.Writer, p common.PartitionType, probs []uint8, hasRows, hasCols bool) {
	switch {
	case hasRows && hasCols:
		enc := PartitionEncodings[p]
		writeTreeBits(bw, common.PartitionTree[:], probs, enc.value, enc.length)
	case !hasRows && hasCols:
		// 1-bit choice between Horz (0) and Split (1).
		if p == common.PartitionSplit {
			bw.Write(1, uint32(probs[1]))
		} else {
			bw.Write(0, uint32(probs[1]))
		}
	case hasRows && !hasCols:
		if p == common.PartitionSplit {
			bw.Write(1, uint32(probs[2]))
		} else {
			bw.Write(0, uint32(probs[2]))
		}
	}
}

// WriteSwitchableInterpFilter mirrors the libvpx write call that
// emits the per-block interp filter — assumes a non-Switchable
// frame-level filter has already been gated out.
func WriteSwitchableInterpFilter(bw *bitstream.Writer, f int, probs []uint8) {
	enc := SwitchableInterpEncodings[f]
	writeTreeBits(bw, common.SwitchableInterpTree[:], probs, enc.value, enc.length)
}

// WriteSelectedTxSize mirrors write_selected_tx_size — emits the
// cascade libvpx uses when TxModeSelect is in effect: a 1-bit
// "not Tx4x4", then if max >= Tx16x16 another "not Tx8x8", then
// if max >= Tx32x32 another "is Tx32x32" bit.
//
// `txProbs` is the 3-element row picked by (maxTxSize, ctx) from
// fc.TxProbs.
func WriteSelectedTxSize(bw *bitstream.Writer, txSize, maxTxSize common.TxSize, txProbs []uint8) {
	// First bit: 0 = Tx4x4, 1 = not Tx4x4.
	if txSize == common.Tx4x4 {
		bw.Write(0, uint32(txProbs[0]))
		return
	}
	bw.Write(1, uint32(txProbs[0]))
	if maxTxSize < common.Tx16x16 {
		return
	}
	// Second bit: 0 = stop at Tx8x8, 1 = not Tx8x8.
	if txSize == common.Tx8x8 {
		bw.Write(0, uint32(txProbs[1]))
		return
	}
	bw.Write(1, uint32(txProbs[1]))
	if maxTxSize < common.Tx32x32 {
		return
	}
	// Third bit: 0 = Tx16x16, 1 = Tx32x32.
	if txSize == common.Tx16x16 {
		bw.Write(0, uint32(txProbs[2]))
		return
	}
	bw.Write(1, uint32(txProbs[2]))
}
