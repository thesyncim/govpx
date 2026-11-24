package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// VP9 tree-distribution conversion + multi-leaf prob update writer.
// Ported from libvpx v1.16.0:
//   - vp9/encoder/vp9_treewriter.c — vp9_tree_probs_from_distribution
//     and its convert_distribution recursive helper.
//   - vp9/encoder/vp9_bitstream.c — the static prob_diff_update wrapper
//     that bridges (tree, counts) → per-branch cond_prob_diff_update
//     emits.
//
// Together these turn per-leaf event counts (frame_counts.*) into the
// per-branch left/right pair the savings-search-driven cond update
// needs, then walk the tree's internal nodes emitting either an
// update or a no-update bit for each branch slot.

// TreeProbsFromDistribution mirrors libvpx's
// vp9_tree_probs_from_distribution. Recursively walks `tree`
// (positive entries = internal-node indices, non-positive = leaf
// index ↦ -entry), accumulating event counts from `numEvents`
// into per-branch (left, right) pairs stored in `branchCt`. The
// caller sizes branchCt to N-1 entries where N is the number of
// leaves.
func TreeProbsFromDistribution(tree []int8,
	branchCt [][2]uint32, numEvents []uint32,
) {
	convertDistribution(0, tree, branchCt, numEvents)
}

// convertDistribution is the recursive helper invoked from
// TreeProbsFromDistribution. Returns the total event count for the
// subtree rooted at the node pair (tree[i], tree[i+1]).
func convertDistribution(i int, tree []int8,
	branchCt [][2]uint32, numEvents []uint32,
) uint32 {
	var left, right uint32
	if tree[i] <= 0 {
		left = numEvents[-tree[i]]
	} else {
		left = convertDistribution(int(tree[i]), tree, branchCt, numEvents)
	}
	if tree[i+1] <= 0 {
		right = numEvents[-tree[i+1]]
	} else {
		right = convertDistribution(int(tree[i+1]), tree, branchCt, numEvents)
	}
	branchCt[i>>1] = [2]uint32{left, right}
	return left + right
}

// ProbDiffUpdateForTree mirrors libvpx's static prob_diff_update —
// the per-tree wrapper that bridges (counts → branch counts →
// per-branch cond_prob_diff_update emit). The `probs` slice holds
// the N-1 probability slots for the tree's internal nodes; each is
// updated in place when the savings search picks an update.
//
// `tree` is the binary tree (PartitionTree, IntraModeTree, etc.).
// `probs` is the matching probability row (caller picks the row
// from the frame context). `counts` is the per-leaf event counter
// row. `branchCtScratch` is a caller-owned scratch slice sized at
// least len(probs); kept callable to stay zero-alloc on the hot
// path.
func ProbDiffUpdateForTree(bw *bitstream.Writer, tree []int8,
	probs []uint8, counts []uint32, branchCtScratch [][2]uint32,
) {
	TreeProbsFromDistribution(tree, branchCtScratch[:len(probs)], counts)
	for i := range probs {
		CondProbDiffUpdateFromCounts(bw, &probs[i], branchCtScratch[i])
	}
}
