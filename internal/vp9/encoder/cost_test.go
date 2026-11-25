package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"math"
	"testing"
)

// TestProbDiffUpdateSavingsSearchModelPicksUpdate: counts at the
// PivotNode heavily favor "zero" (low prob); the search should
// find a smaller prob that saves bits and report positive savings.
// The tail nodes carry no events so they don't bias the savings.
func TestProbDiffUpdateSavingsSearchModelPicksUpdate(t *testing.T) {
	var ct [EntropyNodes][2]uint32
	ct[PivotNode] = [2]uint32{1000, 100} // heavy zero bias at pivot

	oldp := uint8(128)
	bestp := GetBinaryProb(ct[PivotNode][0], ct[PivotNode][1])
	startBestp := bestp
	savings := ProbDiffUpdateSavingsSearchModel(&ct, oldp, &bestp, DiffUpdateProb, 4)
	if savings <= 0 {
		t.Fatalf("savings = %d, want > 0 for heavy-bias counts", savings)
	}
	if bestp == oldp {
		t.Errorf("bestp unchanged at oldp; expected savings-search to pick a different prob")
	}
	// Search walks toward oldp. The picked bestp lies between
	// startBestp (raw count prob) and oldp, in either direction
	// depending on which way around they are.
	lo, hi := int(oldp), int(startBestp)
	if lo > hi {
		lo, hi = hi, lo
	}
	if int(bestp) < lo || int(bestp) > hi {
		t.Errorf("bestp=%d outside expected [%d, %d]", bestp, lo, hi)
	}
}

// TestProbDiffUpdateSavingsSearchModelStepSizeBounds: stepsize > 1
// reduces the search granularity; picking a stepsize that overshoots
// the gap (oldp == bestp ± 1) leaves bestp unchanged from oldp since
// no candidate lies strictly between them.
func TestProbDiffUpdateSavingsSearchModelStepSizeBounds(t *testing.T) {
	var ct [EntropyNodes][2]uint32
	ct[PivotNode] = [2]uint32{1, 0} // tiny counts → bestp should land near 255

	oldp := uint8(254)
	bestp := uint8(255) // bestp very close to oldp
	savings := ProbDiffUpdateSavingsSearchModel(&ct, oldp, &bestp, DiffUpdateProb, 4)
	// With stepsize=4 and only 1 step between oldp and bestp, the
	// search loop body never runs; savings stays 0 and bestp stays
	// at oldp.
	if savings != 0 {
		t.Errorf("savings = %d, want 0 for too-large stepsize", savings)
	}
	if bestp != oldp {
		t.Errorf("bestp = %d, want oldp = %d", bestp, oldp)
	}
}

// TestProbDiffUpdateSavingsSearchModelRoundTrip: write the update
// fragment that the model picked, then parse it back via
// VpxDiffUpdateProb — both sides land on the same new prob.
func TestProbDiffUpdateSavingsSearchModelRoundTrip(t *testing.T) {
	var ct [EntropyNodes][2]uint32
	ct[PivotNode] = [2]uint32{800, 50}

	oldp := uint8(128)
	bestp := max(GetBinaryProb(ct[PivotNode][0], ct[PivotNode][1]), 1)
	savings := ProbDiffUpdateSavingsSearchModel(&ct, oldp, &bestp, DiffUpdateProb, 4)
	if savings <= 0 {
		t.Skipf("savings=%d, no update path to round-trip", savings)
	}

	buf := make([]byte, 16)
	var bw bitstream.Writer
	bw.Start(buf)
	bw.Write(1, DiffUpdateProb) // update bit
	WriteProbDiffUpdate(&bw, bestp, oldp)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	decoded := oldp
	vp9dec.VpxDiffUpdateProb(&r, &decoded)
	if decoded != bestp {
		t.Errorf("decoded = %d, want bestp = %d (oldp=%d)", decoded, bestp, oldp)
	}
}

var coeffTreeTokenCostBenchModels = [][]uint8{
	{220, 134, 178},
	{32, 224, 8},
	{64, 192, 32},
	{96, 160, 64},
	{128, 128, 96},
	{160, 96, 160},
	{192, 64, 224},
	{224, 32, 255},
}

var coeffTreeTokenCostBenchTokens = [...]int{
	EobToken, ZeroToken, OneToken, TwoToken,
	ThreeToken, FourToken, Category1Tok, Category2Tok,
	Category3Tok, Category4Tok, Category5Tok, Category6Tok,
	EobToken, ZeroToken, OneToken, Category6Tok,
}

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

func BenchmarkCoeffTreeTokenCost(b *testing.B) {
	models := coeffTreeTokenCostBenchModels
	tokens := coeffTreeTokenCostBenchTokens
	for _, skipEOB := range []bool{false, true} {
		name := "full"
		if skipEOB {
			name = "skipEOB"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			got := 0
			for i := 0; i < b.N; i++ {
				got += CoeffTreeTokenCost(models[(i>>4)&7], skipEOB, tokens[i&15])
			}
			if got == 0 {
				b.Fatal("unexpected zero benchmark accumulator")
			}
		})
	}
}
