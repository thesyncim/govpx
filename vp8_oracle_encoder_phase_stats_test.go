//go:build govpx_oracle_trace && govpx_phase_stats

package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleEncoderStreamByteParityPhaseStatsNoop(t *testing.T) {
	vp8test.RequireOracle(t, "PhaseStats byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		width      = 64
		height     = 64
	)

	cases := []struct {
		name       string
		frames     int
		targetKbps int
		mutate     func(*EncoderOptions)
		extraArgs  []string
	}{
		{
			name:       "baseline-cbr",
			frames:     12,
			targetKbps: targetKbps,
			extraArgs:  []string{"--end-usage=cbr"},
		},
		{
			name:       "denoiser-threads-token",
			frames:     12,
			targetKbps: targetKbps,
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				opts.TokenPartitions = 2
			},
			extraArgs: []string{"--end-usage=cbr", "--noise-sensitivity=3", "--threads=2", "--token-parts=2"},
		},
		{
			name:       "lookahead-auto-alt-ref",
			frames:     14,
			targetKbps: targetKbps,
			mutate: func(opts *EncoderOptions) {
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
			},
			extraArgs: []string{"--end-usage=cbr", "--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name:       "actual-drop",
			frames:     30,
			targetKbps: 50,
			mutate: func(opts *EncoderOptions) {
				opts.TargetBitrateKbps = 50
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
			},
			extraArgs: []string{"--end-usage=cbr", "--target-bitrate=50", "--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=60"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(width, height, i)
			}
			var stats EncoderPhaseStats
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           -3,
				Tuning:            TunePSNR,
			}
			opts.PhaseStats = &stats
			if tc.mutate != nil {
				tc.mutate(&opts)
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "phase-stats-noop-"+tc.name, opts, tc.targetKbps, sources, nil, tc.extraArgs)
			assertSegmentByteParity(t, "phase-stats-noop-"+tc.name, govpxFrames, libvpxFrames, 0)
			if stats.KeyAttempts == 0 || stats.InterAttempts == 0 || stats.PacketWriteNS == 0 {
				t.Fatalf("PhaseStats did not record encode work: key_attempts=%d inter_attempts=%d packet_write_ns=%d", stats.KeyAttempts, stats.InterAttempts, stats.PacketWriteNS)
			}
		})
	}
}
