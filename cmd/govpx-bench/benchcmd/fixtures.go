package benchcmd

import (
	govpx "github.com/thesyncim/govpx"
)

// makePanningFrame returns a deterministic 4:2:0 panning fixture that
// resembles a slowly translating natural-content frame: low-frequency
// luma gradient plus a few mid-frequency sinusoidal harmonics that are
// expensive to code only at first sight, then become cheap once the
// inter-predictor can track them. Index advances the pan by +2 luma
// samples per frame in X and +1 in Y so motion-estimation gets a clear
// signal without the texture becoming pure noise. The deterministic
// generator avoids any floating-point math so the output is bit-exact
// across runs and platforms.
func makePanningFrame(width int, height int, index int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	xoff := index * 2
	yoff := index
	for y := range height {
		for x := range width {
			srcX := x + xoff
			srcY := y + yoff
			// Smooth bilinear gradient + low-frequency triangle
			// modulation. The triangle waves have a 64-sample
			// period so motion estimation has a clear signal at
			// realistic block sizes. Mix is centred and clamped
			// rather than wrapped to avoid synthetic high-
			// frequency edges from byte overflow.
			gradient := 64 + triangleByte(srcX+srcY, 256)/4 // ~[64, 127]
			triX := triangleByte(srcX, 64) / 4              // ~[0, 64]
			triY := triangleByte(srcY, 64) / 4              // ~[0, 64]
			img.Y[y*img.YStride+x] = clampByte(gradient + triX + triY)
		}
	}
	for y := range uvHeight {
		for x := range uvWidth {
			srcX := 2*x + xoff
			srcY := 2*y + yoff
			img.U[y*img.UStride+x] = clampByte(128 + (triangleByte(srcX, 128)-128)/8)
			img.V[y*img.VStride+x] = clampByte(128 + (triangleByte(srcY, 128)-128)/8)
		}
	}
	return img
}

// triangleByte returns a deterministic [0,255] triangle wave with the
// given period. Used by makePanningFrame to construct a smoothly-varying
// luma/chroma signal without floating-point math. The companion
// clampByte saturator lives in feature_gates.go.
func triangleByte(x int, period int) int {
	if period <= 0 {
		period = 32
	}
	half := period / 2
	r := ((x % period) + period) % period
	if r < half {
		return r * 255 / half
	}
	return (period - r) * 255 / half
}

// makeCheckerFrame returns a checkerboard fixture with motion induced by
// per-frame phase. The pattern is intentionally smooth-edged so the VP9
// encoder hits its inter-block paths rather than spending the entire frame
// in intra; the slow alternation also gives the SSIM kernel something
// non-degenerate to chew on.
func makeCheckerFrame(width int, height int, index int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	// Two-square checkerboard with a slow brightness modulation driven by
	// `index`. The square size is 16 luma samples to align with VP9
	// transform-block boundaries so the encoder doesn't waste effort
	// splitting blocks across high-contrast edges.
	const cell = 16
	for y := range height {
		for x := range width {
			cx := (x + index) / cell
			cy := (y + index/2) / cell
			lo := byte(48)
			hi := byte(208)
			if ((cx ^ cy) & 1) == 0 {
				img.Y[y*img.YStride+x] = lo
			} else {
				img.Y[y*img.YStride+x] = hi
			}
		}
	}
	for y := range uvHeight {
		for x := range uvWidth {
			cx := (2*x + index) / cell
			cy := (2*y + index/2) / cell
			if ((cx ^ cy) & 1) == 0 {
				img.U[y*img.UStride+x] = byte(112)
				img.V[y*img.VStride+x] = byte(144)
			} else {
				img.U[y*img.UStride+x] = byte(144)
				img.V[y*img.VStride+x] = byte(112)
			}
		}
	}
	return img
}

// qualityFixture describes one deterministic test sequence for the
// quality-gate verify mode. Each fixture is sized and bitrate-budgeted to
// hit a realistic operating point for govpx VP9: panning at 1280x720 /
// 2 Mbps exercises full-resolution inter coding, checker at 640x360 /
// 600 kbps exercises low-bitrate CBR rate control.
type qualityFixture struct {
	Name        string
	Width       int
	Height      int
	Frames      int
	FPS         int
	BitrateKbps int
	Mode        string
	Source      func(width int, height int, index int) govpx.Image
}

// qualityGateFixtures returns the canonical fixture list referenced by
// the `make verify-quality` target. The fixtures are deliberately kept
// modest in resolution (max 640x360) so the gate completes quickly under
// CI and so realtime VP9 CBR doesn't spend its budget recovering from
// rate-control oscillations -- the gate's job is to flag perceptual
// regressions, not to stress-test the rate controller.
func qualityGateFixtures() []qualityFixture {
	return []qualityFixture{
		{
			// Smooth panning at 360p / 2 Mbps gives the inter
			// predictor enough budget to track the deterministic
			// translation cleanly; PSNR/SSIM regressions in inter
			// coding fall straight out of this fixture.
			Name:        "panning-360p-2m-60f",
			Width:       640,
			Height:      360,
			Frames:      60,
			FPS:         30,
			BitrateKbps: 2000,
			Mode:        "realtime",
			Source:      makePanningFrame,
		},
		{
			// 16-cell checker at 360p / 600 kbps exercises the
			// CBR rate controller's tighter operating point: the
			// content compresses well and the encoder has to spend
			// most of its budget on keyframes.
			Name:        "checker-360p-600k-120f",
			Width:       640,
			Height:      360,
			Frames:      120,
			FPS:         30,
			BitrateKbps: 600,
			Mode:        "realtime",
			Source:      makeCheckerFrame,
		},
	}
}

// runQualityGateBench drives a single qualityFixture through runVP9Benchmark
// with a custom frame source. It is package-internal so the
// `govpx-bench -quality-fixtures` driver and tests can share the same code
// path.
func runQualityGateBench(base benchConfig, fx qualityFixture) (benchReport, error) {
	cfg := base
	cfg.Width = fx.Width
	cfg.Height = fx.Height
	cfg.Frames = fx.Frames
	cfg.FPS = fx.FPS
	cfg.BitrateKbps = fx.BitrateKbps
	cfg.Mode = fx.Mode
	cfg.Codec = codecVP9
	cfg.Decode = false
	return runVP9BenchmarkWithSource(cfg, fx.Source)
}

// runVP9BenchmarkWithSource is the runVP9Benchmark variant that accepts a
// caller-provided source generator instead of the synthetic
// makeBenchmarkFrame ramp. It exists for fixture-driven verification and
// keeps the heavy lifting in the regular VP9 encode path -- runVP9Benchmark
// itself just generates a default ramp and then delegates here.
func runVP9BenchmarkWithSource(cfg benchConfig, source func(int, int, int) govpx.Image) (benchReport, error) {
	if source == nil {
		return runVP9Benchmark(cfg)
	}
	return runVP9BenchmarkInternal(cfg, source)
}

// runQualityFixtureSuite drives every fixture from qualityGateFixtures
// through runQualityGateBench and bundles the results into a suiteReport
// so the bench's suite-report formatter and quality gate code path can
// consume them unchanged.
func runQualityFixtureSuite(base benchConfig) (suiteReport, error) {
	fixtures := qualityGateFixtures()
	out := suiteReport{
		Name:         "vp9-quality-fixtures",
		Runs:         1,
		Selector:     "single-shot",
		LibvpxVpxenc: base.LibvpxVpxencVP9,
		QualitySkip:  base.SkipQuality,
		Cases:        make([]suiteCaseReport, 0, len(fixtures)),
	}
	for _, fx := range fixtures {
		report, err := runQualityGateBench(base, fx)
		if err != nil {
			return suiteReport{}, err
		}
		out.Cases = append(out.Cases, suiteCaseReport{
			Name:   fx.Name,
			Report: report,
		})
	}
	return out, nil
}
