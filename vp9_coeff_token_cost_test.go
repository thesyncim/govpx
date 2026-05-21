package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestVP9CoeffTokenRateCostMatchesLibvpxLeafCost checks that
// vp9CoeffTokenRateCost, the per-coefficient cost helper invoked by
// vp9KeyframeCoeffBlockRateCost and vp9InterCoeffBlockRateCost — adds
// up to the same total libvpx's vp9_cost_tokens does, once the caller
// has charged the two leading unconstrained bits (not-EOB at probs[0]
// and not-ZERO at probs[1]).
//
// The libvpx-equivalent leaf cost is:
//
//	leaf(T) = vp9_cost_tokens(full_probs)[T]
//
// where full_probs is vp9_model_to_full_probs(model). The encoder's
// per-coefficient adder breaks that into:
//
//	cost = cost_bit(probs[0], 1)    // not-EOB     (outer caller)
//	     + cost_bit(probs[1], 1)    // not-ZERO    (outer caller)
//	     + vp9CoeffTokenRateCost(probs, absVal, sign)
//
// so vp9CoeffTokenRateCost(absVal, sign=0) without the trailing sign
// bit must equal leaf(T) - cost_bit(probs[0], 1) - cost_bit(probs[1], 1)
// + extra_bits_cost(absVal). Adding the libvpx-spec sign cost (a flat
// vp9_cost_bit(128, sign) — 512 cost units) and the extra-bits cost on
// both sides keeps the equality intact.
//
// libvpx references (v1.16.0):
//
//	vp9/encoder/vp9_rdopt.c:358-459    cost_coeffs (per-coef adder)
//	vp9/encoder/vp9_cost.c             vp9_cost_tokens (leaf cost)
//	vp9/encoder/vp9_tokenize.c:75      vp9_coef_tree
//	vp9/common/vp9_entropy.c:1035      vp9_model_to_full_probs
func TestVP9CoeffTokenRateCostMatchesLibvpxLeafCost(t *testing.T) {
	// A small but representative sweep: every pivot (selected sparsely
	// because the per-coefficient adder is pivot-driven), each
	// EOB/ZERO axis, and the canonical absolute-value witnesses for
	// every token class (OneToken..Cat6Tok). For each combination,
	// reconstruct the libvpx-equivalent total and compare to
	// vp9CoeffTokenRateCost.
	type witness struct {
		absVal int
		token  int
	}
	witnesses := []witness{
		{1, encoder.OneToken},
		{2, encoder.TwoToken},
		{3, encoder.ThreeToken},
		{4, encoder.FourToken},
		{5, encoder.Category1Tok},
		{7, encoder.Category2Tok},
		{11, encoder.Category3Tok},
		{19, encoder.Category4Tok},
		{35, encoder.Category5Tok},
		{67, encoder.Category6Tok},
	}
	axis := []uint8{1, 32, 64, 128, 192, 224, 255}
	pivots := []uint8{1, 8, 32, 64, 96, 128, 160, 192, 224, 240, 255}
	for _, eobP := range axis {
		for _, zeroP := range axis {
			for _, pivotP := range pivots {
				full := buildFullProbRow(eobP, zeroP, pivotP)
				var leaf [encoder.EntropyTokens]int
				encoder.VP9CostTokens(leaf[:], full[:], encoder.CoefTree[:])
				notEOB := encoder.VP9CostBit(full[0], 1)
				notZero := encoder.VP9CostBit(full[1], 1)
				for _, w := range witnesses {
					// Sign axis: 0 (non-negative) and 1 (negative) —
					// both sides charge cost_bit(128, sign).
					for _, sign := range []int{0, 1} {
						got := vp9CoeffTokenRateCost(full[:], w.absVal, sign)
						// libvpx-equivalent expected cost:
						// leaf(T) minus the two outer unconstrained bits,
						// plus extra-bits, plus sign. The token witnesses
						// pick the BaseVal so extra=0 — extra bits collapse
						// to len * cost_bit(prob[i], 0) = sum_i cost_bit(prob[i], 0).
						want := leaf[w.token] - notEOB - notZero +
							extraBitsCostAtBase(w.token) +
							encoder.VP9CostBit(128, sign)
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

// TestVP9CoeffTokenRateCostExtraBitsSweep stresses the extra-bits
// path: for CAT1..CAT6, walks every possible extra-bit payload and
// confirms the cost equals leaf(T) - outer_unconstrained + per_bit_sum
// + sign. This catches divergence in the per-bit probability table
// (VP9ExtraBits[].Prob) and the MSB-first bit emission order.
func TestVP9CoeffTokenRateCostExtraBitsSweep(t *testing.T) {
	type catCase struct {
		token   int
		baseVal int
		maxAbs  int
	}
	cats := []catCase{
		{encoder.Category1Tok, 5, 6},    // 1 extra bit  → 5..6
		{encoder.Category2Tok, 7, 10},   // 2 extra bits → 7..10
		{encoder.Category3Tok, 11, 18},  // 3 extra bits → 11..18
		{encoder.Category4Tok, 19, 34},  // 4 extra bits → 19..34
		{encoder.Category5Tok, 35, 66},  // 5 extra bits → 35..66
		{encoder.Category6Tok, 67, 200}, // 14 extra bits → 67..(67+2^14-1); cap at 200 to bound runtime
	}
	full := buildFullProbRow(128, 128, 128)
	var leaf [encoder.EntropyTokens]int
	encoder.VP9CostTokens(leaf[:], full[:], encoder.CoefTree[:])
	notEOB := encoder.VP9CostBit(full[0], 1)
	notZero := encoder.VP9CostBit(full[1], 1)
	sign := 0
	signCost := encoder.VP9CostBit(128, sign)
	for _, c := range cats {
		eb := encoder.VP9ExtraBits[c.token]
		for absVal := c.baseVal; absVal <= c.maxAbs; absVal++ {
			extra := absVal - eb.BaseVal
			var extraCost int
			for i := eb.Len - 1; i >= 0; i-- {
				bit := (extra >> uint(i)) & 1
				extraCost += encoder.VP9CostBit(eb.Prob[eb.Len-1-i], bit)
			}
			want := leaf[c.token] - notEOB - notZero + extraCost + signCost
			got := vp9CoeffTokenRateCost(full[:], absVal, sign)
			if got != want {
				t.Errorf("token=%d absVal=%d: got=%d want=%d (extra=%d extraCost=%d)",
					c.token, absVal, got, want, extra, extraCost)
				break
			}
		}
	}
}

// TestVP9CoeffBlockRateCostSlowSkipsEOBAfterZeroToken verifies that a ZERO
// token suppresses the impossible EOB branch when the following coefficient is
// non-zero.
func TestVP9CoeffBlockRateCostSlowSkipsEOBAfterZeroToken(t *testing.T) {
	var e VP9Encoder
	coefModel := &e.fc.CoefProbs[common.Tx4x4][0][0]
	for band := range vp9dec.CoefBands {
		for ctx := range vp9dec.CoefContexts {
			(*coefModel)[band][ctx][0] = 128
			(*coefModel)[band][ctx][1] = 128
			(*coefModel)[band][ctx][2] = 128
		}
	}

	scanOrder := common.DefaultScanOrders[common.Tx4x4]
	coeffs := make([]int16, vp9dec.MaxEobForTxSize(common.Tx4x4))
	qcoeffs := make([]int16, len(coeffs))
	qcoeffs[scanOrder.Scan[1]] = 1

	got := e.vp9KeyframeCoeffBlockRateCostPlaneQ(common.Tx4x4, 0, scanOrder,
		[2]int16{4, 4}, coeffs, qcoeffs, 0)

	var tokenCache [1024]uint8
	tokenCache[0] = encoder.PtEnergyClass[encoder.ZeroToken]
	pt := vp9dec.GetCoefContext(scanOrder.Neighbors, &tokenCache, 1)
	oneToken, oneExtra := vp9CoeffTokenExtraCost(1, 0)
	want := vp9CoeffTreeTokenCost((*coefModel)[0][0][:], false,
		encoder.ZeroToken)
	want += oneExtra + vp9CoeffTreeTokenCost((*coefModel)[1][pt][:],
		true, oneToken)
	tokenCache[scanOrder.Scan[1]] = encoder.PtEnergyClass[oneToken]
	eobCtx := vp9dec.GetCoefContext(scanOrder.Neighbors, &tokenCache, 2)
	want += vp9CoeffTreeTokenCost((*coefModel)[1][eobCtx][:], false,
		encoder.EobToken)

	overcharged := vp9CoeffTreeTokenCost((*coefModel)[0][0][:], false,
		encoder.ZeroToken)
	overcharged += oneExtra + vp9CoeffTreeTokenCost((*coefModel)[1][pt][:],
		false, oneToken)
	overcharged += vp9CoeffTreeTokenCost((*coefModel)[1][eobCtx][:], false,
		encoder.EobToken)

	if got != want {
		t.Fatalf("slow cost = %d, want %d", got, want)
	}
	if got == overcharged {
		t.Fatalf("slow cost charged full tree after ZERO token: got %d", got)
	}
}

// buildFullProbRow mirrors libvpx's vp9_model_to_full_probs: copy the
// three unconstrained probs verbatim, then replace nodes [3..10] with
// vp9_pareto8_full[pivotP-1]. tables.Pareto8Full is the pinned port.
func buildFullProbRow(eobP, zeroP, pivotP uint8) [encoder.EntropyNodes]uint8 {
	var full [encoder.EntropyNodes]uint8
	full[0] = eobP
	full[1] = zeroP
	full[2] = pivotP
	tail := tables.Pareto8Full[pivotP-1]
	for k := range 8 {
		full[3+k] = tail[k]
	}
	return full
}

// extraBitsCostAtBase returns the extra-bits cost when absVal equals
// the token's BaseVal — i.e. extra=0, so every bit is a zero against
// VP9ExtraBits[token].Prob[i]. Used by the witness-driven parity test
// where each token's representative absVal is its BaseVal.
func extraBitsCostAtBase(token int) int {
	eb := encoder.VP9ExtraBits[token]
	if eb.Len == 0 || eb.Prob == nil {
		return 0
	}
	cost := 0
	for i := 0; i < eb.Len; i++ {
		cost += encoder.VP9CostBit(eb.Prob[i], 0)
	}
	return cost
}
