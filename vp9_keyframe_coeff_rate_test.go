package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9KeyframeCoeffRateUsesIntraModeScan(t *testing.T) {
	var e VP9Encoder
	for tx := range e.fc.CoefProbs {
		for plane := range e.fc.CoefProbs[tx] {
			for ref := range e.fc.CoefProbs[tx][plane] {
				for band := range e.fc.CoefProbs[tx][plane][ref] {
					for ctx := range e.fc.CoefProbs[tx][plane][ref][band] {
						for node := range e.fc.CoefProbs[tx][plane][ref][band][ctx] {
							e.fc.CoefProbs[tx][plane][ref][band][ctx][node] = 128
						}
					}
				}
			}
		}
	}

	const tx = common.Tx4x4
	mode := common.VPred
	dequant := [2]int16{16, 17}
	var coeffs [16]int16

	wantScan := common.GetScan(tx, 0, 0, false, mode)
	defaultScan := common.DefaultScanOrders[tx]
	found := false
	for raster := range coeffs {
		if raster == 0 {
			continue
		}
		coeffs = [16]int16{}
		coeffs[raster] = dequant[1]
		want := e.vp9KeyframeCoeffBlockRateCostPlane(tx, 0, wantScan,
			dequant, coeffs[:], 0)
		defaultRate := e.vp9KeyframeCoeffBlockRateCostPlane(tx, 0, defaultScan,
			dequant, coeffs[:], 0)
		if want != defaultRate {
			found = true
			got := e.vp9KeyframeCoeffBlockRateCost(tx, mode, false,
				dequant, coeffs[:], 0)
			if got != want {
				t.Fatalf("rate = %d, want %d from intra-mode scan", got, want)
			}
			if got == defaultRate {
				t.Fatalf("rate = default scan rate %d, want intra-mode scan rate %d",
					defaultRate, want)
			}
			break
		}
	}
	if !found {
		t.Fatalf("test setup did not find a coefficient that distinguishes scan orders")
	}
}
