//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strconv"
	"testing"
)

func strictByteParityCPUUsed(deadline Deadline, cpuUsed int) int {
	if deadline == DeadlineRealtime && cpuUsed > 0 {
		// Positive realtime cpu-used is libvpx's wall-clock adaptive
		// auto-speed mode. Strict byte-parity cases pin the requested
		// speed explicitly so govpx and libvpx make matching encoder
		// decisions on every machine.
		return -cpuUsed
	}
	return cpuUsed
}

// extraArgsContainsKFDist reports whether the caller already supplied a
// `--kf-min-dist` or `--kf-max-dist` (with either an `=` form or the
// space-separated `--name value` form) in extraArgs, so the default
// `--kf-min-dist=999 --kf-max-dist=999` "disable auto-KF" pair shouldn't
// be appended on top.
func extraArgsContainsKFDist(extraArgs []string) bool {
	for _, arg := range extraArgs {
		switch {
		case arg == "--kf-min-dist", arg == "--kf-max-dist":
			return true
		case len(arg) >= len("--kf-min-dist=") && arg[:len("--kf-min-dist=")] == "--kf-min-dist=":
			return true
		case len(arg) >= len("--kf-max-dist=") && arg[:len("--kf-max-dist=")] == "--kf-max-dist=":
			return true
		}
	}
	return false
}

func libvpxEndUsageArgs(extraArgs []string) []string {
	for _, arg := range extraArgs {
		if arg == "--end-usage" || len(arg) >= len("--end-usage=") && arg[:len("--end-usage=")] == "--end-usage=" {
			return extraArgs
		}
	}
	args := make([]string, 0, len(extraArgs)+1)
	args = append(args, "--end-usage=cbr")
	args = append(args, extraArgs...)
	return args
}

// encodeFramesWithGovpx returns the raw per-frame VP8 packet payloads
// produced by govpx for the supplied sources. Dropped frames (CBR
// decimation drops, buffer-underrun drops, vp8_drop_encodedframe_overshoot
// drops) leave no payload in the returned slice, mirroring libvpx's
// observable output where a drop produces no IVF packet for that source
// frame. Callers that need to validate a specific drop pattern compare
// the returned slice against the libvpx oracle's IVF packet list.
func encodeFramesWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	appendResult := func(_ string, result EncodeResult) {
		if result.Dropped {
			return
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		appendResult("EncodeInto frame "+strconv.Itoa(i), result)
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		appendResult("FlushInto", result)
	}
	return out
}

// encodeFramesWithLibvpxOracle runs vpxenc-oracle on the supplied I420
// fixture and returns the per-frame VP8 packet payloads extracted from
// the resulting IVF file.
func encodeFramesWithLibvpxOracle(t *testing.T, vpxencOracle string, _ string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	cfg := vp8test.VpxencVP8Config{
		BinaryPath:           vpxencOracle,
		Width:                opts.Width,
		Height:               opts.Height,
		Frames:               len(sources),
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
		KeyFrameMinDist:      999,
		KeyFrameMaxDist:      999,
		ExtraArgs:            extraArgs,
	}
	// Only inject the default `--kf-min-dist=999 --kf-max-dist=999`
	// "no auto-KF" pair when the caller hasn't supplied its own kf-*
	// arguments via extraArgs. Several callers (long-fixture fuzz,
	// production parity, transitions, twopass fuzz, runtime-controls
	// parity) configure a finite KeyFrameInterval on the govpx side
	// and need libvpx's `cpi->key_frame_frequency` to match; passing
	// the default 999/999 silently in those cases would force govpx
	// to insert a keyframe at frame `KeyFrameInterval` while libvpx
	// keeps producing inter frames.
	if !extraArgsContainsKFDist(extraArgs) {
		cfg.KeyFrameDistSet = true
	}
	frames, diag, err := vp8test.VpxencVP8OracleFramePayloadsI420(
		encoderValidationI420Bytes(t, sources), cfg)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}
	return frames
}

func libvpxOracleDeadline(deadline Deadline) string {
	switch deadline {
	case DeadlineBestQuality:
		return "best"
	case DeadlineRealtime:
		return "rt"
	default:
		return "good"
	}
}

func libvpxOracleTimebaseArg(opts EncoderOptions) string {
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return strconv.Itoa(opts.TimebaseNum) + "/" + strconv.Itoa(opts.TimebaseDen)
	}
	return "1/" + strconv.Itoa(opts.FPS)
}

func libvpxOracleFPSArg(opts EncoderOptions) string {
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return strconv.Itoa(opts.TimebaseDen) + "/" + strconv.Itoa(opts.TimebaseNum)
	}
	return strconv.Itoa(opts.FPS) + "/1"
}

// parseVP8FramePartitionSizes returns the first-partition byte length
// declared in the VP8 frame header plus whether the frame is marked as
// a keyframe. Returns (0, false) when the payload is too short.
func parseVP8FramePartitionSizes(p []byte) (firstPart int, isKeyframe bool) {
	if len(p) < 3 {
		return 0, false
	}
	tag := uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
	isKeyframe = (tag & 1) == 0
	firstPart = int((tag >> 5) & 0x7FFFF)
	return firstPart, isKeyframe
}
