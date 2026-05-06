package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	libgopx "github.com/thesyncim/libgopx"
)

const quantizerHistogramBins = 128

type benchConfig struct {
	Width        int
	Height       int
	Frames       int
	FPS          int
	BitrateKbps  int
	Mode         string
	Decode       bool
	LibvpxVpxenc string
	LibvpxOracle string
	LibvpxArgs   []string
}

type benchReport struct {
	Encoder           string             `json:"encoder"`
	Mode              string             `json:"mode"`
	Width             int                `json:"width"`
	Height            int                `json:"height"`
	Frames            int                `json:"frames"`
	FPS               int                `json:"fps"`
	TargetBitrateKbps int                `json:"target_bitrate_kbps"`
	OutputBitrateKbps float64            `json:"output_bitrate_kbps"`
	BitrateErrorPct   float64            `json:"bitrate_error_pct"`
	NSPerFrame        int64              `json:"ns_per_frame"`
	EncodeFPS         float64            `json:"encode_fps"`
	MacroblocksPerSec float64            `json:"macroblocks_per_second"`
	AllocsPerFrame    float64            `json:"allocs_per_frame"`
	PSNR              float64            `json:"psnr"`
	SSIM              float64            `json:"ssim"`
	KeyframeBytes     int                `json:"keyframe_bytes"`
	AvgInterBytes     float64            `json:"avg_interframe_bytes"`
	Quantizers        quantizerReport    `json:"quantizers"`
	LatencyNS         latencyReport      `json:"latency_ns"`
	OutputBytes       int                `json:"output_bytes"`
	EncodedFrames     int                `json:"encoded_frames"`
	DroppedFrames     int                `json:"dropped_frames"`
	QuantizerHist     map[string]int     `json:"quantizer_histogram"`
	Reference         *referenceReport   `json:"reference,omitempty"`
	Options           benchConfigSummary `json:"options"`
}

type referenceReport struct {
	Encoder           string        `json:"encoder"`
	Mode              string        `json:"mode"`
	OutputBitrateKbps float64       `json:"output_bitrate_kbps"`
	BitrateErrorPct   float64       `json:"bitrate_error_pct"`
	NSPerFrame        int64         `json:"ns_per_frame"`
	EncodeFPS         float64       `json:"encode_fps"`
	MacroblocksPerSec float64       `json:"macroblocks_per_second"`
	PSNR              float64       `json:"psnr"`
	SSIM              float64       `json:"ssim"`
	QualityFrames     int           `json:"quality_frames"`
	QualityError      string        `json:"quality_error,omitempty"`
	KeyframeBytes     int           `json:"keyframe_bytes"`
	AvgInterBytes     float64       `json:"avg_interframe_bytes"`
	LatencyNS         latencyReport `json:"latency_ns"`
	OutputBytes       int           `json:"output_bytes"`
	EncodedFrames     int           `json:"encoded_frames"`
}

type decodeBenchReport struct {
	Decoder                  string                 `json:"decoder"`
	Operation                string                 `json:"operation"`
	Mode                     string                 `json:"mode"`
	Width                    int                    `json:"width"`
	Height                   int                    `json:"height"`
	Frames                   int                    `json:"frames"`
	FPS                      int                    `json:"fps"`
	InputBytes               int                    `json:"input_bytes"`
	DecodedFrames            int                    `json:"decoded_frames"`
	NSPerFrame               int64                  `json:"ns_per_frame"`
	DecodeFPS                float64                `json:"decode_fps"`
	MacroblocksPerSec        float64                `json:"macroblocks_per_second"`
	CodedMegabytesPerSec     float64                `json:"coded_megabytes_per_second"`
	AllocsPerFrame           float64                `json:"allocs_per_frame"`
	LatencyNS                latencyReport          `json:"latency_ns"`
	Reference                *decodeReferenceReport `json:"reference,omitempty"`
	RelativeSpeedVsReference float64                `json:"relative_speed_vs_reference,omitempty"`
	Options                  benchConfigSummary     `json:"options"`
}

type decodeReferenceReport struct {
	Decoder              string        `json:"decoder"`
	Mode                 string        `json:"mode"`
	DecodedFrames        int           `json:"decoded_frames"`
	NSPerFrame           int64         `json:"ns_per_frame"`
	DecodeFPS            float64       `json:"decode_fps"`
	MacroblocksPerSec    float64       `json:"macroblocks_per_second"`
	CodedMegabytesPerSec float64       `json:"coded_megabytes_per_second"`
	LatencyNS            latencyReport `json:"latency_ns"`
}

type quantizerReport struct {
	Min  int     `json:"min"`
	Max  int     `json:"max"`
	Mean float64 `json:"mean"`
}

type latencyReport struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
}

type benchConfigSummary struct {
	Deadline string `json:"deadline"`
}

func main() {
	cfg := benchConfig{}
	flag.IntVar(&cfg.Width, "width", 64, "frame width")
	flag.IntVar(&cfg.Height, "height", 64, "frame height")
	flag.IntVar(&cfg.Frames, "frames", 30, "number of frames")
	flag.IntVar(&cfg.FPS, "fps", 30, "frame rate")
	flag.IntVar(&cfg.BitrateKbps, "bitrate", 1200, "target bitrate in kbps")
	flag.StringVar(&cfg.Mode, "mode", "realtime", "encoder mode: realtime or good")
	flag.BoolVar(&cfg.Decode, "decode", false, "run decoder benchmark mode")
	flag.StringVar(&cfg.LibvpxVpxenc, "libvpx-vpxenc", os.Getenv("LIBGOPX_VPXENC"), "optional libvpx vpxenc path for reference comparison")
	flag.StringVar(&cfg.LibvpxOracle, "libvpx-oracle", os.Getenv("LIBGOPX_ORACLE"), "optional libvpx checksum oracle path for decoder reference timing")
	flag.Parse()

	var report any
	var err error
	if cfg.Decode {
		report, err = runDecodeBenchmark(cfg)
	} else {
		report, err = runBenchmark(cfg)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopx-bench: %v\n", err)
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "gopx-bench: encode json: %v\n", err)
		os.Exit(1)
	}
}

func runBenchmark(cfg benchConfig) (benchReport, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Frames <= 0 || cfg.FPS <= 0 || cfg.BitrateKbps <= 0 {
		return benchReport{}, errors.New("width, height, frames, fps, and bitrate must be positive")
	}
	if cfg.Width > 16383 || cfg.Height > 16383 {
		return benchReport{}, errors.New("dimensions exceed VP8 limits")
	}
	deadline, deadlineName, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		return benchReport{}, err
	}

	frames := make([]libgopx.Image, cfg.Frames)
	for i := range frames {
		frames[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
	}

	enc, err := newBenchmarkEncoder(cfg, deadline)
	if err != nil {
		return benchReport{}, err
	}

	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	latencies := make([]int64, 0, cfg.Frames)
	var quantHist [quantizerHistogramBins]int
	quantMin := 0
	quantMax := 0
	quantSum := 0
	encodedFrames := 0
	droppedFrames := 0
	outputBytes := 0
	keyframeBytes := 0
	interBytes := 0
	interCount := 0

	runtime.GC()
	for i, frame := range frames {
		if _, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0); err != nil {
			return benchReport{}, err
		}
	}
	enc.Reset()
	var memBefore runtime.MemStats
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	for i, frame := range frames {
		start := time.Now()
		result, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0)
		elapsed := time.Since(start)
		if err != nil {
			return benchReport{}, err
		}
		latencies = append(latencies, elapsed.Nanoseconds())
		if result.Dropped {
			droppedFrames++
			continue
		}
		encodedFrames++
		outputBytes += result.SizeBytes
		quantSum += result.Quantizer
		if quantMin == 0 || result.Quantizer < quantMin {
			quantMin = result.Quantizer
		}
		if result.Quantizer > quantMax {
			quantMax = result.Quantizer
		}
		if result.Quantizer >= 0 && result.Quantizer < len(quantHist) {
			quantHist[result.Quantizer]++
		}
		if result.KeyFrame {
			keyframeBytes = result.SizeBytes
		} else {
			interBytes += result.SizeBytes
			interCount++
		}
	}
	runtime.ReadMemStats(&memAfter)
	psnr, ssim, _, err := benchmarkQualityMetrics(cfg, deadline, frames)
	if err != nil {
		return benchReport{}, err
	}

	totalLatency := int64(0)
	for _, ns := range latencies {
		totalLatency += ns
	}
	nsPerFrame := totalLatency / int64(len(latencies))
	outputBitrate := float64(outputBytes*8*cfg.FPS) / float64(cfg.Frames*1000)
	bitrateError := 0.0
	if cfg.BitrateKbps > 0 {
		bitrateError = (outputBitrate - float64(cfg.BitrateKbps)) * 100 / float64(cfg.BitrateKbps)
	}
	avgInter := 0.0
	if interCount > 0 {
		avgInter = float64(interBytes) / float64(interCount)
	}
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	quantMean := 0.0
	if encodedFrames > 0 {
		quantMean = float64(quantSum) / float64(encodedFrames)
	}

	report := benchReport{
		Encoder:           "libgopx",
		Mode:              deadlineName,
		Width:             cfg.Width,
		Height:            cfg.Height,
		Frames:            cfg.Frames,
		FPS:               cfg.FPS,
		TargetBitrateKbps: cfg.BitrateKbps,
		OutputBitrateKbps: outputBitrate,
		BitrateErrorPct:   bitrateError,
		NSPerFrame:        nsPerFrame,
		EncodeFPS:         1e9 / float64(nsPerFrame),
		MacroblocksPerSec: macroblocksPerFrame * 1e9 / float64(nsPerFrame),
		AllocsPerFrame:    float64(memAfter.Mallocs-memBefore.Mallocs) / float64(cfg.Frames),
		PSNR:              psnr,
		SSIM:              ssim,
		KeyframeBytes:     keyframeBytes,
		AvgInterBytes:     avgInter,
		Quantizers: quantizerReport{
			Min:  quantMin,
			Max:  quantMax,
			Mean: quantMean,
		},
		LatencyNS: latencyReport{
			P50: percentileLatency(latencies, 50),
			P95: percentileLatency(latencies, 95),
			P99: percentileLatency(latencies, 99),
		},
		OutputBytes:   outputBytes,
		EncodedFrames: encodedFrames,
		DroppedFrames: droppedFrames,
		QuantizerHist: quantizerHistogramMap(&quantHist),
		Options:       benchConfigSummary{Deadline: deadlineName},
	}
	if cfg.LibvpxVpxenc != "" {
		reference, err := runLibvpxBenchmark(cfg, frames, deadlineName)
		if err != nil {
			return benchReport{}, err
		}
		report.Reference = &reference
	}
	return report, nil
}

func runDecodeBenchmark(cfg benchConfig) (decodeBenchReport, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Frames <= 0 || cfg.FPS <= 0 || cfg.BitrateKbps <= 0 {
		return decodeBenchReport{}, errors.New("width, height, frames, fps, and bitrate must be positive")
	}
	if cfg.Width > 16383 || cfg.Height > 16383 {
		return decodeBenchReport{}, errors.New("dimensions exceed VP8 limits")
	}
	deadline, deadlineName, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		return decodeBenchReport{}, err
	}
	frames := make([]libgopx.Image, cfg.Frames)
	for i := range frames {
		frames[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
	}
	packets, err := encodeBenchmarkPackets(cfg, deadline, frames)
	if err != nil {
		return decodeBenchReport{}, err
	}
	ivf := makeBenchmarkIVF(cfg.Width, cfg.Height, cfg.FPS, packets)
	dec, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		return decodeBenchReport{}, err
	}
	if _, _, err := decodeBenchmarkPackets(dec, packets, make([]int64, 0, len(packets))); err != nil {
		return decodeBenchReport{}, err
	}

	runtime.GC()
	latencies := make([]int64, 0, len(packets))
	if _, latencies, err = decodeBenchmarkPackets(dec, packets, latencies); err != nil {
		return decodeBenchReport{}, err
	}
	latencies = latencies[:0]
	var memBefore runtime.MemStats
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	decodedFrames, latencies, err := decodeBenchmarkPackets(dec, packets, latencies)
	runtime.ReadMemStats(&memAfter)
	if err != nil {
		return decodeBenchReport{}, err
	}
	totalLatency := totalLatencyNS(latencies)
	nsPerFrame := totalLatency / int64(len(latencies))
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	report := decodeBenchReport{
		Decoder:              "libgopx",
		Operation:            "decode",
		Mode:                 deadlineName,
		Width:                cfg.Width,
		Height:               cfg.Height,
		Frames:               len(packets),
		FPS:                  cfg.FPS,
		InputBytes:           len(ivf),
		DecodedFrames:        decodedFrames,
		NSPerFrame:           nsPerFrame,
		DecodeFPS:            1e9 / float64(nsPerFrame),
		MacroblocksPerSec:    macroblocksPerFrame * 1e9 / float64(nsPerFrame),
		CodedMegabytesPerSec: codedMegabytesPerSecond(len(ivf), totalLatency),
		AllocsPerFrame:       float64(memAfter.Mallocs-memBefore.Mallocs) / float64(len(packets)),
		LatencyNS: latencyReport{
			P50: percentileLatency(latencies, 50),
			P95: percentileLatency(latencies, 95),
			P99: percentileLatency(latencies, 99),
		},
		Options: benchConfigSummary{Deadline: deadlineName},
	}
	if cfg.LibvpxOracle != "" {
		reference, err := runLibvpxDecodeBenchmark(cfg, ivf, deadlineName, len(packets))
		if err != nil {
			return decodeBenchReport{}, err
		}
		report.Reference = &reference
		if report.NSPerFrame > 0 {
			report.RelativeSpeedVsReference = float64(reference.NSPerFrame) / float64(report.NSPerFrame)
		}
	}
	return report, nil
}

func encodeBenchmarkPackets(cfg benchConfig, deadline libgopx.Deadline, frames []libgopx.Image) ([][]byte, error) {
	enc, err := newBenchmarkEncoder(cfg, deadline)
	if err != nil {
		return nil, err
	}
	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	packets := make([][]byte, 0, len(frames))
	for i, frame := range frames {
		result, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0)
		if err != nil {
			return nil, err
		}
		if result.Dropped {
			continue
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	if len(packets) == 0 {
		return nil, errors.New("benchmark encoder dropped every frame")
	}
	return packets, nil
}

func decodeBenchmarkPackets(dec *libgopx.VP8Decoder, packets [][]byte, latencies []int64) (int, []int64, error) {
	dec.Reset()
	decodedFrames := 0
	for i, packet := range packets {
		start := time.Now()
		if err := dec.Decode(packet); err != nil {
			return decodedFrames, latencies, fmt.Errorf("decode frame %d: %w", i, err)
		}
		if _, ok := dec.NextFrame(); ok {
			decodedFrames++
		}
		latencies = append(latencies, time.Since(start).Nanoseconds())
	}
	return decodedFrames, latencies, nil
}

func newBenchmarkEncoder(cfg benchConfig, deadline libgopx.Deadline) (*libgopx.VP8Encoder, error) {
	return libgopx.NewVP8Encoder(libgopx.EncoderOptions{
		Width:               cfg.Width,
		Height:              cfg.Height,
		FPS:                 cfg.FPS,
		RateControlMode:     libgopx.RateControlCBR,
		TargetBitrateKbps:   cfg.BitrateKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            deadline,
		CpuUsed:             8,
		KeyFrameInterval:    cfg.FPS,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
}

func benchmarkQualityMetrics(cfg benchConfig, deadline libgopx.Deadline, frames []libgopx.Image) (float64, float64, int, error) {
	enc, err := newBenchmarkEncoder(cfg, deadline)
	if err != nil {
		return 0, 0, 0, err
	}
	dec, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	psnrSum := 0.0
	ssimSum := 0.0
	qualityFrames := 0
	for i, frame := range frames {
		result, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0)
		if err != nil {
			return 0, 0, qualityFrames, err
		}
		if result.Dropped {
			continue
		}
		if err := dec.Decode(result.Data); err != nil {
			return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, err)
		}
		decoded, ok := dec.NextFrame()
		if !ok {
			continue
		}
		psnrSum += imagePSNR(frame, decoded)
		ssimSum += imageSSIM(frame, decoded)
		qualityFrames++
	}
	return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, nil)
}

func quantizerHistogramMap(hist *[quantizerHistogramBins]int) map[string]int {
	count := 0
	for _, frames := range hist {
		if frames > 0 {
			count++
		}
	}
	out := make(map[string]int, count)
	for q, frames := range hist {
		if frames > 0 {
			out[strconv.Itoa(q)] = frames
		}
	}
	return out
}

func benchmarkDeadline(mode string) (libgopx.Deadline, string, error) {
	switch mode {
	case "", "realtime":
		return libgopx.DeadlineRealtime, "realtime", nil
	case "good":
		return libgopx.DeadlineGoodQuality, "good", nil
	default:
		return 0, "", fmt.Errorf("unsupported mode %q", mode)
	}
}

func makeBenchmarkFrame(width int, height int, index int) libgopx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := libgopx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func runLibvpxBenchmark(cfg benchConfig, frames []libgopx.Image, deadlineName string) (referenceReport, error) {
	tempDir, err := os.MkdirTemp("", "libgopx-bench-*")
	if err != nil {
		return referenceReport{}, err
	}
	defer os.RemoveAll(tempDir)

	rawPath := tempDir + string(os.PathSeparator) + "input.i420"
	outPath := tempDir + string(os.PathSeparator) + "output.ivf"
	raw, err := os.Create(rawPath)
	if err != nil {
		return referenceReport{}, err
	}
	for _, frame := range frames {
		if err := writeI420Frame(raw, frame); err != nil {
			raw.Close()
			return referenceReport{}, err
		}
	}
	if err := raw.Close(); err != nil {
		return referenceReport{}, err
	}

	vpxDeadlineFlag := "--rt"
	if deadlineName == "good" {
		vpxDeadlineFlag = "--good"
	}
	args := append([]string{}, cfg.LibvpxArgs...)
	args = append(args,
		"--codec=vp8",
		"--ivf",
		"--i420",
		fmt.Sprintf("--width=%d", cfg.Width),
		fmt.Sprintf("--height=%d", cfg.Height),
		fmt.Sprintf("--fps=%d/1", cfg.FPS),
		fmt.Sprintf("--limit=%d", cfg.Frames),
		fmt.Sprintf("--target-bitrate=%d", cfg.BitrateKbps),
		vpxDeadlineFlag,
		"--cpu-used=8",
		fmt.Sprintf("--output=%s", outPath),
		rawPath,
	)
	start := time.Now()
	cmd := exec.Command(cfg.LibvpxVpxenc, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return referenceReport{}, fmt.Errorf("libvpx vpxenc failed: %w\n%s", err, out)
	}
	elapsed := time.Since(start)

	ivf, err := os.ReadFile(outPath)
	if err != nil {
		return referenceReport{}, err
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		return referenceReport{}, err
	}
	outputBytes := 0
	for _, size := range sizes {
		outputBytes += size
	}
	nsPerFrame := elapsed.Nanoseconds() / int64(len(frames))
	outputBitrate := float64(outputBytes*8*cfg.FPS) / float64(cfg.Frames*1000)
	bitrateError := (outputBitrate - float64(cfg.BitrateKbps)) * 100 / float64(cfg.BitrateKbps)
	keyframeBytes := 0
	interBytes := 0
	if len(sizes) > 0 {
		keyframeBytes = sizes[0]
		for _, size := range sizes[1:] {
			interBytes += size
		}
	}
	avgInter := 0.0
	if len(sizes) > 1 {
		avgInter = float64(interBytes) / float64(len(sizes)-1)
	}
	psnr, ssim, qualityFrames, qualityErr := referenceQualityMetrics(ivf, frames)
	qualityError := ""
	if qualityErr != nil {
		qualityError = qualityErr.Error()
	}
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	return referenceReport{
		Encoder:           "libvpx-vp8",
		Mode:              deadlineName,
		OutputBitrateKbps: outputBitrate,
		BitrateErrorPct:   bitrateError,
		NSPerFrame:        nsPerFrame,
		EncodeFPS:         1e9 / float64(nsPerFrame),
		MacroblocksPerSec: macroblocksPerFrame * 1e9 / float64(nsPerFrame),
		PSNR:              psnr,
		SSIM:              ssim,
		QualityFrames:     qualityFrames,
		QualityError:      qualityError,
		KeyframeBytes:     keyframeBytes,
		AvgInterBytes:     avgInter,
		LatencyNS: latencyReport{
			P50: nsPerFrame,
			P95: nsPerFrame,
			P99: nsPerFrame,
		},
		OutputBytes:   outputBytes,
		EncodedFrames: len(sizes),
	}, nil
}

func runLibvpxDecodeBenchmark(cfg benchConfig, ivf []byte, deadlineName string, frames int) (decodeReferenceReport, error) {
	tempDir, err := os.MkdirTemp("", "libgopx-decode-bench-*")
	if err != nil {
		return decodeReferenceReport{}, err
	}
	defer os.RemoveAll(tempDir)

	path := tempDir + string(os.PathSeparator) + "input.ivf"
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		return decodeReferenceReport{}, err
	}
	start := time.Now()
	cmd := exec.Command(cfg.LibvpxOracle, "decode", path)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		return decodeReferenceReport{}, fmt.Errorf("libvpx oracle decode failed: %w\n%s", err, out)
	}
	decodedFrames := countJSONLines(out)
	if decodedFrames == 0 {
		return decodeReferenceReport{}, errors.New("libvpx oracle decoded zero frames")
	}
	nsPerFrame := elapsed.Nanoseconds() / int64(frames)
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	return decodeReferenceReport{
		Decoder:              "libvpx-vp8",
		Mode:                 deadlineName,
		DecodedFrames:        decodedFrames,
		NSPerFrame:           nsPerFrame,
		DecodeFPS:            1e9 / float64(nsPerFrame),
		MacroblocksPerSec:    macroblocksPerFrame * 1e9 / float64(nsPerFrame),
		CodedMegabytesPerSec: codedMegabytesPerSecond(len(ivf), elapsed.Nanoseconds()),
		LatencyNS: latencyReport{
			P50: nsPerFrame,
			P95: nsPerFrame,
			P99: nsPerFrame,
		},
	}, nil
}

func countJSONLines(out []byte) int {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func benchmarkMacroblocks(width int, height int) float64 {
	cols := (width + 15) >> 4
	rows := (height + 15) >> 4
	return float64(cols * rows)
}

func totalLatencyNS(latencies []int64) int64 {
	total := int64(0)
	for _, ns := range latencies {
		total += ns
	}
	return total
}

func codedMegabytesPerSecond(bytes int, ns int64) float64 {
	if ns <= 0 {
		return 0
	}
	const megabyte = 1024 * 1024
	return (float64(bytes) / megabyte) * 1e9 / float64(ns)
}

func makeBenchmarkIVF(width int, height int, fps int, packets [][]byte) []byte {
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
	return ivf
}

func referenceQualityMetrics(ivf []byte, frames []libgopx.Image) (float64, float64, int, error) {
	dec, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(ivf) < fileHeaderSize || string(ivf[:4]) != "DKIF" {
		return 0, 0, 0, errors.New("invalid IVF header")
	}
	offset := fileHeaderSize
	psnrSum := 0.0
	ssimSum := 0.0
	qualityFrames := 0
	for frameIndex := 0; offset < len(ivf); frameIndex++ {
		if offset+frameHeaderSize > len(ivf) {
			return 0, 0, qualityFrames, errors.New("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		timestamp := binary.LittleEndian.Uint64(ivf[offset+4:])
		offset += frameHeaderSize
		if size < 0 || offset+size > len(ivf) {
			return 0, 0, qualityFrames, errors.New("truncated IVF frame payload")
		}
		packet := ivf[offset : offset+size]
		offset += size
		if err := dec.Decode(packet); err != nil {
			return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, fmt.Errorf("decode reference frame %d: %w", frameIndex, err))
		}
		decoded, ok := dec.NextFrame()
		if !ok {
			continue
		}
		sourceIndex := frameIndex
		if timestamp < uint64(len(frames)) {
			sourceIndex = int(timestamp)
		}
		if sourceIndex >= len(frames) {
			continue
		}
		source := frames[sourceIndex]
		psnrSum += imagePSNR(source, decoded)
		ssimSum += imageSSIM(source, decoded)
		qualityFrames++
	}
	return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, nil)
}

func averageReferenceQuality(psnrSum float64, ssimSum float64, count int, err error) (float64, float64, int, error) {
	if count == 0 {
		return 0, 0, 0, err
	}
	return psnrSum / float64(count), ssimSum / float64(count), count, err
}

func writeI420Frame(dst *os.File, frame libgopx.Image) error {
	if err := writePlane(dst, frame.Y, frame.YStride, frame.Width, frame.Height); err != nil {
		return err
	}
	uvWidth := (frame.Width + 1) >> 1
	uvHeight := (frame.Height + 1) >> 1
	if err := writePlane(dst, frame.U, frame.UStride, uvWidth, uvHeight); err != nil {
		return err
	}
	return writePlane(dst, frame.V, frame.VStride, uvWidth, uvHeight)
}

func writePlane(dst *os.File, plane []byte, stride int, width int, height int) error {
	for row := 0; row < height; row++ {
		if _, err := dst.Write(plane[row*stride : row*stride+width]); err != nil {
			return err
		}
	}
	return nil
}

func parseIVFFrameSizes(ivf []byte) ([]int, error) {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(ivf) < fileHeaderSize || string(ivf[:4]) != "DKIF" {
		return nil, errors.New("invalid IVF header")
	}
	offset := fileHeaderSize
	var sizes []int
	for offset < len(ivf) {
		if offset+frameHeaderSize > len(ivf) {
			return nil, errors.New("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		offset += frameHeaderSize
		if size < 0 || offset+size > len(ivf) {
			return nil, errors.New("truncated IVF frame payload")
		}
		sizes = append(sizes, size)
		offset += size
	}
	return sizes, nil
}

func imagePSNR(src libgopx.Image, dst libgopx.Image) float64 {
	sse, count := planeSSE(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSE, uCount := planeSSE(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSE, vCount := planeSSE(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	sse += uSSE + vSSE
	count += uCount + vCount
	if sse == 0 {
		return 100
	}
	mse := float64(sse) / float64(count)
	return 10 * math.Log10((255*255)/mse)
}

func imageSSIM(src libgopx.Image, dst libgopx.Image) float64 {
	ssim, count := planeSSIM(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSIM, uCount := planeSSIM(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSIM, vCount := planeSSIM(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	total := count + uCount + vCount
	if total == 0 {
		return 0
	}
	return (ssim*float64(count) + uSSIM*float64(uCount) + vSSIM*float64(vCount)) / float64(total)
}

func planeSSIM(a []byte, aStride int, b []byte, bStride int, width int, height int) (float64, int) {
	count := width * height
	if count <= 0 {
		return 0, 0
	}
	sumA := 0.0
	sumB := 0.0
	sumAA := 0.0
	sumBB := 0.0
	sumAB := 0.0
	for row := 0; row < height; row++ {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := 0; col < width; col++ {
			x := float64(aRow[col])
			y := float64(bRow[col])
			sumA += x
			sumB += y
			sumAA += x * x
			sumBB += y * y
			sumAB += x * y
		}
	}
	n := float64(count)
	meanA := sumA / n
	meanB := sumB / n
	varA := sumAA/n - meanA*meanA
	varB := sumBB/n - meanB*meanB
	cov := sumAB/n - meanA*meanB
	const (
		c1 = 6.5025
		c2 = 58.5225
	)
	num := (2*meanA*meanB + c1) * (2*cov + c2)
	den := (meanA*meanA + meanB*meanB + c1) * (varA + varB + c2)
	if den == 0 {
		return 1, count
	}
	return num / den, count
}

func planeSSE(a []byte, aStride int, b []byte, bStride int, width int, height int) (uint64, int) {
	var sse uint64
	for row := 0; row < height; row++ {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := 0; col < width; col++ {
			diff := int(aRow[col]) - int(bRow[col])
			sse += uint64(diff * diff)
		}
	}
	return sse, width * height
}

func percentileLatency(latencies []int64, percentile int) int64 {
	if len(latencies) == 0 {
		return 0
	}
	sorted := append([]int64(nil), latencies...)
	sort.Slice(sorted, func(i int, j int) bool { return sorted[i] < sorted[j] })
	index := (len(sorted)*percentile + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}
