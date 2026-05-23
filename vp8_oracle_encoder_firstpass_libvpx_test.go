//go:build govpx_oracle_trace

package govpx

import (
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func captureLibvpxFirstPassStats(t *testing.T, vpxenc string, opts EncoderOptions, targetKbps int, frames []Image) []FirstPassFrameStats {
	t.Helper()
	deadline := "good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadline = "best"
	case DeadlineRealtime:
		deadline = "rt"
	}
	data, diag, err := vp8test.VpxencVP8FirstPassStatsI420(
		encoderValidationI420Bytes(t, frames),
		vp8test.VpxencVP8Config{
			BinaryPath:        vpxenc,
			Width:             opts.Width,
			Height:            opts.Height,
			Frames:            len(frames),
			Deadline:          deadline,
			CPUUsed:           opts.CpuUsed,
			TargetBitrateKbps: targetKbps,
			MinQ:              opts.MinQuantizer,
			MaxQ:              opts.MaxQuantizer,
			Timebase:          "1/" + strconv.Itoa(opts.FPS),
			FPS:               strconv.Itoa(opts.FPS) + "/1",
			ExtraArgs:         []string{"--end-usage=vbr"},
		},
	)
	if err != nil {
		t.Fatalf("vpxenc first pass failed: %v\n%s", err, diag)
	}
	return parseLibvpxFirstPassStats(t, data)
}
