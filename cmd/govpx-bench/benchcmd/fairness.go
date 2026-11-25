package benchcmd

import (
	"fmt"
	"slices"
)

func medianMinMaxInt64(values []int64) (median, minVal, maxVal int64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sorted := append([]int64(nil), values...)
	slices.Sort(sorted)
	return sorted[len(sorted)/2], sorted[0], sorted[len(sorted)-1]
}

func collectEncodeFairnessWarnings(cfg benchConfig, r benchReport) []string {
	var warnings []string
	codec := r.Codec
	if codec == "" {
		codec = codecVP8
	}
	if codec == codecVP8 && r.Mode == "realtime" && cfg.CpuUsed > 0 {
		warnings = append(warnings,
			fmt.Sprintf("VP8 realtime -cpu-used=%d enables libvpx adaptive CPU speed; pass -cpu-used=-%d to pin speed", cfg.CpuUsed, cfg.CpuUsed))
	}
	if r.Reference != nil {
		ref := r.Reference
		if r.DroppedFrames != ref.DroppedFrames {
			warnings = append(warnings,
				fmt.Sprintf("frame drop mismatch (govpx dropped %d vs libvpx %d); ns/frame divides by input frames — pass -drop-frame=0 for equal-work timing",
					r.DroppedFrames, ref.DroppedFrames))
		}
		if ref.TimingSource == "wall" {
			warnings = append(warnings, "libvpx encode timed by subprocess wall clock instead of internal vpxenc-stats")
		}
	}
	return warnings
}

func collectDecodeFairnessWarnings(cfg benchConfig, r decodeBenchReport) []string {
	var warnings []string
	if r.Reference == nil {
		return warnings
	}
	ref := r.Reference
	if ref.TimingSource == "wall" || ref.TimingSource == "vpxdec-wall" {
		warnings = append(warnings,
			fmt.Sprintf("libvpx decode timed by subprocess wall clock (%s) rather than internal codec timer", ref.TimingSource))
	}
	if r.DecodedFrames != ref.DecodedFrames {
		warnings = append(warnings,
			fmt.Sprintf("decoded frame count mismatch (govpx %d vs libvpx %d)", r.DecodedFrames, ref.DecodedFrames))
	}
	threads := effectiveBenchThreads(cfg)
	if benchCodec(cfg) == codecVP8 && threads > 1 && ref.TimingSource == "oracle-bench" {
		warnings = append(warnings,
			fmt.Sprintf("VP8 decode thread mismatch (govpx -threads=%d vs libvpx oracle-bench 1T; build vpxdec for threaded reference)", threads))
	}
	return warnings
}

// runRepeatedEncodeBenchmark runs an encode benchmark `runs` times while
// reusing the prepared frames and on-disk raw I420 input, computing quality
// metrics once, alternating govpx-vs-libvpx execution order, and reporting
// independent medians (with min/max spread).
func runRepeatedEncodeBenchmark(cfg benchConfig, runs int) (benchReport, error) {
	if runs <= 1 {
		if benchCodec(cfg) == codecVP9 {
			return runVP9Benchmark(cfg)
		}
		return runBenchmark(cfg)
	}
	frames, err := prepareEncodeFrames(&cfg, nil)
	if err != nil {
		return benchReport{}, err
	}
	rawPath := ""
	cleanup := func() {}
	if suiteLibvpxReferencePath(cfg) != "" {
		rawPath, cleanup, err = writeTempI420File(frames)
		if err != nil {
			return benchReport{}, err
		}
		defer cleanup()
	}

	govpxSamples := make([]int64, 0, runs)
	refSamples := make([]int64, 0, runs)
	var base benchReport
	for runIdx := range runs {
		runCfg := cfg
		runCfg.preloadedFrames = frames
		runCfg.prewrittenRawPath = rawPath
		runCfg.libvpxWarmed = runIdx > 0
		runCfg.ReferenceFirst = runIdx%2 == 1
		if runIdx > 0 {
			// Encoding is deterministic; decode/PSNR/SSIM only needs to run once.
			runCfg.SkipQuality = true
			runCfg.CPUProfile = ""
		}
		var (
			res benchReport
			err error
		)
		if benchCodec(runCfg) == codecVP9 {
			res, err = runVP9Benchmark(runCfg)
		} else {
			res, err = runBenchmark(runCfg)
		}
		if err != nil {
			return benchReport{}, err
		}
		if runIdx == 0 {
			base = res
		}
		govpxSamples = append(govpxSamples, res.NSPerFrame)
		if res.Reference != nil {
			refSamples = append(refSamples, res.Reference.NSPerFrame)
		}
	}

	med, minNS, maxNS := medianMinMaxInt64(govpxSamples)
	base.Runs = runs
	base.NSPerFrame = med
	base.MinNSPerFrame = minNS
	base.MaxNSPerFrame = maxNS
	if med > 0 {
		base.EncodeFPS = 1e9 / float64(med)
		base.MacroblocksPerSec = benchmarkMacroblocks(base.Width, base.Height) * 1e9 / float64(med)
	}
	if base.DroppedFrames > 0 && base.EncodedFrames > 0 {
		base.EncodedNSPerFrame = (med * int64(base.Frames)) / int64(base.EncodedFrames)
	}
	if base.Reference != nil && len(refSamples) > 0 {
		refMed, refMin, refMax := medianMinMaxInt64(refSamples)
		base.Reference.NSPerFrame = refMed
		base.Reference.MinNSPerFrame = refMin
		base.Reference.MaxNSPerFrame = refMax
		if refMed > 0 {
			base.Reference.EncodeFPS = 1e9 / float64(refMed)
			base.Reference.MacroblocksPerSec = benchmarkMacroblocks(base.Width, base.Height) * 1e9 / float64(refMed)
		}
		if base.Reference.DroppedFrames > 0 && base.Reference.EncodedFrames > 0 {
			base.Reference.EncodedNSPerFrame = (refMed * int64(base.Frames)) / int64(base.Reference.EncodedFrames)
		}
		base.Comparison = buildComparisonReport(base, *base.Reference)
	}
	base.FairnessWarnings = collectEncodeFairnessWarnings(cfg, base)
	return base, nil
}
