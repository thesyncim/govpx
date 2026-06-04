package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_partition_cost.go ports the partition-type RATE COST and the
// PARTITION_NONE/HORZ/VERT/SPLIT RDCOST comparison used by libvpx's
// full-RD square-partition recursion (rd_pick_partition in
// vp9/encoder/vp9_encodeframe.c). It is kept in its own file so the
// shared full-RD inter/key partition pickers are not edited.
//
// KEY LIBVPX FACT (the divergence this file fixes):
//
// The RD search accumulates the partition-token rate via the precomputed
// table cpi->partition_cost[pl][PARTITION_X], built once per frame in
// vp9/encoder/vp9_rd.c:430-432:
//
//	for (i = 0; i < PARTITION_CONTEXTS; ++i)
//	  vp9_cost_tokens(cpi->partition_cost[i], get_partition_probs(xd, i),
//	                  vp9_partition_tree);
//
// vp9_cost_tokens walks the FULL 4-leaf vp9_partition_tree
// (vp9/common/vp9_entropymode.c:262):
//
//	{ -PARTITION_NONE, 2, -PARTITION_HORZ, 4, -PARTITION_VERT,
//	  -PARTITION_SPLIT }
//
// so partition_cost[pl][type] is the UNCONDITIONAL tree cost, indexed only
// by (pl, partition type). rd_pick_partition adds it verbatim at
// vp9_encodeframe.c:3826 (NONE), :3969 (SPLIT), :4035 (HORZ), :4085
// (VERT) regardless of frame-edge conditions.
//
// The hasRows/hasCols-clamped form (one or two tree bits) is a property of
// the BITSTREAM WRITER (write_partition in vp9_bitstream.c), NOT of the RD
// rate cost. Mixing the writer's clamped form into the RD accumulation
// understates the rate of HORZ/VERT/SPLIT at frame edges and changes the
// PARTITION_NONE vs split RDCOST comparison.

// RDPartitionCost returns the full-RD partition-token rate cost for the
// supplied partition-plane context and partition type, mirroring
// cpi->partition_cost[ctx][partition].
//
// It is the verbatim vp9_cost_tokens(vp9_partition_tree) cost: it does NOT
// clamp on frame-edge (hasRows/hasCols) conditions. Use this — not the
// writer's PartitionRateCost — inside the full-RD partition RDCOST
// accumulation.
//
// libvpx: vp9/encoder/vp9_rd.c:430-432 (table build),
// vp9/encoder/vp9_cost.c:56 (vp9_cost_tokens),
// vp9/common/vp9_entropymode.c:262 (vp9_partition_tree).
func RDPartitionCost(
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	ctx int, partition common.PartitionType,
) int {
	if partitionProbs == nil || ctx < 0 || ctx >= common.PartitionContexts {
		return 0
	}
	if partition < common.PartitionNone || partition >= common.PartitionTypes {
		return 0
	}
	var costs [common.PartitionTypes]int
	probs := partitionProbs[ctx]
	encoder.VP9CostTokens(costs[:], probs[:], common.PartitionTree[:])
	return costs[partition]
}

// VP9RDCost expands libvpx's RDCOST macro (vp9/encoder/vp9_rd.h:29-30):
//
//	RDCOST(RM, DM, R, D) =
//	  ROUND_POWER_OF_TWO((int64_t)R * RM, VP9_PROB_COST_SHIFT) + (D << DM)
//
// with DM == RDDIV_BITS == 7 and VP9_PROB_COST_SHIFT == 9. Provided so the
// full-RD partition comparison can be pinned against libvpx values without
// reaching into the shared encoder helpers.
func VP9RDCost(rdmult int, rate int, distortion uint64) uint64 {
	return encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion)
}

// vp9RDPartitionBetter reports whether candidate RDCOST `cand` should
// replace the running `best`, mirroring rd_pick_partition's
// "this_rdc.rdcost < best_rdc.rdcost" / "sum_rdc.rdcost < best_rdc.rdcost"
// strict-less comparison (vp9_encodeframe.c:3829, :3973). The first
// candidate (bestSet == false) always wins.
//
// libvpx breaks ties by keeping the earlier (PARTITION_NONE-first) winner,
// i.e. it does NOT replace best on equal rdcost.
func vp9RDPartitionBetter(bestSet bool, best, cand uint64) bool {
	if !bestSet {
		return true
	}
	return cand < best
}
