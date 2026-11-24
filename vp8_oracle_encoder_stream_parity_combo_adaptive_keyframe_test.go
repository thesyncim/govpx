//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityComboAdaptiveKF widens AdaptiveKeyFrames
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
func TestVP8OracleEncoderStreamByteParityComboAdaptiveKF(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

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
				CpuUsed:           strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
				Tuning:            TunePSNR,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(nil)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

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
