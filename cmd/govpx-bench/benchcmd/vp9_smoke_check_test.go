package benchcmd

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestVP9EncodeDecodeRoundtripsSolidColor verifies that the VP9
// encode + decode plumbing in this package reconstructs a deterministic
// solid-color fixture cleanly. A non-trivial gap between source and
// decoded output (e.g. plane stride misuse, U/V swapped, or wrong
// destination layout) would show as a very low PSNR even at 720p; we
// fail loudly if that happens so the gate-failure investigation can
// rule the plumbing out.
func TestVP9EncodeDecodeRoundtripsSolidColor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plumbing roundtrip in -short mode")
	}
	const (
		width  = 64
		height = 64
		frames = 4
	)
	report, err := runVP9BenchmarkWithSource(benchConfig{
		Codec:       codecVP9,
		Width:       width,
		Height:      height,
		Frames:      frames,
		FPS:         30,
		BitrateKbps: 600,
		Mode:        "realtime",
	}, solidColorSource)
	if err != nil {
		t.Fatalf("runVP9BenchmarkWithSource solid-color returned error: %v", err)
	}
	if report.PSNR < 40 {
		t.Fatalf("solid-color PSNR = %.2f dB, want >=40 (plumbing problem?)", report.PSNR)
	}
	if report.SSIM < 0.98 {
		t.Fatalf("solid-color SSIM = %.5f, want >=0.98 (plumbing problem?)", report.SSIM)
	}
	t.Logf("solid-color VP9 roundtrip: psnr=%.2f dB ssim=%.5f frames=%d/%d output=%.2f kbps",
		report.PSNR, report.SSIM, report.QualityFrames, frames, report.OutputBitrateKbps)
}

func solidColorSource(width int, height int, _ int) govpx.Image {
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
	for i := range img.Y {
		img.Y[i] = 128
	}
	for i := range img.U {
		img.U[i] = 128
	}
	for i := range img.V {
		img.V[i] = 128
	}
	return img
}
