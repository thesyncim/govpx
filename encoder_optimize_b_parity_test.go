package govpx

import (
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestOptimizeBSignedBaseCost guards the per-sign branch of
// dctValueBaseCost. Libvpx's vp8/encoder/tokenize.c fill_value_tokens
// writes a different dct_value_cost[i] for i>0 vs i<0 because the sign
// bit cost (vp8_cost_bit(vp8_prob_half=128, sign_bit)) differs by 2
// entropy-bit units between sign=0 and sign=1:
//
//	ProbCost[128] = 255   // emit sign_bit=0 (positive coeff)
//	ProbCost[127] = 257   // emit sign_bit=1 (negative coeff)
//
// vp8_dct_value_cost_ptr is centered on DCT_MAX_VALUE so optimize_b's
// base_bits = *(vp8_dct_value_cost_ptr + x) picks up the signed cost
// correctly. Govpx had previously precomputed a single magnitude-keyed
// LUT and reused it for both signs, undercounting negative coefficients
// by 2 units. That 2-unit drift was enough to flip the trellis `best`
// choice on the small-best-cpu0-16x32 cold-segment SPLITMV case and the
// parallel resize good-quality+cpu0 cold-segment cases. This regression
// test pins both per-sign costs so any future refactor of the base-bits
// LUT lands the per-sign behavior in lockstep with libvpx.
func TestOptimizeBSignedBaseCost(t *testing.T) {
	cases := []struct {
		value    int
		wantCost int
	}{
		{value: 1, wantCost: 255},  // OneToken, positive: signCost(128, 0) + 0
		{value: -1, wantCost: 257}, // OneToken, negative: signCost(128, 1) + 0
		{value: 2, wantCost: 255},  // TwoToken, positive
		{value: -2, wantCost: 257}, // TwoToken, negative
		{value: 3, wantCost: 255},  // ThreeToken, positive
		{value: -3, wantCost: 257}, // ThreeToken, negative
		{value: 4, wantCost: 255},  // FourToken, positive
		{value: -4, wantCost: 257}, // FourToken, negative
	}
	for _, tc := range cases {
		got := dctValueBaseCost(tc.value)
		if got != tc.wantCost {
			t.Errorf("dctValueBaseCost(%d) = %d, want %d (sign-bit cost differs by 2 between positive and negative coefficients)",
				tc.value, got, tc.wantCost)
		}
	}
}

// TestOptimizeBTokenCostsMatchLibvpxFillTokenCosts asserts that
// coefficientTokenCost — read on every optimize_b trellis iteration — is
// byte-identical to libvpx's mb->token_costs[type][band][pt][token] table
// for every (type, band, pt, token) combination over DefaultCoefProbs.
// Libvpx's fill_token_costs (vp8/encoder/rdopt.c) selects between
// vp8_cost_tokens (full tree) and vp8_cost_tokens2 (start=2, EOB-elided)
// based on `k == 0 && j > (i == 0)`. govpx mirrors that selection through
// coefTokenCostElided; this test exercises both branches at every cell.
//
// Before the close-goodquality-cpu0-small-frame fix, govpx's elided-row
// EOB_TOKEN slot returned `full - nonEOB` (typically a few thousand
// entropy-bit units off) while libvpx leaves that slot at the calloc'd
// zero seed because vp8_cost_tokens2 starts past the EOB tree edge. This
// test pins the zero-on-elided behavior so the gap cannot regress.
func TestOptimizeBTokenCostsMatchLibvpxFillTokenCosts(t *testing.T) {
	probs := &vp8tables.DefaultCoefProbs
	for blockType := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for pt := range vp8tables.PrevCoefContexts {
				p := probs[blockType][band][pt]
				wantCosts := libvpxOptimizeBFillTokenCostsRow(&p, blockType, band, pt)
				for token := range vp8tables.MaxEntropyTokens {
					got := coefficientTokenCost(p, token, blockType, band, pt)
					if got != wantCosts[token] {
						t.Errorf("coefficientTokenCost(type=%d band=%d pt=%d token=%d) = %d, want %d (libvpx fill_token_costs)",
							blockType, band, pt, token, got, wantCosts[token])
					}
				}
			}
		}
	}
}

// libvpxOptimizeBFillTokenCostsRow mirrors fill_token_costs's per-(i,j,k)
// branch exactly: vp8_cost_tokens for non-elided cells, vp8_cost_tokens2
// (start=2, leaving EOB slot at 0) for the elided cells where
// k == 0 && j > (i == 0).
func libvpxOptimizeBFillTokenCostsRow(probs *[vp8tables.EntropyNodes]uint8, blockType, band, pt int) [vp8tables.MaxEntropyTokens]int {
	var out [vp8tables.MaxEntropyTokens]int
	start := 0
	elidedAt := 0
	if blockType == 0 {
		elidedAt = 1
	}
	if pt == 0 && band > elidedAt {
		start = 2 // matches vp8_cost_tokens2 start
	}
	libvpxOptimizeBCostTokensWalk(out[:], probs, vp8tables.CoefTree[:], start, 0)
	return out
}

// libvpxOptimizeBCostTokensWalk is a direct port of the static `cost`
// function in vp8/encoder/treewriter.c — a recursive descent over the
// CoefTree that writes each terminal token's accumulated cost into
// out[token]. Initial call has accumulated=0 and the caller picks
// start=0 (full tree) or start=2 (skip EOB root edge).
func libvpxOptimizeBCostTokensWalk(out []int, probs *[vp8tables.EntropyNodes]uint8, tree []int16, i int, accumulated int) {
	prob := probs[i>>1]
	for {
		j := tree[i]
		bit := i & 1
		var stepCost int
		if bit != 0 {
			stepCost = vp8tables.ProbCost[255-int(prob)]
		} else {
			stepCost = vp8tables.ProbCost[prob]
		}
		d := accumulated + stepCost
		if j <= 0 {
			out[-j] = d
		} else {
			libvpxOptimizeBCostTokensWalk(out, probs, tree, int(j), d)
		}
		i++
		if i&1 == 0 {
			return
		}
	}
}
