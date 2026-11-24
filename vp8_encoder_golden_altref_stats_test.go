package govpx

import (
	"testing"
)

func TestUpdateGoldenFrameStatsMirrorsLibvpxCounter(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{AutoAltRef: true}}

	// Plain inter frame increments frames_since_golden.
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 1 || e.sourceAltRefActive {
		t.Fatalf("plain inter -> {framesSinceGolden:%d sourceAltRefActive:%v}, want {1 false}",
			e.framesSinceGolden, e.sourceAltRefActive)
	}
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 2 {
		t.Fatalf("two plain inters frames_since_golden = %d, want 2", e.framesSinceGolden)
	}

	// Refresh alt-ref: counter resets, alt-ref becomes active.
	e.updateGoldenFrameStats(false, true)
	if e.framesSinceGolden != 0 || !e.sourceAltRefActive {
		t.Fatalf("alt-ref refresh -> {%d %v}, want {0 true}", e.framesSinceGolden, e.sourceAltRefActive)
	}

	// Plain inter after alt-ref keeps alt-ref active and increments counter.
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 1 || !e.sourceAltRefActive {
		t.Fatalf("post-altref inter -> {%d %v}, want {1 true}", e.framesSinceGolden, e.sourceAltRefActive)
	}

	// Refresh golden: counter resets, alt-ref active clears (no auto-arf
	// pending in govpx).
	e.updateGoldenFrameStats(true, false)
	if e.framesSinceGolden != 0 || e.sourceAltRefActive {
		t.Fatalf("golden refresh -> {%d %v}, want {0 false}", e.framesSinceGolden, e.sourceAltRefActive)
	}
}

// TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch pins
// `resetGoldenFrameStats` to the libvpx
// `update_golden_frame_stats(refresh_golden_frame=1)` keyframe branch in
// vp8/encoder/onyx_if.c. Two regimes are exercised:
//
//  1. No ARF schedule armed: source_alt_ref_active is zeroed (libvpx
//     `if (!cpi->source_alt_ref_pending) cpi->source_alt_ref_active = 0`),
//     frames_since_golden is reset, and frames_till_gf_update_due is
//     decremented (libvpx `if (frames_till_gf_update_due > 0)
//     frames_till_gf_update_due--`).
//
//  2. ARF schedule armed during the keyframe's vp8_second_pass call:
//     source_alt_ref_pending and alt_ref_source are preserved so that the
//     next vp8_get_compressed_data ARF block can fire; only
//     frames_till_alt_ref_frame decrements per the libvpx update.

func TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch(t *testing.T) {
	t.Run("no-arf-schedule", func(t *testing.T) {
		e := &VP8Encoder{
			framesSinceGolden:     7,
			sourceAltRefActive:    true,
			sourceAltRefPending:   false,
			altRefSourceValid:     false,
			framesTillAltRefFrame: 5,
		}
		e.resetGoldenFrameStats()
		if e.framesSinceGolden != 0 || e.sourceAltRefActive ||
			e.framesTillAltRefFrame != 4 {
			t.Fatalf("post-keyframe state = {fsg:%d active:%v till:%d}, want {0 false 4}",
				e.framesSinceGolden, e.sourceAltRefActive, e.framesTillAltRefFrame)
		}
	})
	t.Run("preserves-arf-schedule", func(t *testing.T) {
		e := &VP8Encoder{
			framesSinceGolden:     3,
			sourceAltRefActive:    true,
			sourceAltRefPending:   true,
			altRefSourceValid:     true,
			altRefSourcePTS:       1234,
			framesTillAltRefFrame: 7,
		}
		e.resetGoldenFrameStats()
		if e.framesSinceGolden != 0 {
			t.Fatalf("framesSinceGolden = %d, want 0", e.framesSinceGolden)
		}
		if !e.sourceAltRefActive {
			t.Fatalf("sourceAltRefActive cleared while pending=true; libvpx only zeroes active when !pending")
		}
		if !e.sourceAltRefPending || !e.altRefSourceValid || e.altRefSourcePTS != 1234 {
			t.Fatalf("ARF schedule mutated: pending=%v valid=%v pts=%d, want true/true/1234",
				e.sourceAltRefPending, e.altRefSourceValid, e.altRefSourcePTS)
		}
		if e.framesTillAltRefFrame != 6 {
			t.Fatalf("framesTillAltRefFrame = %d, want 6 (decremented from 7)", e.framesTillAltRefFrame)
		}
	})
}

// TestClearAltRefScheduleDropsPendingState pins the lifecycle reset path used
// from Reset()/encoder init: dropping any in-flight ARF schedule entirely so
// that no leftover pending state survives into a fresh stream.

func TestClearAltRefScheduleDropsPendingState(t *testing.T) {
	e := &VP8Encoder{
		sourceAltRefPending:   true,
		altRefSourceValid:     true,
		altRefSourcePTS:       42,
		framesTillAltRefFrame: 5,
	}
	e.clearAltRefSchedule()
	if e.sourceAltRefPending || e.altRefSourceValid || e.framesTillAltRefFrame != 0 {
		t.Fatalf("post-clear state = {pending:%v valid:%v till:%d}, want {false false 0}",
			e.sourceAltRefPending, e.altRefSourceValid, e.framesTillAltRefFrame)
	}
}

// TestScheduleAltRefSourceArmsPendingFlagAndPTS pins the libvpx
// `cpi->source_alt_ref_pending = 1; cpi->alt_ref_source = source` set
// inside vp8_get_compressed_data: scheduling the ARF arms the pending
// flag, records the PTS, and primes frames_till_alt_ref_frame.

func TestScheduleAltRefSourceArmsPendingFlagAndPTS(t *testing.T) {
	var e VP8Encoder
	e.scheduleAltRefSource(1234, 7)
	if !e.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending after schedule = false, want true")
	}
	if !e.altRefSourceValid || e.altRefSourcePTS != 1234 {
		t.Fatalf("altRefSourcePTS = %d valid=%v, want 1234 valid=true",
			e.altRefSourcePTS, e.altRefSourceValid)
	}
	if e.framesTillAltRefFrame != 7 {
		t.Fatalf("framesTillAltRefFrame = %d, want 7", e.framesTillAltRefFrame)
	}
}

// TestIsSrcFrameAltRefMatchesScheduledPTS pins the libvpx
// is_src_frame_alt_ref = (alt_ref_source != NULL && source ==
// alt_ref_source) check.

func TestIsSrcFrameAltRefMatchesScheduledPTS(t *testing.T) {
	var e VP8Encoder
	if e.isSrcFrameAltRef(1234) {
		t.Fatalf("unscheduled frame should not match")
	}
	e.scheduleAltRefSource(1234, 7)
	if !e.isSrcFrameAltRef(1234) {
		t.Fatalf("scheduled PTS should match")
	}
	if e.isSrcFrameAltRef(9999) {
		t.Fatalf("non-matching PTS should not be ARF source")
	}
}

// TestUpdateGoldenFrameStatsCountsDownAltRefFrame pins the libvpx
// `if (cpi->frames_till_alt_ref_frame) cpi->frames_till_alt_ref_frame--`
// counter.

func TestUpdateGoldenFrameStatsCountsDownAltRefFrame(t *testing.T) {
	var e VP8Encoder
	e.scheduleAltRefSource(1234, 3)
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 2 {
		t.Fatalf("frames_till_alt_ref_frame after first inter = %d, want 2", e.framesTillAltRefFrame)
	}
	e.updateGoldenFrameStats(false, false)
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 0 {
		t.Fatalf("frames_till_alt_ref_frame after 3 inters = %d, want 0", e.framesTillAltRefFrame)
	}
	// Counter should not go negative.
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 0 {
		t.Fatalf("frames_till_alt_ref_frame after underflow = %d, want 0 floor", e.framesTillAltRefFrame)
	}
}

// TestUpdateGoldenFrameStatsAltRefRefreshClearsPending pins the libvpx
// update_alt_ref_frame_stats branch: a successful ARF refresh consumes
// the pending flag. The branch is gated on AutoAltRef
// (libvpx oxcf.play_alternate) — see TestUpdateGoldenFrameStatsMirrorsLibvpxCounter
// for the dispatcher reference.

func TestUpdateGoldenFrameStatsAltRefRefreshClearsPending(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{AutoAltRef: true}, sourceAltRefPending: true, framesTillAltRefFrame: 2}
	e.updateGoldenFrameStats(false, true)
	if e.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending after ARF refresh = true, want false (consumed)")
	}
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive after ARF refresh = false, want true")
	}
}

// TestUpdateGoldenFrameStatsDirectAltRefRefreshSkipsPlainInterCounters pins the
// libvpx update_golden_frame_stats path when refresh_alt_ref_frame=1 but
// play_alternate is disabled: the function does not enter the plain inter
// `else if (!refresh_alt_ref_frame)` branch, so frames_since_golden and
// frames_till_alt_ref_frame are left untouched.

func TestUpdateGoldenFrameStatsDirectAltRefRefreshSkipsPlainInterCounters(t *testing.T) {
	e := &VP8Encoder{framesSinceGolden: 7, framesTillAltRefFrame: 3}
	e.updateGoldenFrameStats(false, true)
	if e.framesSinceGolden != 7 || e.framesTillAltRefFrame != 3 || e.sourceAltRefActive {
		t.Fatalf("direct ALTREF refresh state = {since:%d till:%d active:%v}, want {7 3 false}",
			e.framesSinceGolden, e.framesTillAltRefFrame, e.sourceAltRefActive)
	}
}

func TestSnapshotDroppedFrameCoefProbsMirrorsRefreshMask(t *testing.T) {
	var e VP8Encoder
	e.coefProbsSnapshotsValid = true
	e.coefProbs[0][0][0][0] = 77
	e.coefProbsLast[0][0][0][0] = 11
	e.coefProbsGolden[0][0][0][0] = 22
	e.coefProbsAltRef[0][0][0][0] = 33

	e.snapshotDroppedFrameCoefProbs(true, false, false)
	if got := e.coefProbsLast[0][0][0][0]; got != 77 {
		t.Fatalf("LAST snapshot = %d, want 77", got)
	}
	if got := e.coefProbsGolden[0][0][0][0]; got != 22 {
		t.Fatalf("GOLDEN snapshot changed to %d, want 22", got)
	}
	if got := e.coefProbsAltRef[0][0][0][0]; got != 33 {
		t.Fatalf("ALTREF snapshot changed to %d, want 33", got)
	}
}

// TestUpdateGoldenFrameStatsGoldenRefreshKeepsActiveOnPending pins the
// libvpx `if (!source_alt_ref_pending) source_alt_ref_active = 0`
// branch: when an ARF is still pending, refreshing GOLDEN does not
// clear the active flag.

func TestUpdateGoldenFrameStatsGoldenRefreshKeepsActiveOnPending(t *testing.T) {
	e := &VP8Encoder{sourceAltRefActive: true, sourceAltRefPending: true}
	e.updateGoldenFrameStats(true, false)
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive after GF refresh with ARF pending = false, want true (gated)")
	}
}
