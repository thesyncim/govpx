package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// BenchmarkVP9EncodeCPUUsed measures govpx's per-frame encode time across
// representative cpu_used lanes: 0, 4, 6, 8.
// Frame size is 640x360 in line with the realtime CBR oracle target. The
// benchmark uses a fixed panning-YCbCr source ring across cpu_used buckets so
// source-frame allocation stays outside the timed encode loop.
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
			sources := vp9test.NewPanningSources(width, height, 8)
			dst := make([]byte, width*height*2)
			for i, src := range sources {
				if _, err := e.EncodeInto(src, dst); err != nil {
					b.Fatalf("warmup EncodeInto[%d]: %v", i, err)
				}
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := sources[i%len(sources)]
				if _, err := e.EncodeInto(src, dst); err != nil {
					b.Fatalf("EncodeInto[%d]: %v", i, err)
				}
			}
		})
	}
}
