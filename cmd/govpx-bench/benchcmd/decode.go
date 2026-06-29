package benchcmd

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	govpx "github.com/thesyncim/govpx"
)

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
	var decodeMallocs uint64
	if cfg.CPUProfile != "" {
		runtime.GC()
		runtime.ReadMemStats(&memBefore)
		if _, _, err := decodeBenchmarkPackets(dec, packets, latencies[:0]); err != nil {
			return decodeBenchReport{}, err
		}
		runtime.ReadMemStats(&memAfter)
		decodeMallocs = memAfter.Mallocs - memBefore.Mallocs
		latencies = latencies[:0]
	}
	stopCPUProfile, err := startBenchmarkCPUProfile(cfg.CPUProfile)
	if err != nil {
		return decodeBenchReport{}, err
	}
	defer stopCPUProfile()
	if cfg.CPUProfile == "" {
		runtime.ReadMemStats(&memBefore)
	}
	decodedFrames, latencies, err := decodeBenchmarkPackets(dec, packets, latencies)
	stopCPUProfile()
	if cfg.CPUProfile == "" {
		runtime.ReadMemStats(&memAfter)
		decodeMallocs = memAfter.Mallocs - memBefore.Mallocs
	}
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
		AllocsPerFrame:       float64(decodeMallocs) / float64(len(packets)),
		LatencyNS: latencyReport{
			P50: percentileLatency(latencies, 50),
			P95: percentileLatency(latencies, 95),
			P99: percentileLatency(latencies, 99),
		},
		Options: benchSummary(deadlineName),
	}
	if cfg.LibvpxOracle != "" {
		reference, err := runLibvpxDecodeBenchmark(cfg, ivf, deadlineName, len(packets))
		if err != nil {
			return decodeBenchReport{}, err
		}
		report.Reference = &reference
		report.Comparison = buildDecodeComparisonReport(report, reference)
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
