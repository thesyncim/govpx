//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
	"testing"
)

// TestVP9Seed8Byte4Diag dumps the first 32 bytes of every frame for the
// historical RuntimeControls seed #8 alias ({0x32}). The seed now lives in
// vp9RuntimeControlsRegressionSeeds and byte-matches libvpx; the dump is kept
// as the closure audit for the old task #156 byte-4 interp-filter divergence.
func TestVP9Seed8Byte4Diag(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	requireVP9VpxencFrameFlagsOracle(t)
	seed := vp9RuntimeControlsRegressionSeeds[1]
	tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
	t.Logf("runtime-speed8-alias w=%d h=%d frames=%d cpu=%d flags=%v",
		tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

	got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
	want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
	if !seedByteIdentical(got, want) {
		t.Fatalf("historical RuntimeControls speed-8 alias lost byte parity")
	}

	for i := 0; i < len(got) && i < len(want); i++ {
		g := got[i]
		w := want[i]
		n := 32
		if len(g) < n {
			n = len(g)
		}
		if len(w) < n {
			n = len(w)
		}
		gh := make([]string, n)
		wh := make([]string, n)
		gb := make([]string, n)
		wb := make([]string, n)
		for k := 0; k < n; k++ {
			gh[k] = fmt.Sprintf("%02x", g[k])
			wh[k] = fmt.Sprintf("%02x", w[k])
			gb[k] = fmt.Sprintf("%08b", g[k])
			wb[k] = fmt.Sprintf("%08b", w[k])
		}
		t.Logf("frame=%d len_got=%d len_want=%d", i, len(g), len(w))
		t.Logf("  got hex : %s", joinSeed8Bytes(gh))
		t.Logf("  want hex: %s", joinSeed8Bytes(wh))
		var diffs []int
		for k := 0; k < n; k++ {
			if g[k] != w[k] {
				diffs = append(diffs, k)
			}
		}
		t.Logf("  diff_indexes_first32: %v", diffs)
		lo := 0
		hi := 12
		if hi > n {
			hi = n
		}
		for k := lo; k < hi; k++ {
			t.Logf("    [%2d] got=0x%02x %s  want=0x%02x %s  xor=0x%02x",
				k, g[k], gb[k], w[k], wb[k], g[k]^w[k])
		}
	}
}

func joinSeed8Bytes(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " "
		}
		out += v
	}
	return out
}
