package govpx

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestOracleRealtimeBenchAutoSpeedProductionParity pins the 720p realtime
// bench workload against the uninstrumented libvpx vpxenc binary. The
// realtime auto-speed path is wall-clock sensitive, so trace-instrumented
// vpxenc-oracle is not the right reference for this size-ratio check.
func TestOracleRealtimeBenchAutoSpeedProductionParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run production realtime bench parity")
	}
	vpxenc := findVpxenc(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 3000
		frames     = 60
	)
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    fps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		UndershootPct:       100,
		OvershootPct:        15,
		Threads:             1,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = realtimeBenchNoiseFrame(width, height, i)
	}

	govpxBytes, govpxCount := encodeRealtimeBenchWithGovpx(t, opts, sources)
	libvpxBytes, libvpxCount := encodeRealtimeBenchWithVpxenc(t, vpxenc, "rtbench-720p-cpu8", opts, targetKbps, sources)
	if govpxCount != frames || libvpxCount != frames {
		t.Fatalf("encoded frames govpx=%d libvpx=%d, want %d each", govpxCount, libvpxCount, frames)
	}
	ratio := float64(govpxBytes) / float64(libvpxBytes)
	t.Logf("720p realtime bench bytes: govpx=%d libvpx=%d ratio=%.4f", govpxBytes, libvpxBytes, ratio)
	if math.Abs(ratio-1.0) > 0.06 {
		t.Fatalf("720p realtime bench byte ratio = %.4f, want within 6%% of uninstrumented libvpx", ratio)
	}
}

func encodeRealtimeBenchWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) (int, int) {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	total := 0
	encoded := 0
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		total += result.SizeBytes
		encoded++
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			continue
		}
		total += result.SizeBytes
		encoded++
	}
	return total, encoded
}

func encodeRealtimeBenchWithVpxenc(t *testing.T, vpxenc string, name string, opts EncoderOptions, targetKbps int, sources []Image) (int, int) {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	writeEncoderValidationI420(t, yuvPath, sources)
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--rt",
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=1",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--kf-max-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--undershoot-pct=100",
		"--overshoot-pct=15",
		"--threads=1",
		"--token-parts=0",
		"--noise-sensitivity=0",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		yuvPath,
	}
	if out, err := exec.Command(vpxenc, args...).CombinedOutput(); err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("ReadFile %s returned error: %v", ivfPath, err)
	}
	return parseIVFFramePayloadSizes(t, data)
}

func realtimeBenchNoiseFrame(width, height, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func parseIVFFramePayloadSizes(t *testing.T, data []byte) (int, int) {
	t.Helper()
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(data) < fileHeaderSize || string(data[:4]) != "DKIF" {
		t.Fatalf("invalid IVF header")
	}
	total := 0
	frames := 0
	offset := fileHeaderSize
	for offset < len(data) {
		if offset+frameHeaderSize > len(data) {
			t.Fatalf("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += frameHeaderSize
		if size < 0 || offset+size > len(data) {
			t.Fatalf("truncated IVF payload size=%d offset=%d len=%d", size, offset, len(data))
		}
		total += size
		frames++
		offset += size
	}
	return total, frames
}
