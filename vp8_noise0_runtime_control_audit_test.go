//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

// TestVP8RuntimeNoiseSensitivity0SideEffectAudit pins the residual
// bitstream divergence on the deterministic 640x360 noise:0 fuzz seed
// from `FuzzOracleEncoderProductionRuntimeControls`
// (`testdata/fuzz/.../regression_640x360_serial_noise0_inter_diverge`),
// the long-pending failure originally filed as task #24 / gap D.
//
// Prior commit 714577b4 ("Stop tearing down VP8 denoiser on runtime
// noise_sensitivity=0") closed the part of gap D that traced to govpx
// calling `e.denoiser.reset()` whenever the runtime sensitivity went
// to 0, which libvpx never does (the buffers and per-MB FILTER/COPY
// state stay alive). That fix lands the SetNoiseSensitivity sticky
// regression test (encoder_denoiser_active_test.go) at strict parity,
// but the production 640x360 seed still fails at frame 1.
//
// This audit pins what the residual divergence ISN'T and, by
// elimination, where the remaining gap lives:
//
//   - Frame 0 (keyframe): byte MATCH (len=121766, sha matches).
//   - Frame 1 (inter, after noise:0 fires):
//     got_len=1541 (govpx) vs want_len=1301 (libvpx), first_part 511 vs 494.
//   - With the same seed but the `noise:0` control REMOVED (script
//     `"-", "-"`) both encoders match byte-for-byte: govpx=libvpx=1534
//     bytes, sha=4e5c91979c843aad. Probe row "noop" below confirms.
//
// What changes between the two libvpx runs (with vs without
// noise:0):
//   - libvpx shrinks frame 1 by ~233 bytes (1534 -> 1301).
//   - First-partition shrinks by 10 bytes (504 -> 494).
//   - LF auto-pick lands on level 28 instead of 29.
//   - ProbIntra drops from 8/256 to 3/256 — libvpx codes substantially
//     more inter MBs and almost no intra after firing noise:0.
//   - MVUpdateCount jumps from 0 to 9 — the per-frame MVcount[]
//     distribution diverges enough from `vp8_default_mv_context` that
//     libvpx emits 9 prob updates from the bitstream MV-prob update
//     phase (encodemv.c write_component_probs).
//
// What changes for govpx between the two runs:
//   - govpx grows frame 1 by 7 bytes (1534 -> 1541).
//   - First-partition grows by 7 bytes (504 -> 511).
//   - Second-partition stays IDENTICAL (1030 bytes in both); govpx
//     makes IDENTICAL per-MB mode/MV/coefficient decisions whether
//     SetNoiseSensitivity(0) is invoked or not.
//   - LF level stays 29; ProbIntra stays 8; MVUpdateCount stays 0.
//   - The +7 byte first-part bump is exactly the LF delta refresh
//     pattern that govpx's `forceNextLFDeltaUpdate` triggers on the
//     next frame after any runtime config-set (it does not zero the
//     `lastSignaledRefLFDeltas` / `lastSignaledModeLFDeltas` arrays
//     like libvpx `setup_features` does, so the per-delta comparison
//     in the bitstream writer all hit "no change" branches).
//
// Eliminated as the trigger (`TestVP8Noise0ProbeMinimalApply` below):
//   - `applyChangeConfigSpeedReset` (autoSpeed reset to cpu_used=0):
//     produces ZERO bitstream change. govpx and libvpx both run
//     vp8_auto_select_speed at the top of the next frame's
//     encode_mb_row, both end at Speed=4 with zeroed timers; the picker
//     dispatch sees identical state.
//   - `forceNextLFDeltaUpdate`: produces the +7 byte first-part bump
//     (the entire govpx-side observable bitstream delta).
//   - `refreshRuntimeCyclicRefreshConfig`: zero bitstream effect.
//   - `applyVP8ChangeConfigRateModel` + `applyVP8ChangeConfigQuantizerClamp` +
//     `refreshDropFramesAllowed`: zero bitstream effect.
//
// Verbatim libvpx state mutations performed by `vp8_change_config`
// (vp8/encoder/onyx_if.c:1435-1740) that I audited and ruled out as
// the residual MB-decision divergence trigger:
//   - cpi->Speed = cpi->oxcf.cpu_used (line 1706): vp8_auto_select_speed
//     overwrites this at the top of encode_mb_row before
//     vp8_set_speed_features reads it, so the reset value never reaches
//     the picker dispatch when realtime+positive-cpu_used auto-select
//     is active. Both with/without enter the picker at Speed=4 with
//     timers zeroed.
//   - cpi->ext_refresh_frame_flags_pending = 0 (line 1539): no user-set
//     refresh flag is in flight for this 2-frame seed (`flags=nil`),
//     and the read site at onyx_if.c:4371 only gates
//     `cm->copy_buffer_to_arf` on cm->refresh_golden_frame, which is 0
//     for the normal inter frame 1.
//   - cpi->baseline_gf_interval = ... (lines 1541-1548): only feeds
//     frames_till_gf_update_due at the next keyframe / GF cycle, neither
//     of which fires within the 2-frame seed.
//   - vp8_new_framerate(cpi, cpi->framerate) (line 1612): recomputes
//     per_frame_bandwidth / av_per_frame_bandwidth / max_gf_interval /
//     static_scene_max_gf_interval. With unchanged
//     framerate=30 and target_bandwidth=300000bps, all four outputs are
//     idempotent across vp8_change_config calls; govpx's
//     `applyVP8ChangeConfigRateModel` mirrors the same arithmetic.
//   - Buffer-level rescaling (lines 1587-1603): `rescale(starting/
//     optimal/maximum_buffer_size, target_bandwidth, 1000)` runs every
//     vp8_change_config, but the input oxcf field is re-seeded from the
//     public cfg.rc_buf_*_sz (ms) by `set_vp8e_config` before each call,
//     so the result is idempotent: same buffer level after each invoke.
//   - Buffer clamp `if (cpi->bits_off_target > maximum_buffer_size)` at
//     line 1606: bits_off_target is deeply negative after the 121766-
//     byte keyframe, so the clamp never fires.
//   - setup_features (line 1558): zeroes last_*_lf_deltas and re-arms
//     mode_ref_lf_delta_{enabled,update}=1; this is the source of the
//     LF delta refresh observed in BOTH encoders' first-part. govpx's
//     `forceNextLFDeltaUpdate` mirrors the update bit but does NOT zero
//     `lastSignaledRefLFDeltas`/`lastSignaledModeLFDeltas`, hence the
//     7-byte first-part delta. This explains the first_part diff (511
//     vs 504 in govpx, 494 vs 504 in libvpx) but NOT the second-part
//     230-byte diff.
//   - cpi->oxcf.encode_breakout / cpi->segment_encode_breakout[i]
//     refresh (lines 1560-1565): static_thresh=0 in the seed config,
//     so the assignment is a no-op (already 0).
//   - cpi->alt_ref_source = NULL / cpi->is_src_frame_alt_ref = 0
//     (lines 1718-1719): no ARNR / lag_in_frames in the seed config.
//
// The remaining gap therefore lives in some other state mutation that
// `vp8_change_config` performs (or whose effect ripples through one
// frame later via, e.g., `vp8_set_speed_features` reseeding the
// `cpi->mb.mbs_tested_so_far` / `cpi->mb.mbs_zero_last_dot_suppress` /
// `cpi->mb.error_bins[]` counters at line 783-784, 1025 — both of which
// govpx does NOT obviously model since they are speed-7+ adaptive
// thresholding state). Closing the gap requires either:
//
//	(1) Identifying which libvpx `cpi->mb.*` counter, when reset between
//	    the keyframe and inter frame, flips the picker's MV/intra
//	    decision toward the libvpx-with-noise output; or
//	(2) A wider audit of all `cpi->mb.*` fields touched by
//	    `vp8_set_speed_features` (onyx_if.c:768-1087) and confirming
//	    whether govpx mirrors each reset. This is the natural next-step
//	    once a verbatim libvpx tracer can extract the per-MB mode and
//	    MV picks from frame 1 of the with-noise:0 run for direct
//	    comparison.
//
// libvpx source references (v1.16.0):
//   - vp8/vp8_cx_iface.c:552-557 set_noise_sensitivity / update_extracfg
//   - vp8/encoder/onyx_if.c:1435-1740 vp8_change_config
//   - vp8/encoder/onyx_if.c:768-1087 vp8_set_speed_features
//   - vp8/encoder/rdopt.c:163-227 vp8_initialize_rd_consts
//   - vp8/encoder/rdopt.c:261-320 vp8_auto_select_speed
//   - vp8/encoder/encodeframe.c:680-722 encode_mb_row pre-encode setup
//   - vp8/encoder/encodemv.c:155-313 MV prob updates emitted from
//     cpi->mb.MVcount aggregated by per-MB picks
//
// govpx source references:
//   - encoder_config.go:967 SetNoiseSensitivity
//   - encoder_config.go:868 applyVP8ChangeConfigRuntimeSideEffects
//   - encoder_config.go:861 applyChangeConfigSpeedReset
//   - encoder_loopfilter.go:122 forceNextLFDeltaUpdate (LF delta state)
//   - encoder_loopfilter.go:89 computeLFDeltaUpdateBit (bitstream emit)
func TestVP8RuntimeNoiseSensitivity0SideEffectAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	// Skip-by-design: this test exists as documentation pinning the
	// gap-D residual divergence for task #173. The active failure
	// surfaces from FuzzOracleEncoderProductionRuntimeControls /
	// regression_640x360_serial_noise0_inter_diverge so the harness is
	// already in place; running another encode here would duplicate
	// state without exercising additional surface.
	t.Skip("documentation-only; live regression at FuzzOracleEncoderProductionRuntimeControls/regression_640x360_serial_noise0_inter_diverge")
}

// TestVP8Noise0ProbeMinimalApply pins which side-effect of
// applyVP8ChangeConfigRuntimeSideEffects produces an observable
// bitstream change on the deterministic 640x360 noise:0 seed. Each
// probe row encodes the same two-frame seed but invokes only ONE
// reset path between frame 0 and frame 1; the per-row gotLen +
// matchNoNoise columns isolate which mirror is doing what:
//
//	probe=noop                       gotLen=1534 matchNoNoise=true
//	probe=set-noise-sensitivity-0    gotLen=1541 matchNoNoise=false
//	probe=speed-reset-only           gotLen=1534 matchNoNoise=true
//	probe=forceNextLFDelta-only      gotLen=1541 matchNoNoise=false
//	probe=refreshCyclic-only         gotLen=1534 matchNoNoise=true
//	probe=rateModel-only             gotLen=1534 matchNoNoise=true
//
// Only `forceNextLFDelta-only` reproduces the full govpx-with-noise
// output (1541 bytes). The other side-effects of vp8_change_config
// that govpx mirrors are bitstream-neutral on this seed.
//
// The libvpx oracle in contrast lands at 1301 bytes when noise:0 is
// invoked (vs 1534 without), so libvpx's vp8_change_config is doing
// something else (a state mutation that govpx does not model) that
// flips the inter MB decisions in the libvpx-with-noise direction.
// Closing the gap requires identifying that missing mirror — see the
// TestVP8RuntimeNoiseSensitivity0SideEffectAudit comment above for
// the libvpx call sites already ruled out.
func TestVP8Noise0ProbeMinimalApply(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	driver := findVpxencFrameFlags(t)
	const (
		w          = 640
		h          = 360
		cpuUsed    = 0
		targetKbps = 300
	)
	opts := oracleRuntimeBaseFuzzOptions(w, h, targetKbps, cpuUsed)
	opts.Threads = 0
	sources := oracleRuntimeFuzzSources(w, h, 2, 0)

	type probe struct {
		name  string
		apply func(t *testing.T, e *VP8Encoder)
	}
	probes := []probe{
		{"noop", func(t *testing.T, e *VP8Encoder) {}},
		{"set-noise-sensitivity-0", func(t *testing.T, e *VP8Encoder) {
			mustRuntime(t, "Set", e.SetNoiseSensitivity(0))
		}},
		{"speed-reset-only", func(t *testing.T, e *VP8Encoder) {
			e.applyChangeConfigSpeedReset()
		}},
		{"forceNextLFDelta-only", func(t *testing.T, e *VP8Encoder) {
			e.forceNextLFDeltaUpdate()
		}},
		{"refreshCyclic-only", func(t *testing.T, e *VP8Encoder) {
			e.refreshRuntimeCyclicRefreshConfig()
		}},
		{"rateModel-only", func(t *testing.T, e *VP8Encoder) {
			e.rc.applyVP8ChangeConfigRateModel(e.opts.TwoPassMinPct)
			e.rc.applyVP8ChangeConfigQuantizerClamp()
			e.rc.refreshDropFramesAllowed()
		}},
	}

	libvpxWith := encodeFramesWithFrameFlagsDriver(t, driver, "noise0-want",
		opts, targetKbps, sources, nil,
		[]string{"--control-script=" + strings.Join([]string{"-", "noise:0"}, ",")})
	libvpxWithout := encodeFramesWithFrameFlagsDriver(t, driver, "noise0-want-no",
		opts, targetKbps, sources, nil,
		[]string{"--control-script=" + strings.Join([]string{"-", "-"}, ",")})

	// Pin the libvpx oracle outputs so the probe rows are interpreted
	// against the right baselines. These are the deterministic
	// libvpx-1.16.0 outputs captured at task #173 audit time. Any
	// change here means libvpx (or our oracle driver) materially
	// shifted behaviour and the probe matrix below needs to be
	// re-interpreted.
	const (
		wantWithNoiseLen = 1301
		wantNoNoiseLen   = 1534
	)
	if len(libvpxWith) < 2 || len(libvpxWith[1]) != wantWithNoiseLen {
		t.Fatalf("libvpx with-noise frame1 len = %d, want %d (oracle baseline drifted)",
			frameLenOrZero(libvpxWith, 1), wantWithNoiseLen)
	}
	if len(libvpxWithout) < 2 || len(libvpxWithout[1]) != wantNoNoiseLen {
		t.Fatalf("libvpx without-noise frame1 len = %d, want %d (oracle baseline drifted)",
			frameLenOrZero(libvpxWithout, 1), wantNoNoiseLen)
	}

	for _, p := range probes {
		apply := map[int]func(*testing.T, *VP8Encoder){1: p.apply}
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
		f1 := frameOrNil(got, 1)
		matchNoise0 := frameLenOrZero(got, 1) == wantWithNoiseLen && bytesEqualSafe(f1, libvpxWith[1])
		matchNoNoise := frameLenOrZero(got, 1) == wantNoNoiseLen && bytesEqualSafe(f1, libvpxWithout[1])
		t.Logf("probe=%-30s gotLen=%d matchNoise0=%v matchNoNoise=%v", p.name, len(f1), matchNoise0, matchNoNoise)
	}
	_ = vp8dec.FrameHeader{} // keep the decoder import live for future header probes
}

func frameLenOrZero(frames [][]byte, idx int) int {
	if idx < 0 || idx >= len(frames) {
		return 0
	}
	return len(frames[idx])
}

func frameOrNil(frames [][]byte, idx int) []byte {
	if idx < 0 || idx >= len(frames) {
		return nil
	}
	return frames[idx]
}

func bytesEqualSafe(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
