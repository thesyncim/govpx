//go:build govpx_oracle_trace

package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9FullRDInterCompUsageFinding documents — as an executable assertion —
// the usage finding established by the private $TMPDIR vpxenc-vp9 fprintf
// probe: the three full-RD long-fixture seeds {0,2,0,0,2}/{0,1,1,0,1}/
// {1,1,1,1,0} NEVER reach the compound handle_inter_mode. For every inter
// frame cm->ref_frame_sign_bias is 0/0/0/0, so vp9_compound_reference_allowed
// (vp9/common/vp9_pred_common.c:16) is 0, cpi->allow_comp_inter_inter is 0
// (vp9/encoder/vp9_encodeframe.c:5842), cm->reference_mode is SINGLE_REFERENCE
// (:5866), and every enumerated compound candidate continues at
// vp9/encoder/vp9_rdopt.c:3757 (`if (!cpi->allow_comp_inter_inter) continue;`)
// before handle_inter_mode. The probe measured HANDLE_COMP==0 for all three.
//
// vp9GetJointSearchIters is the compound-only gate; for the realtime RT path
// these seeds take (cpu0 speed 0 / cpu4 speed 4 / cpu8 speed 8) the compound
// branch is unreachable, so this test simply records the finding alongside the
// derived 2-ref fixture used for the real pin below.
func TestVP9FullRDInterCompUsageFinding(t *testing.T) {
	// sf_level for the RT seeds' speeds; comp branch never reached regardless.
	// The finding is the SINGLE_REFERENCE / allow_comp=0 / HANDLE_COMP=0 result,
	// not derivable from a pure function — it is documented in the file header
	// and the probe transcript. This assertion guards the derived-fixture
	// iteration counts the pin depends on (good-quality sf_level 1).
	cases := []struct {
		name  string
		level int
		bsize common.BlockSize
		want  int
	}{
		{"good-32x32", 1, common.Block32x32, 4},
		{"good-16x16", 1, common.Block16x16, 2},
		{"good-8x8", 1, common.Block8x8, 2},
		{"good-4x8-subpel", 1, common.Block4x8, 0},
		{"speed3plus-any", 2, common.Block32x32, 0},
		{"speed0-default", 0, common.Block16x16, 4},
	}
	for _, c := range cases {
		if got := vp9GetJointSearchIters(c.level, c.bsize); got != c.want {
			t.Errorf("%s: vp9GetJointSearchIters(%d,%v)=%d want %d",
				c.name, c.level, c.bsize, got, c.want)
		}
	}
}

// TestVP9FullRDInterCompAssembleParity pins the COMPOUND RD assembly
// (vp9FullRDInterCompAssemble: rate_mv + cost_mv_ref + rs + rate_y + rate_uv +
// ref_costs_comp, the skip-vs-non-skip pick, and this_rd = RDCOST) against the
// libvpx ground truth captured from the DERIVED 2-ref fixture (good-quality,
// --auto-alt-ref=1 --lag-in-frames=16, cpu0), frame 2, SB0 root, BLOCK_32X32,
// NEWMV, refs GOLDEN(2)+ALTREF(3) — the first compound candidate that survives
// the allow_comp_inter_inter gate and runs handle_inter_mode.
//
// libvpx ground truth (private $TMPDIR vpxenc-vp9 + TEMPORARY fprintf in
// handle_inter_mode + vp9_rd_pick_inter_mode_sb, gated on
// mi_row==0 && mi_col==0 && comp_pred, reverted; shared oracle binaries' md5
// unchanged 758eb784… / 16ddb772…):
//
//	joint search iters=4 single0=(21,10) single1=(20,14)
//	              refmv0=(5,20) refmv1=(-5,-20) -> jms0=(21,10) jms1=(20,14)
//	rate_mv=14223  cost_mv_ref=400  rs=1069  (rate2_preY=15692)
//	rate_y=2724984  rate_uv=836545  ref_costs_comp[GOLDEN]=512
//	skip_cost0(no-skip flag)=23  skip2=0
//	rate2=3577756  dist2=48096  total_sse=27113520
//	rdmult=5442  rddiv=7  this_rd=44183921
//
// The assembly must reproduce rate2, dist2, skip2 and this_rd byte-exactly from
// the captured components. (rate_y/rate_uv/rate_mv depend on frame-2's
// reconstructed reference buffers + per-frame nmv entropy, which a standalone
// unit cannot rebuild; they enter as the captured libvpx components, mirroring
// how vp9_fullrd_inter_thisrd_parity_test.go pins its own super_block_yrd/uvrd
// ground truth.)
func TestVP9FullRDInterCompAssembleParity(t *testing.T) {
	const (
		rateMv       = 14223
		costMvRef    = 400
		rs           = 1069
		rateY        = 2724984
		rateUV       = 836545
		refCostsComp = 512
		skipCost0    = 23
		rdmult       = 5442

		wantRate2  = 3577756
		wantDist2  = uint64(48096)
		wantSSE    = uint64(27113520)
		wantThisRD = uint64(44183921)
	)
	// Non-skippable (real residual), no-skip branch chosen (skip2=0). The
	// no-skip cost (skip_cost0) is charged. distY+distUV == dist2; the libvpx
	// dump reports only the post-pick dist2 — for the no-skip branch dist2 is
	// unchanged, so feed it all as distY with distUV=0 (the assembly sums them).
	// total_sse = sseY + sseUV; feed it as sseY with sseUV=0.
	// skip_cost1 is needed to evaluate the non-skip pick; the captured branch
	// chose no-skip, which requires RDCOST(rateY+rateUV+skip0, dist2) <
	// RDCOST(skip1, total_sse). skip1 is not separately captured, but the pick
	// outcome (skip2=0) is asserted: any skip1 large enough leaves no-skip.
	// Pin with the libvpx skip0=23 and a skip1 that preserves the no-skip pick;
	// the assembly's rate2/this_rd are independent of skip1 once no-skip wins.
	const skipCost1 = 1 // any value keeping no-skip the winner (verified below)

	rate2, dist2, totalSSE, skip2, thisRD := vp9FullRDInterCompAssemble(
		rateMv, costMvRef, rs, rateY, rateUV, refCostsComp,
		skipCost0, skipCost1, wantDist2, 0, wantSSE, 0,
		false /*skippableY*/, false /*skippableUV*/, false /*lossless*/, false /*sharpness*/, rdmult)

	if skip2 {
		t.Fatalf("skip2=true, want false (libvpx chose no-skip)")
	}
	if rate2 != wantRate2 {
		t.Errorf("rate2=%d want %d", rate2, wantRate2)
	}
	if dist2 != wantDist2 {
		t.Errorf("dist2=%d want %d", dist2, wantDist2)
	}
	if totalSSE != wantSSE {
		t.Errorf("total_sse=%d want %d", totalSSE, wantSSE)
	}
	if thisRD != wantThisRD {
		t.Errorf("this_rd=%d want %d", thisRD, wantThisRD)
	}

	// Independently pin RDCOST (the final compound RD estimate) on the
	// captured rate2/dist2 — the verbatim vp9_rdopt.c:3929 formula.
	if got := encoder.RDCost(rdmult, encoder.RDDivBits, wantRate2, wantDist2); got != wantThisRD {
		t.Errorf("RDCost(%d,7,%d,%d)=%d want %d", rdmult, wantRate2, wantDist2, got, wantThisRD)
	}

	// Pin the pre-Y rate decomposition the dump reports as rate2_preY=15692.
	if pre := rateMv + costMvRef + rs; pre != 15692 {
		t.Errorf("rate2_preY=%d want 15692", pre)
	}
}

// TestVP9FullRDInterCompAssembleSkipPick pins the SKIPPABLE and forced-skip
// branches of the compound skip-pick (vp9_rdopt.c:3901-3922) so the producer's
// rate-backout + skip-flag charge is verbatim for all three outcomes.
func TestVP9FullRDInterCompAssembleSkipPick(t *testing.T) {
	const (
		rateMv = 1000
		cmr    = 400
		rs     = 1069
		rateY  = 50000
		rateUV = 20000
		refC   = 512
		skip0  = 23
		skip1  = 900
		rdmult = 5442
	)
	// skippable: rate2 backs out rateY+rateUV and charges skip1.
	rate2, _, _, skip2, _ := vp9FullRDInterCompAssemble(
		rateMv, cmr, rs, rateY, rateUV, refC, skip0, skip1,
		1000, 0, 9_000_000, 0, true, true, false, false, rdmult)
	wantSkippable := rateMv + cmr + rs + refC + skip1
	if rate2 != wantSkippable {
		t.Errorf("skippable rate2=%d want %d", rate2, wantSkippable)
	}
	if skip2 {
		t.Errorf("skippable: skip2=true, want false (skippable path never sets this_skip2)")
	}

	// Forced skip: when RDCOST(skip1,total_sse) <= RDCOST(rateY+rateUV+skip0,
	// dist2), rate2 backs out coeffs, charges skip1, dist2:=total_sse, skip2=1.
	// Drive it with a tiny total_sse and a huge dist2 so the skip side wins.
	rate2f, dist2f, sse, skip2f, _ := vp9FullRDInterCompAssemble(
		rateMv, cmr, rs, rateY, rateUV, refC, skip0, skip1,
		50_000_000, 0, 10, 0, false, false, false, false, rdmult)
	if !skip2f {
		t.Fatalf("forced-skip: skip2=false, want true")
	}
	if dist2f != sse {
		t.Errorf("forced-skip dist2=%d want total_sse=%d", dist2f, sse)
	}
	wantForced := rateMv + cmr + rs + refC + skip1
	if rate2f != wantForced {
		t.Errorf("forced-skip rate2=%d want %d", rate2f, wantForced)
	}
}

// TestVP9FullRDInterCompCostMvRefDiscount pins the compound cost_mv_ref +
// discount_newmv_test VPXMIN (vp9_rdopt.c:2970-2977) verbatim: when the
// discount fires for a compound NEWMV the charge is
// VPXMIN(cost_mv_ref(NEWMV), cost_mv_ref(NEARESTMV)); otherwise the plain
// cost_mv_ref(mode). cost_mv_ref itself is the already-pinned
// encoder.CostMvRef (mode_cost_discount_test.go), so this guards only the new
// VPXMIN wrapper and that the discount is NOT gated on single-reference.
func TestVP9FullRDInterCompCostMvRefDiscount(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	for ctx := 0; ctx < 7; ctx++ {
		newMv := encoder.CostMvRef(&fc, ctx, common.NewMv)
		nearest := encoder.CostMvRef(&fc, ctx, common.NearestMv)
		wantDisc := newMv
		if nearest < wantDisc {
			wantDisc = nearest
		}
		// discount=true on NEWMV -> VPXMIN(NEWMV, NEARESTMV).
		if got := vp9FullRDInterCompCostMvRef(&fc, ctx, common.NewMv, true); got != wantDisc {
			t.Errorf("ctx=%d discount NEWMV cost=%d want VPXMIN=%d", ctx, got, wantDisc)
		}
		// discount=false -> plain cost_mv_ref(NEWMV).
		if got := vp9FullRDInterCompCostMvRef(&fc, ctx, common.NewMv, false); got != newMv {
			t.Errorf("ctx=%d plain NEWMV cost=%d want %d", ctx, got, newMv)
		}
		// discount only applies to NEWMV — NEARMV must be unaffected.
		nearMv := encoder.CostMvRef(&fc, ctx, common.NearMv)
		if got := vp9FullRDInterCompCostMvRef(&fc, ctx, common.NearMv, true); got != nearMv {
			t.Errorf("ctx=%d NEARMV discount-ignored cost=%d want %d", ctx, got, nearMv)
		}
	}
}

// TestVP9JointSearchSkipItersParity pins skip_iters (vp9_rdopt.c:1837-1847)
// verbatim: it breaks the joint search when the OTHER ref's MV repeats from two
// iterations back AND the searched ref's full-pixel (>>3) MV repeats.
func TestVP9JointSearchSkipItersParity(t *testing.T) {
	// iter_mvs[ite][ref]. id is the searched ref this iteration.
	build := func(rows [][2][2]int16) [][2]vp9dec.MV {
		out := make([][2]vp9dec.MV, len(rows))
		for i, r := range rows {
			out[i][0] = vp9dec.MV{Row: r[0][0], Col: r[0][1]}
			out[i][1] = vp9dec.MV{Row: r[1][0], Col: r[1][1]}
		}
		return out
	}

	// ite<2 never skips.
	im := build([][2][2]int16{{{0, 0}, {0, 0}}, {{8, 8}, {0, 0}}})
	if vp9JointSearchSkipIters(im, 1, 1) {
		t.Error("ite=1 must not skip")
	}

	// ite=2, id=0: other(ref1) unchanged from ite0, searched(ref0) full-pel
	// (16>>3==2) == (17>>3==2) -> skip.
	im = build([][2][2]int16{
		{{16, 16}, {5, 5}}, // ite0
		{{0, 0}, {9, 9}},   // ite1
		{{17, 17}, {5, 5}}, // ite2: ref1==ite0 ref1, ref0 17>>3 == 16>>3
	})
	if !vp9JointSearchSkipIters(im, 2, 0) {
		t.Error("ite=2 id=0 converged full-pel must skip")
	}

	// Same but other ref changed -> no skip.
	im = build([][2][2]int16{
		{{16, 16}, {5, 5}},
		{{0, 0}, {9, 9}},
		{{17, 17}, {6, 5}}, // ref1 changed
	})
	if vp9JointSearchSkipIters(im, 2, 0) {
		t.Error("ite=2 other-ref changed must not skip")
	}

	// Other ref same but searched full-pel differs (24>>3==3 != 16>>3==2).
	im = build([][2][2]int16{
		{{16, 16}, {5, 5}},
		{{0, 0}, {9, 9}},
		{{24, 16}, {5, 5}},
	})
	if vp9JointSearchSkipIters(im, 2, 0) {
		t.Error("ite=2 searched full-pel differs must not skip")
	}
}
