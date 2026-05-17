//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

// TestVP9DeferredSeedsRemeasureRefControl re-measures strict byte-parity for
// every entry in vp9RefControlsSeedsDeferred under whichever opt-in env gates
// are active. Reports a per-seed PASS/FAIL plus aggregate counts so the
// caller can decide whether to flip the gate default to ON and un-defer
// individual seeds. Intentionally non-asserting (always passes) so it can
// run in the gate without forcing the not-yet-libvpx-faithful divergences
// to fail — siblings TestVP9NonrdPickPartitionDeferredSeedsProgress and the
// fuzz harness itself enforce the actual gating.
//
// Measurement under
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 GOVPX_VP9_NONRD_PICK_PARTITION=1
// (this commit): PASS=0/9 FAIL=9/9. Inter frames diverge at byte 9
// (FirstPartitionSize literal) by 39-552 bytes. Closure path: port libvpx
// vp9_pick_inter_mode (vp9_pickmode.c:1696) so the recursive walker's
// per-leaf MV / tx_size / interp picks match libvpx byte-exactly.
func TestVP9DeferredSeedsRemeasureRefControl(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to remeasure deferred RefControl seeds")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	t.Logf("gate: GOVPX_VP9_NONRD_PICK_PARTITION=%q GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=%q",
		os.Getenv("GOVPX_VP9_NONRD_PICK_PARTITION"),
		os.Getenv("GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING"))

	pass, fail := 0, 0
	for idx, seed := range vp9RefControlsSeedsDeferred {
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("refctrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		tc := newVP9RefControlsFuzzCase(seed)
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources,
			tc.flags, tc.extraArgs)
		if seedByteIdentical(got, want) {
			t.Logf("%s PASS (frames=%d)", label, len(got))
			pass++
			continue
		}
		fail++
		n := len(got)
		if len(want) < n {
			n = len(want)
		}
		firstMis := -1
		for i := 0; i < n; i++ {
			g := sha256.Sum256(got[i])
			w := sha256.Sum256(want[i])
			if g != w {
				firstMis = i
				t.Logf("%s FAIL: first_mismatch_frame=%d got_len=%d want_len=%d first_byte_diff=%d",
					label, i, len(got[i]), len(want[i]),
					firstVP9PacketDiffForTest(got[i], want[i]))
				break
			}
		}
		if firstMis < 0 {
			t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d",
				label, len(got), len(want))
		}
	}
	t.Logf("RefControl deferred-seed remeasure: PASS=%d FAIL=%d total=%d",
		pass, fail, len(vp9RefControlsSeedsDeferred))
}

// TestVP9DeferredSeedsRemeasureRuntimeControls is the sibling probe for the
// vp9RuntimeControlsSeedsDeferred set.
//
// Measurement under
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 GOVPX_VP9_NONRD_PICK_PARTITION=1
// (this commit): PASS=0/9 FAIL=9/9. Seeds #0/#2/#4/#6 diverge frame 0 at
// byte 9 (cost_coeffs proxy gap); seeds #1/#7 at byte 16 (RT cpu_used=-3
// coef_prob_appx_step amplification); seed #3 (cpu=-8) at frame 1 byte 8;
// seeds #5 and "2" alias hit structural ErrInvalidConfig / Conflicting
// flags pending the set_ext_overrides resolution port.
//
// Intentionally non-asserting — see RefControl sibling for rationale.
func TestVP9DeferredSeedsRemeasureRuntimeControls(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to remeasure deferred RuntimeControls seeds")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	t.Logf("gate: GOVPX_VP9_NONRD_PICK_PARTITION=%q GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=%q",
		os.Getenv("GOVPX_VP9_NONRD_PICK_PARTITION"),
		os.Getenv("GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING"))

	// Seed indices that hit the set_ext_overrides structural-reject
	// path (govpx returns ErrInvalidConfig, libvpx returns
	// "Conflicting flags"). These are skipped here so the
	// measurement test stays green — the underlying handoff is
	// already documented under seed #5 and the "2" alias in the
	// deferred list.
	structuralReject := map[int]bool{
		5: true, // {1,2,1,0,4,1,0,1} — EncodeForceGoldenFrame|EncodeNoUpdateGolden
		8: true, // []byte("2") alias of seed #5 family
	}

	pass, fail, skipped := 0, 0, 0
	for idx, seed := range vp9RuntimeControlsSeedsDeferred {
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("runtimectrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		if structuralReject[idx] {
			t.Logf("%s STRUCTURAL_REJECT (set_ext_overrides handoff — see deferred list)",
				label)
			skipped++
			continue
		}
		tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
		t.Logf("%s w=%d h=%d frames=%d cpu=%d flags=%v",
			label, tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources,
			tc.flags, tc.extraArgs)
		if seedByteIdentical(got, want) {
			t.Logf("%s PASS (frames=%d)", label, len(got))
			pass++
			continue
		}
		fail++
		n := len(got)
		if len(want) < n {
			n = len(want)
		}
		firstMis := -1
		for i := 0; i < n; i++ {
			g := sha256.Sum256(got[i])
			w := sha256.Sum256(want[i])
			if g != w {
				firstMis = i
				t.Logf("%s FAIL: first_mismatch_frame=%d got_len=%d want_len=%d first_byte_diff=%d",
					label, i, len(got[i]), len(want[i]),
					firstVP9PacketDiffForTest(got[i], want[i]))
				break
			}
		}
		if firstMis < 0 {
			t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d",
				label, len(got), len(want))
		}
	}
	t.Logf("RuntimeControls deferred-seed remeasure: PASS=%d MISMATCH=%d STRUCTURAL_REJECT=%d total=%d",
		pass, fail, skipped, len(vp9RuntimeControlsSeedsDeferred))
}

func seedByteIdentical(got, want [][]byte) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		g := sha256.Sum256(got[i])
		w := sha256.Sum256(want[i])
		if g != w {
			return false
		}
	}
	return true
}
