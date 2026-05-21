package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9CoeffBlockRateCostQUsesQCoeffEOBAndFastCosting(t *testing.T) {
	var e VP9Encoder
	fillVP9CoefProbsForTest(&e, 128)

	const tx = common.Tx4x4
	dequant := [2]int16{4, 4}
	var coeffs [16]int16
	var qcoeffs [16]int16
	qcoeffs[0] = 1

	want := 5 * vp9enc.VP9CostBit(128, 0)
	eobOnly := vp9enc.CoeffTreeTokenCost(
		e.fc.CoefProbs[tx][0][0][0][0][:], false, vp9enc.EobToken)

	e.sf.UseFastCoefCosting = 0
	slow := e.vp9KeyframeCoeffBlockRateCostPlaneQ(tx, 0,
		common.DefaultScanOrders[tx], dequant, coeffs[:], qcoeffs[:], 0)
	if slow != want {
		t.Fatalf("slow keyframe cost = %d, want %d", slow, want)
	}
	if slow == eobOnly {
		t.Fatalf("slow keyframe cost collapsed to EOB-only cost %d", eobOnly)
	}

	e.sf.UseFastCoefCosting = 1
	fast := e.vp9KeyframeCoeffBlockRateCostPlaneQ(tx, 0,
		common.DefaultScanOrders[tx], dequant, coeffs[:], qcoeffs[:], 0)
	if fast != want {
		t.Fatalf("fast keyframe cost = %d, want %d", fast, want)
	}
	if fast != slow {
		t.Fatalf("fast keyframe cost = %d, want slow cost %d", fast, slow)
	}

	interFast := e.vp9InterCoeffBlockRateCostQ(tx, 0, dequant,
		coeffs[:], qcoeffs[:], 0)
	if interFast != want {
		t.Fatalf("fast inter cost = %d, want %d", interFast, want)
	}
}

func fillVP9CoefProbsForTest(e *VP9Encoder, p uint8) {
	for tx := range e.fc.CoefProbs {
		for plane := range e.fc.CoefProbs[tx] {
			for ref := range e.fc.CoefProbs[tx][plane] {
				for band := range e.fc.CoefProbs[tx][plane][ref] {
					for ctx := range e.fc.CoefProbs[tx][plane][ref][band] {
						for node := range e.fc.CoefProbs[tx][plane][ref][band][ctx] {
							e.fc.CoefProbs[tx][plane][ref][band][ctx][node] = p
						}
					}
				}
			}
		}
	}
}
