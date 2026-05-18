package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9MvPredNumCandidates pins the libvpx num_mv_refs formula.
// libvpx ref: vp9/encoder/vp9_rd.c:599-601.
func TestVP9MvPredNumCandidates(t *testing.T) {
	cases := []struct {
		name             string
		bsize            common.BlockSize
		maxPartitionSize common.BlockSize
		want             int
	}{
		{"bsize_lt_max", common.Block16x16, common.Block64x64, 3},
		{"bsize_eq_max", common.Block64x64, common.Block64x64, 2},
		{"bsize_gt_max_impossible", common.Block64x64, common.Block32x32, 2},
		{"bsize_4x4_max_64", common.Block4x4, common.Block64x64, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vp9MvPredNumCandidates(tc.bsize, tc.maxPartitionSize)
			if got != tc.want {
				t.Errorf("vp9MvPredNumCandidates(%v, %v) = %d, want %d",
					tc.bsize, tc.maxPartitionSize, got, tc.want)
			}
		})
	}
}

// TestVP9MvPredScanCandidatesNearSameNearest pins the i==1 dedup when
// ref_mvs[0] == ref_mvs[1].
// libvpx ref: vp9/encoder/vp9_rd.c:608-609 + 615.
func TestVP9MvPredScanCandidatesNearSameNearest(t *testing.T) {
	const w, h = 8, 8
	const stride = 32
	const refRows = 32
	src := make([]byte, stride*h)
	ref := make([]byte, stride*refRows)
	for i := range src {
		src[i] = byte(i & 0xff)
	}
	for i := range ref {
		ref[i] = byte(i & 0xff)
	}
	cands := []vp9MvPredInputCandidate{
		{mv: vp9dec.MV{Row: 8, Col: 8}, valid: true}, // fp=(1,1)
		{mv: vp9dec.MV{Row: 8, Col: 8}, valid: true}, // duplicate
		{valid: false},
	}
	// Both candidates would scan position (1,1) but near_same_nearest
	// triggers and only one is scanned. The expected best_index = 0.
	result := vp9MvPredScanCandidates(cands, 3,
		src, stride, 0, 0,
		ref, stride, 0, 0, 0, 0, refRows,
		w, h)
	if result.bestIndex != 0 {
		t.Errorf("bestIndex = %d, want 0 (libvpx near_same_nearest dedup)",
			result.bestIndex)
	}
	if result.bestSad == ^uint64(0) {
		t.Error("bestSad sentinel — candidate scan never ran")
	}
}

// TestVP9MvPredScanCandidatesZeroSeenDedup pins the libvpx zero_seen
// guard: when two candidates resolve to (0,0) full-pel, only the first
// is scanned.
// libvpx ref: vp9/encoder/vp9_rd.c:620-621.
func TestVP9MvPredScanCandidatesZeroSeenDedup(t *testing.T) {
	const w, h = 8, 8
	const stride = 16
	src := make([]byte, stride*h)
	ref := make([]byte, stride*h)
	// Make src = ref so SAD at (0,0) is zero.
	for i := range src {
		src[i] = 100
		ref[i] = 100
	}
	cands := []vp9MvPredInputCandidate{
		// MV (3,3) eighth-pel rounds to (1,1) full-pel:
		//   fp_row = (3 + 3 + 1) >> 3 = 0
		//   fp_col = (3 + 3 + 1) >> 3 = 0
		// So this is a "near-zero" candidate.
		{mv: vp9dec.MV{Row: 3, Col: 3}, valid: true},
		// MV (1,1) eighth-pel also rounds to (0,0).
		{mv: vp9dec.MV{Row: 1, Col: 1}, valid: true},
		{valid: false},
	}
	result := vp9MvPredScanCandidates(cands, 3,
		src, stride, 0, 0,
		ref, stride, 0, 0, 0, 0, h,
		w, h)
	if result.bestSad != 0 {
		t.Errorf("bestSad = %d, want 0 (matched ref at fp_offset (0,0))",
			result.bestSad)
	}
	if result.bestIndex != 0 {
		t.Errorf("bestIndex = %d, want 0 (libvpx zero_seen dedup keeps first)",
			result.bestIndex)
	}
}

// TestVP9MvPredScanCandidatesInvalidSkip pins the INT16_MAX skip semantics.
// govpx encodes "absent" as valid=false; libvpx encodes it as
// row==INT16_MAX || col==INT16_MAX.
// libvpx ref: vp9/encoder/vp9_rd.c:614.
func TestVP9MvPredScanCandidatesInvalidSkip(t *testing.T) {
	const w, h = 8, 8
	const stride = 16
	src := make([]byte, stride*h)
	ref := make([]byte, stride*h)
	for i := range src {
		src[i] = byte(i & 0xff)
		ref[i] = byte(i & 0xff)
	}
	cands := []vp9MvPredInputCandidate{
		{valid: false},
		{valid: false},
		{valid: false},
	}
	result := vp9MvPredScanCandidates(cands, 3,
		src, stride, 0, 0,
		ref, stride, 0, 0, 0, 0, h,
		w, h)
	if result.bestSad != ^uint64(0) {
		t.Errorf("bestSad = %d, want sentinel (no valid candidates scanned)",
			result.bestSad)
	}
}

// TestVP9MvPredScanCandidatesMaxMvContext pins the max_mv tracker that
// libvpx writes to x->max_mv_context[ref_frame].
// libvpx ref: vp9/encoder/vp9_rd.c:618 + 637.
func TestVP9MvPredScanCandidatesMaxMvContext(t *testing.T) {
	const w, h = 8, 8
	const stride = 16
	src := make([]byte, stride*h)
	ref := make([]byte, stride*h)
	cands := []vp9MvPredInputCandidate{
		// |row|/|col| = max(|8|, |16|) >> 3 = 2.
		{mv: vp9dec.MV{Row: 8, Col: 16}, valid: true},
		// |row|/|col| = max(|24|, |-7|) >> 3 = 3.
		{mv: vp9dec.MV{Row: 24, Col: -7}, valid: true},
		{valid: false},
	}
	result := vp9MvPredScanCandidates(cands, 3,
		src, stride, 0, 0,
		ref, stride, 0, 0, 0, 0, h,
		w, h)
	if result.maxMvContext != 3 {
		t.Errorf("maxMvContext = %d, want 3 (max(|row|,|col|) >> 3 across candidates)",
			result.maxMvContext)
	}
}

// TestVP9NewmvDiffBiasOutOfBandShift pins the libvpx out-of-band row/col
// shift adjustment.
// libvpx ref: vp9/encoder/vp9_pickmode.c:1346-1351.
func TestVP9NewmvDiffBiasOutOfBandShift(t *testing.T) {
	above := &vp9dec.NeighborMi{
		Mv: [2]vp9dec.MV{{Row: 0, Col: 0}},
	}
	left := &vp9dec.NeighborMi{
		Mv: [2]vp9dec.MV{{Row: 0, Col: 0}},
	}
	// row_diff = 0 - 100 = -100, abs > 48 → fire.
	// bsize > BLOCK_32X32 → rdcost << 1.
	got := vp9NewmvDiffBias(common.NewMv, 1000, common.Block64x64,
		100, 0, above, left, false, false, false, false, false)
	if !got.adjusted {
		t.Error("expected adjusted=true when |row_diff| > 48 and bsize > 32x32")
	}
	if got.rdcost != 2000 {
		t.Errorf("rdcost = %d, want 2000 (1000 << 1 for bsize > BLOCK_32X32)",
			got.rdcost)
	}

	// bsize <= BLOCK_32X32 → rdcost = 3 * rdcost / 2.
	got = vp9NewmvDiffBias(common.NewMv, 1000, common.Block16x16,
		100, 0, above, left, false, false, false, false, false)
	if !got.adjusted {
		t.Error("expected adjusted=true when |row_diff| > 48 and bsize <= 32x32")
	}
	if got.rdcost != 1500 {
		t.Errorf("rdcost = %d, want 1500 (3 * 1000 / 2 for bsize <= BLOCK_32X32)",
			got.rdcost)
	}
}

// TestVP9NewmvDiffBiasNoFireUnderThreshold pins the gate that no
// adjustment is applied when the MV is close to the neighbour average.
// libvpx ref: vp9/encoder/vp9_pickmode.c:1346.
func TestVP9NewmvDiffBiasNoFireUnderThreshold(t *testing.T) {
	above := &vp9dec.NeighborMi{
		Mv: [2]vp9dec.MV{{Row: 100, Col: 100}},
	}
	left := &vp9dec.NeighborMi{
		Mv: [2]vp9dec.MV{{Row: 100, Col: 100}},
	}
	// al_avg = (100+100)/2 = 100. mv = (100,100). diff = 0.
	got := vp9NewmvDiffBias(common.NewMv, 1000, common.Block64x64,
		100, 100, above, left, false, false, false, false, false)
	if got.adjusted {
		t.Error("expected adjusted=false when row/col_diff is below 48")
	}
	if got.rdcost != 1000 {
		t.Errorf("rdcost = %d, want 1000 (no adjustment)", got.rdcost)
	}
}

// TestVP9NewmvDiffBiasSkipsNonNewMv pins the libvpx mode gate.
// libvpx ref: vp9/encoder/vp9_pickmode.c:1313 — outer if(this_mode == NEWMV).
func TestVP9NewmvDiffBiasSkipsNonNewMv(t *testing.T) {
	above := &vp9dec.NeighborMi{}
	left := &vp9dec.NeighborMi{}
	got := vp9NewmvDiffBias(common.NearestMv, 1000, common.Block64x64,
		100, 100, above, left, false, false, false, false, false)
	if got.adjusted {
		t.Error("expected NEARESTMV to bypass the NEWMV branch (no shift)")
	}
	if got.rdcost != 1000 {
		t.Errorf("rdcost = %d, want 1000 (NEARESTMV unmodified)", got.rdcost)
	}
}

// TestVP9NewmvDiffBiasNoiseEstimateBias pins the libvpx noise-estimate
// bias clause.
// libvpx ref: vp9/encoder/vp9_pickmode.c:1354-1356.
func TestVP9NewmvDiffBiasNoiseEstimateBias(t *testing.T) {
	// Skip the NEWMV branch by choosing ZEROMV; we want only the
	// noise-estimate branch to fire.
	got := vp9NewmvDiffBias(common.ZeroMv, 8000, common.Block32x32,
		1, 1, nil, nil, true, true, true, false, false)
	if !got.adjusted {
		t.Error("expected adjusted=true when noise-estimate gate passes")
	}
	// libvpx: this_rdc->rdcost = 7 * (this_rdc->rdcost >> 3);
	// 8000 >> 3 = 1000; 7 * 1000 = 7000.
	if got.rdcost != 7000 {
		t.Errorf("rdcost = %d, want 7000 (7 * 8000 >> 3)", got.rdcost)
	}
}

// TestVP9NewmvDiffBiasNoiseEstimateGateLast pins the is_last_frame gate
// on the noise-estimate clause.
// libvpx ref: vp9/encoder/vp9_pickmode.c:1354.
func TestVP9NewmvDiffBiasNoiseEstimateRequiresLast(t *testing.T) {
	got := vp9NewmvDiffBias(common.ZeroMv, 8000, common.Block32x32,
		1, 1, nil, nil, false, true, true, false, false)
	if got.adjusted {
		t.Error("noise-estimate clause must require is_last_frame")
	}
}

// TestVP9NewmvDiffBiasLowvarHighsumdiffBias pins the lowvar_highsumdiff
// branch.
// libvpx ref: vp9/encoder/vp9_pickmode.c:1358-1361.
func TestVP9NewmvDiffBiasLowvarHighsumdiffBias(t *testing.T) {
	got := vp9NewmvDiffBias(common.ZeroMv, 8000, common.Block16x16,
		1, 1, nil, nil, true, false, false, true, false)
	if !got.adjusted {
		t.Error("expected adjusted=true when lowvar_highsumdiff gate passes")
	}
	if got.rdcost != 7000 {
		t.Errorf("rdcost = %d, want 7000 (7 * 8000 >> 3)", got.rdcost)
	}
}
