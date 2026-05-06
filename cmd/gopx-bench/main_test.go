package main

import (
	"encoding/json"
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
	if report.Encoder != "libgopx" || report.Mode != "realtime" {
		t.Fatalf("identity = %s/%s, want libgopx/realtime", report.Encoder, report.Mode)
	}
	if report.Width != 16 || report.Height != 16 || report.Frames != 3 || report.EncodedFrames == 0 {
		t.Fatalf("dimensions/counts = %+v", report)
	}
	if report.NSPerFrame <= 0 || report.EncodeFPS <= 0 || report.LatencyNS.P50 <= 0 || report.OutputBytes <= 0 {
		t.Fatalf("timing/output metrics = ns:%d fps:%f p50:%d bytes:%d", report.NSPerFrame, report.EncodeFPS, report.LatencyNS.P50, report.OutputBytes)
	}
	if report.PSNR <= 0 || report.Quantizers.Min <= 0 || report.Quantizers.Max < report.Quantizers.Min || len(report.QuantizerHist) == 0 {
		t.Fatalf("quality/quantizer metrics = psnr:%f quant:%+v hist:%v", report.PSNR, report.Quantizers, report.QuantizerHist)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("Marshal returned error: %v", err)
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
