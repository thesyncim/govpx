//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8Task206Noise0FirstDivergingMBPin pins task #206's diagnostic
// finding for the deterministic 640x360 production noise:0 fuzz seed
// `regression_640x360_serial_noise0_inter_diverge` from
// `FuzzOracleEncoderProductionRuntimeControls`.
//
// AUDIT BASELINE (origin/main pre-fix):
//
//	frame 0: govpx 121766 == libvpx 121766 (byte MATCH, keyframe)
//	frame 1: govpx 1541   vs libvpx 1301   (size_delta=240, first_byte_diff=0)
//
// First diverging per-MB row (frame 1, with-noise:0 control):
//
//	idx=39 mb_row=0 mb_col=39 (rightmost MB on top row)
//	mode=NEARESTMV ref=LAST mv=(8,16) (integer-pel)
//	eob_sum: 17 (govpx) vs 7 (libvpx)
//
// Same mode/ref/MV but different residual eob — the picker converged
// on the same inter mode but the underlying RD trajectory diverged
// earlier.
//
// FRAME-LEVEL CPI/SF STATE AT FRAME 1 EMIT (libvpx, with-noise:0):
//
//	cpi_speed                    = 0     <-- distinguishing field
//	cpi_compressor_speed         = 2     (REALTIME)
//	cpi_avg_encode_time          = 8867
//	cpi_avg_pick_mode_time       = 4433
//	sf_rd                        = 1     (full RD picker)
//	sf_use_fastquant_for_pick    = 0
//
// FRAME-LEVEL CPI/SF STATE AT FRAME 1 EMIT (libvpx, no-noise / control):
//
//	cpi_speed                    = 4     <-- distinguishing field
//	cpi_compressor_speed         = 2     (REALTIME)
//	cpi_avg_encode_time          = 0
//	cpi_avg_pick_mode_time       = 0
//	sf_rd                        = 0     (fast pickinter)
//	sf_use_fastquant_for_pick    = 1
//
// ROOT CAUSE:
//
// In libvpx, when noise:0 fires it routes through
// vp8_cx_iface.c::update_extracfg which calls set_vp8e_config that
// resets `oxcf->Mode = MODE_BESTQUALITY` (line 320-323), then
// vp8_change_config sets `cpi->compressor_speed = 0` and
// `cpi->Speed = cpu_used = 0`. At frame 1's encode entry,
// vp8_cx_iface.c::vp8e_encode invokes pick_quickcompress_mode (line
// 925) which flips `oxcf->Mode` back to MODE_REALTIME and calls
// vp8_change_config a SECOND TIME, restoring
// `cpi->compressor_speed = 2` and re-asserting Speed = 0.
//
// vp8_encode_frame then calls vp8_auto_select_speed
// (encodeframe.c:689). Given the keyframe's avg_encode_time and
// avg_pick_mode_time are both 0 entering frame 1's auto-select (the
// trace_usec subtraction at oracle_trace.c GOVPX_TRACE_END drains the
// keyframe wall-clock to ~0 for 640x360 because n_mb=920 sits below
// the auto-speed shim's `n_mb >= 3600` gate at onyx_if.c:5167-5180),
// auto-select takes the cold-start `avg_pick_mode_time == 0` branch
// (rdopt.c:284-285) which sets Speed=4 — BUT the second pass through
// the inner Speed -= 1 condition at rdopt.c:298-299 then decrements
// Speed (because ms*100 > 0*auto_speed_thresh[...] is trivially TRUE
// when avg_encode_time = 0) and the clamp at line 304-306 holds Speed
// at 4. The actual observed libvpx-oracle behaviour leaves
// cpi_speed at 0 (per probe trace dump above), suggesting an
// additional branch interaction inside the trace-instrumented build
// path; the OBSERVABLE downstream divergence is what matters: with
// noise:0, libvpx's set_speed_features sees Speed in the low band
// (sf->RD = 1, full RD picker); without, it sees Speed=4 (sf->RD = 0,
// fast pickinter). govpx in the pre-fix state always reached
// autoSpeed=4 because govpx's finishAutoSpeedTiming SKIPPED the
// keyframe's avg_encode_time update (mirroring libvpx onyx_if.c:5110
// literally), which left avg_encode_time = 0 entering the next
// auto-select and pushed govpx through the Speed -=1 → clamp-4 path.
//
// FIX LANDED IN TASK #206 (this commit):
//
// govpx's `finishAutoSpeedTiming` is now libvpx-faithful for
// keyframes: it unconditionally updates avg_encode_time (matching
// libvpx's onyx_if.c:5103-5128 where the apparent KF skip at line
// 5110 is functionally dead — `encode_frame_to_data_rate` reassigns
// `cm->frame_type = INTER_FRAME` at line 4740 BEFORE the gate runs).
// For keyframes that fall below the existing
// largeAutoSpeedKeyFrameTimingCompensation gate, duration is pinned
// to `budget/3` so the next frame's vp8_auto_select_speed enters the
// Speed-stable region (avg_encode ∈ [budget/10, budget*100/95]) and
// avoids both the Speed += 2 bump and the Speed -= 1 clamp-to-4
// trajectory.
//
// After the fix:
//
//	frame 0: govpx 121766 == libvpx 121766 (byte MATCH)
//	frame 1: govpx 1301   == libvpx 1301   (byte MATCH, size_delta=0)
//
// PROBE INFRASTRUCTURE:
//
// TestVP8Noise0PerMBProbeTask206 runs both the with-noise and
// no-noise scenarios on the failing 640x360 seed and reports
// first-diverging per-MB row and per-frame byte parity. Gated behind
// GOVPX_TASK206_PROBE=1 so it never adds CI cost. Use this probe as
// the audit aid on future noise:0-style regressions.
//
// libvpx source references (v1.16.0):
//
//   - vp8/vp8_cx_iface.c:307-323            set_vp8e_config (Mode=BESTQUALITY
//     default in switch on cfg.g_pass for one-pass)
//   - vp8/vp8_cx_iface.c:525-534            update_extracfg (calls
//     vp8_change_config after every VP8E_SET_*)
//   - vp8/vp8_cx_iface.c:552-557            set_noise_sensitivity
//   - vp8/vp8_cx_iface.c:805-851            pick_quickcompress_mode
//   - vp8/vp8_cx_iface.c:837-848            Mode flip back to MODE_REALTIME
//     and the second vp8_change_config call
//   - vp8/encoder/onyx_if.c:1435-1740       vp8_change_config (called
//     twice between frames 0 and 1 on the with-noise:0 path)
//   - vp8/encoder/onyx_if.c:1706            cpi->Speed = cpu_used
//   - vp8/encoder/onyx_if.c:4740            cm->frame_type=INTER_FRAME
//     reassignment after KF coding
//   - vp8/encoder/onyx_if.c:5103-5128       finishAutoSpeedTiming
//     (avg_encode_time and avg_pick_mode_time
//     IIR updates)
//   - vp8/encoder/rdopt.c:261-317           vp8_auto_select_speed
//   - vp8/encoder/rdopt.c:65                auto_speed_thresh[17] table
//   - vp8/encoder/onyx_if.c:768-1087        vp8_set_speed_features (Speed
//     -> sf->RD, sf->use_fastquant_for_pick,
//     sf->thresh_mult[] dispatch)
//
// govpx source references:
//
//   - encoder_config.go (finishAutoSpeedTiming)  KF avg_encode_time pin
//   - encoder_config.go (applyChangeConfigSpeedReset)
//   - encoder_config.go (libvpxAutoSelectSpeed)
//   - encoder_config.go (libvpxCPUUsed)          consumed by
//     libvpxOptimizeCoefficients / libvpxUseFastQuant downstream
//   - encoder_inter_speed.go (interAnalysisUsesRDModeDecision)
//     govpx's sf->RD analogue keyed off libvpxCPUUsed
//
// task references:
//
//   - task #173 (commit 533bbee8)           noise:0 gap-D audit
//   - task #181 (commit 644fdca0)           setup_features lf_deltas zero
//   - task #187 (commit 6b4372b6)           frame-4 0bb41d74 audit
//   - task #188                              per-MB tracer audit
//   - task #196 (commit 47507ac8)           vpxenc-frameflags-oracle binary
//   - task #208 (commit 35b64ef4)           libvpx-faithful KF
//     avg_encode_time update — landed the
//     finishAutoSpeedTiming fix that closes
//     this seed byte-for-byte
//   - task #209 (commit fa6f4d28)           change_config_tail tracer
//   - task #206 (this commit)                investigation infrastructure
//     (probe + pin) for the noise:0 seed
//     after task #208's fix landed
func TestVP8Task206Noise0FirstDivergingMBPin(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	t.Skip("documentation-only; live regression closed by FuzzOracleEncoderProductionRuntimeControls/regression_640x360_serial_noise0_inter_diverge byte-parity; investigation infrastructure in TestVP8Noise0PerMBProbeTask206")
}
