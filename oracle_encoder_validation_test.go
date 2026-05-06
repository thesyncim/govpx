package libgopx

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/libgopx/internal/testutil"
	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
)

type encoderValidationPattern int

const (
	encoderValidationMotion encoderValidationPattern = iota
	encoderValidationSegmented
)

type encoderValidationCase struct {
	name        string
	width       int
	height      int
	frames      int
	fps         int
	targetKbps  int
	pattern     encoderValidationPattern
	opts        EncoderOptions
	libvpxArgs  []string
	minPSNR     float64
	minSSIM     float64
	maxPSNRGap  float64
	maxSSIMGap  float64
	maxRateHigh float64
	maxRateLow  float64

	wantTokenPartition            vp8common.TokenPartition
	checkTokenPartition           bool
	checkAllTokenPartitionsActive bool
	checkSegmentationMap          bool
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
}

func TestOracleEncoderCorpusValidation(t *testing.T) {
	if os.Getenv("LIBGOPX_WITH_ORACLE") != "1" {
		t.Skip("set LIBGOPX_WITH_ORACLE=1 to run encoder oracle validation")
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
			// These quality thresholds are current-regression guards. The
			// remaining libvpx gap documents that encoder quality parity is
			// not complete yet.
			minPSNR:                       20.0,
			minSSIM:                       0.94,
			maxPSNRGap:                    30.0,
			maxSSIMGap:                    0.06,
			maxRateHigh:                   250.0,
			maxRateLow:                    95.0,
			wantTokenPartition:            vp8common.EightPartition,
			checkTokenPartition:           true,
			checkAllTokenPartitionsActive: true,
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
			// These quality thresholds are current-regression guards. The
			// remaining libvpx gap documents that encoder quality parity is
			// not complete yet.
			minPSNR:              40.0,
			minSSIM:              0.99,
			maxPSNRGap:           10.0,
			maxSSIMGap:           0.02,
			maxRateHigh:          250.0,
			maxRateLow:           95.0,
			checkSegmentationMap: true,
			checkInterFrames:     true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sources := encoderValidationFrames(tc)
			got := encodeLibgopxValidationCorpus(t, tc, sources)
			wantChecksums := runLibvpxChecksumOracle(t, oracle, got.ivf)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			assertFrameChecksumsEqual(t, "libgopx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertLibgopxEncoderValidationFeatures(t, got.ivf, tc)
			assertEncoderValidationQuality(t, "libgopx", got.quality, tc.minPSNR, tc.minSSIM)
			assertEncoderValidationRate(t, "libgopx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, sources)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, sources)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(sources)*testutil.IVFFrameHeaderSize, tc.fps, len(sources))
			t.Logf("libgopx quality psnr=%.2f ssim=%.4f bitrate=%.1f kbps; libvpx quality psnr=%.2f ssim=%.4f bitrate=%.1f kbps",
				got.quality.PSNR, got.quality.SSIM, got.outputKbps, libvpxQuality.PSNR, libvpxQuality.SSIM, libvpxOutputKbps)
			assertEncoderValidationQuality(t, "libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM)
			assertEncoderValidationRate(t, "libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			if got.quality.PSNR+tc.maxPSNRGap < libvpxQuality.PSNR {
				t.Fatalf("libgopx PSNR = %.2f dB, libvpx = %.2f dB, allowed gap %.2f dB", got.quality.PSNR, libvpxQuality.PSNR, tc.maxPSNRGap)
			}
			if got.quality.SSIM+tc.maxSSIMGap < libvpxQuality.SSIM {
				t.Fatalf("libgopx SSIM = %.4f, libvpx = %.4f, allowed gap %.4f", got.quality.SSIM, libvpxQuality.SSIM, tc.maxSSIMGap)
			}
		})
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
		default:
			frames[i] = rateControlTestFrame(tc.width, tc.height, i)
		}
	}
	return frames
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

func encodeLibgopxValidationCorpus(t *testing.T, tc encoderValidationCase, sources []Image) encoderValidationResult {
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
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--good",
		"--cpu-used=0",
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

func assertLibgopxEncoderValidationFeatures(t *testing.T, ivf []byte, tc encoderValidationCase) {
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
	sawSegmentation := !tc.checkSegmentationMap
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
		if tc.checkSegmentationMap {
			if err := dec.Decode(frame.Data); err != nil {
				t.Fatalf("Decode frame %d returned error while checking encoder features: %v", frameIndex, err)
			}
			for _, segmentID := range dec.segmentMap {
				if segmentID != 0 {
					sawSegmentation = true
					break
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
	for i := range sources {
		psnrSum += encoderValidationImagePSNR(sources[i], decoded[i])
		ssimSum += encoderValidationImageSSIM(sources[i], decoded[i])
	}
	return encoderQualityMetrics{
		PSNR:   psnrSum / float64(len(sources)),
		SSIM:   ssimSum / float64(len(sources)),
		Frames: len(sources),
	}
}

func assertEncoderValidationQuality(t *testing.T, label string, q encoderQualityMetrics, minPSNR float64, minSSIM float64) {
	t.Helper()
	if q.Frames == 0 {
		t.Fatalf("%s quality frames = 0", label)
	}
	if q.PSNR < minPSNR {
		t.Fatalf("%s PSNR = %.2f dB, want >= %.2f dB", label, q.PSNR, minPSNR)
	}
	if q.SSIM < minSSIM {
		t.Fatalf("%s SSIM = %.4f, want >= %.4f", label, q.SSIM, minSSIM)
	}
}

func assertEncoderValidationRate(t *testing.T, label string, outputKbps float64, targetKbps int, maxLowPct float64, maxHighPct float64) {
	t.Helper()
	errorPct := (outputKbps - float64(targetKbps)) * 100 / float64(targetKbps)
	if errorPct < -maxLowPct || errorPct > maxHighPct {
		t.Fatalf("%s output bitrate = %.1f kbps target=%d error=%.1f%%, allowed -%.1f%%/+%.1f%%", label, outputKbps, targetKbps, errorPct, maxLowPct, maxHighPct)
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
