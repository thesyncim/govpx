package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 counts-driven compressed-header prob updates. Ported from
// libvpx v1.16.0 vp9/encoder/vp9_bitstream.c — the per-section
// update walkers (update_skip_probs, update_inter_mode_probs,
// update_switchable_interp_probs, write_partition_probs, etc.) that
// feed frame_counts.* through prob_diff_update / vp9_cond_prob_diff_update.
//
// Each helper here mirrors the matching libvpx walker exactly: same
// iteration order, same prob slots, same counts shape. The
// existing WriteCompressedHeaderNoUpdate writer falls back to all-
// zero "update?" bits per slot; these helpers replace that fallback
// when the caller has real per-frame counters and wants byte-parity
// updates.

// WriteSkipProbsFromCounts mirrors libvpx's update_skip_probs.
// Walks the 3 skip contexts and runs the savings-search cond-update
// for each against the (skip=0, skip=1) counter pair.
func WriteSkipProbsFromCounts(bw *bitstream.Writer,
	probs *[3]uint8, counts *[3][2]uint32,
) {
	for i := range counts {
		CondProbDiffUpdateFromCounts(bw, &probs[i], counts[i])
	}
}

// WriteIntraInterProbsFromCounts mirrors libvpx's
// update_intra_inter_probs callsite. Walks the 4 intra/inter
// contexts and runs the cond-update against the (intra, inter)
// counter pair.
func WriteIntraInterProbsFromCounts(bw *bitstream.Writer,
	probs *[common.IntraInterContexts]uint8,
	counts *[common.IntraInterContexts][2]uint32,
) {
	for i := range counts {
		CondProbDiffUpdateFromCounts(bw, &probs[i], counts[i])
	}
}

// WriteInterModeProbsFromCounts mirrors libvpx's
// update_inter_mode_probs. Walks the 7 inter-mode contexts and
// runs ProbDiffUpdateForTree against InterModeTree for each
// context's per-mode event counter. The 3 per-context branch
// slots scratch lives in `scratch[:]`.
func WriteInterModeProbsFromCounts(bw *bitstream.Writer,
	probs *[common.InterModeContexts][common.InterModes - 1]uint8,
	counts *[common.InterModeContexts][common.InterModes]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.InterModeTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// WriteSwitchableInterpProbsFromCounts mirrors libvpx's
// update_switchable_interp_probs. One ProbDiffUpdateForTree per
// switchable-filter context, walking SwitchableInterpTree.
func WriteSwitchableInterpProbsFromCounts(bw *bitstream.Writer,
	probs *[vp9dec.SwitchableFilterContexts][vp9dec.SwitchableFilters - 1]uint8,
	counts *[vp9dec.SwitchableFilterContexts][vp9dec.SwitchableFilters]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.SwitchableInterpTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// WritePartitionProbsFromCounts mirrors libvpx's
// write_partition_probs. One ProbDiffUpdateForTree per partition
// context, walking PartitionTree.
func WritePartitionProbsFromCounts(bw *bitstream.Writer,
	probs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	counts *[common.PartitionContexts][common.PartitionTypes]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.PartitionTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// WriteYModeProbsFromCounts mirrors libvpx's update_y_mode_probs
// callsite — one ProbDiffUpdateForTree per BlockSizeGroups row,
// walking IntraModeTree (the 9-node Y-mode tree).
func WriteYModeProbsFromCounts(bw *bitstream.Writer,
	probs *[vp9dec.BlockSizeGroups][common.IntraModes - 1]uint8,
	counts *[vp9dec.BlockSizeGroups][common.IntraModes]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.IntraModeTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}
