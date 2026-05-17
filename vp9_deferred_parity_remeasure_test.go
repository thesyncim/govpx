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
// are active. Reports a per-seed PASS/FAIL plus aggregate size_delta and
// counts so the caller can decide whether to flip the gate default to ON and
// un-defer individual seeds. Intentionally non-asserting (always passes) so
// it can run in the gate without forcing the not-yet-libvpx-faithful
// divergences to fail — siblings TestVP9NonrdPickPartitionDeferredSeedsProgress
// and the fuzz harness itself enforce the actual gating.
//
// Measurement (task #148, this commit) under
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 GOVPX_VP9_NONRD_PICK_PARTITION=1:
//
//	PASS=0/10 FAIL=10/10. Frame 0 (keyframe) diverges uniformly at
//	first_byte_diff=17 with got_len=3014 want_len=3040 (a -26 byte
//	keyframe deficit; under-shoots in compressed-header coef-payload).
//	Per-seed aggregate size_delta (sum across all frames):
//	  af5570f5: +55, b9af55f0: -105, fda5b6b4: +204, ffa55725: +43,
//	  8ec0abe5: +304, 9c3e08e8: +483, 5feceb66: +65, 6b86b273: +549,
//	  d4735e3a: +380, 7902699b: +24. Aggregate +2002 / avg +200B/seed.
//
// Identical to the f5fe476 (#142) baseline byte-for-byte despite the
// 838691b token-cost / b87ff4d super_block_uvrd / 404c7dd intra-only
// counts landings since #142. Closure path: route the picker's
// mrdTxSize through to the leaf commit so pickVP9InterTxSize stops
// overriding the picker's libvpx-faithful tx_size decision
// (vp9_encoder.go:8498/8513) AND close the keyframe -26 byte
// first_byte_diff=17 deficit (compressed-header coef-update payload).
func TestVP9DeferredSeedsRemeasureRefControl(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to remeasure deferred RefControl seeds")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	t.Logf("gate: GOVPX_VP9_NONRD_PICK_PARTITION=%q GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=%q",
		os.Getenv("GOVPX_VP9_NONRD_PICK_PARTITION"),
		os.Getenv("GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING"))

	pass, fail := 0, 0
	aggSizeDelta := 0
	for idx, seed := range vp9RefControlsSeedsDeferred {
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("refctrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		tc := newVP9RefControlsFuzzCase(seed)
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources,
			tc.flags, tc.extraArgs)
		seedDelta := seedSizeDelta(got, want)
		aggSizeDelta += seedDelta
		if seedByteIdentical(got, want) {
			t.Logf("%s PASS (frames=%d size_delta=%+d)", label, len(got), seedDelta)
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
				t.Logf("%s FAIL: first_mismatch_frame=%d got_len=%d want_len=%d first_byte_diff=%d size_delta=%+d",
					label, i, len(got[i]), len(want[i]),
					firstVP9PacketDiffForTest(got[i], want[i]),
					seedDelta)
				break
			}
		}
		if firstMis < 0 {
			t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d size_delta=%+d",
				label, len(got), len(want), seedDelta)
		}
	}
	t.Logf("RefControl deferred-seed remeasure: PASS=%d FAIL=%d total=%d agg_size_delta=%+d avg_per_seed=%+d",
		pass, fail, len(vp9RefControlsSeedsDeferred), aggSizeDelta,
		aggSizeDelta/max(1, len(vp9RefControlsSeedsDeferred)))
}

// TestVP9DeferredSeedsRemeasureRuntimeControls is the sibling probe for the
// vp9RuntimeControlsSeedsDeferred set.
//
// Measurement (task #148, this commit) under
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 GOVPX_VP9_NONRD_PICK_PARTITION=1:
//
//	PASS=0/8 measurable FAIL=8/8 (STRUCTURAL_REJECT=2 #5/#8). Seeds
//	#0/#2/#4/#6 (cpu=0 panning content) diverge frame 0 at byte 9
//	(cost_coeffs proxy gap); seeds #1/#7 (cpu=-3) at byte 16 (RT
//	speed=3 coef_prob_appx_step amplification); seed #3 (cpu=-8) at
//	byte 17; seed #9 (cpu=4) at byte 16.
//
// Per-seed aggregate size_delta (sum across all frames):
//
//	#0: +2754, #1: +4141, #2: +7038, #3: -262, #4: +6808,
//	#6: +2754, #7: +8971, #9: +2293. Aggregate +34497 / avg +4312
//	per measurable seed.
//
// Frame-0 size_delta (comparable to f5fe476 / #142):
//
//	#0: +996, #1: +995, #2: +2276, #3: -31, #4: +996, #6: +996,
//	#7: +2285, #9: +47. Down ~10-23 bytes from #142 on seeds
//	#0/#2/#4/#6 (token-cost reconcile + super_block_uvrd nibble);
//	seeds #3/#9 unchanged. Structural cost_coeffs gap dominates.
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
	aggSizeDelta := 0
	measured := 0
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
		seedDelta := seedSizeDelta(got, want)
		aggSizeDelta += seedDelta
		measured++
		if seedByteIdentical(got, want) {
			t.Logf("%s PASS (frames=%d size_delta=%+d)", label, len(got), seedDelta)
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
				t.Logf("%s FAIL: first_mismatch_frame=%d got_len=%d want_len=%d first_byte_diff=%d size_delta=%+d",
					label, i, len(got[i]), len(want[i]),
					firstVP9PacketDiffForTest(got[i], want[i]),
					seedDelta)
				break
			}
		}
		if firstMis < 0 {
			t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d size_delta=%+d",
				label, len(got), len(want), seedDelta)
		}
	}
	t.Logf("RuntimeControls deferred-seed remeasure: PASS=%d MISMATCH=%d STRUCTURAL_REJECT=%d total=%d agg_size_delta=%+d avg_per_measurable=%+d",
		pass, fail, skipped, len(vp9RuntimeControlsSeedsDeferred), aggSizeDelta,
		aggSizeDelta/max(1, measured))
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

// seedSizeDelta returns the signed sum of (len(got[i]) - len(want[i])) across
// every frame index measurable on both sides (using min(len(got),len(want))).
// Positive = govpx emits more bytes than libvpx; negative = govpx under-shoots.
func seedSizeDelta(got, want [][]byte) int {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	delta := 0
	for i := 0; i < n; i++ {
		delta += len(got[i]) - len(want[i])
	}
	return delta
}
