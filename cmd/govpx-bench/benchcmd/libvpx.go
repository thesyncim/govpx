package benchcmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	govpx "github.com/thesyncim/govpx"
)

func runLibvpxBenchmark(cfg benchConfig, frames []govpx.Image, deadlineName string) (referenceReport, error) {
	tempDir, err := os.MkdirTemp("", "govpx-bench-*")
	if err != nil {
		return referenceReport{}, err
	}
	defer os.RemoveAll(tempDir)

	rawPath := cfg.prewrittenRawPath
	outPath := tempDir + string(os.PathSeparator) + "output.ivf"
	if rawPath == "" {
		rawPath = tempDir + string(os.PathSeparator) + "input.i420"
		raw, err := os.Create(rawPath)
		if err != nil {
			return referenceReport{}, err
		}
		bw := bufio.NewWriterSize(raw, 512*1024)
		for _, frame := range frames {
			if err := writeI420FrameBuf(bw, frame); err != nil {
				raw.Close()
				return referenceReport{}, err
			}
		}
		if err := bw.Flush(); err != nil {
			raw.Close()
			return referenceReport{}, err
		}
		if err := raw.Close(); err != nil {
			return referenceReport{}, err
		}
	}

	vpxDeadlineFlag := "--rt"
	if deadlineName == "good" {
		vpxDeadlineFlag = "--good"
	}
	parity := parityFor(cfg)
	parityFlags := libvpxParityFlags(cfg, parity, vpxDeadlineFlag)
	baseArgs := []string{
		"--codec=vp8",
		"--ivf",
		"--i420",
		fmt.Sprintf("--width=%d", cfg.Width),
		fmt.Sprintf("--height=%d", cfg.Height),
		fmt.Sprintf("--fps=%d/1", cfg.FPS),
	}
	// Warm up vpxenc binary pages, dyld, and input page cache before the
	// measured run so libvpx does not pay cold-start page faults while govpx
	// runs after an in-process warmup pass.
	if !cfg.libvpxWarmed {
		warmFrames := min(len(frames), 4)
		if warmFrames > 0 {
			warmArgs := append(append([]string{}, baseArgs...), fmt.Sprintf("--limit=%d", warmFrames))
			warmArgs = append(warmArgs, parityFlags...)
			warmArgs = append(warmArgs, cfg.LibvpxArgs...)
			warmArgs = append(warmArgs, fmt.Sprintf("--output=%s", tempDir+string(os.PathSeparator)+"warm.ivf"), rawPath)
			warm := exec.Command(cfg.LibvpxVpxenc, warmArgs...)
			warm.Stdout = nil
			warm.Stderr = nil
			_ = warm.Run()
		}
	}
	args := append(append([]string{}, baseArgs...), fmt.Sprintf("--limit=%d", cfg.Frames))
	args = append(args, parityFlags...)
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
	framesInfo, err := parseIVFFrameInfo(ivf)
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
	overheadNS := max(wallNS-encodeNS, 0)
	outputBitrate := float64(outputBytes*8*cfg.FPS) / float64(cfg.Frames*1000)
	bitrateError := (outputBitrate - float64(cfg.BitrateKbps)) * 100 / float64(cfg.BitrateKbps)
	keyframeBytes := 0
	interBytes := 0
	interCount := 0
	for _, frame := range framesInfo {
		if frame.keyFrame {
			keyframeBytes = frame.size
		} else {
			interBytes += frame.size
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
		psnr, ssim, qualityFrames, qualityErr = referenceQualityMetrics(ivf, frames)
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
	encodedPerFrame := int64(0)
	if len(sizes) > 0 && len(sizes) < len(frames) {
		encodedPerFrame = encodeNS / int64(len(sizes))
	}
	return referenceReport{
		Encoder:           "libvpx-vp8",
		Mode:              deadlineName,
		OutputBitrateKbps: outputBitrate,
		BitrateErrorPct:   bitrateError,
		NSPerFrame:        encodePerFrame,
		EncodedNSPerFrame: encodedPerFrame,
		EncodeFPS:         1e9 / float64(encodePerFrame),
		MacroblocksPerSec: macroblocksPerFrame * 1e9 / float64(encodePerFrame),
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
		EncodedFrames:        len(sizes),
		DroppedFrames:        max(cfg.Frames-len(sizes), 0),
		TimingSource:         timingSource,
		WallNSPerFrame:       wallPerFrame,
		WallEncodeFPS:        wallFPS,
		SubprocessOverheadNS: overheadNS,
		ParityFlags:          parityFlags,
	}, nil
}

// libvpxParityFlags returns the vpxenc flags that mirror govpx's
// EncoderOptions for a fair benchmark: same CBR target and buffer model,
// same q-range and keyframe cadence, realtime drop/intra/noise/static knobs
// when enabled, single-pass, matched thread count, no lag, deadline matched.
// The deadlineFlag is "--rt" or "--good" depending on benchConfig.Mode.
func libvpxParityFlags(cfg benchConfig, p encoderParity, deadlineFlag string) []string {
	flags := []string{
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
		fmt.Sprintf("--token-parts=%d", p.TokenPartitions),
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

const libvpxVP9CallStatsPrefix = "LIBVPX_VP9_CALL_STATS "

func parseLibvpxVP9CallStats(stderr []byte) (*vp9CallStats, bool) {
	text := string(stderr)
	idx := strings.LastIndex(text, libvpxVP9CallStatsPrefix)
	if idx < 0 {
		return nil, false
	}
	line := text[idx+len(libvpxVP9CallStatsPrefix):]
	if end := strings.IndexAny(line, "\r\n"); end >= 0 {
		line = line[:end]
	}
	var stats vp9CallStats
	seen := false
	for field := range strings.FieldsSeq(line) {
		key, rawValue, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		value, err := strconv.ParseUint(rawValue, 10, 64)
		if err != nil {
			continue
		}
		if assignVP9CallStat(&stats, key, value) {
			seen = true
		}
	}
	if !seen {
		return nil, false
	}
	return &stats, true
}

func assignVP9CallStat(stats *vp9CallStats, key string, value uint64) bool {
	switch key {
	case "inter_mode_picks":
		stats.InterModePicks = value
	case "inter_mode_sub8x8_picks":
		stats.InterModeSub8x8Picks = value
	case "build_sby":
		stats.BuildSBY = value
	case "build_sbp":
		stats.BuildSBP = value
	case "build_sbuv":
		stats.BuildSBUV = value
	case "build_sb":
		stats.BuildSB = value
	case "build_planes":
		stats.BuildPlanes = value
	case "single_predictor_builds":
		stats.SinglePredictorBuilds = value
	case "fullpel_searches":
		stats.FullpelSearches = value
	case "sad_calls":
		stats.SADCalls = value
	case "sad_candidates":
		stats.SADCandidates = value
	case "sad_batch_calls":
		stats.SADBatchCalls = value
	case "predictor_copy":
		stats.PredictorCopy = value
	case "predictor_avg":
		stats.PredictorAvg = value
	case "predictor_vert":
		stats.PredictorVert = value
	case "predictor_avg_vert":
		stats.PredictorAvgVert = value
	case "predictor_horiz":
		stats.PredictorHoriz = value
	case "predictor_avg_horiz":
		stats.PredictorAvgHoriz = value
	case "predictor_2d":
		stats.Predictor2D = value
	case "predictor_avg_2d":
		stats.PredictorAvg2D = value
	case "mode_block_64x64":
		stats.ModeBlock64x64 = value
	case "mode_block_32x32":
		stats.ModeBlock32x32 = value
	case "mode_block_32x16":
		stats.ModeBlock32x16 = value
	case "mode_block_16x32":
		stats.ModeBlock16x32 = value
	case "mode_block_16x16":
		stats.ModeBlock16x16 = value
	case "mode_block_16x8":
		stats.ModeBlock16x8 = value
	case "mode_block_8x16":
		stats.ModeBlock8x16 = value
	case "mode_block_8x8":
		stats.ModeBlock8x8 = value
	case "mode_block_sub8":
		stats.ModeBlockSub8 = value
	case "varpart_choose_calls":
		stats.VarpartChooseCalls = value
	case "varpart_copy_hits":
		stats.VarpartCopyHits = value
	case "varpart_content_state_invalid":
		stats.VarpartContentStateInvalid = value
	case "varpart_content_state_low_sad_low_sumdiff":
		stats.VarpartContentStateLowSadLowSumdiff = value
	case "varpart_content_state_low_sad_high_sumdiff":
		stats.VarpartContentStateLowSadHighSumdiff = value
	case "varpart_content_state_high_sad_low_sumdiff":
		stats.VarpartContentStateHighSadLowSumdiff = value
	case "varpart_content_state_high_sad_high_sumdiff":
		stats.VarpartContentStateHighSadHighSumdiff = value
	case "varpart_content_state_low_var_high_sumdiff":
		stats.VarpartContentStateLowVarHighSumdiff = value
	case "varpart_content_state_very_high_sad":
		stats.VarpartContentStateVeryHighSad = value
	case "varpart_ysad_valid":
		stats.VarpartYSADValid = value
	case "varpart_ysad_select_64x64":
		stats.VarpartYSADSelect64x64 = value
	case "varpart_copy_partition_select":
		stats.VarpartCopyPartitionSelect = value
	case "varpart_force_split_64":
		stats.VarpartForceSplit64 = value
	case "varpart_force_split_32":
		stats.VarpartForceSplit32 = value
	case "varpart_force_split_16":
		stats.VarpartForceSplit16 = value
	case "varpart_force_split_16_variance":
		stats.VarpartForceSplit16Variance = value
	case "varpart_force_split_16_minmax":
		stats.VarpartForceSplit16Minmax = value
	case "varpart_threshold2_count":
		stats.VarpartThreshold2Count = value
	case "varpart_threshold2_sum":
		stats.VarpartThreshold2Sum = value
	case "varpart_var16_samples":
		stats.VarpartVar16Samples = value
	case "varpart_var16_sum":
		stats.VarpartVar16Sum = value
	case "varpart_force16_variance_sum":
		stats.VarpartForce16VarianceSum = value
	case "varpart_force16_threshold_sum":
		stats.VarpartForce16ThresholdSum = value
	case "varpart_setvt_calls":
		stats.VarpartSetVTCalls = value
	case "varpart_setvt_64x64":
		stats.VarpartSetVT64x64 = value
	case "varpart_setvt_32x32":
		stats.VarpartSetVT32x32 = value
	case "varpart_setvt_16x16":
		stats.VarpartSetVT16x16 = value
	case "varpart_setvt_8x8":
		stats.VarpartSetVT8x8 = value
	case "varpart_setvt_force_split":
		stats.VarpartSetVTForceSplit = value
	case "varpart_setvt_force_split_64x64":
		stats.VarpartSetVTForceSplit64x64 = value
	case "varpart_setvt_force_split_32x32":
		stats.VarpartSetVTForceSplit32x32 = value
	case "varpart_setvt_force_split_16x16":
		stats.VarpartSetVTForceSplit16x16 = value
	case "varpart_setvt_select":
		stats.VarpartSetVTSelect = value
	case "varpart_setvt_split":
		stats.VarpartSetVTSplit = value
	default:
		return false
	}
	return true
}

func runLibvpxDecodeBenchmark(cfg benchConfig, ivf []byte, deadlineName string, frames int) (decodeReferenceReport, error) {
	if benchCodec(cfg) == codecVP9 {
		return runLibvpxVP9DecodeBenchmark(cfg, ivf, deadlineName, frames)
	}
	threads := effectiveBenchThreads(cfg)
	if threads > 1 {
		if vpxdecBin := findSiblingVpxdec(cfg.LibvpxOracle); vpxdecBin != "" {
			return runLibvpxVpxdecDecodeBenchmark(cfg, vpxdecBin, codecVP8, ivf, deadlineName, frames)
		}
	}
	tempDir, err := os.MkdirTemp("", "govpx-decode-bench-*")
	if err != nil {
		return decodeReferenceReport{}, err
	}
	defer os.RemoveAll(tempDir)

	path := tempDir + string(os.PathSeparator) + "input.ivf"
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		return decodeReferenceReport{}, err
	}

	// Warmup invocation primes the file cache and dyld so the measured run
	// reflects steady-state subprocess overhead. We discard its output and
	// timing.
	if !cfg.libvpxWarmed {
		warm := exec.Command(cfg.LibvpxOracle, "decode-bench", path)
		warm.Stdout = nil
		warm.Stderr = nil
		_ = warm.Run()
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(cfg.LibvpxOracle, "decode-bench", path)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		return decodeReferenceReport{}, fmt.Errorf("libvpx oracle decode-bench failed: %w\nstderr:\n%s", err, stderr.Bytes())
	}
	decodedFrames := 0
	if n, perr := strconv.Atoi(strings.TrimSpace(stdout.String())); perr == nil && n > 0 {
		decodedFrames = n
	} else {
		decodedFrames = countJSONLines(stdout.Bytes())
	}
	if decodedFrames == 0 {
		return decodeReferenceReport{}, errors.New("libvpx oracle decoded zero frames")
	}
	wallNS := elapsed.Nanoseconds()
	wallPerFrame := wallNS / int64(frames)
	decodeNS := wallNS
	timingSource := "wall"
	p50 := wallPerFrame
	p95 := wallPerFrame
	p99 := wallPerFrame
	if t, ok := parseOracleBenchTiming(stderr.Bytes()); ok && t.frames > 0 && t.sumNS > 0 {
		decodeNS = t.sumNS
		timingSource = "oracle-bench"
		if t.p50NS > 0 {
			p50 = t.p50NS
		}
		if t.p95NS > 0 {
			p95 = t.p95NS
		}
		if t.p99NS > 0 {
			p99 = t.p99NS
		}
	}
	nsPerFrame := decodeNS / int64(frames)
	if nsPerFrame <= 0 {
		// Fall back so downstream divisions stay positive.
		nsPerFrame = wallPerFrame
		decodeNS = wallNS
		timingSource = "wall"
	}
	overheadNS := max(wallNS-decodeNS, 0)
	wallFPS := 0.0
	if wallPerFrame > 0 {
		wallFPS = 1e9 / float64(wallPerFrame)
	}
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	return decodeReferenceReport{
		Decoder:              "libvpx-vp8",
		Mode:                 deadlineName,
		DecodedFrames:        decodedFrames,
		NSPerFrame:           nsPerFrame,
		DecodeFPS:            1e9 / float64(nsPerFrame),
		MacroblocksPerSec:    macroblocksPerFrame * 1e9 / float64(nsPerFrame),
		CodedMegabytesPerSec: codedMegabytesPerSecond(len(ivf), decodeNS),
		LatencyNS: latencyReport{
			P50: p50,
			P95: p95,
			P99: p99,
		},
		TimingSource:         timingSource,
		WallNSPerFrame:       wallPerFrame,
		WallDecodeFPS:        wallFPS,
		SubprocessOverheadNS: overheadNS,
		ParityFlags:          []string{"-t", "1"},
	}, nil
}

func findSiblingVpxdec(oraclePath string) string {
	if oraclePath != "" {
		candidate := filepath.Join(filepath.Dir(oraclePath), "vpxdec")
		if isExecutable(candidate) {
			return candidate
		}
	}
	if path, err := exec.LookPath("vpxdec"); err == nil {
		return path
	}
	return ""
}

func runLibvpxVP9DecodeBenchmark(cfg benchConfig, ivf []byte, deadlineName string, frames int) (decodeReferenceReport, error) {
	return runLibvpxVpxdecDecodeBenchmark(cfg, cfg.LibvpxOracle, codecVP9, ivf, deadlineName, frames)
}

func runLibvpxVpxdecDecodeBenchmark(cfg benchConfig, bin string, codec string, ivf []byte, deadlineName string, frames int) (decodeReferenceReport, error) {
	tempDir, err := os.MkdirTemp("", "govpx-vpxdec-bench-*")
	if err != nil {
		return decodeReferenceReport{}, err
	}
	defer os.RemoveAll(tempDir)

	path := tempDir + string(os.PathSeparator) + "input.ivf"
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		return decodeReferenceReport{}, err
	}

	args := []string{"--codec=" + codec, "--noblit", "--summary"}
	if codec == codecVP9 {
		policy := vp9DecodeThreadPolicyForBench(cfg)
		if policy.Threads > 0 {
			args = append(args, "-t", strconv.Itoa(policy.Threads))
		}
		if policy.RowMT {
			args = append(args, "--row-mt=1")
		} else {
			args = append(args, "--row-mt=0")
		}
		if policy.LoopFilterOpt {
			args = append(args, "--lpf-opt=1")
		}
	} else {
		threads := effectiveBenchThreads(cfg)
		if threads > 0 {
			args = append(args, "-t", strconv.Itoa(threads))
		}
	}
	parityFlags := append([]string(nil), args...)
	args = append(args, path)
	if !cfg.libvpxWarmed {
		warm := exec.Command(bin, args...)
		warm.Stdout = nil
		warm.Stderr = nil
		_ = warm.Run()
	}

	var stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stderr = &stderr
	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		return decodeReferenceReport{}, fmt.Errorf("libvpx vpxdec (%s) failed: %w\nstderr:\n%s", codec, err, stderr.Bytes())
	}
	if frames <= 0 {
		return decodeReferenceReport{}, fmt.Errorf("libvpx vpxdec (%s) decoded zero frames", codec)
	}
	wallNS := elapsed.Nanoseconds()
	wallPerFrame := wallNS / int64(frames)
	if wallPerFrame <= 0 {
		wallPerFrame = 1
		wallNS = int64(frames)
	}
	decodeNS := wallNS
	decodedFrames := frames
	timingSource := "vpxdec-wall"
	if parsed, ok := parseVpxdecSummary(stderr.Bytes()); ok && parsed.totalNS > 0 {
		decodeNS = parsed.totalNS
		timingSource = "vpxdec-summary"
		if parsed.showed > 0 {
			decodedFrames = parsed.showed
		} else if parsed.decoded > 0 {
			decodedFrames = parsed.decoded
		}
	}
	nsPerFrame := decodeNS / int64(frames)
	if nsPerFrame <= 0 {
		nsPerFrame = wallPerFrame
		decodeNS = wallNS
		timingSource = "vpxdec-wall"
	}
	overheadNS := max(wallNS-decodeNS, 0)
	macroblocksPerFrame := benchmarkMacroblocks(cfg.Width, cfg.Height)
	return decodeReferenceReport{
		Decoder:              "libvpx-" + codec,
		Mode:                 deadlineName,
		DecodedFrames:        decodedFrames,
		NSPerFrame:           nsPerFrame,
		DecodeFPS:            1e9 / float64(nsPerFrame),
		MacroblocksPerSec:    macroblocksPerFrame * 1e9 / float64(nsPerFrame),
		CodedMegabytesPerSec: codedMegabytesPerSecond(len(ivf), decodeNS),
		LatencyNS: latencyReport{
			P50: nsPerFrame,
			P95: nsPerFrame,
			P99: nsPerFrame,
		},
		TimingSource:         timingSource,
		WallNSPerFrame:       wallPerFrame,
		WallDecodeFPS:        1e9 / float64(wallPerFrame),
		SubprocessOverheadNS: overheadNS,
		ParityFlags:          parityFlags,
	}, nil
}

type vpxdecSummary struct {
	decoded int
	showed  int
	totalNS int64
}

// vpxdec --summary prints a line on stderr like:
//
//	48 decoded frames/48 showed frames in 42150 us (1138.79 fps)
//
// where the microsecond count is vpxdec's internal dx_time (vpx_codec_decode +
// vpx_codec_get_frame), excluding subprocess startup and IVF file I/O.
var vpxdecSummaryRE = regexp.MustCompile(`(\d+)\s+decoded frames/(\d+)\s+showed frames in\s+(\d+)\s+us`)

func parseVpxdecSummary(stderr []byte) (vpxdecSummary, bool) {
	matches := vpxdecSummaryRE.FindAllSubmatch(stderr, -1)
	if len(matches) == 0 {
		return vpxdecSummary{}, false
	}
	last := matches[len(matches)-1]
	decoded, _ := strconv.Atoi(string(last[1]))
	showed, _ := strconv.Atoi(string(last[2]))
	us, _ := strconv.ParseInt(string(last[3]), 10, 64)
	if decoded <= 0 || us <= 0 {
		return vpxdecSummary{}, false
	}
	return vpxdecSummary{
		decoded: decoded,
		showed:  showed,
		totalNS: us * int64(time.Microsecond),
	}, true
}

type oracleBenchTiming struct {
	frames  int
	decoded int
	sumNS   int64
	loopNS  int64
	p50NS   int64
	p95NS   int64
	p99NS   int64
}

// The govpx-vpx-oracle decode-bench subcommand emits one summary line on
// stderr like
//
//	oracle-bench frames=30 decoded=30 sum_ns=2456789 loop_ns=2480010 p50_ns=78900 p95_ns=120100 p99_ns=140000
//
// where sum_ns is the cumulative per-frame decode time (excluding subprocess
// startup, file read, and IVF parsing). govpx-bench uses sum_ns as libvpx's
// ns/frame number so the comparison is decode-loop vs decode-loop instead of
// in-process call vs whole-subprocess wall time.
var oracleBenchRE = regexp.MustCompile(`oracle-bench\s+frames=(\d+)\s+decoded=(\d+)\s+sum_ns=(\d+)\s+loop_ns=(\d+)\s+p50_ns=(\d+)\s+p95_ns=(\d+)\s+p99_ns=(\d+)`)

func parseOracleBenchTiming(stderr []byte) (oracleBenchTiming, bool) {
	m := oracleBenchRE.FindSubmatch(stderr)
	if m == nil {
		return oracleBenchTiming{}, false
	}
	frames, _ := strconv.Atoi(string(m[1]))
	decoded, _ := strconv.Atoi(string(m[2]))
	sumNS, _ := strconv.ParseInt(string(m[3]), 10, 64)
	loopNS, _ := strconv.ParseInt(string(m[4]), 10, 64)
	p50, _ := strconv.ParseInt(string(m[5]), 10, 64)
	p95, _ := strconv.ParseInt(string(m[6]), 10, 64)
	p99, _ := strconv.ParseInt(string(m[7]), 10, 64)
	if frames <= 0 || sumNS <= 0 {
		return oracleBenchTiming{}, false
	}
	return oracleBenchTiming{
		frames:  frames,
		decoded: decoded,
		sumNS:   sumNS,
		loopNS:  loopNS,
		p50NS:   p50,
		p95NS:   p95,
		p99NS:   p99,
	}, true
}

func countJSONLines(out []byte) int {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}
