package govpx

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

type encoderValidationPattern int

const (
	encoderValidationMotion encoderValidationPattern = iota
	encoderValidationSegmented
	encoderValidationPanning
)

type encoderValidationCase struct {
	name            string
	width           int
	height          int
	frames          int
	fps             int
	targetKbps      int
	pattern         encoderValidationPattern
	opts            EncoderOptions
	libvpxArgs      []string
	minPSNR         float64
	minSSIM         float64
	minFramePSNR    float64
	minFrameSSIM    float64
	maxPSNRGap      float64
	maxSSIMGap      float64
	maxFramePSNRGap float64
	maxFrameSSIMGap float64
	maxRateHigh     float64
	maxRateLow      float64
	maxRateGapPct   float64

	wantTokenPartition            vp8common.TokenPartition
	checkTokenPartition           bool
	checkAllTokenPartitionsActive bool
	checkSegmentationHeader       bool
	checkSegmentationMap          bool
	checkBPredModes               bool
	checkInterFrames              bool
}

type encoderValidationResult struct {
	ivf        []byte
	quality    encoderQualityMetrics
	outputKbps float64
}

type encoderQualityMetrics struct {
	PSNR   float64
	SSIM   float64
	Frames int

	Frame []encoderFrameQualityMetrics
}

type encoderFrameQualityMetrics struct {
	Index int
	PSNR  float64
	SSIM  float64
}

func TestOracleEncoderCorpusValidation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle validation")
	}
	oracle := findChecksumOracle(t)
	vpxenc := findVpxenc(t)

	cases := []encoderValidationCase{
		{
			name:       "motion-eight-token-partitions",
			width:      64,
			height:     128,
			frames:     18,
			fps:        30,
			targetKbps: 700,
			pattern:    encoderValidationMotion,
			opts: encoderValidationOptions(64, 128, 30, 700, func(opts *EncoderOptions) {
				opts.TokenPartitions = int(vp8common.EightPartition)
			}),
			libvpxArgs: []string{"--token-parts=3"},
			// Realtime mode follows the cheaper libvpx pick_inter path; the
			// remaining gap is mostly mode-loop and rate-control parity.
			minPSNR:                       48.0,
			minSSIM:                       0.999,
			minFramePSNR:                  48.25,
			minFrameSSIM:                  0.999,
			maxPSNRGap:                    0.8,
			maxSSIMGap:                    0.001,
			maxFramePSNRGap:               1.5,
			maxFrameSSIMGap:               0.002,
			maxRateHigh:                   250.0,
			maxRateLow:                    95.0,
			maxRateGapPct:                 35.0,
			wantTokenPartition:            vp8common.EightPartition,
			checkTokenPartition:           true,
			checkAllTokenPartitionsActive: true,
			checkBPredModes:               true,
			checkInterFrames:              true,
		},
		{
			name:       "static-segmentation",
			width:      64,
			height:     64,
			frames:     18,
			fps:        30,
			targetKbps: 500,
			pattern:    encoderValidationSegmented,
			opts: encoderValidationOptions(64, 64, 30, 500, func(opts *EncoderOptions) {
				opts.StaticThreshold = 1
				opts.MaxQuantizer = 56
			}),
			libvpxArgs: []string{"--static-thresh=1"},
			// Static-threshold encode-breakout is bounded by the libvpx oracle,
			// while full mode RD and rate parity remain open.
			minPSNR:                 49.0,
			minSSIM:                 0.999,
			minFramePSNR:            48.75,
			minFrameSSIM:            0.999,
			maxPSNRGap:              0.5,
			maxSSIMGap:              0.001,
			maxFramePSNRGap:         0.55,
			maxFrameSSIMGap:         0.002,
			maxRateHigh:             250.0,
			maxRateLow:              95.0,
			maxRateGapPct:           5.0,
			checkSegmentationHeader: true,
			checkInterFrames:        true,
		},
		qualityValidationCase("best-quality-panning", DeadlineBestQuality, 0, 47.4, 47.1, 0.6, 0.7, 8.0),
		qualityValidationCase("good-quality-rd-panning", DeadlineGoodQuality, 3, 47.9, 47.6, 1.0, 1.2, 5.0),
		qualityValidationCase("good-quality-fast-panning", DeadlineGoodQuality, 4, 47.9, 47.6, 1.0, 1.2, 5.0),
		realtimeSpeedValidationCase(0, 47.9, 47.6, 0.8, 0.8, 12.0),
		realtimeSpeedValidationCase(3, 47.9, 47.6, 0.8, 0.8, 8.0),
		realtimeSpeedValidationCase(4, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(5, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(8, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(9, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(15, 47.9, 47.6, 0.4, 0.4, 5.0),
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sources := encoderValidationFrames(tc)
			got := encodeGopvxValidationCorpus(t, tc, sources)
			wantChecksums := runLibvpxChecksumOracle(t, oracle, got.ivf)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			assertFrameChecksumsEqual(t, "govpx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertGopvxEncoderValidationFeatures(t, got.ivf, tc)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, sources)
			libvpxWantChecksums := runLibvpxChecksumOracle(t, oracle, libvpxIVF)
			libvpxGotChecksums := decodeIVFChecksums(t, libvpxIVF)
			assertFrameChecksumsEqual(t, "libvpx encode decoded by govpx", libvpxGotChecksums, libvpxWantChecksums)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, sources)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(sources)*testutil.IVFFrameHeaderSize, tc.fps, len(sources))
			logEncoderValidationQuality(t, got.quality, got.outputKbps, libvpxQuality, libvpxOutputKbps)

			assertEncoderValidationQuality(t, "govpx", got.quality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "govpx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQuality(t, "libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQualityGap(t, got.quality, libvpxQuality, tc)
			assertEncoderValidationRateGap(t, got.outputKbps, libvpxOutputKbps, tc)
		})
	}
}

func qualityValidationCase(name string, deadline Deadline, cpuUsed int, minPSNR float64, minFramePSNR float64, maxPSNRGap float64, maxFramePSNRGap float64, maxRateGapPct float64) encoderValidationCase {
	return encoderValidationCase{
		name:       name,
		width:      64,
		height:     64,
		frames:     10,
		fps:        30,
		targetKbps: 700,
		pattern:    encoderValidationPanning,
		opts: encoderValidationOptions(64, 64, 30, 700, func(opts *EncoderOptions) {
			opts.Deadline = deadline
			opts.CpuUsed = cpuUsed
		}),
		minPSNR:          minPSNR,
		minSSIM:          0.998,
		minFramePSNR:     minFramePSNR,
		minFrameSSIM:     0.997,
		maxPSNRGap:       maxPSNRGap,
		maxSSIMGap:       0.002,
		maxFramePSNRGap:  maxFramePSNRGap,
		maxFrameSSIMGap:  0.004,
		maxRateHigh:      260.0,
		maxRateLow:       80.0,
		maxRateGapPct:    maxRateGapPct,
		checkInterFrames: true,
	}
}

func realtimeSpeedValidationCase(cpuUsed int, minPSNR float64, minFramePSNR float64, maxPSNRGap float64, maxFramePSNRGap float64, maxRateGapPct float64) encoderValidationCase {
	return encoderValidationCase{
		name:       "realtime-cpu-used-" + strconv.Itoa(cpuUsed) + "-panning",
		width:      64,
		height:     64,
		frames:     12,
		fps:        30,
		targetKbps: 700,
		pattern:    encoderValidationPanning,
		opts: encoderValidationOptions(64, 64, 30, 700, func(opts *EncoderOptions) {
			opts.CpuUsed = cpuUsed
		}),
		minPSNR:          minPSNR,
		minSSIM:          0.998,
		minFramePSNR:     minFramePSNR,
		minFrameSSIM:     0.997,
		maxPSNRGap:       maxPSNRGap,
		maxSSIMGap:       0.002,
		maxFramePSNRGap:  maxFramePSNRGap,
		maxFrameSSIMGap:  0.004,
		maxRateHigh:      260.0,
		maxRateLow:       80.0,
		maxRateGapPct:    maxRateGapPct,
		checkInterFrames: true,
	}
}

func encoderValidationOptions(width int, height int, fps int, targetKbps int, mutate func(*EncoderOptions)) EncoderOptions {
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
	if mutate != nil {
		mutate(&opts)
	}
	return opts
}

func encoderValidationFrames(tc encoderValidationCase) []Image {
	frames := make([]Image, tc.frames)
	for i := range frames {
		switch tc.pattern {
		case encoderValidationSegmented:
			frames[i] = encoderValidationSegmentedFrame(tc.width, tc.height, i)
		case encoderValidationPanning:
			frames[i] = encoderValidationPanningFrame(tc.width, tc.height, i)
		default:
			frames[i] = rateControlTestFrame(tc.width, tc.height, i)
		}
	}
	return frames
}

func encoderValidationPanningFrame(width int, height int, index int) Image {
	img := testImage(width, height)
	xoff := index * 2
	yoff := index
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := x + xoff
			srcY := y + yoff
			img.Y[y*img.YStride+x] = byte(32 + ((srcY*7 + srcX*11 + (srcX/8)*(srcY/8)*13) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < uvHeight; y++ {
		for x := 0; x < uvWidth; x++ {
			srcX := x + xoff/2
			srcY := y + yoff/2
			img.U[y*img.UStride+x] = byte(96 + ((srcX*5 + srcY*3) & 63))
			img.V[y*img.VStride+x] = byte(144 + ((srcX*2 + srcY*7) & 63))
		}
	}
	return img
}

func encoderValidationSegmentedFrame(width int, height int, index int) Image {
	img := testImage(width, height)
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			mbX := x >> 4
			mbY := y >> 4
			if (mbX+mbY)&1 == 0 {
				img.Y[y*img.YStride+x] = byte(72 + (index & 3))
			} else {
				img.Y[y*img.YStride+x] = byte(32 + ((x*9 + y*5 + index*11) & 191))
			}
		}
	}
	for y := 0; y < uvHeight; y++ {
		for x := 0; x < uvWidth; x++ {
			img.U[y*img.UStride+x] = byte(96 + ((x*3 + y + index*5) & 63))
			img.V[y*img.VStride+x] = byte(144 + ((x + y*3 + index*7) & 63))
		}
	}
	return img
}

func encodeGopvxValidationCorpus(t *testing.T, tc encoderValidationCase, sources []Image) encoderValidationResult {
	t.Helper()
	enc, err := NewVP8Encoder(tc.opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, tc.width*tc.height*3)
	packets := make([][]byte, 0, len(sources))
	outputBytes := 0
	for i, source := range sources {
		result, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto frame %d dropped, want full validation corpus", i)
		}
		pkt := append([]byte(nil), result.Data...)
		packets = append(packets, pkt)
		outputBytes += result.SizeBytes
	}
	ivf := makeIVF(tc.width, tc.height, uint32(tc.fps), 1, packets)
	decoded := decodeIVFFrames(t, ivf)
	return encoderValidationResult{
		ivf:        ivf,
		quality:    qualityMetricsForFrames(t, sources, decoded),
		outputKbps: encoderValidationOutputKbps(outputBytes, tc.fps, len(sources)),
	}
}

func encodeLibvpxValidationCorpus(t *testing.T, vpxenc string, tc encoderValidationCase, sources []Image) []byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, tc.name+".yuv")
	ivfPath := filepath.Join(dir, tc.name+".ivf")
	writeEncoderValidationI420(t, yuvPath, sources)
	deadlineArg := "--good"
	switch tc.opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(tc.opts.CpuUsed),
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(tc.targetKbps),
		"--min-q=4",
		"--max-q=56",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--i420",
		"--width=" + strconv.Itoa(tc.width),
		"--height=" + strconv.Itoa(tc.height),
		"--fps=" + strconv.Itoa(tc.fps) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
	}
	args = append(args, tc.libvpxArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxenc, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, out)
	}
	ivf, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("ReadFile %s returned error: %v", ivfPath, err)
	}
	return ivf
}

func writeEncoderValidationI420(t *testing.T, path string, frames []Image) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create %s returned error: %v", path, err)
	}
	defer file.Close()
	for i, frame := range frames {
		if err := writeEncoderValidationPlane(file, frame.Y, frame.YStride, frame.Width, frame.Height); err != nil {
			t.Fatalf("write frame %d Y returned error: %v", i, err)
		}
		uvWidth := (frame.Width + 1) >> 1
		uvHeight := (frame.Height + 1) >> 1
		if err := writeEncoderValidationPlane(file, frame.U, frame.UStride, uvWidth, uvHeight); err != nil {
			t.Fatalf("write frame %d U returned error: %v", i, err)
		}
		if err := writeEncoderValidationPlane(file, frame.V, frame.VStride, uvWidth, uvHeight); err != nil {
			t.Fatalf("write frame %d V returned error: %v", i, err)
		}
	}
}

func writeEncoderValidationPlane(file *os.File, plane []byte, stride int, width int, height int) error {
	for row := 0; row < height; row++ {
		if _, err := file.Write(plane[row*stride : row*stride+width]); err != nil {
			return err
		}
	}
	return nil
}

func decodeIVFFrames(t *testing.T, ivf []byte) []Image {
	t.Helper()
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	var frames []Image
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		if err := dec.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", inputIndex, err)
		}
		if img, ok := dec.NextFrame(); ok {
			frames = append(frames, cloneImage(img))
		}
		offset = next
	}
	return frames
}

func assertGopvxEncoderValidationFeatures(t *testing.T, ivf []byte, tc encoderValidationCase) {
	t.Helper()
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	previousQuant := vp8dec.QuantHeader{}
	sawTokenPartition := !tc.checkTokenPartition
	sawAllTokenPartitionsActive := !tc.checkAllTokenPartitionsActive
	sawSegmentationHeader := !tc.checkSegmentationHeader
	sawSegmentation := !tc.checkSegmentationMap
	sawBPred := !tc.checkBPredModes
	sawInter := !tc.checkInterFrames
	for frameIndex := 0; offset < len(ivf); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, frameIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", frameIndex, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", frameIndex, err)
		}
		if !info.KeyFrame {
			sawInter = true
		}
		header, state, err := vp8dec.ParseStateHeader(frame.Data, previousQuant)
		if err != nil {
			t.Fatalf("ParseStateHeader frame %d returned error: %v", frameIndex, err)
		}
		if tc.checkTokenPartition && state.TokenPartition == tc.wantTokenPartition {
			sawTokenPartition = true
		}
		if tc.checkSegmentationHeader && state.Segmentation.Enabled {
			sawSegmentationHeader = true
		}
		if tc.checkAllTokenPartitionsActive {
			var layout vp8dec.PartitionLayout
			if err := vp8dec.ParsePartitionLayout(frame.Data, header, state.TokenPartition, &layout); err != nil {
				t.Fatalf("ParsePartitionLayout frame %d returned error: %v", frameIndex, err)
			}
			allActive := layout.TokenCount == int(1<<uint(tc.wantTokenPartition))
			for i := 0; i < layout.TokenCount; i++ {
				if len(layout.Tokens[i]) <= 1 {
					allActive = false
					break
				}
			}
			if allActive {
				sawAllTokenPartitionsActive = true
			}
		}
		if tc.checkSegmentationMap || tc.checkBPredModes {
			if err := dec.Decode(frame.Data); err != nil {
				t.Fatalf("Decode frame %d returned error while checking encoder features: %v", frameIndex, err)
			}
			if tc.checkSegmentationMap {
				for _, segmentID := range dec.segmentMap {
					if segmentID != 0 {
						sawSegmentation = true
						break
					}
				}
			}
			if tc.checkBPredModes {
				for _, mode := range dec.modes {
					if mode.Mode == vp8common.BPred || mode.Is4x4 {
						sawBPred = true
						break
					}
				}
			}
		}
		previousQuant = state.Quant
		offset = next
	}
	if !sawTokenPartition {
		t.Fatalf("encoded corpus did not contain token partition %d", tc.wantTokenPartition)
	}
	if !sawAllTokenPartitionsActive {
		t.Fatalf("encoded corpus did not exercise all token partitions with active payload")
	}
	if !sawSegmentation {
		t.Fatalf("encoded corpus did not contain a nonzero segmentation map")
	}
	if !sawSegmentationHeader {
		t.Fatalf("encoded corpus did not contain segmentation headers")
	}
	if !sawBPred {
		t.Fatalf("encoded corpus did not contain B_PRED macroblocks")
	}
	if !sawInter {
		t.Fatalf("encoded corpus did not contain interframes")
	}
}

func qualityMetricsForIVF(t *testing.T, ivf []byte, sources []Image) encoderQualityMetrics {
	t.Helper()
	return qualityMetricsForFrames(t, sources, decodeIVFFrames(t, ivf))
}

func qualityMetricsForFrames(t *testing.T, sources []Image, decoded []Image) encoderQualityMetrics {
	t.Helper()
	if len(decoded) != len(sources) {
		t.Fatalf("decoded frame count = %d, want %d source frames", len(decoded), len(sources))
	}
	psnrSum := 0.0
	ssimSum := 0.0
	frames := make([]encoderFrameQualityMetrics, len(sources))
	for i := range sources {
		framePSNR := encoderValidationImagePSNR(sources[i], decoded[i])
		frameSSIM := encoderValidationImageSSIM(sources[i], decoded[i])
		frames[i] = encoderFrameQualityMetrics{Index: i, PSNR: framePSNR, SSIM: frameSSIM}
		psnrSum += framePSNR
		ssimSum += frameSSIM
	}
	return encoderQualityMetrics{
		PSNR:   psnrSum / float64(len(sources)),
		SSIM:   ssimSum / float64(len(sources)),
		Frames: len(sources),
		Frame:  frames,
	}
}

func assertEncoderValidationQuality(t *testing.T, label string, q encoderQualityMetrics, minPSNR float64, minSSIM float64, minFramePSNR float64, minFrameSSIM float64) {
	t.Helper()
	if q.Frames == 0 {
		t.Fatalf("%s quality frames = 0", label)
	}
	if len(q.Frame) != q.Frames {
		t.Fatalf("%s quality per-frame metrics = %d, want %d", label, len(q.Frame), q.Frames)
	}
	if q.PSNR < minPSNR {
		t.Fatalf("%s PSNR = %.2f dB, want >= %.2f dB", label, q.PSNR, minPSNR)
	}
	if q.SSIM < minSSIM {
		t.Fatalf("%s SSIM = %.4f, want >= %.4f", label, q.SSIM, minSSIM)
	}
	for _, frame := range q.Frame {
		if frame.PSNR < minFramePSNR {
			t.Fatalf("%s frame %d PSNR = %.2f dB, want >= %.2f dB", label, frame.Index, frame.PSNR, minFramePSNR)
		}
		if frame.SSIM < minFrameSSIM {
			t.Fatalf("%s frame %d SSIM = %.4f, want >= %.4f", label, frame.Index, frame.SSIM, minFrameSSIM)
		}
	}
}

func assertEncoderValidationQualityGap(t *testing.T, got encoderQualityMetrics, libvpx encoderQualityMetrics, tc encoderValidationCase) {
	t.Helper()
	if got.qualityPSNRGap(libvpx) > tc.maxPSNRGap {
		t.Fatalf("govpx PSNR = %.2f dB, libvpx = %.2f dB, allowed gap %.2f dB", got.PSNR, libvpx.PSNR, tc.maxPSNRGap)
	}
	if got.qualitySSIMGap(libvpx) > tc.maxSSIMGap {
		t.Fatalf("govpx SSIM = %.4f, libvpx = %.4f, allowed gap %.4f", got.SSIM, libvpx.SSIM, tc.maxSSIMGap)
	}
	if len(got.Frame) != len(libvpx.Frame) {
		t.Fatalf("govpx per-frame quality count = %d, libvpx = %d", len(got.Frame), len(libvpx.Frame))
	}
	for i := range got.Frame {
		psnrGap := libvpx.Frame[i].PSNR - got.Frame[i].PSNR
		if psnrGap > tc.maxFramePSNRGap {
			t.Fatalf("frame %d govpx PSNR = %.2f dB, libvpx = %.2f dB, allowed gap %.2f dB", i, got.Frame[i].PSNR, libvpx.Frame[i].PSNR, tc.maxFramePSNRGap)
		}
		ssimGap := libvpx.Frame[i].SSIM - got.Frame[i].SSIM
		if ssimGap > tc.maxFrameSSIMGap {
			t.Fatalf("frame %d govpx SSIM = %.4f, libvpx = %.4f, allowed gap %.4f", i, got.Frame[i].SSIM, libvpx.Frame[i].SSIM, tc.maxFrameSSIMGap)
		}
	}
}

func (q encoderQualityMetrics) qualityPSNRGap(libvpx encoderQualityMetrics) float64 {
	return libvpx.PSNR - q.PSNR
}

func (q encoderQualityMetrics) qualitySSIMGap(libvpx encoderQualityMetrics) float64 {
	return libvpx.SSIM - q.SSIM
}

func logEncoderValidationQuality(t *testing.T, got encoderQualityMetrics, gotKbps float64, libvpx encoderQualityMetrics, libvpxKbps float64) {
	t.Helper()
	gotWorstPSNR, libvpxWorstPSNR := worstEncoderValidationPSNR(got), worstEncoderValidationPSNR(libvpx)
	gotWorstSSIM, libvpxWorstSSIM := worstEncoderValidationSSIM(got), worstEncoderValidationSSIM(libvpx)
	maxPSNRGapIndex, maxPSNRGap := maxEncoderValidationFramePSNRGap(got, libvpx)
	maxSSIMGapIndex, maxSSIMGap := maxEncoderValidationFrameSSIMGap(got, libvpx)
	t.Logf("govpx quality psnr=%.2f ssim=%.4f bitrate=%.1f kbps worst_psnr=f%d/%.2f worst_ssim=f%d/%.4f; libvpx quality psnr=%.2f ssim=%.4f bitrate=%.1f kbps worst_psnr=f%d/%.2f worst_ssim=f%d/%.4f max_frame_gap=psnr:f%d/%.2f ssim:f%d/%.4f",
		got.PSNR, got.SSIM, gotKbps, gotWorstPSNR.Index, gotWorstPSNR.PSNR, gotWorstSSIM.Index, gotWorstSSIM.SSIM,
		libvpx.PSNR, libvpx.SSIM, libvpxKbps, libvpxWorstPSNR.Index, libvpxWorstPSNR.PSNR, libvpxWorstSSIM.Index, libvpxWorstSSIM.SSIM,
		maxPSNRGapIndex, maxPSNRGap, maxSSIMGapIndex, maxSSIMGap)
}

func worstEncoderValidationPSNR(q encoderQualityMetrics) encoderFrameQualityMetrics {
	if len(q.Frame) == 0 {
		return encoderFrameQualityMetrics{}
	}
	worst := q.Frame[0]
	for _, frame := range q.Frame[1:] {
		if frame.PSNR < worst.PSNR {
			worst = frame
		}
	}
	return worst
}

func worstEncoderValidationSSIM(q encoderQualityMetrics) encoderFrameQualityMetrics {
	if len(q.Frame) == 0 {
		return encoderFrameQualityMetrics{}
	}
	worst := q.Frame[0]
	for _, frame := range q.Frame[1:] {
		if frame.SSIM < worst.SSIM {
			worst = frame
		}
	}
	return worst
}

func maxEncoderValidationFramePSNRGap(got encoderQualityMetrics, libvpx encoderQualityMetrics) (int, float64) {
	maxIndex := -1
	maxGap := 0.0
	for i := range got.Frame {
		gap := libvpx.Frame[i].PSNR - got.Frame[i].PSNR
		if gap > maxGap {
			maxIndex = i
			maxGap = gap
		}
	}
	return maxIndex, maxGap
}

func maxEncoderValidationFrameSSIMGap(got encoderQualityMetrics, libvpx encoderQualityMetrics) (int, float64) {
	maxIndex := -1
	maxGap := 0.0
	for i := range got.Frame {
		gap := libvpx.Frame[i].SSIM - got.Frame[i].SSIM
		if gap > maxGap {
			maxIndex = i
			maxGap = gap
		}
	}
	return maxIndex, maxGap
}

func assertEncoderValidationRate(t *testing.T, label string, outputKbps float64, targetKbps int, maxLowPct float64, maxHighPct float64) {
	t.Helper()
	errorPct := (outputKbps - float64(targetKbps)) * 100 / float64(targetKbps)
	if errorPct < -maxLowPct || errorPct > maxHighPct {
		t.Fatalf("%s output bitrate = %.1f kbps target=%d error=%.1f%%, allowed -%.1f%%/+%.1f%%", label, outputKbps, targetKbps, errorPct, maxLowPct, maxHighPct)
	}
}

func assertEncoderValidationRateGap(t *testing.T, gotKbps float64, libvpxKbps float64, tc encoderValidationCase) {
	t.Helper()
	if tc.maxRateGapPct <= 0 {
		return
	}
	if libvpxKbps <= 0 {
		t.Fatalf("libvpx output bitrate = %.1f kbps, cannot compute direct rate gap", libvpxKbps)
	}
	gapPct := math.Abs(gotKbps-libvpxKbps) * 100 / libvpxKbps
	if gapPct > tc.maxRateGapPct {
		t.Fatalf("govpx output bitrate = %.1f kbps, libvpx = %.1f kbps, direct gap %.1f%% exceeds %.1f%%",
			gotKbps, libvpxKbps, gapPct, tc.maxRateGapPct)
	}
}

func encoderValidationOutputKbps(outputBytes int, fps int, frames int) float64 {
	return float64(outputBytes*8*fps) / float64(frames*1000)
}

func encoderValidationImagePSNR(src Image, dst Image) float64 {
	sse, count := encoderValidationPlaneSSE(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSE, uCount := encoderValidationPlaneSSE(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSE, vCount := encoderValidationPlaneSSE(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	sse += uSSE + vSSE
	count += uCount + vCount
	if sse == 0 {
		return 100
	}
	mse := float64(sse) / float64(count)
	return 10 * math.Log10((255*255)/mse)
}

func encoderValidationPlaneSSE(a []byte, aStride int, b []byte, bStride int, width int, height int) (int64, int) {
	var sse int64
	count := width * height
	for row := 0; row < height; row++ {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := 0; col < width; col++ {
			d := int(aRow[col]) - int(bRow[col])
			sse += int64(d * d)
		}
	}
	return sse, count
}

func encoderValidationImageSSIM(src Image, dst Image) float64 {
	ssim, count := encoderValidationPlaneSSIM(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSIM, uCount := encoderValidationPlaneSSIM(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSIM, vCount := encoderValidationPlaneSSIM(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	total := count + uCount + vCount
	if total == 0 {
		return 0
	}
	return (ssim*float64(count) + uSSIM*float64(uCount) + vSSIM*float64(vCount)) / float64(total)
}

func encoderValidationPlaneSSIM(a []byte, aStride int, b []byte, bStride int, width int, height int) (float64, int) {
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
