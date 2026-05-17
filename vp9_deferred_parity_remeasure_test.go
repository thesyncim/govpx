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
// (this commit): PASS=0/10 FAIL=10/10. Inter frames diverge at byte 9
// (FirstPartitionSize literal) by 39-552 bytes. After task #119's port of
// find_predictors's frame_mv table + the libvpx-exact mode_checked /
// NEARESTMV dedup paths (vp9_pickmode.c:1710 + 2269-2299), the aggregate
// per-seed size_delta flipped from +3900 bytes (pre-#119 baseline) to
// -716 bytes (avg -80B/seed); individual seeds now under-shoot or
// over-shoot libvpx by 42-502 bytes vs the previous uniform +30-552 B
// over-shoot.
//
// Re-measurement (task #142, this commit) after Phase E1b/c keyframe
// RD landings (a2f325c cost_coeffs port, d2baaf8 sub-8x8 4x4 rewire,
// 8399e87 sub-8x8 intra picker, e95504e estimate_block_intra,
// 7017378 block_yrd compare + breakout): aggregate per-seed
// size_delta is now +2002 bytes (avg +200B/seed) across 10 seeds.
// Sign flipped vs #119 and a new regression at frame 0 surfaces:
// the previously byte-exact keyframe now diverges with got_len=3014
// vs want_len=3040 first_byte_diff=17 across every seed (a uniform
// -26 byte under-shoot of the compressed-header payload). See the
// expanded citation at vp9RefControlsSeedsDeferred.
//
// Closure path: (a) audit pickVP9KeyframeBlockTxSize + the
// sub-8x8 keyframe RD glue against vp9_rdopt.c:907-1023 +
// 3604-3700 — the new keyframe deficit means the rate-RD scorer
// is now sub-libvpx by 26 bytes worth of coef rate; (b) route the
// picker's mrdTxSize through to the leaf commit so
// pickVP9InterTxSize stops overriding the picker's
// libvpx-faithful tx_size decision (vp9_encoder.go:8498/8513).
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
// Re-measurement (task #142, this commit) after Phase E1b/c keyframe
// RD landings: aggregate frame-0 size_delta across the 8 measurable
// seeds is +8618 bytes (avg +1077 B/seed). Per-seed: #0 +1006,
// #1 +997, #2 +2299, #3 -31, #4 +1006, #6 +1006, #7 +2288, #9 +47.
// Seed #3 (cpu=-8 RT speed=8) is now within ±50 bytes; all others
// remain >|500| bytes pending the compressed-header writer port.
// First_byte_diff bytes unchanged from #119 baseline. See expanded
// citation at vp9RuntimeControlsSeedsDeferred.
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
