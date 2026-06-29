package benchcmd

import (
	"encoding/binary"
	"encoding/json"
	govpx "github.com/thesyncim/govpx"
	"os"
	"path/filepath"
	"testing"
)

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
		EncodedFrames:     28,
		DroppedFrames:     2,
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
		EncodedFrames:     30,
		DroppedFrames:     0,
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
	if cmp.EncodedFramesDelta != report.EncodedFrames-reference.EncodedFrames {
		t.Fatalf("EncodedFramesDelta = %d, want %d", cmp.EncodedFramesDelta, report.EncodedFrames-reference.EncodedFrames)
	}
	if cmp.DroppedFramesDelta != report.DroppedFrames-reference.DroppedFrames {
		t.Fatalf("DroppedFramesDelta = %d, want %d", cmp.DroppedFramesDelta, report.DroppedFrames-reference.DroppedFrames)
	}
	var asJSON map[string]any
	raw, err := json.Marshal(cmp)
	if err != nil {
		t.Fatalf("Marshal comparison returned error: %v", err)
	}
	if err := json.Unmarshal(raw, &asJSON); err != nil {
		t.Fatalf("Unmarshal comparison returned error: %v", err)
	}
	if asJSON["encoded_frames_delta"] != float64(cmp.EncodedFramesDelta) ||
		asJSON["dropped_frames_delta"] != float64(cmp.DroppedFramesDelta) {
		t.Fatalf("comparison JSON missing frame deltas: %s", raw)
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
