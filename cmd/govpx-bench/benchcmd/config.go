package benchcmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	govpx "github.com/thesyncim/govpx"
)

const benchThreadsDefault = -1

func benchSummary(deadline string) benchConfigSummary {
	return benchConfigSummary{
		Deadline: deadline,
	}
}

type benchCLIOptions struct {
	format          string
	autoCompare     bool
	buildLibvpx     bool
	suite           string
	suiteRuns       int
	memProfile      string
	ffmpeg          string
	plotPath        string
	plotCSV         string
	plotJSON        string
	qualityFixtures bool
}

func defaultBenchCLIOptions() benchCLIOptions {
	return benchCLIOptions{
		format:      "text",
		autoCompare: true,
		ffmpeg:      "ffmpeg",
	}
}

func registerBenchFlags(fs *flag.FlagSet, cfg *benchConfig, opts *benchCLIOptions) {
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.IntVar(&cfg.Width, "width", 64, "frame width")
	fs.IntVar(&cfg.Height, "height", 64, "frame height")
	fs.IntVar(&cfg.Frames, "frames", 30, "number of frames")
	fs.IntVar(&cfg.FPS, "fps", 30, "frame rate")
	fs.IntVar(&cfg.BitrateKbps, "bitrate", 1200, "target bitrate in kbps")
	fs.StringVar(&cfg.Mode, "mode", "realtime", "encoder mode: realtime or good")
	fs.BoolVar(&cfg.Decode, "decode", false, "run decoder benchmark mode")
	fs.StringVar(&opts.suite, "suite", "", "run an encode comparison matrix instead of one case: quick, vp8, webrtc, vod, or stress")
	fs.IntVar(&opts.suiteRuns, "suite-runs", 1, "number of repeats per suite case; selects median govpx ns/frame")
	fs.BoolVar(&cfg.SkipQuality, "encode-only", false, "skip quality decode/PSNR/SSIM computation")
	cfg.Threads = benchThreadsDefault
	fs.Var(benchThreadsFlag{cfg: cfg}, "threads", "encoder thread count; default is VP9 realtime auto and 1 otherwise; 0 lets the encoder pick")
	fs.IntVar(&cfg.CpuUsed, "cpu-used", 8, "encoder CPU-used setting passed to govpx and optional libvpx comparison; negative realtime values pin libvpx Speed")
	fs.BoolVar(&cfg.PhaseTiming, "phase-timing", false, "include opt-in govpx encoder phase timing in the report")
	fs.StringVar(&cfg.LibvpxVpxenc, "libvpx-vpxenc", "", "optional libvpx vpxenc path for VP8 reference comparison")
	fs.StringVar(&cfg.LibvpxVpxencVP9, "libvpx-vpxenc-vp9", "", "optional libvpx vpxenc-vp9 path for VP9 reference comparison")
	fs.StringVar(&cfg.LibvpxOracle, "libvpx-oracle", "", "optional libvpx checksum oracle path for decoder reference timing")
	fs.StringVar(&cfg.Codec, "codec", codecVP8, "codec to benchmark: vp8 or vp9")
	fs.BoolVar(&opts.qualityFixtures, "quality-fixtures", false, "run the canonical VP9 quality-gate fixture suite (panning + checker)")
	registerQualityGateFlags(fs, &cfg.QualityGate)
	fs.BoolVar(&opts.autoCompare, "auto-libvpx", opts.autoCompare, "auto-locate the project's makefile-built vpxenc (and PATH vpxenc) for encode comparison; decoder mode also locates the oracle")
	fs.BoolVar(&opts.buildLibvpx, "build-libvpx", opts.buildLibvpx, "if -auto-libvpx finds no built binaries, run `make oracle-tools` to build them")
	fs.StringVar(&opts.ffmpeg, "ffmpeg", opts.ffmpeg, "ffmpeg binary for -plot mode; it must include the libvpx encoder and libvmaf filter")
	fs.StringVar(&opts.plotPath, "plot", "", "write a reproducible govpx-vs-libvpx VMAF SVG comparison plot using ffmpeg")
	fs.StringVar(&opts.plotCSV, "plot-csv", "", "optional CSV path for -plot per-frame VMAF metrics; defaults beside the SVG")
	fs.StringVar(&opts.plotJSON, "plot-json", "", "optional JSON path for -plot VMAF summary data; defaults beside the SVG")
	fs.StringVar(&cfg.CPUProfile, "cpuprofile", "", "write a CPU pprof profile of the measured encode/decode pass to this file")
	fs.StringVar(&opts.memProfile, "memprofile", "", "write a heap pprof profile after the measured pass to this file")
}

type benchThreadsFlag struct {
	cfg *benchConfig
}

func (f benchThreadsFlag) String() string {
	if f.cfg == nil || f.cfg.Threads == benchThreadsDefault {
		return "default"
	}
	return strconv.Itoa(f.cfg.Threads)
}

func (f benchThreadsFlag) Set(s string) error {
	threads, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	if threads < 0 {
		return fmt.Errorf("threads must be >= 0")
	}
	f.cfg.Threads = threads
	return nil
}

func resolveLibvpxDefaults(cfg *benchConfig, buildIfMissing bool) {
	root, haveRoot := findGovpxRoot()
	repoVpxenc := ""
	repoVpxencVP9 := ""
	repoOracle := ""
	if haveRoot {
		repoVpxenc = filepath.Join(root, "internal", "coracle", "build", "vpxenc")
		repoVpxencVP9 = filepath.Join(root, "internal", "coracle", "build", "vpxenc-vp9")
		repoOracle = filepath.Join(root, "internal", "coracle", "build", "govpx-vpx-oracle")
	}

	codec := benchCodec(*cfg)
	needVpxenc := !cfg.Decode && codec == codecVP8 && cfg.LibvpxVpxenc == "" && haveRoot && !isExecutable(repoVpxenc)
	needVpxencVP9 := !cfg.Decode && codec == codecVP9 && cfg.LibvpxVpxencVP9 == "" && haveRoot && !isExecutable(repoVpxencVP9)
	needOracle := cfg.Decode && cfg.LibvpxOracle == "" && haveRoot && !isExecutable(repoOracle)
	if buildIfMissing && haveRoot && (needVpxenc || needVpxencVP9 || needOracle) {
		target := "oracle-tools"
		if needVpxencVP9 {
			target = "vp9-vpxdec-tools"
		}
		fmt.Fprintf(os.Stderr, "govpx-bench: building libvpx oracle tools (make %s)\n", target)
		makeCmd := exec.Command("make", target)
		makeCmd.Dir = root
		makeCmd.Stdout = os.Stderr
		makeCmd.Stderr = os.Stderr
		if err := makeCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: make %s failed: %v\n", target, err)
		}
	}

	if !cfg.Decode && codec == codecVP8 && cfg.LibvpxVpxenc == "" {
		if isExecutable(repoVpxenc) {
			cfg.LibvpxVpxenc = repoVpxenc
		} else if path, err := exec.LookPath("vpxenc"); err == nil {
			cfg.LibvpxVpxenc = path
		}
	}
	if !cfg.Decode && codec == codecVP9 && cfg.LibvpxVpxencVP9 == "" {
		if isExecutable(repoVpxencVP9) {
			cfg.LibvpxVpxencVP9 = repoVpxencVP9
		} else if path, err := exec.LookPath("vpxenc-vp9"); err == nil {
			cfg.LibvpxVpxencVP9 = path
		}
	}
	if cfg.Decode && cfg.LibvpxOracle == "" && isExecutable(repoOracle) {
		cfg.LibvpxOracle = repoOracle
	}

	// Warn loudly when -auto-libvpx was requested but no reference binary
	// could be located. Without this the bench silently prints only the
	// govpx column, which looks like a successful run without comparison.
	if !cfg.Decode && codec == codecVP8 && cfg.LibvpxVpxenc == "" {
		fmt.Fprintln(os.Stderr, "govpx-bench: -auto-libvpx requested but vpxenc not found; "+
			"run `make oracle-tools` or pass -libvpx-vpxenc=<path> (or -build-libvpx=true)")
	}
	if !cfg.Decode && codec == codecVP9 && cfg.LibvpxVpxencVP9 == "" {
		fmt.Fprintln(os.Stderr, "govpx-bench: -auto-libvpx requested but vpxenc-vp9 not found; "+
			"run `make vp9-vpxdec-tools` or pass -libvpx-vpxenc-vp9=<path> (or -build-libvpx=true)")
	}
	if cfg.Decode && cfg.LibvpxOracle == "" {
		fmt.Fprintln(os.Stderr, "govpx-bench: -auto-libvpx requested but govpx-vpx-oracle not found; "+
			"run `make oracle-tools` or pass -libvpx-oracle=<path> (or -build-libvpx=true)")
	}
}

// findGovpxRoot walks up from the working directory (and, as a fallback,
// the executable's directory) looking for a parent that contains both a
// Makefile and the internal/coracle directory — the marker pair for the
// govpx repo root.
func findGovpxRoot() (string, bool) {
	if cwd, err := os.Getwd(); err == nil {
		if root, ok := walkUpForMarkers(cwd, "Makefile", filepath.Join("internal", "coracle")); ok {
			return root, true
		}
	}
	if exe, err := os.Executable(); err == nil {
		if root, ok := walkUpForMarkers(filepath.Dir(exe), "Makefile", filepath.Join("internal", "coracle")); ok {
			return root, true
		}
	}
	return "", false
}

func walkUpForMarkers(start string, markers ...string) (string, bool) {
	dir := start
	for {
		match := true
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
				match = false
				break
			}
		}
		if match {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0o111 != 0
}

func parityFor(cfg benchConfig) encoderParity {
	fps := cfg.FPS
	if fps <= 0 {
		fps = 30
	}
	threads := effectiveBenchThreads(cfg)
	tokenPartitions := 0
	for partitions := 1; partitions < threads && tokenPartitions < 3; partitions <<= 1 {
		tokenPartitions++
	}
	p := encoderParity{
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    fps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		UndershootPct:       100,
		OvershootPct:        15,
		Threads:             threads,
		TokenPartitions:     tokenPartitions,
		CpuUsed:             cfg.CpuUsed,
	}
	if cfg.Mode == "" || cfg.Mode == "realtime" {
		p.MinQuantizer = 2
		p.KeyFrameInterval = 3000
		p.BufferSizeMs = 1000
		p.BufferInitialSizeMs = 500
		p.BufferOptimalSizeMs = 600
		p.MaxIntraBitratePct = webrtcMaxIntraTargetPct(600, fps)
		p.DropFrameAllowed = true
		p.DropFrameWaterMark = 30
		p.NoiseSensitivity = 4
		p.StaticThreshold = 1
	}
	return p
}

func effectiveBenchThreads(cfg benchConfig) int {
	if cfg.Threads == benchThreadsDefault {
		return defaultBenchThreads(cfg)
	}
	if cfg.Threads < 0 {
		return 1
	}
	return cfg.Threads
}

func defaultBenchThreads(cfg benchConfig) int {
	if benchCodec(cfg) == codecVP9 && (cfg.Mode == "" || cfg.Mode == "realtime") {
		return 0
	}
	return 1
}

func webrtcMaxIntraTargetPct(maxIntraTarget int, fps int) int {
	if fps <= 0 {
		fps = 30
	}
	return max(300, maxIntraTarget*fps/20)
}

func benchmarkEncoderOptions(cfg benchConfig, deadline govpx.Deadline) govpx.EncoderOptions {
	p := parityFor(cfg)
	return govpx.EncoderOptions{
		Width:               cfg.Width,
		Height:              cfg.Height,
		FPS:                 cfg.FPS,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   cfg.BitrateKbps,
		MinQuantizer:        p.MinQuantizer,
		MaxQuantizer:        p.MaxQuantizer,
		Deadline:            deadline,
		CpuUsed:             p.CpuUsed,
		KeyFrameInterval:    p.KeyFrameInterval,
		BufferSizeMs:        p.BufferSizeMs,
		BufferInitialSizeMs: p.BufferInitialSizeMs,
		BufferOptimalSizeMs: p.BufferOptimalSizeMs,
		UndershootPct:       p.UndershootPct,
		OvershootPct:        p.OvershootPct,
		MaxIntraBitratePct:  p.MaxIntraBitratePct,
		DropFrameAllowed:    p.DropFrameAllowed,
		DropFrameWaterMark:  p.DropFrameWaterMark,
		NoiseSensitivity:    p.NoiseSensitivity,
		StaticThreshold:     p.StaticThreshold,
		Threads:             p.Threads,
		TokenPartitions:     p.TokenPartitions,
	}
}

func newBenchmarkEncoder(cfg benchConfig, deadline govpx.Deadline) (*govpx.VP8Encoder, error) {
	return govpx.NewVP8Encoder(benchmarkEncoderOptions(cfg, deadline))
}

func benchmarkDeadline(mode string) (govpx.Deadline, string, error) {
	switch mode {
	case "", "realtime":
		return govpx.DeadlineRealtime, "realtime", nil
	case "good":
		return govpx.DeadlineGoodQuality, "good", nil
	default:
		return 0, "", fmt.Errorf("unsupported mode %q", mode)
	}
}
