package govpx

import (
	"bytes"
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

// TestEncoderThreadsProducesIdenticalBitstream pins the byte-for-byte
// invariant the parity scoreboards depend on: the Threads option must
// not change the encoded bitstream while the encoder runs the
// historical serial macroblock loop. This guard fires the moment a
// future row-threaded path introduces any divergence between the
// canonical Threads=1 reference and other Threads values, forcing the
// follow-up to either land behind a flag or refresh baselines
// explicitly.
func TestEncoderThreadsProducesIdenticalBitstream(t *testing.T) {
	const (
		width  = 64
		height = 48
		frames = 4
	)
	threadCounts := []int{1, 2, 4}
	if n := runtime.NumCPU(); n > 4 && n != 1 {
		threadCounts = append(threadCounts, n)
	}
	threadCounts = append(threadCounts, 0)

	makeFrame := func(index int) Image {
		img := testImage(width, height)
		for i := range img.Y {
			img.Y[i] = byte((i*7 + index*13) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(96 + ((i + index*3) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(144 + ((i*2 + index*5) & 0x3F))
		}
		return img
	}

	encode := func(t *testing.T, threads int) [][]byte {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			DropFrameAllowed:    false,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Threads:             threads,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
		}
		packets := make([][]byte, 0, frames)
		buf := make([]byte, max(8192, width*height*4))
		for i := 0; i < frames; i++ {
			res, err := e.EncodeInto(buf, makeFrame(i), uint64(i), 1, 0)
			if err != nil {
				t.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i, err)
			}
			if res.Dropped {
				t.Fatalf("EncodeInto Threads=%d frame %d unexpectedly dropped", threads, i)
			}
			packets = append(packets, append([]byte(nil), res.Data...))
		}
		return packets
	}

	baseline := encode(t, 1)
	for _, threads := range threadCounts {
		threads := threads
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			got := encode(t, threads)
			if len(got) != len(baseline) {
				t.Fatalf("threads=%d produced %d packets, baseline=%d", threads, len(got), len(baseline))
			}
			for i := range got {
				if !bytes.Equal(got[i], baseline[i]) {
					t.Fatalf("threads=%d frame %d bitstream diverges from Threads=1 baseline (%d vs %d bytes)", threads, i, len(got[i]), len(baseline[i]))
				}
			}
		})
	}
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
