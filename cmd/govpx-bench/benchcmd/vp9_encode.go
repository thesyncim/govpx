// VP9 encode benchmark path: mirrors runBenchmark for VP9 profile 0 by
// driving govpx.VP9Encoder and (optionally) libvpx vpxenc-vp9 against the
// same source frames and comparing PSNR/SSIM through govpx.VP9Decoder.
package benchcmd

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"time"

	govpx "github.com/thesyncim/govpx"
)

// runVP9Benchmark runs the VP9 encode benchmark and (when a libvpx vpxenc-vp9
// is configured) the matched-parity reference encode plus quality comparison.
// It mirrors runBenchmark's reporting layout so downstream consumers can use
// the same comparisonReport / quality-gate plumbing across both codecs.
func runVP9Benchmark(cfg benchConfig) (benchReport, error) {
	return runVP9BenchmarkInternal(cfg, makeBenchmarkFrame)
}

// runVP9BenchmarkInternal is the shared implementation behind
// runVP9Benchmark and the fixture-driven runVP9BenchmarkWithSource path.
// The source function is invoked with (width, height, frameIndex) and must
// return a govpx.Image with the visible dimensions matching cfg.Width /
// cfg.Height.
func runVP9BenchmarkInternal(cfg benchConfig, source func(int, int, int) govpx.Image) (benchReport, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Frames <= 0 || cfg.FPS <= 0 || cfg.BitrateKbps <= 0 {
		return benchReport{}, errors.New("width, height, frames, fps, and bitrate must be positive")
	}
	// VP9 profile-0 max coded dimension is 65535 in each axis; bench
	// runs typically stay <= 4K so the same lenient sanity check used by
	// the VP8 path is sufficient here.
	if cfg.Width > 65535 || cfg.Height > 65535 {
		return benchReport{}, errors.New("dimensions exceed VP9 limits")
	}
	if source == nil {
		source = makeBenchmarkFrame
	}
	deadline, deadlineName, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		return benchReport{}, err
	}

	frames := make([]govpx.Image, cfg.Frames)
	ycbcr := make([]*image.YCbCr, cfg.Frames)
	for i := range frames {
		frames[i] = source(cfg.Width, cfg.Height, i)
		ycbcr[i] = imageToYCbCr(frames[i])
	}

	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))

	// Warmup pass with a throwaway encoder; VP9Encoder has no Reset, so a
	// fresh encoder is the cleanest way to prime caches without polluting
	// the measured pass.
	{
		warm, err := newVP9BenchmarkEncoder(cfg, deadline)
		if err != nil {
			return benchReport{}, err
		}
		for i := range frames {
			if _, err := warm.EncodeIntoWithResult(ycbcr[i], packet); err != nil {
				warm.Close()
				return benchReport{}, fmt.Errorf("vp9 warmup encode frame %d: %w", i, err)
			}
		}
		warm.Close()
	}

	enc, err := newVP9BenchmarkEncoder(cfg, deadline)
	if err != nil {
		return benchReport{}, err
	}
	defer enc.Close()

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

	stopCPUProfile, err := startBenchmarkCPUProfile(cfg.CPUProfile)
	if err != nil {
		return benchReport{}, err
	}
	defer stopCPUProfile()

	measuredPackets := make([]measuredEncodePacket, 0, cfg.Frames)
	encodeMallocs := uint64(0)
	runtime.GC()
	for i := range frames {
		var memBefore runtime.MemStats
		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memBefore)
		start := time.Now()
		result, err := enc.EncodeIntoWithResult(ycbcr[i], packet)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&memAfter)
		if err != nil {
			return benchReport{}, fmt.Errorf("vp9 encode frame %d: %w", i, err)
		}
		encodeMallocs += memAfter.Mallocs - memBefore.Mallocs
		latencies = append(latencies, elapsed.Nanoseconds())
		if result.Dropped {
			droppedFrames++
			continue
		}
		if len(result.Data) == 0 {
			// Hidden / buffered packet -- treat as no-output for size
			// accounting but keep the latency sample.
			continue
		}
		packetCopy := append([]byte(nil), result.Data...)
		measuredPackets = append(measuredPackets, measuredEncodePacket{
			data:        packetCopy,
			sourceIndex: i,
		})
		encodedFrames++
		outputBytes += result.SizeBytes
		// VP9 reports the public 0..63 quantizer in Quantizer; mirror that
		// into the histogram bucket so downstream JSON/text reports look
		// the same as VP8.
		q := result.Quantizer
		quantSum += q
		if quantMin == 0 || q < quantMin {
			quantMin = q
		}
		if q > quantMax {
			quantMax = q
		}
		if q >= 0 && q < len(quantHist) {
			quantHist[q]++
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
		psnr, ssim, qualityFrames, err = measuredVP9EncodeQualityMetrics(measuredPackets, frames, cfg.Width, cfg.Height)
		if err != nil {
			return benchReport{}, err
		}
	}

	totalLatency := int64(0)
	for _, ns := range latencies {
		totalLatency += ns
	}
	denom := int64(len(latencies))
	if denom <= 0 {
		denom = 1
	}
	nsPerFrame := totalLatency / denom
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
	encodeFPS := 0.0
	mbps := 0.0
	if nsPerFrame > 0 {
		encodeFPS = 1e9 / float64(nsPerFrame)
		mbps = macroblocksPerFrame * 1e9 / float64(nsPerFrame)
	}

	report := benchReport{
		Codec:             codecVP9,
		Encoder:           "govpx-vp9",
		Mode:              deadlineName,
		Width:             cfg.Width,
		Height:            cfg.Height,
		Frames:            cfg.Frames,
		FPS:               cfg.FPS,
		TargetBitrateKbps: cfg.BitrateKbps,
		OutputBitrateKbps: outputBitrate,
		BitrateErrorPct:   bitrateError,
		NSPerFrame:        nsPerFrame,
		EncodeFPS:         encodeFPS,
		MacroblocksPerSec: mbps,
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
	if cfg.LibvpxVpxencVP9 != "" {
		reference, err := runLibvpxVP9Benchmark(cfg, frames, deadlineName)
		if err != nil {
			return benchReport{}, err
		}
		report.Reference = &reference
		report.Comparison = buildComparisonReport(report, reference)
	}
	return report, nil
}

// newVP9BenchmarkEncoder builds a VP9 encoder that mirrors the VP8 bench
// parity model (CBR, matched quantizer / buffer / drop parameters) so the
// govpx-vs-libvpx comparison stays apples-to-apples.
func newVP9BenchmarkEncoder(cfg benchConfig, deadline govpx.Deadline) (*govpx.VP9Encoder, error) {
	return govpx.NewVP9Encoder(vp9BenchmarkEncoderOptions(cfg, deadline))
}

func vp9BenchmarkEncoderOptions(cfg benchConfig, deadline govpx.Deadline) govpx.VP9EncoderOptions {
	p := parityFor(cfg)
	cpuUsed := p.CpuUsed
	// VP9 normalizes negative cpu-used through SetDeadline-equivalent
	// abs(). Clamp to the int8 field type.
	if cpuUsed > 9 {
		cpuUsed = 9
	} else if cpuUsed < -9 {
		cpuUsed = -9
	}
	opts := govpx.VP9EncoderOptions{
		Width:               cfg.Width,
		Height:              cfg.Height,
		FPS:                 cfg.FPS,
		Threads:             p.Threads,
		Deadline:            deadline,
		CpuUsed:             int8(cpuUsed),
		TargetBitrateKbps:   cfg.BitrateKbps,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		MinQuantizer:        p.MinQuantizer,
		MaxQuantizer:        p.MaxQuantizer,
		MaxKeyframeInterval: p.KeyFrameInterval,
		BufferSizeMs:        p.BufferSizeMs,
		BufferInitialSizeMs: p.BufferInitialSizeMs,
		BufferOptimalSizeMs: p.BufferOptimalSizeMs,
		UndershootPct:       p.UndershootPct,
		OvershootPct:        p.OvershootPct,
		MaxIntraBitratePct:  p.MaxIntraBitratePct,
		DropFrameAllowed:    p.DropFrameAllowed,
		DropFrameWaterMark:  p.DropFrameWaterMark,
		NoiseSensitivity:    int8(p.NoiseSensitivity),
		StaticThreshold:     p.StaticThreshold,
	}
	return opts
}

// imageToYCbCr wraps a govpx.Image as an *image.YCbCr that the VP9 encoder
// consumes. The plane slices alias the source -- the encoder copies what it
// needs into its scratch state so callers can keep mutating the input.
func imageToYCbCr(img govpx.Image) *image.YCbCr {
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	_ = uvHeight
	return &image.YCbCr{
		Y:              img.Y,
		Cb:             img.U,
		Cr:             img.V,
		YStride:        img.YStride,
		CStride:        uvWidth,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, img.Width, img.Height),
	}
}

// measuredVP9EncodeQualityMetrics decodes each measured govpx VP9 packet and
// computes PSNR / SSIM against the original source frame. Width/height drive
// the decoder destination plane sizing.
func measuredVP9EncodeQualityMetrics(packets []measuredEncodePacket, frames []govpx.Image, width int, height int) (float64, float64, int, error) {
	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	defer dec.Close()
	dst := newImageBuffer(width, height)
	psnrSum := 0.0
	ssimSum := 0.0
	qualityFrames := 0
	for packetIndex, packet := range packets {
		if packet.sourceIndex < 0 || packet.sourceIndex >= len(frames) {
			continue
		}
		info, err := dec.DecodeInto(packet.data, &dst)
		if err != nil {
			return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, fmt.Errorf("decode measured vp9 frame %d: %w", packetIndex, err))
		}
		if !info.ShowFrame {
			continue
		}
		frame := frames[packet.sourceIndex]
		psnrSum += imagePSNR(frame, dst)
		ssimSum += imageSSIM(frame, dst)
		qualityFrames++
	}
	return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, nil)
}

// newImageBuffer allocates a fresh govpx.Image sized for the given visible
// dimensions, with plane strides equal to the visible width.
func newImageBuffer(width int, height int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}
