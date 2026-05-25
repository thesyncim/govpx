//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityComboThreadsTokens pins the strict
// byte-parity gate at the cross product of Threads ∈ {2, 4, 8} and
// TokenPartitions ∈ {1, 2, 3} on panning-128x128 and splitmv-96x96.
//
// The base matrix pins Threads alone or TokenPartitions alone at
// these sizes, but never both together. With both flags non-zero the
// row workers and the per-partition writer are active simultaneously,
// so this cross product is the natural unifying probe.
func TestVP8OracleEncoderStreamByteParityComboThreadsTokens(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

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
		{name: "threads8-tokens3-panning128-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, threads: 8, tokenPartitions: 3, extraArgs: []string{"--threads=8", "--token-parts=3"}},

		// splitmv-96x96 cross product. 6 combos.
		{name: "threads2-tokens1-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 2, tokenPartitions: 1, extraArgs: []string{"--threads=2", "--token-parts=1"}},
		{name: "threads2-tokens2-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 2, tokenPartitions: 2, extraArgs: []string{"--threads=2", "--token-parts=2"}},
		{name: "threads2-tokens3-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 2, tokenPartitions: 3, extraArgs: []string{"--threads=2", "--token-parts=3"}},
		{name: "threads4-tokens2-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 4, tokenPartitions: 2, extraArgs: []string{"--threads=4", "--token-parts=2"}},
		{name: "threads4-tokens3-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 4, tokenPartitions: 3, extraArgs: []string{"--threads=4", "--token-parts=3"}},
		{name: "threads8-tokens3-splitmv96-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv96, threads: 8, tokenPartitions: 3, extraArgs: []string{"--threads=8", "--token-parts=3"}},
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
				CpuUsed:           strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
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

func TestVP8OracleEncoderStreamByteParityComboThreadZeroERTokenIsolation(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

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
