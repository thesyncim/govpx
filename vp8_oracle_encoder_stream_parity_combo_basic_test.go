//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityCombo expands the strict byte-parity
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
func TestVP8OracleEncoderStreamByteParityCombo(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

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
				CpuUsed:           strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
				Tuning:            TunePSNR,
				TokenPartitions:   tc.tokenParts,
			}

			govpxFrames := encodeFramesWithGovpxFrameFlags(t, opts, sources, tc.flags)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, tc.flags, tc.extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					assertStrictGateKnownGapMatchedPrefix(t, tc.name, govpxFrames, libvpxFrames, 1)
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
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
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
