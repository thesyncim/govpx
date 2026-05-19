package govpx

import (
	"os"
	"runtime/pprof"
	"testing"
)

// TestVP8EncoderSteadyStateCPUProfile dumps a CPU profile of govpx
// encoding 720p realtime CBR at steady-state. Run as:
//
//	GOVPX_STEADY_STATE_PROFILE=1 go test -count=1 \
//	    -run TestVP8EncoderSteadyStateCPUProfile -cpuprofile=/tmp/govpx-steady.pprof .
//
// or programmatically via GOVPX_STEADY_STATE_PROFILE=/path/to/out.pprof to
// have the test write the profile itself.
func TestVP8EncoderSteadyStateCPUProfile(t *testing.T) {
	out := os.Getenv("GOVPX_STEADY_STATE_PROFILE")
	if out == "" {
		t.Skip("set GOVPX_STEADY_STATE_PROFILE=1 (or to a path) to capture CPU profile")
	}
	const (
		width, height = 1280, 720
		fps           = 30
		baselineKbps  = 2500
		warmupFrames  = 120 // settle to steady state
		captureFrames = 240
	)
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		TargetBitrateKbps: baselineKbps,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		RateControlMode:   RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, width*height*4+4096)
	frameIdx := uint64(0)
	durationNS := uint64(1_000_000_000 / fps)
	encode := func(n int) {
		for i := range n {
			src := encoderValidationPanningFrame(width, height, int(frameIdx))
			if _, err := e.EncodeInto(dst, src, frameIdx*durationNS, durationNS, 0); err != nil {
				t.Fatalf("encode frame %d: %v", i, err)
			}
			frameIdx++
		}
	}

	encode(warmupFrames)

	if out != "1" {
		f, err := os.Create(out)
		if err != nil {
			t.Fatalf("create profile %s: %v", out, err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			t.Fatalf("start profile: %v", err)
		}
		defer pprof.StopCPUProfile()
	}

	encode(captureFrames)
}
