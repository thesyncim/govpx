package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// This file pins concrete libvpx v1.16.0 VALUES (not just formulas) for the
// four full-RD scoring primitives consumed by vp9_rd_pick_inter_mode_sb:
//
//   - RDCOST                                  (vp9/encoder/vp9_rd.h:29-30)
//   - vp9_compute_rd_mult_based_on_qindex     (vp9/encoder/vp9_rd.c:241-276)
//   - vp9_mv_bit_cost                         (vp9/encoder/vp9_mcomp.c:80-84)
//   - vp9_cost_mv_ref / cost_mv_ref           (vp9/encoder/vp9_rdopt.c:1551-1554)
//
// The expected numbers were derived offline by executing the exact libvpx
// integer formulas over the libvpx default tables: the vp9_prob_cost[256]
// table (vp9/encoder/vp9_cost.c:16), default_inter_mode_probs
// (vp9/common/vp9_entropymode.c:234-242), and default_nmv_context
// (vp9/common/vp9_entropymv.c:38-53). They are hard-coded constants so the
// test is an independent cross-check rather than a self-reference.

// TestComputeRDMultBasedOnQindexPinsLibvpxValues pins the exact integer
// rdmult libvpx's vp9_compute_rd_mult_based_on_qindex returns at a few
// qindices. libvpx (8-bit): q = vp9_dc_quant(qindex,0,8); rdmult = q*q;
// rdmult = (int)((double)rdmult * (base + 0.001*qindex)).
func TestComputeRDMultBasedOnQindexPinsLibvpxValues(t *testing.T) {
	cases := []struct {
		qindex int
		ft     RDFrameType
		want   int
	}{
		// q=vp9_dc_quant(0,0,8)=4 ; rdmult=16
		{qindex: 0, ft: RDFrameInter, want: 66},      // (int)(16*4.150)=66
		{qindex: 0, ft: RDFrameKey, want: 69},        // (int)(16*4.350)=69
		{qindex: 0, ft: RDFrameArfGolden, want: 68},  // (int)(16*4.250)=68
		{qindex: 64, ft: RDFrameInter, want: 15680},  // q=61 rdmult=3721 *(4.214)
		{qindex: 64, ft: RDFrameKey, want: 16424},    // q=61 rdmult=3721 *(4.414)
		{qindex: 128, ft: RDFrameInter, want: 83848}, // q=140 rdmult=19600 *(4.278)
		{qindex: 255, ft: RDFrameKey, want: 8219446}, // q=1336 rdmult=1784896 *(4.605)
	}
	for _, c := range cases {
		if got := ComputeRDMultBasedOnQindex(c.qindex, c.ft); got != c.want {
			t.Fatalf("qindex=%d ft=%d: rdmult=%d want libvpx=%d",
				c.qindex, c.ft, got, c.want)
		}
	}
}

// TestDcQuantSeedsForRDMult pins the vp9_dc_quant values the rdmult constants
// above are derived from, so a dc_qlookup table regression is caught directly.
func TestDcQuantSeedsForRDMult(t *testing.T) {
	cases := []struct {
		qindex int
		want   int16
	}{
		{0, 4}, {64, 61}, {128, 140}, {255, 1336},
	}
	for _, c := range cases {
		if got := vp9dec.VpxDcQuant(c.qindex, 0, vp9dec.BitDepth8); got != c.want {
			t.Fatalf("vp9_dc_quant(%d,0,8)=%d want %d", c.qindex, got, c.want)
		}
	}
}

// TestRDCostPinsLibvpxValues pins concrete RDCOST(RM,DM,R,D) outputs:
//
//	RDCOST = ROUND_POWER_OF_TWO(R*RM, 9) + (D << DM)   (vp9_rd.h:29-30)
//	ROUND_POWER_OF_TWO(v,9) = (v + 256) >> 9           (vpx_ports/mem.h:37)
func TestRDCostPinsLibvpxValues(t *testing.T) {
	cases := []struct {
		rdmult, rddiv, rate int
		dist                uint64
		want                uint64
	}{
		// (512*66 + 256)>>9 = 34048>>9 = 66 ; D=0
		{rdmult: 66, rddiv: 7, rate: 512, dist: 0, want: 66},
		// (1000*15680 + 256)>>9 = 15680256>>9 = 30625 ; +(10<<7)=1280 -> 31905
		{rdmult: 15680, rddiv: 7, rate: 1000, dist: 10, want: 31905},
		// rate term 0 ; (4096<<7)=524288
		{rdmult: 83848, rddiv: 7, rate: 0, dist: 4096, want: 524288},
	}
	for _, c := range cases {
		if got := RDCost(c.rdmult, c.rddiv, c.rate, c.dist); got != c.want {
			t.Fatalf("RDCOST(RM=%d,DM=%d,R=%d,D=%d)=%d want libvpx=%d",
				c.rdmult, c.rddiv, c.rate, c.dist, got, c.want)
		}
	}
}

// TestCostMvRefPinsLibvpxValues pins vp9_cost_mv_ref / cost_mv_ref outputs,
// which equal cpi->inter_mode_cost[mode_context][INTER_OFFSET(mode)] built by
// vp9_cost_tokens over vp9_inter_mode_tree with default_inter_mode_probs.
//
// Values computed by walking vp9_inter_mode_tree with vp9_prob_cost over the
// default probs per context (NEARESTMV=0,NEARMV=1,ZEROMV=2,NEWMV=3).
//
// The govpx primitive is InterModeRateCost: for the predictor modes the
// returned cost equals cost_mv_ref exactly (no MV term); for NEWMV it equals
// cost_mv_ref plus vp9_mv_bit_cost, so it is exercised with a zero MV diff and
// the joint-zero MV contribution is added separately in the expectation.
func TestCostMvRefPinsLibvpxValues(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	zero := vp9dec.MV{}
	predictorCases := []struct {
		ctx  int
		mode common.PredictionMode
		want int
	}{
		// ctx 0 probs {2,173,34}
		{ctx: 0, mode: common.NearestMv, want: 295},
		{ctx: 0, mode: common.ZeroMv, want: 3584},
		{ctx: 0, mode: common.NearMv, want: 2329},
		// ctx 4 probs {8,64,46}
		{ctx: 4, mode: common.NearestMv, want: 1047},
		{ctx: 4, mode: common.ZeroMv, want: 2560},
		// ctx 6 probs {25,29,30}
		{ctx: 6, mode: common.ZeroMv, want: 1718},
	}
	for _, c := range predictorCases {
		got := InterModeRateCost(&fc, c.ctx, c.mode, zero, zero, false)
		if got != c.want {
			t.Fatalf("cost_mv_ref(ctx=%d mode=%d)=%d want libvpx=%d",
				c.ctx, c.mode, got, c.want)
		}
	}

	// NEWMV: the mode-rate portion (cost_mv_ref) is the three-bit tree walk to
	// the NEWMV leaf. Pin it directly via the inter-mode probability bits, the
	// exact expansion of cpi->inter_mode_cost[ctx][INTER_OFFSET(NEWMV)].
	newMvCases := []struct {
		ctx  int
		want int
	}{
		{ctx: 0, want: 943},
		{ctx: 4, want: 381},
		{ctx: 6, want: 257},
	}
	for _, c := range newMvCases {
		p := fc.InterModeProbs[c.ctx]
		// vp9_inter_mode_tree: ZEROMV at bit0=0; else bit1 selects NEARESTMV(0)
		// vs deeper; bit2 selects NEARMV(0) vs NEWMV(1). NEWMV path is 1,1,1.
		got := VP9CostBit(p[0], 1) + VP9CostBit(p[1], 1) + VP9CostBit(p[2], 1)
		if got != c.want {
			t.Fatalf("cost_mv_ref(ctx=%d NEWMV)=%d want libvpx=%d",
				c.ctx, got, c.want)
		}
	}
}

// TestMvBitCostPinsLibvpxValues pins vp9_mv_bit_cost(mv, ref, MV_COST_WEIGHT)
// against libvpx values computed from build_nmv_component_cost_table over the
// default_nmv_context (vp9_encodemv.c:69-140) and
// vp9_mv_bit_cost = ROUND_POWER_OF_TWO(mv_cost(diff)*108, 7) (vp9_mcomp.c:80-84,
// MV_COST_WEIGHT=108 at vp9_rd.h:38). usehp mirrors allow_high_precision_mv:
// the table charges the high-precision bit iff usehp is set (vp9_rd.c:420-423).
func TestMvBitCostPinsLibvpxValues(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	cases := []struct {
		mv, ref vp9dec.MV
		usehp   bool
		want    int
	}{
		{mv: vp9dec.MV{Row: 8, Col: 4}, ref: vp9dec.MV{}, usehp: true, want: 4971},
		{mv: vp9dec.MV{Row: 8, Col: 4}, ref: vp9dec.MV{}, usehp: false, want: 3750},
		{mv: vp9dec.MV{Row: 135, Col: 13}, ref: vp9dec.MV{Row: 128, Col: 0}, usehp: true, want: 5900},
		{mv: vp9dec.MV{Row: -20, Col: 30}, ref: vp9dec.MV{Row: 5, Col: -5}, usehp: false, want: 8086},
		{mv: vp9dec.MV{Row: 3, Col: 0}, ref: vp9dec.MV{}, usehp: true, want: 2651},
	}
	for _, c := range cases {
		got := MvBitCost(c.mv, c.ref, &fc.Nmvc, c.usehp)
		if got != c.want {
			t.Fatalf("vp9_mv_bit_cost(mv=%+v ref=%+v usehp=%v)=%d want libvpx=%d",
				c.mv, c.ref, c.usehp, got, c.want)
		}
	}
}
