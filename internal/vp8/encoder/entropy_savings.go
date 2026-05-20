package encoder

import vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"

// ReferenceFrameCosts ports libvpx v1.16.0 vp8/encoder/bitstream.c
// vp8_calc_ref_frame_costs. The four-entry result is indexed by libvpx's
// MAX_REF_FRAMES order: INTRA, LAST, GOLDEN, ALTREF.
func ReferenceFrameCosts(probIntra, probLast, probGarf int) (intra, last, golden, alt int) {
	probIntra = clampProb255(probIntra)
	probLast = clampProb255(probLast)
	probGarf = clampProb255(probGarf)
	costZeroIntra := vp8tables.ProbCost[probIntra]
	costOneIntra := vp8tables.ProbCost[255-probIntra]
	costZeroLast := vp8tables.ProbCost[probLast]
	costOneLast := vp8tables.ProbCost[255-probLast]
	costZeroGarf := vp8tables.ProbCost[probGarf]
	costOneGarf := vp8tables.ProbCost[255-probGarf]
	intra = costZeroIntra
	last = costOneIntra + costZeroLast
	golden = costOneIntra + costOneLast + costZeroGarf
	alt = costOneIntra + costOneLast + costOneGarf
	return intra, last, golden, alt
}

// ReferenceFrameEntropySavings ports the inter-frame branch of libvpx v1.16.0
// vp8_estimate_entropy_savings from vp8/encoder/bitstream.c.
func ReferenceFrameEntropySavings(keyFrame bool, rfctIntra, rfctLast, rfctGolden, rfctAltRef int, probIntra, probLast, probGarf int) int {
	if keyFrame {
		return 0
	}
	if rfctIntra < 0 {
		rfctIntra = 0
	}
	if rfctLast < 0 {
		rfctLast = 0
	}
	if rfctGolden < 0 {
		rfctGolden = 0
	}
	if rfctAltRef < 0 {
		rfctAltRef = 0
	}
	rfInter := rfctLast + rfctGolden + rfctAltRef
	if rfctIntra+rfInter == 0 {
		return 0
	}
	newIntra := rfctIntra * 255 / (rfctIntra + rfInter)
	if newIntra == 0 {
		newIntra = 1
	}
	newLast := 128
	if rfInter > 0 {
		newLast = rfctLast * 255 / rfInter
	}
	newGarf := 128
	if rfctGolden+rfctAltRef > 0 {
		newGarf = rfctGolden * 255 / (rfctGolden + rfctAltRef)
	}

	newCostIntra, newCostLast, newCostGolden, newCostAlt := ReferenceFrameCosts(newIntra, newLast, newGarf)
	oldCostIntra, oldCostLast, oldCostGolden, oldCostAlt := ReferenceFrameCosts(probIntra, probLast, probGarf)

	newTotal := rfctIntra*newCostIntra + rfctLast*newCostLast + rfctGolden*newCostGolden + rfctAltRef*newCostAlt
	oldTotal := rfctIntra*oldCostIntra + rfctLast*oldCostLast + rfctGolden*oldCostGolden + rfctAltRef*oldCostAlt
	return (oldTotal - newTotal) / 256
}

// DecideKeyFrame ports libvpx v1.16.0 vp8/encoder/onyx_if.c
// decide_key_frame for the auto-key recode decision.
func DecideKeyFrame(thisFramePercentIntra, lastFramePercentIntra int, refreshGoldenFrame bool) bool {
	if (thisFramePercentIntra == 100 && thisFramePercentIntra > lastFramePercentIntra+2) ||
		(thisFramePercentIntra > 95 && thisFramePercentIntra >= lastFramePercentIntra+5) {
		return true
	}
	if !refreshGoldenFrame {
		if (thisFramePercentIntra > 60 && thisFramePercentIntra > lastFramePercentIntra*2) ||
			(thisFramePercentIntra > 75 && thisFramePercentIntra > lastFramePercentIntra*3/2) ||
			(thisFramePercentIntra > 90 && thisFramePercentIntra > lastFramePercentIntra+10) {
			return true
		}
	}
	return false
}

func clampProb255(p int) int {
	return min(max(p, 0), 255)
}
