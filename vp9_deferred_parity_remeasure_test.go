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
// Measurement under GOVPX_VP9_NONRD_PICK_PARTITION=1 (the
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING gate is a no-op for these ML-based
// RefControl seeds once nonrd_pick_partition is active):
//
//	PASS=0/10 FAIL=10/10 (10/70 frames byte-match, was 0/70). After
//	task #146's port of the libvpx-faithful x->skip + bestEarlyTerm
//	control-flow (vp9_pickmode.c:2460/2478-2488), the strict-<
//	winner-selection (vp9_pickmode.c:2460), the
//	sse_zeromv_normalized + CBR golden-skip gate (vp9_pickmode.c:
//	2350-2354 + 2123-2126), and the removal of govpx's heuristic
//	1/64-ratio early-term gate AND in combination with the
//	keyframe-coeff / hybrid-nonrd / variance-part-thresh-mult ports,
//	and the low-res partition predictor fixes (Q3 int-pro MV -> Q4 luma
//	convolve conversion plus LAST-buffer estimation even when LAST is masked
//	for coding), per-seed aggregate size_delta (sum across all frames)
//	shrinks from the f5fe476 / #142 baseline aggregate +2002
//	(avg +200B/seed) to:
//	  af5570f5: +44, b9af55f0: +71, fda5b6b4: +295, ffa55725: +233,
//	  8ec0abe5: +132, 9c3e08e8: -120, 5feceb66: -138, 6b86b273: +48,
//	  d4735e3a: -179, 7902699b: +60. Aggregate +446 / avg +44B/seed.
//	  Range -179..+295 (was uniformly +24..+549 pre-merge).
//
// Closure path: the raw mrdTxSize leaf-commit experiment was too broad
// because the nonrd scorer caps / forces tx sizes before some block_yrd
// scoring paths. The next tx-size slice should carry a capped candidate
// through the same vp9InterTxApplyForces safety path, then remeasure the
// remaining +200..+300 residuals (fda5b6b4 / ffa55725).
//
// Task #151 verification (post-b36888f tip): cost_coeffs is wired through
// the second-tier RD chain — see TestVP9DeferredSeedsRemeasureRuntimeControls
// docstring for the four integration points and libvpx file:line citations.
// The RefControl aggregate (+44B/seed avg) confirms the wiring is in place;
// remaining gap is dominated by the nonrd Tx-size leaf-commit slice noted
// above, not by a missing cost_coeffs port.
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
// Measurement (task #150, this commit — set_ext_overrides port) at the
// default gate (no opt-in):
//
//	PASS=0/10 measurable FAIL=10/10 STRUCTURAL_REJECT=0/10. Seeds
//	#0/#2/#4/#6 (cpu=0 panning content) diverge frame 0 at byte 9
//	(cost_coeffs proxy gap); seeds #1/#5/#7 (cpu=-3, RT speed=3)
//	at byte 16 (coef_prob_appx_step amplification); seed #3 (cpu=-8
//	frame 1) at byte 9; seed #8 (cpu=-8 frame 1) at byte 4 (RT
//	speed=8 compressed-header coef-update walk); seed #9 (cpu=4)
//	at byte 17. Seeds #5 and #8 transitioned from STRUCTURAL_REJECT
//	to MISMATCH after the libvpx vp9_apply_encoding_flags +
//	set_ext_overrides routing landed (vp9_encoder.c:6812-6843 +
//	vp9_encoder.c:4761-4775, plumbed in vp9_ext_overrides.go).
//
// Per-seed aggregate size_delta (sum across all frames) at default gate:
//
//	#0: +2754, #1: +4141, #2: +7038, #3: +5462, #4: +6808,
//	#5: +10609 (NEW — frame 0 KF cpu=-3, first_byte_diff=16),
//	#6: +2754, #7: +8971,
//	#8: +4185  (NEW — frame 1 inter cpu=-8, first_byte_diff=4),
//	#9: +2854. Aggregate +55576 / avg +5557 per measurable seed.
//
// Frame-0 size_delta (comparable to f5fe476 / #142):
//
//	#0: +996, #1: +995, #2: +2276, #3: -31, #4: +996, #6: +996,
//	#7: +2285, #9: +47. Down ~10-23 bytes from #142 on seeds
//	#0/#2/#4/#6 (token-cost reconcile + super_block_uvrd nibble);
//	seeds #3/#9 unchanged.
//
// Status (#151 closure on cost_coeffs second-tier RD chain):
//
//   - libvpx vp9_rdopt.c:358-459 (cost_coeffs) is wired through all four
//     intra-RD integration points:
//
//   - super_block_yrd (vp9_rdopt.c:1025-1042) analog ->
//     scoreVP9KeyframeModeTransformRD (vp9_encoder.go:8143) ->
//     vp9KeyframeCoeffBlockRateCost (vp9_encoder.go:9144).
//
//   - super_block_uvrd (vp9_rdopt.c:1418-1466) analog ->
//     scoreVP9KeyframeUvPlaneRD (vp9_encoder.go:8638) ->
//     vp9KeyframeUvCoeffBlockRateCost (vp9_encoder.go:9163).
//
//   - choose_tx_size_from_rd (vp9_rdopt.c:907-1023) analog ->
//     pickVP9KeyframeBlockTxSize (vp9_encoder.go:8916) ->
//     vp9KeyframeCoeffBlockRateCost (vp9_encoder.go:9079).
//
//   - rd_pick_intra4x4block (vp9_rdopt.c:1061-1297) analog ->
//     pickVP9Sub4x4IntraBlockMode (vp9_encoder.go:7771) ->
//     vp9KeyframeCoeffBlockRateCost (vp9_encoder.go:7882).
//
//   - Residual now driven by orthogonal non-cost_coeffs gaps: speed=3
//     RT coef_prob_appx_step (libvpx vp9_encoder.c:5024-5039),
//     speed=8 partition heuristic differences (vp9_pickmode.c:1696),
//     and Tx32x32 qcoeff recovery drift in vp9CoeffTokenAbsVal
//     (vp9_encoder.go:10269 — recovers qcoeff from dqcoeff via
//     /dq; loss-of-precision when dqcoeff = q*dq/2 is truncated).
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

	// Task #150: seeds #5 and #8 previously skipped here as
	// STRUCTURAL_REJECT because the fuzz materialiser's
	// normalizeVP9EncodeFlags resolution of the libvpx
	// vp9/vp9_cx_iface.c:1394-1398 "Conflicting flags." rejection
	// was the only place the FORCE_GF + NO_UPD_GF conflict was
	// pre-resolved. With vp9_apply_encoding_flags (libvpx
	// vp9/encoder/vp9_encoder.c:6812-6843) and set_ext_overrides
	// (libvpx vp9/encoder/vp9_encoder.c:4761-4775) now ported
	// verbatim via vp9_ext_overrides.go and the encoder body
	// running the same ext_refresh_* -> refresh_*_frame routing
	// libvpx uses, both seeds reach the per-frame encode loop and
	// are measurable. They still mismatch byte-exact (the dominant
	// residual is the cost_coeffs rate-proxy gap at
	// vp9_rdopt.c:358) but are no longer structural rejects.

	pass, fail, skipped := 0, 0, 0
	_ = skipped // task #150: no seed is STRUCTURAL_REJECT after set_ext_overrides port.
	aggSizeDelta := 0
	measured := 0
	for idx, seed := range vp9RuntimeControlsSeedsDeferred {
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("runtimectrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
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
