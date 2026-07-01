package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestDiscountNewMvTestTruthTable pins libvpx discount_newmv_test
// (vp9/encoder/vp9_rdopt.c:2798-2807, CONFIG_NON_GREEDY_MV==0): true iff
// !is_src_frame_alt_ref && mode==NEWMV && this_mv!=0 && nearest is 0/INVALID &&
// near is 0/INVALID. An unavailable predictor (valid==false) is treated as the
// INVALID_MV slot, i.e. the same as a zero MV.
func TestDiscountNewMvTestTruthTable(t *testing.T) {
	nz := vp9dec.MV{Row: 12, Col: 4} // non-zero (the frame-1 SB0 NEWMV)
	zero := vp9dec.MV{}
	cases := []struct {
		name         string
		isAltRef     bool
		mode         common.PredictionMode
		thisMv       vp9dec.MV
		nearest      vp9dec.MV
		nearestValid bool
		near         vp9dec.MV
		nearValid    bool
		want         bool
	}{
		// The captured frame-1 SB0 candidate: NEWMV mv=(12,4), nearest=(0,0)
		// available, near unavailable → discount fires (libvpx capture
		// discount=1).
		{"frame1_sb0_newmv", false, common.NewMv, nz, zero, true, zero, false, true},
		{"both_zero_available", false, common.NewMv, nz, zero, true, zero, true, true},
		{"both_invalid", false, common.NewMv, nz, zero, false, zero, false, true},
		{"nearest_nonzero", false, common.NewMv, nz, nz, true, zero, true, false},
		{"near_nonzero", false, common.NewMv, nz, zero, true, nz, true, false},
		{"this_mv_zero", false, common.NewMv, zero, zero, true, zero, true, false},
		{"not_newmv", false, common.NearestMv, nz, zero, true, zero, true, false},
		{"src_is_altref", true, common.NewMv, nz, zero, true, zero, true, false},
	}
	for _, c := range cases {
		got := DiscountNewMvTest(c.isAltRef, c.mode, c.thisMv, c.nearest,
			c.nearestValid, c.near, c.nearValid)
		if got != c.want {
			t.Errorf("%s: DiscountNewMvTest = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestCostMvRefPinsNewMvLeaf pins CostMvRef against the inter-mode-tree bits the
// existing TestCostMvRefPinsLibvpxValues expectations imply, and verifies it is
// the MV-free portion of InterModeRateCost (cost_mv_ref == InterModeRateCost for
// predictor modes; == InterModeRateCost - MvBitCost for NEWMV).
func TestCostMvRefPinsNewMvLeaf(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	// cost_mv_ref(NEWMV) for ctx 0/4/6 == the libvpx-pinned 943/381/257.
	for _, c := range []struct {
		ctx  int
		want int
	}{{0, 943}, {4, 381}, {6, 257}} {
		if got := CostMvRef(&fc, c.ctx, common.NewMv); got != c.want {
			t.Errorf("CostMvRef(ctx=%d, NEWMV) = %d, want %d", c.ctx, got, c.want)
		}
	}

	// CostMvRef == InterModeRateCost for the predictor modes (no MV term).
	zero := vp9dec.MV{}
	for _, mode := range []common.PredictionMode{
		common.ZeroMv, common.NearestMv, common.NearMv,
	} {
		full := InterModeRateCost(&fc, 0, mode, zero, zero, false)
		ref := CostMvRef(&fc, 0, mode)
		if full != ref {
			t.Errorf("mode=%d: InterModeRateCost=%d != CostMvRef=%d", mode, full, ref)
		}
	}

	// For NEWMV, InterModeRateCost = CostMvRef(NEWMV) + MvBitCost(mv, refMv).
	mv := vp9dec.MV{Row: 12, Col: 4}
	refMv := vp9dec.MV{}
	full := InterModeRateCost(&fc, 0, common.NewMv, mv, refMv, true)
	want := CostMvRef(&fc, 0, common.NewMv) + MvBitCost(mv, refMv, &fc.Nmvc, true)
	if full != want {
		t.Errorf("NEWMV InterModeRateCost=%d, want CostMvRef+MvBitCost=%d", full, want)
	}
}

// TestInterModeMvRateWithDiscount pins the genuine handle_inter_mode mode+MV
// rate2 contribution (vp9_rdopt.c:2936-2941 + :2970-2977): discounted uses
// VPXMAX(rate_mv/8, 1) for the MV and VPXMIN(cost_mv_ref(NEWMV),
// cost_mv_ref(NEARESTMV)) for the mode bits; undiscounted uses the full sums.
func TestInterModeMvRateWithDiscount(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	ctx := 0
	mv := vp9dec.MV{Row: 12, Col: 4}
	refMv := vp9dec.MV{}
	allowHP := true

	rateMv := MvBitCost(mv, refMv, &fc.Nmvc, allowHP)
	costNew := CostMvRef(&fc, ctx, common.NewMv)
	costNearest := CostMvRef(&fc, ctx, common.NearestMv)

	// Undiscounted NEWMV == full rate_mv + cost_mv_ref(NEWMV) == InterModeRateCost.
	wantFull := rateMv + costNew
	if got := InterModeMvRateWithDiscount(&fc, ctx, common.NewMv, mv, refMv, allowHP, false); got != wantFull {
		t.Errorf("undiscounted NEWMV rate = %d, want %d", got, wantFull)
	}
	if ref := InterModeRateCost(&fc, ctx, common.NewMv, mv, refMv, allowHP); ref != wantFull {
		t.Errorf("InterModeRateCost NEWMV = %d, want %d", ref, wantFull)
	}

	// Discounted NEWMV == max(rate_mv/8,1) + min(cost_mv_ref(NEWMV), cost(NEAREST)).
	discMv := max(rateMv/8, 1)
	discCost := min(costNearest, costNew)
	wantDisc := discMv + discCost
	if got := InterModeMvRateWithDiscount(&fc, ctx, common.NewMv, mv, refMv, allowHP, true); got != wantDisc {
		t.Errorf("discounted NEWMV rate = %d, want %d", got, wantDisc)
	}

	// Non-NEWMV ignores the discount flag (no MV term).
	for _, mode := range []common.PredictionMode{
		common.ZeroMv, common.NearestMv, common.NearMv,
	} {
		off := InterModeMvRateWithDiscount(&fc, ctx, mode, mv, refMv, allowHP, false)
		on := InterModeMvRateWithDiscount(&fc, ctx, mode, mv, refMv, allowHP, true)
		if off != on || off != CostMvRef(&fc, ctx, mode) {
			t.Errorf("mode=%d: discount-invariant rate mismatch off=%d on=%d cmvr=%d",
				mode, off, on, CostMvRef(&fc, ctx, mode))
		}
	}
}
