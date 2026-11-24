package benchcmd

import (
	"strings"
	"testing"
)

func TestRunVP9BenchmarkOutputsMetrics(t *testing.T) {
	report, err := runVP9Benchmark(benchConfig{
		Codec:       codecVP9,
		Width:       64,
		Height:      64,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
	})
	if err != nil {
		t.Fatalf("runVP9Benchmark returned error: %v", err)
	}
	if report.Codec != codecVP9 {
		t.Fatalf("Codec = %q, want %q", report.Codec, codecVP9)
	}
	if report.Encoder != "govpx-vp9" {
		t.Fatalf("Encoder = %q, want govpx-vp9", report.Encoder)
	}
	if report.EncodedFrames == 0 || report.OutputBytes <= 0 {
		t.Fatalf("encode metrics = frames:%d bytes:%d, want positive",
			report.EncodedFrames, report.OutputBytes)
	}
	if report.NSPerFrame <= 0 || report.EncodeFPS <= 0 {
		t.Fatalf("timing metrics = ns:%d fps:%f, want positive",
			report.NSPerFrame, report.EncodeFPS)
	}
	if report.QualityFrames == 0 || report.PSNR <= 0 || report.SSIM <= 0 || report.SSIM > 1 {
		t.Fatalf("quality metrics = frames:%d psnr:%f ssim:%f, want populated",
			report.QualityFrames, report.PSNR, report.SSIM)
	}
}

func TestRunVP9BenchmarkSkipQuality(t *testing.T) {
	report, err := runVP9Benchmark(benchConfig{
		Codec:       codecVP9,
		Width:       64,
		Height:      64,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
		SkipQuality: true,
	})
	if err != nil {
		t.Fatalf("runVP9Benchmark returned error: %v", err)
	}
	if !report.QualitySkipped {
		t.Fatalf("QualitySkipped = false, want true")
	}
	if report.PSNR != 0 || report.SSIM != 0 || report.QualityFrames != 0 {
		t.Fatalf("quality fields = psnr:%f ssim:%f frames:%d, want zero",
			report.PSNR, report.SSIM, report.QualityFrames)
	}
}

func TestRunVP9BenchmarkPeriodicKeyframeReplay(t *testing.T) {
	report, err := runVP9Benchmark(benchConfig{
		Codec:       codecVP9,
		Width:       1280,
		Height:      720,
		Frames:      31,
		FPS:         30,
		BitrateKbps: 2500,
		Mode:        "good",
		CpuUsed:     8,
		Threads:     4,
		SkipQuality: true,
	})
	if err != nil {
		t.Fatalf("periodic keyframe benchmark: %v", err)
	}
	if report.EncodedFrames != 31 || report.OutputBytes <= 0 {
		t.Fatalf("periodic keyframe result = frames:%d bytes:%d, want 31 and positive bytes",
			report.EncodedFrames, report.OutputBytes)
	}
}

func TestRunVP9BenchmarkCheckerSub8EdgeFallback(t *testing.T) {
	report, err := runVP9BenchmarkWithSource(benchConfig{
		Codec: codecVP9, Width: 640, Height: 360, Frames: 66, FPS: 30,
		BitrateKbps: 600, Mode: "realtime", CpuUsed: 8,
	}, makeCheckerFrame)
	if err != nil {
		t.Fatalf("checker sub-8x8 edge fallback: %v", err)
	}
	if report.OutputBytes <= 0 || report.EncodedFrames <= 0 || report.QualityFrames <= 0 {
		t.Fatalf("checker sub-8x8 fallback result = frames:%d quality:%d bytes:%d, want positive",
			report.EncodedFrames, report.QualityFrames, report.OutputBytes)
	}
}

func TestRunVP9BenchmarkCPUProfileKeepsEncodeAllocsScoped(t *testing.T) {
	profile := t.TempDir() + "/vp9-encode.pprof"
	report, err := runVP9Benchmark(benchConfig{
		Codec:       codecVP9,
		Width:       64,
		Height:      64,
		Frames:      5,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
		CPUProfile:  profile,
		SkipQuality: true,
	})
	if err != nil {
		t.Fatalf("runVP9Benchmark returned error: %v", err)
	}
	maxAllocs := 64.0
	if puregoBuild {
		maxAllocs = 128
	}
	if report.AllocsPerFrame > maxAllocs {
		t.Fatalf("AllocsPerFrame with cpuprofile = %f, want <= %f for measured VP9 encode pass", report.AllocsPerFrame, maxAllocs)
	}
}

func TestRunVP9BenchmarkPhaseTiming(t *testing.T) {
	if !phaseTimingEnabled {
		t.Skip("phase timing requires the govpx_phase_stats build tag")
	}
	report, err := runVP9Benchmark(benchConfig{
		Codec:           codecVP9,
		Width:           64,
		Height:          64,
		Frames:          4,
		FPS:             30,
		BitrateKbps:     600,
		Mode:            "realtime",
		SkipQuality:     true,
		PhaseTiming:     true,
		LibvpxVpxencVP9: fakeVpxencPath(t),
	})
	if err != nil {
		t.Fatalf("runVP9Benchmark returned error: %v", err)
	}
	if report.PhaseNS == nil {
		t.Fatalf("PhaseNS = nil, want populated when PhaseTiming is true")
	}
	if report.PhaseNS.KeyAttempts == 0 || report.PhaseNS.InterAttempts == 0 {
		t.Fatalf("phase attempts = %+v, want key and inter attempts counted", *report.PhaseNS)
	}
	if report.PhaseNS.VP9ModeBlocks == 0 || report.PhaseNS.VP9InterPredictionBlocks == 0 {
		t.Fatalf("vp9 topology stats = %+v, want mode and predictor work counted", *report.PhaseNS)
	}
	if report.PhaseNS.VP9CountNS <= 0 || report.PhaseNS.VP9HeaderWriteNS <= 0 ||
		report.PhaseNS.VP9TileWriteNS <= 0 {
		t.Fatalf("vp9 phase timings = %+v, want count/header/tile work timed", *report.PhaseNS)
	}
	if report.Reference == nil || report.Reference.VP9CallStats == nil {
		t.Fatalf("reference VP9 call stats = %+v, want populated when phase timing is enabled", report.Reference)
	}
	if report.Reference.VP9CallStats.InterModePicks != 11 ||
		report.Reference.VP9CallStats.VarpartContentStateVeryHighSad != 37 {
		t.Fatalf("reference VP9 call stats = %+v", *report.Reference.VP9CallStats)
	}
	text := formatEncodeReport(report)
	if !strings.Contains(text, "vp9 phase/frame") ||
		!strings.Contains(text, "vp9 topology") || !strings.Contains(text, "vp9 mode pass") ||
		!strings.Contains(text, "vp9 inter pass") || !strings.Contains(text, "vp9 predictor") ||
		!strings.Contains(text, "vp9 varpass") || !strings.Contains(text, "vp9 content") ||
		!strings.Contains(text, "libvpx topology") || !strings.Contains(text, "libvpx content") {
		t.Fatalf("formatted report missing VP9 phase stats:\n%s", text)
	}
}

func TestVP9LibvpxEncoderPathUsesStatsOnlyForPhaseTiming(t *testing.T) {
	cfg := benchConfig{
		LibvpxVpxencVP9:      "/tmp/vpxenc-vp9",
		LibvpxVpxencVP9Stats: "/tmp/vpxenc-vp9-callstats",
	}
	if got := vp9LibvpxEncoderPath(cfg); got != cfg.LibvpxVpxencVP9 {
		t.Fatalf("normal VP9 libvpx path = %q, want %q", got, cfg.LibvpxVpxencVP9)
	}
	cfg.PhaseTiming = true
	if got := vp9LibvpxEncoderPath(cfg); got != cfg.LibvpxVpxencVP9Stats {
		t.Fatalf("phase VP9 libvpx path = %q, want %q", got, cfg.LibvpxVpxencVP9Stats)
	}
	cfg.LibvpxVpxencVP9Stats = ""
	if got := vp9LibvpxEncoderPath(cfg); got != cfg.LibvpxVpxencVP9 {
		t.Fatalf("phase fallback VP9 libvpx path = %q, want %q", got, cfg.LibvpxVpxencVP9)
	}
}

func TestRunVP9BenchmarkWithCustomSource(t *testing.T) {
	report, err := runVP9BenchmarkWithSource(benchConfig{
		Codec:       codecVP9,
		Width:       64,
		Height:      64,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
	}, makePanningFrame)
	if err != nil {
		t.Fatalf("runVP9BenchmarkWithSource panning returned error: %v", err)
	}
	if report.EncodedFrames == 0 {
		t.Fatalf("panning source produced zero encoded frames")
	}

	report2, err := runVP9BenchmarkWithSource(benchConfig{
		Codec:       codecVP9,
		Width:       64,
		Height:      64,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
	}, makeCheckerFrame)
	if err != nil {
		t.Fatalf("runVP9BenchmarkWithSource checker returned error: %v", err)
	}
	if report2.EncodedFrames == 0 {
		t.Fatalf("checker source produced zero encoded frames")
	}
}

func TestRunVP9BenchmarkRejectsBadConfig(t *testing.T) {
	if _, err := runVP9Benchmark(benchConfig{Codec: codecVP9, Width: 0, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 1200, Mode: "realtime"}); err == nil {
		t.Fatalf("runVP9Benchmark accepted zero width")
	}
	if _, err := runVP9Benchmark(benchConfig{Codec: codecVP9, Width: 16, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 1200, Mode: "slow"}); err == nil {
		t.Fatalf("runVP9Benchmark accepted unsupported mode")
	}
}

func TestImageToYCbCrAliasesPlanes(t *testing.T) {
	img := makeBenchmarkFrame(32, 32, 0)
	y := imageToYCbCr(img)
	if y == nil || len(y.Y) == 0 {
		t.Fatalf("imageToYCbCr returned empty result")
	}
	if y.Rect.Dx() != 32 || y.Rect.Dy() != 32 {
		t.Fatalf("Rect = %v, want 32x32", y.Rect)
	}
	if y.YStride != img.YStride || y.CStride != (img.Width+1)>>1 {
		t.Fatalf("strides = y:%d c:%d, want y:%d c:%d", y.YStride, y.CStride, img.YStride, (img.Width+1)>>1)
	}
	// Plane slices must alias rather than copy so the encoder sees the
	// caller's pixels directly.
	if &y.Y[0] != &img.Y[0] || &y.Cb[0] != &img.U[0] || &y.Cr[0] != &img.V[0] {
		t.Fatalf("imageToYCbCr did not alias plane slices")
	}
}

func TestParseVP9IVFFrameInfoRejectsInvalid(t *testing.T) {
	if _, err := parseVP9IVFFrameInfo(nil); err == nil {
		t.Fatalf("parseVP9IVFFrameInfo accepted nil input")
	}
	if _, err := parseVP9IVFFrameInfo([]byte("not an ivf")); err == nil {
		t.Fatalf("parseVP9IVFFrameInfo accepted garbage prefix")
	}
}

func TestRunVP9BenchmarkReportFormat(t *testing.T) {
	report, err := runVP9Benchmark(benchConfig{
		Codec:       codecVP9,
		Width:       64,
		Height:      64,
		Frames:      3,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
	})
	if err != nil {
		t.Fatalf("runVP9Benchmark returned error: %v", err)
	}
	text := formatEncodeReport(report)
	if !strings.Contains(text, "encode") || !strings.Contains(text, "ns/frame") {
		t.Fatalf("formatted VP9 report missing expected fields:\n%s", text)
	}
}
