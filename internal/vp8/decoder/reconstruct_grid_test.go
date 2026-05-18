package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

func TestReconstructIntraMacroblockDispatchesModes(t *testing.T) {
	dequant := testMacroblockDequant()
	var scratch MacroblockResidual

	wholeMode := MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred}
	wholeTokens := wholeBlockResidualTokens()
	wholeRefs := testIntraPredictorRefs(100, 90, 70)
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	if ok := ReconstructIntraMacroblock(&wholeMode, &wholeTokens, &dequant, wholeRefs, y, 16, u, 8, v, 8, &scratch); !ok {
		t.Fatalf("whole-block dispatch returned false")
	}
	if got := y[0]; got != 101 {
		t.Fatalf("whole-block dispatch Y[0] = %d, want 101", got)
	}

	bpredMode := bpredMacroblockMode(false)
	bpredTokens := bpredResidualTokens()
	bpredRefs := tmIntraPredictorRefs()
	y = filledPlane(16, 16, 0)
	u = filledPlane(8, 8, 0)
	v = filledPlane(8, 8, 0)
	if ok := ReconstructIntraMacroblock(&bpredMode, &bpredTokens, &dequant, bpredRefs, y, 16, u, 8, v, 8, &scratch); !ok {
		t.Fatalf("BPred dispatch returned false")
	}
	if got := y[4]; got != 94 {
		t.Fatalf("BPred dispatch Y[4] = %d, want 94", got)
	}
}

func TestReconstructKeyFrameIntraGridUsesReconstructedLeft(t *testing.T) {
	img := blankImage(32, 16)
	modes := []MacroblockMode{
		{Mode: common.DCPred, UVMode: common.DCPred},
		{Mode: common.DCPred, UVMode: common.DCPred},
	}
	tokens := make([]MacroblockTokens, 2)
	tokens[0] = wholeBlockResidualTokens()
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructKeyFrameIntraGrid(&img, 1, 2, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructKeyFrameIntraGrid returned error: %v", err)
	}

	for row := range 16 {
		for col := range 32 {
			if got := img.Y[row*img.YStride+col]; got != 129 {
				t.Fatalf("Y[%d,%d] = %d, want 129", row, col, got)
			}
		}
	}
}

func TestReconstructKeyFrameIntraGridSelectsSegmentDequant(t *testing.T) {
	img := blankImage(16, 16)
	modes := []MacroblockMode{{Mode: common.DCPred, UVMode: common.DCPred, SegmentID: 1}}
	tokens := []MacroblockTokens{wholeBlockResidualTokens()}
	dequants := testMacroblockDequants()
	dequants[1].Y2[0] = 8
	var scratch IntraReconstructionScratch

	if err := ReconstructKeyFrameIntraGrid(&img, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructKeyFrameIntraGrid returned error: %v", err)
	}

	assertPlaneValue(t, "Y", img.Y, 130)
}

func TestReconstructKeyFrameIntraGridRejectsSmallBuffers(t *testing.T) {
	img := blankImage(16, 16)
	modes := []MacroblockMode{{Mode: common.DCPred, UVMode: common.DCPred}}
	tokens := []MacroblockTokens{wholeBlockResidualTokens()}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructKeyFrameIntraGrid(&img, 1, 2, modes, tokens, &dequants, &scratch)
	if err != ErrReconstructGridBufferTooSmall {
		t.Fatalf("error = %v, want ErrReconstructGridBufferTooSmall", err)
	}
}

func TestReconstructKeyFrameIntraGridUsesCodedFrameDimensions(t *testing.T) {
	fb, err := common.NewFrameBuffer(5, 3, 2, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	modes := []MacroblockMode{{Mode: common.DCPred, UVMode: common.DCPred}}
	tokens := []MacroblockTokens{wholeBlockResidualTokens()}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructKeyFrameIntraGrid(&fb.Img, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructKeyFrameIntraGrid returned error: %v", err)
	}
	if got := fb.Img.Y[15*fb.Img.YStride+15]; got != 129 {
		t.Fatalf("coded Y edge = %d, want 129", got)
	}
}

func TestReconstructKeyFrameIntraGridRejectsUnsupportedMode(t *testing.T) {
	img := blankImage(16, 16)
	modes := []MacroblockMode{{Mode: common.NearestMV, UVMode: common.DCPred}}
	tokens := []MacroblockTokens{wholeBlockResidualTokens()}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructKeyFrameIntraGrid(&img, 1, 1, modes, tokens, &dequants, &scratch)
	if err != ErrUnsupportedIntraReconstructionMode {
		t.Fatalf("error = %v, want ErrUnsupportedIntraReconstructionMode", err)
	}
}

func TestReconstructKeyFrameIntraGridAllocatesZero(t *testing.T) {
	img := blankImage(16, 16)
	modes := []MacroblockMode{{Mode: common.DCPred, UVMode: common.DCPred}}
	tokens := []MacroblockTokens{wholeBlockResidualTokens()}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	allocs := testing.AllocsPerRun(1000, func() {
		_ = ReconstructKeyFrameIntraGrid(&img, 1, 1, modes, tokens, &dequants, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestReconstructInterFrameGridCopiesLastZeroMV(t *testing.T) {
	img := blankImage(16, 16)
	last := testImage(16, 16)
	golden := blankImage(16, 16)
	alt := blankImage(16, 16)
	modes := []MacroblockMode{{Mode: common.ZeroMV, RefFrame: common.LastFrame}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &last, &golden, &alt, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}
	for row := range 16 {
		for col := range 16 {
			if got, want := img.Y[row*img.YStride+col], last.Y[row*last.YStride+col]; got != want {
				t.Fatalf("Y[%d,%d] = %d, want last %d", row, col, got, want)
			}
		}
	}
	for row := range 8 {
		for col := range 8 {
			if got, want := img.U[row*img.UStride+col], last.U[row*last.UStride+col]; got != want {
				t.Fatalf("U[%d,%d] = %d, want last %d", row, col, got, want)
			}
			if got, want := img.V[row*img.VStride+col], last.V[row*last.VStride+col]; got != want {
				t.Fatalf("V[%d,%d] = %d, want last %d", row, col, got, want)
			}
		}
	}
}

func TestReconstructInterFrameGridCopiesFullPixelWholeMV(t *testing.T) {
	img := blankImage(16, 16)
	last := testImage(48, 48)
	golden := blankImage(16, 16)
	alt := blankImage(16, 16)
	modes := []MacroblockMode{{
		Mode:        common.NewMV,
		RefFrame:    common.LastFrame,
		MV:          MotionVector{Row: 16, Col: 16},
		MBSkipCoeff: true,
	}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &last, &golden, &alt, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	assertCopiedBlock(t, "Y", img.Y, img.YStride, last.Y, last.YStride, 2, 2, 16, 16)
	assertCopiedBlock(t, "U", img.U, img.UStride, last.U, last.UStride, 1, 1, 8, 8)
	assertCopiedBlock(t, "V", img.V, img.VStride, last.V, last.VStride, 1, 1, 8, 8)
}

func TestReconstructInterFrameGridPredictsSubpixelWholeMV(t *testing.T) {
	img := blankImage(32, 32)
	last := testImage(48, 48)
	ref := blankImage(32, 32)
	modes := []MacroblockMode{
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 2}, MBSkipCoeff: true},
	}
	tokens := make([]MacroblockTokens, 4)
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &last, &ref, &ref, 2, 2, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	wantY := make([]byte, 16*16)
	wantU := make([]byte, 8*8)
	wantV := make([]byte, 8*8)
	dsp.SixTapPredict16x16(last.Y[14*last.YStride+14:], last.YStride, 2, 2, wantY, 16)
	dsp.SixTapPredict8x8(last.U[6*last.UStride+6:], last.UStride, 1, 1, wantU, 8)
	dsp.SixTapPredict8x8(last.V[6*last.VStride+6:], last.VStride, 1, 1, wantV, 8)

	assertCopiedBlock(t, "Y", img.Y[16*img.YStride+16:], img.YStride, wantY, 16, 0, 0, 16, 16)
	assertCopiedBlock(t, "U", img.U[8*img.UStride+8:], img.UStride, wantU, 8, 0, 0, 8, 8)
	assertCopiedBlock(t, "V", img.V[8*img.VStride+8:], img.VStride, wantV, 8, 0, 0, 8, 8)
}

func TestReconstructInterFrameGridUsesBilinearInterFilter(t *testing.T) {
	img := blankImage(32, 32)
	last := testImage(48, 48)
	ref := blankImage(32, 32)
	modes := []MacroblockMode{
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 2}, MBSkipCoeff: true},
	}
	tokens := make([]MacroblockTokens, 4)
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructInterFrameGridWithConfig(&img, &last, &ref, &ref, 2, 2, modes, tokens, &dequants, &scratch, InterPredictionConfig{UseBilinear: true})
	if err != nil {
		t.Fatalf("ReconstructInterFrameGridWithConfig returned error: %v", err)
	}

	wantY := make([]byte, 16*16)
	wantU := make([]byte, 8*8)
	wantV := make([]byte, 8*8)
	dsp.BilinearPredict16x16(last.Y[16*last.YStride+16:], last.YStride, 2, 2, wantY, 16)
	dsp.BilinearPredict8x8(last.U[8*last.UStride+8:], last.UStride, 1, 1, wantU, 8)
	dsp.BilinearPredict8x8(last.V[8*last.VStride+8:], last.VStride, 1, 1, wantV, 8)

	assertCopiedBlock(t, "Y bilinear", img.Y[16*img.YStride+16:], img.YStride, wantY, 16, 0, 0, 16, 16)
	assertCopiedBlock(t, "U bilinear", img.U[8*img.UStride+8:], img.UStride, wantU, 8, 0, 0, 8, 8)
	assertCopiedBlock(t, "V bilinear", img.V[8*img.VStride+8:], img.VStride, wantV, 8, 0, 0, 8, 8)
}

func TestReconstructInterFrameGridMasksFullPixelVersionMV(t *testing.T) {
	// libvpx v1.16.0 vp8/common/reconinter.c vp8_build_inter16x16_predictors_mb
	// applies xd->fullpixel_mask ONLY to the derived chroma MV (lines 333-334);
	// the luma MV is consumed as-is by subpixel_predict16x16. So with version=3
	// (FullPixel=true, UseBilinear=true) and luma MV (Row:2, Col:2):
	//   - Luma: bilinear-predicts at sub-pixel (2,2) from last.Y[16,16:]
	//   - Chroma: derives mv +=1|sign / 2 → (1,1), then &^7 → (0,0), so chroma
	//     becomes a clean 8x8 copy from last.U/V[8,8:].
	img := blankImage(32, 32)
	last := testImage(48, 48)
	ref := blankImage(32, 32)
	modes := []MacroblockMode{
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 2}, MBSkipCoeff: true},
	}
	tokens := make([]MacroblockTokens, 4)
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructInterFrameGridWithConfig(&img, &last, &ref, &ref, 2, 2, modes, tokens, &dequants, &scratch, InterPredictionConfig{UseBilinear: true, FullPixel: true})
	if err != nil {
		t.Fatalf("ReconstructInterFrameGridWithConfig returned error: %v", err)
	}

	// Luma: bilinear sub-pixel predict; chroma: full-pixel copy.
	wantY := make([]byte, 16*16)
	dsp.BilinearPredict16x16(last.Y[16*last.YStride+16:], last.YStride, 2, 2, wantY, 16)
	assertCopiedBlock(t, "Y full-pixel version (luma bilinear)", img.Y[16*img.YStride+16:], img.YStride, wantY, 16, 0, 0, 16, 16)
	assertCopiedBlock(t, "U full-pixel version (chroma copy)", img.U[8*img.UStride+8:], img.UStride, last.U, last.UStride, 8, 8, 8, 8)
	assertCopiedBlock(t, "V full-pixel version (chroma copy)", img.V[8*img.VStride+8:], img.VStride, last.V, last.VStride, 8, 8, 8, 8)
}

func TestReconstructInterFrameGridPredictsBorderSubpixelWholeMV(t *testing.T) {
	img := blankImage(16, 16)
	ref, err := common.NewFrameBuffer(16, 16, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	fillImage(&ref.Img, testImage(16, 16))
	ref.ExtendBorders()
	modes := []MacroblockMode{{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 2}, MBSkipCoeff: true}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &ref.Img, &ref.Img, &ref.Img, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	wantY := make([]byte, 16*16)
	wantU := make([]byte, 8*8)
	wantV := make([]byte, 8*8)
	dsp.SixTapPredict16x16(ref.Img.YFull[ref.Img.YOrigin-2*ref.Img.YStride-2:], ref.Img.YStride, 2, 2, wantY, 16)
	dsp.SixTapPredict8x8(ref.Img.UFull[ref.Img.UOrigin-2*ref.Img.UStride-2:], ref.Img.UStride, 1, 1, wantU, 8)
	dsp.SixTapPredict8x8(ref.Img.VFull[ref.Img.VOrigin-2*ref.Img.VStride-2:], ref.Img.VStride, 1, 1, wantV, 8)

	assertCopiedBlock(t, "Y border subpixel", img.Y, img.YStride, wantY, 16, 0, 0, 16, 16)
	assertCopiedBlock(t, "U border subpixel", img.U, img.UStride, wantU, 8, 0, 0, 8, 8)
	assertCopiedBlock(t, "V border subpixel", img.V, img.VStride, wantV, 8, 0, 0, 8, 8)
}

func TestReconstructInterFrameGridClampsWholeMVToBorder(t *testing.T) {
	img := blankImage(16, 16)
	ref, err := common.NewFrameBuffer(16, 16, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	fillImage(&ref.Img, testImage(16, 16))
	ref.ExtendBorders()
	modes := []MacroblockMode{{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: -300, Col: -300}, MBSkipCoeff: true}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &ref.Img, &ref.Img, &ref.Img, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	assertCopiedBlock(t, "Y clamped whole MV", img.Y, img.YStride, ref.Img.YFull[ref.Img.YOrigin-16*ref.Img.YStride-16:], ref.Img.YStride, 0, 0, 16, 16)
	assertCopiedBlock(t, "U clamped whole MV", img.U, img.UStride, ref.Img.UFull[ref.Img.UOrigin-8*ref.Img.UStride-8:], ref.Img.UStride, 0, 0, 8, 8)
	assertCopiedBlock(t, "V clamped whole MV", img.V, img.VStride, ref.Img.VFull[ref.Img.VOrigin-8*ref.Img.VStride-8:], ref.Img.VStride, 0, 0, 8, 8)
}

func TestReconstructInterFrameGridPredictsSplitMVQuadrants(t *testing.T) {
	img := blankImage(32, 32)
	last := testImage(48, 48)
	ref := blankImage(32, 32)
	split := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame, Is4x4: true, MBSkipCoeff: true}
	fillSplitQuadrant(&split, 0, MotionVector{Row: 16, Col: 16})
	fillSplitQuadrant(&split, 1, MotionVector{Row: 16})
	fillSplitQuadrant(&split, 2, MotionVector{Col: 16})
	fillSplitQuadrant(&split, 3, MotionVector{})
	modes := []MacroblockMode{
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		split,
	}
	tokens := make([]MacroblockTokens, 4)
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &last, &ref, &ref, 2, 2, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	dstY := img.Y[16*img.YStride+16:]
	assertCopiedBlock(t, "Y split top-left", dstY, img.YStride, last.Y, last.YStride, 18, 18, 4, 4)
	assertCopiedBlock(t, "Y split top-right", dstY[8:], img.YStride, last.Y, last.YStride, 18, 24, 4, 4)
	assertCopiedBlock(t, "Y split bottom-left", dstY[8*img.YStride:], img.YStride, last.Y, last.YStride, 24, 18, 4, 4)
	assertCopiedBlock(t, "Y split bottom-right", dstY[8*img.YStride+8:], img.YStride, last.Y, last.YStride, 24, 24, 4, 4)

	dstU := img.U[8*img.UStride+8:]
	dstV := img.V[8*img.VStride+8:]
	assertCopiedBlock(t, "U split top-left", dstU, img.UStride, last.U, last.UStride, 9, 9, 4, 4)
	assertCopiedBlock(t, "U split top-right", dstU[4:], img.UStride, last.U, last.UStride, 9, 12, 4, 4)
	assertCopiedBlock(t, "U split bottom-left", dstU[4*img.UStride:], img.UStride, last.U, last.UStride, 12, 9, 4, 4)
	assertCopiedBlock(t, "U split bottom-right", dstU[4*img.UStride+4:], img.UStride, last.U, last.UStride, 12, 12, 4, 4)
	assertCopiedBlock(t, "V split top-left", dstV, img.VStride, last.V, last.VStride, 9, 9, 4, 4)
	assertCopiedBlock(t, "V split top-right", dstV[4:], img.VStride, last.V, last.VStride, 9, 12, 4, 4)
	assertCopiedBlock(t, "V split bottom-left", dstV[4*img.VStride:], img.VStride, last.V, last.VStride, 12, 9, 4, 4)
	assertCopiedBlock(t, "V split bottom-right", dstV[4*img.VStride+4:], img.VStride, last.V, last.VStride, 12, 12, 4, 4)
}

func TestReconstructInterFrameGridPredictsSplitMVSubpixel(t *testing.T) {
	img := blankImage(32, 32)
	last := testImage(48, 48)
	ref := blankImage(32, 32)
	split := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame, Is4x4: true, MBSkipCoeff: true}
	for i := range split.BlockMV {
		split.BlockMV[i] = MotionVector{Row: 2, Col: 2}
	}
	modes := []MacroblockMode{
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		split,
	}
	tokens := make([]MacroblockTokens, 4)
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &last, &ref, &ref, 2, 2, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	wantY := make([]byte, 4*4)
	wantU := make([]byte, 4*4)
	wantV := make([]byte, 4*4)
	dsp.SixTapPredict4x4(last.Y[14*last.YStride+14:], last.YStride, 2, 2, wantY, 4)
	dsp.SixTapPredict4x4(last.U[6*last.UStride+6:], last.UStride, 1, 1, wantU, 4)
	dsp.SixTapPredict4x4(last.V[6*last.VStride+6:], last.VStride, 1, 1, wantV, 4)

	assertCopiedBlock(t, "Y split subpixel", img.Y[16*img.YStride+16:], img.YStride, wantY, 4, 0, 0, 4, 4)
	assertCopiedBlock(t, "U split subpixel", img.U[8*img.UStride+8:], img.UStride, wantU, 4, 0, 0, 4, 4)
	assertCopiedBlock(t, "V split subpixel", img.V[8*img.VStride+8:], img.VStride, wantV, 4, 0, 0, 4, 4)
}

func TestReconstructInterFrameGridPredictsBorderSubpixelSplitMV(t *testing.T) {
	img := blankImage(16, 16)
	ref, err := common.NewFrameBuffer(16, 16, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	fillImage(&ref.Img, testImage(16, 16))
	ref.ExtendBorders()
	split := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame, Is4x4: true, MBSkipCoeff: true}
	for i := range split.BlockMV {
		split.BlockMV[i] = MotionVector{Row: 2, Col: 2}
	}
	modes := []MacroblockMode{split}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &ref.Img, &ref.Img, &ref.Img, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	wantY := make([]byte, 4*4)
	wantU := make([]byte, 4*4)
	wantV := make([]byte, 4*4)
	dsp.SixTapPredict4x4(ref.Img.YFull[ref.Img.YOrigin-2*ref.Img.YStride-2:], ref.Img.YStride, 2, 2, wantY, 4)
	dsp.SixTapPredict4x4(ref.Img.UFull[ref.Img.UOrigin-2*ref.Img.UStride-2:], ref.Img.UStride, 1, 1, wantU, 4)
	dsp.SixTapPredict4x4(ref.Img.VFull[ref.Img.VOrigin-2*ref.Img.VStride-2:], ref.Img.VStride, 1, 1, wantV, 4)

	assertCopiedBlock(t, "Y split border subpixel", img.Y, img.YStride, wantY, 4, 0, 0, 4, 4)
	assertCopiedBlock(t, "U split border subpixel", img.U, img.UStride, wantU, 4, 0, 0, 4, 4)
	assertCopiedBlock(t, "V split border subpixel", img.V, img.VStride, wantV, 4, 0, 0, 4, 4)
}

func TestReconstructInterFrameGridClampsSplitMVToBorder(t *testing.T) {
	img := blankImage(16, 16)
	ref, err := common.NewFrameBuffer(16, 16, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	fillImage(&ref.Img, testImage(16, 16))
	ref.ExtendBorders()
	split := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame, Is4x4: true, MBSkipCoeff: true}
	for i := range split.BlockMV {
		split.BlockMV[i] = MotionVector{Row: -300, Col: -300}
	}
	modes := []MacroblockMode{split}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &ref.Img, &ref.Img, &ref.Img, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}

	assertCopiedBlock(t, "Y clamped split MV", img.Y, img.YStride, ref.Img.YFull[ref.Img.YOrigin-16*ref.Img.YStride-16:], ref.Img.YStride, 0, 0, 4, 4)
	assertCopiedBlock(t, "U clamped split MV", img.U, img.UStride, ref.Img.UFull[ref.Img.UOrigin-8*ref.Img.UStride-8:], ref.Img.UStride, 0, 0, 4, 4)
	assertCopiedBlock(t, "V clamped split MV", img.V, img.VStride, ref.Img.VFull[ref.Img.VOrigin-8*ref.Img.VStride-8:], ref.Img.VStride, 0, 0, 4, 4)
}

func TestReconstructInterFrameGridRejectsUnaddressableLumaSubpixelMV(t *testing.T) {
	img := blankImage(16, 16)
	ref := testImage(16, 16)
	modes := []MacroblockMode{{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 2}}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructInterFrameGrid(&img, &ref, &ref, &ref, 1, 1, modes, tokens, &dequants, &scratch)
	if err != ErrUnsupportedInterReconstructionMode {
		t.Fatalf("error = %v, want ErrUnsupportedInterReconstructionMode", err)
	}
}

func TestReconstructInterFrameGridRejectsNonZeroZeroMV(t *testing.T) {
	img := blankImage(16, 16)
	ref := testImage(32, 32)
	modes := []MacroblockMode{{Mode: common.ZeroMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 16}}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructInterFrameGrid(&img, &ref, &ref, &ref, 1, 1, modes, tokens, &dequants, &scratch)
	if err != ErrUnsupportedInterReconstructionMode {
		t.Fatalf("error = %v, want ErrUnsupportedInterReconstructionMode", err)
	}
}

func TestReconstructInterFrameGridRejectsUnaddressableChromaSubpixelMV(t *testing.T) {
	img := blankImage(16, 16)
	ref := testImage(32, 32)
	modes := []MacroblockMode{{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 8}}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	err := ReconstructInterFrameGrid(&img, &ref, &ref, &ref, 1, 1, modes, tokens, &dequants, &scratch)
	if err != ErrUnsupportedInterReconstructionMode {
		t.Fatalf("error = %v, want ErrUnsupportedInterReconstructionMode", err)
	}
}

func TestReconstructInterFrameGridReconstructsIntraMacroblock(t *testing.T) {
	img := blankImage(16, 16)
	ref := testImage(16, 16)
	modes := []MacroblockMode{{Mode: common.DCPred, UVMode: common.DCPred, RefFrame: common.IntraFrame}}
	tokens := []MacroblockTokens{{}}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	if err := ReconstructInterFrameGrid(&img, &ref, &ref, &ref, 1, 1, modes, tokens, &dequants, &scratch); err != nil {
		t.Fatalf("ReconstructInterFrameGrid returned error: %v", err)
	}
	if got := img.Y[0]; got != 128 {
		t.Fatalf("inter intra Y[0] = %d, want synthetic DC 128", got)
	}
	if got := img.U[0]; got != 128 {
		t.Fatalf("inter intra U[0] = %d, want synthetic DC 128", got)
	}
	if got := img.V[0]; got != 128 {
		t.Fatalf("inter intra V[0] = %d, want synthetic DC 128", got)
	}
}

func TestReconstructInterFrameGridAllocatesZero(t *testing.T) {
	img := blankImage(32, 32)
	ref := testImage(48, 48)
	modes := []MacroblockMode{
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true},
		{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 2}, MBSkipCoeff: true},
	}
	tokens := make([]MacroblockTokens, 4)
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	allocs := testing.AllocsPerRun(1000, func() {
		_ = ReconstructInterFrameGrid(&img, &ref, &ref, &ref, 2, 2, modes, tokens, &dequants, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
