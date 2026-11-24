package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestFindNearMotionVectorsUsesAbove(t *testing.T) {
	above := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 4, Col: 2}}

	nearest, near, best, counts := FindNearMotionVectors(&above, nil, nil, common.LastFrame, [common.MaxRefFrames]bool{})

	if nearest != above.MV || best != above.MV || !near.IsZero() {
		t.Fatalf("nearest/near/best = %+v/%+v/%+v, want above/zero/above", nearest, near, best)
	}
	if counts != (InterModeCounts{Nearest: 2}) {
		t.Fatalf("counts = %+v, want nearest count 2", counts)
	}
}

func TestFindNearMotionVectorsBiasesReference(t *testing.T) {
	above := MacroblockMode{RefFrame: common.GoldenFrame, MV: MotionVector{Row: 6, Col: -8}}
	signBias := [common.MaxRefFrames]bool{common.GoldenFrame: true}

	nearest, _, best, _ := FindNearMotionVectors(&above, nil, nil, common.LastFrame, signBias)

	if nearest != (MotionVector{Row: -6, Col: 8}) || best != nearest {
		t.Fatalf("biased nearest/best = %+v/%+v, want {-6,8}", nearest, best)
	}
}

func TestFindNearMotionVectorsSwapsNearAndNearest(t *testing.T) {
	above := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 0}}
	left := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 4, Col: 0}}
	aboveLeft := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 4, Col: 0}}

	nearest, near, best, counts := FindNearMotionVectors(&above, &left, &aboveLeft, common.LastFrame, [common.MaxRefFrames]bool{})

	if nearest != left.MV || near != above.MV || best != left.MV {
		t.Fatalf("nearest/near/best = %+v/%+v/%+v, want left/above/left", nearest, near, best)
	}
	if counts.Nearest != 3 || counts.Near != 2 {
		t.Fatalf("counts = %+v, want nearest 3 near 2", counts)
	}
}

func TestFindNearMotionVectorsCountsSplitNeighbors(t *testing.T) {
	above := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame}
	left := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame}
	aboveLeft := MacroblockMode{Mode: common.SplitMV, RefFrame: common.LastFrame}

	_, _, _, counts := FindNearMotionVectors(&above, &left, &aboveLeft, common.LastFrame, [common.MaxRefFrames]bool{})

	if counts.Split != 5 {
		t.Fatalf("split count = %d, want 5", counts.Split)
	}
}

func TestFindNearMotionVectorsAllocatesZero(t *testing.T) {
	above := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 2, Col: 0}}
	left := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 4, Col: 0}}
	aboveLeft := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 4, Col: 0}}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _, _, _ = FindNearMotionVectors(&above, &left, &aboveLeft, common.LastFrame, [common.MaxRefFrames]bool{})
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
