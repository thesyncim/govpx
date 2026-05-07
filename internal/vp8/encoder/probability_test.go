package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestBuildKeyFrameCoefficientProbabilityUpdates(t *testing.T) {
	const rows, cols = 16, 16
	modes := make([]KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	above := make([]TokenContextPlanes, cols)

	frameProbs, updates, err := BuildKeyFrameCoefficientProbabilityUpdates(rows, cols, modes, coeffs, above, &tables.DefaultCoefProbs)
	if err != nil {
		t.Fatalf("BuildKeyFrameCoefficientProbabilityUpdates returned error: %v", err)
	}

	if updates.UpdateCount == 0 {
		t.Fatalf("UpdateCount = 0, want EOB-heavy coefficient grid to update probabilities")
	}
	if frameProbs == tables.DefaultCoefProbs {
		t.Fatalf("frame probabilities equal defaults, want updated probabilities")
	}
}

// TestIndependentCoefContextSavingsHandComputed pins the independent-context
// new-probability output to the libvpx integer formula taken from
// vp8/common/treecoder.c vp8_tree_probs_from_distribution (Pfactor=256,
// Round=1):
//
//	tot = c[0] + c[1]
//	p   = (c[0]*256 + tot/2) / tot          # rounded toward nearest
//	prob = clamp(p, 1, 255)                 # 0 -> 1, >255 -> 255
//
// In the independent (VPX_ERROR_RESILIENT_PARTITIONS) path libvpx sums the
// per-prev-context branch counts before applying the formula
// (bitstream.c:686-711, sum_probs_over_prev_coef_context). We populate the
// branch counts so a single (block_type=0, band=0, node=0) sees three
// contexts with branch counts (10,30), (40,20), (50,50). Summed they are
// (100,100), giving p = (100*256 + 100) / 200 = 25700/200 = 128. The
// independent output therefore emits prob = 128 across all k for that node
// (and the matching node in every block_type/band that has no counts will
// see total==0, returning vp8_prob_half=128 from
// coefficientProbabilityFromBranchCount).
func TestIndependentCoefContextSavingsHandComputed(t *testing.T) {
	var counts coefficientBranchCounts
	// Node 0 of (block 0, band 0) has the controlled distribution.
	counts[0][0][0][0] = [2]int{10, 30}
	counts[0][0][1][0] = [2]int{40, 20}
	counts[0][0][2][0] = [2]int{50, 50}

	// Pick a base where every k=0..2 has oldp == 1 for node 0 of (0,0)
	// so newp_summed=128 != oldp. That ensures aggregate savings>0 and
	// the independent path emits an update for all three k contexts.
	var base tables.CoefficientProbs
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					base[block][band][ctx][node] = 128
				}
			}
		}
	}
	base[0][0][0][0] = 1
	base[0][0][1][0] = 1
	base[0][0][2][0] = 1

	frameProbs, updates, err := coefficientProbabilityUpdatesFromCountsIndependent(&base, &counts, false)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCountsIndependent: %v", err)
	}

	const wantNew uint8 = 128 // (100*256 + 100) / 200 = 128.
	for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
		if got := frameProbs[0][0][ctx][0]; got != wantNew {
			t.Fatalf("frameProbs[0][0][%d][0] = %d, want %d (libvpx (100*256+100)/200)", ctx, got, wantNew)
		}
		if !updates.Update[0][0][ctx][0] {
			t.Fatalf("updates.Update[0][0][%d][0] = false, want true (aggregate savings>0)", ctx)
		}
		if got := updates.Probs[0][0][ctx][0]; got != wantNew {
			t.Fatalf("updates.Probs[0][0][%d][0] = %d, want %d", ctx, got, wantNew)
		}
	}

	// Sanity-check the per-context summed branch count in case the
	// helper is ever changed. tot = 200, c[0] = 100, formula constants
	// match libvpx Pfactor=256/Round=1.
	const c0, tot = 100, 200
	p := (c0*256 + tot/2) / tot
	if p != int(wantNew) {
		t.Fatalf("hand formula reproduction = %d, want %d", p, wantNew)
	}
}

// TestIndependentCoefContextDivergesFromDefault exercises the divergence
// between the default `default_coef_context_savings` path and the
// `independent_coef_context_savings` path: when only one prev-coef context
// has observed counts, the default path can update only that single k while
// the independent path sums savings across k and applies the shared new
// prob to every k. We construct a counts vector with entries solely at
// k=0 and a base where k=1, k=2 hold a starkly different oldp; the
// independent path should set Update[k=1] / Update[k=2] for the affected
// node, the default path should not.
func TestIndependentCoefContextDivergesFromDefault(t *testing.T) {
	var counts coefficientBranchCounts
	// Strong (1,0) bias only at k=0 of (block=2, band=3, node=2).
	// (200, 0) → newp_summed = (200*256 + 100)/200 = 256 → clamped 255.
	counts[2][3][0][2] = [2]int{200, 0}

	var base tables.CoefficientProbs
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					base[block][band][ctx][node] = 128
				}
			}
		}
	}
	// Make k=0 already match newp (saves no bits there); make k=1, k=2
	// far from newp so summed savings carry the update for all k.
	base[2][3][0][2] = 255
	base[2][3][1][2] = 1
	base[2][3][2][2] = 1

	_, defaultUpdates, err := coefficientProbabilityUpdatesFromCounts(&base, &counts)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCounts: %v", err)
	}
	_, indepUpdates, err := coefficientProbabilityUpdatesFromCountsIndependent(&base, &counts, false)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCountsIndependent: %v", err)
	}

	// Default path: k=0 sees (200,0) but oldp==newp==255 → skipped.
	// k=1, k=2 have total=0 in counts → skipped. UpdateCount=0.
	if defaultUpdates.UpdateCount != 0 {
		t.Fatalf("default path UpdateCount = %d, want 0 (k=0 oldp==newp, k=1/k=2 zero counts)", defaultUpdates.UpdateCount)
	}

	// Independent path: aggregate savings at node 2 of (2,3) sum
	// across k. k=0 contributes 0 (oldp==newp==255). k=1, k=2 contribute
	// large positives because oldp=1 vs. newp=255 with summed_ct=(200,0)
	// is a huge cost win. The independent path therefore updates k=1
	// and k=2 to 255.
	if !indepUpdates.Update[2][3][1][2] {
		t.Fatalf("independent path did not update [block=2][band=3][k=1][node=2]")
	}
	if !indepUpdates.Update[2][3][2][2] {
		t.Fatalf("independent path did not update [block=2][band=3][k=2][node=2]")
	}
	if got := indepUpdates.Probs[2][3][1][2]; got != 255 {
		t.Fatalf("independent path prob[2][3][1][2] = %d, want 255", got)
	}
	if got := indepUpdates.Probs[2][3][2][2]; got != 255 {
		t.Fatalf("independent path prob[2][3][2][2] = %d, want 255", got)
	}
	if indepUpdates.UpdateCount == defaultUpdates.UpdateCount {
		t.Fatalf("independent UpdateCount (%d) matches default (%d), want divergence", indepUpdates.UpdateCount, defaultUpdates.UpdateCount)
	}
}

// TestIndependentCoefContextKeyFrameForcesEqualization pins the
// libvpx VPX_ERROR_RESILIENT_PARTITIONS key-frame branch
// (bitstream.c:924-928): on key frames each k forces u=1 whenever the
// shared new prob differs from that k's old prob, even when aggregate
// savings would not justify an update. With zero counts, the shared new
// prob is vp8_prob_half (128) per
// vp8_tree_probs_from_distribution's tot==0 branch.
func TestIndependentCoefContextKeyFrameForcesEqualization(t *testing.T) {
	var counts coefficientBranchCounts // all zeros: total==0 → newp=128

	var base tables.CoefficientProbs
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					base[block][band][ctx][node] = 128
				}
			}
		}
	}
	// Force a single divergent k for one node.
	base[1][2][0][3] = 64

	_, updates, err := coefficientProbabilityUpdatesFromCountsIndependent(&base, &counts, true)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCountsIndependent: %v", err)
	}

	if !updates.Update[1][2][0][3] {
		t.Fatalf("key-frame independent path did not force-update divergent k=0 (newp=128, oldp=64)")
	}
	if got := updates.Probs[1][2][0][3]; got != 128 {
		t.Fatalf("key-frame independent path prob[1][2][0][3] = %d, want 128", got)
	}
	// The k=1, k=2 contexts already match newp=128 so libvpx's
	// per-(k,t) key-frame force does NOT fire there (and aggregate
	// savings is non-positive because only the k=0 contribution
	// counts and 8-bit literal cost > 0).
	if updates.Update[1][2][1][3] || updates.Update[1][2][2][3] {
		t.Fatalf("key-frame independent path force-updated matching k contexts")
	}
}

func TestWriteCoefficientKeyFrameEmitsCoefficientProbabilityUpdates(t *testing.T) {
	const rows, cols = 16, 16
	modes := make([]KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	above := make([]TokenContextPlanes, cols)
	packet := make([]byte, 65536)

	n, err := WriteCoefficientKeyFrame(packet, cols*16, rows*16, KeyFrameStateConfig{BaseQIndex: 20}, modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientKeyFrame returned error: %v", err)
	}

	coefProbs := tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Probability.UpdateCount == 0 {
		t.Fatalf("state coefficient update count = 0, want emitted updates")
	}
	if coefProbs == tables.DefaultCoefProbs {
		t.Fatalf("parsed coefficient probabilities equal defaults, want updates applied")
	}
}
