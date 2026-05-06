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
	if report.Reference.Encoder != "libvpx-vp8" || report.Reference.EncodedFrames != 3 || report.Reference.OutputBytes != 68 {
		t.Fatalf("reference = %+v, want libvpx-vp8 with 3 frames and 68 bytes", *report.Reference)
	}
	if report.Reference.KeyframeBytes != 33 || report.Reference.AvgInterBytes != 17.5 {
		t.Fatalf("reference sizes = key:%d inter:%f, want 33/17.5", report.Reference.KeyframeBytes, report.Reference.AvgInterBytes)
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

func TestFakeVpxencHelper(t *testing.T) {
	if os.Getenv("LIBGOPX_FAKE_VPXENC") != "1" {
		return
	}
	output := ""
	limit := 1
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
	}
	if output == "" {
		fmt.Fprintln(os.Stderr, "fake vpxenc missing --output")
		os.Exit(2)
	}
	if err := writeFakeIVF(output, limit); err != nil {
		fmt.Fprintf(os.Stderr, "fake vpxenc write output: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
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

func writeFakeIVF(path string, frames int) error {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for i := 0; i < frames; i++ {
		size += frameHeaderSize + fakeFrameSize(i)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[4:], 0)
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint32(ivf[24:], uint32(frames))
	offset := fileHeaderSize
	for i := 0; i < frames; i++ {
		frameSize := fakeFrameSize(i)
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(frameSize))
		offset += frameHeaderSize
		for j := 0; j < frameSize; j++ {
			ivf[offset+j] = byte(i + j)
		}
		offset += frameSize
	}
	return os.WriteFile(path, ivf, 0o600)
}

func fakeFrameSize(index int) int {
	if index == 0 {
		return 33
	}
	return 10 + index*5
}
