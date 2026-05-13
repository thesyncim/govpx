package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) estimateInterIntraModeRDScore(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant) (vp8enc.InterFrameMacroblockMode, int, int, int, int, staleY2Snapshot, bool) {
	zbinOverQuant := e.rc.currentZbinOverQuant
	if e.activityMapValid {
		zbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, mbRow, mbCol)
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
		bModes, bRate, bDist, ok := predictBestBPredLumaModeRDWithRDConstants(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, pickerProbs, fastQuant, rdMult, rdDiv)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
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
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
		}
		uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant, rdMult, rdDiv)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
		}
		rate := yRate + uvRate + e.interIntraMacroblockModeRate()
		score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		if e.activityMapValid {
			score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		}
		distortion := bDist + uvDist
		return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: uvMode, BModes: bModes}, score, yrd, rate, distortion, staleY2Snapshot{}, true
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
	}
	yRate, yDist, y2EOB, y2QCoeff := wholeBlockYTransformRD(src, &e.analysis.Img, mbRow, mbCol, zbinOverQuant, aboveTok, leftTok, quant, pickerProbs, fastQuant)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant, rdMult, rdDiv)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
	}
	modeRate := e.interIntraYModeRate(mbMode)
	rate := yRate + uvRate + modeRate + e.interIntraMacroblockModeRate()
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
	yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+modeRate, yDist)
	if e.activityMapValid {
		score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate+modeRate, yDist)
	}
	distortion := yDist + uvDist
	var staleY2 staleY2Snapshot
	if oracleTraceBuild {
		staleY2 = makeOracleStaleY2Snapshot(uint8(y2EOB), y2QCoeff)
	}
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: uvMode}, score, yrd, rate, distortion, staleY2, true
}
