package govpx

import (
	"runtime"
	"testing"
)

// BenchmarkEncodeIntoThreadingMatrix sweeps Threads={1,2,4,8,NumCPU} on
// a 1280x720 RT CBR cpu_used=8 inter-frame encode so Threads=1
// regressions vs. the historical zero-cost baseline are visible at
// per-commit cadence, and so the row-threaded pipeline (when it lands)
// has a single fixture to demonstrate scaling against. Each sub-bench
// drives a fresh encoder so per-frame state caches do not bleed between
// thread counts.
func BenchmarkEncodeIntoThreadingMatrix(b *testing.B) {
	const (
		width  = 1280
		height = 720
	)
	threadCounts := []int{1, 2, 4, 8}
	if n := runtime.NumCPU(); n > 8 && n != 1 && n != 2 && n != 4 && n != 8 {
		threadCounts = append(threadCounts, n)
	}

	// Pre-allocate one frame and mutate its content per iteration. The
	// previous form allocated 1.4 MB per b.N iter (Y/U/V slices), which
	// reported as encoder allocations even though the encoder hot path
	// itself is zero-alloc.
	img := testImage(width, height)
	fillFrame := func(index int) {
		for i := range img.Y {
			img.Y[i] = byte((i*7 + index*13) & 0xFF)
		}
		for i := range img.U {
			img.U[i] = byte(96 + ((i + index*3) & 0x3F))
		}
		for i := range img.V {
			img.V[i] = byte(144 + ((i*2 + index*5) & 0x3F))
		}
	}

	for _, threads := range threadCounts {
		b.Run("threads_"+itoaSmall(threads), func(b *testing.B) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   2500,
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
				b.Fatalf("NewVP8Encoder Threads=%d returned error: %v", threads, err)
			}
			buf := make([]byte, width*height*4)
			// Prime: encode a key frame so subsequent encodes are inter.
			fillFrame(0)
			if _, err := e.EncodeInto(buf, img, 0, 1, 0); err != nil {
				b.Fatalf("prime EncodeInto Threads=%d: %v", threads, err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				fillFrame(i + 1)
				if _, err := e.EncodeInto(buf, img, uint64(i+1), 1, 0); err != nil {
					b.Fatalf("EncodeInto Threads=%d frame %d: %v", threads, i+1, err)
				}
			}
		})
	}
}
