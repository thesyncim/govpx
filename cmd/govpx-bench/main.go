package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	govpx "github.com/thesyncim/govpx"
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
	Comparison        *comparisonReport  `json:"comparison_vs_reference,omitempty"`
	Options           benchConfigSummary `json:"options"`
}

// comparisonReport summarizes how govpx compared against the libvpx
// reference encoder on the same input. It is populated only when a
// libvpx vpxenc binary is configured (via `-libvpx-vpxenc` or the
// `GOVPX_VPXENC` environment variable) so callers can read a single
// "did we beat libvpx?" snapshot without diffing the full reference
// block manually.
type comparisonReport struct {
	BitrateRatioVsReference float64 `json:"bitrate_ratio_vs_reference"`
	BitrateDeltaKbps        float64 `json:"bitrate_delta_kbps"`
	BitrateErrorPctDelta    float64 `json:"bitrate_error_pct_delta"`
	PSNRDeltaDB             float64 `json:"psnr_delta_db"`
	SSIMDelta               float64 `json:"ssim_delta"`
	EncodeFPSRatio          float64 `json:"encode_fps_ratio_vs_reference"`
	NSPerFrameRatio         float64 `json:"ns_per_frame_ratio_vs_reference"`
	OutputBytesRatio        float64 `json:"output_bytes_ratio_vs_reference"`
	AvgInterBytesRatio      float64 `json:"avg_interframe_bytes_ratio_vs_reference"`
	KeyframeBytesRatio      float64 `json:"keyframe_bytes_ratio_vs_reference"`
}

type referenceReport struct {
	Encoder              string        `json:"encoder"`
	Mode                 string        `json:"mode"`
	OutputBitrateKbps    float64       `json:"output_bitrate_kbps"`
	BitrateErrorPct      float64       `json:"bitrate_error_pct"`
	NSPerFrame           int64         `json:"ns_per_frame"`
	EncodeFPS            float64       `json:"encode_fps"`
	MacroblocksPerSec    float64       `json:"macroblocks_per_second"`
	PSNR                 float64       `json:"psnr"`
	SSIM                 float64       `json:"ssim"`
	QualityFrames        int           `json:"quality_frames"`
	QualityError         string        `json:"quality_error,omitempty"`
	KeyframeBytes        int           `json:"keyframe_bytes"`
	AvgInterBytes        float64       `json:"avg_interframe_bytes"`
	LatencyNS            latencyReport `json:"latency_ns"`
	OutputBytes          int           `json:"output_bytes"`
	EncodedFrames        int           `json:"encoded_frames"`
	TimingSource         string        `json:"timing_source"`
	WallNSPerFrame       int64         `json:"wall_ns_per_frame"`
	WallEncodeFPS        float64       `json:"wall_encode_fps"`
	SubprocessOverheadNS int64         `json:"subprocess_overhead_ns"`
	ParityFlags          []string      `json:"parity_flags,omitempty"`
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
	autoCompare := false
	cpuProfile := ""
	memProfile := ""
	flag.IntVar(&cfg.Width, "width", 64, "frame width")
	flag.IntVar(&cfg.Height, "height", 64, "frame height")
	flag.IntVar(&cfg.Frames, "frames", 30, "number of frames")
	flag.IntVar(&cfg.FPS, "fps", 30, "frame rate")
	flag.IntVar(&cfg.BitrateKbps, "bitrate", 1200, "target bitrate in kbps")
	flag.StringVar(&cfg.Mode, "mode", "realtime", "encoder mode: realtime or good")
	flag.BoolVar(&cfg.Decode, "decode", false, "run decoder benchmark mode")
	flag.StringVar(&cfg.LibvpxVpxenc, "libvpx-vpxenc", os.Getenv("GOVPX_VPXENC"), "optional libvpx vpxenc path for reference comparison")
	flag.StringVar(&cfg.LibvpxOracle, "libvpx-oracle", os.Getenv("GOVPX_ORACLE"), "optional libvpx checksum oracle path for decoder reference timing")
	flag.BoolVar(&autoCompare, "auto-libvpx", true, "auto-locate vpxenc/vpxdec in PATH when -libvpx-vpxenc/-libvpx-oracle are unset, for an automatic libvpx comparison")
	flag.StringVar(&cpuProfile, "cpuprofile", "", "write a CPU pprof profile of the measured encode/decode pass to this file")
	flag.StringVar(&memProfile, "memprofile", "", "write a heap pprof profile after the measured pass to this file")
	flag.Parse()
	if autoCompare {
		if cfg.LibvpxVpxenc == "" {
			if path, err := exec.LookPath("vpxenc"); err == nil {
				cfg.LibvpxVpxenc = path
			}
		}
		if cfg.LibvpxOracle == "" {
			// The decoder benchmark expects the project's checksum oracle
			// helper (govpx-vpx-oracle), not vpxdec, so do not auto-fill
			// from PATH here. Auto-detection would silently fall back to
			// the wrong binary. The user must opt in explicitly.
			_ = cfg.LibvpxOracle
		}
	}

	if cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: create cpu profile: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: start cpu profile: %v\n", err)
			os.Exit(2)
		}
		defer pprof.StopCPUProfile()
	}

	var report any
	var err error
	if cfg.Decode {
		report, err = runDecodeBenchmark(cfg)
	} else {
		report, err = runBenchmark(cfg)
	}
	if memProfile != "" {
		f, err := os.Create(memProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "govpx-bench: create mem profile: %v\n", err)
			os.Exit(2)
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "govpx-bench: write mem profile: %v\n", err)
			os.Exit(2)
		}
		f.Close()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "govpx-bench: %v\n", err)
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "govpx-bench: encode json: %v\n", err)
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

	frames := make([]govpx.Image, cfg.Frames)
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
		Encoder:           "govpx",
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
		report.Comparison = buildComparisonReport(report, reference)
	}
	return report, nil
}

// buildComparisonReport derives govpx-vs-libvpx ratios and deltas from a
// completed govpx benchReport plus its libvpx referenceReport. Ratios are
// govpx/libvpx; positive deltas mean govpx is higher than libvpx.
func buildComparisonReport(report benchReport, reference referenceReport) *comparisonReport {
	cmp := &comparisonReport{
		BitrateDeltaKbps:     report.OutputBitrateKbps - reference.OutputBitrateKbps,
		BitrateErrorPctDelta: report.BitrateErrorPct - reference.BitrateErrorPct,
		PSNRDeltaDB:          report.PSNR - reference.PSNR,
		SSIMDelta:            report.SSIM - reference.SSIM,
	}
	if reference.OutputBitrateKbps > 0 {
		cmp.BitrateRatioVsReference = report.OutputBitrateKbps / reference.OutputBitrateKbps
	}
	if reference.NSPerFrame > 0 {
		cmp.NSPerFrameRatio = float64(report.NSPerFrame) / float64(reference.NSPerFrame)
	}
	if reference.EncodeFPS > 0 {
		cmp.EncodeFPSRatio = report.EncodeFPS / reference.EncodeFPS
	}
	if reference.OutputBytes > 0 {
		cmp.OutputBytesRatio = float64(report.OutputBytes) / float64(reference.OutputBytes)
	}
	if reference.AvgInterBytes > 0 {
		cmp.AvgInterBytesRatio = report.AvgInterBytes / reference.AvgInterBytes
	}
	if reference.KeyframeBytes > 0 {
		cmp.KeyframeBytesRatio = float64(report.KeyframeBytes) / float64(reference.KeyframeBytes)
	}
	return cmp
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
	frames := make([]govpx.Image, cfg.Frames)
	for i := range frames {
		frames[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
	}
	packets, err := encodeBenchmarkPackets(cfg, deadline, frames)
	if err != nil {
		return decodeBenchReport{}, err
	}
	ivf := makeBenchmarkIVF(cfg.Width, cfg.Height, cfg.FPS, packets)
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
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
		Decoder:              "govpx",
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

func encodeBenchmarkPackets(cfg benchConfig, deadline govpx.Deadline, frames []govpx.Image) ([][]byte, error) {
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

func decodeBenchmarkPackets(dec *govpx.VP8Decoder, packets [][]byte, latencies []int64) (int, []int64, error) {
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

// encoderParity captures the rate-control knobs that have to match between
// govpx and libvpx for the comparison to be apples-to-apples. Both
// newBenchmarkEncoder and runLibvpxBenchmark consume this so the two encoders
// see the same problem (CBR, same buffer sizes, same q-range, same kf
// cadence, single-pass, single-thread, zero lag).
type encoderParity struct {
	MinQuantizer        int
	MaxQuantizer        int
	KeyFrameInterval    int
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	UndershootPct       int
	OvershootPct        int
	Threads             int
	CpuUsed             int
}

func parityFor(cfg benchConfig) encoderParity {
	kf := cfg.FPS
	if kf <= 0 {
		kf = 30
	}
	return encoderParity{
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    kf,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		UndershootPct:       100,
		OvershootPct:        15,
		Threads:             1,
		CpuUsed:             8,
	}
}

func newBenchmarkEncoder(cfg benchConfig, deadline govpx.Deadline) (*govpx.VP8Encoder, error) {
	p := parityFor(cfg)
	return govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               cfg.Width,
		Height:              cfg.Height,
		FPS:                 cfg.FPS,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   cfg.BitrateKbps,
		MinQuantizer:        p.MinQuantizer,
		MaxQuantizer:        p.MaxQuantizer,
		Deadline:            deadline,
		CpuUsed:             p.CpuUsed,
		KeyFrameInterval:    p.KeyFrameInterval,
		BufferSizeMs:        p.BufferSizeMs,
		BufferInitialSizeMs: p.BufferInitialSizeMs,
		BufferOptimalSizeMs: p.BufferOptimalSizeMs,
	})
}

func benchmarkQualityMetrics(cfg benchConfig, deadline govpx.Deadline, frames []govpx.Image) (float64, float64, int, error) {
	enc, err := newBenchmarkEncoder(cfg, deadline)
	if err != nil {
		return 0, 0, 0, err
	}
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
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

func benchmarkDeadline(mode string) (govpx.Deadline, string, error) {
	switch mode {
	case "", "realtime":
		return govpx.DeadlineRealtime, "realtime", nil
	case "good":
		return govpx.DeadlineGoodQuality, "good", nil
	default:
		return 0, "", fmt.Errorf("unsupported mode %q", mode)
	}
}

func makeBenchmarkFrame(width int, height int, index int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := govpx.Image{
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

func runLibvpxBenchmark(cfg benchConfig, frames []govpx.Image, deadlineName string) (referenceReport, error) {
	tempDir, err := os.MkdirTemp("", "govpx-bench-*")
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
	parity := parityFor(cfg)
	parityFlags := libvpxParityFlags(cfg, parity, vpxDeadlineFlag)
	args := append([]string{
		"--codec=vp8",
		"--ivf",
		"--i420",
		fmt.Sprintf("--width=%d", cfg.Width),
		fmt.Sprintf("--height=%d", cfg.Height),
		fmt.Sprintf("--fps=%d/1", cfg.FPS),
		fmt.Sprintf("--limit=%d", cfg.Frames),
	}, parityFlags...)
	// User overrides come after parity defaults so the same-flag-wins
	// behaviour of vpxenc lets callers tweak rate control if they need to.
	args = append(args, cfg.LibvpxArgs...)
	args = append(args, fmt.Sprintf("--output=%s", outPath), rawPath)

	var stderr bytes.Buffer
	cmd := exec.Command(cfg.LibvpxVpxenc, args...)
	cmd.Stderr = &stderr
	start := time.Now()
	stdout, err := cmd.Output()
	elapsed := time.Since(start)
	if err != nil {
		return referenceReport{}, fmt.Errorf("libvpx vpxenc failed: %w\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr.Bytes())
	}

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
	wallNS := elapsed.Nanoseconds()
	wallPerFrame := wallNS / int64(len(frames))
	encodeNS := wallNS
	timingSource := "wall"
	if parsed, ok := parseVpxencEncodeTime(stderr.Bytes()); ok && parsed.frames > 0 && parsed.totalNS > 0 {
		encodeNS = parsed.totalNS
		timingSource = "vpxenc-stats"
	}
	encodePerFrame := encodeNS / int64(len(frames))
	if encodePerFrame <= 0 {
		// Fall back so downstream divisions stay positive.
		encodePerFrame = wallPerFrame
		encodeNS = wallNS
		timingSource = "wall"
	}
	overheadNS := wallNS - encodeNS
	if overheadNS < 0 {
		overheadNS = 0
	}
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
	wallFPS := 0.0
	if wallPerFrame > 0 {
		wallFPS = 1e9 / float64(wallPerFrame)
	}
	return referenceReport{
		Encoder:           "libvpx-vp8",
		Mode:              deadlineName,
		OutputBitrateKbps: outputBitrate,
		BitrateErrorPct:   bitrateError,
		NSPerFrame:        encodePerFrame,
		EncodeFPS:         1e9 / float64(encodePerFrame),
		MacroblocksPerSec: macroblocksPerFrame * 1e9 / float64(encodePerFrame),
		PSNR:              psnr,
		SSIM:              ssim,
		QualityFrames:     qualityFrames,
		QualityError:      qualityError,
		KeyframeBytes:     keyframeBytes,
		AvgInterBytes:     avgInter,
		LatencyNS: latencyReport{
			P50: encodePerFrame,
			P95: encodePerFrame,
			P99: encodePerFrame,
		},
		OutputBytes:          outputBytes,
		EncodedFrames:        len(sizes),
		TimingSource:         timingSource,
		WallNSPerFrame:       wallPerFrame,
		WallEncodeFPS:        wallFPS,
		SubprocessOverheadNS: overheadNS,
		ParityFlags:          parityFlags,
	}, nil
}

// libvpxParityFlags returns the vpxenc flags that mirror govpx's
// EncoderOptions for a fair benchmark: same CBR target and buffer model,
// same q-range and keyframe cadence, single-pass, single-thread, no lag,
// noise sensitivity off, deadline matched. The deadlineFlag is "--rt" or
// "--good" depending on benchConfig.Mode.
func libvpxParityFlags(cfg benchConfig, p encoderParity, deadlineFlag string) []string {
	return []string{
		"--passes=1",
		"--lag-in-frames=0",
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", cfg.BitrateKbps),
		fmt.Sprintf("--min-q=%d", p.MinQuantizer),
		fmt.Sprintf("--max-q=%d", p.MaxQuantizer),
		fmt.Sprintf("--kf-min-dist=%d", p.KeyFrameInterval),
		fmt.Sprintf("--kf-max-dist=%d", p.KeyFrameInterval),
		fmt.Sprintf("--buf-sz=%d", p.BufferSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", p.BufferInitialSizeMs),
		fmt.Sprintf("--buf-optimal-sz=%d", p.BufferOptimalSizeMs),
		fmt.Sprintf("--undershoot-pct=%d", p.UndershootPct),
		fmt.Sprintf("--overshoot-pct=%d", p.OvershootPct),
		fmt.Sprintf("--threads=%d", p.Threads),
		"--noise-sensitivity=0",
		deadlineFlag,
		fmt.Sprintf("--cpu-used=%d", p.CpuUsed),
	}
}

type vpxencProgress struct {
	frames  int
	bytes   int
	totalNS int64
}

// vpxenc prints (and updates with carriage returns) lines like
//
//	Pass 1/1 frame   30/30   12345B   123456 us 24.31 fps
//
// to stderr while encoding. The numeric column is microseconds for short
// runs and switches to milliseconds when the total exceeds ~10 seconds.
// We take the last match so we get the final cumulative tally rather than
// an intermediate update.
var vpxencProgressRE = regexp.MustCompile(`Pass\s+\d+/\d+\s+frame\s+(\d+)/(\d+)\s+(\d+)B\s+(\d+)\s+(us|ms)`)

func parseVpxencEncodeTime(stderr []byte) (vpxencProgress, bool) {
	matches := vpxencProgressRE.FindAllSubmatch(stderr, -1)
	if len(matches) == 0 {
		return vpxencProgress{}, false
	}
	last := matches[len(matches)-1]
	framesIn, _ := strconv.Atoi(string(last[1]))
	framesOut, _ := strconv.Atoi(string(last[2]))
	rawBytes, _ := strconv.Atoi(string(last[3]))
	rawTime, _ := strconv.ParseInt(string(last[4]), 10, 64)
	unit := string(last[5])
	frames := framesOut
	if frames == 0 {
		frames = framesIn
	}
	var ns int64
	switch unit {
	case "ms":
		ns = rawTime * int64(time.Millisecond)
	default:
		ns = rawTime * int64(time.Microsecond)
	}
	if frames <= 0 || ns <= 0 {
		return vpxencProgress{}, false
	}
	return vpxencProgress{frames: frames, bytes: rawBytes, totalNS: ns}, true
}

func runLibvpxDecodeBenchmark(cfg benchConfig, ivf []byte, deadlineName string, frames int) (decodeReferenceReport, error) {
	tempDir, err := os.MkdirTemp("", "govpx-decode-bench-*")
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

func referenceQualityMetrics(ivf []byte, frames []govpx.Image) (float64, float64, int, error) {
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
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

func writeI420Frame(dst *os.File, frame govpx.Image) error {
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

func imagePSNR(src govpx.Image, dst govpx.Image) float64 {
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

func imageSSIM(src govpx.Image, dst govpx.Image) float64 {
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
