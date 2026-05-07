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

func TestIndependentCoefContextEntropySavingsMatchesPositiveUpdates(t *testing.T) {
	var counts coefficientBranchCounts
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
	base[2][3][0][2] = 255
	base[2][3][1][2] = 1
	base[2][3][2][2] = 1

	_, updates, err := coefficientProbabilityUpdatesFromCountsIndependent(&base, &counts, false)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCountsIndependent: %v", err)
	}
	savings := coefficientEntropySavingsFromCountsIndependent(&base, &counts, false)

	if updates.UpdateCount == 0 {
		t.Fatalf("independent path UpdateCount = 0, want positive updates")
	}
	if savings <= 0 {
		t.Fatalf("independent entropy savings = %d, want positive savings matching update decision", savings)
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

func TestKeyFrameIndependentCoefUpdatesUseDefaultCounts(t *testing.T) {
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	zeroCoeffs := []MacroblockCoefficients{{}}
	contentCoeffs := []MacroblockCoefficients{{}}
	contentCoeffs[0].QCoeff[24][0] = 1
	contentCoeffs[0].QCoeff[0][1] = 2
	contentCoeffs[0].QCoeff[16][0] = -3

	zeroProbs, zeroUpdates, err := BuildKeyFrameCoefficientProbabilityUpdatesIndependent(1, 1, modes, zeroCoeffs, make([]TokenContextPlanes, 1), &tables.DefaultCoefProbs)
	if err != nil {
		t.Fatalf("BuildKeyFrameCoefficientProbabilityUpdatesIndependent zero: %v", err)
	}
	contentProbs, contentUpdates, err := BuildKeyFrameCoefficientProbabilityUpdatesIndependent(1, 1, modes, contentCoeffs, make([]TokenContextPlanes, 1), &tables.DefaultCoefProbs)
	if err != nil {
		t.Fatalf("BuildKeyFrameCoefficientProbabilityUpdatesIndependent content: %v", err)
	}
	if zeroProbs != contentProbs || zeroUpdates != contentUpdates {
		t.Fatalf("independent key-frame coef updates changed with content; zero updates=%d content updates=%d",
			zeroUpdates.UpdateCount, contentUpdates.UpdateCount)
	}

	for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
		if got, want := zeroProbs[0][1][ctx][0], uint8(248); got != want {
			t.Fatalf("default-count prob[0][1][%d][0] = %d, want %d", ctx, got, want)
		}
		if !zeroUpdates.Update[0][1][ctx][0] || zeroUpdates.Probs[0][1][ctx][0] != 248 {
			t.Fatalf("default-count update[0][1][%d][0] = %t prob=%d, want forced prob 248",
				ctx, zeroUpdates.Update[0][1][ctx][0], zeroUpdates.Probs[0][1][ctx][0])
		}
	}

	zeroSavings, err := KeyFrameCoefficientEntropySavingsIndependent(1, 1, modes, zeroCoeffs, make([]TokenContextPlanes, 1), &tables.DefaultCoefProbs)
	if err != nil {
		t.Fatalf("KeyFrameCoefficientEntropySavingsIndependent zero: %v", err)
	}
	contentSavings, err := KeyFrameCoefficientEntropySavingsIndependent(1, 1, modes, contentCoeffs, make([]TokenContextPlanes, 1), &tables.DefaultCoefProbs)
	if err != nil {
		t.Fatalf("KeyFrameCoefficientEntropySavingsIndependent content: %v", err)
	}
	if zeroSavings != contentSavings {
		t.Fatalf("independent key-frame entropy savings changed with content: zero=%d content=%d", zeroSavings, contentSavings)
	}
}

// TestDefaultCoefContextKeyFrameMatchesLibvpxNoForce pins the libvpx
// default-path (non-error-resilient) coef-prob update behaviour for key
// frames: vp8_update_coef_probs only sets u=1 when prob_update_savings>0,
// and the per-(k,t) "force when newp != *Pold on key frames" branch at
// bitstream.c:920-928 is gated on VPX_ERROR_RESILIENT_PARTITIONS, so it does
// NOT fire on the default path. That force IS exercised in the Independent
// variant, which mirrors the error-resilient branch.
//
// We construct a counts vector with a single (block, band, ctx, node)
// populated at ct=(1,0) so the new probability resolves to 255 against an
// oldp=128 base. With only one observed branch the entropy savings are
// dominated by the 8-bit literal cost and are non-positive; the default
// builder for both inter and key frames must therefore skip the update,
// while the Independent builder run with keyFrame=true must force it.
func TestDefaultCoefContextKeyFrameMatchesLibvpxNoForce(t *testing.T) {
	const blk, bnd, k, n = 1, 2, 1, 3

	var counts coefficientBranchCounts
	counts[blk][bnd][k][n] = [2]int{1, 0}

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

	// Sanity-check the construction: newp = clamp((1*256+0)/1) = 255 and
	// the savings rule alone rejects the update because the 8-bit literal
	// dominates the tiny per-token cost difference for ct=(1,0).
	ct := counts[blk][bnd][k][n]
	const oldProb uint8 = 128
	newProb := coefficientProbabilityFromBranchCount(ct)
	if newProb == oldProb {
		t.Fatalf("setup: newProb (%d) must differ from oldProb (%d)", newProb, oldProb)
	}
	updateProb := tables.CoefUpdateProbs[blk][bnd][k][n]
	if s := coefficientProbabilityUpdateSavings(ct, oldProb, newProb, updateProb); s > 0 {
		t.Fatalf("setup: savings = %d > 0; want non-positive so default path skips", s)
	}

	// Default path with key-frame counts: matches libvpx default branch,
	// so no force-update fires when savings <= 0.
	defaultProbs, defaultUpdates, err := coefficientProbabilityUpdatesFromCounts(&base, &counts)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCounts: %v", err)
	}
	if defaultUpdates.Update[blk][bnd][k][n] {
		t.Fatalf("default key-frame path forced update at [%d][%d][%d][%d] (savings<=0); libvpx default branch does not force on key frames", blk, bnd, k, n)
	}
	if got := defaultProbs[blk][bnd][k][n]; got != oldProb {
		t.Fatalf("default key-frame path prob[%d][%d][%d][%d] = %d, want %d (no update)", blk, bnd, k, n, got, oldProb)
	}
	if defaultUpdates.UpdateCount != 0 {
		t.Fatalf("default key-frame path UpdateCount = %d, want 0 (savings rule rejects)", defaultUpdates.UpdateCount)
	}

	// Independent (error-resilient) path with keyFrame=true: libvpx
	// bitstream.c:924-928 forces u=1 whenever newp != *Pold. With the same
	// ct, the shared newp across k for this node is the per-context value
	// (because only one ctx has counts) — so for ctx=k the shared newp is
	// 255 and oldp is 128 → force fires.
	indepProbs, indepUpdates, err := coefficientProbabilityUpdatesFromCountsIndependent(&base, &counts, true)
	if err != nil {
		t.Fatalf("coefficientProbabilityUpdatesFromCountsIndependent: %v", err)
	}
	if !indepUpdates.Update[blk][bnd][k][n] {
		t.Fatalf("independent key-frame path did not force update at [%d][%d][%d][%d] (newp != oldp)", blk, bnd, k, n)
	}
	if got := indepProbs[blk][bnd][k][n]; got != newProb {
		t.Fatalf("independent key-frame path prob[%d][%d][%d][%d] = %d, want %d (forced)", blk, bnd, k, n, got, newProb)
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
