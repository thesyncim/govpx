package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// BenchmarkVP9EncodeRowMT compares the same VP9 encode with row-MT off
// vs on at 1280x720 / 1920x1080. The benchmark exists so future
// row-worker integration changes have a baseline; today the row workers
// are allocated but not driving the production encode path, so the
// numbers should match the serial path within scheduler noise. The timed loop
// refreshes a reusable panning source while the timer is stopped, so the
// benchmark measures encode work rather than synthetic source-frame generation.
// Run with
// `go test -bench=BenchmarkVP9EncodeRowMT -benchmem -short .` to compare.
func BenchmarkVP9EncodeRowMT(b *testing.B) {
	cases := []struct {
		name          string
		width, height int
		threads       int
	}{
		{"720p_T4", 1280, 720, 4},
		{"720p_T8", 1280, 720, 8},
		{"1080p_T4", 1920, 1080, 4},
		{"1080p_T8", 1920, 1080, 8},
	}
	for _, tc := range cases {
		b.Run(tc.name+"/RowMT_off", func(b *testing.B) {
			benchmarkVP9EncodeRowMT(b, tc.width, tc.height, tc.threads, false)
		})
		b.Run(tc.name+"/RowMT_on", func(b *testing.B) {
			benchmarkVP9EncodeRowMT(b, tc.width, tc.height, tc.threads, true)
		})
	}
}

func benchmarkVP9EncodeRowMT(b *testing.B, width, height, threads int, rowMT bool) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: threads,
		RowMT:   rowMT,
	})
	if err != nil {
		b.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	src := vp9test.NewPanningYCbCr(width, height, 0)
	dst := make([]byte, width*height*2)
	// Warmup to size all scratch and reach steady-state.
	for i := 0; i < 8; i++ {
		vp9test.FillPanningYCbCr(src, i)
		if _, err := e.EncodeInto(src, dst); err != nil {
			b.Fatalf("warmup EncodeInto[%d]: %v", i, err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		vp9test.FillPanningYCbCr(src, i+8)
		b.StartTimer()
		if _, err := e.EncodeInto(src, dst); err != nil {
			b.Fatalf("EncodeInto[%d]: %v", i, err)
		}
	}
}
