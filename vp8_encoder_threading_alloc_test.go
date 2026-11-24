package govpx

import "testing"

// TestEncoderThreadsInterFrameAllocatesZero pins the row-threaded
// steady-state encode path against heap regressions. The fixture is wide
// enough to take the threaded reconstruction path and uses a small frame
// ring so source generation is outside the measured closure.
func TestEncoderThreadsInterFrameAllocatesZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping threaded alloc sweep in -short")
	}
	const (
		width  = 640
		height = 480
		frames = 6
	)
	for _, threads := range []int{2, 4, 8} {
		t.Run("threads_"+itoaSmall(threads), func(t *testing.T) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   1800,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				DropFrameAllowed:    false,
				Deadline:            DeadlineRealtime,
				CpuUsed:             8,
				KeyFrameInterval:    120,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
				Threads:             threads,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
			}
			defer e.Close()
			srcs := make([]Image, frames)
			for i := range srcs {
				srcs[i] = rateControlTestFrame(width, height, i)
			}
			dst := make([]byte, width*height*6+4096)
			if _, err := e.EncodeInto(dst, srcs[0], 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto Threads=%d: %v", threads, err)
			}
			for i := 1; i < frames; i++ {
				if _, err := e.EncodeInto(dst, srcs[i], uint64(i), 1, 0); err != nil {
					t.Fatalf("warmup EncodeInto Threads=%d: %v", threads, err)
				}
			}
			pts := uint64(frames)
			idx := 0
			allocs := testing.AllocsPerRun(20, func() {
				_, _ = e.EncodeInto(dst, srcs[idx%frames], pts, 1, 0)
				idx++
				pts++
			})
			if allocs != 0 {
				t.Fatalf("inter-frame EncodeInto allocs = %v at Threads=%d, want 0", allocs, threads)
			}
		})
	}
}
