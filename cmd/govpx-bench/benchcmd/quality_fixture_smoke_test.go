package benchcmd

import (
	"os"
	"testing"
)

// TestQualityFixtureGovpxOnly runs the canonical quality-gate fixtures
// through runVP9Benchmark (govpx-only, libvpx skipped) so we can verify
// each fixture stays above the default gate floors on the current
// vp9-port encoder. Skipped in -short mode; set
// GOVPX_BENCH_QUALITY_SMOKE=1 to force the run.
func TestQualityFixtureGovpxOnly(t *testing.T) {
	if testing.Short() && os.Getenv("GOVPX_BENCH_QUALITY_SMOKE") != "1" {
		t.Skip("skipping fixture smoke test in -short mode; set GOVPX_BENCH_QUALITY_SMOKE=1 to force run")
	}
	gate := defaultQualityGate()
	for _, fx := range qualityGateFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			cfg := benchConfig{
				Codec:       codecVP9,
				Width:       fx.Width,
				Height:      fx.Height,
				Frames:      10, // shorter than the 120 used by the gate so the test stays fast
				FPS:         fx.FPS,
				BitrateKbps: fx.BitrateKbps,
				Mode:        fx.Mode,
			}
			report, err := runVP9BenchmarkWithSource(cfg, fx.Source)
			if err != nil {
				t.Fatalf("runVP9BenchmarkWithSource returned error: %v", err)
			}
			if report.QualityFrames == 0 || report.PSNR <= 0 || report.SSIM <= 0 {
				t.Fatalf("quality metrics = frames:%d psnr:%f ssim:%f, want populated",
					report.QualityFrames, report.PSNR, report.SSIM)
			}
			t.Logf("%s govpx-only: psnr=%.2f dB ssim=%.5f output=%.2f kbps frames=%d/%d",
				fx.Name, report.PSNR, report.SSIM,
				report.OutputBitrateKbps, report.EncodedFrames, report.Frames)

			// Each fixture must stay above the documented gate floor.
			// A regression that drops these numbers below the floor
			// will land here first, before the production verify-
			// quality run.
			if report.PSNR < gate.MinPSNR {
				t.Fatalf("%s govpx PSNR %.2f dB < default floor %.2f dB; investigate before relaxing MinPSNR",
					fx.Name, report.PSNR, gate.MinPSNR)
			}
			if report.SSIM < gate.MinSSIM {
				t.Fatalf("%s govpx SSIM %.5f < default floor %.2f; investigate before relaxing MinSSIM",
					fx.Name, report.SSIM, gate.MinSSIM)
			}
		})
	}
}
