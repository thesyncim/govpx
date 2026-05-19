package analysis

import (
	"testing"
)

// fillLumaDeterministic populates a luma plane with a deterministic
// pattern so analyzer benchmarks see stable per-frame work.
func fillLumaDeterministic(plane []byte, stride, width, height, seed int) {
	for y := range height {
		row := plane[y*stride : y*stride+width]
		for x := range row {
			row[x] = byte((x*7 + y*13 + seed*17) & 0xFF)
		}
	}
}

func benchmarkAnalyzeFrame(b *testing.B, width, height int, cfg Config) {
	stride := width
	luma := make([]byte, stride*height)
	chroma := make([]byte, (stride/2)*(height/2)+1)
	for i := range chroma {
		chroma[i] = 0x80
	}
	in := &FrameInput{
		Width:   width,
		Height:  height,
		YStride: stride,
		UStride: stride / 2,
		VStride: stride / 2,
		Y:       luma,
		U:       chroma[:(stride/2)*(height/2)],
		V:       chroma[:(stride/2)*(height/2)],
	}
	analyzer := New(cfg)
	if analyzer == nil {
		b.Fatalf("New returned nil for mode %v", cfg.Mode)
	}
	defer analyzer.Close()
	out := &FrameAnalysis{}

	// Warm up the previous-frame cache so steady-state work
	// dominates the measurement.
	fillLumaDeterministic(luma, stride, width, height, 0)
	analyzer.Observe(in, out)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fillLumaDeterministic(luma, stride, width, height, i+1)
		in.FrameIndex = uint64(i + 1)
		analyzer.Observe(in, out)
	}
}

// commonSizes mirrors the analyzer-microbench size list from the patch
// spec.
var commonSizes = []struct {
	name string
	w, h int
}{
	{"320x180", 320, 180},
	{"640x360", 640, 360},
	{"1280x720", 1280, 720},
	{"1920x1080", 1920, 1080},
}

// BenchmarkAnalyzeFrameAllFeatures exercises the observation path with
// every collection flag enabled. This is the upper-bound per-frame
// observation cost.
func BenchmarkAnalyzeFrameAllFeatures(b *testing.B) {
	cfg := Config{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range commonSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchmarkAnalyzeFrame(b, sz.w, sz.h, cfg)
		})
	}
}

// BenchmarkAnalyzeFrameMotionOnly isolates the motion-feature cost.
func BenchmarkAnalyzeFrameMotionOnly(b *testing.B) {
	cfg := Config{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
	}
	for _, sz := range commonSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchmarkAnalyzeFrame(b, sz.w, sz.h, cfg)
		})
	}
}

// BenchmarkAnalyzeFrameComplexityOnly isolates the complexity-feature
// cost (variance + texture + edge energy).
func BenchmarkAnalyzeFrameComplexityOnly(b *testing.B) {
	cfg := Config{
		Mode:              VP8AnalysisObserveCPU,
		CollectComplexity: true,
	}
	for _, sz := range commonSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchmarkAnalyzeFrame(b, sz.w, sz.h, cfg)
		})
	}
}
