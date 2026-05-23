//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleEncoderStreamByteParityRuntimeRateControlModeTransitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime rate-control mode-transition byte-parity gate")
	}
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps         = 30
		targetKbps  = 700
		frames      = 8
		switchFrame = 3
	)
	modes := []RateControlMode{
		RateControlCBR,
		RateControlVBR,
		RateControlCQ,
		RateControlQ,
	}

	for _, from := range modes {
		for _, to := range modes {
			if from == to {
				continue
			}
			for _, forceKeyFrame := range []bool{false, true} {
				name := runtimeRateControlModeName(from) + "-to-" + runtimeRateControlModeName(to)
				if forceKeyFrame {
					name += "-force-kf"
				}
				t.Run(name, func(t *testing.T) {
					opts := EncoderOptions{
						Width:             32,
						Height:            32,
						FPS:               fps,
						RateControlMode:   from,
						TargetBitrateKbps: targetKbps,
						MinQuantizer:      4,
						MaxQuantizer:      56,
						CQLevel:           runtimeRateControlModeCQLevel(from),
						KeyFrameInterval:  999,
						Deadline:          DeadlineRealtime,
						CpuUsed:           0,
						Tuning:            TunePSNR,
					}
					flags := make([]EncodeFlags, frames)
					if forceKeyFrame {
						flags[switchFrame] = EncodeForceKeyFrame
					}
					script := runtimeControlScript(frames, map[int]string{
						switchFrame: runtimeRateControlModeControlToken(to, targetKbps),
					})
					apply := map[int]func(*testing.T, *VP8Encoder){
						switchFrame: func(t *testing.T, e *VP8Encoder) {
							t.Helper()
							mustRuntime(t, "SetRateControl("+runtimeRateControlModeName(to)+")", e.SetRateControl(runtimeRateControlModeConfig(to, targetKbps)))
						},
					}
					sources := make([]Image, frames)
					for i := range sources {
						sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
					}
					govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
					libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, name, opts, targetKbps, sources, flags, []string{
						"--control-script=" + strings.Join(script, ","),
					})
					matchLimit := runtimeRateControlModeTransitionMatchLimit(from, to, forceKeyFrame, switchFrame)
					assertSegmentByteParity(t, "runtime-rc-mode-"+name, govpxFrames, libvpxFrames, matchLimit)
				})
			}
		}
	}
}

func TestVP8OracleEncoderStreamByteParityRuntimeRateControlModeControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime rate-control mode/control cross byte-parity gate")
	}
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps         = 30
		targetKbps  = 700
		frames      = 10
		switchFrame = 4
	)

	baseOpts := func(from RateControlMode) EncoderOptions {
		return EncoderOptions{
			Width:             32,
			Height:            32,
			FPS:               fps,
			RateControlMode:   from,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			CQLevel:           runtimeRateControlModeCQLevel(from),
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			Tuning:            TunePSNR,
		}
	}
	rateControlApply := func(mode RateControlMode) func(*testing.T, *VP8Encoder) {
		return func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetRateControl("+runtimeRateControlModeName(mode)+")", e.SetRateControl(runtimeRateControlModeConfig(mode, targetKbps)))
		}
	}

	type rateControlCrossCase struct {
		name          string
		opts          EncoderOptions
		to            RateControlMode
		flags         []EncodeFlags
		script        []string
		apply         map[int]func(*testing.T, *VP8Encoder)
		extraArgs     []string
		forceKeyFrame bool
	}
	cases := []rateControlCrossCase{
		{
			name: "cbr-to-vbr-threads2-token4",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlCBR)
				opts.Threads = 2
				opts.TokenPartitions = 2
				return opts
			}(),
			to:        RateControlVBR,
			extraArgs: []string{"--threads=2"},
		},
		{
			name: "cbr-to-cq-screen2-static500",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlCBR)
				opts.ScreenContentMode = 2
				opts.StaticThreshold = 500
				return opts
			}(),
			to:        RateControlCQ,
			extraArgs: []string{"--screen-content-mode=2", "--static-thresh=500"},
		},
		{
			name: "vbr-to-q-force-kf-token4",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlVBR)
				opts.TokenPartitions = 2
				return opts
			}(),
			to:            RateControlQ,
			forceKeyFrame: true,
		},
		{
			name: "q-to-vbr-threads2",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlQ)
				opts.Threads = 2
				return opts
			}(),
			to:        RateControlVBR,
			extraArgs: []string{"--threads=2"},
		},
	}

	for _, tc := range cases {
		if tc.script == nil {
			tc.script = runtimeControlScript(frames, map[int]string{
				switchFrame: runtimeRateControlModeControlToken(tc.to, targetKbps),
			})
		}
		if tc.apply == nil {
			tc.apply = map[int]func(*testing.T, *VP8Encoder){
				switchFrame: rateControlApply(tc.to),
			}
		}
		if tc.forceKeyFrame && tc.flags == nil {
			tc.flags = make([]EncodeFlags, frames)
			tc.flags[switchFrame] = EncodeForceKeyFrame
		}
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(tc.opts.Width, tc.opts.Height, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-rc-mode-cross-"+tc.name, tc.opts, targetKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-rc-mode-cross-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityRuntimeRateControlModeLongTailTransitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime rate-control mode-transition long-tail byte-parity gate")
	}
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps         = 30
		targetKbps  = 700
		frames      = 16
		switchFrame = 3
	)
	cases := []struct {
		name          string
		from          RateControlMode
		to            RateControlMode
		forceKeyFrame bool
		matchLimit    int
	}{
		{name: "vbr-to-cbr-long-tail", from: RateControlVBR, to: RateControlCBR},
		{name: "cq-to-cbr-no-force-long-tail", from: RateControlCQ, to: RateControlCBR},
		{name: "cq-to-cbr-force-kf-long-tail", from: RateControlCQ, to: RateControlCBR, forceKeyFrame: true},
		{name: "q-to-cbr-no-force-kf-long-tail", from: RateControlQ, to: RateControlCBR},
		{name: "cq-to-q-post-switch-tail", from: RateControlCQ, to: RateControlQ},
	}
	for _, tc := range cases {
		if tc.matchLimit == 0 {
			tc.matchLimit = runtimeRateControlModeTransitionMatchLimit(tc.from, tc.to, tc.forceKeyFrame, switchFrame)
		}
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:             32,
				Height:            32,
				FPS:               fps,
				RateControlMode:   tc.from,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				CQLevel:           runtimeRateControlModeCQLevel(tc.from),
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           0,
				Tuning:            TunePSNR,
			}
			flags := make([]EncodeFlags, frames)
			if tc.forceKeyFrame {
				flags[switchFrame] = EncodeForceKeyFrame
			}
			script := runtimeControlScript(frames, map[int]string{
				switchFrame: runtimeRateControlModeControlToken(tc.to, targetKbps),
			})
			apply := map[int]func(*testing.T, *VP8Encoder){
				switchFrame: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl("+runtimeRateControlModeName(tc.to)+")", e.SetRateControl(runtimeRateControlModeConfig(tc.to, targetKbps)))
				},
			}
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, flags, []string{
				"--control-script=" + strings.Join(script, ","),
			})
			assertSegmentByteParity(t, "runtime-rc-mode-long-tail-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
