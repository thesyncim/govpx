package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestCoeffBlockRateCostSlowSkipsEOBAfterZeroToken(t *testing.T) {
	var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	for band := range vp9dec.CoefBands {
		for ctx := range vp9dec.CoefContexts {
			coefModel[band][ctx][0] = 128
			coefModel[band][ctx][1] = 128
			coefModel[band][ctx][2] = 128
		}
	}

	scanOrder := common.DefaultScanOrders[common.Tx4x4]
	coeffs := make([]int16, vp9dec.MaxEobForTxSize(common.Tx4x4))
	qcoeffs := make([]int16, len(coeffs))
	qcoeffs[scanOrder.Scan[1]] = 1
	var scratch [1024]byte

	got := CoeffBlockRateCost(CoeffBlockRateCostInput{
		TxSize:     common.Tx4x4,
		CoefModel:  &coefModel,
		ScanOrder:  scanOrder,
		Dequant:    [2]int16{4, 4},
		Coeffs:     coeffs,
		QCoeffs:    qcoeffs,
		InitCtx:    0,
		TokenCache: &scratch,
	})

	var tokenCache [1024]byte
	tokenCache[0] = PtEnergyClass[ZeroToken]
	pt := vp9dec.GetCoefContext(scanOrder.Neighbors, &tokenCache, 1)
	oneToken, oneExtra := CoeffTokenExtraCost(1, 0)
	want := CoeffTreeTokenCost(coefModel[0][0][:], false, ZeroToken)
	want += oneExtra + CoeffTreeTokenCost(coefModel[1][pt][:],
		true, oneToken)
	tokenCache[scanOrder.Scan[1]] = PtEnergyClass[oneToken]
	eobCtx := vp9dec.GetCoefContext(scanOrder.Neighbors, &tokenCache, 2)
	want += CoeffTreeTokenCost(coefModel[1][eobCtx][:], false, EobToken)

	overcharged := CoeffTreeTokenCost(coefModel[0][0][:], false, ZeroToken)
	overcharged += oneExtra + CoeffTreeTokenCost(coefModel[1][pt][:],
		false, oneToken)
	overcharged += CoeffTreeTokenCost(coefModel[1][eobCtx][:], false, EobToken)

	if got != want {
		t.Fatalf("slow cost = %d, want %d", got, want)
	}
	if got == overcharged {
		t.Fatalf("slow cost charged full tree after ZERO token: got %d", got)
	}
}

func TestCoeffBlockRateCostUsesQCoeffEOBAndFastCosting(t *testing.T) {
	var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	fillCoefCostModelForTest(&coefModel, 128)

	const tx = common.Tx4x4
	dequant := [2]int16{4, 4}
	var coeffs [16]int16
	var qcoeffs [16]int16
	qcoeffs[0] = 1
	var scratch [1024]byte

	input := CoeffBlockRateCostInput{
		TxSize:     tx,
		CoefModel:  &coefModel,
		ScanOrder:  common.DefaultScanOrders[tx],
		Dequant:    dequant,
		Coeffs:     coeffs[:],
		QCoeffs:    qcoeffs[:],
		InitCtx:    0,
		TokenCache: &scratch,
	}
	want := 5 * VP9CostBit(128, 0)
	eobOnly := CoeffTreeTokenCost(coefModel[0][0][:], false, EobToken)

	slow := CoeffBlockRateCost(input)
	if slow != want {
		t.Fatalf("slow cost = %d, want %d", slow, want)
	}
	if slow == eobOnly {
		t.Fatalf("slow cost collapsed to EOB-only cost %d", eobOnly)
	}

	input.Fast = true
	fast := CoeffBlockRateCost(input)
	if fast != want {
		t.Fatalf("fast cost = %d, want %d", fast, want)
	}
	if fast != slow {
		t.Fatalf("fast cost = %d, want slow cost %d", fast, slow)
	}
}

func fillCoefCostModelForTest(
	model *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8,
	p uint8,
) {
	for band := range model {
		for ctx := range model[band] {
			for node := range model[band][ctx] {
				model[band][ctx][node] = p
			}
		}
	}
}
