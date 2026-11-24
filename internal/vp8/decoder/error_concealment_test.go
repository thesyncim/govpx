package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestPrepareErrorConcealmentModesCopiesMacroblockMV(t *testing.T) {
	modes := []MacroblockMode{{
		Mode:     common.NewMV,
		RefFrame: common.LastFrame,
		MV:       MotionVector{Row: 3, Col: -7},
	}}

	PrepareErrorConcealmentModes(modes)

	for i, mv := range modes[0].BlockMV {
		if mv != modes[0].MV {
			t.Fatalf("BlockMV[%d] = %+v, want macroblock MV %+v", i, mv, modes[0].MV)
		}
	}
}

func TestEstimateMissingMotionVectorsUsesPreviousOverlaps(t *testing.T) {
	prevModes := []MacroblockMode{
		{RefFrame: common.LastFrame},
		{},
	}
	for i := range prevModes[0].BlockMV {
		prevModes[0].BlockMV[i] = MotionVector{Col: -128}
	}
	modes := make([]MacroblockMode, 2)

	if err := EstimateMissingMotionVectors(modes, prevModes, 1, 2, 1); err != nil {
		t.Fatalf("EstimateMissingMotionVectors returned error: %v", err)
	}

	mode := modes[1]
	if mode.RefFrame != common.LastFrame || mode.Mode != common.SplitMV || !mode.Is4x4 || mode.Partition != 3 {
		t.Fatalf("estimated mode = %+v, want split LAST concealment mode", mode)
	}
	for i, mv := range mode.BlockMV {
		if mv.Col != -128 || mv.Row != 0 {
			t.Fatalf("BlockMV[%d] = %+v, want horizontal overlap MV", i, mv)
		}
	}
	if mode.MV.Col != -128 || mode.MV.Row != 0 {
		t.Fatalf("macroblock MV = %+v, want average horizontal overlap MV", mode.MV)
	}
}
