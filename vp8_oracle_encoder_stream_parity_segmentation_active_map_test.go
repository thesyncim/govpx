//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityActiveMapPatterns(t *testing.T) {
	vp8test.RequireOracle(t, "active-map byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
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
		CpuUsed:           0,
		Tuning:            TunePSNR,
	}

	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	cases := []struct {
		name                string
		pattern             string
		limit               int
		cpuUsed             int
		noiseSensitivity    int
		screenContentMode   int
		tokenPartitions     int
		sharpness           int
		tuning              Tuning
		tuningSet           bool
		errorResilient      bool
		errorResilientParts bool
		threads             int
		extraArgs           []string
	}{
		{name: "all", pattern: "all", limit: 0},
		{name: "checker", pattern: "checker", limit: 0},
		{name: "left-off", pattern: "left-off", limit: 0},
		{name: "right-off", pattern: "right-off", limit: 0},
		{name: "border-off", pattern: "border-off", limit: 0},
		{name: "off", pattern: "off", limit: 0},
		{name: "left-off-cpu-3", pattern: "left-off", cpuUsed: -3, limit: 0},
		{name: "right-off-cpu-3", pattern: "right-off", cpuUsed: -3, limit: 0},
		{name: "border-off-cpu-3", pattern: "border-off", cpuUsed: -3, limit: 0},
		{name: "checker-noise1", pattern: "checker", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "checker-noise2", pattern: "checker", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "checker-noise4", pattern: "checker", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "checker-noise5", pattern: "checker", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "checker-noise6", pattern: "checker", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "left-off-noise1", pattern: "left-off", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "left-off-noise2", pattern: "left-off", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "left-off-noise3", pattern: "left-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "left-off-noise4", pattern: "left-off", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "left-off-noise5", pattern: "left-off", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "left-off-noise6", pattern: "left-off", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "right-off-noise1", pattern: "right-off", noiseSensitivity: 1, limit: 0, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "right-off-noise2", pattern: "right-off", noiseSensitivity: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "right-off-noise3", pattern: "right-off", noiseSensitivity: 3, limit: 0, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "right-off-noise4", pattern: "right-off", noiseSensitivity: 4, limit: 0, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "right-off-noise5", pattern: "right-off", noiseSensitivity: 5, limit: 0, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "right-off-noise6", pattern: "right-off", noiseSensitivity: 6, limit: 0, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "right-off-noise3-cpu-3", pattern: "right-off", cpuUsed: -3, noiseSensitivity: 3, limit: 0, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "checker-noise3-screen-content2", pattern: "checker", noiseSensitivity: 3, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=2"}},
		{name: "right-off-noise3-screen-content2", pattern: "right-off", noiseSensitivity: 3, screenContentMode: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=2"}},
		{name: "checker-token-parts4", pattern: "checker", tokenPartitions: 2, limit: 0, extraArgs: []string{"--token-parts=2"}},
		{name: "right-off-sharpness4", pattern: "right-off", sharpness: 4, limit: 0, extraArgs: []string{"--sharpness=4"}},
		{name: "left-off-tune-ssim", pattern: "left-off", tuning: TuneSSIM, tuningSet: true, limit: 0, extraArgs: []string{"--tune=ssim"}},
		{name: "border-off-er3-token-parts4", pattern: "border-off", tokenPartitions: 2, errorResilient: true, errorResilientParts: true, limit: 0, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
		{name: "border-off-noise1", pattern: "border-off", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "border-off-noise2", pattern: "border-off", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "border-off-noise3", pattern: "border-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border-off-noise4", pattern: "border-off", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "border-off-noise5", pattern: "border-off", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "border-off-noise6", pattern: "border-off", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "checker-noise3-threads2", pattern: "checker", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "left-off-noise3-threads2", pattern: "left-off", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "right-off-noise3-threads2", pattern: "right-off", noiseSensitivity: 3, threads: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "border-off-noise3-threads2", pattern: "border-off", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseOpts := opts
			if tc.cpuUsed != 0 {
				caseOpts.CpuUsed = tc.cpuUsed
			}
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			caseOpts.ScreenContentMode = tc.screenContentMode
			caseOpts.TokenPartitions = tc.tokenPartitions
			caseOpts.Sharpness = tc.sharpness
			if tc.tuningSet {
				caseOpts.Tuning = tc.tuning
			}
			caseOpts.ErrorResilient = tc.errorResilient
			caseOpts.ErrorResilientPartitions = tc.errorResilientParts
			caseOpts.Threads = tc.threads
			apply := map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					if tc.pattern == "off" {
						mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
						return
					}
					mustRuntime(t, "SetActiveMap("+tc.pattern+")", e.SetActiveMap(activeMapPattern(tc.pattern, rows, cols), rows, cols))
				},
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, apply)
			extraArgs := []string{
				"--active-map=" + tc.pattern,
			}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "active-map-"+tc.name+"-64x64", caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "active-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityActiveMapOddDimensions(t *testing.T) {
	vp8test.RequireOracle(t, "active-map byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 65
		height     = 33
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
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
		CpuUsed:           0,
		Tuning:            TunePSNR,
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	cases := []struct {
		name             string
		pattern          string
		limit            int
		noiseSensitivity int
		extraArgs        []string
	}{
		{name: "checker", pattern: "checker"},
		{name: "left-off", pattern: "left-off"},
		{name: "right-off", pattern: "right-off"},
		{name: "border-off", pattern: "border-off"},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "left-off-noise3", pattern: "left-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "right-off-noise3", pattern: "right-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border-off-noise3", pattern: "border-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseOpts := opts
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap("+tc.pattern+")", e.SetActiveMap(activeMapPattern(tc.pattern, rows, cols), rows, cols))
				},
			})
			extraArgs := []string{
				"--active-map=" + tc.pattern,
			}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "active-map-odd-"+tc.name, caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "active-map-odd-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}
