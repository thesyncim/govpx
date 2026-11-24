package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

func algo136R3sSearchCounts(seed int) (ct [EntropyNodes][2]uint32) {
	for i := range ct {
		ct[i] = [2]uint32{uint32((seed*107 + i*73) % 4096), uint32((seed*53 + i*37) % 2048)}
	}
	return
}

func TestAlgo136Round3SearchSeed(t *testing.T) {
	for seed := 1; seed <= 255; seed++ {
		ct := algo136R3sSearchCounts(seed)
		for _, best := range []uint8{1, uint8(seed), 128, 255} {
			for _, step := range []int{1, 2, 4} {
				a, b := best, best
				x := ProbDiffUpdateSavingsSearchModel(&ct, uint8(seed), &a, 252, step)
				y := algo136R3sFrozenModel(&ct, uint8(seed), &b, 252, step)
				if x != y || a != b {
					t.Fatalf("model seed%d best%d step%d", seed, best, step)
				}
				a, b = best, best
				x = ProbDiffUpdateSavingsSearch(ct[2], uint8(seed), &a, 252)
				y = algo136R3sFrozenBinary(ct[2], uint8(seed), &b, 252)
				if x != y || a != b {
					t.Fatalf("binary seed%d best%d", seed, best)
				}
			}
		}
	}
}

var algo136R3sSavings int64

var algo136R3sBest uint8

func BenchmarkAlgo136Round3SearchSeed(b *testing.B) {
	for _, model := range []bool{false, true} {
		for _, aligned := range []bool{false, true} {
			b.Run(fmt.Sprintf("model%v/aligned%v", model, aligned), func(b *testing.B) {
				ct := algo136R3sSearchCounts(17)
				seed := uint8(73)
				if !aligned {
					seed = 192
				}
				b.ReportAllocs()
				for b.Loop() {
					algo136R3sBest = seed
					if model {
						algo136R3sSavings = ProbDiffUpdateSavingsSearchModel(&ct, 73, &algo136R3sBest, 252, 4)
					} else {
						algo136R3sSavings = ProbDiffUpdateSavingsSearch(ct[2], 73, &algo136R3sBest, 252)
					}
				}
			})
		}
	}
}

func algo136R3sFrozenBinary(ct [2]uint32, oldp uint8, bestp *uint8, upd uint8) int64 {
	oldB := int64(CostBranch256(ct, oldp))
	bestSavings := int64(0)
	bestNewP := oldp
	step := int(1)
	if int(*bestp) > int(oldp) {
		step = -1
	}
	updCost := VP9CostOne(upd) - VP9CostZero(upd)

	if oldB > int64(updCost+(MinDelpBits<<VP9ProbCostShift)) {
		for newp := int(*bestp); newp != int(oldp); newp += step {
			newB := int64(CostBranch256(ct, uint8(newp)))
			updateB := int64(ProbDiffUpdateCost(uint8(newp), oldp) + updCost)
			savings := oldB - newB - updateB
			if savings > bestSavings {
				bestSavings = savings
				bestNewP = uint8(newp)
			}
		}
	}
	*bestp = bestNewP
	return bestSavings
}

// Frozen870699ce implementation.

func algo136R3sFrozenModel(ct *[EntropyNodes][2]uint32,
	oldp uint8, bestp *uint8, upd uint8, stepsize int,
) int64 {
	stepSign := int64(1)
	if int(*bestp) > int(oldp) {
		stepSign = -1
	}
	step := int64(stepsize) * stepSign
	updCost := int64(VP9CostOne(upd) - VP9CostZero(upd))

	oldPareto := tables.Pareto8Full[oldp-1]
	oldB := int64(CostBranch256(ct[PivotNode], oldp))
	for i := UnconstrainedNodes; i < EntropyNodes; i++ {
		oldB += int64(CostBranch256(ct[i], oldPareto[i-UnconstrainedNodes]))
	}

	bestSavings := int64(0)
	bestNewP := oldp

	if oldB > updCost+(MinDelpBits<<VP9ProbCostShift) {
		for newp := int64(*bestp); (newp-int64(oldp))*stepSign < 0; newp += step {
			if newp < 1 || newp > 255 {
				continue
			}
			newPareto := tables.Pareto8Full[newp-1]
			newB := int64(CostBranch256(ct[PivotNode], uint8(newp)))
			for i := UnconstrainedNodes; i < EntropyNodes; i++ {
				newB += int64(CostBranch256(ct[i], newPareto[i-UnconstrainedNodes]))
			}
			updateB := int64(ProbDiffUpdateCost(uint8(newp), oldp)) + updCost
			savings := oldB - newB - updateB
			if savings > bestSavings {
				bestSavings = savings
				bestNewP = uint8(newp)
			}
		}
	}
	*bestp = bestNewP
	return bestSavings
}

// Frozen870699ce implementation.
