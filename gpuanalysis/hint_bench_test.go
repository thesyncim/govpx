package gpuanalysis_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	_ "github.com/thesyncim/govpx/gpuanalysis"
)

// hintTestSizes is intentionally smaller than e2eSizes so the hint
// bench finishes quickly. 720p / 1080p / 4K already shown to be the
// "hint helps" regime; 360p is below the crossover.
var hintTestSizes = []struct {
	name string
	w, h int
}{
	{"720p", 1280, 720},
	{"1080p", 1920, 1088},
	{"4K", 3840, 2160},
}

// benchHintEncodeStream encodes N frames with a configurable
// VP8AnalysisConfig and reports wall-clock per frame. Encoder is
// constructed ONCE per benchmark; per-frame b.N times.
func benchHintEncodeStream(b *testing.B, width, height int, cfg govpx.VP8AnalysisConfig) {
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
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
		b.Fatalf("NewVP8Encoder cfg=%+v: %v", cfg, err)
	}
	defer enc.Close()
	img := e2eImage(width, height)
	buf := make([]byte, width*height*4)

	// Warmup so rate control settles and GPU pipeline is warm.
	const warmup = 4
	for i := range warmup {
		e2eFillStaticFrame(img, i)
		if _, err := enc.EncodeInto(buf, img, uint64(i), 1, 0); err != nil {
			b.Fatalf("warmup frame %d: %v", i, err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e2eFillStaticFrame(img, warmup+i)
		if _, err := enc.EncodeInto(buf, img, uint64(warmup+i), 1, 0); err != nil {
			b.Fatalf("frame %d: %v", i, err)
		}
	}
}

// e2eFillStaticFrame writes a mostly-static frame: 90% of the area
// is identical between frames (only a small moving patch changes).
// That way the GPU analyzer flags the static region's MBs as
// FlagStatic (ZeroSAD = 0 against the previous source), and the
// hint-driven mode-decision early-exit has work to skip.
//
// The first call (i == 0) just paints the static background; the
// moving patch starts at i >= 1 so frame 1 onward exercises the
// hint path with a real "static MBs + small motion patch" pattern.
func e2eFillStaticFrame(img govpx.Image, i int) {
	if i == 0 {
		// Background is filled once and then never touched outside
		// the moving-patch region, so all MBs outside that region
		// are byte-identical frame-to-frame -> ZeroSAD == 0 ->
		// FlagStatic.
		for y := 0; y < img.Height; y++ {
			row := img.Y[y*img.YStride : y*img.YStride+img.Width]
			for x := range row {
				row[x] = byte((x ^ y) & 0xFF)
			}
		}
		for j := range img.U {
			img.U[j] = 0x80
		}
		for j := range img.V {
			img.V[j] = 0x80
		}
		return
	}
	// Animate a 32x32 luma patch near the center; everything else
	// stays bit-identical to frame 0.
	patchW := 32
	patchH := 32
	if patchW > img.Width {
		patchW = img.Width
	}
	if patchH > img.Height {
		patchH = img.Height
	}
	startX := (img.Width - patchW) / 2
	startY := (img.Height - patchH) / 2
	// Move the patch around so it doesn't accidentally also become
	// static (which would defeat the purpose of having a non-static
	// region for the encoder to do real work on).
	dx := (i * 3) & 0x1F
	dy := (i * 5) & 0x1F
	for y := 0; y < patchH; y++ {
		py := startY + y
		if py >= img.Height {
			break
		}
		row := img.Y[py*img.YStride+startX : py*img.YStride+startX+patchW]
		for x := range row {
			row[x] = byte((x+dx)*7 ^ (y+dy)*13)
		}
	}
}

// BenchmarkE2EEncodeOffStatic and BenchmarkE2EEncodeGPUHintsStatic
// share the static-content frame filler so the comparison directly
// measures the speedup of the hint-driven early-exit on the kind of
// content the optimization is designed for (talking heads, screen
// capture, slowly-changing gradients).

func BenchmarkE2EEncodeOffStatic(b *testing.B) {
	cfg := govpx.DefaultVP8AnalysisConfig()
	for _, sz := range hintTestSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchHintEncodeStream(b, sz.w, sz.h, cfg)
		})
	}
}

func BenchmarkE2EEncodeGPUStatic(b *testing.B) {
	cfg := govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveGPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range hintTestSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchHintEncodeStream(b, sz.w, sz.h, cfg)
		})
	}
}

func BenchmarkE2EEncodeGPUHintsStatic(b *testing.B) {
	cfg := govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveGPU,
		UseEncodeHints:     true,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range hintTestSizes {
		b.Run(sz.name, func(b *testing.B) {
			benchHintEncodeStream(b, sz.w, sz.h, cfg)
		})
	}
}

// TestVP8AnalysisHintWireUpFires confirms the hint-driven mode-decision
// early-exit is actually active by reading the per-encoder counter.
// We do NOT check for a parity break here because the encoder's own
// mode-decision converges to ZEROMV-LAST for static macroblocks
// anyway; the optimization saves work but happens to produce the
// same output. The byte-level test below is the proof that "early-exit
// fires" is observable.
func TestVP8AnalysisHintWireUpFires(t *testing.T) {
	const (
		width  = 320
		height = 240
		frames = 8
	)
	cfg := govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveCPU,
		UseEncodeHints:     true,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   govpx.RateControlCBR,
		TargetBitrateKbps: 1500,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          govpx.DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  30,
		Threads:           1,
		Analysis:          cfg,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	img := e2eImage(width, height)
	buf := make([]byte, width*height*4)
	for i := range frames {
		e2eFillStaticFrame(img, i)
		if _, err := enc.EncodeInto(buf, img, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	hits := enc.HintEarlyExitCount()
	if hits == 0 {
		t.Fatalf("hint wire-up never fired: HintEarlyExitCount = 0 (expected >0 on static content)")
	}
	t.Logf("hint wire-up active: %d early exits / %d misses across %d frames",
		hits, enc.HintMissCount(), frames)
}

