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
	if opts.autoCompare && !plotMode {
		resolveLibvpxDefaults(&cfg, opts.buildLibvpx)
	}

	if opts.cpuProfile != "" {
		f, err := os.Create(opts.cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: create cpu profile: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: start cpu profile: %v\n", err)
			os.Exit(2)
		}
		defer pprof.StopCPUProfile()
	}

	var report any
	var err error
	if plotMode {
		report, err = runPlotComparison(cfg, plotOptions{
			ffmpegPath: opts.ffmpeg,
			svgPath:    opts.plotPath,
			csvPath:    opts.plotCSV,
			jsonPath:   opts.plotJSON,
		})
	} else if cfg.Decode {
		report, err = runDecodeBenchmark(cfg)
	} else {
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
		default:
			fmt.Fprintf(os.Stderr, "govpx-bench: unexpected report type %T\n", r)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "govpx-bench: unsupported -format %q (want text or json)\n", opts.format)
		os.Exit(2)
	}
}
