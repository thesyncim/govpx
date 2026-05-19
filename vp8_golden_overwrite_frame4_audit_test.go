//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8GoldenOverwriteFrame4DivergenceAudit pins the residual bitstream
// divergence on the deterministic 64x64 golden-reference-overwrite fuzz seed
// `testdata/fuzz/FuzzOracleEncoderRuntimeControlTransitions/`
// `regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74`.
// Task #187 audit substrate — anchors the NEGATIVE FINDING that the libvpx
// golden-frame copy paths (vp8_set_reference, update_reference_frames,
// copy_buffer_to_arf, copy_buffer_to_gf) are byte-faithfully mirrored in
// govpx today.
//
// Seed shape (decoded from []byte("020bA0C)a")):
//
//	dim=64x64, target=300kbps, cpu_used=-8 (spm8), frames=9, src=panning (0)
//	frame 0  flags=0  schedule=  "-"  (keyframe)
//	frame 1  flags=0  schedule=  "maxintra:0+gfboost:0+cq:4+rtc:0+setref:golden:panning:8"
//	frame 2  flags=0  schedule=  "deadline:good+cpu:0"  (rt -> good-quality transition)
//	frame 3  flags=0  schedule=  "arnrmax:0+arnrstrength:0+arnrtype:1"
//	frame 4  flags=0  schedule=  "deadline:good+cpu:0"  (no-op deadline restate)
//	frame 5  flags=0  schedule=  "arnrmax:0+arnrstrength:0+arnrtype:1"
//	frame 6  flags=0  schedule=  "deadline:good+cpu:0"
//	frame 7  flags=0  schedule=  "arnrmax:0+arnrstrength:0+arnrtype:1"
//	frame 8  flags=0  schedule=  "deadline:good+cpu:0"
//
// Strict-gate snapshot under origin/main @ 644fdca0
// ("vp8: zero last_ref/mode_lf_deltas in forceNextLFDeltaUpdate"):
//
//	frame 0  byte MATCH  len=3472 first_part=504 keyframe=true
//	frame 1  byte MATCH  len=4105 first_part=321 keyframe=false
//	frame 2  byte MATCH  len= 889 first_part=100 keyframe=false
//	frame 3  byte MATCH  len= 469 first_part= 77 keyframe=false
//	frame 4  MISMATCH    got_len=409 want_len=422  first_part 102 / 100
//	frame 5  MISMATCH    got_len=587 want_len=595  first_part  65 /  71
//	frame 6  MISMATCH    got_len=349 want_len=339  first_part  73 /  66
//	frame 7  MISMATCH    got_len=362 want_len=364  first_part  58 /  73
//	frame 8  MISMATCH    got_len=284 want_len=274  first_part  78 /  56
//
// Frames 0-3 byte-MATCH proves the golden-reference-overwrite at frame 1
// and the rt -> good-quality deadline transition at frame 2 are
// byte-faithful through the inter-frame pack. The divergence opens at
// frame 4 and tracks first_diff=0 with first_partition_size deltas of
// +/-2 to +/-15 bytes — the signature of per-MB picker decisions
// diverging once the partial good-quality inter recode loop is engaged
// across multiple consecutive inter frames.
//
// task #172 closure (commit 2c72eda) pinned the sibling 9-frame seed
// 77952f43 in the same general/64x64/300kbps/spm8 cohort: that seed
// historically diverged at frame 8 inside the recode loop and was
// closed by 45ded7d5 ("vp8: land libvpx-aligned recode_loop +
// change-config rate-model + segmentation-method WIP"). The
// 0bb41d74 sibling enters the same partial good-quality inter recode
// area but additionally exercises a golden-reference overwrite at
// frame 1 followed by a deadline transition cluster (frames 2-8). The
// commit message for 2c72eda explicitly notes "Sibling seed 0bb41d74
// (limit=4) still diverges on the golden-reference overwrite path and
// is left under the same partial-prefix tolerance."
//
// AUDIT — libvpx golden-frame copy paths ruled out as the residual
// divergence trigger (each path verified byte-faithful in govpx
// today; frames 0-3 byte-MATCH proves the audit):
//
//  1. vp8_set_reference (vp8/encoder/onyx_if.c:2443-2462). libvpx
//     copies the user-supplied YV12 buffer into cm->yv12_fb[ref_fb_idx]
//     keyed off the selector (LAST/GOLDEN/ALTREF -> lst/gld/alt_fb_idx).
//     govpx SetReferenceFrame (vp8_encoder_reference_controls.go:17)
//     dispatches through referenceAliasGroup so a write to ReferenceGolden
//     also propagates to ReferenceLast / ReferenceAltRef when the
//     post-keyframe alias map (vp8_encoder_reference_buffers.go:19-21) is
//     still in effect. Frame 1 of this seed fires the
//     setref:golden:panning:8 path immediately after the keyframe (all
//     three alias bits are true at that point) — govpx writes panning[8]
//     into lastRef + goldenRef + altRef. libvpx achieves the same effect
//     because after the keyframe `cm->lst_fb_idx == cm->gld_fb_idx ==
//     cm->alt_fb_idx == cm->new_fb_idx` (line 2874 + line 2947 with
//     refresh_last_frame=1), so a single vp8_yv12_copy_frame into
//     yv12_fb[gld_fb_idx] is observably equivalent to writing all three
//     selectors. The 4105-byte byte-MATCH on frame 1 (the very next
//     packet) confirms this round-trip.
//
//  2. update_reference_frames (vp8/encoder/onyx_if.c:2860-2942). libvpx
//     re-aliases yv12_fb indices via refresh_last_frame /
//     refresh_golden_frame / refresh_alt_ref_frame and the
//     copy_buffer_to_arf / copy_buffer_to_gf flags. govpx mirrors the
//     index swap as a yv12 buffer COPY in
//     refreshInterFrameReferencesFromAnalysis
//     (vp8_encoder_reference_buffers.go:67-88), which is observationally
//     equivalent up to extend-borders / stride invariants. The frame 1
//     byte-MATCH (which exercises refresh_last_frame=1 + golden/alt
//     unchanged) and frame 2 byte-MATCH (which runs after the deadline
//     transition that re-runs setup_features) prove the index-swap-vs-
//     copy abstraction is byte-faithful on this seed shape.
//
//  3. copy_buffer_to_arf (vp8/encoder/onyx_if.c:4370-4375). libvpx
//     sets `cm->copy_buffer_to_arf = 2` when refresh_golden_frame is
//     true and ext_refresh_frame_flags_pending is false (mirroring the
//     "copy old GF to ARF" default). This seed never refreshes the
//     golden frame from the encoder side (`refresh_golden_frame == 0`
//     on every inter frame because no EncodeForceGoldenFrame /
//     EncodeForceAltRefFrame is in `flags`, and CBR-rebuild does not
//     fire on a 64x64 9-frame fixture under DropFrameAllowed=false),
//     so the gate at line 4370 evaluates false on every frame.
//     copy_buffer_to_arf stays 0 across the entire seed — the path is
//     inert and cannot drive the frame 4+ divergence.
//
//  4. copy_buffer_to_gf (vp8/encoder/onyx_if.c:2919-2940). Same
//     situation: copy_buffer_to_gf is only set non-zero by the
//     auto-alt-ref + recode interaction (onyx_if.c:3866 territory).
//     This seed runs without auto-alt-ref (oxcf.play_alternate = 0,
//     EncoderOptions.AutoAltRef defaults to false) and never enters
//     the second-pass GF cliff that would set copy_buffer_to_gf.
//     Confirmed by trace: the gld_fb_idx slot never receives a
//     buffer-to-buffer copy through this path in either oracle or
//     govpx for this seed.
//
//  5. ext_refresh_frame_flags_pending reset
//     (vp8/encoder/onyx_if.c:1539). vp8_change_config zeroes the
//     pending-refresh-flags signal on every invocation. govpx clears
//     the per-frame refresh-mask state via `armExternalRefreshMask`
//     at encode entry (vp8_encoder_frame.go:108) and re-derives from
//     `flags` each frame; no cross-frame carryover. The frame-3 ARNR
//     control burst (which fires vp8_change_config 3x between encodes)
//     is therefore idempotent on this field. Frame 3 byte-MATCH
//     confirms the burst is bitstream-neutral.
//
//  6. cpi->alt_ref_source = NULL / cpi->is_src_frame_alt_ref = 0
//     (vp8/encoder/onyx_if.c:1718-1719). vp8_change_config resets the
//     alt-ref source pointer on every invocation. govpx tracks
//     altRefSourcePTS/altRefSourceValid (vp8_encoder.go:665-666) only
//     when lookahead is enabled. This seed runs with
//     EncoderOptions.LagInFrames = 0 (oracleRuntimeBaseFuzzOptions),
//     so lookaheadEnabled() returns false and altRefSourceValid is
//     never set true. The reset is observably idempotent here.
//
// AUDIT — task #181 closure validation. Commit 644fdca0 ("vp8: zero
// last_ref/mode_lf_deltas in forceNextLFDeltaUpdate") ported the
// libvpx setup_features last_*_lf_deltas zeroing (onyx_if.c:396-399).
// Re-running this seed after 644fdca0 lands shows IDENTICAL frame 4+
// byte mismatches (got_len=409 want_len=422 first_part 102/100 with
// the same SHAs as pre-fix). The LF delta zeroing therefore does not
// affect this seed — the residual divergence is not in the LF delta
// signaling path.
//
// AUDIT — task #173 closure cross-reference. The 640x360
// noise:0 audit (commit 533bbee8) eliminated all known
// vp8_change_config body mutations through `vp8_set_speed_features`
// at Speed=4 RT as the residual gap-D trigger and pinned the missing
// state in cpi->mb.{mbs_tested_so_far, mbs_zero_last_dot_suppress,
// error_bins[]} which only feed adaptive thresholding at Speed >= 7.
// The 0bb41d74 seed differs in two relevant ways:
//
//   - cpu_used = -8 in realtime mode initially -> Speed = -8 -> fast
//     RT picker (Speed >= 7 territory). The frame-1 encode under
//     setref:golden:panning:8 + maxintra/cq/rtc cluster therefore
//     COULD plausibly read the Speed >= 7 adaptive state — yet
//     frame 1 byte-MATCHES, so any cross-frame error_bins residual
//     either flushes cleanly between frames or is bitstream-neutral
//     here.
//
//   - From frame 2 onward, deadline:good + cpu:0 -> Speed = 0 (good-
//     quality best). Speed = 0 hits the recode-loop dispatcher
//     (sf->recode_loop = 1 default at vp8/encoder/onyx_if.c:792) and
//     uses RD mode decision (sf->RD = 1). The recode loop's
//     active_worst / active_best Q regulation across multiple
//     inter frames is the suspect class — task #172 closure
//     (commit 45ded7d5) closed the recode-loop bookkeeping for the
//     ARNR-free sibling 77952f43; the residual gap on 0bb41d74 was
//     subsequently closed by task #218 and the seed now asserts
//     byte-exact at matchLimit=0.
//
// The residual gap therefore belongs to the same class as task #173
// (per-MB picker state at Speed boundaries across recode loop
// iterations) but expressed via a different runtime-control sequence
// (good-quality deadline transition cluster after golden-reference
// overwrite). Closing the gap requires a libvpx per-MB tracer for
// frame 4 of this exact seed to identify the picker state field
// whose value diverges after the deadline:good control at frame 2.
//
// REFERENCES (libvpx v1.16.0):
//
//   - vp8/encoder/onyx_if.c:1435-1740     vp8_change_config
//   - vp8/encoder/onyx_if.c:1539          ext_refresh_frame_flags_pending reset
//   - vp8/encoder/onyx_if.c:1558          setup_features call
//   - vp8/encoder/onyx_if.c:1706          cpi->Speed = oxcf.cpu_used
//   - vp8/encoder/onyx_if.c:1718-1719     cpi->alt_ref_source / is_src_frame_alt_ref reset
//   - vp8/encoder/onyx_if.c:382-402       setup_features (zeroes deltas + calls set_default_lf_deltas)
//   - vp8/encoder/onyx_if.c:660-683       set_default_lf_deltas (mode_lf_deltas[1] = -12 RT vs -2 GOOD)
//   - vp8/encoder/onyx_if.c:2443-2462     vp8_set_reference (golden-frame overwrite write site)
//   - vp8/encoder/onyx_if.c:2860-2942     update_reference_frames (refresh / copy_buffer)
//   - vp8/encoder/onyx_if.c:4370-4375     copy_buffer_to_arf gate
//   - vp8/encoder/onyx_if.c:782-792       sf->recode_loop = 1 default (Speed = 0)
//   - vp8/encoder/onyx_if.c:910-920       recode_loop = 2 -> 0 transitions (Speed > 2 / > 3)
//   - vp8/vp8_cx_iface.c:525-533          update_extracfg dispatch through vp8_change_config
//   - vp8/vp8_cx_iface.c:579-597          set_arnr_{max_frames,strength,type}
//
// REFERENCES (govpx):
//
//   - vp8_encoder_reference_controls.go:17         SetReferenceFrame
//   - vp8_encoder_reference_controls.go:32         setReferenceFrameNow
//   - vp8_encoder_reference_controls.go:220        referenceAliasGroup
//   - vp8_encoder_reference_buffers.go:67-88       refreshInterFrameReferencesFromAnalysis
//   - vp8_encoder_reference_buffers.go:108-129     copyInterFrameReferences (copy_buffer_to_*)
//   - vp8_encoder_config.go:526                    SetDeadline
//   - vp8_encoder_config.go:553                    SetCPUUsed
//   - vp8_encoder_config.go:868                    applyVP8ChangeConfigRuntimeSideEffects
//   - vp8_encoder_config.go:861                    applyChangeConfigSpeedReset
//   - vp8_encoder_config.go:992-1040               SetARNR / setARNR{Max,Strength,Type}
//   - vp8_encoder_loopfilter.go:122                forceNextLFDeltaUpdate (task #181 zeroed last_*)
//   - ratecontrol.go:414                       applyVP8ChangeConfigQuantizerClamp
//   - ratecontrol.go:429                       applyVP8ChangeConfigRateModel
//
// REFERENCES (companion audit pins):
//
//   - vp8_noise0_runtime_control_audit_test.go  Task #173 audit (gap D)
//   - vp8_arnr_temporal_filter_audit_test.go    Task #175 ARNR negative finding
//   - 2c72eda2                                  Task #172 sibling 77952f43 closure
//   - 644fdca0                                  Task #181 last_*_lf_deltas zeroing
func TestVP8GoldenOverwriteFrame4DivergenceAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	// Skip-by-design: this audit documents the historical divergence on
	// the 0bb41d74 seed for task #187. The gap was closed by task #218
	// (vp8_encoder_inter_modes_rd_split.go SPLITMV skip-backout port); the
	// seed now asserts strict byte-exact at matchLimit=0 inside
	// FuzzOracleEncoderRuntimeControlTransitions /
	// regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74.
	t.Skip("closed by task #218; seed asserts strict byte-exact at matchLimit=0")
}
