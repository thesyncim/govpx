package govpx

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestOracleBenchWorkloadProductionGaps pins the public govpx-bench workloads
// against the uninstrumented libvpx vpxenc binary. This is not byte parity:
// the bench path intentionally tracks production byte-rate and quality gaps
// for the synthetic bench source and WebRTC-style buffer model.
func TestOracleBenchWorkloadProductionGaps(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run production bench gap checks")
	}
	vpxenc := findVpxenc(t)

	cases := []struct {
		name            string
		width           int
		height          int
		frames          int
		fps             int
		targetKbps      int
		deadline        Deadline
		deadlineArg     string
		cpuUsed         int
		maxByteRatioGap float64
		maxPSNRGap      float64
		maxSSIMGap      float64
	}{
		{
			name:            "govpx-bench-rt-720p-cpu8-2mbps",
			width:           1280,
			height:          720,
			frames:          30,
			fps:             30,
			targetKbps:      2000,
			deadline:        DeadlineRealtime,
			deadlineArg:     "--rt",
			cpuUsed:         8,
			maxByteRatioGap: 0.04,
			maxPSNRGap:      0.20,
			maxSSIMGap:      0.003,
		},
		{
			name:            "govpx-bench-good-1080p-cpu8-8mbps",
			width:           1920,
			height:          1080,
			frames:          30,
			fps:             30,
			targetKbps:      8000,
			deadline:        DeadlineGoodQuality,
			deadlineArg:     "--good",
			cpuUsed:         8,
			maxByteRatioGap: 0.03,
			maxPSNRGap:      0.55,
			maxSSIMGap:      0.011,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := benchWorkloadEncoderOptions(tc.width, tc.height, tc.fps, tc.targetKbps, tc.deadline, tc.cpuUsed)
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = realtimeBenchNoiseFrame(tc.width, tc.height, i)
			}

			govpxPackets := encodeBenchWorkloadWithGovpx(t, opts, sources)
			govpxBytes := totalPacketBytes(govpxPackets)
			libvpxIVF := encodeBenchWorkloadWithVpxenc(t, vpxenc, tc.name, opts, tc.targetKbps, tc.deadlineArg, sources)
			libvpxBytes, libvpxCount := parseIVFFramePayloadSizes(t, libvpxIVF)
			if len(govpxPackets) != libvpxCount {
				t.Fatalf("encoded frames govpx=%d libvpx=%d, want same count from matching config", len(govpxPackets), libvpxCount)
			}

			govpxQuality := qualityMetricsForVP8Packets(t, govpxPackets, sources)
			libvpxQuality := qualityMetricsForIVFByTimestamp(t, libvpxIVF, sources)
			byteRatio := float64(govpxBytes) / float64(libvpxBytes)
			govpxKbps := encoderValidationOutputKbps(govpxBytes, tc.fps, tc.frames)
			libvpxKbps := encoderValidationOutputKbps(libvpxBytes, tc.fps, tc.frames)
			psnrGap := govpxQuality.PSNR - libvpxQuality.PSNR
			ssimGap := govpxQuality.SSIM - libvpxQuality.SSIM
			t.Logf("%s bytes: govpx=%d libvpx=%d ratio=%.4f", tc.name, govpxBytes, libvpxBytes, byteRatio)
			t.Logf("%s kbps: govpx=%.2f libvpx=%.2f target=%d", tc.name, govpxKbps, libvpxKbps, tc.targetKbps)
			t.Logf("%s quality: govpx PSNR=%.2f SSIM=%.5f libvpx PSNR=%.2f SSIM=%.5f gap PSNR=%+.2f SSIM=%+.5f",
				tc.name, govpxQuality.PSNR, govpxQuality.SSIM, libvpxQuality.PSNR, libvpxQuality.SSIM, psnrGap, ssimGap)
			if math.Abs(byteRatio-1.0) > tc.maxByteRatioGap {
				t.Fatalf("%s byte ratio = %.4f, want within %.1f%% of uninstrumented libvpx", tc.name, byteRatio, tc.maxByteRatioGap*100)
			}
			if math.Abs(psnrGap) > tc.maxPSNRGap {
				t.Fatalf("%s PSNR gap = %.2f dB, want within %.2f dB of uninstrumented libvpx", tc.name, psnrGap, tc.maxPSNRGap)
			}
			if math.Abs(ssimGap) > tc.maxSSIMGap {
				t.Fatalf("%s SSIM gap = %.5f, want within %.5f of uninstrumented libvpx", tc.name, ssimGap, tc.maxSSIMGap)
			}
		})
	}
}

type benchWorkloadPacket struct {
	data        []byte
	sourceIndex int
}

func benchWorkloadEncoderOptions(width, height, fps, targetKbps int, deadline Deadline, cpuUsed int) EncoderOptions {
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            deadline,
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    fps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		UndershootPct:       100,
		OvershootPct:        15,
		Threads:             1,
		TokenPartitions:     0,
	}
	if deadline == DeadlineRealtime {
		opts.MinQuantizer = 2
		opts.KeyFrameInterval = 3000
		opts.BufferSizeMs = 1000
		opts.BufferInitialSizeMs = 500
		opts.BufferOptimalSizeMs = 600
		opts.MaxIntraBitratePct = webrtcBenchMaxIntraTargetPct(600, fps)
		opts.DropFrameAllowed = true
		opts.DropFrameWaterMark = 30
		opts.NoiseSensitivity = 4
		opts.StaticThreshold = 1
	}
	return opts
}

func webrtcBenchMaxIntraTargetPct(maxIntraTarget int, fps int) int {
	if fps <= 0 {
		fps = 30
	}
	return max(300, maxIntraTarget*fps/20)
}

func encodeBenchWorkloadWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) []benchWorkloadPacket {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	packets := make([]benchWorkloadPacket, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		packets = append(packets, benchWorkloadPacket{
			data:        append([]byte(nil), buf[:result.SizeBytes]...),
			sourceIndex: i,
		})
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			continue
		}
		packets = append(packets, benchWorkloadPacket{
			data:        append([]byte(nil), buf[:result.SizeBytes]...),
			sourceIndex: len(sources) - 1,
		})
	}
	return packets
}

func encodeBenchWorkloadWithVpxenc(t *testing.T, vpxenc string, name string, opts EncoderOptions, targetKbps int, deadlineArg string, sources []Image) []byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	writeEncoderValidationI420(t, yuvPath, sources)
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=1",
		"--lag-in-frames=0",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--kf-max-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--buf-sz=" + strconv.Itoa(opts.BufferSizeMs),
		"--buf-initial-sz=" + strconv.Itoa(opts.BufferInitialSizeMs),
		"--buf-optimal-sz=" + strconv.Itoa(opts.BufferOptimalSizeMs),
		"--undershoot-pct=" + strconv.Itoa(opts.UndershootPct),
		"--overshoot-pct=" + strconv.Itoa(opts.OvershootPct),
		"--threads=1",
		"--token-parts=0",
		"--noise-sensitivity=" + strconv.Itoa(opts.NoiseSensitivity),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
	}
	if opts.DropFrameAllowed {
		args = append(args, "--drop-frame="+strconv.Itoa(opts.DropFrameWaterMark))
	} else {
		args = append(args, "--drop-frame=0")
	}
	if opts.MaxIntraBitratePct > 0 {
		args = append(args, "--max-intra-rate="+strconv.Itoa(opts.MaxIntraBitratePct))
	}
	if opts.StaticThreshold > 0 {
		args = append(args, "--static-thresh="+strconv.Itoa(opts.StaticThreshold))
	}
	args = append(args, "--output="+ivfPath, yuvPath)
	if out, err := exec.Command(vpxenc, args...).CombinedOutput(); err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("ReadFile %s returned error: %v", ivfPath, err)
	}
	return data
}

func totalPacketBytes(packets []benchWorkloadPacket) int {
	total := 0
	for _, packet := range packets {
		total += len(packet.data)
	}
	return total
}

func qualityMetricsForVP8Packets(t *testing.T, packets []benchWorkloadPacket, sources []Image) encoderQualityMetrics {
	t.Helper()
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	psnrSum := 0.0
	ssimSum := 0.0
	frames := make([]encoderFrameQualityMetrics, 0, len(packets))
	decoded := 0
	for i, packet := range packets {
		if packet.sourceIndex < 0 || packet.sourceIndex >= len(sources) {
			t.Fatalf("packet %d source index = %d, want within %d source frames", i, packet.sourceIndex, len(sources))
		}
		if err := dec.Decode(packet.data); err != nil {
			t.Fatalf("Decode packet %d returned error: %v", i, err)
		}
		frame, ok := dec.NextFrame()
		if !ok {
			t.Fatalf("NextFrame packet %d returned no frame", i)
		}
		framePSNR := encoderValidationImagePSNR(sources[packet.sourceIndex], frame)
		frameSSIM := encoderValidationImageSSIM(sources[packet.sourceIndex], frame)
		frames = append(frames, encoderFrameQualityMetrics{Index: packet.sourceIndex, PSNR: framePSNR, SSIM: frameSSIM})
		psnrSum += framePSNR
		ssimSum += frameSSIM
		decoded++
	}
	if decoded == 0 {
		t.Fatalf("decoded frame count = 0")
	}
	return encoderQualityMetrics{
		PSNR:   psnrSum / float64(decoded),
		SSIM:   ssimSum / float64(decoded),
		Frames: decoded,
		Frame:  frames,
	}
}

func qualityMetricsForIVFByTimestamp(t *testing.T, ivf []byte, sources []Image) encoderQualityMetrics {
	t.Helper()
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(ivf) < fileHeaderSize || string(ivf[:4]) != "DKIF" {
		t.Fatalf("invalid IVF header")
	}
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	offset := fileHeaderSize
	psnrSum := 0.0
	ssimSum := 0.0
	frames := make([]encoderFrameQualityMetrics, 0, len(sources))
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		if offset+frameHeaderSize > len(ivf) {
			t.Fatalf("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		timestamp := binary.LittleEndian.Uint64(ivf[offset+4:])
		offset += frameHeaderSize
		if size < 0 || offset+size > len(ivf) {
			t.Fatalf("truncated IVF payload size=%d offset=%d len=%d", size, offset, len(ivf))
		}
		packet := ivf[offset : offset+size]
		offset += size
		if err := dec.Decode(packet); err != nil {
			t.Fatalf("Decode IVF frame %d returned error: %v", inputIndex, err)
		}
		frame, ok := dec.NextFrame()
		if !ok {
			continue
		}
		if timestamp >= uint64(len(sources)) {
			t.Fatalf("IVF timestamp = %d, want within %d source frames", timestamp, len(sources))
		}
		sourceIndex := int(timestamp)
		framePSNR := encoderValidationImagePSNR(sources[sourceIndex], frame)
		frameSSIM := encoderValidationImageSSIM(sources[sourceIndex], frame)
		frames = append(frames, encoderFrameQualityMetrics{Index: sourceIndex, PSNR: framePSNR, SSIM: frameSSIM})
		psnrSum += framePSNR
		ssimSum += frameSSIM
	}
	if len(frames) == 0 {
		t.Fatalf("decoded IVF frame count = 0")
	}
	return encoderQualityMetrics{
		PSNR:   psnrSum / float64(len(frames)),
		SSIM:   ssimSum / float64(len(frames)),
		Frames: len(frames),
		Frame:  frames,
	}
}

func realtimeBenchNoiseFrame(width, height, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func parseIVFFramePayloadSizes(t *testing.T, data []byte) (int, int) {
	t.Helper()
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(data) < fileHeaderSize || string(data[:4]) != "DKIF" {
		t.Fatalf("invalid IVF header")
	}
	total := 0
	frames := 0
	offset := fileHeaderSize
	for offset < len(data) {
		if offset+frameHeaderSize > len(data) {
			t.Fatalf("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += frameHeaderSize
		if size < 0 || offset+size > len(data) {
			t.Fatalf("truncated IVF payload size=%d offset=%d len=%d", size, offset, len(data))
		}
		total += size
		frames++
		offset += size
	}
	return total, frames
}
