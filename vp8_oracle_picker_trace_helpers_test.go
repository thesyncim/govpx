//go:build govpx_oracle_trace

package govpx

import "github.com/thesyncim/govpx/internal/testutil/vp8test"

func vp8OracleTraceConfig(vpxencOracle string, opts EncoderOptions, frames int, targetKbps int, extraEnv []string, extraArgs []string) vp8test.VpxencVP8Config {
	if targetKbps == 0 {
		targetKbps = opts.TargetBitrateKbps
	}
	keyFrameInterval := opts.KeyFrameInterval
	if keyFrameInterval == 0 {
		keyFrameInterval = 999
	}
	return vp8test.VpxencVP8Config{
		BinaryPath:           vpxencOracle,
		Width:                opts.Width,
		Height:               opts.Height,
		Frames:               frames,
		Deadline:             libvpxOracleDeadline(opts.Deadline),
		DisableWarningPrompt: true,
		CPUUsed:              opts.CpuUsed,
		LagInFrames:          opts.LookaheadFrames,
		AutoAltRef:           opts.AutoAltRef,
		TargetBitrateKbps:    targetKbps,
		MinQ:                 opts.MinQuantizer,
		MaxQ:                 opts.MaxQuantizer,
		Timebase:             libvpxOracleTimebaseArg(opts),
		FPS:                  libvpxOracleFPSArg(opts),
		KeyFrameDistSet:      true,
		KeyFrameMinDist:      keyFrameInterval,
		KeyFrameMaxDist:      keyFrameInterval,
		ExtraEnv:             extraEnv,
		ExtraArgs:            extraArgs,
	}
}

func vp8BestARNRPickerOracleConfig(vpxencOracle string, opts EncoderOptions, frames int, extraEnv []string) vp8test.VpxencVP8Config {
	return vp8OracleTraceConfig(
		vpxencOracle,
		opts,
		frames,
		opts.TargetBitrateKbps,
		extraEnv,
		[]string{
			"--end-usage=vbr",
			"--screen-content-mode=1",
			"--token-parts=1",
			"--threads=1",
			"--tune=ssim",
			"--arnr-maxframes=1",
			"--arnr-strength=1",
			"--arnr-type=2",
		},
	)
}
