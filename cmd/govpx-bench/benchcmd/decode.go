package benchcmd

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	govpx "github.com/thesyncim/govpx"
)

func runDecodeBenchmark(cfg benchConfig) (decodeBenchReport, error) {
	codec := benchCodec(cfg)
	deadline, deadlineName, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		return decodeBenchReport{}, err
	}
	var (
		packets [][]byte
		ivf     []byte
	)
	if cfg.InputPath != "" {
		stream, err := loadIVFInput(cfg.InputPath, cfg.Frames)
		if err != nil {
			return decodeBenchReport{}, err
		}
		codec = stream.codec
		cfg.Codec = codec
		cfg.Width = stream.width
		cfg.Height = stream.height
		cfg.FPS = stream.fps
		cfg.Frames = len(stream.packets)
		packets = stream.packets
		ivf = stream.raw
	} else {
		if err := validateDecodeBenchmarkConfig(cfg, codec); err != nil {
			return decodeBenchReport{}, err
		}
		frames, err := prepareEncodeFrames(&cfg, nil)
		if err != nil {
			return decodeBenchReport{}, err
		}
		// Disable frame dropping when synthesizing the decode corpus unless
		// the caller explicitly requested -drop-frame, so -frames N decodes N frames.
		encCfg := cfg
		if !encCfg.DropFrameSet {
			encCfg.DropFrameSet = true
			encCfg.DropFrame = 0
		}
		packets, err = encodeDecodeBenchmarkPackets(encCfg, deadline, frames, codec)
		if err != nil {
			return decodeBenchReport{}, err
		}
		ivf = makeBenchmarkIVFForCodec(codec, cfg.Width, cfg.Height, cfg.FPS, packets)
	}
	dec, err := newBenchmarkDecoder(codec, cfg)
	if err != nil {
		return decodeBenchReport{}, err
	}
	defer closeBenchmarkDecoder(dec)
	if _, _, err := decodeBenchmarkPackets(dec, packets, make([]int64, 0, len(packets))); err != nil {
		return decodeBenchReport{}, err
	}
	runtime.GC()
	if _, _, err := decodeBenchmarkPackets(dec, packets, make([]int64, 0, len(packets))); err != nil {
		return decodeBenchReport{}, err
	}

	runs := max(cfg.Runs, 1)
	govpxSamples := make([]int64, 0, runs)
	refSamples := make([]int64, 0, runs)
	var lastReport decodeBenchReport
	var lastRef *decodeReferenceReport

	for runIdx := range runs {
		runCfg := cfg
		if runs > 1 {
			runCfg.ReferenceFirst = runIdx%2 == 1
			runCfg.libvpxWarmed = runIdx > 0
		}
		var runRef *decodeReferenceReport
		if runCfg.ReferenceFirst && runCfg.LibvpxOracle != "" {
			reference, err := runLibvpxDecodeBenchmark(runCfg, ivf, deadlineName, len(packets))
			if err != nil {
				return decodeBenchReport{}, err
			}
			runRef = &reference
		}

		latencies := make([]int64, 0, len(packets))
		var memBefore runtime.MemStats
		var memAfter runtime.MemStats
		var decodeMallocs uint64
		if runCfg.CPUProfile != "" && runIdx == 0 {
			runtime.GC()
			runtime.ReadMemStats(&memBefore)
			if _, _, err := decodeBenchmarkPackets(dec, packets, latencies[:0]); err != nil {
				return decodeBenchReport{}, err
			}
			runtime.ReadMemStats(&memAfter)
			decodeMallocs = memAfter.Mallocs - memBefore.Mallocs
			latencies = latencies[:0]
		}
		profilePath := ""
		if runIdx == 0 {
			profilePath = runCfg.CPUProfile
		}
		stopCPUProfile, err := startBenchmarkCPUProfile(profilePath)
		if err != nil {
			return decodeBenchReport{}, err
		}
		if profilePath == "" {
			runtime.ReadMemStats(&memBefore)
		}
		decodedFrames, latencies, err := decodeBenchmarkPackets(dec, packets, latencies)
		stopCPUProfile()
		if profilePath == "" {
			runtime.ReadMemStats(&memAfter)
			decodeMallocs = memAfter.Mallocs - memBefore.Mallocs
		}
		if err != nil {
			return decodeBenchReport{}, err
		}
		totalLatency := totalLatencyNS(latencies)
		nsPerFrame := totalLatency / int64(len(latencies))
		macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
		lastReport = decodeBenchReport{
			Codec:                codec,
			Decoder:              decodeReportDecoderName(codec),
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
		govpxSamples = append(govpxSamples, nsPerFrame)

		if runCfg.LibvpxOracle != "" {
			if runRef == nil {
				reference, err := runLibvpxDecodeBenchmark(runCfg, ivf, deadlineName, len(packets))
				if err != nil {
					return decodeBenchReport{}, err
				}
				runRef = &reference
			}
			lastRef = runRef
			refSamples = append(refSamples, runRef.NSPerFrame)
		}
	}

	if runs > 1 {
		med, minNS, maxNS := medianMinMaxInt64(govpxSamples)
		lastReport.Runs = runs
		lastReport.NSPerFrame = med
		lastReport.MinNSPerFrame = minNS
		lastReport.MaxNSPerFrame = maxNS
		lastReport.DecodeFPS = 1e9 / float64(med)
		lastReport.MacroblocksPerSec = benchmarkMacroblocks(cfg.Width, cfg.Height) * 1e9 / float64(med)
		lastReport.CodedMegabytesPerSec = codedMegabytesPerSecond(len(ivf), med*int64(len(packets)))
	}
	if lastRef != nil {
		if runs > 1 && len(refSamples) > 0 {
			med, minNS, maxNS := medianMinMaxInt64(refSamples)
			lastRef.NSPerFrame = med
			lastRef.MinNSPerFrame = minNS
			lastRef.MaxNSPerFrame = maxNS
			lastRef.DecodeFPS = 1e9 / float64(med)
			lastRef.MacroblocksPerSec = benchmarkMacroblocks(cfg.Width, cfg.Height) * 1e9 / float64(med)
			lastRef.CodedMegabytesPerSec = codedMegabytesPerSecond(len(ivf), med*int64(len(packets)))
		}
		lastReport.Reference = lastRef
		lastReport.Comparison = buildDecodeComparisonReport(lastReport, *lastRef)
		if lastReport.NSPerFrame > 0 {
			lastReport.RelativeSpeedVsReference = float64(lastRef.NSPerFrame) / float64(lastReport.NSPerFrame)
		}
	}
	lastReport.FairnessWarnings = collectDecodeFairnessWarnings(cfg, lastReport)
	return lastReport, nil
}

func validateDecodeBenchmarkConfig(cfg benchConfig, codec string) error {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Frames <= 0 || cfg.FPS <= 0 || cfg.BitrateKbps <= 0 {
		return errors.New("width, height, frames, fps, and bitrate must be positive")
	}
	switch codec {
	case codecVP9:
		if cfg.Width > 65535 || cfg.Height > 65535 {
			return errors.New("dimensions exceed VP9 limits")
		}
	default:
		if cfg.Width > 16383 || cfg.Height > 16383 {
			return errors.New("dimensions exceed VP8 limits")
		}
	}
	return nil
}

func encodeDecodeBenchmarkPackets(cfg benchConfig, deadline govpx.Deadline, frames []govpx.Image, codec string) ([][]byte, error) {
	if codec == codecVP9 {
		return encodeVP9DecodeBenchmarkPackets(cfg, deadline, frames)
	}
	return encodeBenchmarkPackets(cfg, deadline, frames)
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

func encodeVP9DecodeBenchmarkPackets(cfg benchConfig, deadline govpx.Deadline, frames []govpx.Image) ([][]byte, error) {
	enc, err := newVP9BenchmarkEncoder(cfg, deadline)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	packets := make([][]byte, 0, len(frames))
	for i, frame := range frames {
		result, err := enc.EncodeIntoWithResult(imageToYCbCr(frame), packet)
		if err != nil {
			return nil, fmt.Errorf("vp9 encode frame %d: %w", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	if len(packets) == 0 {
		return nil, errors.New("benchmark encoder emitted no VP9 packets")
	}
	return packets, nil
}

type benchmarkDecoder interface {
	Reset()
	Decode([]byte) error
	NextFrame() (govpx.Image, bool)
}

func newBenchmarkDecoder(codec string, cfg benchConfig) (benchmarkDecoder, error) {
	threads := effectiveBenchThreads(cfg)
	if codec == codecVP9 {
		policy := vp9DecodeThreadPolicyForBench(cfg)
		return govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			Threads:              policy.Threads,
			DecoderRowMT:         policy.RowMT,
			DecoderLoopFilterOpt: policy.LoopFilterOpt,
		})
	}
	return govpx.NewVP8Decoder(govpx.DecoderOptions{Threads: threads})
}

type vp9DecodeThreadPolicy struct {
	Threads       int
	RowMT         bool
	LoopFilterOpt bool
}

func vp9DecodeThreadPolicyForBench(cfg benchConfig) vp9DecodeThreadPolicy {
	threads := effectiveBenchThreads(cfg)
	return vp9DecodeThreadPolicy{
		Threads:       threads,
		RowMT:         false,
		LoopFilterOpt: threads > 1,
	}
}

func closeBenchmarkDecoder(dec benchmarkDecoder) {
	if c, ok := dec.(io.Closer); ok {
		_ = c.Close()
	}
}

func decodeReportDecoderName(codec string) string {
	if codec == codecVP9 {
		return "govpx-vp9"
	}
	return "govpx"
}

func decodeBenchmarkPackets(dec benchmarkDecoder, packets [][]byte, latencies []int64) (int, []int64, error) {
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
