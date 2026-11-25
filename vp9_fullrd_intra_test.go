package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"image"
	"testing"
)

// withVP9InterDeepRDThisRDScore routes pickVP9InterModeWithOrder's candidate
// score through the GENUINE per-mode this_rd assembly (vp9FullRDInterThisRD)
// instead of the model-RD vp9InterModeScore approximation, for the duration of
// a test. The flag defaults to false (production + the deep-RD partition
// serialization tests stay on the model score); these tests flip it to exercise
// the genuine-RD wire-in end-to-end.
func withVP9InterDeepRDThisRDScore(t *testing.T) {
	t.Helper()
	prev := vp9InterUseDeepRDThisRDScore
	vp9InterUseDeepRDThisRDScore = true
	t.Cleanup(func() { vp9InterUseDeepRDThisRDScore = prev })
}

// TestVP9InterDeepRDThisRDScoreWiresThrough proves the genuine per-mode this_rd
// flows into pickVP9InterModeWithOrder's decision when
// vp9InterUseDeepRDThisRDScore is on: a multi-frame inter encode with the flag
// flipped still produces a valid, cleanly-decodable bitstream (the genuine
// Y-RD + UV-RD + skip-pick scoring drives real mode/MV/tx decisions through the
// writer without desync), AND its inter frames differ from the model-score
// production encode (the genuine RD selects different candidates) — so the wire
// is load-bearing, not a no-op.
func TestVP9InterDeepRDThisRDScoreWiresThrough(t *testing.T) {
	const width, height = 64, 64
	encode := func(genuine bool) ([][]byte, error) {
		if genuine {
			withVP9InterDeepRDThisRDScore(t)
		}
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width: width, Height: height, CpuUsed: -3,
		})
		if err != nil {
			return nil, err
		}
		defer e.Close()
		sources := vp9test.NewPanningSources(width, height, 4)
		var frames [][]byte
		for i := range 4 {
			pkt, err := e.Encode(sources[i])
			if err != nil {
				return nil, err
			}
			cp := make([]byte, len(pkt))
			copy(cp, pkt)
			frames = append(frames, cp)
		}
		return frames, nil
	}

	// Production (model-score) reference.
	modelFrames, err := encode(false)
	if err != nil {
		t.Fatalf("model-score encode: %v", err)
	}

	// Genuine-RD encode under the flag.
	genuineFrames, err := encode(true)
	if err != nil {
		t.Fatalf("genuine-RD encode: %v", err)
	}

	// The genuine-RD inter frames must decode cleanly (no serialization desync).
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, pkt := range genuineFrames {
		if err := d.Decode(pkt); err != nil {
			t.Fatalf("genuine-RD frame %d decode failed: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("genuine-RD frame %d NextFrame !ok", i)
		}
	}

	// The wire must be load-bearing: at least one inter frame should differ from
	// the model-score encode (frame 0 is the keyframe and is unaffected by the
	// inter scorer, so compare from frame 1).
	differs := false
	for i := 1; i < len(modelFrames) && i < len(genuineFrames); i++ {
		if len(modelFrames[i]) != len(genuineFrames[i]) {
			differs = true
			break
		}
		for j := range modelFrames[i] {
			if modelFrames[i][j] != genuineFrames[i][j] {
				differs = true
				break
			}
		}
		if differs {
			break
		}
	}
	if !differs {
		t.Fatal("genuine per-mode this_rd scoring produced byte-identical inter " +
			"frames to the model-score path; the wire is not load-bearing")
	}
}

// The expected per-mode bit costs below were produced by running libvpx's
// vp9_cost_tokens over the relevant probability rows of vp9_intra_mode_tree.
// They were captured with a standalone C harness that links the verbatim
// libvpx v1.16.0 tables and walker (vp9/encoder/vp9_cost.c vp9_cost_tokens,
// vp9/common/vp9_entropymode.c vp9_intra_mode_tree / vp9_kf_y_mode_prob /
// vp9_kf_uv_mode_prob / default_if_y_probs / default_if_uv_probs, and the
// vp9_prob_cost[256] table from vp9/encoder/vp9_cost.c). The harness was a
// byte-for-byte copy of those tables/functions; it was run once to read the
// values and then discarded (not committed). The same values are what
// fill_mode_costs (vp9/encoder/vp9_rd.c:92-110) stores into
// cpi->mbmode_cost / cpi->y_mode_costs / cpi->intra_uv_mode_cost.
//
// Mode order is the PredictionMode enum DC_PRED..TM_PRED (0..9):
//   {DC, V, H, D45, D135, D117, D153, D207, D63, TM}.

// mbmode_cost = vp9_cost_tokens(default_if_y_probs[1], vp9_intra_mode_tree).
// libvpx vp9_rd.c:103, consumed at vp9_rdopt.c:3864.
var wantVP9MbmodeCost = [common.IntraModes]int{
	489, 2724, 1263, 2865, 2728, 3603, 2727, 2117, 3095, 1514,
}

// intra_uv_mode_cost[INTER_FRAME][DC_PRED] =
//
//	vp9_cost_tokens(default_if_uv_probs[DC_PRED], vp9_intra_mode_tree).
//
// libvpx vp9_rd.c:107-108, consumed at vp9_rdopt.c:1496.
var wantVP9InterUVCostDC = [common.IntraModes]int{
	560, 1384, 1177, 3241, 2784, 2761, 2540, 2247, 2841, 3126,
}

// y_mode_costs[V_PRED][H_PRED] =
//
//	vp9_cost_tokens(vp9_kf_y_mode_prob[V_PRED][H_PRED], vp9_intra_mode_tree).
//
// libvpx vp9_rd.c:97-100, consumed at vp9_rdopt.c:1379.
var wantVP9KeyframeYCostVH = [common.IntraModes]int{
	1301, 1207, 987, 2379, 2217, 2812, 2407, 1784, 2444, 1748,
}

// intra_uv_mode_cost[KEY_FRAME][DC_PRED] =
//
//	vp9_cost_tokens(vp9_kf_uv_mode_prob[DC_PRED], vp9_intra_mode_tree).
//
// libvpx vp9_rd.c:104-106, consumed at vp9_rdopt.c:1496.
var wantVP9KeyframeUVCostDC = [common.IntraModes]int{
	425, 1792, 1380, 2788, 2739, 2762, 2493, 2261, 2763, 2936,
}

func TestVP9FullRDInterIntraYModeCostMatchesLibvpx(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	var got [common.IntraModes]int
	vp9FullRDInterIntraYModeCosts(got[:], &fc)
	if got != wantVP9MbmodeCost {
		t.Fatalf("mbmode_cost = %v, libvpx = %v", got, wantVP9MbmodeCost)
	}

	// Guard against the historical divergence: keying on
	// size_group_lookup[bsize] instead of the fixed index 1 produces a
	// different table for any size group != 1. Size group 2 (e.g. BLOCK_16X16)
	// must NOT equal the mbmode_cost row.
	var sg2 [common.IntraModes]int
	encoder.VP9CostTokens(sg2[:], fc.YModeProb[2][:], common.IntraModeTree[:])
	if sg2 == wantVP9MbmodeCost {
		t.Fatalf("size group 2 cost row unexpectedly equals mbmode_cost; "+
			"the test cannot detect the size-group divergence (sg2=%v)", sg2)
	}
}

func TestVP9FullRDIntraUVModeCostInterMatchesLibvpx(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	var got [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(got[:], vp9FullRDInterFrame, common.DcPred, &fc)
	if got != wantVP9InterUVCostDC {
		t.Fatalf("intra_uv_mode_cost[INTER][DC] = %v, libvpx = %v", got, wantVP9InterUVCostDC)
	}
}

func TestVP9FullRDKeyframeYModeCostMatchesLibvpx(t *testing.T) {
	var got [common.IntraModes]int
	vp9FullRDKeyframeYModeCosts(got[:], common.VPred, common.HPred)
	if got != wantVP9KeyframeYCostVH {
		t.Fatalf("y_mode_costs[V][H] = %v, libvpx = %v", got, wantVP9KeyframeYCostVH)
	}
}

func TestVP9FullRDIntraUVModeCostKeyframeMatchesLibvpx(t *testing.T) {
	// KEY_FRAME UV cost ignores fc; pass an empty context to confirm the
	// keyframe branch reads vp9_kf_uv_mode_prob and not fc->uv_mode_prob.
	var fc vp9dec.FrameContext
	var got [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(got[:], vp9FullRDKeyFrame, common.DcPred, &fc)
	if got != wantVP9KeyframeUVCostDC {
		t.Fatalf("intra_uv_mode_cost[KEY][DC] = %v, libvpx = %v", got, wantVP9KeyframeUVCostDC)
	}
}

// TestVP9FullRDIntraModeRDDecision pins one concrete intra-mode RD comparison
// exactly as libvpx's rd_pick_intra_sby_mode does it:
//
//	this_rate = this_rate_tokenonly + bmode_costs[mode];
//	this_rd   = RDCOST(x->rdmult, x->rddiv, this_rate, this_distortion);
//	if (this_rd < best_rd) best_rd = this_rd; mode_selected = mode;
//
// (libvpx vp9/encoder/vp9_rdopt.c:1398-1407). The decision compares two
// candidate modes under a fixed rdmult and the verbatim mbmode_cost values.
// rddiv is RD_DIV_BITS (encoder.RDDivBits = 7); RDCOST is
//
//	((128 * rate * rdmult) >> 8 (RD_EPB_SHIFT-fold)) + (dist << rddiv)
//
// as implemented by encoder.RDCost. The expected best mode is computed from
// the same formula by hand below so the pin doubles as a sanity cross-check.
func TestVP9FullRDIntraModeRDDecision(t *testing.T) {
	// Use the keyframe Y rdmult at a representative qindex so the numbers are
	// concrete. KeyframeRDMul ports vp9's q*q*(4350+q)/1000.
	const qindex = 64
	rdmult := encoder.KeyframeRDMul(qindex)

	// Candidate A: V_PRED with a small token rate and moderate distortion.
	// Candidate B: DC_PRED with a larger token rate but smaller distortion.
	// mbmode_cost[V]=2724, mbmode_cost[DC]=489 (pinned above).
	const (
		tokenRateV = 120
		distV      = uint64(900)
		tokenRateD = 400
		distD      = uint64(700)
	)

	rateV, rdV := vp9FullRDIntraModeRD(rdmult, wantVP9MbmodeCost[common.VPred], tokenRateV, distV)
	rateD, rdD := vp9FullRDIntraModeRD(rdmult, wantVP9MbmodeCost[common.DcPred], tokenRateD, distD)

	// Cross-check the rate halves: rate = tokenRate + mbmode_cost[mode].
	if rateV != tokenRateV+wantVP9MbmodeCost[common.VPred] {
		t.Fatalf("rateV = %d, want %d", rateV, tokenRateV+wantVP9MbmodeCost[common.VPred])
	}
	if rateD != tokenRateD+wantVP9MbmodeCost[common.DcPred] {
		t.Fatalf("rateD = %d, want %d", rateD, tokenRateD+wantVP9MbmodeCost[common.DcPred])
	}

	// Cross-check the RD values against an independent RDCOST evaluation.
	wantRDV := encoder.RDCost(rdmult, encoder.RDDivBits, rateV, distV)
	wantRDD := encoder.RDCost(rdmult, encoder.RDDivBits, rateD, distD)
	if rdV != wantRDV || rdD != wantRDD {
		t.Fatalf("rd mismatch: rdV=%d (want %d) rdD=%d (want %d)", rdV, wantRDV, rdD, wantRDD)
	}

	// The libvpx picker keeps the minimum-RD mode. With the chosen inputs the
	// DC candidate's much smaller mbmode_cost (489 vs 2724) dominates the rate
	// term, so DC_PRED must win despite its larger token rate.
	if !(rdD < rdV) {
		t.Fatalf("expected DC_PRED (rd=%d) to beat V_PRED (rd=%d); rdmult=%d", rdD, rdV, rdmult)
	}
}

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

// vp9UsePartitionSeed0_1_1_0_1Frame1 is the libvpx v1.16.0 ground-truth
// committed block decomposition of frame 1 (first inter frame), superblock 0,
// for the {0,1,1,0,1} long-fixture parity-gap seed (CBR 700 kbps kf=30 realtime
// cpu4, VAR_BASED_PARTITION, one-pass q=145). Captured 2026-06-05 from a private
// $TMPDIR-built libvpx vpxenc with an encode_b (vp9_encodeframe.c:2226) GTBLK
// fprintf probe; the produced two-frame IVF was md5-identical to the unmodified
// pinned vpxenc-vp9 oracle, proving the probe non-mutating. Single-motion-search
// inputs/outputs were cross-checked with a second probe in single_motion_search
// (vp9_rdopt.c:2563): mi(0,0) 8x8 NEWMV uses step_param=6, search_method=FAST_HEX
// and lands at full-pel+subpel (8,14).
//
// Every leaf is BLOCK_8X8 NONE; single-ref LAST; one whole-8x8 intra DC at
// mi(7,2); no compound; interp filters EIGHTTAP(0) + EIGHTTAP_SMOOTH(1) only
// (no SHARP). Fields: {MiRow, MiCol, Mode, Ref0, Interp, MvRow, MvCol}; Mode is
// PREDICTION_MODE (0=DC .. 10=NEARESTMV 11=NEARMV 13=NEWMV), Ref0 is
// ref_frame[0] (0=INTRA 1=LAST), Interp is interp_filter (0=EIGHTTAP
// 1=EIGHTTAP_SMOOTH; 3=SWITCHABLE default on the intra block). MV is in 1/8-pel.
var vp9UsePartitionSeed0_1_1_0_1Frame1 = [64][7]int{
	{0, 0, 13, 1, 0, 8, 14}, {0, 1, 10, 1, 1, 8, 14}, {0, 2, 13, 1, 1, -2, 30}, {0, 3, 13, 1, 1, -8, 40}, {0, 4, 11, 1, 1, -2, 30}, {0, 5, 13, 1, 0, 8, 10}, {0, 6, 10, 1, 0, 8, 10}, {0, 7, 13, 1, 1, 16, 10},
	{1, 0, 10, 1, 1, 8, 14}, {1, 1, 10, 1, 1, 8, 14}, {1, 2, 11, 1, 1, 8, 14}, {1, 3, 13, 1, 1, 54, -64}, {1, 4, 13, 1, 0, 10, 6}, {1, 5, 11, 1, 1, 10, 6}, {1, 6, 13, 1, 0, 8, 0}, {1, 7, 10, 1, 1, 16, 10},
	{2, 0, 10, 1, 0, 8, 14}, {2, 1, 10, 1, 1, 8, 14}, {2, 2, 13, 1, 0, 18, -6}, {2, 3, 13, 1, 0, 52, -58}, {2, 4, 10, 1, 1, 10, 6}, {2, 5, 10, 1, 1, 10, 6}, {2, 6, 10, 1, 0, 8, 0}, {2, 7, 10, 1, 1, 16, 10},
	{3, 0, 13, 1, 0, 0, 24}, {3, 1, 11, 1, 1, 0, 24}, {3, 2, 10, 1, 1, 18, -6}, {3, 3, 11, 1, 1, 18, -6}, {3, 4, 10, 1, 1, 10, 6}, {3, 5, 10, 1, 1, 10, 6}, {3, 6, 13, 1, 1, 2, 20}, {3, 7, 13, 1, 0, 10, 8},
	{4, 0, 13, 1, 0, -8, 32}, {4, 1, 10, 1, 0, 0, 24}, {4, 2, 13, 1, 0, 16, 8}, {4, 3, 13, 1, 1, 22, -6}, {4, 4, 13, 1, 1, 26, -2}, {4, 5, 10, 1, 1, 10, 6}, {4, 6, 13, 1, 1, 8, 0}, {4, 7, 13, 1, 0, 14, 6},
	{5, 0, 13, 1, 0, 12, 10}, {5, 1, 10, 1, 0, 0, 24}, {5, 2, 10, 1, 0, 16, 8}, {5, 3, 13, 1, 0, 4, 18}, {5, 4, 13, 1, 0, 8, 8}, {5, 5, 11, 1, 0, 8, 8}, {5, 6, 13, 1, 1, 0, 26}, {5, 7, 13, 1, 1, 18, 6},
	{6, 0, 10, 1, 1, 12, 10}, {6, 1, 10, 1, 1, 0, 24}, {6, 2, 10, 1, 0, 16, 8}, {6, 3, 10, 1, 1, 4, 18}, {6, 4, 11, 1, 1, 4, 18}, {6, 5, 11, 1, 1, 4, 18}, {6, 6, 11, 1, 1, 4, 18}, {6, 7, 13, 1, 0, 14, -2},
	{7, 0, 10, 1, 1, 12, 10}, {7, 1, 11, 1, 1, 12, 10}, {7, 2, 0, 0, 3, 0, 0}, {7, 3, 10, 1, 1, 4, 18}, {7, 4, 10, 1, 1, 4, 18}, {7, 5, 10, 1, 1, 4, 18}, {7, 6, 10, 1, 1, 4, 18}, {7, 7, 10, 1, 0, 14, -2},
}

// TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1 exercises the VAR_BASED_PARTITION
// full-RD inter driver (vp9InterUseDeepRDUsePartition) for the {0,1,1,0,1}
// frame-1 superblock and pins the prefix of 8x8 leaves that already commit the
// libvpx-exact (mode, ref, interp, mv). The headline assertion is that the deep
// path's libvpx-faithful single_motion_search (step_param via auto_mv_step_size
// + adaptive_motion_search boffset; FAST_HEX dispatch) plus the x->pred_mv[ref]
// candidate[2] threading produce mi(0,0) NEWMV mv=(8,14) — the exact libvpx
// full-pel+subpel result — instead of the model-RD (4,22).
//
// Flag default is OFF, so production and every other VP9 oracle gate are
// byte-identical (the seed stays in vp9LongFixtureParityGapSeeds); this test
// flips it locally. With the genuine larger-block intra producer threaded in
// (pickVP9FullRDInterIntraLeaf), the entire 64-leaf SB committed (mode, ref,
// interp, mv) DECOMPOSITION now matches libvpx. (The full frame-1 bitstream is
// not yet byte-exact: the residual coefficient coding still diverges — govpx
// emits ~30 extra token bytes — which is the next frontier; the committed mode
// decomposition this test pins is the prerequisite for it.)
func TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1(t *testing.T) {
	const width, height = 64, 64

	saved := vp9InterUseDeepRDUsePartition
	savedRBR := vp9InterUseDeepRDRefBestRD
	defer func() {
		vp9InterUseDeepRDUsePartition = saved
		vp9InterUseDeepRDRefBestRD = savedRBR
	}()
	vp9InterUseDeepRDUsePartition = true
	// Thread the running best_rd as the genuine handle_inter_mode budget so the
	// mode-pre-filtering breakouts (super_block_yrd txfm-RD early-exit etc.)
	// prune NEW/NEAR exactly as libvpx — the mechanism that closes mi(1,1).
	vp9InterUseDeepRDRefBestRD = true

	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 30,
		Deadline:            DeadlineRealtime,
		CpuUsed:             4,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	sources := []*image.YCbCr{
		vp9test.NewPanningYCbCr(width, height, 0),
		vp9test.NewPanningYCbCr(width, height, 1),
	}
	dst := make([]byte, 1<<20)
	for i := range 2 {
		if _, err := e.EncodeIntoWithResult(sources[i], dst); err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
	}

	// The committed mi grid (miRows*miCols, stride miCols) holds frame 1's blocks.
	const miCols = (width + 7) / 8
	if len(e.miGrid) < miCols*miCols {
		t.Fatalf("miGrid too small: %d", len(e.miGrid))
	}
	mode := func(r, c int) *vp9dec.NeighborMi { return &e.miGrid[r*miCols+c] }

	// Headline: mi(0,0) NEWMV must be the libvpx full-pel+subpel result (8,14).
	mi00 := mode(0, 0)
	if mi00.Mode != common.NewMv || mi00.RefFrame[0] != vp9dec.LastFrame ||
		mi00.Mv[0] != (vp9dec.MV{Row: 8, Col: 14}) {
		t.Fatalf("mi(0,0) = mode %d ref %d mv %v; want NEWMV LAST (8,14) "+
			"(libvpx single_motion_search FAST_HEX step_param=6)",
			mi00.Mode, mi00.RefFrame[0], mi00.Mv[0])
	}

	// Walk the SB in z-order (the rd_use_partition / writeVP9ModesSb recursion
	// order) and count the leading prefix whose committed (mode, ref, interp, mv)
	// is libvpx-exact. The count is pinned so a regression (or progress) trips
	// the test; the leading prefix is what the deep driver already closes.
	lut := map[[2]int][7]int{}
	for _, b := range vp9UsePartitionSeed0_1_1_0_1Frame1 {
		lut[[2]int{b[0], b[1]}] = b
	}
	zorder := vp9SB64ZOrder8x8()
	matched := 0
	var firstDiff [2]int
	firstDiffSet := false
	for _, pos := range zorder {
		r, c := pos[0], pos[1]
		want := lut[[2]int{r, c}]
		mi := mode(r, c)
		gotInterp := int(mi.InterpFilter)
		// Intra leaf: libvpx leaves interp at SWITCHABLE(3); govpx may park a
		// different don't-care there, so compare interp only on inter leaves.
		ok := int(mi.Mode) == want[2] && int(mi.RefFrame[0]) == want[3] &&
			int(mi.Mv[0].Row) == want[5] && int(mi.Mv[0].Col) == want[6]
		if want[3] == int(vp9dec.LastFrame) {
			ok = ok && gotInterp == want[4]
		}
		if ok {
			matched++
		} else {
			firstDiff = [2]int{r, c}
			firstDiffSet = true
			break
		}
	}

	// Pin: the deep driver closes the ENTIRE 64-leaf z-order SB libvpx-exact
	// (mode/ref/interp/mv) — frame-1 SB0's committed mode decomposition is FULLY
	// CLOSED (the residual coefficient bitstream is the next frontier). The prefix
	// climbed in stages: 13 -> 46 by
	// disabling the coefficient trellis (vp9_optimize_b) in the genuine inter
	// super_block_yrd / super_block_uvrd producers for REALTIME speed >= 1 (cpu4,
	// do_trellis_opt == 0 via DISABLE_TRELLIS_OPT + optimize_coefficients = 0,
	// vp9_speed_features.c:485-488,553), then 46 -> 64 by wiring the genuine
	// larger-block intra RD producer (vp9FullRDInterIntraSB, the
	// ref_frame==INTRA_FRAME arm of vp9_rd_pick_inter_mode_sb) into the >= 8x8
	// leaf score (pickVP9FullRDInterIntraLeaf), so the lone whole-8x8 INTRA leaf
	// at mi(7,2) commits DC_PRED intra (mode=0, ref=INTRA, uv=DC, TX_4X4, mv=0)
	// exactly as libvpx — its intra DC this_rd (42633654) beats NEARESTMV
	// (51074099), NEWMV(-26,60) (43705965) and NEARMV (44857337) on the identical
	// post-ref/post-skip-bit RD basis (libvpx ground truth, $TMPDIR vpxenc
	// rd_pick_inter_mode_sb intra-arm fprintf 2026-06-05: rate_y=98860 dist_y=77516
	// rate_uv=14341 dist_uv=7663 mbmode_cost=489 uv_mode_cost=560 rate2=116746
	// dist2=85179 ref_cost=2473 skip0=23). The committed intra mv is 0
	// (vp9_rdopt.c:3990, the full-RD intra arm, NOT the nonrd INVALID_MV at
	// vp9_pickmode.c:2645). Asserting a floor (not the exact count) lets future
	// frontier work (later SBs / later frames) extend without editing this test,
	// while catching a regression below the milestone.
	const minPrefix = 64
	if matched < minPrefix {
		fd := "none"
		if firstDiffSet {
			fd = string(rune('0'+firstDiff[0])) + "," + string(rune('0'+firstDiff[1]))
		}
		t.Fatalf("deep use-partition closed z-order prefix = %d (< %d); "+
			"first diverging leaf mi(%s). The genuine MV search / pred-mv "+
			"threading, the model_rd_for_sb + ref_best_rd breakout "+
			"(vp9InterUseDeepRDRefBestRD), or the do_trellis_opt gate "+
			"(vp9DoTrellisOptInterY) regressed.", matched, minPrefix, fd)
	}
	if matched >= 64 {
		t.Logf("deep use-partition: %d/64 z-order leaves libvpx-exact "+
			"(mode/ref/interp/mv) — frame-1 SB0 mode decomposition FULLY CLOSED",
			matched)
	} else {
		t.Logf("deep use-partition: %d/64 leading z-order leaves libvpx-exact "+
			"(mode/ref/interp/mv); first divergence at mi(%d,%d)",
			matched, firstDiff[0], firstDiff[1])
	}
}

// vp9SB64ZOrder8x8 returns the 64 (miRow, miCol) positions of a 64x64 superblock
// in the depth-first split recursion order (64->32->16->8) that both libvpx
// rd_use_partition and govpx writeVP9ModesSb walk.
func vp9SB64ZOrder8x8() [][2]int {
	var out [][2]int
	var rec func(r, c, size int)
	rec = func(r, c, size int) {
		if size == 1 {
			out = append(out, [2]int{r, c})
			return
		}
		h := size / 2
		rec(r, c, h)
		rec(r, c+h, h)
		rec(r+h, c, h)
		rec(r+h, c+h, h)
	}
	rec(0, 0, 8)
	return out
}
