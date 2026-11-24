package encoder_test

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// referenceCoefBlockTokenRate is a literal walk of libvpx's
// vp8/encoder/rdopt.c:cost_coeffs against the same probability tables and
// helpers used by the Go encoder. It is intentionally shaped like the libvpx
// loop so any divergence between CoefficientBlockTokenRate and cost_coeffs
// surfaces as a numeric mismatch.
//
// libvpx loop:
//
//	c = !type; pt = combined ctx; cost = 0;
//	for (; c < eob; ++c) {
//	    v = qcoeff[zigzag[c]]; t = token(v);
//	    cost += token_costs[type][bands[c]][pt][t];
//	    cost += dct_value_cost[v];
//	    pt = prev_token_class[t];
//	}
//	if (c < 16) cost += token_costs[type][bands[c]][pt][EOB];
//
// where token_costs[type][band][0][...] for band > (type == 0 ? 1 : 0) drops
// the EOB-vs-not bit (cost_tokens2 with start=2), matching the encoder's
// skip_eob_node = (pt == 0) elision in tokenize.c.
func referenceCoefBlockTokenRate(probs *vp8tables.CoefficientProbs, blockType, ctx, skipDC int, qcoeff *[16]int16, eob int) int {
	threshold := 0
	if blockType == 0 {
		threshold = 1
	}
	tokenCost := func(p [vp8tables.EntropyNodes]uint8, token, band, pt int) int {
		full := vp8enc.TreeTokenCost(vp8tables.CoefTree[:], p[:], token)
		if pt == 0 && band > threshold {
			return full - vp8enc.BoolBitCost(p[0], 1)
		}
		return full
	}
	pt := ctx
	cost := 0
	pos := skipDC
	for pos < eob {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		var token int
		if coeff == 0 {
			token = vp8tables.ZeroToken
		} else {
			t, _, ok := vp8enc.CoefficientTokenMagnitude(coeff)
			if !ok {
				return -1
			}
			token = t
		}
		cost += tokenCost(p, token, band, pt)
		if coeff != 0 {
			t, mag, _ := vp8enc.CoefficientTokenMagnitude(coeff)
			if coeff < 0 {
				cost += vp8enc.BoolBitCost(128, 1)
			} else {
				cost += vp8enc.BoolBitCost(128, 0)
			}
			cost += vp8enc.CoefficientExtraBitsRate(t, mag)
		}
		pt = int(vp8tables.PrevTokenClass[token])
		pos++
	}
	if pos < 16 {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		cost += vp8enc.TreeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
	}
	return cost
}

// blockEOB returns the libvpx-style "one past last non-zero" EOB so the
// reference walk and the production walk see the same termination.
func blockEOBFromCoeffs(qcoeff *[16]int16, skipDC int) int {
	for pos := 15; pos >= skipDC; pos-- {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		if qcoeff[rc] != 0 {
			return pos + 1
		}
	}
	return skipDC
}

func TestCoefficientBlockTokenRateMatchesReferenceWalk(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs

	type vec struct {
		name      string
		blockType int
		ctx       int
		skipDC    int
		qcoeff    [16]int16
		// eobOverride <= 0 means "derive from coeffs"; otherwise force this eob
		// to exercise the trellis-style "EOB inside trailing zeros" case.
		eobOverride int
	}

	// Helper to mark an unused slot in zigzag scan order.
	at := func(zigPos int, val int16) (int, int16) {
		return int(vp8tables.DefaultZigZag1D[zigPos]), val
	}

	build := func(pairs ...struct {
		zig int
		val int16
	}) [16]int16 {
		var q [16]int16
		for _, p := range pairs {
			rc, v := at(p.zig, p.val)
			q[rc] = v
		}
		return q
	}

	type pair = struct {
		zig int
		val int16
	}

	cases := []vec{
		{
			name:      "early-eob-type3-ctx0",
			blockType: 3, ctx: 0, skipDC: 0,
			qcoeff: build(pair{0, 1}),
		},
		{
			name:      "early-eob-type0-ctx2-skipDC",
			blockType: 0, ctx: 2, skipDC: 1,
			qcoeff: build(pair{1, -3}),
		},
		{
			name:      "first-zero-then-nonzero-type2",
			blockType: 2, ctx: 1, skipDC: 0,
			qcoeff: build(pair{1, 5}, pair{2, -2}),
		},
		{
			name: "post-nonzero-zero-then-nonzero-type1",
			// 1, 1, 0, 1 — exercises pt!=0 followed by ZERO_TOKEN cost which
			// must include the full EOB-vs-not bit (libvpx uses cost_tokens
			// for token_costs[type][band][k!=0]).
			blockType: 1, ctx: 0, skipDC: 0,
			qcoeff: build(pair{0, 1}, pair{1, 1}, pair{3, 1}),
		},
		{
			name: "full-block-type3",
			// All sixteen positions non-zero so the EOB-vs-not bit must be
			// charged at every position (no skip_eob_node anywhere).
			blockType: 3, ctx: 1, skipDC: 0,
			qcoeff: [16]int16{1, -1, 1, -1, 1, -1, 1, -1, 1, -1, 1, -1, 1, -1, 1, -1},
		},
		{
			name:      "eob-at-15-type0",
			blockType: 0, ctx: 0, skipDC: 1,
			qcoeff: build(pair{1, 2}, pair{15, -7}),
		},
		{
			name: "trailing-zeros-inside-eob-type3",
			// qcoeff has a non-zero at zig 0 only, but force eob=4 so the
			// reference and production walks both encode three ZERO_TOKENs
			// before the EOB. This is the "EOB inside trailing zeros" case
			// the trellis can leave behind during recode.
			blockType: 3, ctx: 0, skipDC: 0,
			qcoeff:      build(pair{0, 4}),
			eobOverride: 4,
		},
		{
			name: "mixed-magnitudes-type2",
			// Mix of category tokens (DCT_VAL_CATEGORY3 covers magnitudes
			// 11..18) and small tokens, with varying signs.
			blockType: 2, ctx: 2, skipDC: 0,
			qcoeff: build(pair{0, 12}, pair{2, -3}, pair{4, 1}, pair{6, -67}),
		},
		{
			name: "y_no_dc_post_zero_subtree",
			// type=0, skipDC=1: first encoded position is c=1 (band=1).
			// Sequence: 0 (band 1), nonzero (band 2). Position 2 is post-zero
			// with band=2 > threshold=1 → must use subtree-only cost.
			blockType: 0, ctx: 0, skipDC: 1,
			qcoeff: build(pair{2, 2}),
		},
		{
			name:      "all-zero-block-type1",
			blockType: 1, ctx: 1, skipDC: 0,
			qcoeff: [16]int16{},
		},
		{
			name:      "all-zero-block-type0",
			blockType: 0, ctx: 0, skipDC: 1,
			qcoeff: [16]int16{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eob := c.eobOverride
			if eob <= 0 {
				eob = blockEOBFromCoeffs(&c.qcoeff, c.skipDC)
			}
			got := vp8enc.CoefficientBlockTokenRate(&probs, c.blockType, c.ctx, c.skipDC, &c.qcoeff, eob)
			want := referenceCoefBlockTokenRate(&probs, c.blockType, c.ctx, c.skipDC, &c.qcoeff, eob)
			if got != want {
				t.Fatalf("rate mismatch: got=%d want=%d (blockType=%d ctx=%d skipDC=%d eob=%d coeffs=%v)",
					got, want, c.blockType, c.ctx, c.skipDC, eob, c.qcoeff)
			}
		})
	}
}

func TestCoefficientBlockTokenRateWithTableMatchesDynamicRate(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var costs vp8enc.CoefficientTokenCostTable
	if !vp8enc.FillCoefficientTokenCostTable(&probs, &costs) {
		t.Fatal("FillCoefficientTokenCostTable returned false")
	}

	coeffSets := [][16]int16{
		{},
		{0: 1},
		{0: -3, 1: 2, 5: 1, 10: -1},
		{0: 12, 4: -67, 8: 3, 15: -2048},
	}
	for blockType := range vp8tables.BlockTypes {
		for ctx := range vp8tables.PrevCoefContexts {
			for skipDC := 0; skipDC <= 1; skipDC++ {
				for _, qcoeff := range coeffSets {
					eob := blockEOBFromCoeffs(&qcoeff, skipDC)
					if eob < 16 && eob < skipDC+4 {
						eob = min(16, skipDC+4)
					}
					got := vp8enc.CoefficientBlockTokenRateWithTable(&costs, blockType, ctx, skipDC, &qcoeff, eob)
					want := vp8enc.CoefficientBlockTokenRate(&probs, blockType, ctx, skipDC, &qcoeff, eob)
					if got != want {
						t.Fatalf("table rate mismatch blockType=%d ctx=%d skipDC=%d eob=%d coeffs=%v: got=%d want=%d",
							blockType, ctx, skipDC, eob, qcoeff, got, want)
					}
				}
			}
		}
	}
}

func TestCoefficientBlockTokenRateTrustedMatchesGuardedTableRate(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var costs vp8enc.CoefficientTokenCostTable
	if !vp8enc.FillCoefficientTokenCostTable(&probs, &costs) {
		t.Fatal("FillCoefficientTokenCostTable returned false")
	}

	coeffSets := [][16]int16{
		{},
		{0: 1},
		{0: -3, 1: 2, 5: 1, 10: -1},
		{0: 12, 4: -67, 8: 3, 15: -2048},
	}
	for blockType := range vp8tables.BlockTypes {
		for ctx := range vp8tables.PrevCoefContexts {
			for skipDC := 0; skipDC <= 1; skipDC++ {
				for _, qcoeff := range coeffSets {
					eob := blockEOBFromCoeffs(&qcoeff, skipDC)
					if eob < 16 && eob < skipDC+4 {
						eob = min(16, skipDC+4)
					}
					got := vp8enc.CoefficientBlockTokenRateWithTableTrusted(
						&costs, blockType, ctx, skipDC, &qcoeff, eob)
					want := vp8enc.CoefficientBlockTokenRateWithTable(
						&costs, blockType, ctx, skipDC, &qcoeff, eob)
					if got != want {
						t.Fatalf("trusted rate mismatch blockType=%d ctx=%d skipDC=%d eob=%d coeffs=%v: got=%d want=%d",
							blockType, ctx, skipDC, eob, qcoeff, got, want)
					}
				}
			}
		}
	}
}

// TestCoefficientBlockTokenRateIncrementalMatchesWholeBlock verifies that an
// explicit per-position walk, matching the shape libvpx's trellis uses while
// rolling rate forward through the sentinel, produces the same total as the
// whole-block rate helper.
func TestCoefficientBlockTokenRateIncrementalMatchesWholeBlock(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs

	type vec struct {
		name      string
		blockType int
		ctx       int
		skipDC    int
		qcoeff    [16]int16
	}

	cases := []vec{
		{"type0-mixed", 0, 0, 1, [16]int16{0, 3, -2, 0, 1, 0, 0, 5, 0, 0, -1, 0, 0, 0, 0, 0}},
		{"type1-ydc", 1, 2, 0, [16]int16{4, 0, -1, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"type2-uv", 2, 1, 0, [16]int16{-7, 0, 11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		{"type3-bpred", 3, 0, 0, [16]int16{1, 1, 0, -1, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eob := blockEOBFromCoeffs(&c.qcoeff, c.skipDC)
			whole := vp8enc.CoefficientBlockTokenRate(&probs, c.blockType, c.ctx, c.skipDC, &c.qcoeff, eob)

			// Incremental: walk position by position, accumulating the cost
			// libvpx assigns to each token transition.
			threshold := 0
			if c.blockType == 0 {
				threshold = 1
			}
			pt := c.ctx
			cost := 0
			pos := c.skipDC
			for pos < eob {
				band := int(vp8tables.CoefBandsTable[pos])
				p := probs[c.blockType][band][pt]
				rc := int(vp8tables.DefaultZigZag1D[pos])
				coeff := int(c.qcoeff[rc])
				token := vp8tables.ZeroToken
				if coeff != 0 {
					tk, mag, ok := vp8enc.CoefficientTokenMagnitude(coeff)
					if !ok {
						t.Fatalf("bad coeff %d", coeff)
					}
					token = tk
					full := vp8enc.TreeTokenCost(vp8tables.CoefTree[:], p[:], token)
					if pt == 0 && band > threshold {
						cost += full - vp8enc.BoolBitCost(p[0], 1)
					} else {
						cost += full
					}
					if coeff < 0 {
						cost += vp8enc.BoolBitCost(128, 1)
					} else {
						cost += vp8enc.BoolBitCost(128, 0)
					}
					cost += vp8enc.CoefficientExtraBitsRate(tk, mag)
				} else {
					full := vp8enc.TreeTokenCost(vp8tables.CoefTree[:], p[:], token)
					if pt == 0 && band > threshold {
						cost += full - vp8enc.BoolBitCost(p[0], 1)
					} else {
						cost += full
					}
				}
				pt = int(vp8tables.PrevTokenClass[token])
				pos++
			}
			if pos < 16 {
				band := int(vp8tables.CoefBandsTable[pos])
				p := probs[c.blockType][band][pt]
				cost += vp8enc.TreeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
			}
			if whole != cost {
				t.Fatalf("incremental=%d whole=%d (blockType=%d ctx=%d skipDC=%d eob=%d)",
					cost, whole, c.blockType, c.ctx, c.skipDC, eob)
			}
		})
	}
}
