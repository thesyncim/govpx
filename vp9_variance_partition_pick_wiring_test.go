package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9VarPartDecisionForClaimAtBSize: when the picker stamped bsize
// at (miRow, miCol), the decision read-back returns (BlockInvalid,
// false) — i.e. the caller stays at bsize (no subdivision).
func TestVP9VarPartDecisionForClaimAtBSize(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	// Picker claimed Block32x32 at (0, 0).
	e.varPartGrid[0].SbType = common.Block32x32
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if ok || got != common.BlockInvalid {
		t.Errorf("decision = (%v, %v), want (BlockInvalid, false)", got, ok)
	}
}

// TestVP9VarPartDecisionForSplitToSmaller: when the picker stamped a
// smaller bsize than the call's bsize, the decision returns splitSize.
func TestVP9VarPartDecisionForSplitToSmaller(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	// Picker stamped Block16x16 at (0, 0) under a Block32x32 call.
	e.varPartGrid[0].SbType = common.Block16x16
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != common.Block16x16 {
		t.Errorf("decision = (%v, %v), want (Block16x16, true)", got, ok)
	}
}

// TestVP9VarPartDecisionForHorzSplit pins the horizontal-split detection.
func TestVP9VarPartDecisionForHorzSplit(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	// Block32x32 with PartitionHorz => Block32x16.
	horz := common.SubsizeLookup[common.PartitionHorz][common.Block32x32]
	e.varPartGrid[0].SbType = horz
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != horz {
		t.Errorf("decision = (%v, %v), want (%v, true)", got, ok, horz)
	}
}

// TestVP9VarPartDecisionForVertSplit pins the vertical-split detection.
func TestVP9VarPartDecisionForVertSplit(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = true
	vert := common.SubsizeLookup[common.PartitionVert][common.Block32x32]
	e.varPartGrid[0].SbType = vert
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if !ok || got != vert {
		t.Errorf("decision = (%v, %v), want (%v, true)", got, ok, vert)
	}
}

// TestVP9VarPartDecisionForInvalidWhenNotValid: when varPartFrameValid
// is false (picker hasn't run), the read-back returns
// (BlockInvalid, false) so the existing pickers can take over.
func TestVP9VarPartDecisionForInvalidWhenNotValid(t *testing.T) {
	const miRows, miCols = 8, 8
	e := &VP9Encoder{}
	e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	e.varPartFrameValid = false
	e.varPartGrid[0].SbType = common.Block16x16
	got, ok := e.vp9VarPartDecisionFor(miCols, 0, 0, common.Block32x32)
	if ok || got != common.BlockInvalid {
		t.Errorf("decision = (%v, %v), want (BlockInvalid, false) when frame invalid",
			got, ok)
	}
}

// TestVP9ChoosePartitioningSBIndex pins the SB-index computation
// (mi_stride >> 3) * (mi_row >> 3) + (mi_col >> 3) — libvpx
// vp9_encodeframe.c:1314.
func TestVP9ChoosePartitioningSBIndex(t *testing.T) {
	e := &VP9Encoder{}
	// 64x64 frame: 8 mi cols, 8 mi rows, 1 SB.
	if got := e.vp9ChoosePartitioningSBIndex(8, 0, 0); got != 0 {
		t.Errorf("sbIdx(8, 0, 0) = %d, want 0", got)
	}
	// 128x64 frame: 16 mi cols, 8 mi rows, 2 SBs.
	if got := e.vp9ChoosePartitioningSBIndex(16, 0, 0); got != 0 {
		t.Errorf("sbIdx(16, 0, 0) = %d, want 0", got)
	}
	if got := e.vp9ChoosePartitioningSBIndex(16, 0, 8); got != 1 {
		t.Errorf("sbIdx(16, 0, 8) = %d, want 1", got)
	}
	// 128x128 frame: 16 mi cols, 16 mi rows, 4 SBs.
	if got := e.vp9ChoosePartitioningSBIndex(16, 8, 0); got != 2 {
		t.Errorf("sbIdx(16, 8, 0) = %d, want 2", got)
	}
	if got := e.vp9ChoosePartitioningSBIndex(16, 8, 8); got != 3 {
		t.Errorf("sbIdx(16, 8, 8) = %d, want 3", got)
	}
}
