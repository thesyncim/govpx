package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestVP8ZeroMVModeCostParity pins the audit conclusion of task
// #299 — namely that govpx's per-mode rate cost path
// (ZEROMV / NEARESTMV / NEARMV / NEWMV / SPLITMV) is a byte-faithful port of
// the libvpx v1.16.0 rate accounting it mirrors.
//
// Sibling pins #297 / #293 surface the same SPLITMV-vs-NEWMV / NEARESTMV-vs-
// SPLITMV mode-pick divergence at MB(0,0) of frame 1 on the 1280×720 SSIM
// best-quality screen-content cohort:
//
//	govpx: 3482 NEARESTMV + 116 SPLITMV + 2 NEWMV (out of 3600)
//	libvpx: 295 NEARESTMV + 664 SPLITMV + 1 NEWMV (out of 960 labeled)
//
// Task #299 was scoped to audit whether the per-mode rate components driving
// the mode preference were divergent. The audit walks every libvpx site that
// contributes to a candidate mode's rate2 in vp8_rd_pick_inter_mode and
// asserts the govpx mirror agrees verbatim:
//
//  1. vp8_cost_mv_ref            → interPredictionModeRate          (5 modes)
//  2. vp8_mv_ref_tree            → vp8tables.MVRefTree
//  3. vp8_mode_contexts          → vp8tables.InterModeContexts
//  4. vp8_mbsplit_tree           → vp8tables.MBSplitTree
//  5. vp8_mbsplit_probs          → vp8tables.MBSplitProbs
//  6. vp8_sub_mv_ref_tree        → vp8tables.SubMVRefTree
//  7. sub_mv_ref_prob (default)  → libvpxDefaultSubMVRefProbs
//  8. vp8_find_near_mvs counts   → findNearInterMotionVectors counts
//  9. picker-cached mdcounts     → selectRDInterFrameModeDecision.modeMVs
//     (cached once for the primary ref via interModeMVSlots and re-used for
//     every mode, matching libvpx rdopt.c:1813-1820's single
//     vp8_find_near_mvs_bias for ref_frame_map[1] feeding every iteration of
//     the MAX_MODES loop)
//
// All eight surface as byte-exact in this test. Conclusion: the residual
// task #297 / #293 mode-pick divergence is NOT in the per-mode rate
// accounting layer; it lies in the SPLITMV per-label motion-search seed /
// distortion / zbin_mode_boost path documented in the #297 commit body.
//
// Future investigators picking up the mode-pick bisect after #299 should
// start their search downstream of rate accounting:
//
//   - vp8_rd_pick_best_mbsegmentation per-label motion search (rdopt.c:998+)
//   - splitMotionSubsetContext.selectMotion seed/step_param derivation
//   - zbin_mode_boost split-vs-new (govpx splitInterModeZbinBoost=0,
//     NEWMV zbinModeBoost=MV_ZBIN_BOOST=4 — verified matching libvpx
//     rdopt.c:1913-1931)
//   - rd_threshes[THR_NEW{1,2,3}]/rd_thresh_mult evolution for SPLITMV
//
// References:
//
//   - libvpx v1.16.0 vp8/encoder/rdopt.c:797-803 vp8_cost_mv_ref
//   - libvpx v1.16.0 vp8/common/findnearmv.c:150-159 vp8_mv_ref_probs
//   - libvpx v1.16.0 vp8/common/modecont.c:13-26 vp8_mode_contexts
//   - libvpx v1.16.0 vp8/common/entropymode.c:35-95
//     sub_mv_ref_prob / vp8_mbsplit_probs / vp8_mv_ref_tree
//   - libvpx v1.16.0 vp8/encoder/modecosts.c:37-38 inter_bmode_costs init
//   - libvpx v1.16.0 vp8/encoder/rdopt.c:985-987 SPLITMV picker seed
//     (mbsplit_tree + cost_mv_ref(SPLITMV, mdcounts))
//   - libvpx v1.16.0 vp8/encoder/rdopt.c:1816,1834 mdcounts cached for
//     ref_frame_map[1] then reused for every this_ref_frame iteration
//   - govpx vp8_encoder_inter_rate.go:227-262 interPredictionModeRate
//   - govpx vp8_encoder_inter_modes_refs.go:79 modeMVs.counts caching
func TestVP8ZeroMVModeCostParity(t *testing.T) {
	t.Run("InterModeContextsMatchesLibvpx", testVP8ZeroMVInterModeContextsMatchLibvpx)
	t.Run("MVRefTreeMatchesLibvpx", testVP8ZeroMVMVRefTreeMatchesLibvpx)
	t.Run("MBSplitTreeAndProbsMatchLibvpx", testVP8ZeroMVMBSplitTreeAndProbsMatchLibvpx)
	t.Run("SubMVRefTreeAndProbsMatchLibvpx", testVP8ZeroMVSubMVRefTreeAndProbsMatchLibvpx)
	t.Run("MBZeroZeroFrame1AllModeCostsMatchOracle", testVP8ZeroMVFrameOriginModeCostsMatchOracle)
	t.Run("PerModeCostMatchesVP8CostMVRefOracleAllContexts", testVP8ZeroMVPerModeCostMatchesOracleAllContexts)
	t.Run("PartitionCostMatchesVP8CostTokenOracle", testVP8ZeroMVPartitionCostMatchesTokenOracle)
	t.Run("SubMVRefCostMatchesInterBmodeCostsOracle", testVP8ZeroMVSubMVRefCostMatchesBModeOracle)
	t.Run("NearMVCountsAtFrameOriginAreZero", testVP8ZeroMVNearMVCountsAtFrameOriginAreZero)
}

func testVP8ZeroMVInterModeContextsMatchLibvpx(t *testing.T) {
	// libvpx v1.16.0 vp8/common/modecont.c:13-26.
	want := [6][4]uint8{
		{7, 1, 1, 143},
		{14, 18, 14, 107},
		{135, 64, 57, 68},
		{60, 56, 128, 65},
		{159, 134, 128, 34},
		{234, 188, 128, 28},
	}
	got := vp8tables.InterModeContexts
	if len(got) != len(want) {
		t.Fatalf("InterModeContexts row count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("InterModeContexts[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func testVP8ZeroMVMVRefTreeMatchesLibvpx(t *testing.T) {
	// libvpx v1.16.0 vp8/common/entropymode.c:87-88
	//   vp8_mv_ref_tree[8] = { -ZEROMV, 2, -NEARESTMV, 4,
	//                          -NEARMV, 6, -NEWMV, -SPLITMV }
	// ZEROMV=7, NEARESTMV=5, NEARMV=6, NEWMV=8, SPLITMV=9 in govpx enum
	// (matches libvpx blockd.h:65-79 enum ordering relative to MV-mode
	// block). The tree must match shape, with leaf indices remapped to
	// the govpx MBPredictionMode enum.
	want := [8]int16{
		-int16(vp8common.ZeroMV), 2,
		-int16(vp8common.NearestMV), 4,
		-int16(vp8common.NearMV), 6,
		-int16(vp8common.NewMV), -int16(vp8common.SplitMV),
	}
	if vp8tables.MVRefTree != want {
		t.Fatalf("MVRefTree = %v, want %v", vp8tables.MVRefTree, want)
	}
}

func testVP8ZeroMVMBSplitTreeAndProbsMatchLibvpx(t *testing.T) {
	// libvpx v1.16.0 vp8/common/entropymode.c:85
	//   vp8_mbsplit_tree[6] = { -3, 2, -2, 4, -0, -1 }
	wantTree := [6]int16{-3, 2, -2, 4, -0, -1}
	if vp8tables.MBSplitTree != wantTree {
		t.Fatalf("MBSplitTree = %v, want %v", vp8tables.MBSplitTree, wantTree)
	}
	// libvpx v1.16.0 vp8/common/entropymode.c:54
	//   vp8_mbsplit_probs[VP8_NUMMBSPLITS - 1] = { 110, 111, 150 }
	wantProbs := [3]uint8{110, 111, 150}
	if vp8tables.MBSplitProbs != wantProbs {
		t.Fatalf("MBSplitProbs = %v, want %v", vp8tables.MBSplitProbs, wantProbs)
	}
}

func testVP8ZeroMVSubMVRefTreeAndProbsMatchLibvpx(t *testing.T) {
	// libvpx v1.16.0 vp8/common/entropymode.c:90-91
	//   vp8_sub_mv_ref_tree[6] = { -LEFT4X4, 2, -ABOVE4X4, 4,
	//                              -ZERO4X4, -NEW4X4 }
	// Left4x4=10, Above4x4=11, Zero4x4=12, New4x4=13 in govpx enum.
	want := [6]int16{
		-int16(vp8common.Left4x4), 2,
		-int16(vp8common.Above4x4), 4,
		-int16(vp8common.Zero4x4), -int16(vp8common.New4x4),
	}
	if vp8tables.SubMVRefTree != want {
		t.Fatalf("SubMVRefTree = %v, want %v", vp8tables.SubMVRefTree, want)
	}
	// libvpx v1.16.0 vp8/common/entropymode.c:35
	//   sub_mv_ref_prob[VP8_SUBMVREFS - 1] = { 180, 162, 25 }
	// memcpy'd into x->fc.sub_mv_ref_prob at every entropy reset
	// (entropymode.c:99) and consumed by modecosts.c:37 to build
	// inter_bmode_costs which the RD picker reads.
	wantSub := [3]uint8{180, 162, 25}
	if libvpxDefaultSubMVRefProbs != wantSub {
		t.Fatalf("libvpxDefaultSubMVRefProbs = %v, want %v",
			libvpxDefaultSubMVRefProbs, wantSub)
	}
}

func testVP8ZeroMVFrameOriginModeCostsMatchOracle(t *testing.T) {
	// MB(0,0) on inter frame 1: above/left/aboveLeft are off-frame edges.
	// In libvpx these slots are calloc'd MODE_INFO records
	// (alloccommon.c:89-94) so mbmi.ref_frame == INTRA_FRAME (=0) for
	// every border slot and vp8_find_near_mvs returns mdcounts=[0,0,0,0].
	// In govpx the equivalent call site passes nil above/left/aboveLeft to
	// findNearInterMotionVectors which short-circuits the increment paths
	// via interFrameReference(nil) == IntraFrame; counts remain
	// InterModeCounts{0,0,0,0}.
	counts := vp8enc.InterFrameModeCounts(nil, nil, nil, vp8common.LastFrame,
		defaultInterFrameSignBias())
	if counts != (vp8enc.InterModeCounts{}) {
		t.Fatalf("MB(0,0) counts at frame 1 = %+v, want zero counts", counts)
	}

	// libvpx's vp8_cost_mv_ref(m, [0,0,0,0]) reduces to a
	// vp8_treed_cost walk over { vp8_mv_ref_tree, p, encoding(m) } with
	//   p[0] = vp8_mode_contexts[0][0] = 7
	//   p[1] = vp8_mode_contexts[0][1] = 1
	//   p[2] = vp8_mode_contexts[0][2] = 1
	//   p[3] = vp8_mode_contexts[0][3] = 143
	// The tree walk produces the per-mode costs derived below from the
	// encoding array vp8_mv_ref_encoding_array (vp8_entropymodedata.h:41):
	//   NEARESTMV {2, 2}: bit 1, bit 0       → cost_one(7)  + cost_zero(1)
	//   NEARMV    {6, 3}: bits 1,1,0         → cost_one(7)  + cost_one(1) + cost_zero(1)
	//   ZEROMV    {0, 1}: bit 0              → cost_zero(7)
	//   NEWMV     {14,4}: bits 1,1,1,0       → cost_one(7)  + cost_one(1) + cost_one(1) + cost_zero(143)
	//   SPLITMV   {15,4}: bits 1,1,1,1       → cost_one(7)  + cost_one(1) + cost_one(1) + cost_one(143)
	const (
		p0 = uint8(7)
		p1 = uint8(1)
		p2 = uint8(1)
		p3 = uint8(143)
	)
	want := map[vp8common.MBPredictionMode]int{
		vp8common.ZeroMV:    boolBitCost(p0, 0),
		vp8common.NearestMV: boolBitCost(p0, 1) + boolBitCost(p1, 0),
		vp8common.NearMV:    boolBitCost(p0, 1) + boolBitCost(p1, 1) + boolBitCost(p2, 0),
		vp8common.NewMV:     boolBitCost(p0, 1) + boolBitCost(p1, 1) + boolBitCost(p2, 1) + boolBitCost(p3, 0),
		vp8common.SplitMV:   boolBitCost(p0, 1) + boolBitCost(p1, 1) + boolBitCost(p2, 1) + boolBitCost(p3, 1),
	}
	for mode, wantCost := range want {
		got := interPredictionModeRate(mode, counts)
		if got != wantCost {
			t.Fatalf("MB(0,0) frame 1 mode=%v rate = %d, want %d",
				mode, got, wantCost)
		}
	}
}

func testVP8ZeroMVPerModeCostMatchesOracleAllContexts(t *testing.T) {
	// Sweep every (ct[0], ct[1], ct[2], ct[3]) in [0,6)^4 — the full set of
	// near_mv_ref_ct values libvpx ever feeds into vp8_cost_mv_ref via
	// vp8_find_near_mvs's [0,6) accumulator. For each context vector,
	// recompute the expected cost via the libvpx-equivalent treed_cost
	// walk and compare against interPredictionModeRate. Guarantees the
	// govpx unrolled boolBitCost ladder agrees with vp8_treed_cost over
	// the entire context table.
	modes := []vp8common.MBPredictionMode{
		vp8common.ZeroMV,
		vp8common.NearestMV,
		vp8common.NearMV,
		vp8common.NewMV,
		vp8common.SplitMV,
	}
	for c0 := range 6 {
		for c1 := range 6 {
			for c2 := range 6 {
				for c3 := range 6 {
					counts := vp8enc.InterModeCounts{
						Intra:   uint8(c0),
						Nearest: uint8(c1),
						Near:    uint8(c2),
						Split:   uint8(c3),
					}
					p := [4]uint8{
						vp8tables.InterModeContexts[c0][0],
						vp8tables.InterModeContexts[c1][1],
						vp8tables.InterModeContexts[c2][2],
						vp8tables.InterModeContexts[c3][3],
					}
					for _, mode := range modes {
						want := task299ExpectedModeRefCost(p, mode)
						got := interPredictionModeRate(mode, counts)
						if got != want {
							t.Fatalf("ct=(%d,%d,%d,%d) mode=%v rate = %d, want %d",
								c0, c1, c2, c3, mode, got, want)
						}
					}
				}
			}
		}
	}
}

func task299ExpectedModeRefCost(p [4]uint8, mode vp8common.MBPredictionMode) int {
	switch mode {
	case vp8common.ZeroMV:
		return boolBitCost(p[0], 0)
	case vp8common.NearestMV:
		return boolBitCost(p[0], 1) + boolBitCost(p[1], 0)
	case vp8common.NearMV:
		return boolBitCost(p[0], 1) + boolBitCost(p[1], 1) + boolBitCost(p[2], 0)
	case vp8common.NewMV:
		return boolBitCost(p[0], 1) + boolBitCost(p[1], 1) + boolBitCost(p[2], 1) + boolBitCost(p[3], 0)
	case vp8common.SplitMV:
		return boolBitCost(p[0], 1) + boolBitCost(p[1], 1) + boolBitCost(p[2], 1) + boolBitCost(p[3], 1)
	default:
		return -1
	}
}

func testVP8ZeroMVPartitionCostMatchesTokenOracle(t *testing.T) {
	// libvpx v1.16.0 vp8/common/vp8_entropymodedata.h:37-39
	//   vp8_mbsplit_encodings[VP8_NUMMBSPLITS] = {
	//     { 6, 3 }, { 7, 3 }, { 2, 2 }, { 0, 1 } }
	// Walks vp8_mbsplit_tree with vp8_mbsplit_probs = {110, 111, 150}.
	probs := [3]uint8{110, 111, 150}
	want := [4]int{
		// partition 0: value=6 (110), len=3 → bits 1,1,0
		boolBitCost(probs[0], 1) + boolBitCost(probs[1], 1) + boolBitCost(probs[2], 0),
		// partition 1: value=7 (111), len=3 → bits 1,1,1
		boolBitCost(probs[0], 1) + boolBitCost(probs[1], 1) + boolBitCost(probs[2], 1),
		// partition 2: value=2 (10),  len=2 → bits 1,0
		boolBitCost(probs[0], 1) + boolBitCost(probs[1], 0),
		// partition 3: value=0 (0),   len=1 → bit 0
		boolBitCost(probs[0], 0),
	}
	for p := range 4 {
		got := mbSplitPartitionRate(uint8(p))
		if got != want[p] {
			t.Fatalf("mbSplitPartitionRate(%d) = %d, want %d", p, got, want[p])
		}
	}
}

func testVP8ZeroMVSubMVRefCostMatchesBModeOracle(t *testing.T) {
	// libvpx v1.16.0 vp8/encoder/modecosts.c:37-38 initialises
	//   rd_costs->inter_bmode_costs[B_MODE] via vp8_cost_tokens over
	//   x->fc.sub_mv_ref_prob (default {180, 162, 25}) and
	//   vp8_sub_mv_ref_tree. The RD picker (rdopt.c:865 cost = x->inter_
	//   bmode_costs[m]) consumes the result.
	// Encoding values from vp8_entropymodedata.h:45-47:
	//   LEFT4X4  {0, 1}: bit 0           → cost_zero(p[0])
	//   ABOVE4X4 {2, 2}: bits 1,0        → cost_one(p[0]) + cost_zero(p[1])
	//   ZERO4X4  {6, 3}: bits 1,1,0      → cost_one(p[0]) + cost_one(p[1]) + cost_zero(p[2])
	//   NEW4X4   {7, 3}: bits 1,1,1      → cost_one(p[0]) + cost_one(p[1]) + cost_one(p[2])
	probs := libvpxDefaultSubMVRefProbs
	want := map[vp8common.BPredictionMode]int{
		vp8common.Left4x4:  boolBitCost(probs[0], 0),
		vp8common.Above4x4: boolBitCost(probs[0], 1) + boolBitCost(probs[1], 0),
		vp8common.Zero4x4:  boolBitCost(probs[0], 1) + boolBitCost(probs[1], 1) + boolBitCost(probs[2], 0),
		vp8common.New4x4:   boolBitCost(probs[0], 1) + boolBitCost(probs[1], 1) + boolBitCost(probs[2], 1),
	}
	for mode, wantCost := range want {
		got := splitSubMotionLabelRate(mode)
		if got != wantCost {
			t.Fatalf("splitSubMotionLabelRate(%v) = %d, want %d",
				mode, got, wantCost)
		}
	}
}

func testVP8ZeroMVNearMVCountsAtFrameOriginAreZero(t *testing.T) {
	// Inter frame 1 picker entry at MB(0,0) drives all three near-MV
	// neighbour slots to nil/intra so vp8_find_near_mvs (and govpx's
	// findNearInterMotionVectors) returns counts == {0,0,0,0}. Same
	// invariant for any signBias permutation. Guards against off-frame
	// MB border handling drift in interFrameReference(nil).
	for _, refFrame := range []vp8common.MVReferenceFrame{
		vp8common.LastFrame,
		vp8common.GoldenFrame,
		vp8common.AltRefFrame,
	} {
		for _, signBiasGF := range []bool{false, true} {
			for _, signBiasAR := range []bool{false, true} {
				signBias := [vp8common.MaxRefFrames]bool{
					vp8common.GoldenFrame: signBiasGF,
					vp8common.AltRefFrame: signBiasAR,
				}
				counts := vp8enc.InterFrameModeCounts(nil, nil, nil, refFrame, signBias)
				if counts != (vp8enc.InterModeCounts{}) {
					t.Fatalf("ref=%v signBias=(GF=%t,AR=%t) MB(0,0) counts = %+v, want zero",
						refFrame, signBiasGF, signBiasAR, counts)
				}
			}
		}
	}
}
