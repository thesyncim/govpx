//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestVP9NonrdPickPartitionRegressionSeedsProgress preserves the historical
// progress dashboard for the now-closed RefControl regression corpus. It
// reports per-seed:
//   - keyframe byte parity (frame 0)
//   - per-frame size delta vs libvpx
//   - count of frames that newly byte-match
//
// Skipped unless GOVPX_WITH_ORACLE=1 and the vpxenc-vp9-frameflags binary is
// available. The strict closure gate lives in
// TestVP9RefControlRegressionSeedsByteParity; this dashboard remains
// non-asserting so it can keep logging the same per-seed measurements when
// experimenting with partition gates.
//
// Baseline data (commit before this test landed):
//
//   - Phase C (no opt-in): keyframe match, 44 inter frames diverge,
//     avg per-seed size_delta ~+3300 bytes.
//   - Phase D (GOVPX_VP9_NONRD_PICK_PARTITION=1): keyframe match, 44 inter
//     frames diverge, avg per-seed size_delta ~+430 bytes (88% reduction).
//   - Phase E (vp9_pickmode.c:2050-2488 control-flow port + merged
//     keyframe-coeff / hybrid-nonrd / var-part-thresh-mult): keyframe
//     parity flips green (10/70 frame byte-match, was 0/70), avg per-seed
//     size_delta +86B/seed (57% further reduction vs Phase D's
//     +200B/seed baseline) after the libvpx-faithful x->skip +
//     bestEarlyTerm + strict-< winner selection + sse_zeromv_normalized
//     landed.
//
// libvpx ref: vp9/encoder/vp9_encodeframe.c:4598-4855 nonrd_pick_partition
// with use_ml_based_partitioning=1.
func TestVP9NonrdPickPartitionRegressionSeedsProgress(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to measure nonrd partition progress")
	}
	requireVP9VpxencFrameFlagsOracle(t)
	t.Log("nonrd pick partition is enabled by the default VP9 speed-feature path")

	type seedResult struct {
		label         string
		matchedFrames int
		totalFrames   int
		firstMismatch int
		sizeDelta     int
	}
	results := make([]seedResult, 0, len(vp9RefControlsRegressionSeeds))
	for _, seed := range vp9RefControlsRegressionSeeds {
		tc := newVP9RefControlsFuzzCase(seed)
		sum := sha256.Sum256(seed)
		label := "regression-" + hex.EncodeToString(sum[:4])

		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources,
			tc.flags, tc.extraArgs)
		n := len(got)
		if len(want) < n {
			n = len(want)
		}
		res := seedResult{label: label, totalFrames: n}
		res.firstMismatch = -1
		var totalSizeDelta int
		for i := 0; i < n; i++ {
			g := sha256.Sum256(got[i])
			w := sha256.Sum256(want[i])
			if g == w {
				res.matchedFrames++
			} else if res.firstMismatch < 0 {
				res.firstMismatch = i
			}
			totalSizeDelta += len(got[i]) - len(want[i])
		}
		res.sizeDelta = totalSizeDelta
		results = append(results, res)
	}
	totalMatch := 0
	totalFrames := 0
	for _, r := range results {
		t.Logf("%s: matched=%d/%d first_mismatch=%d size_delta=%+d bytes",
			r.label, r.matchedFrames, r.totalFrames, r.firstMismatch, r.sizeDelta)
		totalMatch += r.matchedFrames
		totalFrames += r.totalFrames
	}
	t.Logf("phase-D opt-in regression-seeds: %d/%d frames byte-match",
		totalMatch, totalFrames)
}
