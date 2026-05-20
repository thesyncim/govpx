package govpx

import (
	"math"
	"testing"
)

// TestTwoPassFramesToKeyReturnsZeroWhenStatsMissing pins the libvpx
// fallback when stats are not loaded.
func TestTwoPassFramesToKeyReturnsZeroWhenStatsMissing(t *testing.T) {
	var ts twoPassState
	if got := ts.framesToKey(0, 60); got != 0 {
		t.Fatalf("framesToKey with no stats = %d, want 0", got)
	}
}

// TestTwoPassFramesToKeyClampsAtKeyFrameInterval pins the libvpx
// `if (frames_to_key >= keyFrameInterval) break;` clamp: with no
// scene-cut signal in the synthetic stats, framesToKey should not
// exceed the configured interval.
func TestTwoPassFramesToKeyClampsAtKeyFrameInterval(t *testing.T) {
	stats := make([]FirstPassFrameStats, 100)
	for i := range stats {
		// Boring stats that never trigger libvpxTestCandidateKeyFrame.
		stats[i] = FirstPassFrameStats{IntraError: 1000, CodedError: 1000, PcntInter: 0.95}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	got := ts.framesToKey(0, 30)
	if got > 31 {
		t.Fatalf("framesToKey with 30-frame interval = %d, want <= 31", got)
	}
	if got < 30 {
		t.Fatalf("framesToKey with 30-frame interval = %d, want >= 30 (no early KF predicate fires)", got)
	}
}

// TestTwoPassFramesToKeyClampsAtTwoIntervalsForAutoKey pins the libvpx
// `if (frames_to_key >= 2*key_freq) break;` outer clamp by passing
// keyFrameInterval=10 and verifying the result is <= 20.
func TestTwoPassFramesToKeyClampsAtTwoIntervalsForAutoKey(t *testing.T) {
	stats := make([]FirstPassFrameStats, 100)
	for i := range stats {
		stats[i] = FirstPassFrameStats{IntraError: 1000, CodedError: 1000, PcntInter: 0.95}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	if got := ts.framesToKey(0, 10); got > 20 {
		t.Fatalf("framesToKey with 10-frame interval = %d, want <= 20", got)
	}
}

// TestTwoPassAltRefBitChargeDoesNotAdvanceStats pins libvpx Pass2Encode:
// hidden ARF packets subtract from twopass.bits_left, but because
// refresh_alt_ref_frame skips vp8_second_pass and show_frame is false they do
// not consume the visible-frame first-pass stats index.
func TestTwoPassAltRefBitChargeDoesNotAdvanceStats(t *testing.T) {
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
		{IntraError: 1500, CodedError: 200, PcntInter: 0.85},
		{IntraError: 800, CodedError: 50, PcntInter: 0.95},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)

	initialBits := ts.bitsLeft
	initialError := ts.errorLeft
	frame0Error := ts.modifiedError(stats[0])

	ts.chargeAltRefFrameBitsWithProjection(123, 123)
	if ts.frameIndex != 0 {
		t.Fatalf("frameIndex after hidden ARF charge = %d, want 0", ts.frameIndex)
	}
	if ts.errorLeft != initialError {
		t.Fatalf("errorLeft after hidden ARF charge = %v, want unchanged %v", ts.errorLeft, initialError)
	}
	if ts.bitsLeft != initialBits-123 {
		t.Fatalf("bitsLeft after hidden ARF charge = %d, want %d", ts.bitsLeft, initialBits-123)
	}

	ts.finishFrame(77)
	if ts.frameIndex != 1 {
		t.Fatalf("frameIndex after visible frame = %d, want 1", ts.frameIndex)
	}
	if ts.errorLeft != initialError-frame0Error {
		t.Fatalf("errorLeft after visible frame = %v, want %v", ts.errorLeft, initialError-frame0Error)
	}
	wantBitsLeft := initialBits - 123 - 77 + int64(vbrMinFrameBandwidthBits(1000, 50))
	if ts.bitsLeft != wantBitsLeft {
		t.Fatalf("bitsLeft after visible frame = %d, want %d", ts.bitsLeft, wantBitsLeft)
	}
}

// TestTwoPassKFGroupModifiedErrorMatchesSumOfFrames pins libvpx's
// inner accumulator: `kf_group_err += calculate_modified_err(this_frame)`
// across the KF group.
func TestTwoPassKFGroupModifiedErrorMatchesSumOfFrames(t *testing.T) {
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
		{IntraError: 1500, CodedError: 200, PcntInter: 0.85},
		{IntraError: 800, CodedError: 50, PcntInter: 0.95},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	want := twoPassModifiedError(stats[0], 50) + twoPassModifiedError(stats[1], 50) + twoPassModifiedError(stats[2], 50)
	if got := ts.kfGroupModifiedError(0, 3); got != want {
		t.Fatalf("kfGroupModifiedError = %v, want %v", got, want)
	}
}

// TestTwoPassKFGroupBitsAllocatesByErrorRatio pins the libvpx allocation
//
//	kf_group_bits = bits_left * (kf_group_err / modified_error_left)
//
// clamped at max_bits_per_frame * frames_to_key.
func TestTwoPassKFGroupBitsAllocatesByErrorRatio(t *testing.T) {
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
		{IntraError: 1500, CodedError: 200, PcntInter: 0.85},
		{IntraError: 800, CodedError: 50, PcntInter: 0.95},
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	groupErr := ts.kfGroupModifiedError(0, 3)
	want := int64(float64(ts.bitsLeft) * (groupErr / ts.errorLeft))
	if got := ts.kfGroupBits(0, 3, 0); got != want {
		t.Fatalf("kfGroupBits without cap = %d, want %d", got, want)
	}
	// With max_bits_per_frame=100 and frames_to_key=3, the cap is 300.
	if got := ts.kfGroupBits(0, 3, 100); got > 300 {
		t.Fatalf("kfGroupBits with cap=100*3 = %d, want <= 300", got)
	}
}

// buildMultiKFStats builds a 120-frame synthetic FIRSTPASS_STATS
// stream with natural scene cuts at frame 30 and frame 90, so that
// the libvpx find_next_key_frame walk fires the test_candidate_kf
// scene-cut break at both positions. The non-cut frames have high
// PcntInter (>= 0.85) and PcntNeutral=0 so test_candidate_kf's outer
// gate fails for them (PcntInter not < 0.05 AND
// (PcntInter-PcntNeutral) not < 0.25 → the libvpx-negation returns
// false). The cut frames have PcntInter=0.02 to trigger the inner
// boost walk, which sustains via the next ≥ 5 high-intra /
// high-PcntInter post-cut frames.
func buildMultiKFStats() []FirstPassFrameStats {
	stats := make([]FirstPassFrameStats, 120)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:          5000,
			CodedError:          500,
			SSIMWeightedPredErr: 1000,
			PcntInter:           0.95,
			PcntNeutral:         0.0,
			PcntMotion:          0.10,
			PcntSecondRef:       0.0,
		}
	}
	// libvpx find_next_key_frame fires test_candidate_kf when the
	// post-advance "this_frame" at iteration i is the cut frame.
	// Mark frames 30 and 90 as scene cuts with PcntInter < 0.05 so
	// the test_candidate_kf outer disjunction takes the
	// `pcnt_inter < 0.05` branch.
	for _, cut := range []int{30, 90} {
		stats[cut] = FirstPassFrameStats{
			IntraError:          8000,
			CodedError:          400,
			SSIMWeightedPredErr: 4000,
			PcntInter:           0.02,
			PcntNeutral:         0.0,
			PcntMotion:          0.05,
			PcntSecondRef:       0.0,
		}
		// Ensure the next 8 frames sustain IntraError >= 200,
		// PcntInter > 0.85, nextIIRatio > 1.5, boost increment >= 0.5
		// so the inner boost walk runs > 3 iterations with boost_score
		// > 5.0. The default high-quality frames already satisfy
		// this.
		for j := 1; j <= 8 && cut+j < len(stats); j++ {
			stats[cut+j].IntraError = 6000
			stats[cut+j].CodedError = 600
			stats[cut+j].PcntInter = 0.90
		}
	}
	return stats
}

// TestLibvpxFindNextKeyFrameWalkMultiKFSequence pins the libvpx
// find_next_key_frame walk against a 120-frame multi-KF stat stream
// with synthetic scene cuts at frame 30 and 90. The walker must:
//
//  1. From frame 0, return frames_to_key == 30 (KF group spans 0..29).
//  2. From frame 30, return frames_to_key == 60 (KF group 30..89).
//  3. From frame 90, return frames_to_key == 30 (final KF group spans
//     90..119, with the end-of-stats path tail-incrementing).
//
// Before task #192, prepareKFGroup used `len(stats) - frame`, which
// would mis-budget the first group at 120 and the second at 90 —
// under-budgeting the first group and over-budgeting the rest.
func TestLibvpxFindNextKeyFrameWalkMultiKFSequence(t *testing.T) {
	stats := buildMultiKFStats()
	const keyFreq = 9999 // disable the 2x clamp and centering rule
	cases := []struct {
		name  string
		start int
		want  int
	}{
		{"first-group", 0, 30},
		{"second-group", 30, 60},
		{"third-group", 90, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := libvpxFindNextKeyFrameWalk(stats, tc.start, keyFreq, true)
			if got != tc.want {
				t.Fatalf("libvpxFindNextKeyFrameWalk(start=%d) = %d, want %d", tc.start, got, tc.want)
			}
		})
	}
}

// TestTwoPassPrepareKFGroupHonorsNaturalKFOnMultiKFStream pins the
// task #192 fix: prepareKFGroup must seed framesToKeyRemaining and
// kfGroupErrorLeft using the libvpx find_next_key_frame walk, not
// the degenerate `len(stats) - frame` span. On the multi-KF stream
// with scene cuts at frame 30 and 90, the first KF group must span
// 30 frames (not 120), the second 60 frames (not 90), and the third
// 30 frames.
func TestTwoPassPrepareKFGroupHonorsNaturalKFOnMultiKFStream(t *testing.T) {
	stats := buildMultiKFStats()
	const keyFreq = 9999
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 400)
	ts.configureKeyFrameInterval(keyFreq, true)

	expect := []struct {
		frame  uint64
		frames int
	}{
		{0, 30},
		{30, 60},
		{90, 30},
	}
	for _, e := range expect {
		ts.prepareKFGroup(e.frame)
		if !ts.kfGroupValid {
			t.Fatalf("prepareKFGroup(frame=%d): kfGroupValid=false, want true", e.frame)
		}
		if ts.framesToKeyRemaining != e.frames {
			t.Fatalf("prepareKFGroup(frame=%d) framesToKeyRemaining=%d, want %d",
				e.frame, ts.framesToKeyRemaining, e.frames)
		}
		// kf_group_error_left should equal sum(modified_err over
		// frame+1..frame+frames-1) — the libvpx
		// `kf_group_err - kf_mod_err` post-loop assignment.
		wantErr := 0.0
		for i := e.frame + 1; i < e.frame+uint64(e.frames) && i < uint64(len(stats)); i++ {
			wantErr += ts.modifiedError(stats[i])
		}
		if math.Abs(ts.kfGroupErrorLeft-wantErr) > 1e-9 {
			t.Fatalf("prepareKFGroup(frame=%d) kfGroupErrorLeft=%v, want %v",
				e.frame, ts.kfGroupErrorLeft, wantErr)
		}
	}
}

// TestLibvpxFindNextKeyFrameWalkRespectsAutoKeyDisabled pins libvpx's
// `cpi->oxcf.auto_key == 0` branch: with auto_key disabled, the
// scene-cut and transition-to-still gates are skipped and the walk
// just walks to end-of-stats (or to the 2x clamp when key_freq > 0).
// On the multi-KF stream, the walker should ignore the cuts at
// frame 30 and 90 and return the full length when key_freq is large
// enough.
func TestLibvpxFindNextKeyFrameWalkRespectsAutoKeyDisabled(t *testing.T) {
	stats := buildMultiKFStats()
	got := libvpxFindNextKeyFrameWalk(stats, 0, 9999, false)
	if got != len(stats) {
		t.Fatalf("libvpxFindNextKeyFrameWalk(autoKey=false) = %d, want %d", got, len(stats))
	}
}

// TestLibvpxFindNextKeyFrameWalkAppliesCenteringRule pins the libvpx
// post-loop centering at firstpass.c lines 2603-2608: when auto_key
// is set and the natural walk exceeds key_frame_frequency (but not
// 2x), libvpx halves frames_to_key. With a 50-frame stat stream of
// boring content (no scene cuts) and key_freq=20, the walk would
// otherwise hit the 2*20=40 cap; the centering rule then divides by
// 2 to land at 20.
func TestLibvpxFindNextKeyFrameWalkAppliesCenteringRule(t *testing.T) {
	stats := make([]FirstPassFrameStats, 50)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 1000, CodedError: 1000, PcntInter: 0.95,
		}
	}
	got := libvpxFindNextKeyFrameWalk(stats, 0, 20, true)
	if got != 20 {
		t.Fatalf("libvpxFindNextKeyFrameWalk after centering = %d, want 20", got)
	}
}
