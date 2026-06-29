package benchcmd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	govpx "github.com/thesyncim/govpx"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// runLibvpxVP9Benchmark drives vpxenc-vp9 on the same source corpus the
// govpx VP9 encoder consumed, using parity flags that mirror govpx's CBR
// rate-control / buffer / quantizer / drop-frame configuration. The
// resulting IVF stream is decoded through govpx.VP9Decoder to compute
// PSNR/SSIM the same way the in-process VP9 path does.
func runLibvpxVP9Benchmark(cfg benchConfig, frames []govpx.Image, deadlineName string) (referenceReport, error) {
	tempDir, err := os.MkdirTemp("", "govpx-bench-vp9-*")
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
	parityFlags := libvpxVP9ParityFlags(cfg, parity, vpxDeadlineFlag)
	args := append([]string{
		"--codec=vp9",
		"--ivf",
		"--i420",
		fmt.Sprintf("--width=%d", cfg.Width),
		fmt.Sprintf("--height=%d", cfg.Height),
		fmt.Sprintf("--fps=%d/1", cfg.FPS),
		fmt.Sprintf("--limit=%d", cfg.Frames),
	}, parityFlags...)
	args = append(args, cfg.LibvpxArgs...)
	args = append(args, fmt.Sprintf("--output=%s", outPath), rawPath)

	var stderr bytes.Buffer
	cmd := exec.Command(cfg.LibvpxVpxencVP9, args...)
	cmd.Stderr = &stderr
	start := time.Now()
	stdout, err := cmd.Output()
	elapsed := time.Since(start)
	if err != nil {
		return referenceReport{}, fmt.Errorf("libvpx vpxenc-vp9 failed: %w\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr.Bytes())
	}

	ivf, err := os.ReadFile(outPath)
	if err != nil {
		return referenceReport{}, err
	}
	framesInfo, err := parseVP9IVFFrameInfo(ivf)
	if err != nil {
		return referenceReport{}, err
	}
	outputBytes := 0
	for _, info := range framesInfo {
		outputBytes += info.size
	}
	wallNS := elapsed.Nanoseconds()
	denomFrames := int64(len(frames))
	if denomFrames <= 0 {
		denomFrames = 1
	}
	wallPerFrame := wallNS / denomFrames
	encodeNS := wallNS
	timingSource := "wall"
	if parsed, ok := parseVpxencEncodeTime(stderr.Bytes()); ok && parsed.frames > 0 && parsed.totalNS > 0 {
		encodeNS = parsed.totalNS
		timingSource = "vpxenc-stats"
	}
	encodePerFrame := encodeNS / denomFrames
	if encodePerFrame <= 0 {
		encodePerFrame = wallPerFrame
		encodeNS = wallNS
		timingSource = "wall"
	}
	overheadNS := max(wallNS-encodeNS, 0)
	outputBitrate := float64(outputBytes*8*cfg.FPS) / float64(cfg.Frames*1000)
	bitrateError := (outputBitrate - float64(cfg.BitrateKbps)) * 100 / float64(cfg.BitrateKbps)
	keyframeBytes := 0
	interBytes := 0
	interCount := 0
	for _, info := range framesInfo {
		if info.keyFrame {
			keyframeBytes = info.size
		} else {
			interBytes += info.size
			interCount++
		}
	}
	avgInter := 0.0
	if interCount > 0 {
		avgInter = float64(interBytes) / float64(interCount)
	}
	psnr := 0.0
	ssim := 0.0
	qualityFrames := 0
	var qualityErr error
	if !cfg.SkipQuality {
		psnr, ssim, qualityFrames, qualityErr = vp9ReferenceQualityMetrics(ivf, frames)
	}
	qualityError := ""
	if qualityErr != nil {
		qualityError = qualityErr.Error()
	}
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	wallFPS := 0.0
	if wallPerFrame > 0 {
		wallFPS = 1e9 / float64(wallPerFrame)
	}
	encodeFPS := 0.0
	mbps := 0.0
	if encodePerFrame > 0 {
		encodeFPS = 1e9 / float64(encodePerFrame)
		mbps = macroblocksPerFrame * 1e9 / float64(encodePerFrame)
	}
	return referenceReport{
		Encoder:           "libvpx-vp9",
		Mode:              deadlineName,
		OutputBitrateKbps: outputBitrate,
		BitrateErrorPct:   bitrateError,
		NSPerFrame:        encodePerFrame,
		EncodeFPS:         encodeFPS,
		MacroblocksPerSec: mbps,
		PSNR:              psnr,
		SSIM:              ssim,
		QualityFrames:     qualityFrames,
		QualitySkipped:    cfg.SkipQuality,
		QualityError:      qualityError,
		KeyframeBytes:     keyframeBytes,
		AvgInterBytes:     avgInter,
		LatencyNS: latencyReport{
			P50: encodePerFrame,
			P95: encodePerFrame,
			P99: encodePerFrame,
		},
		OutputBytes:          outputBytes,
		EncodedFrames:        len(framesInfo),
		DroppedFrames:        max(cfg.Frames-len(framesInfo), 0),
		TimingSource:         timingSource,
		WallNSPerFrame:       wallPerFrame,
		WallEncodeFPS:        wallFPS,
		SubprocessOverheadNS: overheadNS,
		ParityFlags:          parityFlags,
	}, nil
}

// libvpxVP9ParityFlags mirrors libvpxParityFlags for VP9: same CBR target /
// buffer model / q-range / kf cadence / drop / threading config, but using
// VP9-specific knobs (--row-mt=0, --tile-columns=N, --aq-mode=0,
// --auto-alt-ref=0, --lag-in-frames=0, no --token-parts) so the comparison
// stays apples-to-apples with govpx VP9's tile-threaded realtime configuration.
func libvpxVP9ParityFlags(cfg benchConfig, p encoderParity, deadlineFlag string) []string {
	threadHint, log2TileCols := vp9LibvpxThreadLayout(cfg, p)
	flags := []string{
		"--passes=1",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--aq-mode=0",
		"--row-mt=0",
		fmt.Sprintf("--tile-columns=%d", log2TileCols),
		"--tile-rows=0",
		"--profile=0",
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", cfg.BitrateKbps),
		fmt.Sprintf("--min-q=%d", p.MinQuantizer),
		fmt.Sprintf("--max-q=%d", p.MaxQuantizer),
		// govpx VP9 leaves MinKeyframeInterval=0 in the bench parity model
		// (matches libvpx default kf_min_dist=0). Forcing it to the max
		// distance would deny libvpx adaptive keyframes that govpx still
		// permits, biasing the bitrate/keyframe-size comparison.
		"--kf-min-dist=0",
		fmt.Sprintf("--kf-max-dist=%d", p.KeyFrameInterval),
		fmt.Sprintf("--buf-sz=%d", p.BufferSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", p.BufferInitialSizeMs),
		fmt.Sprintf("--buf-optimal-sz=%d", p.BufferOptimalSizeMs),
		fmt.Sprintf("--undershoot-pct=%d", p.UndershootPct),
		fmt.Sprintf("--overshoot-pct=%d", p.OvershootPct),
		fmt.Sprintf("--threads=%d", threadHint),
		fmt.Sprintf("--timebase=1/%d", cfg.FPS),
		fmt.Sprintf("--noise-sensitivity=%d", p.NoiseSensitivity),
		deadlineFlag,
		fmt.Sprintf("--cpu-used=%d", p.CpuUsed),
	}
	if p.DropFrameAllowed {
		flags = append(flags, fmt.Sprintf("--drop-frame=%d", p.DropFrameWaterMark))
	} else {
		flags = append(flags, "--drop-frame=0")
	}
	if p.MaxIntraBitratePct > 0 {
		flags = append(flags, fmt.Sprintf("--max-intra-rate=%d", p.MaxIntraBitratePct))
	}
	if p.StaticThreshold > 0 {
		flags = append(flags, fmt.Sprintf("--static-thresh=%d", p.StaticThreshold))
	}
	return flags
}

func vp9LibvpxThreadLayout(cfg benchConfig, p encoderParity) (threadHint int, log2TileCols int) {
	threadHint = vp9BenchEffectiveThreadHint(cfg, p)
	return threadHint, vp9BenchLog2TileCols(cfg.Width, threadHint)
}

func vp9BenchEffectiveThreadHint(cfg benchConfig, p encoderParity) int {
	if p.Threads != 0 {
		return p.Threads
	}
	cpus := runtime.NumCPU()
	if !vp9BenchRealtimeAutoThreadingEligible(cfg, p) || cpus < 2 {
		return 1
	}
	threadHint := 2
	if cpus >= 4 {
		threadHint = 4
	}
	tileCols := 1 << uint(vp9BenchLog2TileCols(cfg.Width, threadHint))
	if tileCols <= 1 {
		return 1
	}
	if tileCols < threadHint {
		return tileCols
	}
	return threadHint
}

func vp9BenchRealtimeAutoThreadingEligible(cfg benchConfig, p encoderParity) bool {
	return benchCodec(cfg) == codecVP9 &&
		(cfg.Mode == "" || cfg.Mode == "realtime") &&
		p.NoiseSensitivity == 0
}

func vp9BenchLog2TileCols(width, threads int) int {
	miCols := (width + 7) >> 3
	minLog2, maxLog2 := vp9dec.TileNBits(miCols)
	log2Cols := minLog2
	if threads > 1 {
		log2Cols = max(log2Cols, vp9BenchCeilLog2(threads))
	}
	if log2Cols > maxLog2 {
		log2Cols = maxLog2
	}
	return log2Cols
}

func vp9BenchCeilLog2(v int) int {
	if v <= 1 {
		return 0
	}
	n := 0
	pow := 1
	for pow < v {
		pow <<= 1
		n++
	}
	return n
}

// parseVP9IVFFrameInfo walks an IVF stream produced by vpxenc-vp9 and
// reports per-frame size + keyframe classification. VP9's keyframe bit
// lives in the uncompressed header rather than the first packet byte, so
// the function uses govpx.PeekVP9StreamInfo to classify each packet.
func parseVP9IVFFrameInfo(ivf []byte) ([]ivfFrameInfo, error) {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(ivf) < fileHeaderSize || string(ivf[:4]) != "DKIF" {
		return nil, errors.New("invalid IVF header")
	}
	offset := fileHeaderSize
	var out []ivfFrameInfo
	for offset < len(ivf) {
		if offset+frameHeaderSize > len(ivf) {
			return nil, errors.New("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		offset += frameHeaderSize
		if size < 0 || offset+size > len(ivf) {
			return nil, errors.New("truncated IVF frame payload")
		}
		packet := ivf[offset : offset+size]
		info, err := govpx.PeekVP9StreamInfo(packet)
		if err != nil {
			return nil, fmt.Errorf("peek vp9 stream info: %w", err)
		}
		out = append(out, ivfFrameInfo{
			size:     size,
			keyFrame: info.KeyFrame,
		})
		offset += size
	}
	return out, nil
}

// vp9ReferenceQualityMetrics decodes the libvpx vpxenc-vp9 IVF stream
// through govpx.VP9Decoder and compares each visible frame against its
// source. The decoder writes into a single scratch destination buffer so
// the comparison only allocates once for the whole reference pass.
func vp9ReferenceQualityMetrics(ivf []byte, frames []govpx.Image) (float64, float64, int, error) {
	if len(frames) == 0 {
		return 0, 0, 0, nil
	}
	dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	defer dec.Close()
	width, height := frames[0].Width, frames[0].Height
	dst := newImageBuffer(width, height)
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
		info, err := dec.DecodeInto(packet, &dst)
		if err != nil {
			return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, fmt.Errorf("decode vp9 reference frame %d: %w", frameIndex, err))
		}
		if !info.ShowFrame {
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
		psnrSum += imagePSNR(source, dst)
		ssimSum += imageSSIM(source, dst)
		qualityFrames++
	}
	return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, nil)
}
