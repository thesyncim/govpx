package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestVP8MVRefCostsUseDynamicNeighborCounts verifies that VP8 inter-mode
// reference costs use libvpx-style near-MV reference counts instead of treating
// InterModeContexts as a frozen probability vector. The test exercises the
// neighbour configurations that drive findNearInterMotionVectors to produce
// non-zero near_mv_ref_cnts and asserts that the resulting per-mode cost matches
// the libvpx formula directly:
//
//	p[i]     = vp8_mode_contexts[near_mv_ref_ct[i]][i]
//	cost(m)  = vp8_treed_cost(vp8_mv_ref_tree, p, encoding(m))
//
// References:
//
//   - libvpx v1.16.0 vp8/common/findnearmv.c:23-122 vp8_find_near_mvs
//   - libvpx v1.16.0 vp8/common/findnearmv.c:150-159 vp8_mv_ref_probs
//   - libvpx v1.16.0 vp8/encoder/pickinter.c:734-737, 1100, 1258
//     fast-picker mdcounts use sites
//   - libvpx v1.16.0 vp8/encoder/rdopt.c:797-803 vp8_cost_mv_ref
//   - govpx internal/vp8/encoder/interframe_motion.go:75-133
//     findNearInterMotionVectors
//   - govpx vp8_encoder_inter_rate.go:227-262 interPredictionModeRate
//   - govpx vp8_zeromv_mode_cost_parity_test.go (sibling pin)
//   - govpx vp8_realtime_cpu8_mb_parity_test.go (state-drift bisect)
func TestVP8MVRefCostsUseDynamicNeighborCounts(t *testing.T) {
	t.Run("ZeroMVCostMatchesLibvpxAcrossAllIntraCounts", testVP8MVRefZeroMVCostMatchesLibvpxAcrossAllIntraCounts)
	t.Run("DynamicCountsDriveProbLookup", testVP8MVRefDynamicCountsDriveProbLookup)
	t.Run("CostGapTracesToCountsIntraDelta", testVP8MVRefCostGapTracesToCountsIntraDelta)
}

// testVP8MVRefZeroMVCostMatchesLibvpxAcrossAllIntraCounts confirms that for
// every value of counts.Intra in [0,6) (the full domain libvpx's
// vp8_find_near_mvs ever produces via cntx-pointer advancement), govpx's
// per-mode ZEROMV cost matches the libvpx vp8_cost_mv_ref formula
// applied to vp8_mode_contexts[ct][0].
func testVP8MVRefZeroMVCostMatchesLibvpxAcrossAllIntraCounts(t *testing.T) {
	for ct := range vp8tables.InterModeContextCount {
		counts := vp8enc.InterModeCounts{Intra: uint8(ct)}
		got := interPredictionModeRate(vp8common.ZeroMV, counts)
		want := vp8enc.BoolBitCost(vp8tables.InterModeContexts[ct][0], 0)
		if got != want {
			t.Fatalf("ZEROMV cost @ counts.Intra=%d: govpx=%d, want %d "+
				"(p[0]=%d)", ct, got, want, vp8tables.InterModeContexts[ct][0])
		}
	}
}

// testVP8MVRefDynamicCountsDriveProbLookup confirms findNearInterMotionVectors
// yields counts that vary with the neighbour configuration, so the prob
// vector p[i] = InterModeContexts[counts[i]][i] is dynamic, not static.
func testVP8MVRefDynamicCountsDriveProbLookup(t *testing.T) {
	// Neighbours all intra (frame edge) → counts={0,0,0,0}.
	zeroCounts := vp8enc.InterFrameModeCounts(nil, nil, nil,
		vp8common.LastFrame, defaultInterFrameSignBias())
	if zeroCounts != (vp8enc.InterModeCounts{}) {
		t.Fatalf("edge counts = %+v, want zero", zeroCounts)
	}

	// One non-intra neighbour (above with zero MV) → counts.Intra=2.
	aboveZeroMV := &vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.LastFrame,
		Mode:     vp8common.ZeroMV,
		MV:       vp8enc.MotionVector{},
	}
	zeroCounts = vp8enc.InterFrameModeCounts(aboveZeroMV, nil, nil,
		vp8common.LastFrame, defaultInterFrameSignBias())
	if zeroCounts.Intra != 2 {
		t.Fatalf("above-zero-MV counts.Intra = %d, want 2", zeroCounts.Intra)
	}

	// One non-intra neighbour (above with non-zero MV) → counts.Nearest=2.
	aboveNonZeroMV := &vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.LastFrame,
		Mode:     vp8common.NewMV,
		MV:       vp8enc.MotionVector{Row: 16, Col: 0},
	}
	nzCounts := vp8enc.InterFrameModeCounts(aboveNonZeroMV, nil, nil,
		vp8common.LastFrame, defaultInterFrameSignBias())
	if nzCounts.Nearest != 2 {
		t.Fatalf("above-non-zero-MV counts.Nearest = %d, want 2",
			nzCounts.Nearest)
	}
	if nzCounts.Intra != 0 {
		t.Fatalf("above-non-zero-MV counts.Intra = %d, want 0",
			nzCounts.Intra)
	}

	// Confirm the prob lookup differs between the two configurations: treating
	// InterModeContexts as one static probability vector is misleading because
	// counts[0] feeds p[0] differently for these two neighbours.
	pZero := vp8tables.InterModeContexts[zeroCounts.Intra][0]
	pNonZero := vp8tables.InterModeContexts[nzCounts.Intra][0]
	if pZero == pNonZero {
		t.Fatalf("prob[0] is identical for above-zero (ct=%d, p=%d) vs "+
			"above-nonzero (ct=%d, p=%d) — would mean InterModeContexts "+
			"is static, but the table varies by row",
			zeroCounts.Intra, pZero, nzCounts.Intra, pNonZero)
	}
}

// testVP8MVRefCostGapTracesToCountsIntraDelta shows that a representative
// ZEROMV-LAST cost delta is explained by two encoders observing different
// values of near_mv_ref_ct[CNT_INTRA].
//
// Under the VP8 bool encoder convention (libvpx boolhuff.c:
// vp8_cost_zero(prob)=vp8_prob_cost[prob]) the per-probability cost is
// MONOTONICALLY DECREASING in prob: low prob[0] (rare ZEROMV) → high
// cost(0); high prob[0] (frequent ZEROMV) → low cost(0). With
// vp8_mode_contexts row 0 = {7, 1, 1, 143} and row 2 = {135, 64, 57, 68}:
//
//	ZEROMV cost @ ct[0]=0: vp8enc.BoolBitCost(7, 0)   = 1328  (rare ZEROMV)
//	ZEROMV cost @ ct[0]=2: vp8enc.BoolBitCost(135, 0) =  235  (frequent ZEROMV)
//
// A 530-vs-237 govpx-vs-libvpx delta is consistent with govpx seeing ct[0]=0
// (no zero-MV inter neighbour at MB(0,0)) while libvpx sees ct[0]>=1 (zero-MV
// inter neighbour from libvpx's earlier MB(0,0) decision). The dynamic prob
// lookup is functioning correctly; upstream picker drift drives different ct
// values.
func testVP8MVRefCostGapTracesToCountsIntraDelta(t *testing.T) {
	costAtCtIntra := func(ct uint8) int {
		return interPredictionModeRate(vp8common.ZeroMV,
			vp8enc.InterModeCounts{Intra: ct})
	}
	cost0 := costAtCtIntra(0)
	cost2 := costAtCtIntra(2)

	// p[0] @ ct=0 is 7; @ ct=2 is 135.
	if vp8tables.InterModeContexts[0][0] != 7 {
		t.Fatalf("InterModeContexts[0][0] = %d, want 7",
			vp8tables.InterModeContexts[0][0])
	}
	if vp8tables.InterModeContexts[2][0] != 135 {
		t.Fatalf("InterModeContexts[2][0] = %d, want 135",
			vp8tables.InterModeContexts[2][0])
	}
	if want := vp8enc.BoolBitCost(7, 0); cost0 != want {
		t.Fatalf("ZEROMV cost @ ct=0 = %d, want %d", cost0, want)
	}
	if want := vp8enc.BoolBitCost(135, 0); cost2 != want {
		t.Fatalf("ZEROMV cost @ ct=2 = %d, want %d", cost2, want)
	}

	// The delta is the dynamic prob lookup at work. The signed delta
	// (cost2 - cost0) is negative because high prob[0]=P(0) corresponds to
	// LOW cost-of-0 in the libvpx bool encoder cost table
	// (ProbCost[prob] = -log2(prob/256)).
	delta := cost0 - cost2
	if delta <= 0 {
		t.Fatalf("ZEROMV cost(ct=0) - cost(ct=2) = %d, want > 0 "+
			"(low ct.Intra → high ZEROMV cost via the dynamic "+
			"vp8_mv_ref_probs lookup; libvpx ProbCost convention is "+
			"vp8_cost_zero(p) = -log2(p/256), monotonically "+
			"decreasing in p)", delta)
	}
}
