package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestInterPredictionModeRateMatchesTreeCostForAllContexts(t *testing.T) {
	modes := []common.MBPredictionMode{
		common.ZeroMV,
		common.NearestMV,
		common.NearMV,
		common.NewMV,
		common.SplitMV,
	}
	for c0 := range tables.InterModeContextCount {
		for c1 := range tables.InterModeContextCount {
			for c2 := range tables.InterModeContextCount {
				for c3 := range tables.InterModeContextCount {
					counts := InterModeCounts{
						Intra:   uint8(c0),
						Nearest: uint8(c1),
						Near:    uint8(c2),
						Split:   uint8(c3),
					}
					probs := [4]uint8{
						tables.InterModeContexts[c0][0],
						tables.InterModeContexts[c1][1],
						tables.InterModeContexts[c2][2],
						tables.InterModeContexts[c3][3],
					}
					for _, mode := range modes {
						want := expectedInterPredictionModeRate(probs, mode)
						if got := InterPredictionModeRate(mode, counts); got != want {
							t.Fatalf("counts=(%d,%d,%d,%d) mode=%v rate=%d, want %d",
								c0, c1, c2, c3, mode, got, want)
						}
					}
				}
			}
		}
	}
}

func TestInterFrameModeCountsDriveDynamicProbLookup(t *testing.T) {
	zeroCounts := InterFrameModeCounts(nil, nil, nil,
		common.LastFrame, [common.MaxRefFrames]bool{})
	if zeroCounts != (InterModeCounts{}) {
		t.Fatalf("edge counts = %+v, want zero", zeroCounts)
	}

	aboveZeroMV := &InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.ZeroMV,
		MV:       MotionVector{},
	}
	zeroMVCounts := InterFrameModeCounts(aboveZeroMV, nil, nil,
		common.LastFrame, [common.MaxRefFrames]bool{})
	if zeroMVCounts.Intra != 2 {
		t.Fatalf("above-zero-MV counts.Intra = %d, want 2", zeroMVCounts.Intra)
	}

	aboveNewMV := &InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       MotionVector{Row: 16},
	}
	newMVCounts := InterFrameModeCounts(aboveNewMV, nil, nil,
		common.LastFrame, [common.MaxRefFrames]bool{})
	if newMVCounts.Nearest != 2 {
		t.Fatalf("above-new-MV counts.Nearest = %d, want 2", newMVCounts.Nearest)
	}
	if newMVCounts.Intra != 0 {
		t.Fatalf("above-new-MV counts.Intra = %d, want 0", newMVCounts.Intra)
	}

	zeroMVProb := tables.InterModeContexts[zeroMVCounts.Intra][0]
	newMVProb := tables.InterModeContexts[newMVCounts.Intra][0]
	if zeroMVProb == newMVProb {
		t.Fatalf("prob[0] identical for zero-MV ct=%d and new-MV ct=%d; want dynamic mode-context row lookup",
			zeroMVCounts.Intra, newMVCounts.Intra)
	}
}

func TestZeroMVRateUsesDynamicModeContext(t *testing.T) {
	for ct := range tables.InterModeContextCount {
		counts := InterModeCounts{Intra: uint8(ct)}
		want := BoolBitCost(tables.InterModeContexts[ct][0], 0)
		if got := InterPredictionModeRate(common.ZeroMV, counts); got != want {
			t.Fatalf("ZEROMV cost at counts.Intra=%d = %d, want %d", ct, got, want)
		}
	}

	costNoZeroNeighbor := InterPredictionModeRate(common.ZeroMV, InterModeCounts{Intra: 0})
	costZeroNeighbor := InterPredictionModeRate(common.ZeroMV, InterModeCounts{Intra: 2})
	if costNoZeroNeighbor <= costZeroNeighbor {
		t.Fatalf("ZEROMV cost ct=0 minus ct=2 = %d, want positive dynamic-context gap",
			costNoZeroNeighbor-costZeroNeighbor)
	}
}

func TestInterFrameModeCountsAtFrameOriginAreZero(t *testing.T) {
	for _, refFrame := range []common.MVReferenceFrame{
		common.LastFrame,
		common.GoldenFrame,
		common.AltRefFrame,
	} {
		for _, signBiasGF := range []bool{false, true} {
			for _, signBiasAR := range []bool{false, true} {
				signBias := [common.MaxRefFrames]bool{
					common.GoldenFrame: signBiasGF,
					common.AltRefFrame: signBiasAR,
				}
				counts := InterFrameModeCounts(nil, nil, nil, refFrame, signBias)
				if counts != (InterModeCounts{}) {
					t.Fatalf("ref=%v signBias=(GF=%t,AR=%t) frame-origin counts=%+v, want zero",
						refFrame, signBiasGF, signBiasAR, counts)
				}
			}
		}
	}
}

func TestSplitSubMotionLabelRateMatchesDefaultCostTree(t *testing.T) {
	probs := DefaultSubMVRefProbs
	want := map[common.BPredictionMode]int{
		common.Left4x4:  BoolBitCost(probs[0], 0),
		common.Above4x4: BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 0),
		common.Zero4x4:  BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 0),
		common.New4x4:   BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 1),
	}
	for mode, wantCost := range want {
		if got := SplitSubMotionLabelRate(mode, nil); got != wantCost {
			t.Fatalf("SplitSubMotionLabelRate(%v) = %d, want %d", mode, got, wantCost)
		}
	}
}

func expectedInterPredictionModeRate(probs [4]uint8, mode common.MBPredictionMode) int {
	switch mode {
	case common.ZeroMV:
		return BoolBitCost(probs[0], 0)
	case common.NearestMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 0)
	case common.NearMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 0)
	case common.NewMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 1) + BoolBitCost(probs[3], 0)
	case common.SplitMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 1) + BoolBitCost(probs[3], 1)
	default:
		panic("unknown inter prediction mode")
	}
}
