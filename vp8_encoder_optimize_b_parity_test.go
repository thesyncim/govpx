package govpx

import (
	"fmt"
	"strings"
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// planeTypeUV mirrors libvpx vp8/common/blockd.h:47 `#define PLANE_TYPE_UV 2`.
// Used by task #326 chroma audit and the per-block UV cohort tests below.
const planeTypeUV = 2

// TestOptimizeBSignedBaseCost guards the per-sign branch of
// DCTValueBaseCost. Libvpx's vp8/encoder/tokenize.c fill_value_tokens
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
		got := vp8enc.DCTValueBaseCost(tc.value)
		if got != tc.wantCost {
			t.Errorf("DCTValueBaseCost(%d) = %d, want %d (sign-bit cost differs by 2 between positive and negative coefficients)",
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

// TestVP8ChromaTokenCostsUVMatchLibvpx pins
// `mb->token_costs[PLANE_TYPE_UV][band][prev_token][to_token]` byte-for-byte
// against a fresh libvpx re-derivation of `fill_token_costs` restricted to
// the chroma plane (`blockType == 2`). This targets the value the chroma
// `optimize_b` trellis reads on every
// keep-vs-drop decision via
// `mb->token_costs[type][band][pt][tokens[next][...].token]` at
// vp8/encoder/encodemb.c:222-223 / 274-278 / 334-335.
//
// libvpx anchor:
//   - vp8/encoder/rdopt.c:112-132 fill_token_costs — selects between
//     vp8_cost_tokens (start=0, full tree) and vp8_cost_tokens2 (start=2,
//     EOB tree edge elided, leaves the EOB slot at the calloc'd 0 seed)
//     based on `k == 0 && j > (i == 0)`. For UV (i=2 != 0), the predicate
//     reduces to `pt == 0 && band > 0`; band 0 stays at the full-tree
//     fill, every band >= 1 with pt==0 is filled by vp8_cost_tokens2.
//   - vp8/encoder/treewriter.c vp8_cost_tokens: recursive tree walker that
//     writes each terminal token's accumulated subtree cost into the row.
//   - vp8/common/entropy.c vp8_default_coef_probs: the seed table from
//     which cpi->lfc_n / cpi->lfc_g / cpi->lfc_a (and cm->fc.coef_probs)
//     are initialized at every keyframe via vp8_default_coef_probs ->
//     vp8_init_de_quantizer -> vp8_setup_key_frame.
//
// govpx mirror:
//   - vp8_encoder_token_cost.go coefficientTokenCost / coefTokenCostElided:
//     selects between the full-tree path and the EOB-elided path with the
//     same `pt == 0 && band > coefElisionBandThreshold[blockType&3]`
//     predicate (coefElisionBandThreshold[2] = 0 for UV).
//   - vp8tables.DefaultCoefProbs: the keyframe seed, matches libvpx
//     vp8_default_coef_probs byte-for-byte (separately pinned via the
//     decoder coef-probs tests).
//
// The test enumerates every chroma cell in the 8 (band) by 3 (prev_token) by
// 12 (to_token) matrix (288 cells total), compares govpx's
// `coefficientTokenCost(p, token, blockType=2, band, pt)` against a
// fresh recursive walk of libvpx's CoefTree using the matching elision
// branch, and reports the first divergent cell with full context if any
// drift is found. The regression baseline had no token_costs UV divergence;
// this test pins
// that property as a permanent gate so the chroma keep-cost cannot
// regress under future cost-tree walker refactors.
//
// The per-cell expected values are derived from libvpx by calling
// `libvpxOptimizeBFillTokenCostsRow` (shared with the
// TestOptimizeBTokenCostsMatchLibvpxFillTokenCosts gate above) directly
// with the same chroma probs row.
func TestVP8ChromaTokenCostsUVMatchLibvpx(t *testing.T) {
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
		t.Fatalf("chroma token_costs UV divergence (%d cells):\n  %s",
			len(divergent), strings.Join(divergent, "\n  "))
	}
	t.Logf("token_costs[PLANE_TYPE_UV=2][0..7][0..2][0..11] byte-equal vs libvpx fill_token_costs over DefaultCoefProbs (288 cells)")
}

// TestVP8ChromaTokenCostsUVElisionSelector pins the predicate that
// selects between the full-tree fill (vp8_cost_tokens) and the EOB-elided
// fill (vp8_cost_tokens2) for the chroma plane. libvpx's fill_token_costs
// branch is `k == 0 && j > (i == 0)`; for i=PLANE_TYPE_UV=2 this reduces
// to `pt == 0 && band > 0`. govpx mirrors this via
// `coefElisionBandThreshold[blockType&3]` = 0 for blockType ∈ {1,2,3}.
// The explicit UV check protects against accidentally re-introducing a
// per-blockType asymmetry on the chroma plane.
func TestVP8ChromaTokenCostsUVElisionSelector(t *testing.T) {
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

// TestVP8ChromaEntropyContextSeedMatchesLibvpx pins the chroma optimize_b
// per-block `pt` seed mapping against libvpx's vp8_block2above /
// vp8_block2left tables (vp8/common/blockd.c:14-19). The chroma trellis
// reads its initial `pt` from `*a` + `*l` where
//
//	a = ta + vp8_block2above[b]
//	l = tl + vp8_block2left[b]
//
// and ta/tl are the macroblock-level above/left ENTROPY_CONTEXT planes.
// For b in [16, 24), ENTROPY_CONTEXT indices 4..7 cover U then V (4..5
// for U, 6..7 for V); the within-plane offsets vp8_block2above[b] - 4
// and vp8_block2left[b] - 4 are exactly what govpx's
// `macroblockCoefficientUVContextIndex` / `tokenUVContextIndex` return.
// This test pins both maps byte-equal to libvpx for every chroma block
// 16..23, plus the additional invariant that the FIRST chroma block of a
// fresh macroblock (MB(0,0) seed: above/left planes all-zero) yields
// pt == 0 — i.e. the chroma trellis ALWAYS sees pt=0 at scan_pos=0 of
// block 16 in MB(0,0), in both govpx and libvpx.
//
// libvpx anchor:
//
//	vp8/common/blockd.c:14-19 (vp8_block2above / vp8_block2left tables).
//	vp8/encoder/encodemb.c:413-416 (optimize_mb UV loop: ta + block2above,
//	  tl + block2left).
//	vp8/encoder/encodemb.c:327 (VP8_COMBINEENTROPYCONTEXTS(pt, *a, *l)).
//
// govpx mirror:
//
//	internal/vp8/encoder/tokenize.go tokenUVContextIndex (bitstream-final
//	  context lookup).
//	vp8_encoder_inter_coeff_rate.go macroblockCoefficientUVContextIndex (RD
//	  trellis seed lookup).
//
// Chroma optimize_b parity coverage confirms the runtime corollary of these
// two maps matching libvpx. This test pins the mapping itself so the runtime
// parity cannot regress under a helper refactor.
func TestVP8ChromaEntropyContextSeedMatchesLibvpx(t *testing.T) {
	// vp8_block2above and vp8_block2left ported verbatim from
	// vp8/common/blockd.c:14-19 (libvpx v1.16.0). Entries 16..23 cover
	// U then V in raster order; entry 24 covers the Y2 second-order
	// block (not exercised by the chroma trellis loop).
	libvpxBlock2Above := [25]uint8{
		0, 1, 2, 3, 0, 1, 2, 3, 0,
		1, 2, 3, 0, 1, 2, 3, 4, 5,
		4, 5, 6, 7, 6, 7, 8,
	}
	libvpxBlock2Left := [25]uint8{
		0, 0, 0, 0, 1, 1, 1, 1, 2,
		2, 2, 2, 3, 3, 3, 3, 4, 4,
		5, 5, 6, 6, 7, 7, 8,
	}

	// ENTROPY_CONTEXT layout (libvpx vp8/common/entropymv.h equivalents
	// and the optimize_mb call sites in encodemb.c:413-416):
	//   indices 0..3 = Y1, 4..5 = U, 6..7 = V, 8 = Y2.
	// govpx's UV context arrays are 4-wide indexed [0,1,2,3] -> [U0,U1,V0,V1],
	// so the libvpx within-MB offset for blocks 16..23 must equal the
	// libvpx entry minus 4 (the U-plane base).
	const uvBase = 4
	for block := 16; block < 24; block++ {
		wantA := int(libvpxBlock2Above[block]) - uvBase
		wantL := int(libvpxBlock2Left[block]) - uvBase
		gotA, gotL := macroblockCoefficientUVContextIndex(block)
		if gotA != wantA || gotL != wantL {
			t.Errorf("block %d: macroblockCoefficientUVContextIndex returned (a=%d,l=%d), want (a=%d,l=%d) per libvpx vp8_block2above/vp8_block2left - 4",
				block, gotA, gotL, wantA, wantL)
		}
	}

	// Pin the MB(0,0) seed invariant: with above/left ENTROPY_CONTEXT
	// planes fresh-reset to zero (the state of every macroblock on the
	// top-left row before any prior MB's optimize_b writes have landed),
	// the FIRST chroma block 16's seed pt = above[a] + left[l] = 0 + 0
	// regardless of which (a,l) the map points to. Pin this for all 8
	// chroma blocks so the invariant covers the full chroma trellis
	// loop entry point when above/left are zero.
	var zeroAbove, zeroLeft [4]uint8
	for block := 16; block < 24; block++ {
		a, l := macroblockCoefficientUVContextIndex(block)
		seed := int(zeroAbove[a]) + int(zeroLeft[l])
		if seed != 0 {
			t.Errorf("block %d: zero-context seed pt = %d, want 0 (MB(0,0) chroma trellis entry must see pt=0 when above/left are fresh-reset)",
				block, seed)
		}
	}

	// Pin the second-pass within-MB chroma propagation: after the
	// optimize_b for block 16 writes hasCoeffs=1 to above[0]/left[0]
	// (libvpx encodemb.c:355 `*a = *l = (final_eob != !type)` for
	// type=PLANE_TYPE_UV=2 collapses to `*a = *l = (final_eob != 0)`,
	// which is true iff eob > 0), the seed for block 17 must pick up
	// above[1] (still 0) and left[0] (now 1) -> pt = 1. Walking through
	// blocks 17..23 with this propagation models a hypothetical
	// "every chroma block produces non-zero eob" MB and pins the
	// libvpx-anchored seed sequence so a regression in either the
	// within-MB propagation logic or the (a,l) tuple selection is
	// caught on a deterministic table-driven path.
	above := [4]uint8{}
	left := [4]uint8{}
	wantSeeds := [8]int{
		0, // b16: a=0, l=0 (above/left all-zero)
		1, // b17: a=1, l=0 (left[0] just got 1 from b16)
		1, // b18: a=0, l=1 (above[0]=1; left[1]=0)
		2, // b19: a=1, l=1 (above[1]=1; left[1]=1)
		0, // b20: a=2, l=2 (V plane; above[2]=0, left[2]=0)
		1, // b21: a=3, l=2 (left[2] just got 1 from b20)
		1, // b22: a=2, l=3 (above[2]=1; left[3]=0)
		2, // b23: a=3, l=3 (above[3]=1; left[3]=1)
	}
	for i, block := 0, 16; block < 24; i, block = i+1, block+1 {
		a, l := macroblockCoefficientUVContextIndex(block)
		seed := int(above[a]) + int(left[l])
		if seed != wantSeeds[i] {
			t.Errorf("block %d (i=%d): chroma seed pt = %d, want %d (libvpx-anchored propagation with every prior block writing hasCoeffs=1)",
				block, i, seed, wantSeeds[i])
		}
		// optimize_b writes *a = *l = 1 when final_eob != !type and
		// type=PLANE_TYPE_UV=2 -> writes 1 when final_eob > 0; the
		// hypothetical scenario keeps every block non-zero so we
		// write 1 every iteration to model the worst-case propagation.
		above[a] = 1
		left[l] = 1
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
