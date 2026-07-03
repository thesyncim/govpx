package benchcmd

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"time"

	govpx "github.com/thesyncim/govpx"
)

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
	if cfg.PhaseTiming && !phaseTimingEnabled {
		return benchReport{}, errors.New("phase timing requires the govpx_phase_stats build tag")
	}

	frames := make([]govpx.Image, cfg.Frames)
	for i := range frames {
		frames[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
	}

	encoderOpts := benchmarkEncoderOptions(cfg, deadline)
	var phaseStats phaseStatsState
	phaseStats.configure(&encoderOpts, cfg.PhaseTiming)
	enc, err := govpx.NewVP8Encoder(encoderOpts)
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
	phaseStats.reset()
	encodeMallocs, err := sampleVP8EncodeMallocs(enc, packet, frames)
	if err != nil {
		return benchReport{}, err
	}
	enc.Reset()
	phaseStats.reset()
	runtime.GC()
	var measuredPackets []measuredEncodePacket
	if !cfg.SkipQuality {
		measuredPackets = make([]measuredEncodePacket, cfg.Frames)
	}
	measuredPacketCount := 0
	stopCPUProfile, err := startBenchmarkCPUProfile(cfg.CPUProfile)
	if err != nil {
		return benchReport{}, err
	}
	defer stopCPUProfile()
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
		if !cfg.SkipQuality {
			packetCopy := append([]byte(nil), result.Data...)
			measuredPackets[measuredPacketCount] = measuredEncodePacket{
				data:        packetCopy,
				sourceIndex: i,
			}
			measuredPacketCount++
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
	stopCPUProfile()
	psnr := 0.0
	ssim := 0.0
	qualityFrames := 0
	if !cfg.SkipQuality {
		psnr, ssim, qualityFrames, err = measuredEncodeQualityMetrics(measuredPackets[:measuredPacketCount], frames)
		if err != nil {
			return benchReport{}, err
		}
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
		Codec:             codecVP8,
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
		AllocsPerFrame:    float64(encodeMallocs) / float64(cfg.Frames),
		PSNR:              psnr,
		SSIM:              ssim,
		QualityFrames:     qualityFrames,
		QualitySkipped:    cfg.SkipQuality,
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
		Options:       benchSummary(deadlineName),
	}
	if stats := phaseStats.report(); stats != nil {
		report.PhaseNS = stats
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

func sampleVP8EncodeMallocs(enc *govpx.VP8Encoder, packet []byte, frames []govpx.Image) (uint64, error) {
	const samplePasses = 5
	best := ^uint64(0)
	for pass := 0; pass < samplePasses; pass++ {
		enc.Reset()
		runtime.GC()
		var memBefore runtime.MemStats
		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memBefore)
		for i, frame := range frames {
			if _, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0); err != nil {
				return 0, fmt.Errorf("vp8 allocation sample frame %d: %w", i, err)
			}
		}
		runtime.ReadMemStats(&memAfter)
		if mallocs := memAfter.Mallocs - memBefore.Mallocs; mallocs < best {
			best = mallocs
			if best == 0 {
				break
			}
		}
	}
	return best, nil
}

// measuredEncodeQualityMetrics decodes measured govpx packets and compares
// each visible output against its source frame.
func measuredEncodeQualityMetrics(packets []measuredEncodePacket, frames []govpx.Image) (float64, float64, int, error) {
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	psnrSum := 0.0
	ssimSum := 0.0
	qualityFrames := 0
	for packetIndex, packet := range packets {
		if packet.sourceIndex < 0 || packet.sourceIndex >= len(frames) {
			continue
		}
		if err := dec.Decode(packet.data); err != nil {
			return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, fmt.Errorf("decode measured frame %d: %w", packetIndex, err))
		}
		decoded, ok := dec.NextFrame()
		if !ok {
			continue
		}
		frame := frames[packet.sourceIndex]
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
