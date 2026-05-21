package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestCoeffTokenRateCostMatchesLibvpxLeafCost checks that
// CoeffTokenRateCost adds up to the same total libvpx's vp9_cost_tokens does
// once the caller has charged the two leading unconstrained bits.
//
// libvpx references (v1.16.0):
//
//	vp9/encoder/vp9_rdopt.c:358-459    cost_coeffs
//	vp9/encoder/vp9_cost.c             vp9_cost_tokens
//	vp9/encoder/vp9_tokenize.c:75      vp9_coef_tree
//	vp9/common/vp9_entropy.c:1035      vp9_model_to_full_probs
func TestCoeffTokenRateCostMatchesLibvpxLeafCost(t *testing.T) {
	type witness struct {
		absVal int
		token  int
	}
	witnesses := []witness{
		{1, OneToken},
		{2, TwoToken},
		{3, ThreeToken},
		{4, FourToken},
		{5, Category1Tok},
		{7, Category2Tok},
		{11, Category3Tok},
		{19, Category4Tok},
		{35, Category5Tok},
		{67, Category6Tok},
	}
	axis := []uint8{1, 32, 64, 128, 192, 224, 255}
	pivots := []uint8{1, 8, 32, 64, 96, 128, 160, 192, 224, 240, 255}
	for _, eobP := range axis {
		for _, zeroP := range axis {
			for _, pivotP := range pivots {
				full := coeffCostFullProbRow(eobP, zeroP, pivotP)
				var leaf [EntropyTokens]int
				VP9CostTokens(leaf[:], full[:], CoefTree[:])
				notEOB := VP9CostBit(full[0], 1)
				notZero := VP9CostBit(full[1], 1)
				for _, w := range witnesses {
					for _, sign := range []int{0, 1} {
						got := CoeffTokenRateCost(full[:], w.absVal, sign)
						want := leaf[w.token] - notEOB - notZero +
							coeffCostExtraBitsAtBase(w.token) +
							VP9CostBit(128, sign)
						if got != want {
							t.Errorf("eobP=%d zeroP=%d pivotP=%d absVal=%d sign=%d: got=%d want=%d",
								eobP, zeroP, pivotP, w.absVal, sign, got, want)
						}
					}
				}
			}
		}
	}
}

// TestCoeffTokenRateCostExtraBitsSweep walks every CAT1..CAT5 payload and a
// bounded CAT6 prefix to confirm MSB-first extra-bit costing.
func TestCoeffTokenRateCostExtraBitsSweep(t *testing.T) {
	type catCase struct {
		token   int
		baseVal int
		maxAbs  int
	}
	cats := []catCase{
		{Category1Tok, 5, 6},
		{Category2Tok, 7, 10},
		{Category3Tok, 11, 18},
		{Category4Tok, 19, 34},
		{Category5Tok, 35, 66},
		{Category6Tok, 67, 200},
	}
	full := coeffCostFullProbRow(128, 128, 128)
	var leaf [EntropyTokens]int
	VP9CostTokens(leaf[:], full[:], CoefTree[:])
	notEOB := VP9CostBit(full[0], 1)
	notZero := VP9CostBit(full[1], 1)
	signCost := VP9CostBit(128, 0)
	for _, c := range cats {
		eb := VP9ExtraBits[c.token]
		for absVal := c.baseVal; absVal <= c.maxAbs; absVal++ {
			extra := absVal - eb.BaseVal
			var extraCost int
			for i := eb.Len - 1; i >= 0; i-- {
				bit := (extra >> uint(i)) & 1
				extraCost += VP9CostBit(eb.Prob[eb.Len-1-i], bit)
			}
			want := leaf[c.token] - notEOB - notZero + extraCost + signCost
			got := CoeffTokenRateCost(full[:], absVal, 0)
			if got != want {
				t.Errorf("token=%d absVal=%d: got=%d want=%d (extra=%d extraCost=%d)",
					c.token, absVal, got, want, extra, extraCost)
				break
			}
		}
	}
}

func TestCoeffMagnitudeAndSignPrefersQCoeff(t *testing.T) {
	absVal, sign := CoeffMagnitudeAndSign([]int16{0, -7}, 1, 42, 4, false)
	if absVal != 7 || sign != 1 {
		t.Fatalf("CoeffMagnitudeAndSign with qcoeff = (%d,%d), want (7,1)",
			absVal, sign)
	}
}

func TestCoeffMagnitudeAndSignFallsBackToDqCoeff(t *testing.T) {
	absVal, sign := CoeffMagnitudeAndSign(nil, 0, -15, 4, false)
	if absVal != 3 || sign != 1 {
		t.Fatalf("CoeffMagnitudeAndSign 4x4 fallback = (%d,%d), want (3,1)",
			absVal, sign)
	}
	absVal, sign = CoeffMagnitudeAndSign(nil, 0, 15, 4, true)
	if absVal != 8 || sign != 0 {
		t.Fatalf("CoeffMagnitudeAndSign 32x32 fallback = (%d,%d), want (8,0)",
			absVal, sign)
	}
}

func TestCoeffBlockEOBPrefersQCoeffScanOrder(t *testing.T) {
	scan := []int16{3, 1, 2, 0}
	coeffs := []int16{9, 0, 0, 0}
	qcoeffs := []int16{0, 0, 5, 0}
	if got := CoeffBlockEOB(scan, len(scan), coeffs, qcoeffs); got != 3 {
		t.Fatalf("CoeffBlockEOB = %d, want 3", got)
	}
	if !CoeffBlockHasCoeff(scan, 2, coeffs, qcoeffs) {
		t.Fatal("CoeffBlockHasCoeff did not observe qcoeff at scan position 2")
	}
	if CoeffBlockHasCoeff(scan, -1, coeffs, qcoeffs) {
		t.Fatal("CoeffBlockHasCoeff accepted negative position")
	}
}

func TestTxSizeRateCostRespectsMaxTxSize(t *testing.T) {
	probs := []uint8{128, 64, 192}
	got := TxSizeRateCost(probs, common.Tx16x16, common.Tx16x16)
	want := VP9CostBit(128, 1) + VP9CostBit(64, 1)
	if got != want {
		t.Fatalf("TxSizeRateCost 16x16/max16 = %d, want %d", got, want)
	}
	got = TxSizeRateCost(probs, common.Tx32x32, common.Tx32x32)
	want += VP9CostBit(192, 1)
	if got != want {
		t.Fatalf("TxSizeRateCost 32x32/max32 = %d, want %d", got, want)
	}
}

func coeffCostFullProbRow(eobP, zeroP, pivotP uint8) [EntropyNodes]uint8 {
	var full [EntropyNodes]uint8
	full[0] = eobP
	full[1] = zeroP
	full[2] = pivotP
	tail := tables.Pareto8Full[pivotP-1]
	for k := range 8 {
		full[3+k] = tail[k]
	}
	return full
}

func coeffCostExtraBitsAtBase(token int) int {
	eb := VP9ExtraBits[token]
	if eb.Len == 0 || eb.Prob == nil {
		return 0
	}
	cost := 0
	for i := 0; i < eb.Len; i++ {
		cost += VP9CostBit(eb.Prob[i], 0)
	}
	return cost
}
