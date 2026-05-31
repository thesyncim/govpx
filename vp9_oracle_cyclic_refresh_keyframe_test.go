//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleCBRCyclicRefreshKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewPanningYCbCr(width, height, 0)
	opts := vp9oracle.CyclicRefreshCBROptions(width, height, 700)
	args := vp9oracle.CyclicRefreshCBRVpxencArgs(700, 600, 400, 500, 0)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, opts, args)
}
