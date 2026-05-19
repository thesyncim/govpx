package gpuanalysis_test

import (
	"fmt"
	"testing"

	govpx "github.com/thesyncim/govpx"
	_ "github.com/thesyncim/govpx/gpuanalysis"
)

// e2eFrame is a deterministic test frame; each call mutates the
// supplied Image in place so the encoder sees changing source content
// (otherwise rate-control / static-MB paths short-circuit and the
// measurement no longer reflects realistic work).
func e2eFillFrame(img govpx.Image, i int) {
	for j := range img.Y {
		img.Y[j] = byte((j*7 + i*13) & 0xFF)
	}
	for j := range img.U {
		img.U[j] = byte(96 + ((j + i*3) & 0x3F))
	}
	for j := range img.V {
		img.V[j] = byte(144 + ((j*2 + i*5) & 0x3F))
	}
}

func e2eImage(width, height int) govpx.Image {
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	return govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvW*uvH),
		V:       make([]byte, uvW*uvH),
		YStride: width,
		UStride: uvW,
		VStride: uvW,
	}
}

func e2eNewEncoder(b *testing.B, width, height int, cfg govpx.VP8AnalysisConfig) *govpx.VP8Encoder {
	b.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1500 + 500*(width/640),
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    30,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		Threads:             1,
		Analysis:            cfg,
	})
	if err != nil {
		b.Fatalf("NewVP8Encoder %dx%d mode=%v: %v", width, height, cfg.Mode, err)
	}
	return e
}

// benchE2EEncode is the load-bearing measurement. It constructs an
// encoder ONCE, then loops b.N frames through EncodeInto. ns/op is
// therefore the per-frame encode wall time, which is what callers
// actually care about. Allocs/op includes any per-frame analyzer
// allocations.
func benchE2EEncode(b *testing.B, width, height int, cfg govpx.VP8AnalysisConfig) {
	enc := e2eNewEncoder(b, width, height, cfg)
	defer enc.Close()
	img := e2eImage(width, height)
	buf := make([]byte, width*height*4)

	// Run a few warmup frames so rate control settles and any first-frame
	// pipeline compile / pipeline cache work is not in the measurement.
	const warmup = 4
	for i := range warmup {
		e2eFillFrame(img, i)
		if _, err := enc.EncodeInto(buf, img, uint64(i), 1, 0); err != nil {
			b.Fatalf("warmup frame %d: %v", i, err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e2eFillFrame(img, warmup+i)
		if _, err := enc.EncodeInto(buf, img, uint64(warmup+i), 1, 0); err != nil {
			b.Fatalf("frame %d: %v", i, err)
		}
	}
}

// resolutions exercised by the e2e benchmarks. These are the sizes
// the user asked to see numbers for.
var e2eSizes = []struct {
	name string
	w, h int
}{
	{"360p", 640, 368}, // 360 rounded up to MB boundary
	{"720p", 1280, 720},
	{"1080p", 1920, 1088}, // 1080 rounded up to MB boundary
	{"4K", 3840, 2160},
}

func BenchmarkE2EEncodeOff(b *testing.B) {
	cfg := govpx.DefaultVP8AnalysisConfig()
	for _, sz := range e2eSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchE2EEncode(b, sz.w, sz.h, cfg)
		})
	}
}

func BenchmarkE2EEncodeCPU(b *testing.B) {
	cfg := govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range e2eSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchE2EEncode(b, sz.w, sz.h, cfg)
		})
	}
}

func BenchmarkE2EEncodeGPU(b *testing.B) {
	cfg := govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveGPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range e2eSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchE2EEncode(b, sz.w, sz.h, cfg)
		})
	}
}

// silence "no benchmarks were run" if -bench filter excludes everything.
var _ = fmt.Sprintf
