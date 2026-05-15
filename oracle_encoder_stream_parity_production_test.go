//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestOracleEncoderStreamByteParityProductionShortRuns pins the production
// benchmark/WebRTC-like configurations that are easy to miss in the smaller
// oracle matrices. In particular, the 1-frame vs 2-frame realtime runs catch
// cold-start control/config drift before it hides behind longer GOP averages.
func TestOracleEncoderStreamByteParityProductionShortRuns(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run production byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	type productionCase struct {
		name       string
		width      int
		height     int
		frames     int
		fps        int
		bitrate    int
		deadline   Deadline
		cpuUsed    int
		benchShape string
	}

	cases := []productionCase{
		{
			name:       "webrtc-720p-1500kbps-realtime-pinned-speed-1f",
			width:      1280,
			height:     720,
			frames:     1,
			fps:        30,
			bitrate:    1500,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "webrtc-720p-1500kbps-realtime-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    1500,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "bench-720p-2000kbps-realtime-pinned-speed-1f",
			width:      1280,
			height:     720,
			frames:     1,
			fps:        30,
			bitrate:    2000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "bench-720p-2000kbps-realtime-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    2000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "bench-720p-2000kbps-realtime-no-drop-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    2000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime-nodrop",
		},
		{
			name:       "bench-720p-8000kbps-realtime-no-drop-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    8000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime-nodrop",
		},
		{
			name:       "bench-1080p-8000kbps-good-2f",
			width:      1920,
			height:     1080,
			frames:     2,
			fps:        30,
			bitrate:    8000,
			deadline:   DeadlineGoodQuality,
			cpuUsed:    8,
			benchShape: "good",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(tc.width, tc.height, i)
			}

			opts, extraArgs := productionParityOptions(tc.width, tc.height, tc.fps, tc.bitrate, tc.deadline, tc.cpuUsed, tc.benchShape)
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, tc.bitrate, sources, extraArgs)
			assertSegmentByteParity(t, "production-short-run-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

func productionParityOptions(width, height, fps, bitrate int, deadline Deadline, cpuUsed int, shape string) (EncoderOptions, []string) {
	if fps <= 0 {
		fps = 30
	}
	minQ := 4
	keyFrameInterval := fps
	bufferSizeMs := 600
	bufferInitialMs := 400
	bufferOptimalMs := 500
	undershootPct := 100
	overshootPct := 15
	dropFrameAllowed := false
	dropFrameWaterMark := 0
	noiseSensitivity := 0
	staticThreshold := 0
	maxIntraBitratePct := 0
	if shape == "realtime" || shape == "realtime-nodrop" {
		minQ = 2
		keyFrameInterval = 3000
		bufferSizeMs = 1000
		bufferInitialMs = 500
		bufferOptimalMs = 600
		noiseSensitivity = 4
		staticThreshold = 1
		maxIntraBitratePct = max(300, 600*fps/20)
		if shape == "realtime" {
			dropFrameAllowed = true
			dropFrameWaterMark = 30
		}
	}
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   bitrate,
		MinQuantizer:        minQ,
		MaxQuantizer:        56,
		Deadline:            deadline,
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    keyFrameInterval,
		BufferSizeMs:        bufferSizeMs,
		BufferInitialSizeMs: bufferInitialMs,
		BufferOptimalSizeMs: bufferOptimalMs,
		UndershootPct:       undershootPct,
		OvershootPct:        overshootPct,
		MaxIntraBitratePct:  maxIntraBitratePct,
		DropFrameAllowed:    dropFrameAllowed,
		DropFrameWaterMark:  dropFrameWaterMark,
		NoiseSensitivity:    noiseSensitivity,
		StaticThreshold:     staticThreshold,
		Threads:             1,
		TokenPartitions:     0,
	}
	extraArgs := []string{
		"--end-usage=cbr",
		"--kf-min-dist=" + itoa(keyFrameInterval),
		"--kf-max-dist=" + itoa(keyFrameInterval),
		"--buf-sz=" + itoa(bufferSizeMs),
		"--buf-initial-sz=" + itoa(bufferInitialMs),
		"--buf-optimal-sz=" + itoa(bufferOptimalMs),
		"--undershoot-pct=" + itoa(undershootPct),
		"--overshoot-pct=" + itoa(overshootPct),
		"--threads=1",
		"--token-parts=0",
		"--noise-sensitivity=" + itoa(noiseSensitivity),
		"--drop-frame=" + itoa(dropFrameWaterMark),
	}
	if maxIntraBitratePct > 0 {
		extraArgs = append(extraArgs, "--max-intra-rate="+itoa(maxIntraBitratePct))
	}
	if staticThreshold > 0 {
		extraArgs = append(extraArgs, "--static-thresh="+itoa(staticThreshold))
	}
	return opts, extraArgs
}
