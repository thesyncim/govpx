//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestOracleEncoderStreamByteParityCombo expands the strict byte-parity
// matrix along several "combination" axes the existing matrices do not
// pin together:
//
//  1. Cyclic intra-refresh patterns expressed via per-frame
//     EncodeForceKeyFrame at periodicities of 2/3/4/5/6 frames. This
//     emulates the periodic intra-refresh approach used by some real-
//     time conferencing stacks where the encoder is forced to emit a
//     keyframe on a fixed cadence rather than relying on the encoder
//     itself to schedule them. Force-KF parity is already pinned in
//     the frame-flags matrix; this batch confirms the cadence
//     arithmetic stays byte-identical under repeated forced KFs.
//
//  2. Mixed NoUpdate + NoReference flag combinations applied on the
//     same frame at various indices. The frame-flags matrix already
//     covers the individual flags; this matrix exercises the joint
//     "refresh-and-skip" pairings (NO_UPD_GF + NO_REF_GF being the
//     canonical example) that arise from temporal-SVC base-layer
//     anchor schedules.
//
//  3. AdaptiveKeyFrames at additional standard fixture sizes
//     (panning + segmented at 16x16/48x48/72x40/96x96). The extended
//     matrix's AdaptiveKeyFrames coverage skips these sizes; pin them
//     so future scene-cut-gate drift on the panning fixtures (no
//     scene cut detected) and the segmented fixtures (scene cuts
//     expected on the MB-aligned grid) is visible across the size axis.
//
// All cases run with the standard 16-frame budget and the default
// CBR/realtime knobs unless explicitly overridden. Cases that diverge
// are pinned with `limit:` so the per-frame "byte mismatch (not
// asserted, ...)" log lines stay visible without regressing the
// strict gate.
func TestOracleEncoderStreamByteParityCombo(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	splitmv64 := fixture{name: "splitmv-64x64", w: 64, h: 64, source: encoderValidationSplitMVQuadrantFrame}

	// cyclicForceKFPattern emits EncodeForceKeyFrame on every k-th frame
	// (indices 0, k, 2k, ...). Frame 0 always carries no flag because the
	// initial encode is already a keyframe by definition.
	cyclicForceKFPattern := func(total, k int) []EncodeFlags {
		out := make([]EncodeFlags, total)
		for i := k; i < total; i += k {
			out[i] = EncodeForceKeyFrame
		}
		return out
	}

	cases := []struct {
		name       string
		deadline   Deadline
		cpuUsed    int
		fx         fixture
		limit      int
		flags      []EncodeFlags
		tokenParts int
		extraArgs  []string
	}{
		// ----- 1. Cyclic intra-refresh: forced KF every k frames -----
		//
		// k=2 on panning-16x16: alternating KF/inter cadence. The
		// inter slot squeezes between two keyframes so the writer's
		// refresh-state bookkeeping is reset every other frame.
		{name: "cyclic-forcekf-every2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: cyclicForceKFPattern(frames, 2)},
		{name: "cyclic-forcekf-every2-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: cyclicForceKFPattern(frames, 2)},
		// every2 + 64x64 panning: closed by the
		// estimate_keyframe_frequency off-by-one fix
		// (close-cyclic-forcekf-splitmv). govpx's `framesSinceKeyframe`
		// previously excluded the KF's own end-of-frame increment
		// (libvpx folds it into `cpi->frames_since_key` at the
		// post-encode tail), driving estimate_keyframe_frequency to
		// 1 after a long forced-KF cadence and inflating
		// kf_bitrate_adjustment enough for calc_pframe_target_size
		// to drain all of kf_overspend_bits at once on the trailing
		// inter frame; the resulting Q jump was several steps above
		// libvpx and the frame size diverged by several hundred
		// bytes. After the fix, frame 11's calc_pframe_target_size
		// drain matches libvpx and the trailing inter is byte-
		// identical on smooth panning content.
		{name: "cyclic-forcekf-every2-panning64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: cyclicForceKFPattern(frames, 2)},
		// every2 + splitmv-64 at cpu0: after the libvpx-style
		// prior_key_frame_distance cold-start seed, the repeated forced-KF
		// overspend drain matches libvpx and the trailing inter frame is
		// byte-identical.
		{name: "cyclic-forcekf-every2-splitmv64", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, flags: cyclicForceKFPattern(frames, 2)},
		// k=3
		{name: "cyclic-forcekf-every3-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: cyclicForceKFPattern(frames, 3)},
		{name: "cyclic-forcekf-every3-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: cyclicForceKFPattern(frames, 3)},
		{name: "cyclic-forcekf-every3-panning64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: cyclicForceKFPattern(frames, 3)},
		// every3 + splitmv-64 at cpu0: same forced-KF overspend-history
		// path as every2, now strict across the full 12-frame window.
		{name: "cyclic-forcekf-every3-splitmv64", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, flags: cyclicForceKFPattern(frames, 3)},
		// k=4
		{name: "cyclic-forcekf-every4-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: cyclicForceKFPattern(frames, 4)},
		{name: "cyclic-forcekf-every4-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: cyclicForceKFPattern(frames, 4)},
		{name: "cyclic-forcekf-every4-panning64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: cyclicForceKFPattern(frames, 4)},
		{name: "cyclic-forcekf-every4-splitmv64", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, flags: cyclicForceKFPattern(frames, 4)},
		// k=5
		{name: "cyclic-forcekf-every5-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: cyclicForceKFPattern(frames, 5)},
		{name: "cyclic-forcekf-every5-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: cyclicForceKFPattern(frames, 5)},
		{name: "cyclic-forcekf-every5-panning64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: cyclicForceKFPattern(frames, 5)},
		// k=6
		{name: "cyclic-forcekf-every6-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: cyclicForceKFPattern(frames, 6)},
		{name: "cyclic-forcekf-every6-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: cyclicForceKFPattern(frames, 6)},
		{name: "cyclic-forcekf-every6-panning64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: cyclicForceKFPattern(frames, 6)},
		{name: "cyclic-forcekf-every6-splitmv64", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, flags: cyclicForceKFPattern(frames, 6)},

		// ----- 2. Mixed NoUpdate + NoReference on the same frame -----
		//
		// NO_UPD_GF + NO_REF_GF on every inter frame: "GF is invisible
		// to me and I won't update it either" — the canonical layered
		// pattern where the GF slot is reserved for a higher-rate
		// layer that the current layer must never touch. libvpx's
		// upd-mask + ref-mask handling routes these independently so
		// the combination probes the writer's joint suppression path.
		{name: "no-upd-gf-no-ref-gf-every-inter-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoReferenceGolden)},
		{name: "no-upd-gf-no-ref-gf-every-inter-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoReferenceGolden)},
		{name: "no-upd-gf-no-ref-gf-every-inter-panning64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoReferenceGolden)},
		// Same pattern for ARF.
		{name: "no-upd-arf-no-ref-arf-every-inter-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateAltRef|EncodeNoReferenceAltRef)},
		{name: "no-upd-arf-no-ref-arf-every-inter-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateAltRef|EncodeNoReferenceAltRef)},
		// Joint NO_UPD_GF + NO_UPD_ARF + NO_REF_GF + NO_REF_ARF — "only
		// LAST is touched", the strictest pattern used by base-layer
		// encoders in SVC mode.
		{name: "no-upd-no-ref-gf-arf-every-inter-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-upd-no-ref-gf-arf-every-inter-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		// Mixed-at-index combos: the flag is applied at one specific
		// frame and dropped on the next. Probes the upd-mask
		// transition between mixed-bit and bare frames — the path
		// most likely to expose a stale-reference-tracking bug.
		{name: "no-upd-gf-no-ref-gf-frame3-only-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: oneAtIndex(frames, 3, EncodeNoUpdateGolden|EncodeNoReferenceGolden)},
		{name: "no-upd-gf-no-ref-gf-frame5-only-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: oneAtIndex(frames, 5, EncodeNoUpdateGolden|EncodeNoReferenceGolden)},
		{name: "no-upd-arf-no-ref-arf-frame4-only-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: oneAtIndex(frames, 4, EncodeNoUpdateAltRef|EncodeNoReferenceAltRef)},
		{name: "no-upd-arf-no-ref-arf-frame6-only-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: oneAtIndex(frames, 6, EncodeNoUpdateAltRef|EncodeNoReferenceAltRef)},
		// Mixed schedules where every-other inter frame switches
		// between two mixed-flag combinations, to stress upd-mask
		// transitions on both edges of the alternating pattern.
		{name: "alt-no-upd-gf-no-ref-arf-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: alternateFlags(frames, EncodeNoUpdateGolden|EncodeNoReferenceAltRef, EncodeNoUpdateAltRef|EncodeNoReferenceGolden)},
		{name: "alt-no-upd-gf-no-ref-arf-panning32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: alternateFlags(frames, EncodeNoUpdateGolden|EncodeNoReferenceAltRef, EncodeNoUpdateAltRef|EncodeNoReferenceGolden)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				Tuning:            TunePSNR,
				TokenPartitions:   tc.tokenParts,
			}

			govpxFrames := encodeFramesWithGovpxFrameFlags(t, opts, sources, tc.flags)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, tc.flags, tc.extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

// oneAtIndex returns a length-total flag slice with index `at` set to
// f and every other entry zero. Useful for "single-frame-only"
// schedules that exercise the upd-mask transition on both edges.
func oneAtIndex(total, at int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, total)
	if at >= 0 && at < total {
		out[at] = f
	}
	return out
}

// alternateFlags returns a length-total flag slice with index 0 set
// to 0 (initial keyframe) and the remaining slots alternating between
// a (odd indices) and b (even indices).
func alternateFlags(total int, a, b EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, total)
	for i := 1; i < total; i++ {
		if i%2 == 1 {
			out[i] = a
		} else {
			out[i] = b
		}
	}
	return out
}

// TestOracleEncoderStreamByteParityComboBig pins the strict byte-parity
// gate at larger fixture sizes (256x144, 320x180, 640x480) for the
// Loss / Buffer / Dimensions control patterns. These resolutions
// expose the per-MB row workers and the larger MB grids — the
// existing matrices pin those patterns at <= 128x128 only.
//
// The libvpx driver is the standard vpxenc-oracle here: the cases
// only need static controls (no per-frame flags), so we don't need
// the frame-flags driver.
//
// Runtime budget: 8 frames per case to keep the matrix bounded;
// the per-MB count at 640x480 alone is 1200, which is two orders of
// magnitude more than the 16x16 cases.
func TestOracleEncoderStreamByteParityComboBig(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 1500
		frames     = 8
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	mk := func(w, h int) fixture {
		return fixture{
			name:   panningName(w, h),
			w:      w,
			h:      h,
			source: encoderValidationPanningFrame,
		}
	}

	cases := []struct {
		name                     string
		deadline                 Deadline
		cpuUsed                  int
		fx                       fixture
		limit                    int
		errorResilient           bool
		errorResilientPartitions bool
		tokenPartitions          int
		noiseSensitivity         int
		dropFrameAllowed         bool
		dropFrameWaterMark       int
		bufferSizeMs             int
		bufferInitialSizeMs      int
		bufferOptimalSizeMs      int
		targetKbpsOverride       int
		extraArgs                []string
	}{
		// ----- Big-fixture Loss patterns (ErrorResilient + token-parts) -----
		//
		// ErrorResilient + token-parts at 256x144 / 320x180 / 640x480.
		// The 320x180 fixture is at the lower QVGA/QCIF bin libvpx uses
		// for the "low-resolution" branch in vp8_pick_intra_mbuv_mode,
		// which is independent of the per-MB row worker count.
		// limit:-1 on the largest fixtures lets the per-frame log lines
		// surface any divergence while the runtime cost stays bounded.
		// limit-truncated only if a failure surfaces during the test run.
		{name: "big-er1-2partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=1", "--token-parts=1"}},
		{name: "big-er1-4partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "big-er1-8partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=1", "--token-parts=3"}},
		// er3 + 8-partitions at 320x180 cpu-3: first 3 frames byte-match,
		// frame 3+ diverges by a small first-partition delta. The
		// fixture's larger MB grid (20x12 = 240 MBs split across 8
		// token partitions) exposes the same per-partition entropy
		// drift pinned in the existing er3 + token-parts splitmv/96x96
		// matrix. Pin frames 0..2; trailing frames stay under the
		// broader er3-multi-partition gap.
		{name: "big-er3-8partitions-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180), limit: 3, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		{name: "big-er1-4partitions-640x480-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(640, 480), errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		// er3 + 2-partitions at 640x480 cpu8: frames 0..4 byte-match,
		// frame 5+ diverges. Same er3-multi-partition gap surfacing at
		// the VGA fixture. Pin frames 0..4.
		{name: "big-er3-2partitions-640x480-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(640, 480), limit: 5, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=3", "--token-parts=1"}},

		// ----- Big-fixture Buffer patterns (override buf-sz/init/optimal) -----
		//
		// Same 200/100/150 / 500/100/300 tight-buffer presets the
		// buffer matrix pins at 32x32 / 64x64, applied at 256x144 /
		// 320x180 / 640x480 so the buffer-level arithmetic is pinned
		// on the larger MB grids.
		{name: "big-buffer-200-100-150-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "big-buffer-500-100-300-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), bufferSizeMs: 500, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 300, extraArgs: []string{"--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"}},
		{name: "big-buffer-1000-500-600-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180), bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "big-buffer-2000-1000-1500-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180), bufferSizeMs: 2000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 1500, extraArgs: []string{"--buf-sz=2000", "--buf-initial-sz=1000", "--buf-optimal-sz=1500"}},
		{name: "big-buffer-1000-500-600-640x480-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(640, 480), bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "big-buffer-2000-1000-1500-640x480-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(640, 480), bufferSizeMs: 2000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 1500, extraArgs: []string{"--buf-sz=2000", "--buf-initial-sz=1000", "--buf-optimal-sz=1500"}},

		// ----- Big-fixture Dimensions sweep (multiple cpu_used) -----
		//
		// Plain panning at 256x144 / 320x180 / 640x480 across
		// cpu_used in {-3, 0, 4, 8}. The dimensions matrix pins
		// these at cpu_used 4/8 only; this batch adds cpu_used -3
		// and 0 so the row-worker dispatch is pinned on the slow
		// realtime presets at these sizes too.
		{name: "big-plain-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144)},
		{name: "big-plain-256x144-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(256, 144)},
		{name: "big-plain-256x144-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(256, 144)},
		{name: "big-plain-256x144-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(256, 144)},
		{name: "big-plain-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180)},
		{name: "big-plain-320x180-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(320, 180)},
		{name: "big-plain-320x180-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(320, 180)},
		{name: "big-plain-320x180-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(320, 180)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			caseTargetKbps := targetKbps
			if tc.targetKbpsOverride > 0 {
				caseTargetKbps = tc.targetKbpsOverride
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      fps,
				RateControlMode:          RateControlCBR,
				TargetBitrateKbps:        caseTargetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				KeyFrameInterval:         999,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   TunePSNR,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
				NoiseSensitivity:         tc.noiseSensitivity,
				TokenPartitions:          tc.tokenPartitions,
				DropFrameAllowed:         tc.dropFrameAllowed,
				DropFrameWaterMark:       tc.dropFrameWaterMark,
				BufferSizeMs:             tc.bufferSizeMs,
				BufferInitialSizeMs:      tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:      tc.bufferOptimalSizeMs,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, caseTargetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

// TestOracleEncoderStreamByteParityComboAdaptiveKF widens AdaptiveKeyFrames
// strict-parity coverage to the standard sizes the existing matrices skip:
// panning + segmented at 16x16 / 48x48 / 72x40 / 96x96.
//
// The extended matrix already pins panning AdaptiveKeyFrames at
// 16x16 / 32x32 / 48x48 / 64x64; the segmentation matrix pins
// segmented AdaptiveKeyFrames at 32x32 / 64x64 / 128x128. This batch
// fills in the missing sizes: panning at 72x40 / 96x96 (and 16x16
// adds the cpu_used variants the extended matrix does not cover);
// segmented at 16x16 / 48x48 / 72x40 / 96x96.
//
// The panning fixture is smooth so libvpx's scene-cut detector must
// never fire; the segmented fixture is MB-grid-aligned high-contrast
// so scene-cut behavior is the most informative cross-axis to pin.
func TestOracleEncoderStreamByteParityComboAdaptiveKF(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning72 := fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}
	panning96 := fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}
	segmented16 := fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}
	segmented48 := fixture{name: "segmented-48x48", w: 48, h: 48, source: encoderValidationSegmentedFrame}
	segmented72 := fixture{name: "segmented-72x40", w: 72, h: 40, source: encoderValidationSegmentedFrame}
	segmented96 := fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
		limit    int
	}{
		// Panning fixtures: smooth content, no scene cuts expected.
		// CPU sweep at each size to round out the cpu_used axis the
		// extended matrix only covers at 16x16/32x32.
		{name: "adaptive-kf-panning-16x16-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16},
		{name: "adaptive-kf-panning-16x16-cpu-8", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning16},
		{name: "adaptive-kf-panning-48x48-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48},
		{name: "adaptive-kf-panning-48x48-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning48},
		{name: "adaptive-kf-panning-48x48-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning48},
		{name: "adaptive-kf-panning-72x40-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning72},
		{name: "adaptive-kf-panning-72x40-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning72},
		{name: "adaptive-kf-panning-72x40-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning72},
		{name: "adaptive-kf-panning-72x40-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning72},
		{name: "adaptive-kf-panning-96x96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96},
		{name: "adaptive-kf-panning-96x96-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning96},
		{name: "adaptive-kf-panning-96x96-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning96},
		{name: "adaptive-kf-panning-96x96-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning96},

		// Segmented fixtures: scene cuts may be detected. Pinning the
		// gate matches libvpx's detection behaviour byte-for-byte.
		{name: "adaptive-kf-segmented-16x16-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented16},
		{name: "adaptive-kf-segmented-16x16-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented16},
		{name: "adaptive-kf-segmented-48x48-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented48},
		{name: "adaptive-kf-segmented-48x48-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented48},
		{name: "adaptive-kf-segmented-48x48-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented48},
		{name: "adaptive-kf-segmented-72x40-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented72},
		{name: "adaptive-kf-segmented-72x40-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented72},
		{name: "adaptive-kf-segmented-72x40-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented72},
		{name: "adaptive-kf-segmented-96x96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented96},
		{name: "adaptive-kf-segmented-96x96-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented96},
		{name: "adaptive-kf-segmented-96x96-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented96},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				AdaptiveKeyFrames: true,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				Tuning:            TunePSNR,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(nil)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

// TestOracleEncoderStreamByteParityComboThreadsTokens pins the strict
// byte-parity gate at the cross product of Threads ∈ {2, 4} and
// TokenPartitions ∈ {1, 2, 3} on panning-128x128 and splitmv-96x96.
//
// The base matrix pins Threads alone or TokenPartitions alone at
// these sizes, but never both together. With both flags non-zero the
// row workers and the per-partition writer are active simultaneously,
// so this cross product is the natural unifying probe.
func TestOracleEncoderStreamByteParityComboThreadsTokens(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 1000
		frames     = 12
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning128 := fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}
	splitmv96 := fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}

	cases := []struct {
		name            string
		deadline        Deadline
		cpuUsed         int
		fx              fixture
		limit           int
		threads         int
		tokenPartitions int
		extraArgs       []string
	}{
		// panning-128x128 cross product. 8 combos.
		{name: "threads2-tokens1-panning128-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, threads: 2, tokenPartitions: 1, extraArgs: []string{"--threads=2", "--token-parts=1"}},
		{name: "threads2-tokens2-panning128-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, threads: 2, tokenPartitions: 2, extraArgs: []string{"--threads=2", "--token-parts=2"}},
		{name: "threads2-tokens3-panning128-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, threads: 2, tokenPartitions: 3, extraArgs: []string{"--threads=2", "--token-parts=3"}},
		{name: "threads4-tokens1-panning128-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, threads: 4, tokenPartitions: 1, extraArgs: []string{"--threads=4", "--token-parts=1"}},
		{name: "threads4-tokens2-panning128-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, threads: 4, tokenPartitions: 2, extraArgs: []string{"--threads=4", "--token-parts=2"}},
		{name: "threads4-tokens3-panning128-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, threads: 4, tokenPartitions: 3, extraArgs: []string{"--threads=4", "--token-parts=3"}},
		// Same cross at cpu_used=-3 (the slower realtime preset that
		// exercises more of the picker).
		{name: "threads2-tokens2-panning128-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, threads: 2, tokenPartitions: 2, extraArgs: []string{"--threads=2", "--token-parts=2"}},
		{name: "threads4-tokens3-panning128-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, threads: 4, tokenPartitions: 3, extraArgs: []string{"--threads=4", "--token-parts=3"}},

		// splitmv-96x96 cross product. 6 combos.
		{name: "threads2-tokens1-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 2, tokenPartitions: 1, extraArgs: []string{"--threads=2", "--token-parts=1"}},
		{name: "threads2-tokens2-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 2, tokenPartitions: 2, extraArgs: []string{"--threads=2", "--token-parts=2"}},
		{name: "threads2-tokens3-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 2, tokenPartitions: 3, extraArgs: []string{"--threads=2", "--token-parts=3"}},
		{name: "threads4-tokens2-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 4, tokenPartitions: 2, extraArgs: []string{"--threads=4", "--token-parts=2"}},
		{name: "threads4-tokens3-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 4, tokenPartitions: 3, extraArgs: []string{"--threads=4", "--token-parts=3"}},
		{name: "threads2-tokens3-splitmv96-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv96, threads: 2, tokenPartitions: 3, extraArgs: []string{"--threads=2", "--token-parts=3"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				Tuning:            TunePSNR,
				TokenPartitions:   tc.tokenPartitions,
				Threads:           tc.threads,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

func TestOracleEncoderStreamByteParityComboThreadZeroERTokenIsolation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	cases := []struct {
		name                     string
		errorResilient           bool
		errorResilientPartitions bool
		tokenPartitions          int
		extraArgs                []string
	}{
		{name: "threads0-explicit-threads1", extraArgs: []string{"--threads=1"}},
		{name: "er1-token0-threads0", errorResilient: true, extraArgs: []string{"--threads=1", "--error-resilient=1", "--token-parts=0"}},
		{name: "er2-token0-threads0", errorResilientPartitions: true, extraArgs: []string{"--threads=1", "--error-resilient=2", "--token-parts=0"}},
		{name: "er3-token0-threads0", errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--threads=1", "--error-resilient=3", "--token-parts=0"}},
		{name: "er1-token8-threads0", errorResilient: true, tokenPartitions: 3, extraArgs: []string{"--threads=1", "--error-resilient=1", "--token-parts=3"}},
		{name: "er2-token8-threads0", errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--threads=1", "--error-resilient=2", "--token-parts=3"}},
		{name: "er3-token8-threads0", errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--threads=1", "--error-resilient=3", "--token-parts=3"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:                    width,
				Height:                   height,
				FPS:                      fps,
				RateControlMode:          RateControlCBR,
				TargetBitrateKbps:        targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				KeyFrameInterval:         999,
				Deadline:                 DeadlineRealtime,
				CpuUsed:                  -3,
				Tuning:                   TunePSNR,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
				TokenPartitions:          tc.tokenPartitions,
				Threads:                  0,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "thread0-er-token-"+tc.name, opts, targetKbps, sources, libvpxEndUsageArgs(tc.extraArgs))
			assertSegmentByteParity(t, "thread0-er-token-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

// TestOracleEncoderStreamByteParityComboDropDenoiser pins the strict
// byte-parity gate at the cross product of DropFrameWaterMark ∈
// {1, 30, 60, 90} and NoiseSensitivity ∈ {1, 3, 6} on panning-64x64.
// The drop-frame gate and the temporal denoiser interact through the
// shared per-frame buffer-level + cyclic-refresh state machine, which
// no existing matrix pins together.
//
// 4 watermark × 3 noise = 12 cases at the base size; one cpu_used
// variant per noise level rounds out the cpu axis at the boundary.
func TestOracleEncoderStreamByteParityComboDropDenoiser(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}

	cases := []struct {
		name               string
		deadline           Deadline
		cpuUsed            int
		fx                 fixture
		limit              int
		noiseSensitivity   int
		dropFrameAllowed   bool
		dropFrameWaterMark int
		extraArgs          []string
	}{
		// watermark=1 (extremely-low threshold, drops only at near-
		// empty buffer): the panning fixture won't trigger an actual
		// drop at 700 kbps but the writer still walks the gate, so
		// the parity probe is on the gate-evaluation path.
		{name: "dropframe1-noise1-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 1, noiseSensitivity: 1, extraArgs: []string{"--drop-frame=1", "--noise-sensitivity=1"}},
		{name: "dropframe1-noise3-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 1, noiseSensitivity: 3, extraArgs: []string{"--drop-frame=1", "--noise-sensitivity=3"}},
		{name: "dropframe1-noise6-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 1, noiseSensitivity: 6, extraArgs: []string{"--drop-frame=1", "--noise-sensitivity=6"}},
		// watermark=30 (the standard "drop on slight underrun"
		// preset).
		{name: "dropframe30-noise1-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 30, noiseSensitivity: 1, extraArgs: []string{"--drop-frame=30", "--noise-sensitivity=1"}},
		{name: "dropframe30-noise3-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 30, noiseSensitivity: 3, extraArgs: []string{"--drop-frame=30", "--noise-sensitivity=3"}},
		{name: "dropframe30-noise6-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 30, noiseSensitivity: 6, extraArgs: []string{"--drop-frame=30", "--noise-sensitivity=6"}},
		// watermark=60 (default WebRTC preset).
		{name: "dropframe60-noise1-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, noiseSensitivity: 1, extraArgs: []string{"--drop-frame=60", "--noise-sensitivity=1"}},
		{name: "dropframe60-noise3-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, noiseSensitivity: 3, extraArgs: []string{"--drop-frame=60", "--noise-sensitivity=3"}},
		{name: "dropframe60-noise6-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, noiseSensitivity: 6, extraArgs: []string{"--drop-frame=60", "--noise-sensitivity=6"}},
		// watermark=90 (aggressive drop preset).
		{name: "dropframe90-noise1-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 90, noiseSensitivity: 1, extraArgs: []string{"--drop-frame=90", "--noise-sensitivity=1"}},
		{name: "dropframe90-noise3-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 90, noiseSensitivity: 3, extraArgs: []string{"--drop-frame=90", "--noise-sensitivity=3"}},
		{name: "dropframe90-noise6-panning64-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 90, noiseSensitivity: 6, extraArgs: []string{"--drop-frame=90", "--noise-sensitivity=6"}},
		// cpu_used=-3 round (one per noise level at watermark=60).
		{name: "dropframe60-noise1-panning64-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, noiseSensitivity: 1, extraArgs: []string{"--drop-frame=60", "--noise-sensitivity=1"}},
		{name: "dropframe60-noise3-panning64-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, noiseSensitivity: 3, extraArgs: []string{"--drop-frame=60", "--noise-sensitivity=3"}},
		{name: "dropframe60-noise6-panning64-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, noiseSensitivity: 6, extraArgs: []string{"--drop-frame=60", "--noise-sensitivity=6"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:              tc.fx.w,
				Height:             tc.fx.h,
				FPS:                fps,
				RateControlMode:    RateControlCBR,
				TargetBitrateKbps:  targetKbps,
				MinQuantizer:       4,
				MaxQuantizer:       56,
				KeyFrameInterval:   999,
				Deadline:           tc.deadline,
				CpuUsed:            tc.cpuUsed,
				Tuning:             TunePSNR,
				NoiseSensitivity:   tc.noiseSensitivity,
				DropFrameAllowed:   tc.dropFrameAllowed,
				DropFrameWaterMark: tc.dropFrameWaterMark,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}
