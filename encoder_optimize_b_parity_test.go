package govpx

import (
	"fmt"
	"strings"
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// planeTypeUV mirrors libvpx vp8/common/blockd.h:47 `#define PLANE_TYPE_UV 2`.
// Used by task #326 chroma audit and the per-block UV cohort tests below.
const planeTypeUV = 2

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

// TestVP8Task326ChromaTokenCostsUVAudit pins
// `mb->token_costs[PLANE_TYPE_UV][band][prev_token][to_token]` byte-for-byte
// against a fresh libvpx re-derivation of `fill_token_costs` restricted to
// the chroma plane (`blockType == 2`). This is the focused chroma-keep-cost
// audit angle of task #324's per-coefficient cost bisect, specifically
// targeting the value the chroma `optimize_b` trellis reads on every
// keep-vs-drop decision via
// `mb->token_costs[type][band][pt][tokens[next][...].token]` at
// vp8/encoder/encodemb.c:222-223 / 274-278 / 334-335.
//
// libvpx anchor:
//   - vp8/encoder/rdopt.c:112-132 fill_token_costs — selects between
//     vp8_cost_tokens (start=0, full tree) and vp8_cost_tokens2 (start=2,
//     EOB tree edge elided, leaves the EOB slot at the calloc'd 0 seed)
//     based on `k == 0 && j > (i == 0)`. For UV (i=2 != 0), the predicate
//     reduces to `pt == 0 && band > 0` — band 0 stays at the full-tree
//     fill, every band ≥ 1 with pt==0 is filled by vp8_cost_tokens2.
//   - vp8/encoder/treewriter.c vp8_cost_tokens — recursive tree walker that
//     writes each terminal token's accumulated subtree cost into the row.
//   - vp8/common/entropy.c vp8_default_coef_probs — the seed table from
//     which cpi->lfc_n / cpi->lfc_g / cpi->lfc_a (and cm->fc.coef_probs)
//     are initialized at every keyframe via vp8_default_coef_probs ->
//     vp8_init_de_quantizer -> vp8_setup_key_frame.
//
// govpx mirror:
//   - encoder_token_cost.go coefficientTokenCost / coefTokenCostElided —
//     selects between the full-tree path and the EOB-elided path with the
//     same `pt == 0 && band > coefElisionBandThreshold[blockType&3]`
//     predicate (coefElisionBandThreshold[2] = 0 for UV).
//   - vp8tables.DefaultCoefProbs — the keyframe seed, matches libvpx
//     vp8_default_coef_probs byte-for-byte (separately pinned via the
//     decoder coef-probs tests).
//
// The audit enumerates every chroma cell in the 8 (band) × 3 (prev_token) ×
// 12 (to_token) matrix (288 cells total), compares govpx's
// `coefficientTokenCost(p, token, blockType=2, band, pt)` against a
// fresh recursive walk of libvpx's CoefTree using the matching elision
// branch, and reports the first divergent cell with full context if any
// drift is found. The pre-fix baseline (task #316 chroma trellis ±1 DC
// drop investigation) had no token_costs UV divergence — this test pins
// that property as a permanent gate so the chroma keep-cost cannot
// regress under future cost-tree walker refactors.
//
// The per-cell expected values are derived from libvpx by calling
// `libvpxOptimizeBFillTokenCostsRow` (shared with the
// TestOptimizeBTokenCostsMatchLibvpxFillTokenCosts gate above) directly
// with the same chroma probs row.
func TestVP8Task326ChromaTokenCostsUVAudit(t *testing.T) {
	const blockType = planeTypeUV
	probs := &vp8tables.DefaultCoefProbs
	var divergent []string
	for band := range vp8tables.CoefBands {
		for pt := range vp8tables.PrevCoefContexts {
			p := probs[blockType][band][pt]
			wantCosts := libvpxOptimizeBFillTokenCostsRow(&p, blockType, band, pt)
			for token := range vp8tables.MaxEntropyTokens {
				got := coefficientTokenCost(p, token, blockType, band, pt)
				if got == wantCosts[token] {
					continue
				}
				divergent = append(divergent, fmt.Sprintf(
					"UV cell (band=%d, prev_token=%d, to_token=%d): govpx=%d libvpx=%d delta=%+d",
					band, pt, token, got, wantCosts[token], got-wantCosts[token]))
			}
		}
	}
	if len(divergent) > 0 {
		t.Fatalf("task #326 chroma token_costs UV divergence (%d cells):\n  %s",
			len(divergent), strings.Join(divergent, "\n  "))
	}
	t.Logf("task #326 pinned: token_costs[PLANE_TYPE_UV=2][0..7][0..2][0..11] byte-equal vs libvpx fill_token_costs over DefaultCoefProbs (288 cells)")
}

// TestVP8Task326ChromaTokenCostsUVElisionSelector pins the predicate that
// selects between the full-tree fill (vp8_cost_tokens) and the EOB-elided
// fill (vp8_cost_tokens2) for the chroma plane. libvpx's fill_token_costs
// branch is `k == 0 && j > (i == 0)`; for i=PLANE_TYPE_UV=2 this reduces
// to `pt == 0 && band > 0`. govpx mirrors this via
// `coefElisionBandThreshold[blockType&3]` = 0 for blockType ∈ {1,2,3}.
// Task #326 audit pins coefElisionBandThreshold[2] == 0 explicitly so a
// future refactor cannot accidentally re-introduce a per-blockType
// asymmetry on the chroma plane.
func TestVP8Task326ChromaTokenCostsUVElisionSelector(t *testing.T) {
	if got := coefElisionBandThreshold[planeTypeUV]; got != 0 {
		t.Fatalf("coefElisionBandThreshold[PLANE_TYPE_UV=2] = %d, want 0 (libvpx fill_token_costs: k==0 && j > (i==0) reduces to band > 0 for i=2)", got)
	}
	// Also pin the per-blockType table against libvpx's branch verbatim:
	//   i=0 (Y after Y2): elide when band > 1
	//   i=1 (Y2):         elide when band > 0
	//   i=2 (UV):         elide when band > 0
	//   i=3 (Y with DC):  elide when band > 0
	want := [4]int{1, 0, 0, 0}
	if coefElisionBandThreshold != want {
		t.Fatalf("coefElisionBandThreshold = %v, want %v (libvpx fill_token_costs k==0 && j > (i==0))",
			coefElisionBandThreshold, want)
	}
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
