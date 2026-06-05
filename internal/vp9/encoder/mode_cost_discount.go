package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
		discMv := rateMv / newMvDiscountFactor
		if discMv < 1 {
			discMv = 1
		}
		discCostMvRef := CostMvRef(fc, ctx, common.NearestMv)
		if costMvRef < discCostMvRef {
			discCostMvRef = costMvRef
		}
		return discMv + discCostMvRef
	}
	return rateMv + costMvRef
}
