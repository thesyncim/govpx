package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestRDPartitionCostMatchesLibvpx pins the full-RD partition-token rate cost
// (cpi->partition_cost[pl][PARTITION_X]) against verbatim libvpx v1.16.0
// values.
//
// Expected values produced by vp9_cost_tokens(costs, probs, vp9_partition_tree)
// — vp9/encoder/vp9_cost.c:56, walking vp9/common/vp9_entropymode.c:262
//
//	{ -PARTITION_NONE, 2, -PARTITION_HORZ, 4, -PARTITION_VERT,
//	  -PARTITION_SPLIT }
//
// over vp9_prob_cost (vp9/encoder/vp9_cost.c:18). Reproduced by extracting
// vp9_cost_tokens + vp9_prob_cost + vp9_partition_tree verbatim into a
// standalone C harness and printing the four leaf costs for two contexts
// taken from default_partition_probs (vp9/common/vp9_entropymode.c):
//
//	ctx 0  {199,122,141} (8x8, a/l both not split):
//	  NONE=186 HORZ=1657 VERT=2029 SPLIT=2179
//	ctx 15 {10,7,6}      (64x64, a/l both split):
//	  NONE=2395 HORZ=2688 VERT=2821 SPLIT=67
func TestRDPartitionCostMatchesLibvpx(t *testing.T) {
	var probs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	probs[0] = [3]uint8{199, 122, 141} // default_partition_probs ctx 0
	probs[15] = [3]uint8{10, 7, 6}     // default_partition_probs ctx 15

	cases := []struct {
		ctx       int
		partition common.PartitionType
		want      int
	}{
		{0, common.PartitionNone, 186},
		{0, common.PartitionHorz, 1657},
		{0, common.PartitionVert, 2029},
		{0, common.PartitionSplit, 2179},
		{15, common.PartitionNone, 2395},
		{15, common.PartitionHorz, 2688},
		{15, common.PartitionVert, 2821},
		{15, common.PartitionSplit, 67},
	}
	for _, tc := range cases {
		got := RDPartitionCost(&probs, tc.ctx, tc.partition)
		if got != tc.want {
			t.Errorf("RDPartitionCost(ctx=%d, part=%d) = %d, want %d (libvpx)",
				tc.ctx, tc.partition, got, tc.want)
		}
	}
}

// TestRDPartitionCostIsUnconditional confirms the full-RD cost does NOT clamp
// on frame-edge conditions: HORZ/VERT/SPLIT carry their full multi-bit tree
// cost regardless of position, unlike the bitstream writer's clamped form.
// This is the divergence vp9_fullrd_partition_cost.go fixes. At ctx 0 the
// writer's clamped !hasRows form for HORZ would be cost_bit(probs[2],0) = 414,
// whereas the RD cost is the full 1657.
func TestRDPartitionCostIsUnconditional(t *testing.T) {
	var probs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	probs[0] = [3]uint8{199, 122, 141}
	if got := RDPartitionCost(&probs, 0, common.PartitionHorz); got != 1657 {
		t.Fatalf("RDPartitionCost HORZ ctx0 = %d, want 1657 (full tree)", got)
	}
}

// TestVP9RDCostPartitionComparison pins the PARTITION_NONE vs PARTITION_SPLIT
// RDCOST comparison against verbatim libvpx RDCOST macro output
// (vp9/encoder/vp9_rd.h:29-30) with rdmult=300, RDDIV_BITS=7,
// VP9_PROB_COST_SHIFT=9:
//
//	RDCOST(300, 7, 5000,  120000) = 15362930  (NONE)
//	RDCOST(300, 7, 8000,   90000) = 11524688  (SPLIT)
//
// so the split RDCOST is the smaller and rd_pick_partition selects SPLIT.
// Values produced by a standalone C harness using the verbatim RDCOST /
// ROUND_POWER_OF_TWO macros.
func TestVP9RDCostPartitionComparison(t *testing.T) {
	const rdmult = 300
	none := VP9RDCost(rdmult, 5000, 120000)
	split := VP9RDCost(rdmult, 8000, 90000)
	if none != 15362930 {
		t.Errorf("VP9RDCost(NONE) = %d, want 15362930 (libvpx)", none)
	}
	if split != 11524688 {
		t.Errorf("VP9RDCost(SPLIT) = %d, want 11524688 (libvpx)", split)
	}

	// rd_pick_partition keeps the running best on strict-less; PARTITION_NONE
	// is considered first, then SPLIT replaces it because split < none.
	best := none
	bestSet := true
	if vp9RDPartitionBetter(bestSet, best, split) {
		best = split
	}
	if best != split {
		t.Errorf("partition compare picked rdcost=%d, want SPLIT=%d", best, split)
	}

	// Tie does not replace the earlier (NONE-first) winner.
	if vp9RDPartitionBetter(true, none, none) {
		t.Error("vp9RDPartitionBetter replaced best on equal rdcost; libvpx keeps earlier winner")
	}
	// First candidate always wins.
	if !vp9RDPartitionBetter(false, 0, none) {
		t.Error("vp9RDPartitionBetter rejected first candidate")
	}
}
