//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleRealtimeZeroCPUUsesSpeed8(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	first := vp9test.NewYCbCr(width, height, 128, 128, 128)
	second := vp9test.NewYCbCr(width, height, 160, 128, 128)

	vp9oracle.AssertTwoFrameByteParityWithOptions(t, first, second,
		govpx.VP9EncoderOptions{
			Deadline: govpx.DeadlineRealtime,
			CpuUsed:  0,
		},
		[]string{
			"--rt",
			"--cpu-used=8",
		})
}
