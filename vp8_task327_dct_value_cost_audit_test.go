package govpx

import (
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestTask327DCTValueCostFullRangeMatchesLibvpxFillValueTokens audits the
// per-coefficient base-cost lookup that optimize_b consults on every Viterbi
// keep/drop step. Libvpx caches this in `dct_value_cost[2048*2]` and indexes
// it by `vp8_dct_value_cost_ptr + x` where x is the signed quantized
// coefficient (vp8/encoder/encodemb.c:233 and :290). Govpx ports the table
// inside dctValueBaseCostLUT (encoder_inter_quantize.go:411-429).
//
// The libvpx algorithm in vp8/encoder/tokenize.c fill_value_tokens
// (commented out at lines 39-98 — the generator that produced the embedded
// dct_value_cost.h and dct_value_tokens.h headers) is, for each signed
// coefficient i ∈ [-DCT_MAX_VALUE, DCT_MAX_VALUE):
//
//	const int a = sign ? -i : i;    // a = |i|
//	int eb = sign;                  // bit 0 of Extra is the sign bit
//	if (a > 4) {
//	    int j = 4;
//	    while (++j < 11  &&  vp8_extra_bits[j].base_val <= a) {}
//	    t[i].Token = --j;
//	    eb |= (a - vp8_extra_bits[j].base_val) << 1;
//	} else {
//	    t[i].Token = a;
//	}
//	t[i].Extra = eb;
//
//	// dct_value_cost
//	cost = 0
//	if (p->base_val) {   // skip ZERO_TOKEN
//	    if (p->Len) cost += vp8_treed_cost(p->tree, p->prob, extra >> 1, p->Len)
//	    cost += vp8_cost_bit(vp8_prob_half=128, extra & 1)  // sign cost
//	    dct_value_cost[i + DCT_MAX_VALUE] = cost
//	}
//
// This test reimplements that algorithm byte-for-byte in Go and compares
// every entry against govpx's lookup. Any divergence — magnitude, sign,
// or boundary cell — is a per-coefficient rate drift inside optimize_b
// that flips trellis decisions on small-best ARNR cold-segment cases and
// corrupts byte-faithful parity with libvpx.
func TestTask327DCTValueCostFullRangeMatchesLibvpxFillValueTokens(t *testing.T) {
	// Build the reference table the same way libvpx's commented
	// fill_value_tokens does. Loop signed i covers [-DCT_MAX_VALUE,
	// DCT_MAX_VALUE) — the same range optimize_b probes via the
	// 2048-centered pointer.
	const dctMaxValue = vp8tables.DCTMaxValue
	for i := -dctMaxValue; i < dctMaxValue; i++ {
		token, extra := libvpxFillValueTokens(i)
		wantCost := libvpxFillValueCost(token, extra)
		gotCost := dctValueBaseCost(i)
		if gotCost != wantCost {
			t.Fatalf("dctValueBaseCost(%d) = %d, want %d (libvpx fill_value_tokens) — token=%d extra=%d",
				i, gotCost, wantCost, token, extra)
		}
		gotToken := dctValueToken(i)
		if i == 0 {
			// Libvpx t[0].Token = 0 (ZERO_TOKEN); govpx returns
			// ZERO_TOKEN for x==0 from dctValueToken.
			if gotToken != vp8tables.ZeroToken {
				t.Fatalf("dctValueToken(0) = %d, want ZeroToken=%d", gotToken, vp8tables.ZeroToken)
			}
			continue
		}
		if gotToken != token {
			t.Fatalf("dctValueToken(%d) = %d, want %d", i, gotToken, token)
		}
	}
}

// libvpxFillValueTokens is a Go transliteration of the libvpx
// vp8/encoder/tokenize.c fill_value_tokens body for one signed coefficient i.
// It returns the token classification and the Extra word that fill_value_cost
// then consumes.
func libvpxFillValueTokens(i int) (token int, extra int) {
	sign := 1
	if i >= 0 {
		sign = 0
	}
	a := i
	if sign != 0 {
		a = -i
	}
	eb := sign
	if a > 4 {
		j := 4
		for {
			j++
			if j >= 11 {
				break
			}
			if int(vp8tables.ExtraBitsTable[j].BaseVal) > a {
				break
			}
		}
		j--
		token = j
		eb |= (a - int(vp8tables.ExtraBitsTable[j].BaseVal)) << 1
	} else {
		token = a
	}
	return token, eb
}

// libvpxFillValueCost is a Go transliteration of fill_value_tokens's cost
// computation for one (token, extra) pair. p->base_val == 0 (ZERO_TOKEN or
// the trailing { 0, 0, 0, 0 } sentinel) returns 0 — libvpx writes nothing
// to dct_value_cost in that case so the C array's pre-zero-initialized
// slot is returned (and govpx's LUT explicitly stores 0 for abs==0 by
// leaving the array element at its zero value).
func libvpxFillValueCost(token int, extra int) int {
	p := vp8tables.ExtraBitsTable[token]
	if p.BaseVal == 0 {
		// Either token 0 (ZERO_TOKEN) or 11 (sentinel) — libvpx skips
		// the cost write. But token 0 still has the sign-cost branch
		// inside optimize_b suppressed by the qcoeff==0 outer guard.
		// Mirror by returning 0; the trellis never reads this slot
		// for a non-zero coefficient (verified by the loop range in
		// the calling test).
		return 0
	}
	cost := 0
	if p.Len != 0 {
		cost += libvpxTreedCost(p.Tree, p.Prob, extra>>1, int(p.Len))
	}
	cost += libvpxCostBit(128, extra&1)
	return cost
}

// libvpxTreedCost mirrors the static `vp8_treed_cost` in
// vp8/encoder/treewriter.h line 78 — degenerate-binary tree walk used by
// the extra-bits coding for DCT_VAL_CATEGORY1..6.
func libvpxTreedCost(tree []int16, prob []uint8, v int, n int) int {
	c := 0
	i := 0
	for {
		n--
		b := (v >> n) & 1
		c += libvpxCostBit(prob[i>>1], b)
		i = int(tree[i+b])
		if n == 0 {
			return c
		}
	}
}

// libvpxCostBit mirrors vp8_cost_bit / vp8_cost_bit0 / vp8_cost_one — the
// 256-entry ProbCost LUT keyed by `prob` for bit=0 and `255-prob` for bit=1.
func libvpxCostBit(prob uint8, bit int) int {
	if bit != 0 {
		return vp8tables.ProbCost[255-int(prob)]
	}
	return vp8tables.ProbCost[prob]
}
