package govpx

import (
	"unsafe"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// Per-tree precomputed traversal paths. For a fixed VP8 tree, the path from
// the root to each leaf token is invariant — only the probability vector
// changes between calls. Walking the tree on every cost lookup was a 2-3%
// slice of mode-decision profiles (the entropy node-index math, the bound
// checks against tree[]/probs[], and the recursive findTreeToken inside
// BuildTreeToken were all per-call). Precomputing the path turns the cost
// into a tight loop over (probIndex, bit) pairs that gofmt+the compiler can
// fully bounds-check-elide.
//
// pathStep packs (probIndex, bit) so the entire path for any VP8 tree fits
// in a small fixed-size array. probIndex is bounded by the number of
// internal nodes (at most ~10 for VP8 trees), and bit is 0 or 1.
type treeTokenPathStep struct {
	probIndex uint8
	bit       uint8
}

// maxTreeTokenPathLen bounds the depth of any VP8 token tree. Cat6Tree is
// the deepest at 11 levels; 16 is comfortable headroom.
const maxTreeTokenPathLen = 16

type treeTokenPath struct {
	steps  [maxTreeTokenPathLen]treeTokenPathStep
	length uint8
	valid  bool
}

// buildTreeTokenPath derives the (probIndex, bit) sequence for the supplied
// (tree, token) pair using the iterative encoder helper. Stored once at
// init for each known (tree, token) pair the cost helpers consult.
func buildTreeTokenPath(tree []int16, token int) treeTokenPath {
	var out treeTokenPath
	var encoded vp8enc.TreeToken
	if !vp8enc.BuildTreeToken(tree, token, &encoded) {
		return out
	}
	if int(encoded.Len) == 0 || int(encoded.Len) > maxTreeTokenPathLen {
		return out
	}
	node := int16(0)
	treeLen := len(tree)
	for bitIndex := int(encoded.Len) - 1; bitIndex >= 0; bitIndex-- {
		nodeIdx := int(node)
		if nodeIdx < 0 || nodeIdx+1 >= treeLen {
			return out
		}
		bit := uint8((encoded.Value >> uint(bitIndex)) & 1)
		probIdx := nodeIdx >> 1
		if probIdx < 0 || probIdx > 255 {
			return out
		}
		out.steps[int(encoded.Len)-1-bitIndex] = treeTokenPathStep{
			probIndex: uint8(probIdx),
			bit:       bit,
		}
		next := tree[nodeIdx+int(bit)]
		if next <= 0 {
			if bitIndex != 0 {
				return out
			}
			out.length = encoded.Len
			out.valid = true
			return out
		}
		node = next
	}
	return out
}

// buildTreeTokenPaths fills out[token] with the precomputed path for each
// reachable token in [0, slots). Unreachable slots are left invalid; callers
// must validate path.valid before consuming entries. This permits sparse
// token-id ranges (e.g. SubMVRefTree, whose tokens are 10..13).
func buildTreeTokenPaths(tree []int16, slots int) []treeTokenPath {
	out := make([]treeTokenPath, slots)
	for i := range slots {
		out[i] = buildTreeTokenPath(tree, i)
	}
	return out
}

var (
	// VP8 mode trees consult tokens 0..N-1. CoefTree consults all 12
	// entropy tokens (including DCTEOBToken=11). SubMVRefTree's leaves
	// are token IDs 10..13 (Left4x4..New4x4) so the slot count must
	// cover the highest reachable token.
	keyFrameYModeTokenPaths = buildTreeTokenPaths(vp8tables.KeyFrameYModeTree[:], 5)
	yModeTokenPaths         = buildTreeTokenPaths(vp8tables.YModeTree[:], 5)
	uvModeTokenPaths        = buildTreeTokenPaths(vp8tables.UVModeTree[:], 4)
	bModeTokenPaths         = buildTreeTokenPaths(vp8tables.BModeTree[:], 10)
	coefTokenPaths          = buildTreeTokenPaths(vp8tables.CoefTree[:], vp8tables.MaxEntropyTokens)
	mbSplitTokenPaths       = buildTreeTokenPaths(vp8tables.MBSplitTree[:], 4)
	subMVRefTokenPaths      = buildTreeTokenPaths(vp8tables.SubMVRefTree[:], 14)
)

// Tree base pointers used by lookupTreeTokenPaths to dispatch by tree
// identity. Comparing slice data pointers avoids the cost of length-based
// or shape-based identification when the caller already passes a global
// fixed tree.
var (
	keyFrameYModeTreeBase = unsafe.SliceData(vp8tables.KeyFrameYModeTree[:])
	yModeTreeBase         = unsafe.SliceData(vp8tables.YModeTree[:])
	uvModeTreeBase        = unsafe.SliceData(vp8tables.UVModeTree[:])
	bModeTreeBase         = unsafe.SliceData(vp8tables.BModeTree[:])
	coefTreeBase          = unsafe.SliceData(vp8tables.CoefTree[:])
	mbSplitTreeBase       = unsafe.SliceData(vp8tables.MBSplitTree[:])
	subMVRefTreeBase      = unsafe.SliceData(vp8tables.SubMVRefTree[:])
)

// lookupTreeTokenPaths resolves the precomputed path table for a known
// fixed VP8 tree. Returns nil if the tree pointer is not one of the known
// fixed trees, in which case callers fall back to the slow walker.
//
//go:nosplit
func lookupTreeTokenPaths(tree []int16) []treeTokenPath {
	if len(tree) == 0 {
		return nil
	}
	base := unsafe.SliceData(tree)
	switch base {
	case coefTreeBase:
		return coefTokenPaths
	case keyFrameYModeTreeBase:
		return keyFrameYModeTokenPaths
	case yModeTreeBase:
		return yModeTokenPaths
	case uvModeTreeBase:
		return uvModeTokenPaths
	case bModeTreeBase:
		return bModeTokenPaths
	case mbSplitTreeBase:
		return mbSplitTokenPaths
	case subMVRefTreeBase:
		return subMVRefTokenPaths
	}
	return nil
}

// treeTokenCostFromPath sums boolBitCost over the precomputed path. The hot
// loop is a simple linear scan whose iteration count is fixed by the leaf
// depth (1..7 for mode trees, 2..6 for CoefTree). Bounds checks on probs[]
// are elided by the maxProbIdx hoist.
//
//go:nosplit
func treeTokenCostFromPath(path *treeTokenPath, probs []uint8) int {
	if !path.valid {
		return maxInt() / 4
	}
	probsLen := len(probs)
	length := int(path.length)
	cost := 0
	for i := range length {
		step := path.steps[i]
		probIdx := int(step.probIndex)
		if probIdx >= probsLen {
			return maxInt() / 4
		}
		// Branchless lookup keyed on the sign of step.bit: XOR with
		// -bit flips prob to 255-prob when the bit is set.
		cost += vp8tables.ProbCost[probs[probIdx]^uint8(-int(step.bit))]
	}
	return cost
}

// coefTokenCostFromPath is the CoefTree-specialized fast path. It hoists
// the probs[0..10] bounds check (the encoder always passes a length-11
// per-band probability array) so the inner loop has zero bounds checks.
//
//go:nosplit
func coefTokenCostFromPath(path *treeTokenPath, probs *[vp8tables.EntropyNodes]uint8) int {
	length := int(path.length)
	cost := 0
	probArr := probs
	for i := range length {
		step := path.steps[i]
		// Branchless: XOR prob with -bit (0x00 when bit=0, 0xFF when
		// bit=1) flips prob to 255-prob in the bit=1 arm without a
		// branch in the per-token-tree-edge loop.
		cost += vp8tables.ProbCost[probArr[step.probIndex]^uint8(-int(step.bit))]
	}
	return cost
}

// coefEOBTokenCost returns the entropy cost for emitting DCTEOBToken in the
// CoefTree given the active per-band probabilities. The EOB leaf sits at
// depth 1 (root, bit 0), so the cost collapses to a single ProbCost lookup
// and is worth inlining at the per-position trailing-EOB call site.
//
//go:nosplit
func coefEOBTokenCost(probs *[vp8tables.EntropyNodes]uint8) int {
	return vp8tables.ProbCost[probs[0]]
}

// coefZeroTokenCostElided returns the elided-band cost for emitting
// ZeroToken when libvpx's skip_eob_node optimization fires (pt == 0 and
// band > threshold). The non-elided branch is the EOB-vs-not bit at
// probs[0], so the elided cost is just the second tree edge at probs[1].
//
//go:nosplit
func coefZeroTokenCostElided(probs *[vp8tables.EntropyNodes]uint8) int {
	return vp8tables.ProbCost[probs[1]]
}

// coefZeroTokenCost returns the full ZeroToken cost (root + non-EOB branch).
// Used at the first encoded position of each plane and after a non-zero
// coefficient where the elision doesn't apply.
//
//go:nosplit
func coefZeroTokenCost(probs *[vp8tables.EntropyNodes]uint8) int {
	return vp8tables.ProbCost[255-int(probs[0])] + vp8tables.ProbCost[probs[1]]
}
