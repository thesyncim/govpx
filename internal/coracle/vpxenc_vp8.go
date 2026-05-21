package coracle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thesyncim/govpx/internal/testutil"
)

// VpxencVP8Config describes a VP8 vpxenc-oracle run over raw I420 frames.
// Fields map directly to libvpx command-line options; callers own
// codec-specific policy such as translating public govpx options.
type VpxencVP8Config struct {
	BinaryPath        string
	Width             int
	Height            int
	Frames            int
	Deadline          string
	CPUUsed           int
	LagInFrames       int
	AutoAltRef        bool
	TargetBitrateKbps int
	MinQ              int
	MaxQ              int
	Timebase          string
	FPS               string
	KeyFrameDistSet   bool
	KeyFrameMinDist   int
	KeyFrameMaxDist   int
	ExtraArgs         []string
}

// VpxencVP8FrameFlagsConfig describes a VP8 vpxenc-frameflags run over raw
// I420 frames. FrameFlags are libvpx VPX_EFLAG/VP8_EFLAG values indexed by
// input frame.
type VpxencVP8FrameFlagsConfig struct {
	BinaryPath        string
	Width             int
	Height            int
	Frames            int
	FPSNum            int
	FPSDen            int
	TargetBitrateKbps int
	MinQ              int
	MaxQ              int
	KeyFrameMinDist   int
	KeyFrameMaxDist   int
	Deadline          string
	CPUUsed           int
	EndUsage          string
	AutoAltRef        bool
	TokenPartitions   int
	CQLevel           int
	Threads           int
	FrameFlags        []uint32
	InvisibleFrames   []bool
	ExtraArgs         []string
}

// VpxencVP8OracleEncodeI420 encodes raw I420 frames with the patched VP8
// vpxenc-oracle helper and returns the IVF stream.
func VpxencVP8OracleEncodeI420(raw []byte, cfg VpxencVP8Config) (ivf []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxencOraclePath()
		if err != nil {
			return nil, nil, err
		}
	}
	return runVpxencVP8I420(raw, bin, "govpx-vpxenc-vp8-oracle-*", cfg.Width,
		cfg.Height, cfg.Frames, cfg.vpxencArgs)
}

// VpxencVP8OracleFramePayloadsI420 encodes raw I420 frames with the patched VP8
// vpxenc-oracle helper and returns per-frame IVF payloads.
func VpxencVP8OracleFramePayloadsI420(raw []byte, cfg VpxencVP8Config) (frames [][]byte, diag []byte, err error) {
	ivf, diag, err := VpxencVP8OracleEncodeI420(raw, cfg)
	if err != nil {
		return nil, diag, err
	}
	frames, err = testutil.IVFFramePayloads(ivf)
	if err != nil {
		return nil, diag, err
	}
	return frames, diag, nil
}

// VpxencVP8FrameFlagsEncodeI420 encodes raw I420 frames with the VP8
// vpxenc-frameflags helper and returns the IVF stream.
func VpxencVP8FrameFlagsEncodeI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) (ivf []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxencFrameFlagsPath()
		if err != nil {
			return nil, nil, err
		}
	}
	return runVpxencVP8I420(raw, bin, "govpx-vpxenc-vp8-frameflags-*",
		cfg.Width, cfg.Height, cfg.Frames, cfg.vpxencArgs)
}

// VpxencVP8FrameFlagsPayloadsI420 encodes raw I420 frames with the VP8
// vpxenc-frameflags helper and returns per-frame IVF payloads.
func VpxencVP8FrameFlagsPayloadsI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) (frames [][]byte, diag []byte, err error) {
	ivf, diag, err := VpxencVP8FrameFlagsEncodeI420(raw, cfg)
	if err != nil {
		return nil, diag, err
	}
	frames, err = testutil.IVFFramePayloads(ivf)
	if err != nil {
		return nil, diag, err
	}
	return frames, diag, nil
}

func runVpxencVP8I420(raw []byte, bin string, tempPattern string, width int, height int, frames int, argsFor func(inPath string, outPath string) []string) (ivf []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, width, height, frames); err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
	}

	cmd := exec.Command(bin, argsFor(inPath, outPath)...)
	cmd.Env = os.Environ()
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	ivf, err = os.ReadFile(outPath)
	if err != nil {
		return nil, diag, err
	}
	return ivf, diag, nil
}

func (cfg VpxencVP8Config) vpxencArgs(inPath string, outPath string) []string {
	deadline := cfg.Deadline
	if deadline == "" {
		deadline = "good"
	}
	autoAltRef := "--auto-alt-ref=0"
	if cfg.AutoAltRef {
		autoAltRef = "--auto-alt-ref=1"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--" + deadline,
		"--cpu-used=" + strconv.Itoa(cfg.CPUUsed),
		"--lag-in-frames=" + strconv.Itoa(cfg.LagInFrames),
		autoAltRef,
		"--target-bitrate=" + strconv.Itoa(cfg.TargetBitrateKbps),
		"--min-q=" + strconv.Itoa(cfg.MinQ),
		"--max-q=" + strconv.Itoa(cfg.MaxQ),
		"--i420",
		"--width=" + strconv.Itoa(cfg.Width),
		"--height=" + strconv.Itoa(cfg.Height),
		"--timebase=" + cfg.Timebase,
		"--fps=" + cfg.FPS,
		"--limit=" + strconv.Itoa(cfg.Frames),
		"--output=" + outPath,
	}
	if cfg.KeyFrameDistSet {
		args = append(args,
			"--kf-min-dist="+strconv.Itoa(cfg.KeyFrameMinDist),
			"--kf-max-dist="+strconv.Itoa(cfg.KeyFrameMaxDist))
	}
	args = append(args, cfg.ExtraArgs...)
	args = append(args, inPath)
	return args
}

func (cfg VpxencVP8FrameFlagsConfig) vpxencArgs(inPath string, outPath string) []string {
	deadline := cfg.Deadline
	if deadline == "" {
		deadline = "good"
	}
	endUsage := cfg.EndUsage
	if endUsage == "" {
		endUsage = "cbr"
	}
	args := []string{
		"--infile=" + inPath,
		"--outfile=" + outPath,
		"--width=" + strconv.Itoa(cfg.Width),
		"--height=" + strconv.Itoa(cfg.Height),
		"--fps-num=" + strconv.Itoa(cfg.FPSNum),
		"--fps-den=" + strconv.Itoa(cfg.FPSDen),
		"--frames=" + strconv.Itoa(cfg.Frames),
		"--target-bitrate=" + strconv.Itoa(cfg.TargetBitrateKbps),
		"--min-q=" + strconv.Itoa(cfg.MinQ),
		"--max-q=" + strconv.Itoa(cfg.MaxQ),
		"--kf-min-dist=" + strconv.Itoa(cfg.KeyFrameMinDist),
		"--kf-max-dist=" + strconv.Itoa(cfg.KeyFrameMaxDist),
		"--deadline=" + deadline,
		"--cpu-used=" + strconv.Itoa(cfg.CPUUsed),
		"--end-usage=" + endUsage,
		"--auto-alt-ref=" + vp8BoolArg(cfg.AutoAltRef),
		"--token-parts=" + strconv.Itoa(cfg.TokenPartitions),
		"--frame-flags=" + joinVP8FrameFlags(cfg.FrameFlags, cfg.Frames),
	}
	if len(cfg.InvisibleFrames) != 0 {
		args = append(args, "--invisible-frames="+joinVP8BoolSchedule(cfg.InvisibleFrames, cfg.Frames))
	}
	if cfg.CQLevel > 0 {
		args = append(args, "--cq-level="+strconv.Itoa(cfg.CQLevel))
	}
	if cfg.Threads > 0 && !extraArgsContainVP8Threads(cfg.ExtraArgs) {
		args = append(args, "--threads="+strconv.Itoa(cfg.Threads))
	}
	args = append(args, cfg.ExtraArgs...)
	return args
}

func joinVP8FrameFlags(flags []uint32, frames int) string {
	var b strings.Builder
	for i := 0; i < frames; i++ {
		if i != 0 {
			b.WriteByte(',')
		}
		var flag uint32
		if i < len(flags) {
			flag = flags[i]
		}
		b.WriteString(strconv.FormatUint(uint64(flag), 10))
	}
	return b.String()
}

func joinVP8BoolSchedule(values []bool, frames int) string {
	var b strings.Builder
	for i := 0; i < frames; i++ {
		if i != 0 {
			b.WriteByte(',')
		}
		if i < len(values) && values[i] {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

func extraArgsContainVP8Threads(args []string) bool {
	for _, arg := range args {
		if arg == "--threads" || strings.HasPrefix(arg, "--threads=") {
			return true
		}
	}
	return false
}

func vp8BoolArg(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
