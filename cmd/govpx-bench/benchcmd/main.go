package benchcmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

func Main() {
	cfg := benchConfig{}
	opts := defaultBenchCLIOptions()
	registerBenchFlags(flag.CommandLine, &cfg, &opts)
	flag.Parse()
	plotMode := opts.plotPath != ""
	suiteMode := opts.suite != ""
	qualityFixtureMode := opts.qualityFixtures
	if qualityFixtureMode {
		// -quality-fixtures always benchmarks the VP9 path; force the codec
		// so resolveLibvpxDefaults picks the vpxenc-vp9 binary.
		cfg.Codec = codecVP9
	}
	if opts.autoCompare && !plotMode {
		resolveLibvpxDefaults(&cfg, opts.buildLibvpx)
	}

	var report any
	var err error
	switch {
	case plotMode:
		report, err = runPlotComparison(cfg, plotOptions{
			ffmpegPath: opts.ffmpeg,
			svgPath:    opts.plotPath,
			csvPath:    opts.plotCSV,
			jsonPath:   opts.plotJSON,
		})
	case qualityFixtureMode:
		report, err = runQualityFixtureSuite(cfg)
	case suiteMode:
		report, err = runEncodeSuite(cfg, opts.suite, opts.suiteRuns)
	case cfg.Decode:
		report, err = runDecodeBenchmark(cfg)
	case benchCodec(cfg) == codecVP9:
		report, err = runVP9Benchmark(cfg)
	default:
		report, err = runBenchmark(cfg)
	}
	if opts.memProfile != "" {
		f, err := os.Create(opts.memProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: create mem profile: %v\n", err)
			os.Exit(2)
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "govpx-bench: write mem profile: %v\n", err)
			os.Exit(2)
		}
		f.Close()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "govpx-bench: %v\n", err)
		os.Exit(2)
	}
	switch opts.format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: encode json: %v\n", err)
			os.Exit(1)
		}
	case "", "text":
		switch r := report.(type) {
		case benchReport:
			os.Stdout.WriteString(formatEncodeReport(r))
		case decodeBenchReport:
			os.Stdout.WriteString(formatDecodeReport(r))
		case plotComparisonReport:
			os.Stdout.WriteString(formatPlotReport(r))
		case suiteReport:
			os.Stdout.WriteString(formatSuiteReport(r))
		default:
			fmt.Fprintf(os.Stderr, "govpx-bench: unexpected report type %T\n", r)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "govpx-bench: unsupported -format %q (want text or json)\n", opts.format)
		os.Exit(2)
	}
	if cfg.QualityGate.Enabled {
		if exitCode := evaluateQualityGate(cfg.QualityGate, report); exitCode != 0 {
			os.Exit(exitCode)
		}
	}
}

// evaluateQualityGate inspects report types that carry govpx PSNR/SSIM
// metrics and prints any violations. Returns the exit code the CLI should
// surface (0 = pass).
func evaluateQualityGate(gate QualityGate, report any) int {
	switch r := report.(type) {
	case benchReport:
		violations := gate.Evaluate(r)
		if len(violations) > 0 {
			fmt.Fprint(os.Stderr, formatQualityGateViolations(qualityGateLabel(r), violations))
			return 3
		}
	case suiteReport:
		failed := false
		for _, c := range r.Cases {
			violations := gate.Evaluate(c.Report)
			if len(violations) > 0 {
				fmt.Fprint(os.Stderr, formatQualityGateViolations(c.Name, violations))
				failed = true
			}
		}
		if failed {
			return 3
		}
	}
	return 0
}

func qualityGateLabel(r benchReport) string {
	codec := r.Codec
	if codec == "" {
		codec = "vp8"
	}
	return fmt.Sprintf("%s %dx%d@%dfps target=%dkbps mode=%s",
		codec, r.Width, r.Height, r.FPS, r.TargetBitrateKbps, r.Mode)
}
