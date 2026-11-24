//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityRTCExternalRateControl(t *testing.T) {
	vp8test.RequireOracle(t, "RTC external-rate-control byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps    = 30
		frames = 16
		width  = 64
		height = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	cases := []struct {
		name                string
		targetKbps          int
		undershootPct       int
		overshootPct        int
		bufferSizeMs        int
		bufferInitialSizeMs int
		bufferOptimalSizeMs int
		dropFrameAllowed    bool
		dropFrameWaterMark  int
		tokenPartitions     int
		threads             int
		errorResilient      bool
		errorResilientParts bool
		screenContentMode   int
		sharpness           int
		extraArgs           []string
	}{
		{
			name:                "drop-buffer-low-bitrate",
			targetKbps:          80,
			bufferSizeMs:        200,
			bufferInitialSizeMs: 100,
			bufferOptimalSizeMs: 150,
			dropFrameAllowed:    true,
			dropFrameWaterMark:  60,
			extraArgs:           []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=60"},
		},
		{
			name:       "default-buffer-mid-bitrate",
			targetKbps: 700,
		},
		{
			name:          "undershoot-overshoot-edges",
			targetKbps:    700,
			undershootPct: 0,
			overshootPct:  100,
			extraArgs:     []string{"--undershoot-pct=0", "--overshoot-pct=100"},
		},
		{
			name:                "tight-buffer-mid-bitrate",
			targetKbps:          400,
			bufferSizeMs:        200,
			bufferInitialSizeMs: 100,
			bufferOptimalSizeMs: 150,
			dropFrameAllowed:    true,
			dropFrameWaterMark:  50,
			extraArgs:           []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=50"},
		},
		{
			name:            "token-parts4-mid-bitrate",
			targetKbps:      700,
			tokenPartitions: 2,
			extraArgs:       []string{"--token-parts=2"},
		},
		{
			name:       "threads2-mid-bitrate",
			targetKbps: 700,
			threads:    2,
			extraArgs:  []string{"--threads=2"},
		},
		{
			name:                "er3-token-parts4-mid-bitrate",
			targetKbps:          700,
			tokenPartitions:     2,
			errorResilient:      true,
			errorResilientParts: true,
			extraArgs:           []string{"--error-resilient=3", "--token-parts=2"},
		},
		{
			name:              "screen-content2-mid-bitrate",
			targetKbps:        700,
			screenContentMode: 2,
			extraArgs:         []string{"--screen-content-mode=2"},
		},
		{
			name:       "sharpness4-mid-bitrate",
			targetKbps: 700,
			sharpness:  4,
			extraArgs:  []string{"--sharpness=4"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:                    width,
				Height:                   height,
				FPS:                      fps,
				RateControlMode:          RateControlCBR,
				TargetBitrateKbps:        tc.targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				KeyFrameInterval:         999,
				Deadline:                 DeadlineRealtime,
				CpuUsed:                  -3,
				Tuning:                   TunePSNR,
				UndershootPct:            tc.undershootPct,
				OvershootPct:             tc.overshootPct,
				BufferSizeMs:             tc.bufferSizeMs,
				BufferInitialSizeMs:      tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:      tc.bufferOptimalSizeMs,
				DropFrameAllowed:         tc.dropFrameAllowed,
				DropFrameWaterMark:       tc.dropFrameWaterMark,
				RTCExternalRateControl:   true,
				TokenPartitions:          tc.tokenPartitions,
				Threads:                  tc.threads,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientParts,
				ScreenContentMode:        tc.screenContentMode,
				Sharpness:                tc.sharpness,
			}
			extraArgs := []string{"--end-usage=cbr", "--rtc-external=1"}
			extraArgs = append(extraArgs, tc.extraArgs...)
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "rtc-external-"+tc.name, opts, tc.targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "rtc-external-rate-control-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}
