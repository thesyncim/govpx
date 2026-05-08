package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestTreeTokenCostMatchesSlowReference asserts that the precomputed-path
// fast path returns identical costs to the original tree-walking
// implementation across every (tree, token) pair encoder callers use, for
// a representative spread of probability values.
func TestTreeTokenCostMatchesSlowReference(t *testing.T) {
	cases := []struct {
		name   string
		tree   []int16
		probs  []uint8
		tokens []int
	}{
		{
			name:   "KeyFrameYMode",
			tree:   vp8tables.KeyFrameYModeTree[:],
			probs:  vp8tables.KeyFrameYModeProbs[:],
			tokens: []int{0, 1, 2, 3, 4},
		},
		{
			name:   "YMode",
			tree:   vp8tables.YModeTree[:],
			probs:  vp8tables.DefaultYModeProbs[:],
			tokens: []int{0, 1, 2, 3, 4},
		},
		{
			name:   "UVMode",
			tree:   vp8tables.UVModeTree[:],
			probs:  vp8tables.KeyFrameUVModeProbs[:],
			tokens: []int{0, 1, 2, 3},
		},
		{
			name:   "BMode",
			tree:   vp8tables.BModeTree[:],
			probs:  vp8tables.DefaultBModeProbs[:],
			tokens: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		},
		{
			name:   "MBSplit",
			tree:   vp8tables.MBSplitTree[:],
			probs:  vp8tables.MBSplitProbs[:],
			tokens: []int{0, 1, 2, 3},
		},
		{
			name:  "SubMVRef",
			tree:  vp8tables.SubMVRefTree[:],
			probs: libvpxDefaultSubMVRefProbs[:],
			tokens: []int{
				int(vp8common.Left4x4),
				int(vp8common.Above4x4),
				int(vp8common.Zero4x4),
				int(vp8common.New4x4),
			},
		},
		{
			name:   "Coef",
			tree:   vp8tables.CoefTree[:],
			probs:  vp8tables.DefaultCoefProbs[0][0][0][:],
			tokens: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for _, tok := range c.tokens {
				got := treeTokenCost(c.tree, c.probs, tok)
				want := treeTokenCostSlow(c.tree, c.probs, tok)
				if got != want {
					t.Fatalf("token %d: fast=%d slow=%d", tok, got, want)
				}
			}
		})
	}
}

// TestCoefTokenCostFromPathMatchesSlowReference checks the CoefTree
// specialized helper against the historical walker for every (token, prob)
// combination that mode decision can synthesize.
func TestCoefTokenCostFromPathMatchesSlowReference(t *testing.T) {
	probSeeds := [][vp8tables.EntropyNodes]uint8{
		vp8tables.DefaultCoefProbs[0][0][0],
		vp8tables.DefaultCoefProbs[1][3][2],
		vp8tables.DefaultCoefProbs[2][5][1],
		vp8tables.DefaultCoefProbs[3][7][2],
	}
	for seedIdx, probs := range probSeeds {
		probs := probs
		for token := 0; token <= vp8tables.DCTEOBToken; token++ {
			fast := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
			slow := treeTokenCostSlow(vp8tables.CoefTree[:], probs[:], token)
			if fast != slow {
				t.Fatalf("seed %d token %d: fast=%d slow=%d", seedIdx, token, fast, slow)
			}
		}
	}
}

// BenchmarkTreeTokenCost exercises the dispatched treeTokenCost across the
// fixed mode and coefficient trees the encoder consults in mode decision.
// The body mirrors the per-MB call mix: 1 KeyFrameYMode + 1 UVMode + 16
// BMode + a handful of CoefTree lookups.
func BenchmarkTreeTokenCost(b *testing.B) {
	bModeProbs := vp8tables.DefaultBModeProbs[:]
	keyYProbs := vp8tables.KeyFrameYModeProbs[:]
	uvProbs := vp8tables.KeyFrameUVModeProbs[:]
	coefProbs := vp8tables.DefaultCoefProbs[0][0][0]
	coefTokens := [...]int{
		vp8tables.ZeroToken, vp8tables.OneToken, vp8tables.TwoToken,
		vp8tables.DCTValCategory1, vp8tables.DCTValCategory3,
		vp8tables.DCTEOBToken,
	}
	bModes := [...]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	yModes := [...]int{0, 1, 2, 3, 4}
	var sink int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, m := range yModes {
			sink += treeTokenCost(vp8tables.KeyFrameYModeTree[:], keyYProbs, m)
		}
		for _, m := range bModes {
			sink += treeTokenCost(vp8tables.BModeTree[:], bModeProbs, m)
		}
		sink += treeTokenCost(vp8tables.UVModeTree[:], uvProbs, 0)
		for _, t := range coefTokens {
			sink += treeTokenCost(vp8tables.CoefTree[:], coefProbs[:], t)
		}
	}
	_ = sink
}

// BenchmarkTreeTokenCostCoef isolates the CoefTree dispatch path, which
// dominates per-block coefficient cost evaluation.
func BenchmarkTreeTokenCostCoef(b *testing.B) {
	probs := vp8tables.DefaultCoefProbs[0][0][0]
	tokens := [12]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	var sink int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tok := range tokens {
			sink += treeTokenCost(vp8tables.CoefTree[:], probs[:], tok)
		}
	}
	_ = sink
}

// BenchmarkCoefTokenCostFromPath benchmarks the inlined CoefTree path
// helper without the dispatch layer, isolating the per-token math.
func BenchmarkCoefTokenCostFromPath(b *testing.B) {
	probs := vp8tables.DefaultCoefProbs[0][0][0]
	tokens := [12]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	var sink int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tok := range tokens {
			sink += coefTokenCostFromPath(&coefTokenPaths[tok], &probs)
		}
	}
	_ = sink
}

// BenchmarkCoefBlockTokenRate exercises the per-position coefficient cost
// loop that consumes most of the entropy-cost budget during mode decision.
// The block has a typical sparse pattern (one DC + a couple low-mag AC
// coefficients) representative of intra16x16 / inter wholeblock RD.
func BenchmarkCoefBlockTokenRate(b *testing.B) {
	probs := vp8tables.DefaultCoefProbs
	var qcoeff [16]int16
	qcoeff[0] = -3
	qcoeff[1] = 2
	qcoeff[5] = 1
	qcoeff[10] = -1
	var sink int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink += coefficientBlockTokenRate(&probs, 0, 0, 1, &qcoeff, 11)
		sink += coefficientBlockTokenRate(&probs, 3, 0, 0, &qcoeff, 11)
		sink += coefficientBlockTokenRate(&probs, 2, 0, 0, &qcoeff, 8)
		sink += coefficientBlockTokenRate(&probs, 1, 0, 0, &qcoeff, 11)
	}
	_ = sink
}

// BenchmarkCoefBlockTokenRateZero captures the all-zero block path that
// fires on quantize-skip macroblocks (eob == skipDC).
func BenchmarkCoefBlockTokenRateZero(b *testing.B) {
	probs := vp8tables.DefaultCoefProbs
	var qcoeff [16]int16
	var sink int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink += coefficientBlockTokenRate(&probs, 0, 0, 1, &qcoeff, 1)
		sink += coefficientBlockTokenRate(&probs, 3, 0, 0, &qcoeff, 0)
	}
	_ = sink
}
