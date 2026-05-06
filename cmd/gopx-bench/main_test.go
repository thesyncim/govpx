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

	libgopx "github.com/thesyncim/libgopx"
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
	if report.Reference.KeyframeBytes <= 0 || report.Reference.AvgInterBytes <= 0 {
		t.Fatalf("reference sizes = key:%d inter:%f, want positive values", report.Reference.KeyframeBytes, report.Reference.AvgInterBytes)
	}
	if report.Reference.PSNR <= 0 || report.Reference.SSIM <= 0 || report.Reference.SSIM > 1 || report.Reference.QualityFrames != 3 || report.Reference.QualityError != "" {
		t.Fatalf("reference quality = psnr:%f ssim:%f frames:%d err:%q, want 3 decoded quality frames", report.Reference.PSNR, report.Reference.SSIM, report.Reference.QualityFrames, report.Reference.QualityError)
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
	frames := []libgopx.Image{
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
	if os.Getenv("LIBGOPX_FAKE_VPXENC") != "1" {
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
	body := fmt.Sprintf("#!/bin/sh\nLIBGOPX_FAKE_VPXENC=1 exec %s -test.run=TestFakeVpxencHelper -- \"$@\"\n", shellQuote(os.Args[0]))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return script
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func writeFakeIVF(path string, width int, height int, fps int, bitrate int, frames int) error {
	enc, err := libgopx.NewVP8Encoder(libgopx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     libgopx.RateControlCBR,
		TargetBitrateKbps:   bitrate,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            libgopx.DeadlineRealtime,
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
