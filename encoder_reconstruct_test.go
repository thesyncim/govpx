package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

var benchmarkInterReference interAnalysisReference
var benchmarkInterMV vp8enc.MotionVector
var benchmarkBool bool

const testInterSearchQIndex = 20

func TestSelectInterFrameReferenceMotionVectorChoosesLowestCostReference(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 16, 16, 220, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	alt := testVP8Frame(t, 16, 16, 80, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			v := byte(32 + ((row*17 + col*11) & 127))
			src.Y[row*src.YStride+col] = v
			golden.Img.Y[row*golden.Img.YStride+col] = v
			last.Img.Y[row*last.Img.YStride+col] = byte(200 - ((row*7 + col*19) & 63))
			alt.Img.Y[row*alt.Img.YStride+col] = byte(96 + ((row*5 + col*3) & 63))
		}
	}
	last.ExtendBorders()
	golden.ExtendBorders()
	alt.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0, testInterSearchQIndex)

	if ref.Frame != vp8common.GoldenFrame || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("selection = %v %+v, want golden zero MV", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorUsesLibvpxHexCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(17 + ((row*19 + col*11) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+2)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, testInterSearchQIndex)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("selection = %v %+v, want last row +16 from libvpx hex ring", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsFullPixelCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(21 + ((row*23 + col*7) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+3)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, testInterSearchQIndex)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 24}) {
		t.Fatalf("selection = %v %+v, want last row +24 after exhaustive search", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsExhaustiveCornerCandidate(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			src.Y[row*src.YStride+col] = byte(31 + ((row*29 + col*5) & 127))
		}
	}

	last := testVP8Frame(t, 64, 64, 220, 90, 170)
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			last.Img.Y[(row+4)*last.Img.YStride+col+4] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, testInterSearchQIndex)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("selection = %v %+v, want last +32,+32 exhaustive candidate", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorRefinesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 13, 90, 170)
	last := testVP8Frame(t, 48, 48, 40, 90, 170)
	for row := 0; row < last.Img.CodedHeight; row++ {
		for col := 0; col < last.Img.CodedWidth; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((19 + row*17 + col*13 + row*col*3) & 0xff)
		}
	}
	last.ExtendBorders()
	refStart := last.Img.YFull[last.Img.YOrigin+(16-2)*last.Img.YStride+16-2:]
	dsp.SixTapPredict16x16(refStart, last.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 1, 1, testInterSearchQIndex)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("selection = %v %+v, want last subpixel +2,+2", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorPrefersCheaperMotionOnTie(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	_, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, testInterSearchQIndex)

	if mv != (vp8enc.MotionVector{}) {
		t.Fatalf("mv = %+v, want zero MV for equal-SAD candidates", mv)
	}
}

func TestMacroblockSubpixelSADHonorsLimit(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(t, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	full, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	if !ok {
		t.Fatalf("macroblockSubpixelSAD returned ok=false")
	}
	limited, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	if !ok {
		t.Fatalf("limited macroblockSubpixelSAD returned ok=false")
	}
	if limited <= 1024 || limited >= full {
		t.Fatalf("limited SAD = %d, full = %d, want early result above limit and below full", limited, full)
	}
}

func TestMacroblockSubpixelVarianceMatchesBilinearPredictor(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*13 + col*19 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 6, src.Y[16*src.YStride+16:], src.YStride)

	variance, sse, ok := macroblockSubpixelVariance(sourceImageFromPublic(src), &ref.Img, 16, 16, 16, 16, 2, 6)

	if !ok {
		t.Fatalf("macroblockSubpixelVariance returned ok=false")
	}
	if variance != 0 || sse != 0 {
		t.Fatalf("subpixel variance = %d/%d, want exact bilinear match", variance, sse)
	}
}

func TestIterativeInterFrameSubpixelMotionVectorUsesBilinearVariance(t *testing.T) {
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

	mv, cost, ok := iterativeInterFrameSubpixelMotionVector(sourceImageFromPublic(src), &ref.Img, 1, 1, vp8enc.MotionVector{}, testInterSearchQIndex)

	if !ok {
		t.Fatalf("iterativeInterFrameSubpixelMotionVector returned ok=false")
	}
	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mv = %+v, want +2,+2 quarter-pel candidate", mv)
	}
	if want := interMotionSearchErrorVectorCost(mv, testInterSearchQIndex); cost != want {
		t.Fatalf("cost = %d, want zero distortion plus mv cost %d", cost, want)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((31 + row*5 + col*17 + row*col*11) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	var candidates [6]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 1, 1, testInterSearchQIndex, &candidates)

	if count != 2 {
		t.Fatalf("candidate count = %d, want full-pixel plus subpixel", count)
	}
	if candidates[0].MV != (vp8enc.MotionVector{}) {
		t.Fatalf("full-pixel candidate = %+v, want zero MV", candidates[0].MV)
	}
	if candidates[1].MV != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("subpixel candidate = %+v, want +2,+2", candidates[1].MV)
	}
}

func TestInterFrameSubpixelSearchCandidateCount(t *testing.T) {
	if got := interFrameSubpixelSearchCandidateCount(); got != 31 {
		t.Fatalf("subpixel candidate count = %d, want libvpx iterative max 31", got)
	}
}

func TestPredictBestKeyFrameIntraModeChoosesBPred(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	pred := testVP8Frame(t, 32, 32, 128, 128, 128)
	for i := 0; i < 16; i++ {
		pred.Img.Y[15*pred.Img.YStride+16+i] = byte(40 + i*7)
		pred.Img.Y[(16+i)*pred.Img.YStride+15] = byte(210 - i*5)
	}
	pred.ExtendBorders()

	var genScratch vp8dec.IntraReconstructionScratch
	refs := vp8dec.BuildIntraPredictorRefs(&pred.Img, 1, 1, &genScratch.Refs)
	yOff := 16*pred.Img.YStride + 16
	y := pred.Img.Y[yOff:]
	for block := 0; block < 16; block++ {
		var blockPred [16]byte
		if !predictAnalysisBPredBlock(vp8common.BHEPred, blockPred[:], 4, y, pred.Img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			t.Fatalf("predictAnalysisBPredBlock returned false")
		}
		copyBPredBlock(blockPred[:], 4, y, pred.Img.YStride, block)
		copyBPredBlockToSource(blockPred[:], 4, src, 1, 1, block)
	}
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			pred.Img.Y[row*pred.Img.YStride+col] = 128
		}
	}

	var scratch vp8dec.IntraReconstructionScratch
	quant := testMacroblockQuant(20)
	mode, ok := predictBestKeyFrameIntraMode(sourceImageFromPublic(src), 20, 1, 1, nil, nil, &quant, &pred.Img, &scratch)
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

func TestPredictBestBPredLumaModeCostReconstructsChosenBlocks(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			src.Y[row*src.YStride+col] = 200
		}
	}
	pred := testVP8Frame(t, 16, 16, 128, 128, 128)
	quant := testMacroblockQuant(4)
	var scratch vp8dec.IntraReconstructionScratch

	_, cost, ok := predictBestBPredLumaModeCost(sourceImageFromPublic(src), 4, true, 0, 0, nil, nil, &quant, &pred.Img, &scratch)

	if !ok {
		t.Fatalf("predictBestBPredLumaModeCost returned ok=false")
	}
	if cost <= 0 {
		t.Fatalf("cost = %d, want positive RD cost", cost)
	}
	if pred.Img.Y[0] <= 128 {
		t.Fatalf("reconstructed block sample = %d, want above raw predictor 128", pred.Img.Y[0])
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
	for block := 0; block < 16; block++ {
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

func TestShouldSkipInterResidualUsesRDTokenCost(t *testing.T) {
	if !shouldSkipInterResidual(40, 512, 100, 100) {
		t.Fatalf("skip decision = false, want skip when residual has no distortion gain")
	}
	if !shouldSkipInterResidual(40, 512, 100, 110) {
		t.Fatalf("skip decision = false, want skip when residual worsens distortion")
	}
	if shouldSkipInterResidual(40, 512, 1000, 0) {
		t.Fatalf("skip decision = true, want coded residual when distortion gain dominates token cost")
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

func TestLibvpxSADPerBit16MatchesInitializeMEConsts(t *testing.T) {
	tests := []struct {
		qIndex int
		want   int
	}{
		{qIndex: 0, want: 2},
		{qIndex: 16, want: 3},
		{qIndex: 30, want: 4},
		{qIndex: 42, want: 5},
		{qIndex: 54, want: 6},
		{qIndex: 66, want: 7},
		{qIndex: 78, want: 8},
		{qIndex: 90, want: 9},
		{qIndex: 102, want: 10},
		{qIndex: 114, want: 11},
		{qIndex: 126, want: 14},
	}
	for _, tt := range tests {
		if got := libvpxSADPerBit16(tt.qIndex); got != tt.want {
			t.Fatalf("q=%d sad_per_bit16 = %d, want %d", tt.qIndex, got, tt.want)
		}
	}
}

func TestInterMotionModeVectorCostOnlyChargesNewMVDelta(t *testing.T) {
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	newMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	delta := vp8enc.MotionVector{Col: 8}

	if got, want := interMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1), interMotionVectorCost(delta); got != want {
		t.Fatalf("NEWMV vector cost = %d, want delta cost %d", got, want)
	}

	nearest := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NearestMV, MV: above.MV}
	if got := interMotionModeVectorCost(&nearest, &above, nil, nil, 0, 0, 1, 1); got != 0 {
		t.Fatalf("NEARESTMV vector cost = %d, want 0", got)
	}
}

func TestRdBlockScoreAppliesLibvpxPlaneAndIntraMultipliers(t *testing.T) {
	if got := rdBlockScore(40, 4, false, 100, 20); got != 79 {
		t.Fatalf("inter block rd = %d, want 79", got)
	}
	if got := rdBlockScore(40, 4, true, 100, 20); got != 53 {
		t.Fatalf("intra block rd = %d, want 53", got)
	}
}

func TestStaticInterEncodeBreakoutUsesStrictLibvpxThreshold(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testMacroblockQuant(20)

	src.Y[0] = 133
	if !staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want skip below AC threshold")
	}

	src.Y[0] = 134
	if staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want no skip at strict AC threshold")
	}
}

func TestStaticInterEncodeBreakoutUsesChromaGate(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 129, 90, 170)
	quant := testMacroblockQuant(80)

	if !staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want uniform low-luma residual skipped")
	}

	src.U[0] = 110
	if staticInterEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want chroma SSE to prevent skip")
	}
}

func TestBuildReconstructingInterFrameCoefficientsUsesStaticEncodeBreakout(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	src.Y[0] = 208

	noBreakout := newSizedTestEncoder(t, 16, 16)
	fillBenchmarkVP8Image(&noBreakout.lastRef.Img, 128, 90, 170)
	noBreakout.lastRef.ExtendBorders()
	noBreakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	noBreakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := noBreakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, noBreakoutModes, noBreakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("no-breakout inter reconstruction returned error: %v", err)
	}
	if noBreakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&noBreakoutCoeffs[0]) == 0 {
		t.Fatalf("no-breakout mode skip=%t coeff sum=%d, want coded residual", noBreakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&noBreakoutCoeffs[0]))
	}

	breakout := newSizedTestEncoder(t, 16, 16)
	breakout.opts.StaticThreshold = 7000
	fillBenchmarkVP8Image(&breakout.lastRef.Img, 128, 90, 170)
	breakout.lastRef.ExtendBorders()
	breakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	breakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := breakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, breakoutModes, breakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("breakout inter reconstruction returned error: %v", err)
	}
	if !breakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&breakoutCoeffs[0]) != 0 {
		t.Fatalf("breakout mode skip=%t coeff sum=%d, want forced skip", breakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&breakoutCoeffs[0]))
	}
}

func TestMacroblockCoefficientTokenRateChargesNonZeroResiduals(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero vp8enc.MacroblockCoefficients
	zeroRate := macroblockCoefficientTokenRate(&probs, false, &zero)

	nonzero := zero
	nonzero.QCoeff[24][0] = 2
	nonzero.SetBlockEOB(24, 1)
	nonzero.QCoeff[0][1] = -1
	nonzero.SetBlockEOB(0, 2)
	nonzero.QCoeff[16][0] = 1
	nonzero.SetBlockEOB(16, 1)
	nonzeroRate := macroblockCoefficientTokenRate(&probs, false, &nonzero)

	if zeroRate <= 0 {
		t.Fatalf("zero residual token rate = %d, want positive EOB signalling cost", zeroRate)
	}
	if nonzeroRate <= zeroRate {
		t.Fatalf("nonzero residual token rate = %d, zero = %d, want higher rate", nonzeroRate, zeroRate)
	}

	clearMacroblockCoefficients(&nonzero)
	if clearedRate := macroblockCoefficientTokenRate(&probs, false, &nonzero); clearedRate != zeroRate {
		t.Fatalf("cleared residual rate = %d, want zero residual rate %d", clearedRate, zeroRate)
	}
}

func TestOptimizeQuantizedBlockDropsTrailingCoefficientWhenRateWins(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(127, 0, 0, 1, false, &coeff, &quant, &qcoeff, 2)

	if eob != 1 || qcoeff[1] != 0 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockKeepsCoefficientWhenDistortionDominates(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 100
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 100
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(4, 0, 0, 1, false, &coeff, &quant, &qcoeff, 2)

	if eob != 2 || qcoeff[1] != 1 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want coefficient preserved", eob, qcoeff[1])
	}
}

func TestQuantizeBlockWithZbinUsesZeroRunBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	boostedRC := int(vp8tables.DefaultZigZag1D[7])
	coeff[boostedRC] = 75

	eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, &qcoeff, &dqcoeff)

	if eob != 0 || qcoeff[boostedRC] != 0 || dqcoeff[boostedRC] != 0 {
		t.Fatalf("boosted coefficient eob/q/dq = %d/%d/%d, want suppressed", eob, qcoeff[boostedRC], dqcoeff[boostedRC])
	}

	coeff = [16]int16{}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	earlyRC := int(vp8tables.DefaultZigZag1D[1])
	coeff[earlyRC] = 80
	eob = quantizeBlockWithZbin(&coeff, &quant, 80, 0, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[earlyRC] == 0 || dqcoeff[earlyRC] == 0 {
		t.Fatalf("early coefficient eob/q/dq = %d/%d/%d, want quantized", eob, qcoeff[earlyRC], dqcoeff[earlyRC])
	}
}

func TestQuantizeBlockWithZbinUsesModeBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 66

	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, 0, &qcoeff, &dqcoeff); eob != 2 || qcoeff[rc] == 0 {
		t.Fatalf("unboosted eob/q = %d/%d, want coefficient quantized", eob, qcoeff[rc])
	}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	if eob := quantizeBlockWithZbin(&coeff, &quant, 80, lastFrameZeroMVZbinBoost, &qcoeff, &dqcoeff); eob != 0 || qcoeff[rc] != 0 {
		t.Fatalf("boosted eob/q = %d/%d, want coefficient suppressed", eob, qcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockUpdatesDequantizedCoefficients(t *testing.T) {
	quant := testRegularBlockQuant(127, 10)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 11

	eob := quantizeOptimizedBlock(127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 1 || qcoeff[rc] != 0 || dqcoeff[rc] != 0 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want trailing coefficient dropped and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockKeepsDequantizedCoefficient(t *testing.T) {
	quant := testRegularBlockQuant(4, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 100

	eob := quantizeOptimizedBlock(4, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[rc] != 1 || dqcoeff[rc] != 100 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want coefficient kept and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func testRegularBlockQuant(qIndex int, dequantValue int16) vp8enc.BlockQuant {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = dequantValue
	}
	var quant vp8enc.BlockQuant
	vp8enc.InitRegularBlockQuant(qIndex, &dequant, &quant)
	return quant
}

func TestInterZbinModeBoostMatchesLibvpxClasses(t *testing.T) {
	tests := []struct {
		name string
		mode vp8enc.InterFrameMacroblockMode
		want int
	}{
		{name: "last zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}, want: lastFrameZeroMVZbinBoost},
		{name: "golden zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "alt zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "newmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV}, want: nonZeroInterModeZbinBoost},
		{name: "splitmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV}, want: splitInterModeZbinBoost},
		{name: "intra", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}, want: intraInterFrameZbinBoost},
	}
	for _, tt := range tests {
		if got := interZbinModeBoost(&tt.mode); got != tt.want {
			t.Fatalf("%s boost = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestEncoderSegmentQIndex(t *testing.T) {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][1] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = -10
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 10 {
		t.Fatalf("delta segment q = %d, want 10", got)
	}
	if got := encoderSegmentQIndex(4, segmentation, 1); got != vp8common.MinQ {
		t.Fatalf("clamped delta segment q = %d, want MinQ", got)
	}
	segmentation.AbsDelta = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = 63
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 63 {
		t.Fatalf("absolute segment q = %d, want 63", got)
	}
	if got := encoderSegmentQIndex(20, segmentation, 2); got != 20 {
		t.Fatalf("disabled segment q = %d, want base q", got)
	}
}

func TestBuildReconstructingKeyFrameCoefficientsWithSegmentationQuantizesPerSegment(t *testing.T) {
	lowEncoder := newSizedTestEncoder(t, 32, 16)
	highEncoder := newSizedTestEncoder(t, 32, 16)
	src := segmentedQuantizationTestImage()
	lowModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	highModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	lowCoeffs := make([]vp8enc.MacroblockCoefficients, 2)
	highCoeffs := make([]vp8enc.MacroblockCoefficients, 2)

	lowSegmentation := testAltQSegmentation(1, 0)
	highSegmentation := testAltQSegmentation(1, 100)
	if err := lowEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, lowSegmentation, true, lowModes, lowCoeffs, 1, 2); err != nil {
		t.Fatalf("low-q keyframe reconstruction returned error: %v", err)
	}
	if err := highEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, highSegmentation, true, highModes, highCoeffs, 1, 2); err != nil {
		t.Fatalf("high-q keyframe reconstruction returned error: %v", err)
	}

	if lowModes[0].SegmentID != 0 || lowModes[1].SegmentID != 1 || highModes[0].SegmentID != 0 || highModes[1].SegmentID != 1 {
		t.Fatalf("segment IDs low=%d/%d high=%d/%d, want preserved 0/1", lowModes[0].SegmentID, lowModes[1].SegmentID, highModes[0].SegmentID, highModes[1].SegmentID)
	}
	if highEncoder.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", highEncoder.reconstructModes[1].SegmentID)
	}
	if highEncoder.dequants[0].Y1[0] == highEncoder.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", highEncoder.dequants[0].Y1[0], highEncoder.dequants[1].Y1[0])
	}

	lowSum := macroblockCoeffAbsSum(&lowCoeffs[1])
	highSum := macroblockCoeffAbsSum(&highCoeffs[1])
	if lowSum <= highSum {
		t.Fatalf("segment 1 coefficient abs sum low/high = %d/%d, want high segment q to quantize harder", lowSum, highSum)
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationPreservesSegmentDequants(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	fillBenchmarkVP8Image(&e.lastRef.Img, 128, 128, 128)
	e.lastRef.ExtendBorders()
	src := segmentedQuantizationTestImage()
	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 2)
	segmentation := testAltQSegmentation(1, 100)

	if err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, segmentation, true, modes, coeffs, 1, 2, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}

	if modes[0].SegmentID != 0 || modes[1].SegmentID != 1 {
		t.Fatalf("segment IDs = %d/%d, want preserved 0/1", modes[0].SegmentID, modes[1].SegmentID)
	}
	if e.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", e.reconstructModes[1].SegmentID)
	}
	if e.dequants[0].Y1[0] == e.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", e.dequants[0].Y1[0], e.dequants[1].Y1[0])
	}
	if got := macroblockCoeffAbsSum(&coeffs[1]); got == 0 {
		t.Fatalf("segment 1 coefficient abs sum = 0, want residual coefficients")
	}
}

func TestBuildReconstructingCoefficientsWithSegmentationRejectsInvalidSegmentID(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	src := segmentedQuantizationTestImage()
	segmentation := testAltQSegmentation(1, 63)
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	keyCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, keyModes, keyCoeffs, 1, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("keyframe invalid segment error = %v, want ErrInvalidConfig", err)
	}

	interModes := []vp8enc.InterFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	interCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, interModes, interCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("inter invalid segment error = %v, want ErrInvalidConfig", err)
	}
}

func copyBPredBlockToSource(block []byte, blockStride int, dst Image, mbRow int, mbCol int, blockIndex int) {
	baseY := mbRow*16 + (blockIndex>>2)*4
	baseX := mbCol*16 + (blockIndex&3)*4
	for row := 0; row < 4; row++ {
		copy(dst.Y[(baseY+row)*dst.YStride+baseX:], block[row*blockStride:row*blockStride+4])
	}
}

func testAltQSegmentation(segmentID uint8, qIndex int8) vp8enc.SegmentationConfig {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true, AbsDelta: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID] = qIndex
	return segmentation
}

func segmentedQuantizationTestImage() Image {
	img := testImage(32, 16)
	fillImage(img, 128, 128, 128)
	for row := 0; row < img.Height; row++ {
		for col := 16; col < img.Width; col++ {
			if (row+col)&1 == 0 {
				img.Y[row*img.YStride+col] = 16
			} else {
				img.Y[row*img.YStride+col] = 240
			}
		}
	}
	return img
}

func macroblockCoeffAbsSum(coeffs *vp8enc.MacroblockCoefficients) int {
	sum := 0
	for block := range coeffs.QCoeff {
		for _, coeff := range coeffs.QCoeff[block] {
			if coeff < 0 {
				sum -= int(coeff)
			} else {
				sum += int(coeff)
			}
		}
	}
	return sum
}

func BenchmarkMacroblockCoefficientsEmpty(b *testing.B) {
	var coeffs vp8enc.MacroblockCoefficients
	for block := 0; block < 16; block++ {
		coeffs.SetBlockEOB(block, 0)
	}
	coeffs.SetBlockEOB(24, 0)
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkBool = macroblockCoefficientsEmpty(&coeffs, false)
	}
}

func BenchmarkSelectInterFrameReferenceMotionVector(b *testing.B) {
	src := testImage(64, 64)
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte(32 + ((row + col) & 127))
		}
	}
	for i := range src.U {
		src.U[i] = 90
		src.V[i] = 170
	}
	last := testVP8Frame(b, 64, 64, 32, 90, 170)
	golden := testVP8Frame(b, 64, 64, 40, 90, 170)
	alt := testVP8Frame(b, 64, 64, 48, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)
	b.ReportAllocs()
	b.SetBytes(16 * 16 * int64(len(refs)) * int64(interFrameSubpixelSearchCandidateCount()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, testInterSearchQIndex)
	}
}

func BenchmarkSelectInterFrameReferenceMotionVectorZeroCost(b *testing.B) {
	src := testImage(64, 64)
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte(32 + ((row + col) & 127))
		}
	}
	for i := range src.U {
		src.U[i] = 90
		src.V[i] = 170
	}
	last := testVP8Frame(b, 64, 64, 0, 0, 0)
	copyPlane(last.Img.Y, last.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	copyPlane(last.Img.U, last.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	copyPlane(last.Img.V, last.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	last.ExtendBorders()
	golden := testVP8Frame(b, 64, 64, 40, 90, 170)
	alt := testVP8Frame(b, 64, 64, 48, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := (i >> 2) & 3
		col := i & 3
		benchmarkInterReference, benchmarkInterMV = selectInterFrameReferenceMotionVector(source, refs[:], len(refs), row, col, testInterSearchQIndex)
	}
}

func BenchmarkMacroblockSubpixelSADLimit(b *testing.B) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(b, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_, _ = macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	}
}

func BenchmarkMacroblockSubpixelSADFull(b *testing.B) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(b, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_, _ = macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	}
}

func sourceImageFromPublic(img Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:   img.Width,
		Height:  img.Height,
		Y:       img.Y,
		U:       img.U,
		V:       img.V,
		YStride: img.YStride,
		UStride: img.UStride,
		VStride: img.VStride,
	}
}

func testMacroblockQuant(qIndex int) vp8enc.MacroblockQuant {
	var tables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	var quant vp8enc.MacroblockQuant
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &tables)
	vp8common.InitMacroblockDequant(&tables, qIndex, &dequant)
	vp8enc.InitFastMacroblockQuant(&dequant, &quant)
	return quant
}

func testVP8Frame(tb testing.TB, width int, height int, y byte, u byte, v byte) vp8common.FrameBuffer {
	tb.Helper()
	var frame vp8common.FrameBuffer
	if err := frame.Resize(width, height, 32, 32); err != nil {
		tb.Fatalf("Resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&frame.Img, y, u, v)
	frame.ExtendBorders()
	return frame
}

func fillBenchmarkVP8Image(img *vp8common.Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}
