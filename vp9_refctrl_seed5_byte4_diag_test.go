//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
	"testing"
)

// TestVP9RefCtrlSeed5Byte4Diag dumps the first 32 bytes of every frame for
// seed #5 of vp9RefControlsSeedsDeferred ({0, 7, 0, 8, 0, 9, 0, 10}). The
// seed materialises a 64x64 6-frame fixture where frame 1 diverges from
// libvpx at byte 4. This diagnostic confirms (or refutes) attribution of
// the divergence to write_frame_size_with_refs (libvpx vp9_bitstream.c:
// 1180-1212): if byte 4 differs at bit 30/31/32 (the 3 "found" flags)
// the divergence is in the frame-size-with-refs writer; if it differs at
// bit 33+ the divergence is downstream (interp_filter / allow_hp_mv).
//
// Task #167 found writeFrameSizeWithRefs (internal/vp9/encoder/
// header_writer.go:159-191) to be a verbatim port of libvpx; the
// dump should show byte-exact bits 30-32 with the divergence localised
// to bit 33 (interp_filter switchable) — the same root cause as seed #8
// of vp9RuntimeControlsSeedsDeferred (task #156 audit).
func TestVP9RefCtrlSeed5Byte4Diag(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	requireVP9VpxencFrameFlagsOracle(t)
	seed := vp9RefControlsSeedsDeferred[5]
	tc := newVP9RefControlsFuzzCase(seed)
	t.Logf("refctrl_seed#5 w=%d h=%d frames=%d cpu=%d flags=%v",
		tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

	got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
	want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)

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
