package govpx

import (
	"os"
	"testing"
	"time"
)

// TestVP8EncoderConstantBitrateBaseline encodes the same content for the
// same total frame count as the recovery repro but never calls
// SetBitrateKbps. If wall time stays steady, the recovery bug is caused
// by the low-bitrate burst specifically — not by frame-count drift or
// natural state evolution.
func TestVP8EncoderConstantBitrateBaseline(t *testing.T) {
	if os.Getenv("GOVPX_BITRATE_RECOVERY_REPRO") != "1" {
		t.Skip("set GOVPX_BITRATE_RECOVERY_REPRO=1 to run encoder FPS baseline")
	}
	const (
		width, height = 1280, 720
		fps           = 30
		baselineKbps  = 2500
		totalFrames   = 90 + 30 + 90 + 180
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		TargetBitrateKbps: baselineKbps,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		RateControlMode:   RateControlCBR,
	}
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, width*height*4+4096)
	frameIdx := uint64(0)
	durationNS := uint64(1_000_000_000 / fps)
	per := make([]int64, totalFrames)
	qindex := make([]int, totalFrames)
	bytesOut := make([]int, totalFrames)
	for i := 0; i < totalFrames; i++ {
		src := encoderValidationPanningFrame(width, height, int(frameIdx))
		start := time.Now()
		result, err := e.EncodeInto(dst, src, frameIdx*durationNS, durationNS, 0)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		per[i] = time.Since(start).Microseconds()
		qindex[i] = result.Quantizer
		bytesOut[i] = len(result.Data)
		frameIdx++
	}
	for i := 0; i < totalFrames; i += 30 {
		end := i + 30
		if end > totalFrames {
			end = totalFrames
		}
		var sum, qsum, bsum int64
		for j := i; j < end; j++ {
			sum += per[j]
			qsum += int64(qindex[j])
			bsum += int64(bytesOut[j])
		}
		t.Logf("bucket[%3d:%3d] avg=%6dus qavg=%5.1f bytes_avg=%5d",
			i, end, sum/int64(end-i),
			float64(qsum)/float64(end-i),
			bsum/int64(end-i))
	}
}
