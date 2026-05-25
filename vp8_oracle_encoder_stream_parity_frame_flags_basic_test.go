//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityFrameFlags exercises per-frame
// VP8 flag scheduling — the EncodeForceKeyFrame /
// EncodeNoUpdateLast / EncodeNoUpdateGolden / EncodeNoUpdateAltRef /
// EncodeNoReferenceLast / EncodeNoReferenceGolden /
// EncodeNoReferenceAltRef / EncodeForceGoldenFrame /
// EncodeForceAltRefFrame / EncodeNoUpdateEntropy axes that the
// stock vpxenc binary cannot drive. The libvpx-side reference is the
// companion vpxenc-frameflags driver, which translates the same
// per-frame schedule into vpx_codec_encode flags.
//
// Cases that diverge are pinned with `limit:` so the gap is visible
// in the per-frame "byte mismatch (not asserted, ...)" log lines
// without regressing the strict gate.
func TestVP8OracleEncoderStreamByteParityFrameFlags(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	panningOdd := fixture{name: "panning-65x33", w: 65, h: 33, source: encoderValidationPanningFrame}

	cases := []struct {
		name                     string
		deadline                 Deadline
		cpuUsed                  int
		fx                       fixture
		limit                    int
		flags                    []EncodeFlags // per-frame; missing indices default to 0.
		rcMode                   RateControlMode
		rcModeSet                bool
		cqLevel                  int
		disableKf                bool
		tokenParts               int
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string // appended to libvpx driver argv.
	}{
		// Force-keyframe at frame 3. Both implementations must emit
		// a keyframe at index 3; the surrounding frames must
		// byte-match too.
		{name: "force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu0-48x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		// Force-keyframe at frame 1 (immediately after the initial key).
		{name: "force-kf-frame1-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		{name: "force-kf-frame1-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		// Force-keyframe on every frame (the "all keyframes by flag" axis
		// — orthogonal to the existing `kfInterval=1` axis which uses
		// kf-min/max-dist instead of the per-frame flag).
		{name: "force-kf-every-frame-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame}},
		// Explicit force-key requests still win when automatic
		// keyframes are disabled from construction.
		{name: "disable-kf-force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, disableKf: true, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--kf-disabled"}},
		// Hidden packets clear VP8 show_frame while keeping the encoded
		// reference update byte-identical to libvpx.
		{name: "invisible-keyframe-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeInvisibleFrame}},
		{name: "invisible-inter-frame1-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame}},
		{name: "invisible-inter-run-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame, EncodeInvisibleFrame, 0, EncodeInvisibleFrame}},
		{name: "invisible-no-upd-entropy-no-ref-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame | EncodeNoUpdateEntropy | EncodeNoUpdateLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef}},
		{name: "invisible-force-gf-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, EncodeInvisibleFrame | EncodeForceGoldenFrame, 0, EncodeInvisibleFrame | EncodeForceAltRefFrame}},
		{name: "invisible-force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeInvisibleFrame | EncodeForceKeyFrame}},
		{name: "invisible-no-upd-all-token8-er3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-no-upd-entropy-no-upd-all-token4-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-force-gf-arf-no-upd-entropy-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, EncodeInvisibleFrame | EncodeForceGoldenFrame | EncodeNoUpdateEntropy, 0, EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateEntropy}},

		// EncodeNoUpdateLast on every inter frame — exercises the
		// "freeze LAST" pattern used by WebRTC scalability layers.
		// libvpx vp8_cx_iface vp8e_set_frame_flags routes any of
		// {NO_UPD_LAST, NO_UPD_GF, NO_UPD_ARF, FORCE_GF, FORCE_ARF}
		// through vp8_update_reference with an inverted "upd" mask
		// (start at all-three, XOR off each NO_UPD_*); govpx mirrors
		// the same mask via libvpxExternalRefreshMask so the
		// downstream refresh / sourceAltRefActive bookkeeping lines
		// up on the next frame.
		{name: "no-upd-last-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast}},
		{name: "no-upd-last-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast}},
		// EncodeNoUpdateGolden / EncodeNoUpdateAltRef on every inter
		// frame. Together with the existing temporal SVC scoreboard
		// tests these pin the per-flag refresh accounting through
		// the libvpx upd-mask semantics described above.
		{name: "no-upd-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateGolden)},
		{name: "no-upd-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateAltRef)},
		{name: "no-upd-gf-arf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-all-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},

		// EncodeNoReferenceGolden / EncodeNoReferenceAltRef — drop
		// the GF/ARF reference from the inter prediction pool. The
		// picker must fall back to LAST-only, which is what WebRTC
		// scalability uses to prevent cross-layer dependencies.
		{name: "no-ref-last-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast)},
		{name: "no-ref-last-every3-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: everyNFlag(frames, 3, EncodeNoReferenceLast)},
		{name: "no-ref-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceGolden)},
		{name: "no-ref-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-arf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-all-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},

		// Combined no-update-Last+no-reference-Golden, the canonical
		// "base temporal layer" pattern from libvpx's
		// vpx_temporal_svc_encoder mode 1.
		{name: "no-upd-last-no-ref-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-last-no-upd-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoUpdateGolden)},
		{name: "no-ref-last-no-upd-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoUpdateAltRef)},
		{name: "no-ref-gf-no-upd-last-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoUpdateLast)},
		{name: "no-ref-arf-no-upd-gf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceAltRef|EncodeNoUpdateGolden)},

		// EncodeForceGoldenFrame / EncodeForceAltRefFrame at a
		// specific frame, mirroring the "manual GF/ARF refresh"
		// pattern. libvpx's upd-mask interpretation here is the
		// surprising part: with no NO_UPD_* bits set the mask stays
		// at 7 so refresh_last_frame, refresh_golden_frame and
		// refresh_alt_ref_frame ALL flip to 1 on the forced frame
		// (independent of which of FORCE_GF / FORCE_ARF the user
		// requested). libvpxExternalRefreshMask reproduces that.
		{name: "force-gf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame}},
		{name: "force-gf-frame4-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame}},
		{name: "force-gf-no-upd-last-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeNoUpdateLast}},
		{name: "force-gf-every3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: everyNFlag(frames, 3, EncodeForceGoldenFrame)},
		{name: "force-arf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame}},
		{name: "force-arf-no-upd-last-gf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden}},
		{name: "force-arf-every3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: everyNFlag(frames, 3, EncodeForceAltRefFrame)},
		{name: "force-gf-arf-same-frame-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeForceAltRefFrame}},
		{name: "force-gf-no-upd-entropy-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeNoUpdateEntropy}},
		{name: "force-arf-no-upd-entropy-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame | EncodeNoUpdateEntropy}},

		// EncodeNoUpdateEntropy on every inter frame — keeps the
		// reference entropy adaptation state frozen.
		{name: "no-upd-entropy-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "no-upd-entropy-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "no-upd-entropy-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "force-kf-no-upd-last-entropy-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame | EncodeNoUpdateLast | EncodeNoUpdateEntropy}},
		{name: "no-upd-entropy-no-upd-all-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-all-er2-token8-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-entropy-no-upd-all-er3-token8-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-entropy-no-upd-all-er3-token8-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-no-ref-all-odd-dims-realtime-cpu-3-65x33", deadline: DeadlineRealtime, cpuUsed: -3, fx: panningOdd, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-upd-last-no-ref-gf-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-no-upd-last-token4-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoUpdateLast)},
		{name: "force-gf-arf-token4-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: []EncodeFlags{0, 0, EncodeForceGoldenFrame, 0, EncodeForceAltRefFrame}},

		// Force-KF + no-update-GF/ARF (the layer-0 "I-frame anchor"
		// pattern used by 3-layer SVC mode 4 in libvpx's example).
		{name: "force-kf-no-upd-gf-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeForceKeyFrame | EncodeNoUpdateGolden | EncodeNoUpdateAltRef}},
		{name: "force-kf-frame3-realtime-vbr-cpu-3-16x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-q-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},

		// Token-partitions=2 / 4 crossed with per-frame flags to
		// confirm the partitioned writer also honors per-frame refs.
		{name: "force-kf-frame3-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "no-upd-last-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoUpdateLast)},

		// Good-quality deadline + per-frame flags to widen the
		// picker's mode-decision coverage.
		{name: "force-kf-frame3-good-quality-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "no-upd-last-good-quality-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      fps,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				CQLevel:                  tc.cqLevel,
				KeyFrameInterval:         999,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   TunePSNR,
				TokenPartitions:          tc.tokenParts,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
			}
			if tc.disableKf {
				opts.KeyFrameInterval = 0
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
