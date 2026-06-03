package coracle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	ivfstream "github.com/thesyncim/govpx/internal/vpx/ivf"
)

// VpxencVP8Config describes a VP8 vpxenc-family run over raw I420 frames.
// Fields map directly to libvpx command-line options; callers own
// codec-specific policy such as translating public govpx options.
type VpxencVP8Config struct {
	BinaryPath           string
	Width                int
	Height               int
	Frames               int
	Deadline             string
	DisableWarningPrompt bool
	CPUUsed              int
	LagInFrames          int
	AutoAltRef           bool
	TargetBitrateKbps    int
	MinQ                 int
	MaxQ                 int
	OmitQuantizerArgs    bool
	Timebase             string
	FPS                  string
	KeyFrameDistSet      bool
	KeyFrameMinDist      int
	KeyFrameMaxDist      int
	ExtraEnv             []string
	ExtraArgs            []string
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
	// FrameSizes, when non-nil, lists the per-input-frame I420 dimensions for
	// runtime-resize parity runs where successive frames change size. It must
	// hold exactly Frames entries. Width/Height still describe the initial
	// coded dimensions handed to vpxenc; the resize:WxH runtime controls in
	// ExtraArgs drive the dimension changes inside the helper. When nil the
	// raw stream is validated as a uniform Width x Height x Frames block.
	FrameSizes [][2]int
}

// VpxencVP8TwoPassConfig describes a VP8 two-pass vpxenc run that shares one
// first-pass stats file across the pass invocations.
type VpxencVP8TwoPassConfig struct {
	FirstPassBinaryPath  string
	SecondPassBinaryPath string
	Common               VpxencVP8Config
	FirstPassExtraArgs   []string
	SecondPassExtraArgs  []string
}

// VpxencVP8OracleEncodeI420 encodes raw I420 frames with the patched VP8
// vpxenc-oracle helper and returns the IVF stream.
func VpxencVP8OracleEncodeI420(raw []byte, cfg VpxencVP8Config) (ivf []byte, diag []byte, err error) {
	return vpxencVP8EncodeI420(raw, cfg, VpxencOraclePath, "govpx-vpxenc-vp8-oracle-*")
}

// VpxencVP8EncodeI420 encodes raw I420 frames with the pinned stock VP8
// vpxenc helper and returns the IVF stream.
func VpxencVP8EncodeI420(raw []byte, cfg VpxencVP8Config) (ivf []byte, diag []byte, err error) {
	return vpxencVP8EncodeI420(raw, cfg, VpxencPath, "govpx-vpxenc-vp8-*")
}

func vpxencVP8EncodeI420(raw []byte, cfg VpxencVP8Config, defaultPath func() (string, error), tempPattern string) (ivf []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = defaultPath()
		if err != nil {
			return nil, nil, err
		}
	}
	return runVpxencVP8I420(raw, bin, tempPattern, cfg.Width,
		cfg.Height, cfg.Frames, cfg.vpxencArgs)
}

// VpxencVP8OracleFramePayloadsI420 encodes raw I420 frames with the patched VP8
// vpxenc-oracle helper and returns per-frame IVF payloads.
func VpxencVP8OracleFramePayloadsI420(raw []byte, cfg VpxencVP8Config) (frames [][]byte, diag []byte, err error) {
	ivf, diag, err := VpxencVP8OracleEncodeI420(raw, cfg)
	if err != nil {
		return nil, diag, err
	}
	frames, err = ivfstream.FramePayloads(ivf)
	if err != nil {
		return nil, diag, err
	}
	return frames, diag, nil
}

// VpxencVP8FirstPassStatsI420 runs VP8 vpxenc pass 1 over raw I420 frames and
// returns the emitted FIRSTPASS_STATS file.
func VpxencVP8FirstPassStatsI420(raw []byte, cfg VpxencVP8Config) (firstPassStats []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxencPath()
		if err != nil {
			return nil, nil, err
		}
	}
	return runVpxencVP8FirstPassStatsI420(raw, bin, "govpx-vpxenc-vp8-pass1-*", cfg)
}

// VpxencVP8OracleTraceI420 encodes raw I420 frames with the patched VP8
// vpxenc-oracle helper and returns the JSONL oracle trace emitted by the
// GOVPX_ORACLE_TRACE_OUT side channel.
func VpxencVP8OracleTraceI420(raw []byte, cfg VpxencVP8Config) (trace []byte, diag []byte, err error) {
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
	return runVpxencVP8TraceI420(raw, bin, "govpx-vpxenc-vp8-oracle-trace-*",
		cfg.Width, cfg.Height, cfg.Frames, cfg.vpxencArgs, cfg.ExtraEnv)
}

// VpxencVP8OracleEncodeTraceI420 encodes raw I420 frames with the patched VP8
// vpxenc-oracle helper and returns both the IVF stream and JSONL oracle trace
// from the same subprocess.
func VpxencVP8OracleEncodeTraceI420(raw []byte, cfg VpxencVP8Config) (ivf []byte, trace []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxencOraclePath()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	out, err := runVpxencVP8I420Files(raw, bin, "govpx-vpxenc-vp8-oracle-trace-*",
		cfg.Width, cfg.Height, cfg.Frames, cfg.vpxencArgs, true, cfg.ExtraEnv)
	if err != nil {
		return nil, nil, out.diag, err
	}
	return out.ivf, out.trace, out.diag, nil
}

// VpxencVP8TwoPassEncodeI420 runs VP8 vpxenc pass 1 and pass 2 over the same
// raw I420 source and returns the first-pass stats plus the second-pass IVF.
func VpxencVP8TwoPassEncodeI420(raw []byte, cfg VpxencVP8TwoPassConfig) (firstPassStats []byte, ivf []byte, diag []byte, err error) {
	out, err := vpxencVP8TwoPassI420(raw, cfg, false)
	if err != nil {
		return out.firstPassStats, out.ivf, out.diag, err
	}
	return out.firstPassStats, out.ivf, out.diag, nil
}

// VpxencVP8TwoPassTraceI420 runs VP8 vpxenc pass 1 and then a patched
// vpxenc-oracle pass 2 over the same raw I420 source, returning the
// second-pass JSONL oracle trace.
func VpxencVP8TwoPassTraceI420(raw []byte, cfg VpxencVP8TwoPassConfig) (firstPassStats []byte, trace []byte, diag []byte, err error) {
	out, err := vpxencVP8TwoPassI420(raw, cfg, true)
	if err != nil {
		return out.firstPassStats, out.trace, out.diag, err
	}
	return out.firstPassStats, out.trace, out.diag, nil
}

func vpxencVP8TwoPassI420(raw []byte, cfg VpxencVP8TwoPassConfig, trace bool) (vpxencVP8RunOutput, error) {
	common := cfg.Common
	if err := validateI420Raw("VP8 vpxenc", raw, common.Width, common.Height, common.Frames); err != nil {
		return vpxencVP8RunOutput{}, err
	}
	firstBin := cfg.FirstPassBinaryPath
	if firstBin == "" {
		firstBin = common.BinaryPath
	}
	if firstBin == "" {
		bin, err := VpxencPath()
		if err != nil {
			return vpxencVP8RunOutput{}, err
		}
		firstBin = bin
	}
	secondBin := cfg.SecondPassBinaryPath
	if secondBin == "" {
		secondBin = common.BinaryPath
	}
	if secondBin == "" {
		pathForSecondPass := VpxencPath
		if trace {
			pathForSecondPass = VpxencOraclePath
		}
		bin, err := pathForSecondPass()
		if err != nil {
			return vpxencVP8RunOutput{}, err
		}
		secondBin = bin
	}
	return runVpxencVP8TwoPassI420(
		raw,
		firstBin,
		secondBin,
		"govpx-vpxenc-vp8-twopass-*",
		common,
		trace,
		cfg.FirstPassExtraArgs,
		cfg.SecondPassExtraArgs,
	)
}

// VpxencVP8FrameFlagsEncodeI420 encodes raw I420 frames with the VP8
// vpxenc-frameflags helper and returns the IVF stream.
func VpxencVP8FrameFlagsEncodeI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) (ivf []byte, diag []byte, err error) {
	if len(cfg.FrameSizes) != 0 {
		if len(cfg.FrameSizes) != cfg.Frames {
			return nil, nil, fmt.Errorf("coracle: VP8 vpxenc has %d frame sizes for %d frames",
				len(cfg.FrameSizes), cfg.Frames)
		}
		if err := validateI420RawFrameSizes("VP8 vpxenc", raw, cfg.FrameSizes); err != nil {
			return nil, nil, err
		}
	} else if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxencFrameFlagsPath()
		if err != nil {
			return nil, nil, err
		}
	}
	if len(cfg.FrameSizes) != 0 {
		return runVpxencVP8I420FrameSizes(raw, bin, "govpx-vpxenc-vp8-frameflags-*",
			cfg.Width, cfg.Height, cfg.Frames, cfg.FrameSizes, cfg.vpxencArgs)
	}
	return runVpxencVP8I420(raw, bin, "govpx-vpxenc-vp8-frameflags-*",
		cfg.Width, cfg.Height, cfg.Frames, cfg.vpxencArgs)
}

// VpxencVP8FrameFlagsEncodeTraceI420 encodes raw I420 frames with the VP8
// vpxenc-frameflags helper and returns both IVF output and the JSONL trace.
func VpxencVP8FrameFlagsEncodeTraceI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) (ivf []byte, trace []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, nil, err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin, err = VpxencFrameFlagsPath()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	out, err := runVpxencVP8I420Files(raw, bin, "govpx-vpxenc-vp8-frameflags-trace-*",
		cfg.Width, cfg.Height, cfg.Frames, cfg.vpxencArgs, true, nil)
	if err != nil {
		return nil, nil, out.diag, err
	}
	return out.ivf, out.trace, out.diag, nil
}

// VpxencVP8FrameFlagsPayloadsI420 encodes raw I420 frames with the VP8
// vpxenc-frameflags helper and returns per-frame IVF payloads.
func VpxencVP8FrameFlagsPayloadsI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) (frames [][]byte, diag []byte, err error) {
	ivf, diag, err := VpxencVP8FrameFlagsEncodeI420(raw, cfg)
	if err != nil {
		return nil, diag, err
	}
	frames, err = ivfstream.FramePayloads(ivf)
	if err != nil {
		return nil, diag, err
	}
	return frames, diag, nil
}

func runVpxencVP8I420(raw []byte, bin string, tempPattern string, width int, height int, frames int, argsFor func(inPath string, outPath string) []string) (ivf []byte, diag []byte, err error) {
	out, err := runVpxencVP8I420Files(raw, bin, tempPattern, width, height, frames, argsFor, false, nil)
	if err != nil {
		return nil, out.diag, err
	}
	return out.ivf, out.diag, nil
}

// runVpxencVP8I420FrameSizes is the variable-frame-size counterpart to
// runVpxencVP8I420. frameSizes lists the per-input-frame I420 dimensions; the
// concatenated raw stream is validated against their summed size rather than a
// uniform width x height x frames block. width/height are still the initial
// coded dimensions handed to the helper.
func runVpxencVP8I420FrameSizes(raw []byte, bin string, tempPattern string, width int, height int, frames int, frameSizes [][2]int, argsFor func(inPath string, outPath string) []string) (ivf []byte, diag []byte, err error) {
	if err := validateI420RawFrameSizes("VP8 vpxenc", raw, frameSizes); err != nil {
		return nil, nil, err
	}
	out, err := runVpxencVP8I420FilesValidated(raw, bin, tempPattern, width, height, frames, argsFor, false, nil)
	if err != nil {
		return nil, out.diag, err
	}
	return out.ivf, out.diag, nil
}

func runVpxencVP8TraceI420(raw []byte, bin string, tempPattern string, width int, height int, frames int, argsFor func(inPath string, outPath string) []string, extraEnv []string) (trace []byte, diag []byte, err error) {
	out, err := runVpxencVP8I420Files(raw, bin, tempPattern, width, height, frames, argsFor, true, extraEnv)
	if err != nil {
		return nil, out.diag, err
	}
	return out.trace, out.diag, nil
}

type vpxencVP8RunOutput struct {
	ivf            []byte
	trace          []byte
	firstPassStats []byte
	diag           []byte
}

func runVpxencVP8I420Files(raw []byte, bin string, tempPattern string, width int, height int, frames int, argsFor func(inPath string, outPath string) []string, trace bool, extraEnv []string) (vpxencVP8RunOutput, error) {
	if err := validateI420Raw("VP8 vpxenc", raw, width, height, frames); err != nil {
		return vpxencVP8RunOutput{}, err
	}
	return runVpxencVP8I420FilesValidated(raw, bin, tempPattern, width, height, frames, argsFor, trace, extraEnv)
}

// runVpxencVP8I420FilesValidated is runVpxencVP8I420Files without the uniform
// frame-size precondition; callers that feed a variable-frame-size raw stream
// validate it themselves before invoking this.
func runVpxencVP8I420FilesValidated(raw []byte, bin string, tempPattern string, width int, height int, frames int, argsFor func(inPath string, outPath string) []string, trace bool, extraEnv []string) (vpxencVP8RunOutput, error) {
	dir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return vpxencVP8RunOutput{}, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	tracePath := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return vpxencVP8RunOutput{}, err
	}

	cmd := exec.Command(bin, argsFor(inPath, outPath)...)
	cmd.Env = os.Environ()
	if trace {
		cmd.Env = append(cmd.Env, "GOVPX_ORACLE_TRACE_OUT="+tracePath)
	}
	cmd.Env = append(cmd.Env, extraEnv...)
	diag, err := cmd.CombinedOutput()
	out := vpxencVP8RunOutput{diag: diag}
	if err != nil {
		return out, err
	}
	out.ivf, err = os.ReadFile(outPath)
	if err != nil {
		return out, err
	}
	if trace {
		out.trace, err = os.ReadFile(tracePath)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func runVpxencVP8FirstPassStatsI420(raw []byte, bin string, tempPattern string, cfg VpxencVP8Config) (firstPassStats []byte, diag []byte, err error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "pass1.ivf")
	fpfPath := filepath.Join(dir, "firstpass.fpf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, cfg.vpxencTwoPassArgs(inPath, outPath, fpfPath, 1)...)
	cmd.Env = append(os.Environ(), cfg.ExtraEnv...)
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	firstPassStats, err = os.ReadFile(fpfPath)
	if err != nil {
		return nil, diag, err
	}
	return firstPassStats, diag, nil
}

func runVpxencVP8TwoPassI420(raw []byte, firstBin string, secondBin string, tempPattern string, cfg VpxencVP8Config, trace bool, firstPassExtraArgs []string, secondPassExtraArgs []string) (vpxencVP8RunOutput, error) {
	if err := validateI420Raw("VP8 vpxenc", raw, cfg.Width, cfg.Height, cfg.Frames); err != nil {
		return vpxencVP8RunOutput{}, err
	}
	dir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return vpxencVP8RunOutput{}, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	pass1OutPath := filepath.Join(dir, "pass1.ivf")
	pass2OutPath := filepath.Join(dir, "pass2.ivf")
	fpfPath := filepath.Join(dir, "firstpass.fpf")
	tracePath := filepath.Join(dir, "pass2.jsonl")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return vpxencVP8RunOutput{}, err
	}

	firstPassCfg := cfg.withExtraArgs(firstPassExtraArgs)
	cmd1 := exec.Command(firstBin, firstPassCfg.vpxencTwoPassArgs(inPath, pass1OutPath, fpfPath, 1)...)
	cmd1.Env = append(os.Environ(), cfg.ExtraEnv...)
	pass1Diag, err := cmd1.CombinedOutput()
	out := vpxencVP8RunOutput{diag: pass1Diag}
	if err != nil {
		return out, err
	}
	out.firstPassStats, err = os.ReadFile(fpfPath)
	if err != nil {
		return out, err
	}

	secondPassCfg := cfg.withExtraArgs(secondPassExtraArgs)
	cmd2 := exec.Command(secondBin, secondPassCfg.vpxencTwoPassArgs(inPath, pass2OutPath, fpfPath, 2)...)
	cmd2.Env = os.Environ()
	if trace {
		cmd2.Env = append(cmd2.Env, "GOVPX_ORACLE_TRACE_OUT="+tracePath)
	}
	cmd2.Env = append(cmd2.Env, cfg.ExtraEnv...)
	pass2Diag, err := cmd2.CombinedOutput()
	out.diag = pass2Diag
	if err != nil {
		return out, err
	}
	out.ivf, err = os.ReadFile(pass2OutPath)
	if err != nil {
		return out, err
	}
	if trace {
		out.trace, err = os.ReadFile(tracePath)
		if err != nil {
			return out, err
		}
	}
	return out, nil
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
	}
	if cfg.DisableWarningPrompt {
		args = append(args, "--disable-warning-prompt")
	}
	args = append(args,
		"--"+deadline,
		"--cpu-used="+strconv.Itoa(cfg.CPUUsed),
		"--lag-in-frames="+strconv.Itoa(cfg.LagInFrames),
		autoAltRef,
		"--target-bitrate="+strconv.Itoa(cfg.TargetBitrateKbps),
		"--i420",
		"--width="+strconv.Itoa(cfg.Width),
		"--height="+strconv.Itoa(cfg.Height),
	)
	if !cfg.OmitQuantizerArgs {
		args = append(args,
			"--min-q="+strconv.Itoa(cfg.MinQ),
			"--max-q="+strconv.Itoa(cfg.MaxQ))
	}
	if cfg.Timebase != "" {
		args = append(args, "--timebase="+cfg.Timebase)
	}
	if cfg.FPS != "" {
		args = append(args, "--fps="+cfg.FPS)
	}
	args = append(args,
		"--limit="+strconv.Itoa(cfg.Frames),
		"--output="+outPath,
	)
	if cfg.KeyFrameDistSet {
		args = append(args,
			"--kf-min-dist="+strconv.Itoa(cfg.KeyFrameMinDist),
			"--kf-max-dist="+strconv.Itoa(cfg.KeyFrameMaxDist))
	}
	args = append(args, cfg.ExtraArgs...)
	args = append(args, inPath)
	return args
}

func (cfg VpxencVP8Config) withExtraArgs(args []string) VpxencVP8Config {
	if len(args) == 0 {
		return cfg
	}
	next := cfg
	next.ExtraArgs = append(append([]string{}, cfg.ExtraArgs...), args...)
	return next
}

func (cfg VpxencVP8Config) vpxencTwoPassArgs(inPath string, outPath string, fpfPath string, pass int) []string {
	args := cfg.vpxencArgs(inPath, outPath)
	input := args[len(args)-1]
	args = args[:len(args)-1]
	args = append(args,
		"--passes=2",
		"--pass="+strconv.Itoa(pass),
		"--fpf="+fpfPath,
		input,
	)
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

// VP8VpxencThreadsArg reports whether a vpxenc-style argument list requests
// parallel VP8 encoding with --threads=N where N is at least two.
func VP8VpxencThreadsArg(args []string) (threads int, parallel bool) {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--threads=") {
			continue
		}
		value := strings.TrimPrefix(arg, "--threads=")
		n := 0
		for _, c := range value {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		return n, n >= 2
	}
	return 0, false
}

func vp8BoolArg(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
