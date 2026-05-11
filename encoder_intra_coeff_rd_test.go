package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
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
		copyBPredBlock(blockPred[:], y, pred.Img.YStride, block)
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
		copyBPredBlock(blockPred[:], y, e.analysis.Img.YStride, block)
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
	tokenRate, wantDist := wholeBlockChromaTransformRD(sourceImageFromPublic(src), &chosenPred.Img, 0, 0, 20, 0, nil, nil, &quant, &probs, false)
	wantRate := intraUVModeRate(true, mode) + tokenRate
	if rate != wantRate || dist != wantDist {
		t.Fatalf("UV RD = rate:%d dist:%d, want transform/token rate:%d dist:%d", rate, dist, wantRate, wantDist)
	}
	if sse := macroblockChromaSSE(sourceImageFromPublic(src), &chosenPred.Img, 0, 0); dist == sse {
		t.Fatalf("UV distortion = %d, want transform-domain error rather than chroma SSE", dist)
	}
}

func TestCoefficientBlockTokenRateUsesEntropyCosts(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero [16]int16

	zeroRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &zero, 0)
	wantZero := treeTokenCost(vp8tables.CoefTree[:], probs[3][0][0][:], vp8tables.DCTEOBToken)
	if zeroRate != wantZero {
		t.Fatalf("zero token rate = %d, want %d", zeroRate, wantZero)
	}

	positive := [16]int16{0: 1}
	positiveRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &positive, 1)
	negative := [16]int16{0: -1}
	negativeRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &negative, 1)
	if positiveRate <= zeroRate {
		t.Fatalf("positive token rate = %d, zero = %d, want nonzero token to cost more", positiveRate, zeroRate)
	}
	if negativeRate <= zeroRate {
		t.Fatalf("negative token rate = %d, zero = %d, want nonzero token to cost more", negativeRate, zeroRate)
	}

	zeroThenOne := [16]int16{1: 1}
	zeroThenOneRate := coefficientBlockTokenRate(&probs, 3, 0, 0, &zeroThenOne, 2)
	p0 := probs[3][0][0]
	p1 := probs[3][vp8tables.CoefBandsTable[1]][0]
	p2 := probs[3][vp8tables.CoefBandsTable[2]][vp8tables.PrevTokenClass[vp8tables.OneToken]]
	wantZeroThenOne := boolBitCost(p0[0], 1) +
		boolBitCost(p0[1], 0) +
		nonZeroCoeffTokenRate(p1, vp8tables.OneToken) +
		boolBitCost(128, 0) +
		treeTokenCost(vp8tables.CoefTree[:], p2[:], vp8tables.DCTEOBToken)
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

func TestMacroblockCoefficientsEmptyTreatsSkippedDCLumaAsEmpty(t *testing.T) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := range 16 {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	if !macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = false, want true for skipped-DC luma blocks")
	}

	coeffs.SetBlockEOB(0, 2)
	if macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("empty = true, want false for luma AC EOB")
	}

	coeffs.SetBlockEOB(0, 1)
	if !macroblockCoefficientsEmpty(&coeffs, false) {
		t.Fatalf("whole-block empty = false, want true for luma DC carried by empty Y2")
	}
	if macroblockCoefficientsEmpty(&coeffs, true) {
		t.Fatalf("4x4 empty = true, want false for luma DC coefficient")
	}
}

func TestLibvpxRDConstantsMatchSinglePassInitializeRDConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		rdMult int
		rdDiv  int
		errBit int
	}{
		{qIndex: 0, rdMult: 44, rdDiv: 100, errBit: 1},
		{qIndex: 4, rdMult: 179, rdDiv: 100, errBit: 1},
		{qIndex: 40, rdMult: 38, rdDiv: 1, errBit: 34},
		{qIndex: 127, rdMult: 690, rdDiv: 1, errBit: 627},
	}
	for _, tt := range tests {
		rdMult, rdDiv := libvpxRDConstants(tt.qIndex)
		if rdMult != tt.rdMult || rdDiv != tt.rdDiv {
			t.Fatalf("q=%d rd = %d/%d, want %d/%d", tt.qIndex, rdMult, rdDiv, tt.rdMult, tt.rdDiv)
		}
		if got := libvpxErrorPerBit(tt.qIndex); got != tt.errBit {
			t.Fatalf("q=%d errorperbit = %d, want %d", tt.qIndex, got, tt.errBit)
		}
	}

	if got := rdModeScore(4, 512, 10); got != 1358 {
		t.Fatalf("rdModeScore low q = %d, want libvpx RDCOST 1358", got)
	}
	if got := rdModeScore(40, 512, 100); got != 176 {
		t.Fatalf("rdModeScore mid q = %d, want libvpx RDCOST 176", got)
	}
}

func TestLibvpxRDConstantsUseZbinOverQuant(t *testing.T) {
	baseMult, baseDiv := libvpxRDConstants(127)
	overMult, overDiv := libvpxRDConstantsWithZbin(127, 128)
	if overMult != 989 || overDiv != 1 {
		t.Fatalf("q127 zbin-over-quant rd = %d/%d, want 989/1", overMult, overDiv)
	}
	if overMult <= baseMult || overDiv != baseDiv {
		t.Fatalf("zbin-over-quant rd = %d/%d, base %d/%d, want larger multiplier with same divider", overMult, overDiv, baseMult, baseDiv)
	}
	if got := rdModeScoreWithZbin(127, 128, 512, 100); got != 2078 {
		t.Fatalf("zbin-over-quant rdModeScore = %d, want libvpx RDCOST 2078", got)
	}
	if got := libvpxErrorPerBitWithZbin(127, 128); got != 899 {
		t.Fatalf("zbin-over-quant errorperbit = %d, want 899", got)
	}
}

func TestLibvpxSADPerBitLUTsMatchInitializeMEConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		want16 int
		want4  int
	}{
		{qIndex: 0, want16: 2, want4: 2},
		{qIndex: 6, want16: 2, want4: 3},
		{qIndex: 20, want16: 3, want4: 4},
		{qIndex: 30, want16: 4, want4: 5},
		{qIndex: 42, want16: 5, want4: 6},
		{qIndex: 54, want16: 6, want4: 7},
		{qIndex: 62, want16: 6, want4: 8},
		{qIndex: 78, want16: 8, want4: 10},
		{qIndex: 90, want16: 9, want4: 12},
		{qIndex: 102, want16: 10, want4: 13},
		{qIndex: 114, want16: 11, want4: 16},
		{qIndex: 126, want16: 14, want4: 20},
	}
	for _, tt := range tests {
		if got := libvpxSADPerBit16(tt.qIndex); got != tt.want16 {
			t.Fatalf("q=%d sad_per_bit16 = %d, want %d", tt.qIndex, got, tt.want16)
		}
		if got := libvpxSADPerBit4(tt.qIndex); got != tt.want4 {
			t.Fatalf("q=%d sad_per_bit4 = %d, want %d", tt.qIndex, got, tt.want4)
		}
	}
}

func TestLibvpxFullPelMVSADCost16MatchesMotionVectorSADCost(t *testing.T) {
	tests := []struct {
		mv  vp8enc.MotionVector
		ref vp8enc.MotionVector
		q   int
	}{
		{mv: vp8enc.MotionVector{}, ref: vp8enc.MotionVector{}, q: 0},
		{mv: vp8enc.MotionVector{Row: 8, Col: -64}, ref: vp8enc.MotionVector{}, q: 30},
		{mv: vp8enc.MotionVector{Row: -96, Col: 72}, ref: vp8enc.MotionVector{Row: 16, Col: -8}, q: 56},
		{mv: vp8enc.MotionVector{Row: 4096, Col: -4096}, ref: vp8enc.MotionVector{}, q: 126},
	}
	for _, tt := range tests {
		got := libvpxFullPelMVSADCost16FromDeltas(int(tt.mv.Row)>>3, int(tt.mv.Col)>>3, int(tt.ref.Row)>>3, int(tt.ref.Col)>>3, tt.q)
		want := vp8enc.MotionVectorSADCost(tt.mv, tt.ref, libvpxSADPerBit16(tt.q))
		if got != want {
			t.Fatalf("mv=%+v ref=%+v q=%d full-pel SAD cost = %d, want %d", tt.mv, tt.ref, tt.q, got, want)
		}
	}
}

func TestInterMotionModeVectorCostOnlyChargesNewMVDelta(t *testing.T) {
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	newMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}

	if got, want := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext), vp8enc.MotionVectorBitCost(newMode.MV, above.MV, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight); got != want {
		t.Fatalf("NEWMV vector cost = %d, want delta cost %d", got, want)
	}

	nearest := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NearestMV, MV: above.MV}
	if got := interMotionModeVectorCost(&nearest, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext); got != 0 {
		t.Fatalf("NEARESTMV vector cost = %d, want 0", got)
	}

	liveProbs := vp8tables.DefaultMVContext
	liveProbs[1][0] = 1
	liveCost := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1, &liveProbs)
	wantLive := vp8enc.MotionVectorBitCost(newMode.MV, above.MV, &liveProbs, libvpxRDNewMVBitCostWeight)
	if liveCost != wantLive {
		t.Fatalf("live NEWMV vector cost = %d, want live-prob delta cost %d", liveCost, wantLive)
	}
	if liveCost == vp8enc.MotionVectorBitCost(newMode.MV, above.MV, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight) {
		t.Fatalf("live NEWMV vector cost = default cost %d, want MV probs to affect RD cost", liveCost)
	}
}

func TestInterMotionModeVectorCostChargesRDNewMVWithLibvpxWeight(t *testing.T) {
	mvProbs := vp8tables.DefaultMVContext
	bestRefMV := vp8enc.MotionVector{Row: 8, Col: -16}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 24, Col: 8}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: bestRefMV}

	got := interMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1, &mvProbs)
	want := vp8enc.MotionVectorBitCost(mode.MV, bestRefMV, &mvProbs, libvpxRDNewMVBitCostWeight)
	if got != want {
		t.Fatalf("RD NEWMV vector cost = %d, want MotionVectorBitCost weight-96 cost %d", got, want)
	}
	if fastWeight := vp8enc.MotionVectorBitCost(mode.MV, bestRefMV, &mvProbs, libvpxFastNewMVBitCostWeight); got == fastWeight {
		t.Fatalf("RD NEWMV vector cost = fast weight-128 cost %d, want weight 96", fastWeight)
	}
}

func TestFastInterMotionModeRateKeepsPickInterNewMVWeight(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	refRate := 17
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, defaultInterFrameSignBias())

	got := e.fastInterMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 1, refRate)
	want := boolBitCost(63, 1) +
		refRate +
		interPredictionModeRate(vp8common.NewMV, counts) +
		vp8enc.MotionVectorBitCost(mode.MV, above.MV, &vp8tables.DefaultMVContext, libvpxFastNewMVBitCostWeight)
	if got != want {
		t.Fatalf("fast NEWMV mode rate = %d, want pickinter weight-128 rate %d", got, want)
	}
	if rdRate := e.interMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 1, refRate); got == rdRate {
		t.Fatalf("fast NEWMV mode rate = RD rate %d, want separate pickinter weight", rdRate)
	}
}

func TestEncoderInterMotionModeRateUsesAltRefSignBias(t *testing.T) {
	e := &VP8Encoder{
		refProbIntra:       63,
		refProbLast:        128,
		refProbGolden:      128,
		sourceAltRefActive: true,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: -24}}
	refRate := 23

	signBias := e.interFrameSignBias()
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, signBias)
	vectorCost := interMotionModeVectorCostWithNewMVWeightAndSignBias(&mode, &above, nil, nil, 0, 0, 1, 2, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight, signBias)
	wantVectorCost := vp8enc.MotionVectorBitCost(mode.MV, vp8enc.MotionVector{Col: -16}, &vp8tables.DefaultMVContext, libvpxRDNewMVBitCostWeight)
	if vectorCost != wantVectorCost {
		t.Fatalf("sign-biased NEWMV vector cost = %d, want cost against inverted best ref MV %d", vectorCost, wantVectorCost)
	}

	want := boolBitCost(63, 1) +
		refRate +
		interPredictionModeRate(vp8common.NewMV, counts) +
		vectorCost
	if got := e.interMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 2, refRate); got != want {
		t.Fatalf("sign-biased inter mode rate = %d, want %d", got, want)
	}
}

func TestEncoderInterReferenceMotionPredictorsUseAltRefSignBias(t *testing.T) {
	e := &VP8Encoder{sourceAltRefActive: true}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}

	nearest, near := e.interAnalysisReferenceMotionPredictors(vp8common.AltRefFrame, &above, nil, nil, 0, 0, 1, 2)
	if nearest != (vp8enc.MotionVector{Col: -16}) || !near.IsZero() {
		t.Fatalf("ALTREF predictors = %+v/%+v, want inverted nearest col -16 and zero near", nearest, near)
	}
}

func TestInterPredictionModeRateMirrorsWriterBranches(t *testing.T) {
	counts := vp8enc.InterModeCounts{Intra: 3, Nearest: 4, Near: 2, Split: 1}
	probs := vp8tables.InterModeContexts
	tests := []struct {
		name string
		mode vp8common.MBPredictionMode
		want int
	}{
		{name: "zero", mode: vp8common.ZeroMV, want: boolBitCost(probs[3][0], 0)},
		{name: "nearest", mode: vp8common.NearestMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 0)},
		{name: "near", mode: vp8common.NearMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 0)},
		{name: "new", mode: vp8common.NewMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 1) + boolBitCost(probs[1][3], 0)},
		{name: "split", mode: vp8common.SplitMV, want: boolBitCost(probs[3][0], 1) + boolBitCost(probs[4][1], 1) + boolBitCost(probs[2][2], 1) + boolBitCost(probs[1][3], 1)},
	}
	for _, tt := range tests {
		if got := interPredictionModeRate(tt.mode, counts); got != tt.want {
			t.Fatalf("%s mode rate = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestInterMotionModeRateChargesReferenceModeAndVector(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, probSkipFalse: 200}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, defaultInterFrameSignBias())
	want := boolBitCost(63, 1) +
		e.interReferenceFrameRate(vp8common.GoldenFrame) +
		interPredictionModeRate(vp8common.NewMV, counts) +
		interMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext)

	if got := e.interMotionModeRate(&mode, &above, nil, nil, 0, 0, 1, 1); got != want {
		t.Fatalf("inter mode rate = %d, want %d", got, want)
	}
	if got := interMacroblockSkipRate(false); got != boolBitCost(128, 0) {
		t.Fatalf("coded skip rate = %d, want prob-128 false cost", got)
	}
	if got := interMacroblockSkipRate(true); got != boolBitCost(128, 1) {
		t.Fatalf("skipped rate = %d, want prob-128 true cost", got)
	}
	if got := e.interMacroblockSkipRate(false); got != boolBitCost(200, 0) {
		t.Fatalf("live coded skip rate = %d, want prob-200 false cost", got)
	}
	if got := e.interMacroblockSkipRate(true); got != boolBitCost(200, 1) {
		t.Fatalf("live skipped rate = %d, want prob-200 true cost", got)
	}
	if got, want := e.interIntraMacroblockModeRate(), boolBitCost(200, 0)+boolBitCost(63, 0); got != want {
		t.Fatalf("inter-intra mode rate = %d, want skip plus intra-reference rate %d", got, want)
	}
}

func TestEstimateFastInterModeScoreUsesLibvpxPickInterDistortion(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	ref := testVP8Frame(t, 16, 16, 50, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	qIndex := testInterSearchQIndex

	got, ok := e.estimateFastInterModeScore(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, qIndex)
	if !ok {
		t.Fatalf("estimateFastInterModeScore returned ok=false")
	}
	variance, sse := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, 0, 0, mode.MV)
	if variance != 0 || sse == 0 {
		t.Fatalf("variance/sse = %d/%d, want flat luma offset with zero variance and nonzero SSE", variance, sse)
	}
	rate := e.interMotionModeRate(&mode, nil, nil, nil, 0, 0, 1, 1)
	want := rdModeScore(qIndex, rate, variance)
	if got != want {
		t.Fatalf("fast inter score = %d, want rate plus luma variance %d", got, want)
	}
	if sseScore := rdModeScore(qIndex, rate, sse); got == sseScore {
		t.Fatalf("fast inter score used SSE %d, want libvpx variance distortion", sse)
	}
}

func TestInterModeForRDLoopEntryAllowsZeroNewMVOnFlatMatch(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	fillBenchmarkVP8Image(&e.analysis.Img, 72, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, 16, 16, 72, 90, 170)
	ref := interAnalysisReference{Frame: vp8common.LastFrame, Img: &last.Img}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}

	mode, ok := e.interModeForRDLoopEntry(sourceImageFromPublic(src), ref, 0, vp8common.NewMV, 0, 0, 1, 1, testInterSearchQIndex, nil, nil, nil, &newMVCandidates, nil)
	if !ok {
		t.Fatalf("RD NEWMV loop entry rejected zero MV on flat matching frame")
	}
	if mode.Mode != vp8common.NewMV || mode.RefFrame != vp8common.LastFrame || !mode.MV.IsZero() {
		t.Fatalf("RD NEWMV loop entry mode = %+v, want LAST/NEWMV with zero MV", mode)
	}
}

func TestSelectRDInterFrameMotionVectorAllowsSubpixelRefinementWithBestRefMVCost(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	bestRefMV := vp8enc.MotionVector{Row: 2, Col: 2}

	mv, cost := selectRDInterFrameMotionVectorWithSearchStart(sourceImageFromPublic(src), &ref.Img, 1, 1, 3, 3, bestRefMV, testInterSearchQIndex, defaultInterAnalysisSearchConfig(), interFrameSearchStart{}, &vp8tables.DefaultMVContext)

	if mv != bestRefMV {
		t.Fatalf("RD NEWMV search MV = %+v, want accepted subpel refinement %+v", mv, bestRefMV)
	}
	want := interMotionSearchErrorVectorCost(mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if cost != want {
		t.Fatalf("RD NEWMV search cost = %d, want best_ref_mv anchored subpel cost %d", cost, want)
	}
	if zeroAnchor := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost == zeroAnchor {
		t.Fatalf("RD NEWMV search cost = zero-anchor cost %d, want best_ref_mv anchor", zeroAnchor)
	}
}

func TestSelectFastInterFrameModeDecisionCanChooseInterleavedIntra(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*29 + col*53) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if !decision.useIntra || decision.intraMode.Mode != vp8common.DCPred || decision.intraMode.RefFrame != vp8common.IntraFrame {
		t.Fatalf("decision = %+v, want intra DC from libvpx interleaved mode loop", decision)
	}
}

// TestSelectFastInterFrameModeDecisionPicksLibvpxUVMode verifies that
// selectFastInterFrameModeDecision mirrors libvpx pickinter.c
// vp8_pick_inter_mode lines 1301-1303: when the winning mode is intra
// (mode <= B_PRED), pick_intra_mbuv_mode runs and sets mbmi.uv_mode to
// the predictor with lowest pred_error against the source. Earlier,
// govpx hardcoded UVMode=DC_PRED which caused 128x128 frame 1 chroma
// reconstruction divergence at the col-7 right-edge B_PRED MBs (R14-E).
// The fixture shapes neighbors so V_PRED has near-zero pred error
// against the source, which is exactly the case libvpx's
// pick_intra_mbuv_mode would resolve to V_PRED.
func TestSelectFastInterFrameModeDecisionPicksLibvpxUVMode(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(32, 32, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 128, 128)
	// Shape chroma predictor references so V_PRED matches the source
	// for the lower 8x8 chroma block: above-row[8..15]=40 makes V_PRED
	// fill the column with 40, matching src.U[8..15][8..15]=40.
	// pick_intra_mbuv_mode picks the mode with minimum SSE.
	for i := range 8 {
		e.analysis.Img.U[7*e.analysis.Img.UStride+8+i] = 40
		e.analysis.Img.V[7*e.analysis.Img.VStride+8+i] = 40
		e.analysis.Img.U[(8+i)*e.analysis.Img.UStride+7] = 220
		e.analysis.Img.V[(8+i)*e.analysis.Img.VStride+7] = 220
	}
	e.analysis.ExtendBorders()

	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	for row := 8; row < 16; row++ {
		for col := 8; col < 16; col++ {
			src.U[row*src.UStride+col] = 40
			src.V[row*src.VStride+col] = 40
		}
	}
	last := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := range 32 {
		for col := range 32 {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*29 + col*53 + 17) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 1, 1, 2, 2, testInterSearchQIndex, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if !decision.useIntra {
		t.Fatalf("decision = %+v, want intra mode to exercise fast pickinter UV policy", decision)
	}
	// V_PRED above-row=40 perfectly predicts source rows 8..15 col 8..15
	// (=40). DC_PRED, H_PRED, and TM_PRED all incur larger SSE.
	// pick_intra_mbuv_mode picks V_PRED.
	if decision.intraMode.UVMode != vp8common.VPred {
		t.Fatalf("fast intra UV mode = %v, want libvpx pickinter V_PRED (lowest SSE)", decision.intraMode.UVMode)
	}
}

func TestSelectFastInterFrameModeDecisionUsesLibvpxReferenceSlots(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 127, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 127, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte((17 + row*43 + col*71 + row*col*11) & 255)
		}
	}
	last := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[row*last.Img.YStride+col] = byte((231 - row*17 - col*31) & 255)
		}
	}
	last.ExtendBorders()
	golden := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := range 16 {
		copy(golden.Img.Y[row*golden.Img.YStride:], src.Y[row*src.YStride:row*src.YStride+16])
	}
	golden.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
	}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.ref.Frame != vp8common.GoldenFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want GOLDEN/ZEROMV from libvpx slot-2 loop entry", decision)
	}
}

func TestSelectFastInterFrameModeDecisionUsesThresholdState(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.Deadline = DeadlineRealtime
	e.opts.CpuUsed = 8
	fillBenchmarkVP8Image(&e.analysis.Img, 96, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	last := testVP8Frame(t, 16, 16, 96, 90, 170)
	for row := range 16 {
		for col := range 16 {
			y := byte((11 + row*37 + col*19 + row*col*5) & 255)
			src.Y[row*src.YStride+col] = y
			last.Img.Y[row*last.Img.YStride+col] = y
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 40, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.ref.Frame != vp8common.LastFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want LAST/ZEROMV on matching reference", decision)
	}
	if got := e.interModeTestHitCounts[libvpxThrZero1]; got != 1 {
		t.Fatalf("ZERO1 hit count = %d, want 1", got)
	}
	if !e.interRDThreshTouched[libvpxThrZero1] {
		t.Fatalf("ZERO1 threshold was not touched")
	}
	if got := e.interRDThreshMult[libvpxThrZero1]; got >= libvpxRDThreshMultStart {
		t.Fatalf("ZERO1 threshold multiplier = %d, want below start after improvement", got)
	}
}
