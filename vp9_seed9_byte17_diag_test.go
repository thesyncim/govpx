//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
	"testing"
)

// TestVP9Seed9Byte17Diag dumps the first 32 bytes of every frame for seed #9
// of vp9RuntimeControlsSeedsDeferred. The fuzz seed `{0x37}` materialises a
// (128x64, 6-frame, cpu_used=4, RealTime) clip where frame 0 (KF) diverges
// from libvpx at byte 17.
//
// Bit-accounting (task #168): byte 17 = bits 136-143 of the uncompressed
// header. Bits 124-139 hold the 16-bit first_partition_size literal
// (libvpx vp9/encoder/vp9_bitstream.c:1441 pack_bitstream back-patched
// saved_wb / govpx internal/vp9/encoder/header_writer.go:155
// WriteLiteral(FirstPartitionSize,16)). Bit 139 (xor=0x10 in the
// observed dump) is the LSB of that 16-bit literal — govpx emits
// compressed_hdr_size=128, libvpx emits 129.
//
// The 1-byte compressed_hdr_size delta is downstream of per-leaf RD
// pick / quant differences in the same cost_coeffs chain documented
// for the byte-9 / byte-16 cluster on the RefControl deferred seeds.
// Byte 17 here is a SHIFTED OBSERVATION of that same root cause, not
// a separate writer-level bug — the uncompressed-header writer is
// verbatim libvpx. This diag is retained as the byte-attribution
// audit trail so any future re-measurement under closure of the
// upstream gap can confirm byte 17 either lands at byte-parity (when
// compressed_hdr_size matches) or shifts byte position with the
// surrounding uncompressed-header bit budget.
func TestVP9Seed9Byte17Diag(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	requireVP9VpxencFrameFlagsOracle(t)
	seed := vp9RuntimeControlsSeedsDeferred[9]
	tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
	t.Logf("seed#9 w=%d h=%d frames=%d cpu=%d flags=%v",
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
		lo := 12
		hi := 24
		if hi > n {
			hi = n
		}
		for k := lo; k < hi; k++ {
			t.Logf("    [%2d] got=0x%02x %s  want=0x%02x %s  xor=0x%02x",
				k, g[k], gb[k], w[k], wb[k], g[k]^w[k])
		}
	}
}
