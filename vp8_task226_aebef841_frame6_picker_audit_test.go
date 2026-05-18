//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8Task226Aebef841Frame6PickerAudit pins task #226: residual frame-6
// divergence on regression_general_64x64_300kbps_sp0_f9_src0_aebef841 fuzz
// seed of FuzzOracleEncoderRuntimeControlTransitions (matchLimit=6 carveout
// in oracle_encoder_runtime_controls_fuzz_test.go).
//
// SEED REPRODUCER:
//
//	[]byte("020b00)a07") → 64x64, 300kbps CBR, cpu_used=0, panning sources,
//	9 frames. Frame 1: rtc:0+deadline:good+cpu:0. Frame 2:
//	setref:golden:panning:8+maxintra:0+gfboost:0+cq:4+...
//
// CURRENT STATE (matchLimit=6 carveout): frames 0-5 byte-MATCH, frame 6
// govpx=645 vs libvpx=762 MISMATCH, frames 7-8 byte-MATCH.
//
// DIVERGENCE ROOT CAUSE:
//
// Task #218 (e331af27) ported the verbatim libvpx SPLITMV skip-backout gate
// (encoder_inter_modes_rd_split.go:265 dropped `&& stats.rateUV == 0`).
// The fix matches vp8/encoder/rdopt.c:1700 calculate_final_rd_costs but
// surfaced a second-order picker delta at MB(2,1) frame 2 SPLITMV-GOLDEN
// that propagates to frame 6.
//
// SPECIFIC GAP - FRAME 2 MB(2,1) SPLITMV-GOLDEN (mode_index=17, partition=1):
//
// Probe captures (PROBE226 instrumentation, not landed):
//
// libvpx after rd_inter4x4_uv, before calculate_final_rd_costs:
//
//	rate_y=192 rate_uv=16376 rate2=20104
//	eobs_y=[0 0 16 16 0 0 16 16 0 0 16 16 0 0 16 16] ← right stripe NONZERO
//	eobs_uv=[0 0 0 0 0 0 0 0]
//	qcoeff[block 2]=[121 -7 8 3 -7 19 3 0 8 3 0 7 3 0 7 -3]
//	coeff[block 2] =[968 -58 64 25 -58 155 25 1 64 24 0 59 25 0 59 -26]
//	src_diff[block 2]=[169 105 105 105 29 29 29 29 0 0 0 0 -22 -22 42 42]
//	bmi.mv[block 2]=(-48,-96)
//
// libvpx tteob for SPLITMV (has_y2_block=0, rdopt.c:1689-1697):
//   - Y blocks: tteob += (eobs[i] > 0) → 8 (right stripe)
//   - UV blocks: tteob += eobs[16..23] → 0
//   - total tteob = 8 > 0 → NO skip backout, rate_uv stays at 16376, rate2=20474
//
// govpx at estimateInterSplitResidualRDAccounting:
//
//	partition=1 SegmentRate=3539 SegmentYRate=192 SegmentTTEOB=0
//	stats.rateUV=16376 stats.distortionUV=2 uvTTEOB=0 uvEOBs=[0 0 0 0 0 0 0 0]
//	mbSkipCoeff = (0+0 == 0) = TRUE → SKIP applied → rate_uv→0, rate2 = 5351
//
// The 15123-bit rate2 gap surfaces as ~59 bits projected_frame_size delta:
//   - frame 2 iter 1 Q=6: govpx proj=3552 vs libvpx 3611
//   - frame 2 iter 2 Q=4: govpx proj=5032 vs libvpx 5091
//
// Both sides exit frame 2 at Q=4 but with different RCF updates:
//   - govpx rcf=0.532000 vs libvpx 0.541500 (delta +0.0095)
//
// rcf drift carries through frames 3-5 (byte-match by Q convergence) and
// cascades at frame 6 iter 1 Q=7:
//   - govpx next_q=3, libvpx next_q=2
//
// Q delta reshuffles per-MB picker at frame 6 → byte mismatch.
//
// REAL ROOT CAUSE - Y PREDICTOR/RESIDUAL MISMATCH:
//
// At MB(2,1) frame 2 SPLITMV-GOLDEN partition=1 with bmi.mv[block=2]=
// (-48,-96) on BOTH sides, same reference (GOLDEN) and same source SHOULD
// produce same Y residual. libvpx's vp8_encode_inter_mb_segment emits
// src_diff[block 2]=[169,105,105,105,...] producing coeff[DC]=968 and
// quantize eob=16. govpx's selectShape → selectMotion →
// labelRD.rateDistortion path (encoder_inter_split.go:509) uses
// predictSplitMotionBlock4x4 + fillSplitMotionResidual4x4 with the same
// (block, MV, ref) inputs but produces all-zero qcoeffs and eob=0 for
// every Y block (SegmentTTEOB=0).
//
// UV residual matches (uvTTEOB=0 on both sides); divergence is restricted
// to the Y label-RD per-block predictor/residual/quantize path.
//
// Candidate sub-investigations for next iteration:
//
//  1. Byte-for-byte compare govpx predictSplitMotionBlock4x4(block=2,
//     MV=(-48,-96), ref=GOLDEN) against libvpx vp8_build_inter_predictors_b
//     for same reference state.
//
//  2. Verify govpx's GOLDEN reference at frame 2 entry matches libvpx's
//     after `setref:golden:panning:8` control at frame 1
//     (setReferencePanningApply with imageIndex=8).
//
//  3. Verify govpx selectShape picks same WINNING partition=1 per-block
//     MV assignments as libvpx vp8_rd_pick_best_mbsegmentation. Trace MB
//     row shows identical bmi.mv across all 16 sub-blocks at FINAL
//     ACCEPTED MB time, but picker-time WINNING SHAPE MVs may differ if
//     selectMotion picks Left/Above/Zero4x4 over New4x4 where libvpx
//     rd_check_segment resolves via NEW4X4.
//
// LIBVPX SOURCE REFERENCES (v1.16.0):
//
//   - vp8/encoder/rdopt.c:1659-1724  calculate_final_rd_costs (tteob gate)
//   - vp8/encoder/rdopt.c:1750-2270  vp8_rd_pick_inter_mode
//   - vp8/encoder/rdopt.c:1986-2006  SPLITMV picker UV branch
//   - vp8/encoder/rdopt.c:1199-1335  vp8_rd_pick_best_mbsegmentation
//   - vp8/encoder/rdopt.c:944-1183   rd_check_segment
//   - vp8/encoder/rdopt.c:895-919    vp8_encode_inter_mb_segment
//   - vp8/encoder/rdopt.c:725-742    rd_inter4x4_uv
//   - vp8/common/reconinter.c:59-80  vp8_build_inter_predictors_b
//
// GOVPX SOURCE REFERENCES:
//
//   - encoder_inter_modes_rd_split.go:42-196   selectInterFrameSplitModeRDScore
//   - encoder_inter_modes_rd_split.go:198-291  estimateInterSplitResidualRDAccounting
//   - encoder_inter_split.go:140-213           splitMotionShapeContext.selectShape
//   - encoder_inter_split.go:329-440           selectMotion
//   - encoder_inter_split.go:509-554           labelRD.rateDistortion
//   - encoder_inter_split.go:556-605           predictSplitMotionBlock4x4
//   - encoder_inter_split.go:630-640           fillSplitMotionResidual4x4
//
// HARNESS REFERENCES:
//
//   - oracle_encoder_runtime_controls_fuzz_test.go (matchLimit=6 carveout)
//   - diag_aebef841_test.go (govpx_oracle_trace && diag) repro tracer.
//
// TASK REFERENCES:
//
//   - task #218 (commit e331af27)  SPLITMV skip-backout gate port
//   - task #217 (commit f94f1d1d)  SPLITMV bestYRD cutoff guards
//   - task #211 (commit 2e872266)  0bb41d74 frame-4 recode-loop audit
//   - task #212 (commit 0ecc3451)  per-iter recode-loop oracle trace
//   - task #219 (commit 08f25b50)  per-iter recode trace per-MB MB dumps
func TestVP8Task226Aebef841Frame6PickerAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	// CLOSED by task #237: the residual frame-6 divergence is not a Y
	// predictor/residual gap. The root cause is that libvpx's
	// rd_check_segment leaves xd->eobs[i] holding the LAST-iterated mode's
	// per-block eob registers (rdopt.c:1124-1158 only restores entropy
	// contexts after labels2mode re-installs the winner; the eobs registers
	// retain whatever vp8_encode_inter_mb_segment wrote on the final inner-
	// loop iteration). bsi->eobs[i] = xd->eobs[i] at rdopt.c:1180 captures
	// that stale snapshot, and calculate_final_rd_costs reads tteob through
	// those stale eobs (rdopt.c:1689-1697) when applying the SPLITMV skip-
	// backout. govpx's per-label selectMotion previously returned the RD-
	// winning mode's tteob; it now mirrors the libvpx side-effect via
	// lastTTEOB. matchLimit dropped 6 -> 0 in
	// oracleRuntimeControlFuzzMatchLimit.
	t.Skip("closed by task #237; lastTTEOB port in encoder_inter_split.go selectMotion mirrors libvpx rdopt.c:1124-1180 xd->eobs side-effect")
}
