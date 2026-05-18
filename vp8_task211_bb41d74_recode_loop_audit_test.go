//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8Task211Bb41d74RecodeLoopAudit pins task #211: the residual
// frame-4+ divergence on the
// `regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74` fuzz seed of
// FuzzOracleEncoderRuntimeControlTransitions (matchLimit=4 tolerance set
// in oracle_encoder_runtime_controls_fuzz_test.go).
//
// SEED REPRODUCER:
//
//	`[]byte("020bA0C)a")` decodes (via oracleRuntimeControlFuzzCaseFromBytes
//	→ oracleRuntimeGeneralFuzzCase) into:
//	  - 64x64, target=300kbps, CBR, cpu_used=-8, panning sources, 9 frames
//	  - Per-frame control script:
//	    frame 0: -
//	    frame 1: maxintra:0+gfboost:0+cq:4+rtc:0+setref:golden:panning:8
//	    frame 2: deadline:good+cpu:0
//	    frame 3: arnrmax:0+arnrstrength:0+arnrtype:1
//	    frame 4: deadline:good+cpu:0
//	    frame 5: arnrmax:0+arnrstrength:0+arnrtype:1
//	    frame 6: deadline:good+cpu:0
//	    frame 7: arnrmax:0+arnrstrength:0+arnrtype:1
//	    frame 8: deadline:good+cpu:0
//
// CURRENT STATE (matchLimit=4 tolerance):
//   - frames 0-3: byte-MATCH (govpx and libvpx identical)
//   - frame 4: govpx=409 bytes, libvpx=422 bytes (first byte mismatch)
//   - frame 5-8: byte mismatch (logged, not asserted)
//
// DIVERGENCE ROOT CAUSE (this audit):
//
// The recode loop at frame 4 converges to a DIFFERENT final Q:
//   - govpx: loop_count=6, final_q=9, projected_frame_size=2956 bits
//   - libvpx: loop_count=7, final_q=7, projected_frame_size=3567 bits
//
// At the FINAL recode iteration's q_index, the per-MB picker walks the
// libvpx-faithful 20-mode order (libvpxFastInterModeOrder) but BOTH
// implementations stop testing modes well before mode_index=19. At MB(0,0)
// of frame 4:
//   - libvpx tests modes 0,1,4,10-14,16,17 (10 modes, including
//     SPLITMV-LAST and SPLITMV-GOLD) and picks SPLITMV (became_best=true
//     at mode_index=17 with score=84912)
//   - govpx tests modes 0,1,4,10-14 (8 modes) and picks NEWMV-GOLDEN
//     (mode_index=14, score=61544). govpx silently skips SPLITMV.
//
// Both sides agree on:
//   - frame 4 entry rc.rateCorrectionFactor (≈1.268586)
//   - active_best_quality=4, active_worst_quality=106, target=3545 bits
//   - refresh_golden=true, refresh_altref=true
//   - all per-frame trace fields except q_index
//
// Per-frame govpx state at frame 4 entry (from diag_bb41d74_test.go):
//
//	frame 4 pre-encode:
//	  rcf=1.268586 gcf=1.000000 kcf=1.150000
//	  cur_q=25 last_inter_q=25 ni_frames=1 ni_av_qi=66 avg_q=53
//	  deadline=GoodQuality cpu=0 activeWorstQChanged=false
//	  rdThreshMult=[32 80 92 144 128 128 128 128 128 128 256 256 256 129
//	                128 128 32 128 128 256]
//	  buffer_level=1168520 kf_overspend=15300 gf_overspend=36903
//	  framesTillGFUpdateDue=6 framesSinceGolden=1 currentGFInterval=10
//
// SPECIFIC GAP (where the next port has to land):
//
//   - In govpx's RD picker (encoder_inter_modes_rd.go:79+
//     selectRDInterFrameModeDecision), the SPLITMV candidates (mode_index
//     16, 17, 18) are silently skipped at MB(0,0) of frame 4 because
//     `selectInterFrameSplitModeRDScore` returns ok=false (no partition
//     survived the segmentYRD<=bestSegmentYRD cutoff). The picker takes
//     the line 350-353 branch (raise rd_thresh_mult, `continue` without
//     emitting a trace row).
//
//   - The CIRCULAR ROOT CAUSE: govpx's bestYRD entering the SPLITMV
//     branch at mode_index=16 is 45362 (from mode 14 NEWMV-GOLD yrd);
//     libvpx's best_mode.yrd at the same point is 60198. The difference
//     (~15000) is because govpx is recoding at q=9 and libvpx at q=7 —
//     RDMULT differs, so the per-mode YRD values differ. govpx's SPLITMV
//     partition shapes produce segmentYRD≈53000 which is LESS than
//     libvpx's bestYRD=60198 (so libvpx accepts and tests them) but
//     MORE than govpx's bestYRD=45362 (so govpx cuts them off).
//
//   - The DEEPER ROOT CAUSE: WHY does the recode loop converge to q=9
//     vs q=7? Both implementations enter frame 4 with identical state
//     (rcf=1.268586, active_best=4, active_worst=106, target=3545).
//     The first regulate_q yields the same initial Q (≈28 from
//     1.268586 * bpm[28]≈89062 ≤ target_bpm 113440). After 6/7 recode
//     iterations the Qs diverge. The per-iter projected_frame_size
//     depends on the per-MB picker output (SPLITMV winning or not).
//     This is the actual point of FIRST divergence: per-iter
//     projected_frame_size deltas accumulate over the recode loop and
//     bias the rate-correction-factor in opposite directions.
//
//   - The libvpx-faithful frame 4 MB(0,0) inter_candidate trace (visible
//     via GOVPX_ORACLE_TRACE_OUT=… with the combined vpxenc-frameflags-
//     oracle binary) shows SPLITMV-GOLDEN winning at score=84912
//     (mode_index=17, threshold=76500). The implied libvpx rd_threshes
//     at q=7 for SPLITMV-GOLD is 76500 = 4500 * 17 (split2 thresh_mult
//     for continuousSpeed=1 with q_pow(10,1.25)≈17).
//
// NEXT STEP TO CLOSE THE GAP:
//
//   - Instrument both sides' recode loops to emit per-iteration Q,
//     projected_frame_size, and rcf trace rows. The first iteration
//     where the projected_frame_size diverges is the actual point of
//     first divergence; everything downstream is a consequence.
//
//   - The libvpx-side instrumentation needs an additional emit hook
//     in encode_frame_to_data_rate's recode loop body (after
//     vp8_encode_frame returns, before vp8_estimate_entropy_savings
//     subtracts from projected_frame_size). The govpx-side equivalent
//     point is encoder_attempts.go after each attempt's encode pass
//     completes and before the recode regulator runs.
//
//   - Once the first diverging iteration's projected_frame_size is
//     pinned, walk back to the per-MB picker decisions in THAT
//     iteration to find which mode/MV/rate diverges first. That's the
//     specific libvpx-port that closes the gap.
//
// LIBVPX SOURCE REFERENCES (v1.16.0):
//
//   - vp8/encoder/rdopt.c:1750-2270             vp8_rd_pick_inter_mode
//   - vp8/encoder/rdopt.c:163-227               vp8_initialize_rd_consts
//   - vp8/encoder/onyx_if.c:768-1087            vp8_set_speed_features
//   - vp8/encoder/onyx_if.c:4099-4250           recode_loop body
//   - vp8/encoder/onyx_if.c:4461                vp8_update_rate_correction_factors(cpi, 2)
//   - vp8/encoder/ratectrl.c:1045-1144          vp8_update_rate_correction_factors
//   - vp8/encoder/ratectrl.c:1154-1232          vp8_regulate_q
//   - vp8/encoder/ratectrl.c:1389-1462          vp8_compute_frame_size_bounds
//
// GOVPX SOURCE REFERENCES:
//
//   - encoder_inter_modes_rd.go:25-450          selectRDInterFrameModeDecision
//   - encoder_inter_speed.go:270-617            interModeRDThresholds*
//   - encoder_inter_speed.go:446-489            resetInterRDThresholdMultipliers / beginInterRDModeDecisionFrame
//   - encoder_inter_speed.go:539-588            lower/raise inter RD threshold helpers
//   - encoder_reconstruct.go:86-110             libvpxFastInterModeOrder /
//     libvpxFastRefFrameOrder / libvpxInterModeCount
//   - encoder_inter_modes_refs.go:8-40          interReferenceSearchOrder /
//     interReferenceBySearchSlot
//   - ratecontrol_recode.go:23-200              frameSizeRecodeQuantizer*
//   - ratecontrol_postencode.go:325-410         updateRateCorrectionFactor /
//     setRateCorrectionFactorForFrame
//
// HARNESS REFERENCES:
//
//   - oracle_encoder_runtime_controls_fuzz_test.go:55-78
//     oracleRuntimeControlFuzzMatchLimit pins this seed at matchLimit=4
//     so frames 0-3 are asserted and 4-8 are logged-only.
//   - diag_bb41d74_test.go                      TestDiagBb41d74Frame4
//     (build-tagged `govpx_oracle_trace && diag`) is the
//     per-frame state probe that captured rdThreshMult/buffer/GF state
//     above; the libvpx-side comparison is via the vpxenc-frameflags-
//     oracle combined binary built by
//     internal/coracle/build_vpxenc_frameflags_oracle.sh.
//
// TASK REFERENCES:
//
//   - task #173 (commit 533bbee8 / closed via #208's KF avg_encode_time fix)
//   - task #187 (commit 6b4372b6)            0bb41d74 frame-4 audit
//   - task #188 (commit ?)                  per-MB tracer infrastructure cross-audit
//   - task #196 (commit 47507ac8)           vpxenc-frameflags-oracle combined binary
//   - task #206 (commit d39a0d0e)           noise:0 byte-parity infrastructure
//   - task #208 (commit 35b64ef4)           KF avg_encode_time dead-code fix
//   - task #209 (commit fa6f4d28)           vp8_change_config tail diagnostic
//   - task #210 (commit 2629cd53)           per-MB activity-masking quartet
func TestVP8Task211Bb41d74RecodeLoopAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	// Skip-by-design: this audit documents the recode-loop divergence on
	// the bb41d74 seed for the next port iteration. The live failure
	// surfaces from FuzzOracleEncoderRuntimeControlTransitions /
	// regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74 under the
	// matchLimit=4 tolerance pinned by oracleRuntimeControlFuzzMatchLimit.
	//
	// The diag_bb41d74_test.go file (build-tagged `diag`) is the
	// in-flight probe that produced the per-frame rdThreshMult / buffer
	// / GF-cadence state captured in the file comment above. Running it
	// repeatedly is the next-step diagnostic for any continuation: it
	// also writes /tmp/diag_libvpx.jsonl and /tmp/diag_govpx.jsonl with
	// the full per-MB inter_candidate / rate / recode / frame oracle
	// rows from both sides for direct diff.
	t.Skip("documentation-only; live regression at FuzzOracleEncoderRuntimeControlTransitions/regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74 under matchLimit=4 tolerance; recode-loop divergence at frame 4 final_q=9 vs 7")
}
