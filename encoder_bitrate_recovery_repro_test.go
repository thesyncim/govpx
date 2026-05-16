package govpx

import (
	"os"
	"testing"
	"time"
)

// TestVP8EncoderRecoversFPSAfterLowBitrateBurst measures per-frame wall
// time across three phases:
//
//	1. baseline at target bitrate
//	2. low-bitrate burst (1s)
//	3. restored bitrate
//
// Diagnostic — paired with TestVP8EncoderConstantBitrateBaseline. The two
// tests together show that phase-3 per-frame time equals the constant-
// bitrate steady-state at the same frame index, which means the apparent
// "FPS never recovers" symptom after a low-bitrate dip is not a
// regression: encode time tracks bytes-per-frame, which in turn tracks
// the post-recovery rate-control Q. The same observation is visible
// running cmd/govpx-bench, where govpx steady-state is ~1.6x slower than
// libvpx vpxenc at cpu_used=8 realtime — that gap is the underlying
// thing the user perceives, not a SetBitrate-induced state leak.
func TestVP8EncoderRecoversFPSAfterLowBitrateBurst(t *testing.T) {
	if os.Getenv("GOVPX_BITRATE_RECOVERY_REPRO") != "1" {
		t.Skip("set GOVPX_BITRATE_RECOVERY_REPRO=1 to run encoder FPS recovery diagnostic")
	}
	const (
		width, height = 1280, 720
		fps           = 30
		baselineKbps  = 2500
		lowKbps       = 80
		phaseFrames   = 90
		burstFrames   = 30
		tailFrames    = 180
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

	measure := func(label string, frames int) (totalUS int64, perFrame []int64) {
		perFrame = make([]int64, frames)
		for i := 0; i < frames; i++ {
			src := encoderValidationPanningFrame(width, height, int(frameIdx))
			start := time.Now()
			if _, err := e.EncodeInto(dst, src, frameIdx*durationNS, durationNS, 0); err != nil {
				t.Fatalf("%s frame %d: %v", label, i, err)
			}
			elapsed := time.Since(start).Microseconds()
			perFrame[i] = elapsed
			totalUS += elapsed
			frameIdx++
		}
		return
	}

	statsFor := func(label string, frames int, totalUS int64, perFrame []int64, autoSpeed int) {
		avg := totalUS / int64(frames)
		var maxUS int64
		for _, v := range perFrame {
			if v > maxUS {
				maxUS = v
			}
		}
		t.Logf("%-12s avg=%6dus max=%6dus autoSpeed=%d", label, avg, maxUS, autoSpeed)
	}

	// Phase 1 baseline (longer to let auto-speed settle).
	t1, p1 := measure("phase1", phaseFrames)
	statsFor("phase1", phaseFrames, t1, p1, e.autoSpeed)

	// Phase 2 low-bitrate burst.
	if err := e.SetBitrateKbps(lowKbps); err != nil {
		t.Fatalf("SetBitrateKbps low: %v", err)
	}
	t2, p2 := measure("phase2-low", burstFrames)
	statsFor("phase2-low", burstFrames, t2, p2, e.autoSpeed)

	// Phase 3 recovery (immediate post-restore).
	if err := e.SetBitrateKbps(baselineKbps); err != nil {
		t.Fatalf("SetBitrateKbps restore: %v", err)
	}
	t3, p3 := measure("phase3-recov", phaseFrames)
	statsFor("phase3-recov", phaseFrames, t3, p3, e.autoSpeed)

	// Phase 4 long tail to see if it converges back to phase-1 baseline.
	t4, p4 := measure("phase4-tail", tailFrames)
	statsFor("phase4-tail", tailFrames, t4, p4, e.autoSpeed)

	// Per-30-frame buckets of phase 4 to spot the convergence curve.
	for i := 0; i < tailFrames; i += 30 {
		end := i + 30
		if end > tailFrames {
			end = tailFrames
		}
		var sum int64
		for _, v := range p4[i:end] {
			sum += v
		}
		t.Logf("  tail[%d:%d] avg=%dus", i, end, sum/int64(end-i))
	}

	avg1 := t1 / int64(phaseFrames)
	avg3 := t3 / int64(phaseFrames)
	avg4 := t4 / int64(tailFrames)
	if avg3 > avg1*3/2 {
		t.Logf("PHASE-3 AVG IS %d%% OF PHASE-1", avg3*100/max64(avg1, 1))
	}
	if avg4 > avg1*3/2 {
		t.Logf("PHASE-4 (LONG TAIL) AVG IS %d%% OF PHASE-1 — does not converge", avg4*100/max64(avg1, 1))
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
