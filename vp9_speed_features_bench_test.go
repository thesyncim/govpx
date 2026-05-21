package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// BenchmarkVP9EncodeCPUUsed measures govpx's per-frame encode time at the
// four cpu_used values cited in the speed-features port task: 0, 4, 6, 8.
// Frame size is 640x360 in line with the realtime CBR oracle target. The
// benchmark uses the panning-YCbCr synthetic source so the cost of source
// allocation is shared across cpu_used buckets.
//
// Invoke with:
//
//	go test -run none -bench BenchmarkVP9EncodeCPUUsed -benchmem -short .
func BenchmarkVP9EncodeCPUUsed(b *testing.B) {
	cpuUsedValues := []int{0, 4, 6, 8}
	const width, height = 640, 360
	for _, cpuUsed := range cpuUsedValues {
		var name string
		switch cpuUsed {
		case 0:
			name = "cpu0"
		case 4:
			name = "cpu4"
		case 6:
			name = "cpu6"
		case 8:
			name = "cpu8"
		}
		b.Run(name, func(b *testing.B) {
			deadline := DeadlineRealtime
			if cpuUsed == 0 {
				deadline = DeadlineBestQuality
			}
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:    width,
				Height:   height,
				Deadline: deadline,
				CpuUsed:  int8(cpuUsed),
			})
			if err != nil {
				b.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()
			dst := make([]byte, width*height*2)
			// Warmup.
			src := vp9test.NewPanningYCbCr(width, height, 0)
			if _, err := e.EncodeInto(src, dst); err != nil {
				b.Fatalf("warmup EncodeInto: %v", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := vp9test.NewPanningYCbCr(width, height, i+1)
				if _, err := e.EncodeInto(src, dst); err != nil {
					b.Fatalf("EncodeInto[%d]: %v", i, err)
				}
			}
		})
	}
}
