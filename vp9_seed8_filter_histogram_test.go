//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP9Seed8FilterHistogram runs the historical RuntimeControls seed #8
// alias ({0x32}) through govpx with a hook that dumps the per-frame
// counts.SwitchableInterp histogram before fix_interp_filter (libvpx
// vp9_bitstream.c:864-885) runs. The seed now lives in
// vp9RuntimeControlsRegressionSeeds; this diagnostic is retained as the
// closure audit for the old task #156 byte-4 filter-histogram divergence.
func TestVP9Seed8FilterHistogram(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	requireVP9VpxencFrameFlagsOracle(t)
	seed := vp9RuntimeControlsRegressionSeeds[1]
	tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
	t.Logf("runtime-speed8-alias w=%d h=%d frames=%d cpu=%d flags=%v",
		tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

	prev := vp9SwitchableInterpHistogramHook
	defer func() { vp9SwitchableInterpHistogramHook = prev }()
	vp9SwitchableInterpHistogramHook = func(frameIdx int,
		hist [vp9SwitchableInterpHistogramContexts][vp9SwitchableInterpHistogramFilters]uint32,
		total [vp9SwitchableInterpHistogramFilters]uint32,
		c int) {
		t.Logf("frame=%d c=%d totals=[E=%d, S=%d, H=%d]",
			frameIdx, c, total[0], total[1], total[2])
		for j := 0; j < vp9SwitchableInterpHistogramContexts; j++ {
			t.Logf("  ctx[%d]: E=%d S=%d H=%d",
				j, hist[j][0], hist[j][1], hist[j][2])
		}
	}

	ResetVP9SearchFilterRefProbes()
	_ = encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
	fires, flips := ProbeVP9SearchFilterRefFires()
	t.Logf("search_filter_ref fires=%d flips=%d", fires, flips)
}
