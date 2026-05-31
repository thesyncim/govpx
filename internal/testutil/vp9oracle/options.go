package vp9oracle

import (
	"strconv"

	govpx "github.com/thesyncim/govpx"
)

// NormalizeFuzzOptionsForLibvpxCLI rewrites VP9 options that do not have an
// equivalent vpxenc-vp9 flag, or that intentionally exercise known encoder
// parity gaps, so a fuzzed govpx keyframe and a libvpx CLI keyframe remain
// comparable.
func NormalizeFuzzOptionsForLibvpxCLI(opts govpx.VP9EncoderOptions) govpx.VP9EncoderOptions {
	opts.DeltaQUV = 0
	opts.ColorRange = govpx.VP9ColorRangeStudio
	opts.MinBitrateKbps = 0
	opts.MaxBitrateKbps = 0
	opts.AdaptiveKeyFrames = false
	opts.AQMode = govpx.VP9AQNone
	opts.NoiseSensitivity = 0
	return opts
}

// LibvpxArgsFromOptions builds the vpxenc-vp9 extra-arg slice for a
// VP9EncoderOptions value. The returned args are intended to follow the pinned
// defaults in vp9test.VpxencVP9EncodeI420 so duplicate vpxenc keys use the
// usual last-wins behavior.
func LibvpxArgsFromOptions(opts govpx.VP9EncoderOptions) []string {
	args := make([]string, 0, 32)

	switch opts.Deadline {
	case govpx.DeadlineBestQuality:
		args = append(args, "--best")
	case govpx.DeadlineGoodQuality:
		args = append(args, "--good")
	case govpx.DeadlineRealtime:
		args = append(args, "--rt")
	}

	switch opts.RateControlMode {
	case govpx.RateControlCBR:
		args = append(args, "--end-usage=cbr")
	case govpx.RateControlVBR:
		args = append(args, "--end-usage=vbr")
	case govpx.RateControlCQ:
		args = append(args, "--end-usage=cq")
	case govpx.RateControlQ:
		args = append(args, "--end-usage=q")
	}

	effMinQ, effMaxQ, effCQ := NormalizedPublicQuantizers(opts)
	args = append(args,
		"--min-q="+strconv.Itoa(effMinQ),
		"--max-q="+strconv.Itoa(effMaxQ),
		"--cq-level="+strconv.Itoa(effCQ),
		"--cpu-used="+strconv.Itoa(int(opts.CpuUsed)),
		"--target-bitrate="+strconv.Itoa(opts.TargetBitrateKbps),
		"--threads="+strconv.Itoa(opts.Threads),
		"--tile-rows="+strconv.Itoa(int(opts.Log2TileRows)),
		"--aq-mode="+strconv.Itoa(int(opts.AQMode)),
		"--sharpness="+strconv.Itoa(int(opts.Sharpness)),
		"--noise-sensitivity="+strconv.Itoa(int(opts.NoiseSensitivity)),
		"--disable-loopfilter="+strconv.Itoa(int(opts.DisableLoopfilter)),
		"--color-space="+LibvpxColorSpaceArg(opts.ColorSpace),
		"--tune-content="+LibvpxTuneContentArg(opts.ScreenContentMode),
		"--undershoot-pct="+strconv.Itoa(opts.UndershootPct),
		"--overshoot-pct="+strconv.Itoa(opts.OvershootPct),
		"--max-intra-rate="+strconv.Itoa(opts.MaxIntraBitratePct),
		"--max-inter-rate="+strconv.Itoa(opts.MaxInterBitratePct),
		"--buf-sz="+strconv.Itoa(opts.BufferSizeMs),
		"--buf-initial-sz="+strconv.Itoa(opts.BufferInitialSizeMs),
		"--buf-optimal-sz="+strconv.Itoa(opts.BufferOptimalSizeMs),
	)

	if opts.MinKeyframeInterval > 0 {
		args = append(args, "--kf-min-dist="+strconv.Itoa(opts.MinKeyframeInterval))
	}
	if opts.MaxKeyframeInterval > 0 {
		args = append(args, "--kf-max-dist="+strconv.Itoa(opts.MaxKeyframeInterval))
	}
	if opts.Lossless {
		args = append(args, "--lossless=1")
	} else {
		args = append(args, "--lossless=0")
	}
	if opts.ErrorResilient {
		args = append(args, "--error-resilient=1")
	} else {
		args = append(args, "--error-resilient=0")
	}
	if opts.FrameParallelDecodingSet {
		if opts.FrameParallelDecoding {
			args = append(args, "--frame-parallel=1")
		} else {
			args = append(args, "--frame-parallel=0")
		}
	}
	if opts.FPS > 0 {
		args = append(args, "--fps="+strconv.Itoa(opts.FPS)+"/1")
	}

	return args
}

// NormalizedPublicQuantizers mirrors the public VP9 option defaults applied by
// the encoder before rate control. Oracle callers use it so vpxenc receives
// the same effective operating quantizer range as govpx.
func NormalizedPublicQuantizers(opts govpx.VP9EncoderOptions) (minQ, maxQ, cqLevel int) {
	const (
		defaultMinQ = 4
		defaultMaxQ = 56
		defaultCQ   = 32
	)
	minQ = opts.MinQuantizer
	maxQ = opts.MaxQuantizer
	if minQ == 0 && maxQ == 0 {
		minQ = defaultMinQ
		maxQ = defaultMaxQ
	}
	cqLevel = opts.CQLevel
	if cqLevel == 0 {
		if minQ == maxQ {
			cqLevel = minQ
		} else {
			cqLevel = defaultCQ
			if cqLevel < minQ {
				cqLevel = minQ
			}
			if cqLevel > maxQ {
				cqLevel = maxQ
			}
		}
	}
	return minQ, maxQ, cqLevel
}

// LibvpxColorSpaceArg maps a VP9ColorSpace value to the vpxenc --color-space
// token names.
func LibvpxColorSpaceArg(cs govpx.VP9ColorSpace) string {
	switch cs {
	case govpx.VP9ColorSpaceBT601:
		return "bt601"
	case govpx.VP9ColorSpaceBT709:
		return "bt709"
	case govpx.VP9ColorSpaceSMPTE170:
		return "smpte170"
	case govpx.VP9ColorSpaceSMPTE240:
		return "smpte240"
	case govpx.VP9ColorSpaceBT2020:
		return "bt2020"
	case govpx.VP9ColorSpaceReserved:
		return "reserved"
	case govpx.VP9ColorSpaceSRGB:
		return "sRGB"
	default:
		return "unknown"
	}
}

// LibvpxTuneContentArg maps VP9 screen-content mode values onto vpxenc's
// --tune-content token set.
func LibvpxTuneContentArg(mode int8) string {
	switch mode {
	case int8(govpx.VP9ScreenContentScreen):
		return "screen"
	case int8(govpx.VP9ScreenContentFilm):
		return "film"
	default:
		return "default"
	}
}
