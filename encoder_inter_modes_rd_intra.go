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
	if mbMode == vp8common.BPred {
		bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, pickerProbs, fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
		}
		uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
		}
		yRate := bRate + e.interIntraYModeRate(vp8common.BPred)
		rate := yRate + uvRate + e.interIntraMacroblockModeRate()
		score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate, bDist)
		if e.activityMapValid {
			score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
			yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate, bDist)
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
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
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
