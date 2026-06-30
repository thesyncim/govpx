package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestPredictBestKeyFrameIntraModeChoosesBPred(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	pred := testVP8Frame(t, 32, 32, 128, 128, 128)
	for i := range 16 {
		pred.Img.Y[15*pred.Img.YStride+16+i] = byte(40 + i*7)
		pred.Img.Y[(16+i)*pred.Img.YStride+15] = byte(210 - i*5)
	}
	pred.ExtendBorders()

	var genScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&pred.Img, 1, 1, &genScratch.Refs)
	yOff := 16*pred.Img.YStride + 16
	y := pred.Img.Y[yOff:]
	for block := range 16 {
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(vp8common.BHEPred, blockPred[:], 4, y, pred.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("predictAnalysisBPredBlock returned false")
		}
		vp8enc.CopyBPredBlock(blockPred[:], y, pred.Img.YStride, block)
		copyBPredBlockToSource(blockPred[:], 4, src, 1, 1, block)
	}
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			pred.Img.Y[row*pred.Img.YStride+col] = 128
		}
	}

	var scratch vp8dec.IntraReconstructionScratch
	quant := testMacroblockQuant(20)
	mode, _, ok := predictBestKeyFrameIntraMode(sourceImageFromPublic(src), 20, 1, 1, nil, nil, nil, nil, &quant, &pred.Img, &scratch, false)
	if !ok {
		t.Fatalf("predictBestKeyFrameIntraMode returned ok=false")
	}
	if mode.YMode != vp8common.BPred || mode.UVMode != vp8common.DCPred {
		t.Fatalf("mode = %+v, want B_PRED/DC chroma", mode)
	}
	if mode.BModes[0] != vp8common.BHEPred {
		t.Fatalf("B mode[0] = %v, want B_HE_PRED", mode.BModes[0])
	}
}

func TestEstimateFastBPredIntraModeRestrictsCandidatesLikeLibvpx(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	e.analysis = testVP8Frame(t, 32, 32, 128, 128, 128)
	for i := range 16 {
		e.analysis.Img.Y[15*e.analysis.Img.YStride+16+i] = byte(30 + i*11)
		e.analysis.Img.Y[(16+i)*e.analysis.Img.YStride+15] = byte(220 - i*9)
	}
	e.analysis.ExtendBorders()

	var genScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&e.analysis.Img, 1, 1, &genScratch.Refs)
	yOff := 16*e.analysis.Img.YStride + 16
	y := e.analysis.Img.Y[yOff:]
	for block := range 16 {
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(vp8common.BLDPred, blockPred[:], 4, y, e.analysis.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("predictAnalysisBPredBlock returned false")
		}
		vp8enc.CopyBPredBlock(blockPred[:], y, e.analysis.Img.YStride, block)
		copyBPredBlockToSource(blockPred[:], 4, src, 1, 1, block)
	}
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			e.analysis.Img.Y[row*e.analysis.Img.YStride+col] = 128
		}
	}

	quant := testMacroblockQuant(20)
	mode, _, _, _, _, ok := e.estimateFastBPredIntraModeScore(sourceImageFromPublic(src), 1, 1, 20, maxInt(), &quant)
	if !ok {
		t.Fatalf("estimateFastBPredIntraModeScore returned ok=false")
	}
	for block, bMode := range mode.BModes {
		if bMode > vp8common.BHEPred {
			t.Fatalf("fast B mode[%d] = %v, want libvpx non-RD candidate <= B_HE_PRED", block, bMode)
		}
	}
}

func TestPredictBestBPredLumaModeRDReconstructsChosenBlocks(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := range 4 {
		for col := range 4 {
			src.Y[row*src.YStride+col] = 200
		}
	}
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	quant := testMacroblockQuant(4)
	var scratch vp8dec.IntraReconstructionScratch
	probs := vp8tables.DefaultCoefProbs

	_, rate, dist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), 4, 0, true, 0, 0, nil, nil, nil, nil, &quant, &pred.Img, &scratch, 0, &probs, false)

	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}
	if rate <= 0 || dist < 0 {
		t.Fatalf("rate=%d dist=%d, want positive rate and non-negative distortion", rate, dist)
	}
	if pred.Img.Y[0] <= 128 {
		t.Fatalf("reconstructed block sample = %d, want above raw predictor 128", pred.Img.Y[0])
	}
}

func TestPredictBestIntraChromaModeRDUsesTransformTokenCost(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := range 8 {
		for col := range 8 {
			src.U[row*src.UStride+col] = byte(24 + ((row*37 + col*19) & 0xff))
			src.V[row*src.VStride+col] = byte(224 - ((row*11 + col*43) & 0x7f))
		}
	}
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	quant := testRegularMacroblockQuant(t, 20)
	probs := vp8tables.DefaultCoefProbs
	var scratch vp8dec.IntraReconstructionScratch

	mode, rate, dist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, true, 0, 0, nil, nil, &quant, &pred.Img, &scratch, &probs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD returned ok=false")
	}
	if mode < vp8common.DCPred || mode > vp8common.TMPred {
		t.Fatalf("UV mode = %v, want valid intra chroma mode", mode)
	}
	if modeRate := intraUVModeRate(true, mode); rate <= modeRate {
		t.Fatalf("UV rate = %d, want mode rate %d plus transform token cost", rate, modeRate)
	}

	chosenPred := testVP8Frame(t, 16, 16, 128, 128, 128)
	var chosenScratch vp8dec.IntraReconstructionScratch
	if !predictAnalysisChroma(&chosenPred.Img, 0, 0, mode, &chosenScratch) {
		t.Fatalf("predictAnalysisChroma returned false")
	}
	tokenRate, wantDist := wholeBlockChromaTransformRD(sourceImageFromPublic(src), &chosenPred.Img, 0, 0, 0, 0, nil, nil, &quant, &probs, false)
	wantRate := intraUVModeRate(true, mode) + tokenRate
	if rate != wantRate || dist != wantDist {
		t.Fatalf("UV RD = rate:%d dist:%d, want transform/token rate:%d dist:%d", rate, dist, wantRate, wantDist)
	}
	if sse := vp8enc.MacroblockChromaSSE(sourceImageFromPublic(src), &chosenPred.Img, 0, 0); dist == sse {
		t.Fatalf("UV distortion = %d, want transform-domain error rather than chroma SSE", dist)
	}
}

func TestPredictBestIntraChromaModeRDEOBsUseFinalTrialState(t *testing.T) {
	quant := testRegularMacroblockQuant(t, 20)
	probs := vp8tables.DefaultCoefProbs
	const mbRow = 1
	const mbCol = 1

	for seed := range 4096 {
		src := testImage(32, 32)
		fillImage(src, 128, 128, 128)
		for row := range 16 {
			for col := range 16 {
				src.U[row*src.UStride+col] = byte(32 + ((seed*17 + row*29 + col*43) & 0xbf))
				src.V[row*src.VStride+col] = byte(224 - ((seed*31 + row*11 + col*37) & 0xbf))
			}
		}
		newPred := func() *vp8common.FrameBuffer {
			pred := testVP8Frame(t, 32, 32, 128, 128, 128)
			for row := range 16 {
				for col := range 16 {
					pred.Img.U[row*pred.Img.UStride+col] = byte(48 + ((seed*13 + row*7 + col*23) & 0x7f))
					pred.Img.V[row*pred.Img.VStride+col] = byte(208 - ((seed*19 + row*17 + col*5) & 0x7f))
				}
			}
			pred.ExtendBorders()
			return &pred
		}

		bestMode := vp8common.DCPred
		bestRate := 0
		bestDist := 0
		bestCost := 0
		bestEOBSum := 0
		liveEOBSum := 0
		for i, uvMode := range wholeBlockIntraUVModeCandidates {
			pred := newPred()
			var scratch vp8dec.IntraReconstructionScratch
			if !predictAnalysisChroma(&pred.Img, mbRow, mbCol, uvMode, &scratch) {
				t.Fatalf("predictAnalysisChroma(%v) returned false", uvMode)
			}
			tokenRate, dist, eobSum := wholeBlockChromaTransformRDWithEOBs(sourceImageFromPublic(src), &pred.Img, mbRow, mbCol, 0, 0, nil, nil, &quant, &probs, false, nil)
			liveEOBSum = eobSum
			rate := intraUVModeRate(false, uvMode) + tokenRate
			cost := vp8enc.RDModeScoreWithZbin(20, 0, rate, dist)
			if i == 0 || cost < bestCost {
				bestMode = uvMode
				bestRate = rate
				bestDist = dist
				bestCost = cost
				bestEOBSum = eobSum
			}
		}
		if bestEOBSum == liveEOBSum {
			continue
		}

		pred := newPred()
		var scratch vp8dec.IntraReconstructionScratch
		gotMode, gotRate, gotDist, gotEOBSum, ok := predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(sourceImageFromPublic(src), 20, 0, 0, false, mbRow, mbCol, nil, nil, &quant, &pred.Img, &scratch, &probs, nil, false, 0, 0, nil)
		if !ok {
			t.Fatalf("predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs returned ok=false")
		}
		if gotMode != bestMode || gotRate != bestRate || gotDist != bestDist {
			t.Fatalf("best UV RD = mode:%v rate:%d dist:%d, want mode:%v rate:%d dist:%d", gotMode, gotRate, gotDist, bestMode, bestRate, bestDist)
		}
		if gotEOBSum != liveEOBSum {
			t.Fatalf("UV EOB sum = %d, want live final-trial EOB sum %d, not winning-mode EOB sum %d", gotEOBSum, liveEOBSum, bestEOBSum)
		}
		return
	}

	t.Fatalf("test fixture did not find a UV-mode case where winning EOB and final-trial EOB differ")
}

func TestCoefficientBlockTokenRateUsesEntropyCosts(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero [16]int16

	zeroRate := vp8enc.CoefficientBlockTokenRate(&probs, 3, 0, 0, &zero, 0)
	wantZero := vp8enc.TreeTokenCost(vp8tables.CoefTree[:], probs[3][0][0][:], vp8tables.DCTEOBToken)
	if zeroRate != wantZero {
		t.Fatalf("zero token rate = %d, want %d", zeroRate, wantZero)
	}

	positive := [16]int16{0: 1}
	positiveRate := vp8enc.CoefficientBlockTokenRate(&probs, 3, 0, 0, &positive, 1)
	negative := [16]int16{0: -1}
	negativeRate := vp8enc.CoefficientBlockTokenRate(&probs, 3, 0, 0, &negative, 1)
	if positiveRate <= zeroRate {
		t.Fatalf("positive token rate = %d, zero = %d, want nonzero token to cost more", positiveRate, zeroRate)
	}
	if negativeRate <= zeroRate {
		t.Fatalf("negative token rate = %d, zero = %d, want nonzero token to cost more", negativeRate, zeroRate)
	}

	zeroThenOne := [16]int16{1: 1}
	zeroThenOneRate := vp8enc.CoefficientBlockTokenRate(&probs, 3, 0, 0, &zeroThenOne, 2)
	p0 := probs[3][0][0]
	p1 := probs[3][vp8tables.CoefBandsTable[1]][0]
	p2 := probs[3][vp8tables.CoefBandsTable[2]][vp8tables.PrevTokenClass[vp8tables.OneToken]]
	wantZeroThenOne := vp8enc.BoolBitCost(p0[0], 1) +
		vp8enc.BoolBitCost(p0[1], 0) +
		vp8enc.NonZeroCoeffTokenRate(p1, vp8tables.OneToken) +
		vp8enc.BoolBitCost(128, 0) +
		vp8enc.TreeTokenCost(vp8tables.CoefTree[:], p2[:], vp8tables.DCTEOBToken)
	if zeroThenOneRate != wantZeroThenOne {
		t.Fatalf("zero-then-one rate = %d, want %d", zeroThenOneRate, wantZeroThenOne)
	}
}

func TestBPredAnalysisKeyFrameUsesNeighborContexts(t *testing.T) {
	var modes [16]vp8common.BPredictionMode
	modes[1] = vp8common.BTMPred
	modes[4] = vp8common.BHDPred

	aboveB := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred}
	aboveB.BModes[12] = vp8common.BHUPred
	aboveB.BModes[13] = vp8common.BRDPred
	leftB := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred}
	leftB.BModes[3] = vp8common.BVLPred
	leftB.BModes[7] = vp8common.BLDPred

	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 0); got != vp8common.BHUPred {
		t.Fatalf("above edge B_PRED context = %v, want B_HU_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 1); got != vp8common.BRDPred {
		t.Fatalf("above edge block 1 context = %v, want B_RD_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 0); got != vp8common.BVLPred {
		t.Fatalf("left edge B_PRED context = %v, want B_VL_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 4); got != vp8common.BLDPred {
		t.Fatalf("left edge block 4 context = %v, want B_LD_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveB, modes, 5); got != vp8common.BTMPred {
		t.Fatalf("internal above context = %v, want B_TM_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftB, modes, 5); got != vp8common.BHDPred {
		t.Fatalf("internal left context = %v, want B_HD_PRED", got)
	}
}

func TestBPredAnalysisKeyFrameMapsWholeBlockNeighborContexts(t *testing.T) {
	var modes [16]vp8common.BPredictionMode
	aboveV := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.VPred}
	aboveH := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.HPred}
	leftTM := vp8enc.KeyFrameMacroblockMode{YMode: vp8common.TMPred}

	if got := bPredAnalysisAboveMode(true, &aboveV, modes, 0); got != vp8common.BVEPred {
		t.Fatalf("above V_PRED context = %v, want B_VE_PRED", got)
	}
	if got := bPredAnalysisAboveMode(true, &aboveH, modes, 0); got != vp8common.BHEPred {
		t.Fatalf("above H_PRED context = %v, want B_HE_PRED", got)
	}
	if got := bPredAnalysisLeftMode(true, &leftTM, modes, 0); got != vp8common.BTMPred {
		t.Fatalf("left TM_PRED context = %v, want B_TM_PRED", got)
	}
	if got := bPredAnalysisAboveMode(false, &aboveV, modes, 0); got != vp8common.BDCPred {
		t.Fatalf("inter above context = %v, want B_DC_PRED", got)
	}
	if got := bPredAnalysisLeftMode(false, &leftTM, modes, 0); got != vp8common.BDCPred {
		t.Fatalf("inter left context = %v, want B_DC_PRED", got)
	}
}
