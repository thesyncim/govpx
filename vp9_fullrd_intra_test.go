package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// The expected per-mode bit costs below were produced by running libvpx's
// vp9_cost_tokens over the relevant probability rows of vp9_intra_mode_tree.
// They were captured with a standalone C harness that links the verbatim
// libvpx v1.16.0 tables and walker (vp9/encoder/vp9_cost.c vp9_cost_tokens,
// vp9/common/vp9_entropymode.c vp9_intra_mode_tree / vp9_kf_y_mode_prob /
// vp9_kf_uv_mode_prob / default_if_y_probs / default_if_uv_probs, and the
// vp9_prob_cost[256] table from vp9/encoder/vp9_cost.c). The harness was a
// byte-for-byte copy of those tables/functions; it was run once to read the
// values and then discarded (not committed). The same values are what
// fill_mode_costs (vp9/encoder/vp9_rd.c:92-110) stores into
// cpi->mbmode_cost / cpi->y_mode_costs / cpi->intra_uv_mode_cost.
//
// Mode order is the PredictionMode enum DC_PRED..TM_PRED (0..9):
//   {DC, V, H, D45, D135, D117, D153, D207, D63, TM}.

// mbmode_cost = vp9_cost_tokens(default_if_y_probs[1], vp9_intra_mode_tree).
// libvpx vp9_rd.c:103, consumed at vp9_rdopt.c:3864.
var wantVP9MbmodeCost = [common.IntraModes]int{
	489, 2724, 1263, 2865, 2728, 3603, 2727, 2117, 3095, 1514,
}

// intra_uv_mode_cost[INTER_FRAME][DC_PRED] =
//
//	vp9_cost_tokens(default_if_uv_probs[DC_PRED], vp9_intra_mode_tree).
//
// libvpx vp9_rd.c:107-108, consumed at vp9_rdopt.c:1496.
var wantVP9InterUVCostDC = [common.IntraModes]int{
	560, 1384, 1177, 3241, 2784, 2761, 2540, 2247, 2841, 3126,
}

// y_mode_costs[V_PRED][H_PRED] =
//
//	vp9_cost_tokens(vp9_kf_y_mode_prob[V_PRED][H_PRED], vp9_intra_mode_tree).
//
// libvpx vp9_rd.c:97-100, consumed at vp9_rdopt.c:1379.
var wantVP9KeyframeYCostVH = [common.IntraModes]int{
	1301, 1207, 987, 2379, 2217, 2812, 2407, 1784, 2444, 1748,
}

// intra_uv_mode_cost[KEY_FRAME][DC_PRED] =
//
//	vp9_cost_tokens(vp9_kf_uv_mode_prob[DC_PRED], vp9_intra_mode_tree).
//
// libvpx vp9_rd.c:104-106, consumed at vp9_rdopt.c:1496.
var wantVP9KeyframeUVCostDC = [common.IntraModes]int{
	425, 1792, 1380, 2788, 2739, 2762, 2493, 2261, 2763, 2936,
}

func TestVP9FullRDInterIntraYModeCostMatchesLibvpx(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	var got [common.IntraModes]int
	vp9FullRDInterIntraYModeCosts(got[:], &fc)
	if got != wantVP9MbmodeCost {
		t.Fatalf("mbmode_cost = %v, libvpx = %v", got, wantVP9MbmodeCost)
	}

	// Guard against the historical divergence: keying on
	// size_group_lookup[bsize] instead of the fixed index 1 produces a
	// different table for any size group != 1. Size group 2 (e.g. BLOCK_16X16)
	// must NOT equal the mbmode_cost row.
	var sg2 [common.IntraModes]int
	encoder.VP9CostTokens(sg2[:], fc.YModeProb[2][:], common.IntraModeTree[:])
	if sg2 == wantVP9MbmodeCost {
		t.Fatalf("size group 2 cost row unexpectedly equals mbmode_cost; "+
			"the test cannot detect the size-group divergence (sg2=%v)", sg2)
	}
}

func TestVP9FullRDIntraUVModeCostInterMatchesLibvpx(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	var got [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(got[:], vp9FullRDInterFrame, common.DcPred, &fc)
	if got != wantVP9InterUVCostDC {
		t.Fatalf("intra_uv_mode_cost[INTER][DC] = %v, libvpx = %v", got, wantVP9InterUVCostDC)
	}
}

func TestVP9FullRDKeyframeYModeCostMatchesLibvpx(t *testing.T) {
	var got [common.IntraModes]int
	vp9FullRDKeyframeYModeCosts(got[:], common.VPred, common.HPred)
	if got != wantVP9KeyframeYCostVH {
		t.Fatalf("y_mode_costs[V][H] = %v, libvpx = %v", got, wantVP9KeyframeYCostVH)
	}
}

func TestVP9FullRDIntraUVModeCostKeyframeMatchesLibvpx(t *testing.T) {
	// KEY_FRAME UV cost ignores fc; pass an empty context to confirm the
	// keyframe branch reads vp9_kf_uv_mode_prob and not fc->uv_mode_prob.
	var fc vp9dec.FrameContext
	var got [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(got[:], vp9FullRDKeyFrame, common.DcPred, &fc)
	if got != wantVP9KeyframeUVCostDC {
		t.Fatalf("intra_uv_mode_cost[KEY][DC] = %v, libvpx = %v", got, wantVP9KeyframeUVCostDC)
	}
}

// TestVP9FullRDIntraModeRDDecision pins one concrete intra-mode RD comparison
// exactly as libvpx's rd_pick_intra_sby_mode does it:
//
//	this_rate = this_rate_tokenonly + bmode_costs[mode];
//	this_rd   = RDCOST(x->rdmult, x->rddiv, this_rate, this_distortion);
//	if (this_rd < best_rd) best_rd = this_rd; mode_selected = mode;
//
// (libvpx vp9/encoder/vp9_rdopt.c:1398-1407). The decision compares two
// candidate modes under a fixed rdmult and the verbatim mbmode_cost values.
// rddiv is RD_DIV_BITS (encoder.RDDivBits = 7); RDCOST is
//
//	((128 * rate * rdmult) >> 8 (RD_EPB_SHIFT-fold)) + (dist << rddiv)
//
// as implemented by encoder.RDCost. The expected best mode is computed from
// the same formula by hand below so the pin doubles as a sanity cross-check.
func TestVP9FullRDIntraModeRDDecision(t *testing.T) {
	// Use the keyframe Y rdmult at a representative qindex so the numbers are
	// concrete. KeyframeRDMul ports vp9's q*q*(4350+q)/1000.
	const qindex = 64
	rdmult := encoder.KeyframeRDMul(qindex)

	// Candidate A: V_PRED with a small token rate and moderate distortion.
	// Candidate B: DC_PRED with a larger token rate but smaller distortion.
	// mbmode_cost[V]=2724, mbmode_cost[DC]=489 (pinned above).
	const (
		tokenRateV = 120
		distV      = uint64(900)
		tokenRateD = 400
		distD      = uint64(700)
	)

	rateV, rdV := vp9FullRDIntraModeRD(rdmult, wantVP9MbmodeCost[common.VPred], tokenRateV, distV)
	rateD, rdD := vp9FullRDIntraModeRD(rdmult, wantVP9MbmodeCost[common.DcPred], tokenRateD, distD)

	// Cross-check the rate halves: rate = tokenRate + mbmode_cost[mode].
	if rateV != tokenRateV+wantVP9MbmodeCost[common.VPred] {
		t.Fatalf("rateV = %d, want %d", rateV, tokenRateV+wantVP9MbmodeCost[common.VPred])
	}
	if rateD != tokenRateD+wantVP9MbmodeCost[common.DcPred] {
		t.Fatalf("rateD = %d, want %d", rateD, tokenRateD+wantVP9MbmodeCost[common.DcPred])
	}

	// Cross-check the RD values against an independent RDCOST evaluation.
	wantRDV := encoder.RDCost(rdmult, encoder.RDDivBits, rateV, distV)
	wantRDD := encoder.RDCost(rdmult, encoder.RDDivBits, rateD, distD)
	if rdV != wantRDV || rdD != wantRDD {
		t.Fatalf("rd mismatch: rdV=%d (want %d) rdD=%d (want %d)", rdV, wantRDV, rdD, wantRDD)
	}

	// The libvpx picker keeps the minimum-RD mode. With the chosen inputs the
	// DC candidate's much smaller mbmode_cost (489 vs 2724) dominates the rate
	// term, so DC_PRED must win despite its larger token rate.
	if !(rdD < rdV) {
		t.Fatalf("expected DC_PRED (rd=%d) to beat V_PRED (rd=%d); rdmult=%d", rdD, rdV, rdmult)
	}
}
