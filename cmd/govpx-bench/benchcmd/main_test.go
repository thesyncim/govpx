package benchcmd

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	govpx "github.com/thesyncim/govpx"
)

func TestRunBenchmarkOutputsJSONMetrics(t *testing.T) {
	report, err := runBenchmark(benchConfig{
		Width:       16,
		Height:      16,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
	})
	if err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	if report.Encoder != "govpx" || report.Mode != "realtime" {
		t.Fatalf("identity = %s/%s, want govpx/realtime", report.Encoder, report.Mode)
	}
	if report.Width != 16 || report.Height != 16 || report.Frames != 3 || report.EncodedFrames == 0 {
		t.Fatalf("dimensions/counts = %+v", report)
	}
	if report.NSPerFrame <= 0 || report.EncodeFPS <= 0 || report.MacroblocksPerSec <= 0 || report.LatencyNS.P50 <= 0 || report.OutputBytes <= 0 {
		t.Fatalf("timing/output metrics = ns:%d fps:%f mbps:%f p50:%d bytes:%d", report.NSPerFrame, report.EncodeFPS, report.MacroblocksPerSec, report.LatencyNS.P50, report.OutputBytes)
	}
	if report.AllocsPerFrame != 0 {
		t.Fatalf("AllocsPerFrame = %f, want 0 for measured encode pass", report.AllocsPerFrame)
	}
	if report.PSNR <= 0 || report.SSIM <= 0 || report.SSIM > 1 || report.QualityFrames != 3 || report.QualitySkipped || report.Quantizers.Min <= 0 || report.Quantizers.Max < report.Quantizers.Min || len(report.QuantizerHist) == 0 {
		t.Fatalf("quality/quantizer metrics = psnr:%f ssim:%f frames:%d skipped:%v quant:%+v hist:%v", report.PSNR, report.SSIM, report.QualityFrames, report.QualitySkipped, report.Quantizers, report.QuantizerHist)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
}

func TestRunBenchmarkIncludesLibvpxReference(t *testing.T) {
	report, err := runBenchmark(benchConfig{
		Width:        16,
		Height:       16,
		Frames:       3,
		FPS:          30,
		BitrateKbps:  1200,
		Mode:         "realtime",
		LibvpxVpxenc: fakeVpxencPath(t),
	})
	if err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	if report.Reference == nil {
		t.Fatalf("reference = nil, want fake libvpx report")
	}
	if report.Reference.Encoder != "libvpx-vp8" || report.Reference.EncodedFrames != 3 || report.Reference.OutputBytes <= 0 {
		t.Fatalf("reference = %+v, want libvpx-vp8 with 3 encoded frames and bytes", *report.Reference)
	}
	if report.Reference.KeyframeBytes <= 0 || report.Reference.AvgInterBytes <= 0 || report.Reference.MacroblocksPerSec <= 0 {
		t.Fatalf("reference sizes/throughput = key:%d inter:%f mbps:%f, want positive values", report.Reference.KeyframeBytes, report.Reference.AvgInterBytes, report.Reference.MacroblocksPerSec)
	}
	if report.Reference.PSNR <= 0 || report.Reference.SSIM <= 0 || report.Reference.SSIM > 1 || report.Reference.QualityFrames != 3 || report.Reference.QualityError != "" {
		t.Fatalf("reference quality = psnr:%f ssim:%f frames:%d err:%q, want 3 decoded quality frames", report.Reference.PSNR, report.Reference.SSIM, report.Reference.QualityFrames, report.Reference.QualityError)
	}
	if report.QualitySkipped || report.Reference.QualitySkipped {
		t.Fatalf("quality skipped = govpx:%v reference:%v, want false by default", report.QualitySkipped, report.Reference.QualitySkipped)
	}
	if report.Comparison == nil {
		t.Fatalf("comparison_vs_reference = nil, want populated when reference is present")
	}
	if report.Comparison.BitrateRatioVsReference <= 0 ||
		report.Comparison.NSPerFrameRatio <= 0 ||
		report.Comparison.EncodeFPSRatio <= 0 ||
		report.Comparison.OutputBytesRatio <= 0 {
		t.Fatalf("comparison ratios = %+v, want all > 0", *report.Comparison)
	}
	wantBitrateDelta := report.OutputBitrateKbps - report.Reference.OutputBitrateKbps
	if report.Comparison.BitrateDeltaKbps != wantBitrateDelta {
		t.Fatalf("comparison bitrate delta = %f, want %f", report.Comparison.BitrateDeltaKbps, wantBitrateDelta)
	}
	wantPSNRDelta := report.PSNR - report.Reference.PSNR
	if report.Comparison.PSNRDeltaDB != wantPSNRDelta {
		t.Fatalf("comparison psnr delta = %f, want %f", report.Comparison.PSNRDeltaDB, wantPSNRDelta)
	}

	// The fake vpxenc emits a vpxenc-style progress line, so the bench
	// should pick up the parsed encode-only timing rather than falling
	// back to the wall clock. Wall and overhead must still be reported
	// for transparency, and the parity flags should travel with the
	// reference report so consumers can verify what was passed to
	// libvpx.
	if report.Reference.TimingSource != "vpxenc-stats" {
		t.Fatalf("timing source = %q, want %q (parser fell back)", report.Reference.TimingSource, "vpxenc-stats")
	}
	if report.Reference.WallNSPerFrame <= 0 || report.Reference.WallEncodeFPS <= 0 {
		t.Fatalf("wall timing = ns:%d fps:%f, want positive values", report.Reference.WallNSPerFrame, report.Reference.WallEncodeFPS)
	}
	if report.Reference.WallNSPerFrame < report.Reference.NSPerFrame {
		t.Fatalf("wall %d < encode %d, want wall >= encode (subprocess overhead is non-negative)", report.Reference.WallNSPerFrame, report.Reference.NSPerFrame)
	}
	if report.Reference.SubprocessOverheadNS < 0 {
		t.Fatalf("subprocess overhead = %d, want >= 0", report.Reference.SubprocessOverheadNS)
	}
	hasFlag := func(want string) bool {
		return slices.Contains(report.Reference.ParityFlags, want)
	}
	for _, want := range []string{"--end-usage=cbr", "--passes=1", "--lag-in-frames=0"} {
		if !hasFlag(want) {
			t.Fatalf("parity flags missing %q\nhave: %v", want, report.Reference.ParityFlags)
		}
	}
}

func TestRunBenchmarkSkipQuality(t *testing.T) {
	report, err := runBenchmark(benchConfig{
		Width:       16,
		Height:      16,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
		SkipQuality: true,
	})
	if err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	if !report.QualitySkipped {
		t.Fatalf("QualitySkipped = false, want true")
	}
	if report.PSNR != 0 || report.SSIM != 0 || report.QualityFrames != 0 {
		t.Fatalf("quality = psnr:%f ssim:%f frames:%d, want all zero when skipped", report.PSNR, report.SSIM, report.QualityFrames)
	}
	if report.NSPerFrame <= 0 || report.EncodeFPS <= 0 || report.OutputBytes <= 0 || report.EncodedFrames == 0 {
		t.Fatalf("encode metrics = ns:%d fps:%f bytes:%d frames:%d, want populated", report.NSPerFrame, report.EncodeFPS, report.OutputBytes, report.EncodedFrames)
	}
	text := formatEncodeReport(report)
	if !strings.Contains(text, "quality") || !strings.Contains(text, "(skipped)") {
		t.Fatalf("formatted report did not mark skipped quality:\n%s", text)
	}
}

func TestRunBenchmarkPhaseTiming(t *testing.T) {
	if !phaseTimingEnabled {
		t.Skip("phase timing requires the govpx_phase_stats build tag")
	}
	report, err := runBenchmark(benchConfig{
		Width:       64,
		Height:      64,
		Frames:      6,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
		SkipQuality: true,
		PhaseTiming: true,
	})
	if err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	if report.PhaseNS == nil {
		t.Fatalf("PhaseNS = nil, want populated when PhaseTiming is true")
	}
	if report.PhaseNS.KeyAttempts == 0 || report.PhaseNS.InterAttempts == 0 {
		t.Fatalf("phase attempts = %+v, want key and inter attempts counted", *report.PhaseNS)
	}
	if report.PhaseNS.PacketWriteNS <= 0 || report.PhaseNS.LoopFilterPickNS <= 0 {
		t.Fatalf("phase timings = %+v, want packet and loop-filter pick timings", *report.PhaseNS)
	}
	if report.PhaseNS.LoopFilterTrials == 0 || report.PhaseNS.LoopFilterTrialFilterNS == 0 {
		t.Fatalf("loop-filter trial timings = %+v, want counted trial work", *report.PhaseNS)
	}
	if report.PhaseNS.FullPelSADCalls == 0 || report.PhaseNS.SubpelVarianceCalls == 0 {
		t.Fatalf("motion-search topology stats = %+v, want SAD and subpel variance work counted", *report.PhaseNS)
	}
	if report.PhaseNS.InterCoefTokenRecords == 0 {
		t.Fatalf("coefficient token records = 0, want accepted-MB token stream counted")
	}
	text := formatEncodeReport(report)
	if !strings.Contains(text, "phase/frame") || !strings.Contains(text, "phase attempts") ||
		!strings.Contains(text, "lf trials") || !strings.Contains(text, "coeff pipeline") ||
		!strings.Contains(text, "motion search") {
		t.Fatalf("formatted report missing phase timing:\n%s", text)
	}
}

func TestRunBenchmarkSkipQualityIncludesLibvpxReference(t *testing.T) {
	report, err := runBenchmark(benchConfig{
		Width:        16,
		Height:       16,
		Frames:       3,
		FPS:          30,
		BitrateKbps:  1200,
		Mode:         "realtime",
		SkipQuality:  true,
		LibvpxVpxenc: fakeVpxencPath(t),
	})
	if err != nil {
		t.Fatalf("runBenchmark returned error: %v", err)
	}
	if report.Reference == nil || report.Comparison == nil {
		t.Fatalf("reference/comparison = %v/%v, want populated", report.Reference, report.Comparison)
	}
	if !report.QualitySkipped || !report.Reference.QualitySkipped {
		t.Fatalf("quality skipped = govpx:%v reference:%v, want both true", report.QualitySkipped, report.Reference.QualitySkipped)
	}
	if report.PSNR != 0 || report.SSIM != 0 || report.QualityFrames != 0 {
		t.Fatalf("govpx quality = psnr:%f ssim:%f frames:%d, want zero", report.PSNR, report.SSIM, report.QualityFrames)
	}
	if report.Reference.PSNR != 0 || report.Reference.SSIM != 0 || report.Reference.QualityFrames != 0 || report.Reference.QualityError != "" {
		t.Fatalf("reference quality = psnr:%f ssim:%f frames:%d err:%q, want skipped zeros", report.Reference.PSNR, report.Reference.SSIM, report.Reference.QualityFrames, report.Reference.QualityError)
	}
	if report.Comparison.PSNRDeltaDB != 0 || report.Comparison.SSIMDelta != 0 {
		t.Fatalf("quality deltas = psnr:%f ssim:%f, want zero when skipped", report.Comparison.PSNRDeltaDB, report.Comparison.SSIMDelta)
	}
	if report.Comparison.NSPerFrameRatio <= 0 || report.Reference.NSPerFrame <= 0 {
		t.Fatalf("timing comparison = %+v reference ns=%d, want populated", *report.Comparison, report.Reference.NSPerFrame)
	}
	text := formatEncodeReport(report)
	if !strings.Contains(text, "(skipped)") {
		t.Fatalf("formatted reference report did not mark skipped quality:\n%s", text)
	}
}

func TestMeasuredEncodeQualityMetricsUsesMeasuredPackets(t *testing.T) {
	cfg := benchConfig{
		Width:       32,
		Height:      32,
		Frames:      2,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
	}
	deadline, _, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		t.Fatalf("benchmarkDeadline returned error: %v", err)
	}
	frames := []govpx.Image{
		makeBenchmarkFrame(cfg.Width, cfg.Height, 0),
		makeBenchmarkFrame(cfg.Width, cfg.Height, 1),
	}
	enc, err := newBenchmarkEncoder(cfg, deadline)
	if err != nil {
		t.Fatalf("newBenchmarkEncoder returned error: %v", err)
	}
	packet := make([]byte, cfg.Width*cfg.Height*6)
	result, err := enc.EncodeInto(packet, frames[0], 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	measured := measuredEncodePacket{data: append([]byte(nil), result.Data...), sourceIndex: 0}
	psnrMatched, _, matchedFrames, err := measuredEncodeQualityMetrics([]measuredEncodePacket{measured}, frames)
	if err != nil {
		t.Fatalf("measuredEncodeQualityMetrics matched returned error: %v", err)
	}
	measured.sourceIndex = 1
	psnrWrongSource, _, wrongFrames, err := measuredEncodeQualityMetrics([]measuredEncodePacket{measured}, frames)
	if err != nil {
		t.Fatalf("measuredEncodeQualityMetrics wrong-source returned error: %v", err)
	}
	if matchedFrames != 1 || wrongFrames != 1 {
		t.Fatalf("quality frames matched/wrong = %d/%d, want 1/1", matchedFrames, wrongFrames)
	}
	if psnrMatched <= psnrWrongSource {
		t.Fatalf("measured packet quality used wrong source: matched PSNR=%f wrong-source PSNR=%f", psnrMatched, psnrWrongSource)
	}
}

func TestRegisterBenchFlagsEncodeOnly(t *testing.T) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	cfg := benchConfig{}
	opts := defaultBenchCLIOptions()
	registerBenchFlags(fs, &cfg, &opts)
	if err := fs.Parse([]string{"-encode-only", "-format=json", "-width=32", "-height=24", "-frames=7", "-cpu-used=-4", "-phase-timing", "-suite=quick", "-suite-runs=2", "-auto-libvpx=false"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !cfg.SkipQuality || !cfg.PhaseTiming {
		t.Fatalf("SkipQuality/PhaseTiming = %v/%v, want true/true", cfg.SkipQuality, cfg.PhaseTiming)
	}
	if cfg.Width != 32 || cfg.Height != 24 || cfg.Frames != 7 || cfg.CpuUsed != -4 {
		t.Fatalf("parsed config = %dx%d frames=%d cpu=%d, want 32x24 frames=7 cpu=-4", cfg.Width, cfg.Height, cfg.Frames, cfg.CpuUsed)
	}
	if opts.format != "json" || opts.suite != "quick" || opts.suiteRuns != 2 || opts.autoCompare {
		t.Fatalf("opts = %+v, want format=json suite=quick suiteRuns=2 autoCompare=false", opts)
	}
}

func TestEncodeSuiteCases(t *testing.T) {
	cases, err := encodeSuiteCases("quick")
	if err != nil {
		t.Fatalf("encodeSuiteCases quick returned error: %v", err)
	}
	if len(cases) != 2 || cases[0].name != "rt-720p-2m-30f" || cases[1].mode != "good" {
		t.Fatalf("quick suite cases = %+v, want realtime 720p and good 1080p", cases)
	}
	if _, err := encodeSuiteCases("unknown"); err == nil {
		t.Fatalf("encodeSuiteCases accepted unknown suite")
	}
	for _, suite := range []string{"vp8", "webrtc", "vod", "stress"} {
		cases, err := encodeSuiteCases(suite)
		if err != nil {
			t.Fatalf("encodeSuiteCases(%q) returned error: %v", suite, err)
		}
		if len(cases) == 0 {
			t.Fatalf("encodeSuiteCases(%q) returned no cases", suite)
		}
		for _, c := range cases {
			if c.name == "" || c.width <= 0 || c.height <= 0 || c.frames <= 0 || c.fps <= 0 || c.bitrateKbps <= 0 {
				t.Fatalf("encodeSuiteCases(%q) bad case %+v", suite, c)
			}
			if c.mode != "realtime" && c.mode != "good" {
				t.Fatalf("encodeSuiteCases(%q) mode=%q, want realtime or good", suite, c.mode)
			}
		}
	}
}

func TestRunEncodeSuiteRequiresLibvpxReference(t *testing.T) {
	if _, err := runEncodeSuite(benchConfig{}, "quick", 1); err == nil {
		t.Fatalf("runEncodeSuite without vpxenc returned nil error")
	}
}

func TestFormatSuiteReportTable(t *testing.T) {
	report := benchReport{
		Mode:              "realtime",
		Width:             1280,
		Height:            720,
		Frames:            30,
		FPS:               30,
		TargetBitrateKbps: 2000,
		OutputBitrateKbps: 2460,
		NSPerFrame:        8_000_000,
		EncodeFPS:         125,
		PSNR:              28.5,
		SSIM:              0.93,
		DroppedFrames:     4,
		EncodedFrames:     26,
		Reference: &referenceReport{
			NSPerFrame:        4_000_000,
			EncodeFPS:         250,
			OutputBitrateKbps: 2448,
			PSNR:              28.6,
			SSIM:              0.934,
			EncodedFrames:     26,
		},
		Comparison: &comparisonReport{
			NSPerFrameRatio:         2,
			EncodeFPSRatio:          0.5,
			BitrateRatioVsReference: 1.0049,
			PSNRDeltaDB:             -0.1,
			SSIMDelta:               -0.004,
		},
	}
	text := formatSuiteReport(suiteReport{
		Name:          "quick",
		Runs:          1,
		Selector:      "median govpx ns/frame",
		GeomeanNSGap:  2,
		GeomeanFPSGap: 0.5,
		Cases:         []suiteCaseReport{{Name: "rt-720p-2m-30f", Report: report}},
	})
	if !strings.Contains(text, "govpx-bench  suite  quick") ||
		!strings.Contains(text, "rt-720p-2m-30f") ||
		!strings.Contains(text, "2.00x") ||
		!strings.Contains(text, "4/4") {
		t.Fatalf("suite report missing expected table data:\n%s", text)
	}
}

func TestBenchCLIOptionsDefaultAutoLibvpx(t *testing.T) {
	t.Setenv("GOVPX_VPXENC", "/tmp/should-not-be-used")
	t.Setenv("GOVPX_ORACLE", "/tmp/should-not-be-used")
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	cfg := benchConfig{}
	opts := defaultBenchCLIOptions()
	registerBenchFlags(fs, &cfg, &opts)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.autoCompare || opts.buildLibvpx || cfg.LibvpxVpxenc != "" || cfg.LibvpxOracle != "" {
		t.Fatalf("defaults = opts:%+v cfg:%+v, want auto libvpx enabled without pre-resolved paths", opts, cfg)
	}
}

func TestResolveLibvpxDefaultsDoesNotSelectOracleForEncode(t *testing.T) {
	cfg := benchConfig{}
	resolveLibvpxDefaults(&cfg, false)
	if cfg.LibvpxOracle != "" {
		t.Fatalf("LibvpxOracle = %q, want empty for encode mode", cfg.LibvpxOracle)
	}
}

func TestResolveLibvpxDefaultsDoesNotSelectVpxencForDecode(t *testing.T) {
	cfg := benchConfig{Decode: true}
	resolveLibvpxDefaults(&cfg, false)
	if cfg.LibvpxVpxenc != "" {
		t.Fatalf("LibvpxVpxenc = %q, want empty for decode mode", cfg.LibvpxVpxenc)
	}
}

func TestBuildComparisonReportComputesGovpxOverLibvpxRatios(t *testing.T) {
	report := benchReport{
		OutputBitrateKbps: 1200,
		BitrateErrorPct:   0,
		PSNR:              40,
		SSIM:              0.99,
		EncodeFPS:         60,
		NSPerFrame:        16_666_667,
		OutputBytes:       12000,
		AvgInterBytes:     400,
		KeyframeBytes:     2000,
	}
	reference := referenceReport{
		OutputBitrateKbps: 1500,
		BitrateErrorPct:   25,
		PSNR:              41,
		SSIM:              0.995,
		EncodeFPS:         30,
		NSPerFrame:        33_333_334,
		OutputBytes:       15000,
		AvgInterBytes:     500,
		KeyframeBytes:     2500,
	}

	cmp := buildComparisonReport(report, reference)
	if cmp == nil {
		t.Fatalf("buildComparisonReport = nil")
	}
	wantBitrateRatio := report.OutputBitrateKbps / reference.OutputBitrateKbps
	if cmp.BitrateRatioVsReference != wantBitrateRatio {
		t.Fatalf("BitrateRatio = %f, want %f", cmp.BitrateRatioVsReference, wantBitrateRatio)
	}
	if cmp.BitrateDeltaKbps != report.OutputBitrateKbps-reference.OutputBitrateKbps {
		t.Fatalf("BitrateDelta = %f, want %f", cmp.BitrateDeltaKbps, report.OutputBitrateKbps-reference.OutputBitrateKbps)
	}
	if cmp.BitrateErrorPctDelta != report.BitrateErrorPct-reference.BitrateErrorPct {
		t.Fatalf("BitrateErrorPctDelta = %f, want %f", cmp.BitrateErrorPctDelta, report.BitrateErrorPct-reference.BitrateErrorPct)
	}
	if cmp.PSNRDeltaDB != report.PSNR-reference.PSNR {
		t.Fatalf("PSNRDelta = %f, want %f", cmp.PSNRDeltaDB, report.PSNR-reference.PSNR)
	}
	if cmp.SSIMDelta != report.SSIM-reference.SSIM {
		t.Fatalf("SSIMDelta = %f, want %f", cmp.SSIMDelta, report.SSIM-reference.SSIM)
	}
	if cmp.EncodeFPSRatio != report.EncodeFPS/reference.EncodeFPS {
		t.Fatalf("EncodeFPSRatio = %f, want %f", cmp.EncodeFPSRatio, report.EncodeFPS/reference.EncodeFPS)
	}
	if cmp.NSPerFrameRatio != float64(report.NSPerFrame)/float64(reference.NSPerFrame) {
		t.Fatalf("NSPerFrameRatio = %f, want %f", cmp.NSPerFrameRatio, float64(report.NSPerFrame)/float64(reference.NSPerFrame))
	}
	if cmp.OutputBytesRatio != float64(report.OutputBytes)/float64(reference.OutputBytes) {
		t.Fatalf("OutputBytesRatio = %f, want %f", cmp.OutputBytesRatio, float64(report.OutputBytes)/float64(reference.OutputBytes))
	}
	if cmp.AvgInterBytesRatio != report.AvgInterBytes/reference.AvgInterBytes {
		t.Fatalf("AvgInterBytesRatio = %f, want %f", cmp.AvgInterBytesRatio, report.AvgInterBytes/reference.AvgInterBytes)
	}
	if cmp.KeyframeBytesRatio != float64(report.KeyframeBytes)/float64(reference.KeyframeBytes) {
		t.Fatalf("KeyframeBytesRatio = %f, want %f", cmp.KeyframeBytesRatio, float64(report.KeyframeBytes)/float64(reference.KeyframeBytes))
	}
}

func TestBuildComparisonReportHandlesZeroDenominators(t *testing.T) {
	report := benchReport{
		OutputBitrateKbps: 1000,
		PSNR:              40,
		SSIM:              0.99,
		EncodeFPS:         30,
		NSPerFrame:        33_333_334,
	}
	reference := referenceReport{}
	cmp := buildComparisonReport(report, reference)
	if cmp == nil {
		t.Fatalf("buildComparisonReport = nil")
	}
	// Ratios stay at zero rather than +Inf when the libvpx side reports zero.
	if cmp.BitrateRatioVsReference != 0 ||
		cmp.NSPerFrameRatio != 0 ||
		cmp.EncodeFPSRatio != 0 ||
		cmp.OutputBytesRatio != 0 ||
		cmp.AvgInterBytesRatio != 0 ||
		cmp.KeyframeBytesRatio != 0 {
		t.Fatalf("ratios with zero denominators = %+v, want all zero", *cmp)
	}
	// Deltas are still computed from raw values.
	if cmp.BitrateDeltaKbps != report.OutputBitrateKbps {
		t.Fatalf("BitrateDelta = %f, want %f", cmp.BitrateDeltaKbps, report.OutputBitrateKbps)
	}
	if cmp.PSNRDeltaDB != report.PSNR {
		t.Fatalf("PSNRDelta = %f, want %f", cmp.PSNRDeltaDB, report.PSNR)
	}
}

func TestRunDecodeBenchmarkOutputsJSONMetrics(t *testing.T) {
	report, err := runDecodeBenchmark(benchConfig{
		Width:       16,
		Height:      16,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
	})
	if err != nil {
		t.Fatalf("runDecodeBenchmark returned error: %v", err)
	}
	if report.Decoder != "govpx" || report.Operation != "decode" || report.Mode != "realtime" {
		t.Fatalf("identity = %s/%s/%s, want govpx/decode/realtime", report.Decoder, report.Operation, report.Mode)
	}
	if report.Width != 16 || report.Height != 16 || report.Frames != 3 || report.DecodedFrames != 3 || report.InputBytes <= 0 {
		t.Fatalf("dimensions/counts = %+v", report)
	}
	if report.NSPerFrame <= 0 || report.DecodeFPS <= 0 || report.MacroblocksPerSec <= 0 || report.CodedMegabytesPerSec <= 0 || report.LatencyNS.P50 <= 0 {
		t.Fatalf("decode timing metrics = ns:%d fps:%f mbps:%f coded:%f p50:%d", report.NSPerFrame, report.DecodeFPS, report.MacroblocksPerSec, report.CodedMegabytesPerSec, report.LatencyNS.P50)
	}
	maxAllocs := 0.0
	if puregoBuild {
		maxAllocs = 1
	}
	if report.AllocsPerFrame > maxAllocs {
		t.Fatalf("AllocsPerFrame = %f, want <= %f for measured decode pass", report.AllocsPerFrame, maxAllocs)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
}

func TestRunDecodeBenchmarkIncludesLibvpxReference(t *testing.T) {
	report, err := runDecodeBenchmark(benchConfig{
		Width:        16,
		Height:       16,
		Frames:       3,
		FPS:          30,
		BitrateKbps:  1200,
		Mode:         "realtime",
		LibvpxOracle: fakeLibvpxOraclePath(t),
	})
	if err != nil {
		t.Fatalf("runDecodeBenchmark returned error: %v", err)
	}
	if report.Reference == nil {
		t.Fatalf("reference = nil, want fake libvpx decode report")
	}
	if report.Reference.Decoder != "libvpx-vp8" || report.Reference.DecodedFrames != 3 {
		t.Fatalf("reference = %+v, want libvpx-vp8 with 3 decoded frames", *report.Reference)
	}
	if report.Reference.NSPerFrame <= 0 || report.Reference.DecodeFPS <= 0 || report.Reference.MacroblocksPerSec <= 0 || report.RelativeSpeedVsReference <= 0 {
		t.Fatalf("reference timing = %+v relative=%f, want positive values", *report.Reference, report.RelativeSpeedVsReference)
	}
}

func TestParseVpxencEncodeTimeUnits(t *testing.T) {
	tests := []struct {
		name      string
		stderr    string
		ok        bool
		frames    int
		totalNS   int64
		bytesWant int
	}{
		{
			name:      "microseconds",
			stderr:    "\rPass 1/1 frame    1/0      0B       0 us 0.00 fps   \rPass 1/1 frame    3/3   1234B   45000 us  66.67 fps   \n",
			ok:        true,
			frames:    3,
			totalNS:   45_000 * int64(time.Microsecond),
			bytesWant: 1234,
		},
		{
			name:      "milliseconds-when-long",
			stderr:    "Pass 1/1 frame   30/30 567890B   12345 ms   2.43 fps   \n",
			ok:        true,
			frames:    30,
			totalNS:   12_345 * int64(time.Millisecond),
			bytesWant: 567890,
		},
		{
			name:   "no-progress-output",
			stderr: "some unrelated logging\nthat does not match\n",
			ok:     false,
		},
		{
			name:   "frames-zero",
			stderr: "Pass 1/1 frame    0/0      0B       0 us 0.00 fps   \n",
			ok:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseVpxencEncodeTime([]byte(tt.stderr))
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v (got=%+v)", ok, tt.ok, got)
			}
			if !ok {
				return
			}
			if got.frames != tt.frames {
				t.Fatalf("frames = %d, want %d", got.frames, tt.frames)
			}
			if got.totalNS != tt.totalNS {
				t.Fatalf("totalNS = %d, want %d", got.totalNS, tt.totalNS)
			}
			if got.bytes != tt.bytesWant {
				t.Fatalf("bytes = %d, want %d", got.bytes, tt.bytesWant)
			}
		})
	}
}

func TestLibvpxParityFlagsCarryEncoderConfig(t *testing.T) {
	cfg := benchConfig{Width: 64, Height: 64, Frames: 30, FPS: 30, BitrateKbps: 1200, Mode: "realtime", CpuUsed: -4}
	parity := parityFor(cfg)
	flags := libvpxParityFlags(cfg, parity, "--rt")

	required := []string{
		"--passes=1",
		"--lag-in-frames=0",
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", cfg.BitrateKbps),
		fmt.Sprintf("--min-q=%d", parity.MinQuantizer),
		fmt.Sprintf("--max-q=%d", parity.MaxQuantizer),
		fmt.Sprintf("--kf-min-dist=%d", parity.KeyFrameInterval),
		fmt.Sprintf("--kf-max-dist=%d", parity.KeyFrameInterval),
		fmt.Sprintf("--buf-sz=%d", parity.BufferSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", parity.BufferInitialSizeMs),
		fmt.Sprintf("--buf-optimal-sz=%d", parity.BufferOptimalSizeMs),
		fmt.Sprintf("--undershoot-pct=%d", parity.UndershootPct),
		fmt.Sprintf("--overshoot-pct=%d", parity.OvershootPct),
		fmt.Sprintf("--drop-frame=%d", parity.DropFrameWaterMark),
		fmt.Sprintf("--max-intra-rate=%d", parity.MaxIntraBitratePct),
		fmt.Sprintf("--noise-sensitivity=%d", parity.NoiseSensitivity),
		fmt.Sprintf("--static-thresh=%d", parity.StaticThreshold),
		fmt.Sprintf("--threads=%d", parity.Threads),
		fmt.Sprintf("--timebase=1/%d", cfg.FPS),
		"--rt",
		fmt.Sprintf("--cpu-used=%d", parity.CpuUsed),
	}
	have := make(map[string]bool, len(flags))
	for _, f := range flags {
		have[f] = true
	}
	for _, want := range required {
		if !have[want] {
			t.Fatalf("parity flags missing %q\nhave: %v", want, flags)
		}
	}
}

func TestParseIVFFrameInfoClassifiesAllKeyframes(t *testing.T) {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	payloads := [][]byte{
		{0x10, 0x00, 0x9d, 0x01}, // key frame: low bit clear
		{0x11, 0x00, 0x00, 0x00}, // inter frame: low bit set
		{0x20, 0x00, 0x9d, 0x01}, // later forced key frame
	}
	size := fileHeaderSize
	for _, payload := range payloads {
		size += frameHeaderSize + len(payload)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP80"))
	offset := fileHeaderSize
	for i, payload := range payloads {
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(len(payload)))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(i))
		offset += frameHeaderSize
		copy(ivf[offset:], payload)
		offset += len(payload)
	}

	frames, err := parseIVFFrameInfo(ivf)
	if err != nil {
		t.Fatalf("parseIVFFrameInfo returned error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("frames len = %d, want 3", len(frames))
	}
	if !frames[0].keyFrame || frames[1].keyFrame || !frames[2].keyFrame {
		t.Fatalf("key classification = [%v %v %v], want [true false true]", frames[0].keyFrame, frames[1].keyFrame, frames[2].keyFrame)
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		t.Fatalf("parseIVFFrameSizes returned error: %v", err)
	}
	if !slices.Equal(sizes, []int{4, 4, 4}) {
		t.Fatalf("sizes = %v, want [4 4 4]", sizes)
	}
}

func TestParityForMatchesEncoderDefaults(t *testing.T) {
	// Sanity check that realtime parity defaults mirror the public WebRTC
	// example rather than the simpler validation-only CBR preset. The
	// CLI default for -threads is 1, so the equivalent benchConfig
	// passed in here mirrors that explicitly.
	got := parityFor(benchConfig{FPS: 24, Threads: 1, CpuUsed: 8})
	if got.KeyFrameInterval != 3000 {
		t.Fatalf("KeyFrameInterval = %d, want 3000", got.KeyFrameInterval)
	}
	if got.MinQuantizer != 2 || got.MaxQuantizer != 56 {
		t.Fatalf("quantizer range = [%d,%d], want [2,56]", got.MinQuantizer, got.MaxQuantizer)
	}
	if got.BufferSizeMs != 1000 || got.BufferInitialSizeMs != 500 || got.BufferOptimalSizeMs != 600 {
		t.Fatalf("buffer model = sz:%d init:%d opt:%d, want 1000/500/600", got.BufferSizeMs, got.BufferInitialSizeMs, got.BufferOptimalSizeMs)
	}
	if !got.DropFrameAllowed || got.DropFrameWaterMark != 30 {
		t.Fatalf("drop frame = enabled:%t watermark:%d, want enabled/30", got.DropFrameAllowed, got.DropFrameWaterMark)
	}
	if got.MaxIntraBitratePct != 720 || got.NoiseSensitivity != 4 || got.StaticThreshold != 1 {
		t.Fatalf("webrtc knobs = max-intra:%d noise:%d static:%d, want 720/4/1",
			got.MaxIntraBitratePct, got.NoiseSensitivity, got.StaticThreshold)
	}
	if got.CpuUsed != 8 || got.Threads != 1 {
		t.Fatalf("cpu/threads = %d/%d, want 8/1", got.CpuUsed, got.Threads)
	}
	good := parityFor(benchConfig{Mode: "good", FPS: 24, Threads: 1, CpuUsed: 8})
	if good.KeyFrameInterval != 24 ||
		good.MinQuantizer != 4 ||
		good.BufferSizeMs != 600 ||
		good.DropFrameAllowed ||
		good.MaxIntraBitratePct != 0 ||
		good.NoiseSensitivity != 0 ||
		good.StaticThreshold != 0 {
		t.Fatalf("good-mode parity = %+v, want validation CBR defaults", good)
	}

	// -threads=0 propagates as 0 to libvpx (its native "auto" sentinel)
	// and to govpx (where normalizeEncoderOptions folds it onto the
	// historical single-thread default). The flag is plumbed verbatim.
	if got := parityFor(benchConfig{FPS: 24, Threads: 0, CpuUsed: 8}); got.Threads != 0 {
		t.Fatalf("Threads=0 propagates as %d, want 0", got.Threads)
	}
	if got := parityFor(benchConfig{FPS: 24, Threads: 4, CpuUsed: 8}); got.Threads != 4 {
		t.Fatalf("Threads=4 propagates as %d, want 4", got.Threads)
	}

	// Zero FPS falls back to a sane default rather than passing 0 to libvpx.
	if parityFor(benchConfig{FPS: 0}).KeyFrameInterval == 0 {
		t.Fatalf("KeyFrameInterval falls back when FPS is 0")
	}
}

func TestBenchmarkEncoderOptionsMatchLibvpxParityConfig(t *testing.T) {
	cfg := benchConfig{
		Width:       80,
		Height:      64,
		Frames:      4,
		FPS:         24,
		BitrateKbps: 900,
		Threads:     3,
		CpuUsed:     -8,
	}
	parity := parityFor(cfg)
	opts := benchmarkEncoderOptions(cfg, govpx.DeadlineRealtime)
	if opts.MinQuantizer != parity.MinQuantizer || opts.MaxQuantizer != parity.MaxQuantizer {
		t.Fatalf("quantizer range = [%d,%d], want parity [%d,%d]",
			opts.MinQuantizer, opts.MaxQuantizer, parity.MinQuantizer, parity.MaxQuantizer)
	}
	if opts.KeyFrameInterval != parity.KeyFrameInterval {
		t.Fatalf("KeyFrameInterval = %d, want %d", opts.KeyFrameInterval, parity.KeyFrameInterval)
	}
	if opts.BufferSizeMs != parity.BufferSizeMs ||
		opts.BufferInitialSizeMs != parity.BufferInitialSizeMs ||
		opts.BufferOptimalSizeMs != parity.BufferOptimalSizeMs {
		t.Fatalf("buffer model = sz:%d init:%d opt:%d, want %d/%d/%d",
			opts.BufferSizeMs, opts.BufferInitialSizeMs, opts.BufferOptimalSizeMs,
			parity.BufferSizeMs, parity.BufferInitialSizeMs, parity.BufferOptimalSizeMs)
	}
	if opts.UndershootPct != parity.UndershootPct || opts.OvershootPct != parity.OvershootPct {
		t.Fatalf("rate-control percentages = under:%d over:%d, want parity %d/%d",
			opts.UndershootPct, opts.OvershootPct, parity.UndershootPct, parity.OvershootPct)
	}
	if opts.MaxIntraBitratePct != parity.MaxIntraBitratePct ||
		opts.DropFrameAllowed != parity.DropFrameAllowed ||
		opts.DropFrameWaterMark != parity.DropFrameWaterMark ||
		opts.NoiseSensitivity != parity.NoiseSensitivity ||
		opts.StaticThreshold != parity.StaticThreshold {
		t.Fatalf("realtime knobs = max-intra:%d drop:%t/%d noise:%d static:%d, want parity %+v",
			opts.MaxIntraBitratePct, opts.DropFrameAllowed, opts.DropFrameWaterMark,
			opts.NoiseSensitivity, opts.StaticThreshold, parity)
	}
	if opts.Threads != parity.Threads || opts.CpuUsed != parity.CpuUsed {
		t.Fatalf("cpu/threads = %d/%d, want parity %d/%d",
			opts.CpuUsed, opts.Threads, parity.CpuUsed, parity.Threads)
	}
}

func TestRunBenchmarkRejectsBadConfig(t *testing.T) {
	if _, err := runBenchmark(benchConfig{Width: 16, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 1200, Mode: "slow"}); err == nil {
		t.Fatalf("runBenchmark accepted unsupported mode")
	}
	if _, err := runBenchmark(benchConfig{Width: 0, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 1200}); err == nil {
		t.Fatalf("runBenchmark accepted invalid dimensions")
	}
}

func TestImageSSIM(t *testing.T) {
	src := makeBenchmarkFrame(16, 16, 0)
	same := makeBenchmarkFrame(16, 16, 0)
	if got := imageSSIM(src, same); got != 1 {
		t.Fatalf("identical SSIM = %f, want 1", got)
	}
	changed := makeBenchmarkFrame(16, 16, 1)
	if got := imageSSIM(src, changed); got <= 0 || got >= 1 {
		t.Fatalf("changed SSIM = %f, want between 0 and 1", got)
	}
}

func TestQuantizerHistogramMap(t *testing.T) {
	var hist [quantizerHistogramBins]int
	hist[4] = 3
	hist[56] = 2

	got := quantizerHistogramMap(&hist)
	if len(got) != 2 || got["4"] != 3 || got["56"] != 2 {
		t.Fatalf("histogram = %v, want q4=3 q56=2", got)
	}
}

func TestBenchmarkMacroblocksRoundsToCodedGrid(t *testing.T) {
	tests := []struct {
		width  int
		height int
		want   float64
	}{
		{width: 16, height: 16, want: 1},
		{width: 17, height: 16, want: 2},
		{width: 17, height: 17, want: 4},
	}
	for _, tt := range tests {
		if got := benchmarkMacroblocks(tt.width, tt.height); got != tt.want {
			t.Fatalf("benchmarkMacroblocks(%d, %d) = %f, want %f", tt.width, tt.height, got, tt.want)
		}
	}
}

func TestReferenceQualityMetricsFallsBackToFrameOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.ivf")
	if err := writeFakeIVF(path, 16, 16, 30, 1200, 3); err != nil {
		t.Fatalf("writeFakeIVF returned error: %v", err)
	}
	ivf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	offset := 32
	for i := 0; offset < len(ivf); i++ {
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(9000+i))
		offset += 12 + size
	}
	frames := []govpx.Image{
		makeBenchmarkFrame(16, 16, 0),
		makeBenchmarkFrame(16, 16, 1),
		makeBenchmarkFrame(16, 16, 2),
	}

	psnr, ssim, qualityFrames, err := referenceQualityMetrics(ivf, frames)
	if err != nil {
		t.Fatalf("referenceQualityMetrics returned error: %v", err)
	}
	if qualityFrames != 3 || psnr <= 0 || ssim <= 0 || ssim > 1 {
		t.Fatalf("quality = psnr:%f ssim:%f frames:%d, want 3 frame-order matches", psnr, ssim, qualityFrames)
	}
}

func TestParseFFmpegVMAFStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmaf.json")
	data := []byte(`{
  "frames": [
    {"frameNum": 0, "metrics": {"integer_vmaf": 91.25, "integer_motion2": 0.1}},
    {"frameNum": 1, "metrics": {"vmaf": 92.50}}
  ],
  "pooled_metrics": {"integer_vmaf": {"mean": 91.875}}
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	values, err := parseFFmpegVMAFStats(path)
	if err != nil {
		t.Fatalf("parseFFmpegVMAFStats returned error: %v", err)
	}
	if len(values) != 2 || values[0] != 91.25 || values[1] != 92.50 {
		t.Fatalf("values = %v, want [91.25 92.5]", values)
	}
}

func TestPlotArtifactsIncludeVMAF(t *testing.T) {
	report := plotComparisonReport{
		Width:       16,
		Height:      16,
		Frames:      2,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
		Govpx: plotEncoderSummary{
			EncodeFPS:         60,
			OutputBitrateKbps: 1100,
			AverageVMAF:       91,
			AveragePSNR:       40,
			AverageSSIM:       0.98,
		},
		Libvpx: plotEncoderSummary{
			EncodeFPS:         120,
			OutputBitrateKbps: 1200,
			AverageVMAF:       93,
			AveragePSNR:       41,
			AverageSSIM:       0.99,
		},
		FramesData: []plotFrameComparison{
			{Frame: 0, GovpxVMAF: 90, LibvpxVMAF: 92, GovpxPSNR: 39, LibvpxPSNR: 40, GovpxSSIM: 0.97, LibvpxSSIM: 0.98, GovpxBytes: 100, LibvpxBytes: 110},
			{Frame: 1, GovpxVMAF: 92, LibvpxVMAF: 94, GovpxPSNR: 41, LibvpxPSNR: 42, GovpxSSIM: 0.99, LibvpxSSIM: 0.995, GovpxBytes: 90, LibvpxBytes: 100},
		},
	}
	csv := renderPlotCSV(report)
	if !strings.Contains(csv, "govpx_vmaf,libvpx_vmaf") || !strings.Contains(csv, "0,90.000000,92.000000") {
		t.Fatalf("CSV missing VMAF columns/data:\n%s", csv)
	}
	svg := renderPlotSVG(report)
	if !strings.Contains(svg, "VMAF") || !strings.Contains(svg, "govpx 60.00 fps") {
		t.Fatalf("SVG missing VMAF summary:\n%s", svg)
	}
	text := formatPlotReport(report)
	if !strings.Contains(text, "vmaf=91.000") || !strings.Contains(text, "vmaf_delta=-2.000") {
		t.Fatalf("text report missing VMAF data:\n%s", text)
	}
}

func TestFakeVpxencHelper(t *testing.T) {
	if os.Getenv("GOVPX_FAKE_VPXENC") != "1" {
		return
	}
	output := ""
	limit := 1
	width := 16
	height := 16
	fps := 30
	bitrate := 1200
	for _, arg := range os.Args {
		if after, ok := strings.CutPrefix(arg, "--output="); ok {
			output = after
		}
		if after, ok := strings.CutPrefix(arg, "--limit="); ok {
			n, err := strconv.Atoi(after)
			if err == nil && n > 0 {
				limit = n
			}
		}
		if after, ok := strings.CutPrefix(arg, "--width="); ok {
			width = atoiPositive(after, width)
		}
		if after, ok := strings.CutPrefix(arg, "--height="); ok {
			height = atoiPositive(after, height)
		}
		if after, ok := strings.CutPrefix(arg, "--fps="); ok {
			fps = atoiPositive(strings.TrimSuffix(after, "/1"), fps)
		}
		if after, ok := strings.CutPrefix(arg, "--target-bitrate="); ok {
			bitrate = atoiPositive(after, bitrate)
		}
	}
	if output == "" {
		fmt.Fprintln(os.Stderr, "fake vpxenc missing --output")
		os.Exit(2)
	}
	if err := writeFakeIVF(output, width, height, fps, bitrate, limit); err != nil {
		fmt.Fprintf(os.Stderr, "fake vpxenc write output: %v\n", err)
		os.Exit(1)
	}
	// Mimic vpxenc's per-pass progress output so the bench's stderr
	// parser has something deterministic to read. 1000 us per frame is
	// arbitrary but small enough to leave room for non-zero subprocess
	// overhead in the wall-clock measurement.
	const usPerFrame = 1000
	totalUS := usPerFrame * limit
	fmt.Fprintf(os.Stderr, "Pass 1/1 frame %4d/%-4d %7dB %7d us %7.2f fps    \n", limit, limit, 0, totalUS, 1e6/float64(usPerFrame))
	os.Exit(0)
}

func TestFakeLibvpxOracleHelper(t *testing.T) {
	if os.Getenv("GOVPX_FAKE_LIBVPX_ORACLE") != "1" {
		return
	}
	subcmd := ""
	input := ""
	for i, arg := range os.Args {
		if arg == "decode" || arg == "decode-bench" {
			subcmd = arg
			if i+1 < len(os.Args) {
				input = os.Args[i+1]
			}
		}
	}
	if input == "" {
		fmt.Fprintln(os.Stderr, "fake libvpx oracle missing decode input")
		os.Exit(2)
	}
	ivf, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake libvpx oracle read input: %v\n", err)
		os.Exit(1)
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake libvpx oracle parse input: %v\n", err)
		os.Exit(1)
	}
	if subcmd == "decode-bench" {
		// Emit a deterministic oracle-bench summary so the bench's
		// stderr parser has something to read. 500 us/frame is
		// arbitrary but small enough to leave room for non-zero
		// subprocess overhead in the wall-clock measurement.
		const nsPerFrame = int64(500 * time.Microsecond)
		sumNS := nsPerFrame * int64(len(sizes))
		fmt.Fprintf(os.Stderr,
			"oracle-bench frames=%d decoded=%d sum_ns=%d loop_ns=%d p50_ns=%d p95_ns=%d p99_ns=%d\n",
			len(sizes), len(sizes), sumNS, sumNS, nsPerFrame, nsPerFrame, nsPerFrame)
		fmt.Println(len(sizes))
		os.Exit(0)
	}
	for i := range sizes {
		fmt.Printf("{\"frame\":%d}\n", i)
	}
	os.Exit(0)
}

func atoiPositive(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func fakeVpxencPath(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-vpxenc")
	body := fmt.Sprintf("#!/bin/sh\nGOVPX_FAKE_VPXENC=1 exec %s -test.run=TestFakeVpxencHelper -- \"$@\"\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return script
}

func fakeLibvpxOraclePath(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-libvpx-oracle")
	body := fmt.Sprintf("#!/bin/sh\nGOVPX_FAKE_LIBVPX_ORACLE=1 exec %s -test.run=TestFakeLibvpxOracleHelper -- \"$@\"\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return script
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func writeFakeIVF(path string, width int, height int, fps int, bitrate int, frames int) error {
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   bitrate,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    fps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return err
	}
	packets := make([][]byte, 0, frames)
	packet := make([]byte, max(4096, width*height*6))
	for i := range frames {
		result, err := enc.EncodeInto(packet, makeBenchmarkFrame(width, height, i), uint64(i), 1, 0)
		if err != nil {
			return err
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}

	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for _, packet := range packets {
		size += frameHeaderSize + len(packet)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[4:], 0)
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(ivf[12:], uint16(width))
	binary.LittleEndian.PutUint16(ivf[14:], uint16(height))
	binary.LittleEndian.PutUint32(ivf[16:], uint32(fps))
	binary.LittleEndian.PutUint32(ivf[20:], 1)
	binary.LittleEndian.PutUint32(ivf[24:], uint32(len(packets)))
	offset := fileHeaderSize
	for i, packet := range packets {
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(len(packet)))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(i))
		offset += frameHeaderSize
		copy(ivf[offset:], packet)
		offset += len(packet)
	}
	return os.WriteFile(path, ivf, 0o600)
}
