package encoder

import (
	"math"
	"testing"
)

// TestVP9ProbCostMatchesFormula spot-checks a few entries against
// the documented formula round(-log2(i/256.0) * (1 << 9)). The
// first entry (i=0) is a sentinel and not formula-derived.
func TestVP9ProbCostMatchesFormula(t *testing.T) {
	for _, i := range []int{1, 16, 64, 128, 192, 255} {
		want := math.Round(-math.Log2(float64(i)/256.0) * float64(int(1)<<VP9ProbCostShift))
		if math.Abs(float64(VP9ProbCost[i])-want) > 0.5 {
			t.Errorf("[%d] table=%d formula=%g", i, VP9ProbCost[i], want)
		}
	}
}

// TestCostBranch256Symmetry: cost(ct={a, b}, p=128) must equal
// cost(ct={b, a}, p=128) because vp9_cost_zero(128) ==
// vp9_cost_one(128).
func TestCostBranch256Symmetry(t *testing.T) {
	got1 := CostBranch256([2]uint32{30, 70}, 128)
	got2 := CostBranch256([2]uint32{70, 30}, 128)
	if got1 != got2 {
		t.Errorf("p=128 not symmetric: %d vs %d", got1, got2)
	}
}

// TestProbDiffUpdateSavingsSearchAcceptsImprovement: a count pair
// strongly biased away from oldp should produce a positive savings
// and shift bestp.
func TestProbDiffUpdateSavingsSearchAcceptsImprovement(t *testing.T) {
	ct := [2]uint32{1000, 10} // mostly zeros → low new-p is the right pick
	old := uint8(200)         // wildly mis-tuned
	best := GetBinaryProb(ct[0], ct[1])
	gotBest := best
	savings := ProbDiffUpdateSavingsSearch(ct, old, &gotBest, DiffUpdateProb)
	if savings <= 0 {
		t.Errorf("savings = %d, want > 0", savings)
	}
	if gotBest == old {
		t.Errorf("bestp unchanged at %d", gotBest)
	}
}

// TestProbDiffUpdateSavingsSearchRejectsNoise: when the counts
// already match `oldp` closely, the savings search keeps oldp.
func TestProbDiffUpdateSavingsSearchRejectsNoise(t *testing.T) {
	ct := [2]uint32{0, 0}
	old := uint8(128)
	best := GetBinaryProb(ct[0], ct[1])
	gotBest := best
	savings := ProbDiffUpdateSavingsSearch(ct, old, &gotBest, DiffUpdateProb)
	if savings != 0 {
		t.Errorf("zero counts savings = %d, want 0", savings)
	}
	if gotBest != old {
		t.Errorf("zero counts: bestp = %d, want %d", gotBest, old)
	}
}

// TestGetBinaryProbBounds: clamps to [1, 255], 0/0 returns 128.
func TestGetBinaryProbBounds(t *testing.T) {
	if got := GetBinaryProb(0, 0); got != 128 {
		t.Errorf("0/0 = %d, want 128", got)
	}
	if got := GetBinaryProb(1000, 0); got != 255 {
		t.Errorf("1000/0 = %d, want 255", got)
	}
	if got := GetBinaryProb(0, 1000); got != 1 {
		t.Errorf("0/1000 = %d, want 1", got)
	}
}

// TestVP9CostTokensTreeDepth walks a 4-leaf right-skewed binary
// tree under p=128 (every bit costs equally), so each leaf's cost
// equals its tree depth times VP9CostBit(128, 0). Tree
// `{2, -0, 4, -1, -2, -3}` puts leaf 0 at depth 1, leaf 1 at depth
// 2, and leaves 2 and 3 at depth 3.
func TestVP9CostTokensTreeDepth(t *testing.T) {
	tree := []int8{2, -0, 4, -1, -2, -3}
	probs := []uint8{128, 128, 128}
	costs := make([]int, 4)
	VP9CostTokens(costs, probs, tree)
	bit := VP9CostBit(128, 0)
	wants := []int{1 * bit, 2 * bit, 3 * bit, 3 * bit}
	for i, c := range costs {
		if c != wants[i] {
			t.Errorf("costs[%d] = %d, want %d", i, c, wants[i])
		}
	}
}

// TestVP9CostTokensSkewedProb anchors a degenerate case: with prob
// 1 (almost always emits 1), the left-most leaf (path 00) should be
// dramatically cheaper than the right-most leaf (path 11).
func TestVP9CostTokensSkewedProb(t *testing.T) {
	tree := []int8{2, -0, 4, -1, -2, -3}
	probs := []uint8{1, 1, 1}
	costs := make([]int, 4)
	VP9CostTokens(costs, probs, tree)
	// Leaf 0 takes a single "go right" decision (bit=1, prob=1 → very cheap).
	// Leaf 3 takes "go left, go left" then to leaf 3 — expensive bits.
	if costs[0] >= costs[3] {
		t.Errorf("skew direction inverted: leaf0=%d leaf3=%d", costs[0], costs[3])
	}
}

// TestTreedCostMatchesCostTokens walks a known leaf bit-pattern
// through TreedCost and compares against the matching
// VP9CostTokens output for that leaf.
func TestTreedCostMatchesCostTokens(t *testing.T) {
	tree := []int8{2, -0, 4, -1, -2, -3}
	probs := []uint8{128, 128, 128}
	costs := make([]int, 4)
	VP9CostTokens(costs, probs, tree)
	// Walk: idx 0 with bit=0 → tree[0]=2 (next-idx). At idx 2 with
	// bit=1 → tree[3]=-1 (leaf 1). So leaf 1 = path "01".
	got := TreedCost(tree, probs, 0b01, 2)
	if got != costs[1] {
		t.Errorf("TreedCost = %d, VP9CostTokens leaf 1 = %d", got, costs[1])
	}
}
