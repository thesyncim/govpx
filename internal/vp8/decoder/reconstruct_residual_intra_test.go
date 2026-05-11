package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

func TestAddMacroblockResidualYDCOnly(t *testing.T) {
	y := filledPlane(16, 16, 100)
	u := filledPlane(8, 8, 90)
	v := filledPlane(8, 8, 80)
	var tokens MacroblockTokens
	var residual MacroblockResidual
	tokens.EOB[5] = 1
	residual.Block(5)[0] = 16
	residual.Block(5)[1] = 512

	AddMacroblockResidual(&tokens, &residual, y, 16, u, 8, v, 8)

	for row := range 16 {
		for col := range 16 {
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
	for row := range 8 {
		for col := range 8 {
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
	for i := range 16 {
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
		for row := range 16 {
			for col := range 16 {
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
	for row := range 8 {
		for col := range 8 {
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

	for row := range 16 {
		for col := range 16 {
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
	for row := range 8 {
		for col := range 8 {
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
