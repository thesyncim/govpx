package decoder

import (
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
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
