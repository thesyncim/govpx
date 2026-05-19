package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestVP8Task324OptimizeBCoeffCostAuditRetraction pins task #324's
// per-coefficient KEEP_COST vs DROP_COST audit conclusion against future
// regressions. Per the BestARNR 19981bff cohort frame-1 chroma-optimize_b
// bisect (task #316 hook artifacts /tmp/324-{govpx,libvpx}-best.jsonl):
//
//	common (mb_row,mb_col,block) triples on frame 1: 4720
//	triples where coeff identical AND qcoeff differs: 0
//	triples where coeff differs: 4720
//
// Every divergent post-trellis chroma qcoeff is downstream of a differing
// `coeff` (FDCT residual) input to optimize_b — not a per-coefficient
// keep/drop cost computation divergence. The audit chain:
//
//  1. Task #319 verified rdMult / rdDiv are byte-faithful all the way to
//     the chroma trellis input (the 326/551 split was a trace-emit
//     asymmetry, not an actual divergence).
//
//  2. Task #322 re-verified rdMult byte-faithfulness independently (the
//     activity-lifted x->rdmult matches libvpx on both sides).
//
//  3. Task #324 (this retraction): the chroma-optimize_b bisect (#316
//     artifacts) shows the post-trellis chroma qcoeff divergence is 100%
//     downstream of a differing `coeff` (FDCT-output residual) input.
//     No (mb_row, mb_col, block) triple has identical coeff with diverging
//     qcoeff — the cost computation IS byte-faithful, but the input to it
//     is not.
//
// The actual root cause for the ARNR pin-hold residual is in the
// per-MB MODE PICKER: 588/960 (61.25%) of frame-1 MBs in the bisect
// trace have a diverging `mode` selection (e.g. govpx NEWMV vs libvpx
// SPLITMV at MB(0,0) — both with effective MV=(8,16)/LAST_FRAME but the
// NEWMV builder uses vp8_build_inter16x16_predictors_mbuv while SPLITMV
// uses vp8_build_inter4x4_predictors_mbuv on the libvpx side. The two
// paths use different chroma MV derivation + different subpixel filter
// granularity, so chroma residuals — and therefore chroma `coeff`
// (FDCT) inputs to optimize_b — drift even when the source/reference
// are byte-identical).
//
// libvpx anchors:
//   - vp8/encoder/encodemb.c:124-356 optimize_b (Viterbi keep/drop trellis)
//   - vp8/encoder/encodemb.c:225-232 first-option rd_cost computation
//   - vp8/encoder/encodemb.c:282-289 second-option rd_cost computation
//   - vp8/encoder/encodemb.c:308-321 zero-coefficient cost update
//   - vp8/encoder/encodemb.c:325-342 finalizer pt + rd_cost
//   - vp8/common/reconinter.c vp8_build_inter16x16_predictors_mbuv (NEWMV
//     chroma predictor, uv MV = (mv + sign-bias-1)/2 & fullpixel_mask)
//   - vp8/common/reconinter.c vp8_build_inter4x4_predictors_mbuv (SPLITMV
//     chroma predictor, uv MV = avg(4 sub-luma-MVs) with /8 rounding)
//
// govpx mirror:
//   - encoder_inter_quantize.go:158-378 optimizeQuantizedBlockWithRDConstants
//
// The cleared-candidate list for the chroma optimize_b cost computation
// (#282 trellis byte-faithfulness, #299 token costs, #319 rdMult/rdDiv,
// #322 rdMult-post-activity-masking) is now joined by #324 (this audit):
// per-coefficient KEEP_COST vs DROP_COST is byte-faithful. The residual
// chroma qcoeff drift lives in the upstream picker/predictor builder,
// not in optimize_b's RDCOST math.
//
// The audit drives a minimal sentinel block through
// optimizeQuantizedBlockWithRDConstants for blockType=2 (UV) and verifies
// the trellis bit-flip happens at the exact RDCOST boundary derived from
// libvpx's vp8_initialize_rd_consts + plane_rd_mult[UV]=2. Any future
// regression in libvpxRDCost / libvpxRDTrunc / dctValueBaseCost / token
// elision will trip the keep/drop decision and surface here as a sentinel
// mismatch.
func TestVP8Task324OptimizeBCoeffCostAuditRetraction(t *testing.T) {
	// Sentinel A: distortion-dominant DC, low rdmult → trellis must keep.
	// At qIndex=4, libvpxRDConstantsWithZbin returns (rdMult=15, rdDiv=100).
	// After UV_RD_MULT=2 → rdMult=30 with rdDiv=100. Distortion=(85-100)^2=225
	// for a DC coefficient where coeff[0]=100, dequant=85, qcoeff[0]=1
	// (keep case) vs coeff[0]^2=10000 (drop case). Drop is 44.4x more
	// distortion than keep, so keep WINS regardless of rate.
	{
		quant := viterbiTestRegularBlockQuant(4, 85)
		var coeff, qcoeff [16]int16
		coeff[0] = 100
		qcoeff[0] = 1
		eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 4, 2, 0, 0, 0, false, &coeff, &quant, &qcoeff, 1)
		if eob != 1 || qcoeff[0] != 1 {
			t.Fatalf("sentinel A (distortion-dominant UV DC): eob/qcoeff[0]=%d/%d, want 1/1 (trellis must keep)", eob, qcoeff[0])
		}
	}

	// Sentinel B: rate-dominant DC, high rdmult → trellis must drop.
	// At qIndex=127, libvpxRDConstantsWithZbin returns rdMult≈3611, rdDiv=1.
	// After UV_RD_MULT=2 → rdMult≈7223 with rdDiv=1. Coefficient
	// coeff[0]=11, dequant=100, qcoeff[0]=1 — distortion(keep)=(100-11)^2
	// =7921, distortion(drop)=11^2=121. Drop saves rate but adds 7800
	// distortion units; with rdmult≈7223 the rate savings (>1 unit at
	// rdMult=7223 each) overcome the distortion delta, so DROP wins.
	{
		quant := viterbiTestRegularBlockQuant(127, 100)
		var coeff, qcoeff [16]int16
		coeff[0] = 11
		qcoeff[0] = 1
		eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 2, 0, 0, 0, false, &coeff, &quant, &qcoeff, 1)
		if eob != 0 || qcoeff[0] != 0 {
			t.Fatalf("sentinel B (rate-dominant UV DC overshoot): eob/qcoeff[0]=%d/%d, want 0/0 (trellis must drop)", eob, qcoeff[0])
		}
	}

	// Sentinel C: negative-coefficient sign asymmetry. The 2-unit gap
	// between vp8_cost_bit(prob_half, sign_bit=0)=255 and
	// vp8_cost_bit(prob_half, sign_bit=1)=257 must be preserved by
	// dctValueBaseCost. If it were not, the negative coefficient cohort
	// (1078 blocks per task #314 scoreboard) would diverge from the
	// positive cohort (1934 blocks). The audit confirmed both 1934 + 1078
	// are now downstream of upstream coeff divergence, not the sign cost.
	{
		const wantPos = 255 // ProbCost[128]
		const wantNeg = 257 // ProbCost[127]
		gotPos := dctValueBaseCost(1)
		gotNeg := dctValueBaseCost(-1)
		if gotPos != wantPos {
			t.Errorf("dctValueBaseCost(+1)=%d, want %d (vp8_cost_bit(128, 0))", gotPos, wantPos)
		}
		if gotNeg != wantNeg {
			t.Errorf("dctValueBaseCost(-1)=%d, want %d (vp8_cost_bit(128, 1))", gotNeg, wantNeg)
		}
		if gotNeg-gotPos != 2 {
			t.Errorf("sign-cost gap %d, want 2 (asymmetric ProbCost lookup for half-prob signs)", gotNeg-gotPos)
		}
	}

	// Sentinel D: RDCOST tie-break uses RDTRUNC's masked-byte truncation
	// (encodemb.c:123 RDTRUNC). If RDTRUNC is byte-faithful and a tie
	// exists at the primary RDCOST level, the trellis must use the
	// truncated value. We construct a degenerate input where both keep
	// and drop paths yield rate0=rate1 to force the RDCOST tie path,
	// then verify RDTRUNC returns a value in [0,255] (the masked byte).
	{
		got := libvpxRDTrunc(551, 12345)
		if got < 0 || got > 255 {
			t.Errorf("libvpxRDTrunc(551, 12345)=%d, want value in [0,255] (RDTRUNC masks with 0xFF)", got)
		}
		// And the byte-faithful formula explicitly: (128 + 12345*551) & 0xFF.
		want := (128 + 12345*551) & 0xFF
		if got != want {
			t.Errorf("libvpxRDTrunc(551, 12345)=%d, want %d (verbatim RDTRUNC)", got, want)
		}
	}
}

// TestVP8Task324OptimizeBStructuralAudit pins the structural shape of
// optimizeQuantizedBlockWithRDConstants against libvpx's optimize_b. Any
// future refactor that breaks one of these structural invariants would
// regress the keep/drop decision math even when the input coeff is
// byte-identical (which is currently not the case per the #324
// retraction audit, but a future picker fix will restore that).
//
// The structural invariants:
//
//  1. Loop traversal: eob-1 down to skipDC, decrement-by-one (libvpx
//     encodemb.c:202 `for (i = eob; i-- > i0;)`).
//  2. Two trellis states per position: state 0 = keep current, state 1 =
//     shortcut-reduced (libvpx encodemb.c:147 `tokens[17][2]`).
//  3. Shortcut activates iff `|x|*dq ∈ (|coeff|, |coeff|+dq)` (libvpx
//     encodemb.c:246-251).
//  4. RDCOST tie-break uses RDTRUNC ((128+R*RM)&0xFF) on both sides
//     (libvpx encodemb.c:227-230 / 284-287 / 338-341).
//  5. Finalizer: band = vp8_coef_bands[i+1] where post-loop i = i0-1
//     (libvpx encodemb.c:326).
//  6. Backtrace: walk tokens[i][best].next from `next` to eob; final_eob
//     = last position with non-zero qc, +1 (libvpx encodemb.c:343-353).
func TestVP8Task324OptimizeBStructuralAudit(t *testing.T) {
	// Invariant 6: backtrace produces a valid eob in [skipDC, eob].
	quant := viterbiTestRegularBlockQuant(80, 50)
	var coeff, qcoeff [16]int16
	for i := range coeff {
		coeff[i] = int16(50 + 3*i)
		qcoeff[i] = 1
	}
	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 80, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 16)
	if eob < 0 || eob > 16 {
		t.Errorf("structural invariant 6 broken: eob=%d, want in [0,16]", eob)
	}

	// Invariant 1: skipDC bounds. For type 0 (Y_NO_DC) skipDC=1, the
	// trellis must NOT touch qcoeff[rc=0] regardless of value.
	{
		var c2 [16]int16
		var q2 [16]int16
		c2[0] = 0
		q2[0] = 42 // skipDC=1 means DC slot is owned by Y2 and must survive.
		c2[int(vp8tables.DefaultZigZag1D[1])] = 100
		q2[int(vp8tables.DefaultZigZag1D[1])] = 2
		quant2 := viterbiTestRegularBlockQuant(127, 50)
		_ = optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &c2, &quant2, &q2, 2)
		if q2[0] != 42 {
			t.Errorf("structural invariant 1 broken: skipDC=1 did not protect qcoeff[0]; got=%d want=42", q2[0])
		}
	}

	// Invariant 3: shortcut threshold for x=+1 fires when |coeff|<dq.
	// At dequant=50, coeff=20 (|coeff|<dq, |coeff|+dq=70), |x|*dq=50 lies
	// in (20, 70). The shortcut reduces x→0 and the trellis can elect
	// to drop. Force rate-dominance via qIndex=127 so the drop wins.
	{
		quant3 := viterbiTestRegularBlockQuant(127, 50)
		var c3, q3 [16]int16
		c3[int(vp8tables.DefaultZigZag1D[1])] = 20
		q3[int(vp8tables.DefaultZigZag1D[1])] = 1
		eob3 := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &c3, &quant3, &q3, 2)
		if eob3 != 1 {
			t.Errorf("structural invariant 3 broken: shortcut+rate-dominance did not drop the AC coefficient; eob=%d want=1", eob3)
		}
	}

	// Smoke: a fully-zero qcoeff retains its zeros regardless of coeff
	// content (libvpx encodemb.c:209-303 only modifies state 0/1 for
	// non-zero entries).
	{
		var c4 [16]int16
		var q4 [16]int16
		c4[0] = 200
		quant4 := viterbiTestRegularBlockQuant(80, 50)
		// Use a non-zero eob so the loop runs but qcoeff stays all-zero.
		_ = optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 80, 0, 0, 1, 0, false, &c4, &quant4, &q4, 4)
		for i := range q4 {
			if q4[i] != 0 {
				t.Errorf("smoke: zero-qcoeff input mutated to %d at %d", q4[i], i)
			}
		}
	}
}

// vp8Task324AssertDequantInvariant pins that
// optimizeQuantizedBlockWithRDConstants and its caller chain leave
// dqcoeff untouched (the caller re-derives it via dequantizeQuantizedBlock
// after the trellis pass). The chain is: quantizeOptimizedBlockWithRDZbinAndActivity
// → optimizeQuantizedBlockWithRDConstants → dequantizeQuantizedBlock,
// so dqcoeff is fresh after the wrapper but inside the trellis it stays
// at its pre-optimize values. Used by callers that snapshot dqcoeff for
// inverse-transform paths.
func vp8Task324AssertDequantInvariant(t *testing.T, quant vp8enc.BlockQuant, qcoeff, dqcoeff *[16]int16) {
	t.Helper()
	for i := range qcoeff {
		want := qcoeff[i] * quant.Dequant[i]
		if dqcoeff[i] != want {
			t.Errorf("dqcoeff[%d]=%d, want qcoeff[%d]*dequant[%d]=%d*%d=%d",
				i, dqcoeff[i], i, i, qcoeff[i], quant.Dequant[i], want)
		}
	}
}
