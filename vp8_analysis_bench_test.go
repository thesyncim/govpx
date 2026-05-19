package govpx

import (
	"testing"
)

// benchmarkEncodeWithAnalysis runs a fixed-length VP8 encode against
// the same deterministic input under the supplied analysis config so
// callers can compare ns/op between off and observe modes.
func benchmarkEncodeWithAnalysis(b *testing.B, width, height int, cfg VP8AnalysisConfig) {
	const frames = 8
	img := testImage(width, height)
	buf := make([]byte, width*height*4)
	for i := range img.U {
		img.U[i] = 0x80
	}
	for i := range img.V {
		img.V[i] = 0x80
	}

	b.ResetTimer()
	b.ReportAllocs()
	for it := 0; it < b.N; it++ {
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    30,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Threads:             1,
			Analysis:            cfg,
		})
		if err != nil {
			b.Fatalf("NewVP8Encoder: %v", err)
		}
		for f := range frames {
			// Modify luma a little each frame so motion observation
			// has work to do.
			for j := range img.Y {
				img.Y[j] = byte((j*7 + (it*frames+f)*13) & 0xFF)
			}
			if _, err := e.EncodeInto(buf, img, uint64(it*frames+f), 1, 0); err != nil {
				b.Fatalf("EncodeInto frame %d/%d: %v", it, f, err)
			}
		}
		e.Close()
	}
}

// BenchmarkVP8EncodeAnalysisOff measures the canonical encode cost
// with the analyzer disabled. Compared against
// BenchmarkVP8EncodeAnalysisObserveCPU to derive observation overhead.
func BenchmarkVP8EncodeAnalysisOff(b *testing.B) {
	cfg := DefaultVP8AnalysisConfig()
	for _, sz := range []struct {
		name string
		w, h int
	}{
		{"320x180", 320, 180},
		{"640x360", 640, 360},
		{"1280x720", 1280, 720},
	} {
		b.Run(sz.name, func(b *testing.B) {
			benchmarkEncodeWithAnalysis(b, sz.w, sz.h, cfg)
		})
	}
}

// BenchmarkVP8EncodeAnalysisObserveCPU measures encode cost with the
// CPU observer collecting every available statistic.
func BenchmarkVP8EncodeAnalysisObserveCPU(b *testing.B) {
	cfg := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range []struct {
		name string
		w, h int
	}{
		{"320x180", 320, 180},
		{"640x360", 640, 360},
		{"1280x720", 1280, 720},
	} {
		b.Run(sz.name, func(b *testing.B) {
			benchmarkEncodeWithAnalysis(b, sz.w, sz.h, cfg)
		})
	}
}

// BenchmarkVP8EncodeSimulcastObserveCPU simulates a three-layer
// simulcast workload by running three independent VP8 encodes per
// iteration at 320x180, 640x360, and 1280x720, all with the CPU
// observer enabled. This captures the cumulative analysis cost a
// simulcast pipeline would pay if every layer is independently
// analyzed on the CPU.
func BenchmarkVP8EncodeSimulcastObserveCPU(b *testing.B) {
	cfg := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	layers := []struct {
		w, h int
	}{
		{320, 180},
		{640, 360},
		{1280, 720},
	}

	type layerState struct {
		img Image
		buf []byte
		enc *VP8Encoder
	}

	states := make([]layerState, len(layers))
	for i, l := range layers {
		img := testImage(l.w, l.h)
		for j := range img.U {
			img.U[j] = 0x80
		}
		for j := range img.V {
			img.V[j] = 0x80
		}
		states[i] = layerState{
			img: img,
			buf: make([]byte, l.w*l.h*4),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for it := 0; it < b.N; it++ {
		for li, l := range layers {
			st := &states[li]
			e, err := NewVP8Encoder(EncoderOptions{
				Width:               l.w,
				Height:              l.h,
				FPS:                 30,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   600 * (li + 1),
				MinQuantizer:        4,
				MaxQuantizer:        56,
				Deadline:            DeadlineRealtime,
				CpuUsed:             8,
				KeyFrameInterval:    30,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
				Threads:             1,
				Analysis:            cfg,
			})
			if err != nil {
				b.Fatalf("layer %d NewVP8Encoder: %v", li, err)
			}
			st.enc = e
		}
		for f := range 4 {
			for li := range layers {
				st := &states[li]
				img := st.img
				for j := range img.Y {
					img.Y[j] = byte((j*7 + (it*4+f)*13 + li*11) & 0xFF)
				}
				if _, err := st.enc.EncodeInto(st.buf, img, uint64(it*4+f), 1, 0); err != nil {
					b.Fatalf("layer %d frame %d: %v", li, f, err)
				}
			}
		}
		for li := range layers {
			states[li].enc.Close()
			states[li].enc = nil
		}
	}
}
