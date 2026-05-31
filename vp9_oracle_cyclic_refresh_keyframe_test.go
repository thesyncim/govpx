//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleCBRCyclicRefreshKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewPanningYCbCr(width, height, 0)
	opts := vp9oracle.CBROptions(width, height, 700)
	opts.AQMode = govpx.VP9AQCyclicRefresh
	opts.Deadline = govpx.DeadlineRealtime
	opts.CpuUsed = -8
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, opts, []string{
		"--end-usage=cbr",
		"--target-bitrate=700",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--cpu-used=8",
		"--aq-mode=3",
	})
}
