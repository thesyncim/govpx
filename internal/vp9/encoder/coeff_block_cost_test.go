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

func TestCoeffBlockRateCostFastDoesNotClearTokenCache(t *testing.T) {
	var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	fillCoefCostModelForTest(&coefModel, 128)

	const tx = common.Tx32x32
	dequant := [2]int16{4, 4}
	coeffs := make([]int16, vp9dec.MaxEobForTxSize(tx))
	qcoeffs := make([]int16, len(coeffs))
	qcoeffs[common.DefaultScanOrders[tx].Scan[37]] = 2
	var scratch [1024]byte
	for i := range scratch {
		scratch[i] = 0xa5
	}

	got := CoeffBlockRateCost(CoeffBlockRateCostInput{
		TxSize:     tx,
		CoefModel:  &coefModel,
		ScanOrder:  common.DefaultScanOrders[tx],
		Dequant:    dequant,
		Coeffs:     coeffs,
		QCoeffs:    qcoeffs,
		InitCtx:    0,
		Fast:       true,
		TokenCache: &scratch,
	})
	if got == 0 {
		t.Fatalf("fast cost = 0, want non-zero populated block cost")
	}
	for i, got := range scratch {
		if got != 0xa5 {
			t.Fatalf("fast cost mutated TokenCache[%d] = %#x, want sentinel", i, got)
		}
	}
}

func TestCoeffBlockRateCostKnownEOBMatchesScannedEOB(t *testing.T) {
	var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	fillCoefCostModelForTest(&coefModel, 128)
	var costTable CoeffTreeTokenCostTable
	FillCoeffTreeTokenCostTable(&coefModel, &costTable)

	const tx = common.Tx32x32
	dequant := [2]int16{4, 4}
	coeffs := make([]int16, vp9dec.MaxEobForTxSize(tx))
	qcoeffs := make([]int16, len(coeffs))
	scan := common.DefaultScanOrders[tx].Scan
	qcoeffs[scan[0]] = 1
	qcoeffs[scan[37]] = -2
	qcoeffs[scan[121]] = 3
	var scratch [1024]byte
	input := CoeffBlockRateCostInput{
		TxSize:     tx,
		CoefModel:  &coefModel,
		ScanOrder:  common.DefaultScanOrders[tx],
		Dequant:    dequant,
		Coeffs:     coeffs,
		QCoeffs:    qcoeffs,
		InitCtx:    0,
		Fast:       true,
		TokenCache: &scratch,
		CostTable:  &costTable,
	}
	scanned := CoeffBlockRateCost(input)
	input.EOB = 122
	input.EOBKnown = true
	known := CoeffBlockRateCost(input)
	if known != scanned {
		t.Fatalf("known EOB cost = %d, want scanned %d", known, scanned)
	}
	direct := CoeffBlockRateCostFastKnownQCoeffTable(tx, &costTable, scan,
		qcoeffs, input.InitCtx, input.EOB)
	if direct != scanned {
		t.Fatalf("direct known EOB cost = %d, want scanned %d", direct, scanned)
	}
	var zeroQCoeffs [1024]int16
	zeroDirect := CoeffBlockRateCostFastKnownQCoeffTable(tx, &costTable, scan,
		zeroQCoeffs[:], input.InitCtx, 0)
	input.QCoeffs = zeroQCoeffs[:]
	input.EOB = 0
	zeroKnown := CoeffBlockRateCost(input)
	if zeroDirect != zeroKnown {
		t.Fatalf("direct EOB-only cost = %d, want generic %d", zeroDirect, zeroKnown)
	}
}

var coeffBlockRateBenchSink int

func BenchmarkCoeffBlockRateCostFastQCoeffTx32x32(b *testing.B) {
	var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	fillCoefCostModelForTest(&coefModel, 128)
	var costTable CoeffTreeTokenCostTable
	FillCoeffTreeTokenCostTable(&coefModel, &costTable)

	const tx = common.Tx32x32
	dequant := [2]int16{4, 4}
	coeffs := make([]int16, vp9dec.MaxEobForTxSize(tx))
	qcoeffs := make([]int16, len(coeffs))
	scan := common.DefaultScanOrders[tx].Scan
	qcoeffs[scan[0]] = 1
	qcoeffs[scan[37]] = -2
	qcoeffs[scan[121]] = 3
	var scratch [1024]byte
	input := CoeffBlockRateCostInput{
		TxSize:     tx,
		CoefModel:  &coefModel,
		ScanOrder:  common.DefaultScanOrders[tx],
		Dequant:    dequant,
		Coeffs:     coeffs,
		QCoeffs:    qcoeffs,
		InitCtx:    0,
		Fast:       true,
		TokenCache: &scratch,
		CostTable:  &costTable,
		EOB:        122,
		EOBKnown:   true,
	}

	total := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		total += CoeffBlockRateCost(input)
	}
	coeffBlockRateBenchSink = total
}

func BenchmarkCoeffBlockRateCostFastKnownQCoeffTableTx32x32(b *testing.B) {
	var coefModel [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	fillCoefCostModelForTest(&coefModel, 128)
	var costTable CoeffTreeTokenCostTable
	FillCoeffTreeTokenCostTable(&coefModel, &costTable)

	const tx = common.Tx32x32
	qcoeffs := make([]int16, vp9dec.MaxEobForTxSize(tx))
	scan := common.DefaultScanOrders[tx].Scan
	qcoeffs[scan[0]] = 1
	qcoeffs[scan[37]] = -2
	qcoeffs[scan[121]] = 3

	total := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		total += CoeffBlockRateCostFastKnownQCoeffTable(tx, &costTable, scan,
			qcoeffs, 0, 122)
	}
	coeffBlockRateBenchSink = total
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
