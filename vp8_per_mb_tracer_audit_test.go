//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8PerMBTracerInfrastructureAudit pins task #188: a cross-audit of the
// existing per-MB oracle tracer infrastructure against the residual
// per-MB picker-state divergences identified by task #173 (the 640x360
// noise:0 inter-frame gap) and task #187 (the 0bb41d74 golden-reference-
// overwrite frame-4 gap). The findings document what is wired up today,
// what is blocked, and where the next port has to land.
//
// EXISTING INFRASTRUCTURE (no new code needed):
//
//   - encoder_oracle_trace_mb.go            govpx-side per-MB row emitter
//     under //go:build govpx_oracle_trace.
//     Emits one oracleTraceMBRow per
//     encoded MB capturing
//     mode/ref/MV/skip/EOB/qcoeff/MB-rate
//     plus optional inter-candidate
//     rows under the "inter_candidate"
//     type tag.
//
//   - internal/coracle/build_vpxenc_oracle.sh
//     libvpx v1.16.0 patch script that
//     drops a self-contained
//     vp8/encoder/oracle_trace.c TU
//     into the upstream source tree
//     and emits matching per-MB JSON
//     Lines, gated on
//     GOVPX_ORACLE_TRACE_OUT.
//
//   - internal/coracle/oracle_compare.go    pure-Go JSONL diff engine that
//     walks both streams in lockstep
//     and reports field-level row
//     divergences with per-MB
//     coordinates. Both per-frame and
//     per-MB rows are compared.
//
//   - oracle_encoder_trace_compare_test.go  TestOracleEncoderTraceDecisionCompare
//     and TestOracleEncoderTraceInterCandidateCompare
//     exercise the full pipeline on a
//     64x64 panning fixture with
//     --end-usage=vbr; both pass under
//     origin/main today, confirming
//     the comparator is wired and the
//     projected-decision schema is
//     stable.
//
// BLOCKED FROM CROSS-AUDIT (the missing wiring this task identified):
//
//   - The two open seeds (640x360 noise:0 + 0bb41d74) BOTH reproduce only
//     via libvpx's runtime control-script entry, implemented in
//     internal/coracle/vpxenc_frameflags.c apply_runtime_codec_token at
//     lines 873 (noise:N -> VP8E_SET_NOISE_SENSITIVITY), 894-901
//     (arnrmax/arnrstrength/arnrtype), 920 (setref:GOLDEN:src:N), and the
//     deadline/cpu config-token path through apply_runtime_config_token
//     (line 689+).
//
//   - vpxenc-frameflags has the runtime-control surface but no per-MB
//     tracer (no vp8/encoder/oracle_trace.c patch applied).
//
//   - vpxenc-oracle has the per-MB tracer but no runtime-control surface
//     (uses stock vpxenc.c, no --control-script).
//
//   - The two oracle binaries are produced from independent libvpx
//     source clones (build/libvpx-v1.16.0-vpxenc-frameflags vs
//     build/libvpx-v1.16.0-vpxenc-oracle), so they cannot trivially share
//     the patch state. Closing the cross-audit requires either:
//
//     (a) Porting the build_vpxenc_oracle.sh per-MB-tracer C anchors
//     (vp8/encoder/oracle_trace.c + encodeframe.c hook + bitstream.c
//     hook + picklpf.c hook + vp8cx.mk wiring) onto the
//     build_vpxenc_frameflags.sh source tree, producing a new
//     vpxenc-frameflags-oracle binary that exposes BOTH the runtime
//     --control-script AND the per-MB tracer.
//
//     (b) Porting the relevant runtime-control surface from
//     vpxenc_frameflags.c (noise:N + arnr* + setref:* +
//     deadline:/cpu: config tokens at minimum) onto the stock
//     vpxenc.c that build_vpxenc_oracle.sh consumes, plus
//     an apply_runtime_controls(ctx,cfg,frame_idx) callback
//     sequenced from vpxenc.c's per-frame encode loop.
//
//     Path (a) is the smaller patch (re-applies an existing C anchor set
//     verbatim onto a sibling tree). Path (b) requires lifting the
//     vpxenc_frameflags.c per-frame harness into vpxenc.c, which is
//     larger because vpxenc.c's main loop has a different YUV/IVF I/O
//     shape than vpxenc_frameflags.c.
//
// NEW NEGATIVE-FINDING CROSS-AUDIT (over the existing #173/#187 pins):
//
//   - vp8/encoder/onyx_if.c:1619-1632 active_worst_quality /
//     active_best_quality clamping after worst_allowed_q /
//     best_allowed_q updates. govpx mirrors this in
//     ratecontrol.go:416 applyVP8ChangeConfigQuantizerClamp. The
//     mirror is already wired through applyVP8ChangeConfigRuntimeSideEffects;
//     #173's probe matrix (probe=rateModel-only) confirmed zero
//     bitstream effect on the noise:0 seed.
//
//   - vp8/encoder/onyx_if.c:1509-1511 q_trans[] translation of
//     worst_allowed_q / best_allowed_q / cq_level. Idempotent across
//     vp8_change_config calls because set_vp8e_config (vp8_cx_iface.c:357-359)
//     re-seeds oxcf->worst_allowed_q = cfg.rc_max_quantizer from the
//     user-space config on every update_extracfg invocation; the
//     translation is applied to the fresh value each time, never
//     re-applied to its prior output.
//
//   - vp8/encoder/onyx_if.c:1541-1548 baseline_gf_interval = (alt_freq
//     ? alt_freq : DEFAULT_GF_INTERVAL); overridden to
//     gf_interval_onepass_cbr in the (RT && CBR && !error_resilient)
//     branch. gf_interval_onepass_cbr is set ONCE in
//     vp8_create_compressor (line 1880-1885) and never updated by
//     vp8_change_config, so the assignment is idempotent across
//     control-driven vp8_change_config calls. govpx's
//     ratecontrol_golden.go consumes BaselineGFInterval from
//     framesTillGFUpdateDue (encoder_frame.go:208) which receives the
//     same constant each frame.
//
//   - vp8/encoder/ethreading.c:485-486 mbs_tested_so_far /
//     mbs_zero_last_dot_suppress reset for helper-thread MACROBLOCKs in
//     vp8cx_init_mbrthread_data. govpx mirrors this in
//     encoder_row_worker.go:91 (rowEncoderState.reset preserves
//     interModeTestHitCounts but expects mbs_tested_so_far reset via the
//     subsequent beginInterRDModeDecisionFrame). Confirmed by
//     encoder_threading_test.go TestVP8ThreadedHelperResetsBetweenFrames
//     (line 664+).
//
//   - vp8/encoder/ethreading.c:319-435 setup_mbby_copy does NOT copy or
//     zero mode_test_hit_counts onto helper-thread MACROBLOCKs.
//     mode_test_hit_counts is only ever zeroed by
//     vp8_initialize_rd_consts (rdopt.c:203-205) on cpi->mb (the main-
//     lane MACROBLOCK). Helper-lane mode_test_hit_counts therefore
//     persists ACROSS FRAMES from whatever the array was when the
//     helper thread was first dispatched (typically all-zero from the
//     cpi allocation, but it grows monotonically thereafter).
//     govpx's rowEncoderState.reset (encoder_row_worker.go:88-92)
//     mirrors this verbatim: preservedModeTestHits is reassigned after
//     `rs.enc = *e` so the helper's existing counter survives the
//     main-encoder snapshot copy. THIS IS THE LIBVPX BEHAVIOUR.
//
//   - vp8/encoder/encodeframe.c:858-860 helper-lane error_bins merge:
//     `cpi->mb.error_bins[c_idx] += cpi->mb_row_ei[i].mb.error_bins[c_idx]`.
//     The merge accumulates each helper's contribution onto the
//     main-lane bin. govpx mirrors this in encoder_row_threaded.go:690
//     (mergeThreadedInterFrameState). The main-lane error_bins is then
//     consumed by the next frame's vp8_set_speed_features only at
//     Speed>=7 (case 2: branch onyx_if.c:957-1010), so the merge is
//     bitstream-relevant only for RT cpu_used>=7. Neither audit seed
//     reaches that Speed boundary in the post-keyframe inter frames
//     where the divergence opens (#173: noise:0 fires at cpu_used=0 ->
//     Speed=0 after auto-select; #187: frame 4+ runs at deadline=good
//     cpu_used=0 -> Speed=0).
//
// CONCLUSION OF #188:
//
//	The per-MB picker tracer infrastructure is COMPLETE on both sides
//	for any seed that does not require runtime controls beyond vpxenc's
//	built-in flag surface. It is BLOCKED for #173 and #187 by the
//	independent-source-clone gap between vpxenc-oracle and
//	vpxenc-frameflags. The next task to close either gap should land
//	path (a) above: re-apply build_vpxenc_oracle.sh's existing
//	vp8/encoder/oracle_trace.c + encodeframe.c/bitstream.c/picklpf.c
//	anchors onto the build_vpxenc_frameflags.sh source tree, producing
//	a vpxenc-frameflags-oracle binary, then plumb a third
//	`findVpxencFrameFlagsOracle(t)` helper through
//	oracle_encoder_trace_compare_test.go to drive both seeds through
//	the existing CompareOracleTraces path.
//
//	This audit also confirms that all FIVE candidate `cpi->mb.*`
//	counters touched by vp8_set_speed_features (mode_test_hit_counts,
//	mbs_tested_so_far, mbs_zero_last_dot_suppress, error_bins,
//	rd_thresh_mult) are byte-faithfully mirrored in govpx today --
//	eliminating them as the residual gap source for either seed. The
//	residual must therefore live in a cpi-level OR cpi->common-level
//	mutation that vp8_change_config performs, OR in a sequencing
//	difference between when govpx and libvpx invoke their respective
//	"apply change-config side-effects" handlers relative to the per-MB
//	loop. Both are pinpointable only by the per-MB row comparator the
//	blocked path (a) unlocks.
//
// libvpx source references (v1.16.0):
//
//   - vp8/encoder/onyx_if.c:768-1087        vp8_set_speed_features
//   - vp8/encoder/onyx_if.c:1435-1740       vp8_change_config
//   - vp8/encoder/onyx_if.c:1619-1632       active_worst/best_quality clamp
//   - vp8/encoder/onyx_if.c:1509-1511       q_trans[] translation
//   - vp8/encoder/onyx_if.c:1541-1548       baseline_gf_interval refresh
//   - vp8/encoder/onyx_if.c:1880-1885       gf_interval_onepass_cbr init
//   - vp8/encoder/rdopt.c:163-227           vp8_initialize_rd_consts
//   - vp8/encoder/rdopt.c:201-205           mode_test_hit_counts zeroing
//   - vp8/encoder/ethreading.c:319-435      setup_mbby_copy (no
//     mode_test_hit_counts copy)
//   - vp8/encoder/ethreading.c:485-486      vp8cx_init_mbrthread_data
//     helper-mb resets
//   - vp8/encoder/encodeframe.c:858-860     error_bins merge
//   - vp8/encoder/pickinter.c:744-826       fast-picker mode_test_hit_counts
//     consumption + increment
//   - vp8/encoder/pickinter.c:1200          error_bins[this_rdbin]++
//   - vp8/vp8_cx_iface.c:525-534            update_extracfg -> vp8_change_config
//
// govpx source references:
//
//   - encoder_oracle_trace_mb.go            per-MB tracer emit functions
//   - encoder_oracle_trace_flag.go          no-op stubs (default build)
//   - encoder_oracle_trace.go               trace state + writer wiring
//   - encoder_inter_speed.go:446-489        beginInterRDModeDecisionFrame
//     (mode_test_hit_counts +
//     mbs_tested_so_far reset)
//   - encoder_row_worker.go:70-103          helper-worker reset (preserves
//     interModeTestHitCounts)
//   - encoder_row_threaded.go:680-710       mergeThreadedInterFrameState
//     (error_bins / mbsZeroLastDotSuppress
//     accumulation)
//   - encoder_frame.go:126                  mbsZeroLastDotSuppress = 0 at
//     per-frame begin
//   - ratecontrol.go:416                    applyVP8ChangeConfigQuantizerClamp
//   - encoder_config.go:868                 applyVP8ChangeConfigRuntimeSideEffects
//   - internal/coracle/oracle_compare.go    JSONL diff engine
//   - internal/coracle/build_vpxenc_oracle.sh
//     libvpx patch script (per-MB
//     tracer, no runtime controls)
//   - internal/coracle/vpxenc_frameflags.c  runtime-control driver
//     (no per-MB tracer)
//
// task references:
//
//   - task #173 (commit 533bbee8)           640x360 noise:0 audit (gap D)
//   - task #181 (commit 644fdca0)           setup_features lf_deltas zero
//   - task #182 (commit 58452621)           vp8_set_speed_features mirror
//   - task #187 (commit 6b4372b6)           0bb41d74 frame-4 audit
//   - c7adc553                              per-MB oracle trace mode add
//   - edd5b37c                              oracle trace runtime accounting
//   - 1e2488aa                              runtime-controls/oracle-trace
//     helper symbol disambiguation
func TestVP8PerMBTracerInfrastructureAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	// Skip-by-design: this test exists as the task #188 audit anchor
	// documenting the per-MB tracer infrastructure state, the
	// runtime-control reproduction blocker, and the cross-audit
	// negative finding (all five `cpi->mb.*` counter mirrors verified
	// byte-faithful). The active failures continue to surface from
	// FuzzOracleEncoderProductionRuntimeControls /
	// regression_640x360_serial_noise0_inter_diverge (task #173) and
	// FuzzOracleEncoderRuntimeControlTransitions /
	// regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74
	// (task #187). The unblock path is path (a) in the file comment
	// above: re-apply build_vpxenc_oracle.sh's anchors onto
	// build_vpxenc_frameflags.sh's source tree.
	t.Skip("documentation-only; live regressions at noise0_inter_diverge (#173) and 0bb41d74 (#187); cross-audit infrastructure path documented in file comment")
}
