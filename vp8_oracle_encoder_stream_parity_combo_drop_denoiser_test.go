//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityComboDropDenoiser pins the strict
// byte-parity gate at the cross product of DropFrameWaterMark ∈
// {1, 30, 60, 90} and NoiseSensitivity ∈ {1, 3, 6} on panning-64x64.
// The drop-frame gate and the temporal denoiser interact through the
// shared per-frame buffer-level + cyclic-refresh state machine, which
// no existing matrix pins together.
//
// 4 watermark × 3 noise = 12 cases at the base size; one cpu_used
// variant per noise level rounds out the cpu axis at the boundary.
func TestVP8OracleEncoderStreamByteParityComboDropDenoiser(t *testing.T) {
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
				CpuUsed:            strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
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
