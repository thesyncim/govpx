package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

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
	oneToken, oneExtra := encoder.CoeffTokenExtraCost(1, 0)
	want := encoder.CoeffTreeTokenCost((*coefModel)[0][0][:], false,
		encoder.ZeroToken)
	want += oneExtra + encoder.CoeffTreeTokenCost((*coefModel)[1][pt][:],
		true, oneToken)
	tokenCache[scanOrder.Scan[1]] = encoder.PtEnergyClass[oneToken]
	eobCtx := vp9dec.GetCoefContext(scanOrder.Neighbors, &tokenCache, 2)
	want += encoder.CoeffTreeTokenCost((*coefModel)[1][eobCtx][:], false,
		encoder.EobToken)

	overcharged := encoder.CoeffTreeTokenCost((*coefModel)[0][0][:], false,
		encoder.ZeroToken)
	overcharged += oneExtra + encoder.CoeffTreeTokenCost((*coefModel)[1][pt][:],
		false, oneToken)
	overcharged += encoder.CoeffTreeTokenCost((*coefModel)[1][eobCtx][:], false,
		encoder.EobToken)

	if got != want {
		t.Fatalf("slow cost = %d, want %d", got, want)
	}
	if got == overcharged {
		t.Fatalf("slow cost charged full tree after ZERO token: got %d", got)
	}
}
