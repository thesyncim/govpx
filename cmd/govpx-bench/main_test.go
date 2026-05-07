package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
	if report.PSNR <= 0 || report.SSIM <= 0 || report.SSIM > 1 || report.Quantizers.Min <= 0 || report.Quantizers.Max < report.Quantizers.Min || len(report.QuantizerHist) == 0 {
		t.Fatalf("quality/quantizer metrics = psnr:%f ssim:%f quant:%+v hist:%v", report.PSNR, report.SSIM, report.Quantizers, report.QuantizerHist)
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
	if report.AllocsPerFrame != 0 {
		t.Fatalf("AllocsPerFrame = %f, want 0 for measured decode pass", report.AllocsPerFrame)
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
		if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
		}
		if strings.HasPrefix(arg, "--limit=") {
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err == nil && n > 0 {
				limit = n
			}
		}
		if strings.HasPrefix(arg, "--width=") {
			width = atoiPositive(strings.TrimPrefix(arg, "--width="), width)
		}
		if strings.HasPrefix(arg, "--height=") {
			height = atoiPositive(strings.TrimPrefix(arg, "--height="), height)
		}
		if strings.HasPrefix(arg, "--fps=") {
			fps = atoiPositive(strings.TrimSuffix(strings.TrimPrefix(arg, "--fps="), "/1"), fps)
		}
		if strings.HasPrefix(arg, "--target-bitrate=") {
			bitrate = atoiPositive(strings.TrimPrefix(arg, "--target-bitrate="), bitrate)
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
	os.Exit(0)
}

func TestFakeLibvpxOracleHelper(t *testing.T) {
	if os.Getenv("GOVPX_FAKE_LIBVPX_ORACLE") != "1" {
		return
	}
	input := ""
	for i, arg := range os.Args {
		if arg == "decode" && i+1 < len(os.Args) {
			input = os.Args[i+1]
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
	for i := 0; i < frames; i++ {
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
