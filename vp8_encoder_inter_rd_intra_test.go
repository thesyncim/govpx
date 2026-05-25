package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestEstimateInterIntraModeRDScoreAddsLibvpxPenalty(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	result, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.DCPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	decMode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, 0, 0, &decMode, &e.reconstructScratch) {
		t.Fatalf("predictAnalysisMacroblock returned false")
	}
	yRate, yDist, _, _ := wholeBlockYTransformRD(sourceImageFromPublic(src), &e.analysis.Img, 0, 0, 0, 0, nil, nil, &quant, &e.coefProbs, false)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD mode=%v ok=false", uvMode)
	}
	rate := yRate + uvRate + intraYModeRate(false, vp8common.DCPred) + e.interIntraMacroblockModeRate()
	want := vp8enc.RDModeScoreWithZbin(20, 0, rate, yDist+uvDist) + vp8enc.InterIntraRDPenalty(20)
	if result.score != want {
		t.Fatalf("inter-intra RD score = %d, want %d with libvpx penalty", result.score, want)
	}
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	wantYRD := vp8enc.RDModeScoreWithZbin(20, 0, yRate+intraYModeRate(false, vp8common.DCPred)+uvModeRate, yDist)
	if result.yrd != wantYRD {
		t.Fatalf("inter-intra YRD = %d, want libvpx Y plus UV-mode RD %d", result.yrd, wantYRD)
	}
}

func TestEstimateInterIntraModeRDScoreUsesLiveInterIntraModeProbs(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.YMode = [vp8tables.YModeProbCount]uint8{250, 4, 220, 9}
	e.modeProbs.UVMode = [vp8tables.UVModeProbCount]uint8{245, 8, 230}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	result, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.DCPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	decMode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, 0, 0, &decMode, &e.reconstructScratch) {
		t.Fatalf("predictAnalysisMacroblock returned false")
	}
	yRate, yDist, _, _ := wholeBlockYTransformRD(sourceImageFromPublic(src), &e.analysis.Img, 0, 0, 0, 0, nil, nil, &quant, &e.coefProbs, false)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, e.modeProbs.UVMode[:], false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRDWithProbs mode=%v ok=false", uvMode)
	}
	liveYModeRate := e.interIntraYModeRate(vp8common.DCPred)
	if liveYModeRate == intraYModeRate(false, vp8common.DCPred) {
		t.Fatalf("live Y mode rate still matches default, test fixture is ineffective")
	}
	rate := yRate + uvRate + liveYModeRate + e.interIntraMacroblockModeRate()
	want := vp8enc.RDModeScoreWithZbin(20, 0, rate, yDist+uvDist) + vp8enc.InterIntraRDPenalty(20)
	if result.score != want {
		t.Fatalf("inter-intra RD score = %d, want %d from live Y/UV mode probabilities", result.score, want)
	}
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	wantYRD := vp8enc.RDModeScoreWithZbin(20, 0, yRate+liveYModeRate+uvModeRate, yDist)
	if result.yrd != wantYRD {
		t.Fatalf("inter-intra YRD = %d, want %d from live Y and UV-mode probabilities", result.yrd, wantYRD)
	}
}

func TestEstimateInterIntraBPredYRDExcludesUVTokensAndRefCosts(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	result, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.BPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore BPred returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, maxInt(), &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD mode=%v bModes=%v ok=false", uvMode, bModes)
	}
	yRate := bRate + intraYModeRate(false, vp8common.BPred)
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	wantYRD := vp8enc.RDModeScoreWithZbin(20, 0, yRate+uvModeRate, bDist)
	if result.yrd != wantYRD {
		t.Fatalf("BPred YRD = %d, want libvpx Y plus UV-mode RD %d", result.yrd, wantYRD)
	}
	rate := yRate + uvRate + e.interIntraMacroblockModeRate()
	want := vp8enc.RDModeScoreWithZbin(20, 0, rate, bDist+uvDist) + vp8enc.InterIntraRDPenalty(20)
	if result.score != want {
		t.Fatalf("BPred RD score = %d, want %d with UV/ref costs and penalty", result.score, want)
	}
}
