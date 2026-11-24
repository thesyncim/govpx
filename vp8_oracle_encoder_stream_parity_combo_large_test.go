//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityComboBig pins the strict byte-parity
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
func TestVP8OracleEncoderStreamByteParityComboBig(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

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
		{name: "big-er1-2partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=1", "--token-parts=1"}},
		{name: "big-er1-4partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "big-er1-8partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=1", "--token-parts=3"}},
		// er3 + token partitions at 256x144 is strict across all partition
		// counts, narrowing the ER3/token gap to larger MB grids.
		{name: "big-er3-2partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=3", "--token-parts=1"}},
		{name: "big-er3-4partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
		{name: "big-er3-8partitions-256x144-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(256, 144), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		{name: "big-er3-2partitions-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=3", "--token-parts=1"}},
		{name: "big-er3-4partitions-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
		{name: "big-er3-8partitions-320x180-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 180), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		{name: "big-er1-4partitions-640x480-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(640, 480), errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "big-er3-2partitions-640x480-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(640, 480), errorResilient: true, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=3", "--token-parts=1"}},

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
				CpuUsed:                  strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
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
