//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleFirstPassStatsCompare(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run first-pass oracle comparison")
	}
	vpxenc := vp8test.Vpxenc(t)

	const (
		width      = 32
		height     = 32
		fps        = 30
		targetKbps = 400
	)
	cases := []struct {
		name   string
		frames []Image
	}{
		{name: "ramp", frames: firstPassOracleFrames(3, func(i int) Image {
			return firstPassOracleRampFrame(width, height, i)
		})},
		{name: "y4m-shaped", frames: firstPassOracleFrames(4, func(i int) Image {
			return firstPassOracleY4MFrame(width, height, i)
		})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  60,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
			}
			govpxStats := captureGovpxFirstPassStats(t, opts, tc.frames)
			libvpxStats := captureLibvpxFirstPassStats(t, vpxenc, opts, targetKbps, tc.frames)
			compareFirstPassStats(t, govpxStats, libvpxStats)
		})
	}
}
