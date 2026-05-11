package govpx

import (
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

	ts.chargeAltRefFrameBits(123)
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
