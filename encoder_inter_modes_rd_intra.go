package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type interIntraModeRDResult struct {
	mode         vp8enc.InterFrameMacroblockMode
	score        int
	yrd          int
	rate         int
	rateY        int
	rateUV       int
	distortion   int
	distortionUV int
	staleY2      staleY2Snapshot
}

func (e *VP8Encoder) estimateInterIntraModeRDScore(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant) (interIntraModeRDResult, bool) {
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := 0
	if e.activityMapValid {
		if adjustment, ok := e.tunedZbinAdjustment(mbRow, mbCol); ok {
			actZbinAdj = adjustment
		}
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	pickerProbs := e.pickerCoefProbs()
	// libvpx propagates x->rdmult (activity-masked under --tune=ssim) into
	// every per-block RDCOST inside vp8/encoder/rdopt.c
	// rd_pick_intra4x4block, rd_pick_intra_mbuv_mode, and the whole-block
	// intra Y picker. Pre-compute the activity-tuned (rdMult, rdDiv) pair
	// once for this MB and thread it through the per-sub-block 4x4 picker
	// AND the UV picker so all the intra mode comparisons agree with
	// libvpx's tuned-rdmult winners.
	rdMult, rdDiv := 0, 0
	if e.activityMapValid {
		rdMult, rdDiv = libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
		rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	}
	if mbMode == vp8common.BPred {
		bModes, bRate, bDist, ok := predictBestBPredLumaModeRDWithRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, pickerProbs, e.modeProbs.BMode[:], fastQuant, rdMult, rdDiv)
		if !ok {
			return interIntraModeRDResult{}, false
		}
		yRate := bRate + e.interIntraYModeRate(vp8common.BPred)
		// Mirror libvpx vp8/encoder/rdopt.c rd_pick_intra4x4mby_modes lines
		// 591/634/637: libvpx starts the cumulative cost with
		// mbmode_cost[B_PRED] and rejects (returns INT_MAX) when the
		// final rdcost(cost, distortion) reaches best_rd. govpx's
		// predictBestBPredLumaModeRD bails per-block but accumulates
		// totalRate from 0 (without the mode cost), so a B_PRED candidate
		// whose final yrd including the mode cost matches or exceeds
		// best_yrd survives the in-loop bail. Mirror libvpx's reject
		// gate here so those candidates are dropped before they reach
		// the became_best comparison in the inter RD picker.
		yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate, bDist)
		if e.activityMapValid {
			yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate, bDist)
		}
		if bestRD > 0 && yrd >= bestRD {
			return interIntraModeRDResult{}, false
		}
		uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant, rdMult, rdDiv)
		if !ok {
			return interIntraModeRDResult{}, false
		}
		uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
		uvTokenRate := uvRate - uvModeRate
		yrd = rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+uvModeRate, bDist)
		if e.activityMapValid {
			yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate+uvModeRate, bDist)
		}
		rate := yRate + uvRate + e.interIntraMacroblockModeRate()
		score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		if e.activityMapValid {
			score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		}
		distortion := bDist + uvDist
		return interIntraModeRDResult{
			mode:         vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: uvMode, BModes: bModes},
			score:        score,
			yrd:          yrd,
			rate:         rate,
			rateY:        bRate,
			rateUV:       uvTokenRate,
			distortion:   distortion,
			distortionUV: uvDist,
		}, true
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return interIntraModeRDResult{}, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return interIntraModeRDResult{}, false
	}
	yRate, yDist, y2EOB, y2QCoeff, yACEOBCount := wholeBlockYTransformRDWithEOBs(src, &e.analysis.Img, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, pickerProbs, fastQuant)
	uvMode, uvRate, uvDist, uvEOBSum, ok := predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src, qIndex, zbinOverQuant, actZbinAdj, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant, rdMult, rdDiv)
	if !ok {
		return interIntraModeRDResult{}, false
	}
	modeRate := e.interIntraYModeRate(mbMode)
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	uvTokenRate := uvRate - uvModeRate
	rate := yRate + uvRate + modeRate + e.interIntraMacroblockModeRate()
	// Port libvpx vp8/encoder/rdopt.c calculate_final_rd_costs (lines
	// 1684-1714) tteob==0 rate2 backout for the intra-in-inter-loop path.
	// libvpx computes:
	//   has_y2_block = (mode != SPLITMV && mode != B_PRED)  // true here
	//   tteob = eobs[Y2] + sum(eobs[0..15] > has_y2_block)
	//   if ref_frame == INTRA_FRAME: tteob += uv_intra_tteob
	// When tteob == 0 the picker drops rate_y + rate_uv from rate2 and
	// adds the skip-flag delta in their place. Without this, govpx
	// charges DC_PRED / V_PRED / H_PRED / TM_PRED the full coefficient
	// rate even when every Y AC and every UV coefficient quantizes to
	// zero — measured +20826-bit rate inflation on flat-Y screen-content
	// MBs (1280x720 task #341 fixture, frame 1 MB(5,0) DC_PRED:
	// govpx rate=20838 vs libvpx rate=1012), driving the picker to spend
	// bits on NEWMV+LAST candidates whose `became_best=true` flip leaves
	// libvpx with DC_PRED+all-zero coefficients. Mirror libvpx verbatim.
	tteob := int(y2EOB) + yACEOBCount + uvEOBSum
	mbSkipCoeff := tteob == 0
	if mbSkipCoeff {
		rate -= yRate + uvRate - uvModeRate
		// libvpx also adjusts the skip-flag bit cost: line 1709-1712 adds
		// (cost_bit(prob_skip_false, 1) - cost_bit(prob_skip_false, 0)) when
		// the skip flag flips from 0 to 1. interIntraMacroblockModeRate
		// already accounts for cost_bit(prob, 0); add the delta.
		rate += e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		uvTokenRate = 0
	}
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
	yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+modeRate+uvModeRate, yDist)
	if e.activityMapValid {
		score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate+modeRate+uvModeRate, yDist)
	}
	distortion := yDist + uvDist
	var staleY2 staleY2Snapshot
	if oracleTraceBuild {
		staleY2 = makeOracleStaleY2Snapshot(uint8(y2EOB), y2QCoeff)
	}
	return interIntraModeRDResult{
		mode:         vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: uvMode, MBSkipCoeff: mbSkipCoeff},
		score:        score,
		yrd:          yrd,
		rate:         rate,
		rateY:        yRate,
		rateUV:       uvTokenRate,
		distortion:   distortion,
		distortionUV: uvDist,
		staleY2:      staleY2,
	}, true
}
