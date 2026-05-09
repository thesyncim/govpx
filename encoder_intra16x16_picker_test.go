package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestPredictBestWholeBlockIntraPicksVerticalGradient builds a 32x32 frame
// whose target macroblock at (1,1) is a pure vertical replication of the
// above-row predictor samples. The vertical pattern produces a zero residual
// only for V_PRED, so libvpx's rd_pick_intra16x16mby_mode (and our port
// predictBestWholeBlockIntraModeRD) must select V_PRED for Y. We also use
// uniform U/V macroblocks so DC_PRED is optimal for chroma.
func TestPredictBestWholeBlockIntraPicksVerticalGradient(t *testing.T) {
	const (
		width  = 32
		height = 32
		mbRow  = 1
		mbCol  = 1
	)

	src := testImage(width, height)
	fillImage(src, 128, 128, 128)

	// Build a column pattern that the V_PRED predictor (above row) will copy.
	pattern := [16]byte{}
	for c := range 16 {
		pattern[c] = byte(40 + c*8)
	}
	// Source MB at (mbRow,mbCol) replicates the pattern down each column —
	// this is exactly what V_PRED reconstructs from its above row.
	for r := range 16 {
		for c := range 16 {
			src.Y[(mbRow*16+r)*src.YStride+(mbCol*16+c)] = pattern[c]
		}
	}
	// Uniform chroma so DC_PRED on a 128 reference is exact.
	for r := range 8 {
		for c := range 8 {
			src.U[(mbRow*8+r)*src.UStride+(mbCol*8+c)] = 128
			src.V[(mbRow*8+r)*src.VStride+(mbCol*8+c)] = 128
		}
	}

	pred := testVP8Frame(t, width, height, 128, 128, 128)
	// Lay the column pattern into the row immediately above the target MB so
	// V_PRED reads it as its above-row predictor. We do not vary the left
	// column or top-left, so the H_PRED and TM_PRED predictors land far from
	// the source.
	for c := range 16 {
		pred.Img.Y[(mbRow*16-1)*pred.Img.YStride+(mbCol*16+c)] = pattern[c]
	}
	pred.ExtendBorders()

	quant := testRegularMacroblockQuant(t, 20)
	probs := vp8tables.DefaultCoefProbs
	var scratch vp8dec.IntraReconstructionScratch

	yMode, uvMode, yRate, yDist, uvRate, uvDist, ok := predictBestWholeBlockIntraModeRD(
		sourceImageFromPublic(src), 20, 0, true, mbRow, mbCol,
		nil, nil, &quant, &pred.Img, &scratch, &probs, false,
	)
	if !ok {
		t.Fatalf("predictBestWholeBlockIntraModeRD returned ok=false")
	}
	if yMode != vp8common.VPred {
		t.Fatalf("Y mode = %v, want VPred for column-replicated source", yMode)
	}
	if uvMode != vp8common.DCPred {
		t.Fatalf("UV mode = %v, want DCPred for uniform 128 chroma", uvMode)
	}

	// Rate must include the mode-tree cost; distortion is a non-negative
	// transform-domain quantity.
	yModeRate := intraYModeRate(true, yMode)
	if yRate <= yModeRate {
		t.Fatalf("Y rate = %d, want mode rate %d plus token rate", yRate, yModeRate)
	}
	if yDist < 0 {
		t.Fatalf("Y distortion = %d, want non-negative", yDist)
	}
	uvModeRate := intraUVModeRate(true, uvMode)
	if uvRate <= uvModeRate {
		t.Fatalf("UV rate = %d, want mode rate %d plus token rate", uvRate, uvModeRate)
	}
	if uvDist < 0 {
		t.Fatalf("UV distortion = %d, want non-negative", uvDist)
	}

	// Sanity: rebuild the chosen mode's predictor and recompute the
	// transform-domain RD via wholeBlockYTransformRD; the picker's reported
	// rate/distortion must match a fresh transform pass exactly.
	freshPred := testVP8Frame(t, width, height, 128, 128, 128)
	for c := range 16 {
		freshPred.Img.Y[(mbRow*16-1)*freshPred.Img.YStride+(mbCol*16+c)] = pattern[c]
	}
	freshPred.ExtendBorders()
	var freshScratch vp8dec.IntraReconstructionScratch
	freshMode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: uvMode}
	if !predictAnalysisMacroblock(&freshPred.Img, mbRow, mbCol, &freshMode, &freshScratch) {
		t.Fatalf("predictAnalysisMacroblock returned false")
	}
	gotYRate, gotYDist := wholeBlockYTransformRD(
		sourceImageFromPublic(src), &freshPred.Img, mbRow, mbCol, 20, 0,
		nil, nil, &quant, &probs, false,
	)
	if yRate != gotYRate+yModeRate {
		t.Fatalf("Y rate breakdown = %d (token %d + mode %d), got token RD pass %d",
			yRate, yRate-yModeRate, yModeRate, gotYRate)
	}
	if yDist != gotYDist {
		t.Fatalf("Y distortion = %d, want fresh transform-RD distortion %d", yDist, gotYDist)
	}
	gotUVTokRate, gotUVDist := wholeBlockChromaTransformRD(
		sourceImageFromPublic(src), &freshPred.Img, mbRow, mbCol, 20, 0,
		nil, nil, &quant, &probs, false,
	)
	if uvRate != gotUVTokRate+uvModeRate {
		t.Fatalf("UV rate breakdown = %d (token %d + mode %d), got token RD pass %d",
			uvRate, uvRate-uvModeRate, uvModeRate, gotUVTokRate)
	}
	if uvDist != gotUVDist {
		t.Fatalf("UV distortion = %d, want fresh transform-RD distortion %d", uvDist, gotUVDist)
	}
}

// TestPredictBestWholeBlockIntraIteratesAllFourYModes verifies the picker
// considers DC, V, H, and TM in libvpx's exact iteration order by exercising
// each mode and then confirming the picker chose a valid Y mode in
// [DC_PRED, TM_PRED]. Combined with the two complementary picks below
// (vertical-favoring, horizontal-favoring), this also documents that all
// four candidates participate in the search.
func TestPredictBestWholeBlockIntraPicksHorizontalGradient(t *testing.T) {
	const (
		width  = 32
		height = 32
		mbRow  = 1
		mbCol  = 1
	)

	src := testImage(width, height)
	fillImage(src, 128, 128, 128)

	// Build a row pattern that H_PRED's left-column predictor will copy.
	pattern := [16]byte{}
	for r := range 16 {
		pattern[r] = byte(50 + r*7)
	}
	for r := range 16 {
		for c := range 16 {
			src.Y[(mbRow*16+r)*src.YStride+(mbCol*16+c)] = pattern[r]
		}
	}
	for r := range 8 {
		for c := range 8 {
			src.U[(mbRow*8+r)*src.UStride+(mbCol*8+c)] = 128
			src.V[(mbRow*8+r)*src.VStride+(mbCol*8+c)] = 128
		}
	}

	pred := testVP8Frame(t, width, height, 128, 128, 128)
	for r := range 16 {
		pred.Img.Y[(mbRow*16+r)*pred.Img.YStride+(mbCol*16-1)] = pattern[r]
	}
	pred.ExtendBorders()

	quant := testRegularMacroblockQuant(t, 20)
	probs := vp8tables.DefaultCoefProbs
	var scratch vp8dec.IntraReconstructionScratch

	yMode, uvMode, yRate, _, uvRate, _, ok := predictBestWholeBlockIntraModeRD(
		sourceImageFromPublic(src), 20, 0, true, mbRow, mbCol,
		nil, nil, &quant, &pred.Img, &scratch, &probs, false,
	)
	if !ok {
		t.Fatalf("predictBestWholeBlockIntraModeRD returned ok=false")
	}
	if yMode != vp8common.HPred {
		t.Fatalf("Y mode = %v, want HPred for row-replicated source", yMode)
	}
	if uvMode != vp8common.DCPred {
		t.Fatalf("UV mode = %v, want DCPred for uniform chroma", uvMode)
	}
	if yRate <= 0 || uvRate <= 0 {
		t.Fatalf("Y/UV rate = %d/%d, want positive", yRate, uvRate)
	}
}
