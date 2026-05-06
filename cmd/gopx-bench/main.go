package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"time"

	libgopx "github.com/thesyncim/libgopx"
)

type benchConfig struct {
	Width       int
	Height      int
	Frames      int
	FPS         int
	BitrateKbps int
	Mode        string
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
	AllocsPerFrame    float64            `json:"allocs_per_frame"`
	PSNR              float64            `json:"psnr"`
	KeyframeBytes     int                `json:"keyframe_bytes"`
	AvgInterBytes     float64            `json:"avg_interframe_bytes"`
	Quantizers        quantizerReport    `json:"quantizers"`
	LatencyNS         latencyReport      `json:"latency_ns"`
	OutputBytes       int                `json:"output_bytes"`
	EncodedFrames     int                `json:"encoded_frames"`
	DroppedFrames     int                `json:"dropped_frames"`
	QuantizerHist     map[string]int     `json:"quantizer_histogram"`
	Options           benchConfigSummary `json:"options"`
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
	flag.Parse()

	report, err := runBenchmark(cfg)
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

	enc, err := libgopx.NewVP8Encoder(libgopx.EncoderOptions{
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
	if err != nil {
		return benchReport{}, err
	}
	dec, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		return benchReport{}, err
	}

	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	latencies := make([]int64, 0, cfg.Frames)
	quantHist := make(map[string]int)
	quantMin := 0
	quantMax := 0
	quantSum := 0
	encodedFrames := 0
	droppedFrames := 0
	outputBytes := 0
	keyframeBytes := 0
	interBytes := 0
	interCount := 0
	psnrSum := 0.0
	psnrCount := 0

	runtime.GC()
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
		quantHist[fmt.Sprintf("%d", result.Quantizer)]++
		if result.KeyFrame {
			keyframeBytes = result.SizeBytes
		} else {
			interBytes += result.SizeBytes
			interCount++
		}
		if err := dec.Decode(result.Data); err != nil {
			return benchReport{}, err
		}
		decoded, ok := dec.NextFrame()
		if ok {
			psnrSum += imagePSNR(frame, decoded)
			psnrCount++
		}
	}
	runtime.ReadMemStats(&memAfter)

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
	psnr := 0.0
	if psnrCount > 0 {
		psnr = psnrSum / float64(psnrCount)
	}
	quantMean := 0.0
	if encodedFrames > 0 {
		quantMean = float64(quantSum) / float64(encodedFrames)
	}

	return benchReport{
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
		AllocsPerFrame:    float64(memAfter.Mallocs-memBefore.Mallocs) / float64(cfg.Frames),
		PSNR:              psnr,
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
		QuantizerHist: quantHist,
		Options:       benchConfigSummary{Deadline: deadlineName},
	}, nil
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
