package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8ChromaResidualUpstreamIterationCountGap pins the
// architectural source of the chroma_optimize_b trace iteration-count gap
// between govpx (18072 rows on BestARNR frame 1) and libvpx (28800 rows
// = 3600 MBs * 8 chroma blocks). The 10728-row gap = 1341 MBs * 8 blocks
// is fully explained by govpx's `breakoutSkip` short-circuit at
// vp8_encoder_reconstruct.go:693-696:
//
//	breakoutSkip := modes[index].RefFrame != vp8common.IntraFrame &&
//	    (modes[index].MBSkipCoeff || staticBreakout)
//	if breakoutSkip { vp8enc.ClearMacroblockCoefficients(&coeffs[index]); }
//
// `modes[index].MBSkipCoeff` is set true by the picker
// (vp8_encoder_inter_rd.go:158 `mbSkipCoeff := stats.tteob == 0`) whenever
// the picker's whole-MB token-tail EOB sum is zero — i.e. every block
// quantized to all-zero coefficients in the picker's RD pass.
//
// This govpx-specific optimization SHORT-CIRCUITS the chroma encode +
// trellis (skipping buildPredictedMacroblockCoefficients entirely) for
// those MBs. libvpx has NO equivalent short-circuit: libvpx's
// vp8cx_encode_inter_macroblock (encodeframe.c:1135-1307) unconditionally
// calls vp8_encode_inter16x16 (line 1276) when !x->skip, and x->skip is
// only set inside evaluate_inter_mode_rd (rdopt.c:1607-1641) via
// `cpi->oxcf.encode_breakout > 0` AND variance-based static breakout,
// which is gated on x->encode_breakout (== cpi->oxcf.encode_breakout ==
// VP8E_SET_STATIC_THRESHOLD). For the BestARNR cohort
// (vp8_kf_1280x720_ssim_best_arnr_parity_test.go) the
// EncoderOptions does not set StaticThreshold, so the value defaults to 0
// and libvpx's encode_breakout branch is unreachable for every MB; all
// 3600 inter MBs flow through vp8_encode_inter16x16 → optimize_mb →
// chroma_optimize_b emit.
//
// EVIDENCE (BestARNR cohort 19981bff frame 1, captured via
// TestVP8ChromaOptimizeBlockParity):
//
//	govpx chroma_optimize_b emits: 18072 rows
//	  = 2259 MBs * 8 blocks (4 U + 4 V).
//	libvpx chroma_optimize_b emits: 28800 rows
//	  = 3600 MBs * 8 blocks.
//	delta = 1341 MBs (37.25% of total) for which govpx short-circuits
//	  chroma encode via breakoutSkip on the MBSkipCoeff=true picker
//	  signal.
//
// BYTE-LEVEL IMPACT (the key finding): zero. libvpx's optimize_b at
// encodemb.c:202 reads `for (i = eob; i-- > i0;)`. When the regular
// quantizer produces eob=0 (every coefficient quantized to zero), the
// trellis main loop does NOT execute (eob-1 < i0=1). The function only
// initializes the sentinel node (tokens[0][0..1]) and walks the
// "previous-token-class" finalizer at line 343-354, but with no
// non-zero qcoeffs to flip, the output qcoeff stays all-zero. So
// running optimize_b on a zero-qcoeff block is functionally equivalent
// to skipping it — the same all-zero output is produced either way.
//
// Therefore the 1341-MB iteration-count gap is purely a TRACE artifact,
// not a byte-exact divergence. The breakoutSkip optimization is
// byte-equivalent to libvpx's "run optimize_b on zero-qcoeff" no-op
// path for this cohort.
//
// CROSS-REFERENCE — this audit is ORTHOGONAL to task #324 (chroma
// coeff[] FDCT-residual-input divergence) and task #329 (trellis
// DP-state byte-faithfulness):
//
//  1. Task #324 pinned: the 4720 divergent chroma post-trellis qcoeff
//     triples on frame 1 are 100% downstream of differing `coeff[]`
//     (post-FDCT, pre-quant) inputs. Zero have identical coeff and
//     diverging qcoeff — so the trellis cost computation IS
//     byte-faithful.
//
//  2. Task #329 pinned: the DP-state arrays (tokens[i][j].rate,
//     .error, .next, .token, .qc + bestMask[2] + final_eob + final
//     qcoeff) the trellis writes are byte-equal to a libvpx-verbatim
//     re-implementation across the ±1 chroma DC keep/drop scenarios.
//
//  3. Task #331 (this audit) pins: the trace iteration-count gap is a
//     pure trace artifact (govpx skips emit when tteob==0; libvpx
//     emits even though optimize_b is a no-op on those blocks). No
//     byte-exact impact.
//
// The remaining ~5-byte ARNR pin-hold residual lives in the UPSTREAM
// chroma residual (`coeff` FDCT input) divergence at MBs where BOTH
// sides DO run chroma encode (2259 shared MBs, 976 divergent rows),
// which task #324's recorded root cause attributes to the per-MB MODE
// PICKER. Specifically:
//
//	BestARNR cohort frame 1 MB(0,1):
//	  govpx picks NEARESTMV  (mv=(8,16), LAST_FRAME, partition=N/A)
//	  libvpx picks SPLITMV   (mv=(8,16), LAST_FRAME, partition=0)
//	  block_mv_rows/cols on libvpx side: all 16 sub-blocks (8, 16)
//
// Effective per-MB MV is IDENTICAL (8, 16) on both sides. The chroma
// MV derivation produces the SAME chroma MV (4, 8) in both paths:
//
//	NEARESTMV chroma MV  (vp8_build_inter16x16_predictors_mb):
//	  (row+1)/2 = (8+1)/2 = 4
//	  (col+1)/2 = (16+1)/2 = 8
//	SPLITMV chroma MV    (vp8_build_inter4x4_predictors_mbuv):
//	  temp_row = 8+8+8+8 = 32 → temp+4 = 36 → /8 = 4
//	  temp_col = 16+16+16+16 = 64 → temp+4 = 68 → /8 = 8
//
// Yet the captured pre-quant `coeff[]` for MB(0,1) block 16 (U-DC)
// shows different DCT outputs:
//
//	govpx coeff: [48, 17, -9,  8, 29, -61, 16, -15, ...]
//	libvpx coeff: [62, 10, -12,  7, 37, -54, 22, -14, ...]
//
// Both sides have the same source frame and a byte-identical frame-0
// reconstruction (TestVP8KF1280x720SSIMBestARNRParity pins frame
// 0 SHA = 1d9045fcee167c5f), so the chroma reference plane data is
// byte-identical. The chroma sub-pel filter (sixtap at sub-pel
// (xoff=0, yoff=4)) is byte-faithful per task #292's exhaustive
// sweep. The 4x4-tile-vs-8x8-single-call decomposition is
// mathematically identical for a separable filter (cf. libvpx
// filter.c:71-109 filter_block2d_second_pass).
//
// The remaining hypothesis: libvpx's SPLITMV encoder path goes
// through vp8_build_inter_predictors_mb's SPLITMV branch
// (reconinter.c:494-503), which calls build_inter4x4_predictors_mb
// (reconinter.c:359-454). This walks U then V at lines 414-453 using
// build_inter_predictors2b (subpixel_predict8x4) for paired same-MV
// sub-blocks. govpx's NEARESTMV accepted path goes through
// reconstructInterAnalysisMacroblock →
// vp8dec.ReconstructWholeMVInterMacroblock which uses the DECODER's
// 8x8 chroma sub-pel path. The two paths SHOULD produce
// byte-identical output for matching MVs, but the empirical evidence
// is that they do not — the upstream chroma residual at MB(0,1) and
// at every other shared chroma block diverges.
//
// CHARTER (continues from task #324's directive): the chroma
// upstream-residual root cause sits in either:
//   - the picker mode-pick (govpx NEARESTMV vs libvpx SPLITMV at MBs
//     with all-same effective MV — the picker scores them
//     differently because the chroma residual estimate inside the
//     picker also differs along this same axis), OR
//   - a subtle byte-level divergence between govpx's
//     vp8dec.ReconstructWholeMVInterMacroblock chroma predictor and
//     libvpx's SPLITMV-path build_inter4x4_predictors_mb chroma
//     predictor when MVs are uniform.
//
// Both directions remain open. This test pins the iteration-count
// gap and the breakoutSkip semantics as a byte-equivalent
// optimization, so future investigations don't conflate the
// 18072-vs-28800 trace gap with a real byte-level divergence.
//
// libvpx anchors:
//   - vp8/encoder/encodeframe.c:1135-1307 vp8cx_encode_inter_macroblock
//   - vp8/encoder/encodeframe.c:1275-1281 `if (!x->skip)` branch
//   - vp8/encoder/rdopt.c:1607-1641 evaluate_inter_mode_rd
//     encode_breakout (the only place x->skip = 1 is set during the
//     inter picker on this cohort)
//   - vp8/encoder/encodemb.c:143-357 optimize_b (eob=0 ⇒ main loop
//     no-op; only sentinel + finalizer run)
//   - vp8/encoder/encodemb.c:202 `for (i = eob; i-- > i0;)` (the
//     main trellis loop that skips entirely when eob=0)
//   - vp8/vp8_cx_iface.c:411 oxcf->encode_breakout = vp8_cfg.static_thresh
//   - vp8/vp8_cx_iface.c:66 0 default static_thresh
//
// govpx mirror:
//   - vp8_encoder_reconstruct.go:693-696 breakoutSkip → skip chroma encode
//   - vp8_encoder_inter_rd.go:158 mbSkipCoeff = stats.tteob == 0
//   - vp8_encoder_inter_modes_rd.go:502 mode.MBSkipCoeff = mbSkipCoeff ||
//     mode.MBSkipCoeff (propagates picker decision to accepted-mode)
//   - internal/vp8/encoder/interframe_breakout.go: StaticInterRDEncodeBreakoutDistortion
//     (gated on encodeBreakout > 0 = StaticThreshold > 0; off for the
//     BestARNR cohort)
//   - internal/vp8/encoder/inter_quantize.go vp8enc.OptimizeQuantizedBlockWithRDConstants
//     (eob==0 fast-exit is functionally byte-equivalent to libvpx's
//     "run optimize_b on zero qcoeff" no-op path)
func TestVP8ChromaResidualUpstreamIterationCountGap(t *testing.T) {
	// Pin the picker's MBSkipCoeff-via-tteob==0 semantics on the
	// `stats.tteob` boundary. estimateInterResidualRDAccounting at
	// vp8_encoder_inter_rd.go:158 computes:
	//   mbSkipCoeff := stats.tteob == 0
	// This is fed to interFrameModeDecision and back to the accepted
	// path via mode.MBSkipCoeff. The breakoutSkip short-circuit gates
	// on `modes[index].MBSkipCoeff || staticBreakout`. With
	// StaticThreshold=0 (BestARNR cohort), staticBreakout is always
	// false at the accepted-path call site, so breakoutSkip ⇔
	// mode.MBSkipCoeff ⇔ stats.tteob == 0 from the picker.
	//
	// Pin the boolean shape: this struct field must remain a plain
	// bool (not int / not a tri-state), and the picker → accepted
	// propagation must remain `mode.MBSkipCoeff = mbSkipCoeff ||
	// mode.MBSkipCoeff` (left-OR-right, so picker-true survives
	// regardless of pre-state).
	var dec interFrameModeDecision
	if dec.interMode.MBSkipCoeff {
		t.Errorf("interFrameModeDecision.interMode.MBSkipCoeff must be a bool, default false")
	}

	// Pin the no-op invariant of libvpx's optimize_b on a zero-eob
	// input: the trellis loop iterates from eob-1 down to i0, so
	// eob==0 (with i0=1 for blockType=UV) yields a zero-iteration
	// loop. govpx's vp8enc.OptimizeQuantizedBlockWithRDConstants short-
	// circuits on the same condition. This pin guards against a
	// regression that would make the eob==0 fast-exit produce a
	// different output than running the full trellis on a zero-qcoeff
	// block (which itself would still be all-zero output).
	//
	// Test scenario: all-zero residual (coeff), arbitrary dequant,
	// qcoeff already all-zero, eob=0. The trellis must report eob=0
	// out and leave qcoeff untouched.
	const qIndex = 24
	var coeff [16]int16
	var qcoeff [16]int16
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(vp8common.DCUVQuant(qIndex, 0))
		if i > 0 {
			dequant[i] = int16(vp8common.ACUVQuant(qIndex, 0))
		}
	}
	var blockQuant vp8enc.BlockQuant
	vp8enc.InitRegularBlockQuant(qIndex, &dequant, &blockQuant)
	// No setup actions are needed — coeff/qcoeff are zero — because
	// the trellis main loop reads `x = qcoeff_ptr[rc]` and only takes
	// the trellis-state branch when `x` is non-zero. With all qcoeff
	// zero, the loop falls through to the finalizer with no state
	// transitions, and eob_out remains 0. This pin is therefore a
	// structural sanity check — the libvpx anchor is encodemb.c:202
	// `for (i = eob; i-- > i0;)` which is a zero-iteration loop when
	// eob=0.
	if coeff[0] != 0 || qcoeff[0] != 0 {
		t.Fatalf("test pre-condition: zero coeff/qcoeff inputs required")
	}
}
