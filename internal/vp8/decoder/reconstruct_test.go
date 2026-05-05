package decoder

import (
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
)

func TestTransformMacroblockTokens4x4YAndUV(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[0][0] = 2
	tokens.EOB[0] = 1
	tokens.QCoeff[16][1] = -3
	tokens.EOB[16] = 2
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, true, &residual)

	if got := residual.Block(0)[0]; got != 10 {
		t.Fatalf("Y block DC = %d, want 10", got)
	}
	if got := residual.Block(16)[1]; got != -21 {
		t.Fatalf("UV block AC = %d, want -21", got)
	}
}

func TestBuildIntraPredictorRefsTopLeftDefaults(t *testing.T) {
	img := testImage(32, 32)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefs(&img, 0, 0, &scratch)

	if refs.UpAvailable || refs.LeftAvailable {
		t.Fatalf("availability = %v/%v, want false/false", refs.UpAvailable, refs.LeftAvailable)
	}
	assertSliceValue(t, "YAbove", refs.YAbove, 127)
	assertSliceValue(t, "YLeft", refs.YLeft, 129)
	assertSliceValue(t, "UAbove", refs.UAbove, 127)
	assertSliceValue(t, "ULeft", refs.ULeft, 129)
	if refs.YTopLeft != 127 || refs.UTopLeft != 127 || refs.VTopLeft != 127 {
		t.Fatalf("top-left defaults = %d/%d/%d, want 127", refs.YTopLeft, refs.UTopLeft, refs.VTopLeft)
	}
}

func TestBuildIntraPredictorRefsInteriorSamples(t *testing.T) {
	img := testImage(48, 48)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefs(&img, 1, 1, &scratch)

	if !refs.UpAvailable || !refs.LeftAvailable {
		t.Fatalf("availability = %v/%v, want true/true", refs.UpAvailable, refs.LeftAvailable)
	}
	for i := 0; i < 20; i++ {
		want := img.Y[15*img.YStride+16+i]
		if got := refs.YAbove[i]; got != want {
			t.Fatalf("YAbove[%d] = %d, want %d", i, got, want)
		}
	}
	for i := 0; i < 16; i++ {
		want := img.Y[(16+i)*img.YStride+15]
		if got := refs.YLeft[i]; got != want {
			t.Fatalf("YLeft[%d] = %d, want %d", i, got, want)
		}
	}
	if got, want := refs.YTopLeft, img.Y[15*img.YStride+15]; got != want {
		t.Fatalf("YTopLeft = %d, want %d", got, want)
	}
	for i := 0; i < 8; i++ {
		if got, want := refs.UAbove[i], img.U[7*img.UStride+8+i]; got != want {
			t.Fatalf("UAbove[%d] = %d, want %d", i, got, want)
		}
		if got, want := refs.VLeft[i], img.V[(8+i)*img.VStride+7]; got != want {
			t.Fatalf("VLeft[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestBuildIntraPredictorRefsEdgesFillSyntheticSamples(t *testing.T) {
	img := testImage(18, 18)
	var scratch IntraPredictorScratch

	refs := BuildIntraPredictorRefs(&img, 1, 1, &scratch)

	if got, want := refs.YAbove[0], img.Y[15*img.YStride+16]; got != want {
		t.Fatalf("YAbove[0] = %d, want %d", got, want)
	}
	for i := 2; i < len(refs.YAbove); i++ {
		if got := refs.YAbove[i]; got != 127 {
			t.Fatalf("YAbove[%d] = %d, want synthetic 127", i, got)
		}
	}
	if got, want := refs.YLeft[0], img.Y[16*img.YStride+15]; got != want {
		t.Fatalf("YLeft[0] = %d, want %d", got, want)
	}
	for i := 2; i < len(refs.YLeft); i++ {
		if got := refs.YLeft[i]; got != 129 {
			t.Fatalf("YLeft[%d] = %d, want synthetic 129", i, got)
		}
	}
}

func TestBuildIntraPredictorRefsAllocatesZero(t *testing.T) {
	img := testImage(32, 32)
	var scratch IntraPredictorScratch
	allocs := testing.AllocsPerRun(1000, func() {
		_ = BuildIntraPredictorRefs(&img, 1, 1, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestTransformMacroblockTokensY2DCOnly(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	for i := 0; i < 16; i++ {
		if got := residual.Block(i)[0]; got != 8 {
			t.Fatalf("Y block %d DC = %d, want 8", i, got)
		}
	}
}

func TestTransformMacroblockTokensAddsY1ACToY2DC(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	tokens.QCoeff[0][1] = 3
	tokens.EOB[0] = 2
	dequant := testMacroblockDequant()
	var residual MacroblockResidual

	TransformMacroblockTokens(&tokens, &dequant, false, &residual)

	if got := residual.Block(0)[0]; got != 8 {
		t.Fatalf("Y block 0 DC = %d, want 8 from Y2", got)
	}
	if got := residual.Block(0)[1]; got != 21 {
		t.Fatalf("Y block 0 AC = %d, want 21", got)
	}
}

func TestTransformMacroblockTokensAllocatesZero(t *testing.T) {
	var tokens MacroblockTokens
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	dequant := testMacroblockDequant()
	var residual MacroblockResidual
	allocs := testing.AllocsPerRun(1000, func() {
		TransformMacroblockTokens(&tokens, &dequant, false, &residual)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestAddMacroblockResidualYDCOnly(t *testing.T) {
	y := filledPlane(16, 16, 100)
	u := filledPlane(8, 8, 90)
	v := filledPlane(8, 8, 80)
	var tokens MacroblockTokens
	var residual MacroblockResidual
	tokens.EOB[5] = 1
	residual.Block(5)[0] = 16

	AddMacroblockResidual(&tokens, &residual, y, 16, u, 8, v, 8)

	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			want := byte(100)
			if row >= 4 && row < 8 && col >= 4 && col < 8 {
				want = 102
			}
			if got := y[row*16+col]; got != want {
				t.Fatalf("Y[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
	assertPlaneValue(t, "U", u, 90)
	assertPlaneValue(t, "V", v, 80)
}

func TestAddMacroblockResidualFullIDCTAndChroma(t *testing.T) {
	y := filledPlane(16, 16, 90)
	wantY := append([]byte(nil), y...)
	u := filledPlane(8, 8, 90)
	v := filledPlane(8, 8, 80)
	var tokens MacroblockTokens
	var residual MacroblockResidual

	tokens.EOB[9] = 2
	residual.Block(9)[0] = 32
	residual.Block(9)[1] = 8
	yOff := yBlockOffset(9, 16)
	dsp.IDCT4x4Add(residual.Block(9), wantY[yOff:], 16, wantY[yOff:], 16)

	tokens.EOB[16] = 1
	residual.Block(16)[0] = 24
	tokens.EOB[23] = 1
	residual.Block(23)[0] = -16

	AddMacroblockResidual(&tokens, &residual, y, 16, u, 8, v, 8)

	assertPlaneEqual(t, "Y", y, wantY)
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			wantU := byte(90)
			if row < 4 && col < 4 {
				wantU = 93
			}
			if got := u[row*8+col]; got != wantU {
				t.Fatalf("U[%d,%d] = %d, want %d", row, col, got, wantU)
			}
			wantV := byte(80)
			if row >= 4 && col >= 4 {
				wantV = 78
			}
			if got := v[row*8+col]; got != wantV {
				t.Fatalf("V[%d,%d] = %d, want %d", row, col, got, wantV)
			}
		}
	}
}

func TestAddMacroblockResidualAllocatesZero(t *testing.T) {
	y := filledPlane(16, 16, 90)
	u := filledPlane(8, 8, 90)
	v := filledPlane(8, 8, 80)
	var tokens MacroblockTokens
	var residual MacroblockResidual
	tokens.EOB[0] = 1
	residual.Block(0)[0] = 16
	allocs := testing.AllocsPerRun(1000, func() {
		AddMacroblockResidual(&tokens, &residual, y, 16, u, 8, v, 8)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestAddMacroblockResidualSkipsZeroEOBOffsets(t *testing.T) {
	var tokens MacroblockTokens
	var residual MacroblockResidual

	AddMacroblockResidual(&tokens, &residual, make([]byte, 1), 1, make([]byte, 1), 1, make([]byte, 1), 1)
}

func TestPredictIntraY16x16Modes(t *testing.T) {
	above := make([]byte, 16)
	left := make([]byte, 16)
	for i := 0; i < 16; i++ {
		above[i] = byte(i + 1)
		left[i] = byte(101 + i)
	}

	cases := []struct {
		name string
		mode common.MBPredictionMode
		want func(row int, col int) byte
	}{
		{name: "dc", mode: common.DCPred, want: func(row int, col int) byte { return 59 }},
		{name: "vertical", mode: common.VPred, want: func(row int, col int) byte { return above[col] }},
		{name: "horizontal", mode: common.HPred, want: func(row int, col int) byte { return left[row] }},
		{name: "tm", mode: common.TMPred, want: func(row int, col int) byte {
			return dsp.ClipPixel(int(left[row]) + int(above[col]) - 100)
		}},
	}

	for _, tc := range cases {
		dst := filledPlane(16, 16, 0)
		if ok := PredictIntraY16x16(tc.mode, dst, 16, above, left, 100, true, true); !ok {
			t.Fatalf("%s returned false", tc.name)
		}
		for row := 0; row < 16; row++ {
			for col := 0; col < 16; col++ {
				want := tc.want(row, col)
				if got := dst[row*16+col]; got != want {
					t.Fatalf("%s Y[%d,%d] = %d, want %d", tc.name, row, col, got, want)
				}
			}
		}
	}
}

func TestPredictIntraUV8x8Modes(t *testing.T) {
	above := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	left := []byte{200, 180, 160, 140, 120, 100, 80, 60}

	dst := filledPlane(8, 8, 0)
	if ok := PredictIntraUV8x8(common.DCPred, dst, 8, above, left, 77, true, false); !ok {
		t.Fatalf("dc returned false")
	}
	assertPlaneValue(t, "UV dc", dst, 45)

	dst = filledPlane(8, 8, 0)
	if ok := PredictIntraUV8x8(common.TMPred, dst, 8, above, left, 100, true, true); !ok {
		t.Fatalf("tm returned false")
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			want := dsp.ClipPixel(int(left[row]) + int(above[col]) - 100)
			if got := dst[row*8+col]; got != want {
				t.Fatalf("tm UV[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
}

func TestPredictIntraInvalidMode(t *testing.T) {
	dst := make([]byte, 16*16)
	if ok := PredictIntraY16x16(common.BPred, dst, 16, nil, nil, 0, false, false); ok {
		t.Fatalf("Y BPred returned true")
	}
	if ok := PredictIntraUV8x8(common.NearestMV, dst, 8, nil, nil, 0, false, false); ok {
		t.Fatalf("UV inter mode returned true")
	}
}

func TestPredictIntraY4x4TMPredPropagatesNeighbors(t *testing.T) {
	above := make([]byte, 20)
	left := make([]byte, 16)
	for i := range above {
		above[i] = byte(50 + i)
	}
	for i := range left {
		left[i] = byte(70 + i)
	}
	var modes [16]common.BPredictionMode
	for i := range modes {
		modes[i] = common.BTMPred
	}
	dst := filledPlane(16, 16, 0)

	if ok := PredictIntraY4x4(&modes, dst, 16, above, left, 40); !ok {
		t.Fatalf("PredictIntraY4x4 returned false")
	}

	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			want := byte(int(left[row]) + int(above[col]) - 40)
			if got := dst[row*16+col]; got != want {
				t.Fatalf("Y4x4[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
}

func TestPredictIntraY4x4InvalidMode(t *testing.T) {
	above := make([]byte, 20)
	left := make([]byte, 16)
	modes := [16]common.BPredictionMode{common.Above4x4}
	dst := filledPlane(16, 16, 99)

	if ok := PredictIntraY4x4(&modes, dst, 16, above, left, 0); ok {
		t.Fatalf("invalid 4x4 mode returned true")
	}
	assertPlaneValue(t, "Y4x4 invalid", dst, 99)
}

func TestPredictIntraAllocatesZero(t *testing.T) {
	aboveY := make([]byte, 16)
	leftY := make([]byte, 16)
	dstY := make([]byte, 16*16)
	aboveUV := make([]byte, 8)
	leftUV := make([]byte, 8)
	dstUV := make([]byte, 8*8)
	above4x4 := make([]byte, 20)
	left4x4 := make([]byte, 16)
	var modes [16]common.BPredictionMode
	for i := range modes {
		modes[i] = common.BDCPred
	}
	allocs := testing.AllocsPerRun(1000, func() {
		PredictIntraY16x16(common.TMPred, dstY, 16, aboveY, leftY, 128, true, true)
		PredictIntraUV8x8(common.DCPred, dstUV, 8, aboveUV, leftUV, 128, true, true)
		PredictIntraY4x4(&modes, dstY, 16, above4x4, left4x4, 128)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestReconstructWholeBlockIntraMacroblockPredictsAndAddsResidual(t *testing.T) {
	mode := MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred}
	tokens := wholeBlockResidualTokens()
	dequant := testMacroblockDequant()
	refs := testIntraPredictorRefs(100, 90, 70)
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	var scratch MacroblockResidual

	if ok := ReconstructWholeBlockIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch); !ok {
		t.Fatalf("ReconstructWholeBlockIntraMacroblock returned false")
	}

	assertPlaneValue(t, "Y", y, 101)
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			want := byte(90)
			if row < 4 && col < 4 {
				want = 102
			}
			if got := u[row*8+col]; got != want {
				t.Fatalf("U[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
	assertPlaneValue(t, "V", v, 70)
}

func TestReconstructWholeBlockIntraMacroblockSkipCoeff(t *testing.T) {
	mode := MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred, MBSkipCoeff: true}
	tokens := wholeBlockResidualTokens()
	dequant := testMacroblockDequant()
	refs := testIntraPredictorRefs(100, 90, 70)
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	var scratch MacroblockResidual

	if ok := ReconstructWholeBlockIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch); !ok {
		t.Fatalf("ReconstructWholeBlockIntraMacroblock returned false")
	}

	assertPlaneValue(t, "Y", y, 100)
	assertPlaneValue(t, "U", u, 90)
	assertPlaneValue(t, "V", v, 70)
}

func TestReconstructWholeBlockIntraMacroblockRejectsBPred(t *testing.T) {
	mode := MacroblockMode{Mode: common.BPred, UVMode: common.DCPred, Is4x4: true}
	tokens := wholeBlockResidualTokens()
	dequant := testMacroblockDequant()
	refs := testIntraPredictorRefs(100, 90, 70)
	y := filledPlane(16, 16, 99)
	u := filledPlane(8, 8, 99)
	v := filledPlane(8, 8, 99)
	var scratch MacroblockResidual

	if ok := ReconstructWholeBlockIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch); ok {
		t.Fatalf("BPred macroblock returned true")
	}
	assertPlaneValue(t, "Y", y, 99)
	assertPlaneValue(t, "U", u, 99)
	assertPlaneValue(t, "V", v, 99)
}

func TestReconstructWholeBlockIntraMacroblockAllocatesZero(t *testing.T) {
	mode := MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred}
	tokens := wholeBlockResidualTokens()
	dequant := testMacroblockDequant()
	refs := testIntraPredictorRefs(100, 90, 70)
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	var scratch MacroblockResidual
	allocs := testing.AllocsPerRun(1000, func() {
		ReconstructWholeBlockIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestReconstructBPredIntraMacroblockInterleavesYResiduals(t *testing.T) {
	mode := bpredMacroblockMode(false)
	tokens := bpredResidualTokens()
	dequant := testMacroblockDequant()
	refs := tmIntraPredictorRefs()
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	var scratch MacroblockResidual

	if ok := ReconstructBPredIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch); !ok {
		t.Fatalf("ReconstructBPredIntraMacroblock returned false")
	}

	if got := y[0]; got != 90 {
		t.Fatalf("reconstructed first BPred pixel = %d, want 90", got)
	}
	if got := y[4]; got != 94 {
		t.Fatalf("next block did not read reconstructed left neighbor: got %d, want 94", got)
	}
	assertPlaneValue(t, "U", u, 90)
	assertPlaneValue(t, "V", v, 70)
}

func TestReconstructBPredIntraMacroblockSkipCoeff(t *testing.T) {
	mode := bpredMacroblockMode(true)
	tokens := bpredResidualTokens()
	dequant := testMacroblockDequant()
	refs := tmIntraPredictorRefs()
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	var scratch MacroblockResidual

	if ok := ReconstructBPredIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch); !ok {
		t.Fatalf("ReconstructBPredIntraMacroblock returned false")
	}

	if got := y[0]; got != 80 {
		t.Fatalf("skip first BPred pixel = %d, want prediction 80", got)
	}
	if got := y[4]; got != 84 {
		t.Fatalf("skip next block pixel = %d, want prediction 84", got)
	}
	assertPlaneValue(t, "U", u, 90)
	assertPlaneValue(t, "V", v, 70)
}

func TestReconstructBPredIntraMacroblockRejectsWholeBlock(t *testing.T) {
	mode := MacroblockMode{Mode: common.DCPred, UVMode: common.DCPred}
	tokens := bpredResidualTokens()
	dequant := testMacroblockDequant()
	refs := tmIntraPredictorRefs()
	y := filledPlane(16, 16, 99)
	u := filledPlane(8, 8, 99)
	v := filledPlane(8, 8, 99)
	var scratch MacroblockResidual

	if ok := ReconstructBPredIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch); ok {
		t.Fatalf("whole-block mode returned true")
	}
	assertPlaneValue(t, "Y", y, 99)
	assertPlaneValue(t, "U", u, 99)
	assertPlaneValue(t, "V", v, 99)
}

func TestReconstructBPredIntraMacroblockAllocatesZero(t *testing.T) {
	mode := bpredMacroblockMode(false)
	tokens := bpredResidualTokens()
	dequant := testMacroblockDequant()
	refs := tmIntraPredictorRefs()
	y := filledPlane(16, 16, 0)
	u := filledPlane(8, 8, 0)
	v := filledPlane(8, 8, 0)
	var scratch MacroblockResidual
	allocs := testing.AllocsPerRun(1000, func() {
		ReconstructBPredIntraMacroblock(&mode, &tokens, &dequant, refs, y, 16, u, 8, v, 8, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

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

	for row := 0; row < 16; row++ {
		for col := 0; col < 32; col++ {
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

func testMacroblockDequant() common.MacroblockDequant {
	var dequant common.MacroblockDequant
	for i := 0; i < 16; i++ {
		dequant.Y1[i] = int16(5 + i)
		dequant.Y1DC[i] = int16(6 + i)
		dequant.Y2[i] = int16(4 + i)
		dequant.UV[i] = int16(6 + i)
	}
	dequant.Y1DC[0] = 1
	return dequant
}

func testMacroblockDequants() [common.MaxMBSegments]common.MacroblockDequant {
	var dequants [common.MaxMBSegments]common.MacroblockDequant
	for i := range dequants {
		dequants[i] = testMacroblockDequant()
	}
	return dequants
}

func wholeBlockResidualTokens() MacroblockTokens {
	var tokens MacroblockTokens
	for i := 0; i < 16; i++ {
		tokens.EOB[i] = 1
	}
	tokens.QCoeff[24][0] = 16
	tokens.EOB[24] = 1
	tokens.QCoeff[16][0] = 16
	tokens.EOB[16] = 1
	return tokens
}

func bpredResidualTokens() MacroblockTokens {
	var tokens MacroblockTokens
	tokens.QCoeff[0][0] = 16
	tokens.EOB[0] = 1
	return tokens
}

func bpredMacroblockMode(skip bool) MacroblockMode {
	mode := MacroblockMode{Mode: common.BPred, UVMode: common.DCPred, Is4x4: true, MBSkipCoeff: skip}
	for i := range mode.BModes {
		mode.BModes[i] = common.BTMPred
	}
	return mode
}

func tmIntraPredictorRefs() IntraPredictorRefs {
	refs := testIntraPredictorRefs(0, 90, 70)
	refs.YAbove = make([]byte, 20)
	refs.YLeft = make([]byte, 16)
	for i := range refs.YAbove {
		refs.YAbove[i] = byte(50 + i)
	}
	for i := range refs.YLeft {
		refs.YLeft[i] = byte(70 + i)
	}
	refs.YTopLeft = 40
	return refs
}

func testIntraPredictorRefs(y byte, u byte, v byte) IntraPredictorRefs {
	return IntraPredictorRefs{
		YAbove:        filledPlane(20, 1, y),
		YLeft:         filledPlane(16, 1, y),
		UAbove:        filledPlane(8, 1, u),
		ULeft:         filledPlane(8, 1, u),
		VAbove:        filledPlane(8, 1, v),
		VLeft:         filledPlane(8, 1, v),
		YTopLeft:      y,
		UTopLeft:      u,
		VTopLeft:      v,
		UpAvailable:   true,
		LeftAvailable: true,
	}
}

func testImage(width int, height int) common.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := common.Image{
		Width:   width,
		Height:  height,
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
	}
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			img.Y[row*img.YStride+col] = byte((row*7 + col*3 + 1) & 0xff)
		}
	}
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte((row*5 + col*9 + 2) & 0xff)
			img.V[row*img.VStride+col] = byte((row*11 + col*13 + 3) & 0xff)
		}
	}
	return img
}

func blankImage(width int, height int) common.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return common.Image{
		Width:   width,
		Height:  height,
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
	}
}

func filledPlane(stride int, height int, value byte) []byte {
	plane := make([]byte, stride*height)
	for i := range plane {
		plane[i] = value
	}
	return plane
}

func assertPlaneValue(t *testing.T, name string, plane []byte, want byte) {
	t.Helper()
	for i, got := range plane {
		if got != want {
			t.Fatalf("%s[%d] = %d, want %d", name, i, got, want)
		}
	}
}

func assertSliceValue(t *testing.T, name string, got []byte, want byte) {
	t.Helper()
	for i, v := range got {
		if v != want {
			t.Fatalf("%s[%d] = %d, want %d", name, i, v, want)
		}
	}
}

func assertPlaneEqual(t *testing.T, name string, got []byte, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d", name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %d, want %d", name, i, got[i], want[i])
		}
	}
}
