package benchcmd

import (
	"encoding/json"
	"flag"
	govpx "github.com/thesyncim/govpx"
	"slices"
	"strings"
	"testing"
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
	if report.Reference.Encoder != "libvpx-vp8" || report.Reference.EncodedFrames != 3 || report.Reference.DroppedFrames != 0 || report.Reference.OutputBytes <= 0 {
		t.Fatalf("reference = %+v, want libvpx-vp8 with 3 encoded frames, 0 drops, and bytes", *report.Reference)
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
	text := formatEncodeReport(report)
	if !strings.Contains(text, "frames encoded/dropped") || !strings.Contains(text, "3/0") {
		t.Fatalf("formatted reference report missing encoded/drop counts:\n%s", text)
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
	if _, err := runEncodeSuite(benchConfig{Codec: codecVP9}, "quick", 1); err == nil || !strings.Contains(err.Error(), "vpxenc-vp9") {
		t.Fatalf("runEncodeSuite VP9 without vpxenc-vp9 err = %v, want vpxenc-vp9 reference error", err)
	}
}

func TestRunEncodeSuiteCaseUsesVP9ReferencePath(t *testing.T) {
	tc := encodeSuiteCase{
		name:        "tiny-vp9",
		width:       32,
		height:      32,
		frames:      2,
		fps:         30,
		bitrateKbps: 600,
		mode:        "realtime",
	}
	report, err := runEncodeSuiteCase(benchConfig{
		Codec:           codecVP9,
		SkipQuality:     true,
		LibvpxVpxencVP9: fakeVpxencPath(t),
	}, tc, 1)
	if err != nil {
		t.Fatalf("runEncodeSuiteCase VP9 returned error: %v", err)
	}
	if report.Codec != codecVP9 {
		t.Fatalf("Codec = %q, want %q", report.Codec, codecVP9)
	}
	if report.Reference == nil || report.Reference.Encoder != "libvpx-vp9" {
		t.Fatalf("Reference = %+v, want libvpx-vp9", report.Reference)
	}
	if report.Comparison == nil || report.Comparison.NSPerFrameRatio <= 0 {
		t.Fatalf("Comparison = %+v, want populated timing ratio", report.Comparison)
	}
	if !report.QualitySkipped || !report.Reference.QualitySkipped {
		t.Fatalf("QualitySkipped govpx/reference = %v/%v, want both true", report.QualitySkipped, report.Reference.QualitySkipped)
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
			DroppedFrames:     4,
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

func TestRunBenchmarkRejectsBadConfig(t *testing.T) {
	if _, err := runBenchmark(benchConfig{Width: 16, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 1200, Mode: "slow"}); err == nil {
		t.Fatalf("runBenchmark accepted unsupported mode")
	}
	if _, err := runBenchmark(benchConfig{Width: 0, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 1200}); err == nil {
		t.Fatalf("runBenchmark accepted invalid dimensions")
	}
}
