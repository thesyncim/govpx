package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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

func TestCoeffTreeTokenCostMatchesCostTokens(t *testing.T) {
	axis := []uint8{1, 32, 64, 128, 192, 224, 255}
	pivots := []uint8{1, 8, 32, 64, 96, 128, 160, 192, 224, 240, 255}
	for _, eobP := range axis {
		for _, zeroP := range axis {
			for _, pivotP := range pivots {
				model := []uint8{eobP, zeroP, pivotP}
				full := coeffCostFullProbRow(eobP, zeroP, pivotP)
				var leaf [EntropyTokens]int
				VP9CostTokens(leaf[:], full[:], CoefTree[:])
				var skip [EntropyTokens]int
				VP9CostTokensSkip(skip[:], full[:], CoefTree[:])
				for token := range EntropyTokens {
					got := CoeffTreeTokenCost(model, false, token)
					if got != leaf[token] {
						t.Fatalf("full eobP=%d zeroP=%d pivotP=%d token=%d: got=%d want=%d",
							eobP, zeroP, pivotP, token, got, leaf[token])
					}
					got = CoeffTreeTokenCost(model, true, token)
					if got != skip[token] {
						t.Fatalf("skip eobP=%d zeroP=%d pivotP=%d token=%d: got=%d want=%d",
							eobP, zeroP, pivotP, token, got, skip[token])
					}
				}
			}
		}
	}
}

func TestCoeffTreeTokenCostTableMatchesScalar(t *testing.T) {
	var model [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	for band := range vp9dec.CoefBands {
		for ctx := range vp9dec.CoefContexts {
			model[band][ctx][0] = uint8(32 + band*23 + ctx)
			model[band][ctx][1] = uint8(224 - band*17 - ctx)
			model[band][ctx][2] = uint8(8 + band*31 + ctx*3)
		}
	}
	var table CoeffTreeTokenCostTable
	if !FillCoeffTreeTokenCostTable(&model, &table) {
		t.Fatal("FillCoeffTreeTokenCostTable returned false")
	}
	for band := range vp9dec.CoefBands {
		for ctx := range vp9dec.CoefContexts {
			for token := range EntropyTokens {
				for _, skipEOB := range []bool{false, true} {
					got := table.TokenCost(band, ctx, skipEOB, token)
					want := CoeffTreeTokenCost(model[band][ctx][:], skipEOB, token)
					if got != want {
						t.Fatalf("table[%d][%d][%v][%d] = %d, want %d",
							band, ctx, skipEOB, token, got, want)
					}
				}
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

func TestCoeffTokenExtraCostTableMatchesSlowPath(t *testing.T) {
	for _, absVal := range []int{
		0, 1, 2, 3, 4, 5, 6, 7, 10, 11, 18, 19, 34, 35, 66, 67,
		255, 1000, coeffTokenExtraCostTableSize - 1,
	} {
		for _, sign := range []int{0, 1} {
			gotToken, gotCost := CoeffTokenExtraCost(absVal, sign)
			wantToken, wantCost := coeffTokenExtraCostSlow(absVal, sign)
			if gotToken != wantToken || gotCost != wantCost {
				t.Fatalf("CoeffTokenExtraCost(%d,%d) = (%d,%d), want slow (%d,%d)",
					absVal, sign, gotToken, gotCost, wantToken, wantCost)
			}
		}
	}
}

func TestCoeffTokenExtraCostOutOfTableFallsBack(t *testing.T) {
	absVal := coeffTokenExtraCostTableSize + 17
	gotToken, gotCost := CoeffTokenExtraCost(absVal, 1)
	wantToken, wantCost := coeffTokenExtraCostSlow(absVal, 1)
	if gotToken != wantToken || gotCost != wantCost {
		t.Fatalf("CoeffTokenExtraCost fallback = (%d,%d), want slow (%d,%d)",
			gotToken, gotCost, wantToken, wantCost)
	}
}

func TestCoeffTokenExtraCostQCoeffMatchesSlowPath(t *testing.T) {
	for _, q := range []int16{
		-4096, -67, -3, -1, 0, 1, 2, 66, 255, 4095, 4096,
	} {
		gotToken, gotCost := coeffTokenExtraCostQCoeff(q)
		absVal := int(q)
		sign := 0
		if absVal < 0 {
			absVal = -absVal
			sign = 1
		}
		wantToken, wantCost := coeffTokenExtraCostSlow(absVal, sign)
		if gotToken != wantToken || gotCost != wantCost {
			t.Fatalf("coeffTokenExtraCostQCoeff(%d) = (%d,%d), want slow (%d,%d)",
				q, gotToken, gotCost, wantToken, wantCost)
		}
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

func TestCoeffBlockEOBFallsBackForPartialQCoeff(t *testing.T) {
	scan := []int16{0, 1, 2, 3}
	coeffs := []int16{0, 0, 0, 7}
	qcoeffs := []int16{0, 0}
	if got := CoeffBlockEOB(scan, len(scan), coeffs, qcoeffs); got != 4 {
		t.Fatalf("CoeffBlockEOB partial qcoeff = %d, want coeff fallback eob 4", got)
	}
}

func TestCoeffBlockEOBCompleteQCoeffMatchesGeneric(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx16x16].Scan
	qcoeffs := make([]int16, 256)
	qcoeffs[scan[4]] = -2
	qcoeffs[scan[37]] = 11

	got := coeffBlockEOBCompleteQCoeff(scan, 256, qcoeffs)
	want := CoeffBlockEOB(scan, 256, nil, qcoeffs)
	if got != want {
		t.Fatalf("coeffBlockEOBCompleteQCoeff = %d, want generic eob %d", got, want)
	}
}

var coeffBlockEOBBenchSink int

func BenchmarkCoeffBlockEOBCompleteQCoeff(b *testing.B) {
	scan := common.DefaultScanOrders[common.Tx16x16].Scan
	qcoeffs := make([]int16, 256)
	qcoeffs[scan[37]] = 11
	total := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		total += coeffBlockEOBCompleteQCoeff(scan, 256, qcoeffs)
	}
	coeffBlockEOBBenchSink = total
}

func BenchmarkCoeffBlockEOBGenericCoeff(b *testing.B) {
	scan := common.DefaultScanOrders[common.Tx16x16].Scan
	coeffs := make([]int16, 256)
	coeffs[scan[37]] = 11
	total := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		total += CoeffBlockEOB(scan, 256, coeffs, nil)
	}
	coeffBlockEOBBenchSink = total
}

func BenchmarkCoeffTokenExtraCostTable(b *testing.B) {
	values := [...]int{0, 1, 2, 3, 4, 5, 7, 11, 19, 35, 67, 255}
	total := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		token, cost := CoeffTokenExtraCost(values[i%len(values)], i&1)
		total += token + cost
	}
	coeffBlockEOBBenchSink = total
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
