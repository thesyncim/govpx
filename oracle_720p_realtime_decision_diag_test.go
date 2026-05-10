package govpx

import (
	"bytes"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

// TestOracle720pRealtimeDecisionDiag compares the 1280x720 realtime CBR
// bench fixture at cpu-used=8 and logs the first decision-row divergence.
// This is a debug-only microscope for the remaining bitrate / quality gap
// against libvpx.
func TestOracle720pRealtimeDecisionDiag(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the 720p realtime decision diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 1200
		frames     = 30
	)
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    999,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = scoreboardBenchNoiseFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-720p-realtime", opts, targetKbps, sources, []string{
		"--end-usage=cbr",
		"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
		"--undershoot-pct=100", "--overshoot-pct=15",
		"--threads=1", "--noise-sensitivity=0",
	})

	govProj := projectOracleDecisionTrace(t, govpxTrace)
	libProj := projectOracleDecisionTrace(t, libvpxTrace)
	div, err := coracle.CompareOracleTraces(bytes.NewReader(govProj), bytes.NewReader(libProj), coracle.CompareOptions{
		MaxDivergences: 512,
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) == 0 {
		t.Log("no projected decision divergences")
		return
	}
	t.Logf("projected decision divergences: %d", len(div))
	t.Logf("%s", formatOracleTraceDivergences(div[:min(len(div), 32)]))
	if d := div[0]; d.Field == "q_index" || d.Field == "projected_frame_size" || d.Field == "this_frame_target" {
		dumpRateRow(t, "govpx", govpxTrace, d.FrameIndex)
		dumpRateRow(t, "libvpx", libvpxTrace, d.FrameIndex)
	}
}
