package govpx

import (
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// libvpxCalcRefFrameCosts ports vp8_calc_ref_frame_costs from
// vp8/encoder/bitstream.c. The four-entry result is indexed by libvpx's
// MAX_REF_FRAMES order (INTRA, LAST, GOLDEN, ALTREF):
//
//	cost[INTRA]  = cost_zero(prob_intra)
//	cost[LAST]   = cost_one(prob_intra) + cost_zero(prob_last)
//	cost[GOLDEN] = cost_one(prob_intra) + cost_one(prob_last) + cost_zero(prob_garf)
//	cost[ALTREF] = cost_one(prob_intra) + cost_one(prob_last) + cost_one(prob_garf)
//
// where cost_zero(p) = ProbCost[p] and cost_one(p) = ProbCost[255-p].
// Probabilities are clamped to [0,255] (libvpx asserts the same range).
func libvpxCalcRefFrameCosts(probIntra, probLast, probGarf int) (intra, last, golden, alt int) {
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

// libvpxRefFrameEntropySavings ports the inter-frame branch of
// vp8_estimate_entropy_savings from vp8/encoder/bitstream.c. Given the
// per-MB ref-frame usage counts (rfctIntra, rfctLast, rfctGolden,
// rfctAltRef) and the previous-frame committed ref-frame probabilities
// (probIntra/probLast/probGarf), it computes the estimated entropy
// savings (in 1/256 of a bit, dropped to whole bits via /256 like
// libvpx) of switching to the new ref-frame probabilities derived from
// the rfct counts:
//
//	new_intra = max(1, rf_intra*255/(rf_intra+rf_inter))
//	new_last  = rf_inter ? rfct[LAST]*255/rf_inter         : 128
//	new_garf  = (rfct[GOLDEN]+rfct[ALTREF]) ?
//	             rfct[GOLDEN]*255/(rfct[GOLDEN]+rfct[ALTREF]) : 128
//
// The result is `(oldtotal - newtotal) / 256`, where oldtotal/newtotal
// are sum(rfct[i] * ref_frame_cost[i]) under the prior and proposed
// ref-frame probabilities respectively. Returns 0 for key frames (libvpx
// gates the inter-frame branch on `frame_type != KEY_FRAME`).
func libvpxRefFrameEntropySavings(keyFrame bool, rfctIntra, rfctLast, rfctGolden, rfctAltRef int, probIntra, probLast, probGarf int) int {
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

	newCostIntra, newCostLast, newCostGolden, newCostAlt := libvpxCalcRefFrameCosts(newIntra, newLast, newGarf)
	oldCostIntra, oldCostLast, oldCostGolden, oldCostAlt := libvpxCalcRefFrameCosts(probIntra, probLast, probGarf)

	newTotal := rfctIntra*newCostIntra + rfctLast*newCostLast + rfctGolden*newCostGolden + rfctAltRef*newCostAlt
	oldTotal := rfctIntra*oldCostIntra + rfctLast*oldCostLast + rfctGolden*oldCostGolden + rfctAltRef*oldCostAlt
	return (oldTotal - newTotal) / 256
}

func clampProb255(p int) int {
	if p < 0 {
		return 0
	}
	if p > 255 {
		return 255
	}
	return p
}
