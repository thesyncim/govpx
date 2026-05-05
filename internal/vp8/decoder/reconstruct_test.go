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
