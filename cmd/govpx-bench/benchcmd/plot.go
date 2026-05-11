package benchcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	govpx "github.com/thesyncim/govpx"
)

type plotOptions struct {
	ffmpegPath string
	svgPath    string
	csvPath    string
	jsonPath   string
}

type plotComparisonReport struct {
	Report       string                `json:"report"`
	Width        int                   `json:"width"`
	Height       int                   `json:"height"`
	Frames       int                   `json:"frames"`
	FPS          int                   `json:"fps"`
	BitrateKbps  int                   `json:"target_bitrate_kbps"`
	Mode         string                `json:"mode"`
	Threads      int                   `json:"threads"`
	CpuUsed      int                   `json:"cpu_used"`
	FFmpeg       string                `json:"ffmpeg"`
	GovpxVersion string                `json:"govpx_version,omitempty"`
	SVGPath      string                `json:"svg_path"`
	CSVPath      string                `json:"csv_path"`
	JSONPath     string                `json:"json_path"`
	Govpx        plotEncoderSummary    `json:"govpx"`
	Libvpx       plotEncoderSummary    `json:"libvpx"`
	FramesData   []plotFrameComparison `json:"frames_data"`
	Options      benchConfigSummary    `json:"options"`
}

type plotEncoderSummary struct {
	Encoder           string  `json:"encoder"`
	TimingSource      string  `json:"timing_source"`
	OutputBytes       int     `json:"output_bytes"`
	EncodedFrames     int     `json:"encoded_frames"`
	OutputBitrateKbps float64 `json:"output_bitrate_kbps"`
	NSPerFrame        int64   `json:"ns_per_frame"`
	EncodeFPS         float64 `json:"encode_fps"`
	AverageVMAF       float64 `json:"average_vmaf"`
	AveragePSNR       float64 `json:"average_psnr"`
	AverageSSIM       float64 `json:"average_ssim"`
}

type plotFrameComparison struct {
	Frame       int     `json:"frame"`
	GovpxVMAF   float64 `json:"govpx_vmaf"`
	LibvpxVMAF  float64 `json:"libvpx_vmaf"`
	GovpxPSNR   float64 `json:"govpx_psnr"`
	LibvpxPSNR  float64 `json:"libvpx_psnr"`
	GovpxSSIM   float64 `json:"govpx_ssim"`
	LibvpxSSIM  float64 `json:"libvpx_ssim"`
	GovpxBytes  int     `json:"govpx_bytes"`
	LibvpxBytes int     `json:"libvpx_bytes"`
}

type plotEncodeResult struct {
	path              string
	sizes             []int
	outputBytes       int
	encodedFrames     int
	outputBitrateKbps float64
	nsPerFrame        int64
	encodeFPS         float64
}

type ffmpegFrameMetrics struct {
	vmaf []float64
	psnr []float64
	ssim []float64
}

func runPlotComparison(cfg benchConfig, opts plotOptions) (plotComparisonReport, error) {
	if opts.svgPath == "" {
		return plotComparisonReport{}, errors.New("-plot requires an SVG output path")
	}
	if cfg.Decode {
		return plotComparisonReport{}, errors.New("-plot supports encode comparisons only")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Frames <= 0 || cfg.FPS <= 0 || cfg.BitrateKbps <= 0 {
		return plotComparisonReport{}, errors.New("width, height, frames, fps, and bitrate must be positive")
	}
	if cfg.Width > 16383 || cfg.Height > 16383 {
		return plotComparisonReport{}, errors.New("dimensions exceed VP8 limits")
	}
	deadline, deadlineName, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		return plotComparisonReport{}, err
	}
	ffmpegPath := opts.ffmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	resolvedFFmpeg, err := exec.LookPath(ffmpegPath)
	if err != nil {
		return plotComparisonReport{}, fmt.Errorf("find ffmpeg: %w", err)
	}
	if err := requireFFmpegLibvpx(resolvedFFmpeg); err != nil {
		return plotComparisonReport{}, err
	}
	if err := requireFFmpegLibvmaf(resolvedFFmpeg); err != nil {
		return plotComparisonReport{}, err
	}
	ffmpegVersion := ffmpegVersionLine(resolvedFFmpeg)

	frames := make([]govpx.Image, cfg.Frames)
	for i := range frames {
		frames[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
	}
	tempDir, err := os.MkdirTemp("", "govpx-plot-*")
	if err != nil {
		return plotComparisonReport{}, err
	}
	defer os.RemoveAll(tempDir)

	sourcePath := filepath.Join(tempDir, "source.i420")
	if err := writeBenchmarkSource(sourcePath, frames); err != nil {
		return plotComparisonReport{}, err
	}
	govpxPath := filepath.Join(tempDir, "govpx.ivf")
	govpxEncoded, err := encodeGovpxForPlot(cfg, deadline, frames, govpxPath)
	if err != nil {
		return plotComparisonReport{}, err
	}
	libvpxPath := filepath.Join(tempDir, "libvpx.ivf")
	libvpxEncoded, err := encodeFFmpegLibvpxForPlot(resolvedFFmpeg, cfg, sourcePath, libvpxPath, deadlineName)
	if err != nil {
		return plotComparisonReport{}, err
	}
	govpxMetrics, err := ffmpegQualityMetrics(resolvedFFmpeg, cfg, sourcePath, govpxPath, filepath.Join(tempDir, "govpx"))
	if err != nil {
		return plotComparisonReport{}, fmt.Errorf("govpx ffmpeg metrics: %w", err)
	}
	libvpxMetrics, err := ffmpegQualityMetrics(resolvedFFmpeg, cfg, sourcePath, libvpxPath, filepath.Join(tempDir, "libvpx"))
	if err != nil {
		return plotComparisonReport{}, fmt.Errorf("libvpx ffmpeg metrics: %w", err)
	}

	frameData := plotFrameData(govpxEncoded.sizes, libvpxEncoded.sizes, govpxMetrics, libvpxMetrics)
	if len(frameData) == 0 {
		return plotComparisonReport{}, errors.New("ffmpeg produced no aligned metric frames")
	}
	csvPath := opts.csvPath
	if csvPath == "" {
		csvPath = replaceExt(opts.svgPath, ".csv")
	}
	jsonPath := opts.jsonPath
	if jsonPath == "" {
		jsonPath = replaceExt(opts.svgPath, ".json")
	}
	report := plotComparisonReport{
		Report:       "govpx-bench-plot",
		Width:        cfg.Width,
		Height:       cfg.Height,
		Frames:       cfg.Frames,
		FPS:          cfg.FPS,
		BitrateKbps:  cfg.BitrateKbps,
		Mode:         deadlineName,
		Threads:      cfg.Threads,
		CpuUsed:      cfg.CpuUsed,
		FFmpeg:       ffmpegVersion,
		GovpxVersion: govpxBuildVersion(),
		SVGPath:      opts.svgPath,
		CSVPath:      csvPath,
		JSONPath:     jsonPath,
		Govpx: plotEncoderSummary{
			Encoder:           "govpx",
			TimingSource:      "go-steady-state",
			OutputBytes:       govpxEncoded.outputBytes,
			EncodedFrames:     govpxEncoded.encodedFrames,
			OutputBitrateKbps: govpxEncoded.outputBitrateKbps,
			NSPerFrame:        govpxEncoded.nsPerFrame,
			EncodeFPS:         govpxEncoded.encodeFPS,
			AverageVMAF:       averageFinite(govpxMetrics.vmaf),
			AveragePSNR:       averageFinite(govpxMetrics.psnr),
			AverageSSIM:       averageFinite(govpxMetrics.ssim),
		},
		Libvpx: plotEncoderSummary{
			Encoder:           "ffmpeg-libvpx-vp8",
			TimingSource:      "ffmpeg-wall",
			OutputBytes:       libvpxEncoded.outputBytes,
			EncodedFrames:     libvpxEncoded.encodedFrames,
			OutputBitrateKbps: libvpxEncoded.outputBitrateKbps,
			NSPerFrame:        libvpxEncoded.nsPerFrame,
			EncodeFPS:         libvpxEncoded.encodeFPS,
			AverageVMAF:       averageFinite(libvpxMetrics.vmaf),
			AveragePSNR:       averageFinite(libvpxMetrics.psnr),
			AverageSSIM:       averageFinite(libvpxMetrics.ssim),
		},
		FramesData: frameData,
		Options:    benchSummary(deadlineName),
	}
	if err := writePlotArtifacts(report); err != nil {
		return plotComparisonReport{}, err
	}
	return report, nil
}

func formatPlotReport(report plotComparisonReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "plot           svg=%s  csv=%s  json=%s\n", report.SVGPath, report.CSVPath, report.JSONPath)
	fmt.Fprintf(&b, "config         %dx%d  frames=%d  fps=%d  bitrate=%dkbps  mode=%s  threads=%d  cpu-used=%d\n",
		report.Width, report.Height, report.Frames, report.FPS, report.BitrateKbps, report.Mode, report.Threads, report.CpuUsed)
	fmt.Fprintf(&b, "govpx          fps=%.2f  kbps=%.2f  vmaf=%.3f  psnr=%.3f  ssim=%.6f  bytes=%d\n",
		report.Govpx.EncodeFPS, report.Govpx.OutputBitrateKbps, report.Govpx.AverageVMAF, report.Govpx.AveragePSNR, report.Govpx.AverageSSIM, report.Govpx.OutputBytes)
	fmt.Fprintf(&b, "libvpx         fps=%.2f  kbps=%.2f  vmaf=%.3f  psnr=%.3f  ssim=%.6f  bytes=%d\n",
		report.Libvpx.EncodeFPS, report.Libvpx.OutputBitrateKbps, report.Libvpx.AverageVMAF, report.Libvpx.AveragePSNR, report.Libvpx.AverageSSIM, report.Libvpx.OutputBytes)
	if report.Libvpx.EncodeFPS > 0 {
		fmt.Fprintf(&b, "relative       fps_ratio=%.4f  vmaf_delta=%.3f  psnr_delta=%.3f  ssim_delta=%.6f\n",
			report.Govpx.EncodeFPS/report.Libvpx.EncodeFPS,
			report.Govpx.AverageVMAF-report.Libvpx.AverageVMAF,
			report.Govpx.AveragePSNR-report.Libvpx.AveragePSNR,
			report.Govpx.AverageSSIM-report.Libvpx.AverageSSIM)
	}
	fmt.Fprintf(&b, "ffmpeg         %s\n", report.FFmpeg)
	return b.String()
}

func writeBenchmarkSource(path string, frames []govpx.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	for _, frame := range frames {
		if err := writeI420Frame(f, frame); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func encodeGovpxForPlot(cfg benchConfig, deadline govpx.Deadline, frames []govpx.Image, outPath string) (plotEncodeResult, error) {
	enc, err := newBenchmarkEncoder(cfg, deadline)
	if err != nil {
		return plotEncodeResult{}, err
	}
	packet := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	for i, frame := range frames {
		if _, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0); err != nil {
			return plotEncodeResult{}, err
		}
	}
	enc.Reset()

	packets := make([][]byte, 0, len(frames))
	latencies := make([]int64, 0, len(frames))
	for i, frame := range frames {
		start := time.Now()
		result, err := enc.EncodeInto(packet, frame, uint64(i), 1, 0)
		elapsed := time.Since(start)
		if err != nil {
			return plotEncodeResult{}, err
		}
		latencies = append(latencies, elapsed.Nanoseconds())
		if result.Dropped {
			continue
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	if len(packets) == 0 {
		return plotEncodeResult{}, errors.New("govpx dropped every frame")
	}
	ivf := makeBenchmarkIVF(cfg.Width, cfg.Height, cfg.FPS, packets)
	if err := os.WriteFile(outPath, ivf, 0o600); err != nil {
		return plotEncodeResult{}, err
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		return plotEncodeResult{}, err
	}
	return summarizePlotEncode(outPath, sizes, cfg, totalLatencyNS(latencies)), nil
}

func encodeFFmpegLibvpxForPlot(ffmpeg string, cfg benchConfig, sourcePath string, outPath string, deadlineName string) (plotEncodeResult, error) {
	args := ffmpegLibvpxPlotArgs(cfg, sourcePath, outPath, deadlineName)
	cmd := exec.Command(ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		return plotEncodeResult{}, fmt.Errorf("ffmpeg libvpx encode failed: %w\ncommand: %s %s\nstderr:\n%s",
			err, ffmpeg, strings.Join(args, " "), stderr.String())
	}
	ivf, err := os.ReadFile(outPath)
	if err != nil {
		return plotEncodeResult{}, err
	}
	sizes, err := parseIVFFrameSizes(ivf)
	if err != nil {
		return plotEncodeResult{}, err
	}
	if len(sizes) == 0 {
		return plotEncodeResult{}, errors.New("ffmpeg libvpx encoded zero frames")
	}
	return summarizePlotEncode(outPath, sizes, cfg, elapsed.Nanoseconds()), nil
}

func ffmpegLibvpxPlotArgs(cfg benchConfig, sourcePath string, outPath string, deadlineName string) []string {
	parity := parityFor(cfg)
	deadline := "realtime"
	if deadlineName == "good" {
		deadline = "good"
	}
	bufferKbits := max(1, cfg.BitrateKbps*parity.BufferSizeMs/1000)
	return []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-bitexact",
		"-f", "rawvideo",
		"-pix_fmt", "yuv420p",
		"-s:v", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"-r", strconv.Itoa(cfg.FPS),
		"-i", sourcePath,
		"-an",
		"-c:v", "libvpx",
		"-deadline", deadline,
		"-cpu-used", strconv.Itoa(parity.CpuUsed),
		"-b:v", fmt.Sprintf("%dk", cfg.BitrateKbps),
		"-minrate", fmt.Sprintf("%dk", cfg.BitrateKbps),
		"-maxrate", fmt.Sprintf("%dk", cfg.BitrateKbps),
		"-bufsize", fmt.Sprintf("%dk", bufferKbits),
		"-qmin", strconv.Itoa(parity.MinQuantizer),
		"-qmax", strconv.Itoa(parity.MaxQuantizer),
		"-g", strconv.Itoa(parity.KeyFrameInterval),
		"-keyint_min", strconv.Itoa(parity.KeyFrameInterval),
		"-lag-in-frames", "0",
		"-auto-alt-ref", "0",
		"-noise-sensitivity", "0",
		"-undershoot-pct", strconv.Itoa(parity.UndershootPct),
		"-overshoot-pct", strconv.Itoa(parity.OvershootPct),
		"-threads", strconv.Itoa(parity.Threads),
		"-f", "ivf",
		outPath,
	}
}

func summarizePlotEncode(path string, sizes []int, cfg benchConfig, totalNS int64) plotEncodeResult {
	outputBytes := 0
	for _, size := range sizes {
		outputBytes += size
	}
	encodedFrames := len(sizes)
	nsPerFrame := int64(0)
	if encodedFrames > 0 {
		nsPerFrame = totalNS / int64(encodedFrames)
	}
	encodeFPS := 0.0
	if nsPerFrame > 0 {
		encodeFPS = 1e9 / float64(nsPerFrame)
	}
	return plotEncodeResult{
		path:              path,
		sizes:             sizes,
		outputBytes:       outputBytes,
		encodedFrames:     encodedFrames,
		outputBitrateKbps: float64(outputBytes*8*cfg.FPS) / float64(cfg.Frames*1000),
		nsPerFrame:        nsPerFrame,
		encodeFPS:         encodeFPS,
	}
}

func ffmpegQualityMetrics(ffmpeg string, cfg benchConfig, sourcePath string, ivfPath string, prefix string) (ffmpegFrameMetrics, error) {
	vmafPath := prefix + ".vmaf.json"
	psnrPath := prefix + ".psnr.log"
	ssimPath := prefix + ".ssim.log"
	if err := runFFmpegMetricFilter(ffmpeg, cfg, sourcePath, ivfPath,
		fmt.Sprintf("[0:v][1:v]libvmaf=n_threads=1:log_fmt=json:log_path=%s", vmafPath)); err != nil {
		return ffmpegFrameMetrics{}, fmt.Errorf("vmaf: %w", err)
	}
	if err := runFFmpegMetricFilter(ffmpeg, cfg, sourcePath, ivfPath,
		fmt.Sprintf("[0:v][1:v]psnr=stats_file=%s", psnrPath)); err != nil {
		return ffmpegFrameMetrics{}, fmt.Errorf("psnr: %w", err)
	}
	if err := runFFmpegMetricFilter(ffmpeg, cfg, sourcePath, ivfPath,
		fmt.Sprintf("[0:v][1:v]ssim=stats_file=%s", ssimPath)); err != nil {
		return ffmpegFrameMetrics{}, fmt.Errorf("ssim: %w", err)
	}
	vmaf, err := parseFFmpegVMAFStats(vmafPath)
	if err != nil {
		return ffmpegFrameMetrics{}, err
	}
	psnr, err := parseFFmpegPSNRStats(psnrPath)
	if err != nil {
		return ffmpegFrameMetrics{}, err
	}
	ssim, err := parseFFmpegSSIMStats(ssimPath)
	if err != nil {
		return ffmpegFrameMetrics{}, err
	}
	return ffmpegFrameMetrics{vmaf: vmaf, psnr: psnr, ssim: ssim}, nil
}

func runFFmpegMetricFilter(ffmpeg string, cfg benchConfig, sourcePath string, ivfPath string, filter string) error {
	args := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-bitexact",
		"-i", ivfPath,
		"-f", "rawvideo",
		"-pix_fmt", "yuv420p",
		"-s:v", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"-r", strconv.Itoa(cfg.FPS),
		"-i", sourcePath,
		"-lavfi", filter,
		"-f", "null",
		"-",
	}
	cmd := exec.Command(ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg metrics failed: %w\ncommand: %s %s\nstderr:\n%s",
			err, ffmpeg, strings.Join(args, " "), stderr.String())
	}
	return nil
}

func parseFFmpegVMAFStats(path string) ([]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stats struct {
		Frames []struct {
			Metrics map[string]float64 `json:"metrics"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}
	values := make([]float64, 0, len(stats.Frames))
	for i, frame := range stats.Frames {
		value, ok := vmafMetricValue(frame.Metrics)
		if !ok {
			return nil, fmt.Errorf("frame %d has no VMAF metric in %s", i, path)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("no VMAF frames in %s", path)
	}
	return values, nil
}

func vmafMetricValue(metrics map[string]float64) (float64, bool) {
	for _, key := range []string{"vmaf", "integer_vmaf", "vmaf_score", "VMAF_score"} {
		if value, ok := metrics[key]; ok {
			return value, true
		}
	}
	for key, value := range metrics {
		if strings.HasSuffix(strings.ToLower(key), "vmaf") {
			return value, true
		}
	}
	return 0, false
}

func parseFFmpegPSNRStats(path string) ([]float64, error) {
	return parseFFmpegMetricStats(path, "psnr_avg")
}

func parseFFmpegSSIMStats(path string) ([]float64, error) {
	return parseFFmpegMetricStats(path, "All")
}

func parseFFmpegMetricStats(path string, key string) ([]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	values := make([]float64, 0, len(lines))
	for _, line := range lines {
		fields := strings.FieldsSeq(line)
		for field := range fields {
			raw, ok := strings.CutPrefix(field, key+":")
			if !ok {
				continue
			}
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, fmt.Errorf("parse %s in %q: %w", key, line, err)
			}
			values = append(values, value)
			break
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("no %s values in %s", key, path)
	}
	return values, nil
}

func plotFrameData(govpxSizes []int, libvpxSizes []int, govpxMetrics ffmpegFrameMetrics, libvpxMetrics ffmpegFrameMetrics) []plotFrameComparison {
	count := min(len(govpxSizes), len(libvpxSizes))
	count = min(count, len(govpxMetrics.vmaf))
	count = min(count, len(libvpxMetrics.vmaf))
	count = min(count, len(govpxMetrics.psnr))
	count = min(count, len(libvpxMetrics.psnr))
	count = min(count, len(govpxMetrics.ssim))
	count = min(count, len(libvpxMetrics.ssim))
	frames := make([]plotFrameComparison, count)
	for i := range count {
		frames[i] = plotFrameComparison{
			Frame:       i,
			GovpxVMAF:   govpxMetrics.vmaf[i],
			LibvpxVMAF:  libvpxMetrics.vmaf[i],
			GovpxPSNR:   govpxMetrics.psnr[i],
			LibvpxPSNR:  libvpxMetrics.psnr[i],
			GovpxSSIM:   govpxMetrics.ssim[i],
			LibvpxSSIM:  libvpxMetrics.ssim[i],
			GovpxBytes:  govpxSizes[i],
			LibvpxBytes: libvpxSizes[i],
		}
	}
	return frames
}

func writePlotArtifacts(report plotComparisonReport) error {
	if err := ensureParentDir(report.SVGPath); err != nil {
		return err
	}
	if err := ensureParentDir(report.CSVPath); err != nil {
		return err
	}
	if err := ensureParentDir(report.JSONPath); err != nil {
		return err
	}
	if err := os.WriteFile(report.SVGPath, []byte(renderPlotSVG(report)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(report.CSVPath, []byte(renderPlotCSV(report)), 0o644); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(report.JSONPath, data, 0o644)
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func renderPlotCSV(report plotComparisonReport) string {
	var b strings.Builder
	b.WriteString("frame,govpx_vmaf,libvpx_vmaf,govpx_psnr,libvpx_psnr,govpx_ssim,libvpx_ssim,govpx_bytes,libvpx_bytes\n")
	for _, frame := range report.FramesData {
		fmt.Fprintf(&b, "%d,%.6f,%.6f,%.6f,%.6f,%.9f,%.9f,%d,%d\n",
			frame.Frame, frame.GovpxVMAF, frame.LibvpxVMAF, frame.GovpxPSNR, frame.LibvpxPSNR, frame.GovpxSSIM, frame.LibvpxSSIM, frame.GovpxBytes, frame.LibvpxBytes)
	}
	return b.String()
}

func renderPlotSVG(report plotComparisonReport) string {
	const (
		width       = 1120
		height      = 920
		left        = 72
		right       = 32
		panelWidth  = width - left - right
		panelHeight = 142
	)
	vmafTop := 178
	psnrTop := 354
	ssimTop := 530
	sizeTop := 706
	govpxColor := "#2563eb"
	libvpxColor := "#dc2626"
	bg := "#ffffff"
	axis := "#334155"
	grid := "#cbd5e1"
	text := "#0f172a"
	title := fmt.Sprintf("govpx vs ffmpeg libvpx VP8 - %dx%d %dfps %dkbps %s",
		report.Width, report.Height, report.FPS, report.BitrateKbps, report.Mode)

	vmafValues := twoSeriesValues(report.FramesData, func(f plotFrameComparison) (float64, float64) {
		return f.GovpxVMAF, f.LibvpxVMAF
	})
	psnrValues := twoSeriesValues(report.FramesData, func(f plotFrameComparison) (float64, float64) {
		return f.GovpxPSNR, f.LibvpxPSNR
	})
	ssimValues := twoSeriesValues(report.FramesData, func(f plotFrameComparison) (float64, float64) {
		return f.GovpxSSIM, f.LibvpxSSIM
	})
	sizeValues := twoSeriesValues(report.FramesData, func(f plotFrameComparison) (float64, float64) {
		return float64(f.GovpxBytes), float64(f.LibvpxBytes)
	})
	vmafScale := valueScale(vmafValues, 0.2)
	psnrScale := valueScale(psnrValues, 0.5)
	ssimScale := valueScale(ssimValues, 0.002)
	sizeScale := valueScale(sizeValues, 8)
	xMax := max(1, len(report.FramesData)-1)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n", width, height, width, height)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s"/>`+"\n", bg)
	fmt.Fprintf(&b, `<text x="32" y="42" font-family="ui-sans-serif, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif" font-size="24" font-weight="700" fill="%s">%s</text>`+"\n", text, html.EscapeString(title))
	fmt.Fprintf(&b, `<text x="32" y="70" font-family="ui-sans-serif, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif" font-size="13" fill="#475569">ffmpeg libvmaf/psnr/ssim metrics, synthetic I420 source, no random seed, no external plotting deps</text>`+"\n")
	fmt.Fprintf(&b, `<text x="32" y="104" font-family="ui-sans-serif, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif" font-size="14" fill="%s">govpx %.2f fps / %.2f kbps / VMAF %.3f / PSNR %.3f / SSIM %.6f</text>`+"\n", govpxColor, report.Govpx.EncodeFPS, report.Govpx.OutputBitrateKbps, report.Govpx.AverageVMAF, report.Govpx.AveragePSNR, report.Govpx.AverageSSIM)
	fmt.Fprintf(&b, `<text x="32" y="126" font-family="ui-sans-serif, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif" font-size="14" fill="%s">libvpx %.2f fps / %.2f kbps / VMAF %.3f / PSNR %.3f / SSIM %.6f</text>`+"\n", libvpxColor, report.Libvpx.EncodeFPS, report.Libvpx.OutputBitrateKbps, report.Libvpx.AverageVMAF, report.Libvpx.AveragePSNR, report.Libvpx.AverageSSIM)

	renderPanel(&b, "VMAF", vmafTop, left, panelWidth, panelHeight, xMax, vmafScale, axis, grid, govpxColor, libvpxColor,
		linePath(report.FramesData, left, vmafTop, panelWidth, panelHeight, xMax, vmafScale, func(f plotFrameComparison) float64 { return f.GovpxVMAF }),
		linePath(report.FramesData, left, vmafTop, panelWidth, panelHeight, xMax, vmafScale, func(f plotFrameComparison) float64 { return f.LibvpxVMAF }),
	)
	renderPanel(&b, "PSNR (dB)", psnrTop, left, panelWidth, panelHeight, xMax, psnrScale, axis, grid, govpxColor, libvpxColor,
		linePath(report.FramesData, left, psnrTop, panelWidth, panelHeight, xMax, psnrScale, func(f plotFrameComparison) float64 { return f.GovpxPSNR }),
		linePath(report.FramesData, left, psnrTop, panelWidth, panelHeight, xMax, psnrScale, func(f plotFrameComparison) float64 { return f.LibvpxPSNR }),
	)
	renderPanel(&b, "SSIM", ssimTop, left, panelWidth, panelHeight, xMax, ssimScale, axis, grid, govpxColor, libvpxColor,
		linePath(report.FramesData, left, ssimTop, panelWidth, panelHeight, xMax, ssimScale, func(f plotFrameComparison) float64 { return f.GovpxSSIM }),
		linePath(report.FramesData, left, ssimTop, panelWidth, panelHeight, xMax, ssimScale, func(f plotFrameComparison) float64 { return f.LibvpxSSIM }),
	)
	renderPanel(&b, "Frame Bytes", sizeTop, left, panelWidth, panelHeight, xMax, sizeScale, axis, grid, govpxColor, libvpxColor,
		linePath(report.FramesData, left, sizeTop, panelWidth, panelHeight, xMax, sizeScale, func(f plotFrameComparison) float64 { return float64(f.GovpxBytes) }),
		linePath(report.FramesData, left, sizeTop, panelWidth, panelHeight, xMax, sizeScale, func(f plotFrameComparison) float64 { return float64(f.LibvpxBytes) }),
	)
	b.WriteString("</svg>\n")
	return b.String()
}

type plotScale struct {
	min float64
	max float64
}

func renderPanel(b *strings.Builder, title string, top int, left int, width int, height int, xMax int, y plotScale, axis string, grid string, govpxColor string, libvpxColor string, govpxPath string, libvpxPath string) {
	font := "ui-sans-serif, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="%s" font-size="15" font-weight="700" fill="#0f172a">%s</text>`+"\n", left, top-18, font, html.EscapeString(title))
	for i := 0; i <= 4; i++ {
		yPos := top + height - (height*i)/4
		value := y.min + (y.max-y.min)*float64(i)/4
		fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1"/>`+"\n", left, yPos, left+width, yPos, grid)
		fmt.Fprintf(b, `<text x="%d" y="%d" text-anchor="end" font-family="%s" font-size="11" fill="#475569">%.3g</text>`+"\n", left-10, yPos+4, font, value)
	}
	for i := 0; i <= 4; i++ {
		xPos := left + (width*i)/4
		frame := (xMax * i) / 4
		fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1"/>`+"\n", xPos, top, xPos, top+height, grid)
		fmt.Fprintf(b, `<text x="%d" y="%d" text-anchor="middle" font-family="%s" font-size="11" fill="#475569">%d</text>`+"\n", xPos, top+height+18, font, frame)
	}
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="%d" fill="none" stroke="%s" stroke-width="1.2"/>`+"\n", left, top, width, height, axis)
	fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="2.2"/>`+"\n", govpxPath, govpxColor)
	fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="2.2"/>`+"\n", libvpxPath, libvpxColor)
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="%s" font-size="12" fill="%s">govpx</text>`+"\n", left+width-118, top+16, font, govpxColor)
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="%s" font-size="12" fill="%s">libvpx</text>`+"\n", left+width-66, top+16, font, libvpxColor)
}

func linePath(frames []plotFrameComparison, left int, top int, width int, height int, xMax int, y plotScale, value func(plotFrameComparison) float64) string {
	if len(frames) == 0 {
		return ""
	}
	var b strings.Builder
	den := y.max - y.min
	if den <= 0 {
		den = 1
	}
	for i, frame := range frames {
		x := float64(left)
		if xMax > 0 {
			x += float64(width) * float64(frame.Frame) / float64(xMax)
		}
		ratio := (value(frame) - y.min) / den
		ratio = math.Max(0, math.Min(1, ratio))
		yy := float64(top+height) - ratio*float64(height)
		if i == 0 {
			fmt.Fprintf(&b, "M %.2f %.2f", x, yy)
		} else {
			fmt.Fprintf(&b, " L %.2f %.2f", x, yy)
		}
	}
	return b.String()
}

func twoSeriesValues(frames []plotFrameComparison, values func(plotFrameComparison) (float64, float64)) []float64 {
	out := make([]float64, 0, len(frames)*2)
	for _, frame := range frames {
		a, b := values(frame)
		if isFinite(a) {
			out = append(out, a)
		}
		if isFinite(b) {
			out = append(out, b)
		}
	}
	return out
}

func valueScale(values []float64, pad float64) plotScale {
	if len(values) == 0 {
		return plotScale{min: 0, max: 1}
	}
	lo := values[0]
	hi := values[0]
	for _, value := range values[1:] {
		lo = min(lo, value)
		hi = max(hi, value)
	}
	if lo == hi {
		lo -= pad
		hi += pad
	} else {
		delta := hi - lo
		lo -= max(pad, delta*0.08)
		hi += max(pad, delta*0.08)
	}
	return plotScale{min: lo, max: hi}
}

func averageFinite(values []float64) float64 {
	sum := 0.0
	count := 0
	for _, value := range values {
		if isFinite(value) {
			sum += value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func replaceExt(path string, ext string) string {
	current := filepath.Ext(path)
	if current == "" {
		return path + ext
	}
	return strings.TrimSuffix(path, current) + ext
}

func requireFFmpegLibvpx(ffmpeg string) error {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-encoders")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg -encoders failed: %w\n%s", err, out)
	}
	if !bytes.Contains(out, []byte(" libvpx ")) && !bytes.Contains(out, []byte(" libvpx\t")) {
		return fmt.Errorf("%s does not report a libvpx encoder", ffmpeg)
	}
	return nil
}

func requireFFmpegLibvmaf(ffmpeg string) error {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-filters")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg -filters failed: %w\n%s", err, out)
	}
	if !bytes.Contains(out, []byte(" libvmaf ")) && !bytes.Contains(out, []byte(" libvmaf\t")) {
		return fmt.Errorf("%s does not report the libvmaf filter; install or select an ffmpeg build with libvmaf enabled", ffmpeg)
	}
	return nil
}

func ffmpegVersionLine(ffmpeg string) string {
	cmd := exec.Command(ffmpeg, "-version")
	out, err := cmd.Output()
	if err != nil {
		return ffmpeg
	}
	line, _, _ := strings.Cut(string(out), "\n")
	if line == "" {
		return ffmpeg
	}
	return line
}

func govpxBuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return info.Main.Version
}
