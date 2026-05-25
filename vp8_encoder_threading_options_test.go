package govpx

import (
	"errors"
	"runtime"
	"testing"
)

// TestEncoderOptionsThreadsValidation pins the public configuration
// surface for EncoderOptions.Threads. Negative values must be rejected
// (mirrors libvpx's reject path in vp8/encoder/onyx_if.c when
// VP8E_SET_NUMBER_OF_THREADS receives a bogus argument); zero and
// positive values must succeed and be folded onto a non-zero internal
// representation so downstream call sites never have to special-case
// the historical zero default.
func TestEncoderOptionsThreadsValidation(t *testing.T) {
	if _, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		TargetBitrateKbps: 1200,
		Threads:           -1,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Threads=-1 error = %v, want ErrInvalidConfig", err)
	}

	for _, threads := range []int{0, 1, 2, 4, 8} {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:             64,
				Height:            64,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 1200,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineRealtime,
				CpuUsed:           8,
				Threads:           threads,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
			}
			if e.opts.Threads <= 0 {
				t.Fatalf("normalized Threads=%d, want >=1 (input %d)", e.opts.Threads, threads)
			}
			if eff := e.effectiveThreadCount(); eff < 1 || eff > runtime.NumCPU() {
				t.Fatalf("effectiveThreadCount=%d outside [1,%d]", eff, runtime.NumCPU())
			}
		})
	}
}

// TestEncoderThreadsExceedingMaxIsClamped verifies the validator
// accepts a request larger than the runtime's NumCPU but the runtime
// thread count is clamped against runtime.NumCPU(). Mirrors libvpx's
// vp8cx_create_encoder_threads ceiling against
// cm->processor_core_count.
func TestEncoderThreadsExceedingMaxIsClamped(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		Threads:           maxEncoderThreads + 64,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", maxEncoderThreads+64, err)
	}
	if e.opts.Threads != maxEncoderThreads {
		t.Fatalf("normalized Threads=%d, want %d", e.opts.Threads, maxEncoderThreads)
	}
	if eff := e.effectiveThreadCount(); eff > runtime.NumCPU() {
		t.Fatalf("effectiveThreadCount=%d > NumCPU=%d", eff, runtime.NumCPU())
	}
}
