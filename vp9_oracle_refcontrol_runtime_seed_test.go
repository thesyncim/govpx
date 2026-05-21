//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP9OracleReferenceControlSeedsMatchLibvpx asserts strict byte parity for the
// pinned RefControl corpus. These schedules exercise per-frame reference
// update and force/no-reference flags against the libvpx frameflags oracle.
func TestVP9OracleReferenceControlSeedsMatchLibvpx(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 RefControl regression seeds")
	coracletest.VpxencVP9FrameFlags(t)

	pass, fail := 0, 0
	aggSizeDelta := 0
	for idx, seed := range vp9RefControlParitySeeds {
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("refctrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		tc := newVP9RefControlsFuzzCase(seed)
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9LibvpxFrameFlags(tc.flags), tc.extraArgs...)
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
					vp9test.FirstPacketDiff(got[i], want[i]),
					seedDelta)
				break
			}
		}
		if firstMis < 0 {
			t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d size_delta=%+d",
				label, len(got), len(want), seedDelta)
		}
	}
	t.Logf("RefControl seed corpus: PASS=%d FAIL=%d total=%d agg_size_delta=%+d avg_per_seed=%+d",
		pass, fail, len(vp9RefControlParitySeeds), aggSizeDelta,
		aggSizeDelta/max(1, len(vp9RefControlParitySeeds)))
	if fail != 0 || aggSizeDelta != 0 {
		t.Fatalf("RefControl seed corpus lost byte parity: fail=%d agg_size_delta=%+d",
			fail, aggSizeDelta)
	}
}

// TestVP9OracleRuntimeControlSpeed8SeedsMatchLibvpx asserts byte parity for
// the RuntimeControls speed-8 non-RD seed set. Slower speed lanes remain in the
// open-lane diagnostic below until they close against libvpx.
func TestVP9OracleRuntimeControlSpeed8SeedsMatchLibvpx(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 RuntimeControls speed-8 regression seeds")
	coracletest.VpxencVP9FrameFlags(t)

	pass, fail := 0, 0
	aggSizeDelta := 0
	for idx, seed := range vp9RuntimeControlsSpeed8ParitySeeds {
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("runtimectrl-speed8-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
		if tc.opts.CpuUsed != -8 {
			t.Fatalf("%s materialised cpu=%d, want speed-8 nonrd cpu=-8", label, tc.opts.CpuUsed)
		}
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9LibvpxFrameFlags(tc.flags), tc.extraArgs...)
		seedDelta := seedSizeDelta(got, want)
		aggSizeDelta += seedDelta
		if seedByteIdentical(got, want) {
			t.Logf("%s PASS (frames=%d size_delta=%+d)", label, len(got), seedDelta)
			pass++
			continue
		}
		fail++
		firstMis := firstVP9MismatchingFrame(got, want)
		if firstMis >= 0 && firstMis < len(got) && firstMis < len(want) {
			t.Errorf("%s FAIL: first_mismatch_frame=%d got_len=%d want_len=%d first_byte_diff=%d size_delta=%+d",
				label, firstMis, len(got[firstMis]), len(want[firstMis]),
				vp9test.FirstPacketDiff(got[firstMis], want[firstMis]), seedDelta)
		} else {
			t.Errorf("%s FAIL: frame_count_mismatch got=%d want=%d size_delta=%+d",
				label, len(got), len(want), seedDelta)
		}
	}
	t.Logf("RuntimeControls speed-8 seed corpus: PASS=%d FAIL=%d total=%d agg_size_delta=%+d avg_per_seed=%+d",
		pass, fail, len(vp9RuntimeControlsSpeed8ParitySeeds), aggSizeDelta,
		aggSizeDelta/max(1, len(vp9RuntimeControlsSpeed8ParitySeeds)))
	if fail != 0 || aggSizeDelta != 0 {
		t.Fatalf("RuntimeControls speed-8 seed corpus lost byte parity: fail=%d agg_size_delta=%+d",
			fail, aggSizeDelta)
	}
}

// TestVP9OracleRuntimeControlOpenGapSeedsRemainReproducible keeps the remaining
// RuntimeControls parity gaps visible. It does not require byte parity yet;
// each open CPU lane must still materialize at least one measurable seed so
// coverage cannot silently disappear.
func TestVP9OracleRuntimeControlOpenGapSeedsRemainReproducible(t *testing.T) {
	coracletest.SkipWithoutOracle(t, "VP9 runtime-control open-gap seeds")
	coracletest.VpxencVP9FrameFlags(t)

	t.Run("RDKeyframeCPU0Neg3", func(t *testing.T) {
		remeasureVP9RuntimeControlSeedLane(t, func(cpu int8) bool {
			return cpu == 0 || cpu == -3
		})
	})
	t.Run("Speed4Realtime", func(t *testing.T) {
		remeasureVP9RuntimeControlSeedLane(t, func(cpu int8) bool {
			return cpu == 4
		})
	})
}

func remeasureVP9RuntimeControlSeedLane(t *testing.T, includeCPU func(int8) bool) {
	t.Helper()
	pass, fail := 0, 0
	aggSizeDelta := 0
	measured := 0
	for idx, seed := range vp9RuntimeControlsOpenGapSeeds {
		if !vp9RuntimeControlsOpenGapSeed(seed) {
			continue
		}
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("runtimectrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
		if !includeCPU(tc.opts.CpuUsed) {
			continue
		}
		t.Logf("%s w=%d h=%d frames=%d cpu=%d flags=%v",
			label, tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9LibvpxFrameFlags(tc.flags), tc.extraArgs...)
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
					vp9test.FirstPacketDiff(got[i], want[i]),
					seedDelta)
				break
			}
		}
		if firstMis < 0 {
			t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d size_delta=%+d",
				label, len(got), len(want), seedDelta)
		}
	}
	t.Logf("RuntimeControls open-lane remeasure: PASS=%d MISMATCH=%d total=%d agg_size_delta=%+d avg_per_measurable=%+d",
		pass, fail, measured, aggSizeDelta,
		aggSizeDelta/max(1, measured))
	if measured == 0 {
		t.Fatal("no open RuntimeControls seeds matched this lane")
	}
}

func firstVP9MismatchingFrame(got, want [][]byte) int {
	n := min(len(got), len(want))
	for i := 0; i < n; i++ {
		g := sha256.Sum256(got[i])
		w := sha256.Sum256(want[i])
		if g != w {
			return i
		}
	}
	if len(got) != len(want) {
		return n
	}
	return -1
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
