package benchcmd

import (
	"testing"

	"github.com/thesyncim/govpx"
)

// benchmarkVP9DecodePackets encodes the canonical decode-benchmark stream
// once and measures repeated serial decodes of it, mirroring the measured
// pass of runDecodeBenchmark without the libvpx reference plumbing. It
// exists so decoder changes can be A/B-ed with benchstat on a noisy box.
func benchmarkVP9DecodePackets(b *testing.B, width, height, frames, bitrate int) {
	b.Helper()
	cfg := benchConfig{
		Codec:       codecVP9,
		Width:       width,
		Height:      height,
		Frames:      frames,
		FPS:         30,
		BitrateKbps: bitrate,
		Mode:        "realtime",
	}
	deadline, _, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		b.Fatalf("benchmarkDeadline: %v", err)
	}
	images := make([]govpx.Image, cfg.Frames)
	for i := range images {
		images[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
	}
	packets, err := encodeDecodeBenchmarkPackets(cfg, deadline, images, codecVP9)
	if err != nil {
		b.Fatalf("encodeDecodeBenchmarkPackets: %v", err)
	}
	dec, err := newBenchmarkDecoder(codecVP9, cfg)
	if err != nil {
		b.Fatalf("newBenchmarkDecoder: %v", err)
	}
	defer closeBenchmarkDecoder(dec)
	latencies := make([]int64, 0, len(packets))
	if _, _, err := decodeBenchmarkPackets(dec, packets, latencies); err != nil {
		b.Fatalf("warmup decode: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := decodeBenchmarkPackets(dec, packets, latencies[:0]); err != nil {
			b.Fatalf("decode: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(packets))/1e6,
		"ms/frame")
}

func BenchmarkVP9Decode720p(b *testing.B) {
	benchmarkVP9DecodePackets(b, 1280, 720, 120, 2500)
}

func BenchmarkVP9Decode1080p(b *testing.B) {
	benchmarkVP9DecodePackets(b, 1920, 1080, 120, 6000)
}
