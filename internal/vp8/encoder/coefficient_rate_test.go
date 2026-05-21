package encoder

import (
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestBlockPlaneRDMultiplierMatchesLibvpxPlaneTable(t *testing.T) {
	wantPlaneRDMult := [4]int{
		0: 4,  // Y1_RD_MULT (PLANE_TYPE_Y_NO_DC)
		1: 16, // Y2_RD_MULT (PLANE_TYPE_Y2)
		2: 2,  // UV_RD_MULT (PLANE_TYPE_UV)
		3: 4,  // Y1_RD_MULT (PLANE_TYPE_Y_WITH_DC)
	}
	for i, want := range wantPlaneRDMult {
		if got := BlockPlaneRDMultiplier(i); got != want {
			t.Fatalf("BlockPlaneRDMultiplier(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestRDBlockScoreAppliesLibvpxPlaneAndIntraMultipliers(t *testing.T) {
	if got := RDBlockScore(40, 4, false, 100, 20); got != 79 {
		t.Fatalf("inter block rd = %d, want 79", got)
	}
	if got := RDBlockScore(40, 4, true, 100, 20); got != 53 {
		t.Fatalf("intra block rd = %d, want 53", got)
	}
}

func TestMacroblockCoefficientTokenRateChargesNonZeroResiduals(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero MacroblockCoefficients
	zeroRate := MacroblockCoefficientTokenRate(&probs, false, &zero)

	nonzero := zero
	nonzero.QCoeff[24][0] = 2
	nonzero.SetBlockEOB(24, 1)
	nonzero.QCoeff[0][1] = -1
	nonzero.SetBlockEOB(0, 2)
	nonzero.QCoeff[16][0] = 1
	nonzero.SetBlockEOB(16, 1)
	nonzeroRate := MacroblockCoefficientTokenRate(&probs, false, &nonzero)

	if zeroRate <= 0 {
		t.Fatalf("zero residual token rate = %d, want positive EOB signalling cost", zeroRate)
	}
	if nonzeroRate <= zeroRate {
		t.Fatalf("nonzero residual token rate = %d, zero = %d, want higher rate", nonzeroRate, zeroRate)
	}

	ClearMacroblockCoefficients(&nonzero)
	if clearedRate := MacroblockCoefficientTokenRate(&probs, false, &nonzero); clearedRate != zeroRate {
		t.Fatalf("cleared residual rate = %d, want zero residual rate %d", clearedRate, zeroRate)
	}
}

func TestMacroblockCoefficientUVContextIndexMatchesLibvpxChromaBlocks(t *testing.T) {
	libvpxBlock2Above := [25]uint8{
		0, 1, 2, 3, 0, 1, 2, 3, 0,
		1, 2, 3, 0, 1, 2, 3, 4, 5,
		4, 5, 6, 7, 6, 7, 8,
	}
	libvpxBlock2Left := [25]uint8{
		0, 0, 0, 0, 1, 1, 1, 1, 2,
		2, 2, 2, 3, 3, 3, 3, 4, 4,
		5, 5, 6, 6, 7, 7, 8,
	}
	const uvBase = 4
	for block := 16; block < 24; block++ {
		wantA := int(libvpxBlock2Above[block]) - uvBase
		wantL := int(libvpxBlock2Left[block]) - uvBase
		gotA, gotL := MacroblockCoefficientUVContextIndex(block)
		if gotA != wantA || gotL != wantL {
			t.Fatalf("block %d context index = (%d,%d), want (%d,%d)", block, gotA, gotL, wantA, wantL)
		}
	}
}
