//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP9Seed8SearchFilterRefProbe runs the historical RuntimeControls seed
// #8 alias ({0x32}) through govpx and reports the trace-build
// search_filter_ref probe counts. The byte-parity closure for this seed lives
// in TestVP9RuntimeControlsSpeed8RegressionSeedsByteParity; this diagnostic
// remains trace-only and non-production.
func TestVP9Seed8SearchFilterRefProbe(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	requireVP9VpxencFrameFlagsOracle(t)
	seed := vp9RuntimeControlsRegressionSeeds[1]
	tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
	t.Logf("runtime-speed8-alias w=%d h=%d frames=%d cpu=%d flags=%v",
		tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

	ResetVP9SearchFilterRefProbes()
	_ = encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
	fires, flips := ProbeVP9SearchFilterRefFires()
	t.Logf("search_filter_ref fires=%d flips=%d", fires, flips)
}
