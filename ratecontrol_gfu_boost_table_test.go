package govpx

import "testing"

// TestActiveBestKFGfuBoostTableSelect pins the libvpx KF active-best-quality
// table selection at vp8/encoder/onyx_if.c:3624-3630:
//
//	if (cpi->pass == 2) {
//	  if (cpi->gfu_boost > 600) {
//	    cpi->active_best_quality = kf_low_motion_minq[Q];
//	  } else {
//	    cpi->active_best_quality = kf_high_motion_minq[Q];
//	  }
//	  ...
//	}
//
// govpx's libvpxActiveQuantizerBoundsForFrame must consult
// rateControlState.gfuBoost (plumbed from twoPassState.gfuBoostValue via
// encoder_frame.go) to select between libvpxKeyFrameLowMotionMinQ and
// libvpxKeyFrameHighMotionMinQ at the exact `> 600` threshold. The
// transition at the boundary (boost == 600 falls to high; boost == 601
// rises to low) must mirror libvpx's strict `>` comparison.
func TestActiveBestKFGfuBoostTableSelect(t *testing.T) {
	const q = 80
	// Sanity-pin the two tables differ at Q=80 so the transition is
	// observable: kf_low_motion_minq[80]=6, kf_high_motion_minq[80]=11.
	if libvpxKeyFrameLowMotionMinQ[q] == libvpxKeyFrameHighMotionMinQ[q] {
		t.Fatalf("test fixture: kf_low[%d]=%d == kf_high[%d]=%d (no observable transition)",
			q, libvpxKeyFrameLowMotionMinQ[q], q, libvpxKeyFrameHighMotionMinQ[q])
	}

	newRC := func(boost int, valid bool) rateControlState {
		return rateControlState{
			mode:                      RateControlVBR,
			minQuantizer:              0,
			maxQuantizer:              q,
			pass2ActiveWorstQOverride: q,
			pass2ActiveWorstQValid:    true,
			gfuBoost:                  boost,
			gfuBoostValid:             valid,
			thisKeyFrameForced:        false,
		}
	}

	// boost == 600 must take the high-motion branch (strict `>`).
	{
		rc := newRC(600, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
		if want := libvpxKeyFrameHighMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=600 KF active-best = %d, want kf_high_motion_minq[%d]=%d (libvpx >600 strict)", best, q, want)
		}
	}
	// boost == 601 must take the low-motion branch.
	{
		rc := newRC(601, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
		if want := libvpxKeyFrameLowMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=601 KF active-best = %d, want kf_low_motion_minq[%d]=%d", best, q, want)
		}
	}
	// A large boost stays on the low-motion branch.
	{
		rc := newRC(2000, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
		if want := libvpxKeyFrameLowMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=2000 KF active-best = %d, want kf_low_motion_minq[%d]=%d", best, q, want)
		}
	}
	// gfuBoostValid=false must fall back to high-motion regardless of
	// the stored boost value (matches libvpx one-pass / pre-define_gf_group
	// behaviour where the conservative table is always selected).
	{
		rc := newRC(5000, false)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
		if want := libvpxKeyFrameHighMotionMinQ[q]; best != want {
			t.Fatalf("gfuBoostValid=false KF active-best = %d, want kf_high_motion_minq[%d]=%d (one-pass fallback)", best, q, want)
		}
	}
	// One-pass fallthrough (pass2 false) on the ni_frames>150 branch
	// must use kf_high_motion_minq even when gfuBoost > 600 — this
	// mirrors libvpx onyx_if.c:3646 (the `pass != 2` arm
	// unconditionally selects kf_high_motion_minq).
	{
		rc := newRC(2000, true)
		rc.pass2ActiveWorstQValid = false
		rc.normalInterFrames = 151
		rc.normalInterAvgQuantizer = q
		rc.bufferOptimalBits = 0 // disable CBR full-buffer adjust
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
		if want := libvpxKeyFrameHighMotionMinQ[q]; best != want {
			t.Fatalf("one-pass ni>150 KF active-best = %d, want kf_high_motion_minq[%d]=%d (libvpx :3646 arm)", best, q, want)
		}
	}
}

// TestActiveBestGFGfuBoostTableSelect pins the libvpx GF/ARF active-best
// table selection at vp8/encoder/onyx_if.c:3667-3674:
//
//	if (cpi->pass == 2) {
//	  if (cpi->gfu_boost > 1000) {
//	    cpi->active_best_quality = gf_low_motion_minq[Q];
//	  } else if (cpi->gfu_boost < 400) {
//	    cpi->active_best_quality = gf_high_motion_minq[Q];
//	  } else {
//	    cpi->active_best_quality = gf_mid_motion_minq[Q];
//	  }
//	  ...
//	}
//
// govpx must mirror the >1000 / <400 strict comparisons exactly: at the
// boundaries (1000 -> mid, 1001 -> low; 400 -> mid, 399 -> high) the
// table selection must flip in lockstep with libvpx.
func TestActiveBestGFGfuBoostTableSelect(t *testing.T) {
	const q = 80
	// Sanity-pin the three GF tables differ at Q=80 so each transition is
	// observable: gf_low[80]=27, gf_mid[80]=30, gf_high[80]=33.
	if libvpxGoldenFrameLowMotionMinQ[q] == libvpxGoldenFrameMidMotionMinQ[q] ||
		libvpxGoldenFrameMidMotionMinQ[q] == libvpxGoldenFrameHighMotionMinQ[q] {
		t.Fatalf("test fixture: GF tables collapse at Q=%d (low=%d mid=%d high=%d)",
			q,
			libvpxGoldenFrameLowMotionMinQ[q],
			libvpxGoldenFrameMidMotionMinQ[q],
			libvpxGoldenFrameHighMotionMinQ[q])
	}

	newRC := func(boost int, valid bool) rateControlState {
		return rateControlState{
			mode:                      RateControlVBR,
			minQuantizer:              0,
			maxQuantizer:              q,
			pass2ActiveWorstQOverride: q,
			pass2ActiveWorstQValid:    true,
			gfuBoost:                  boost,
			gfuBoostValid:             valid,
			currentTemporalLayers:     1,
			framesSinceKeyframe:       30,
			avgFrameQuantizer:         q,
		}
	}

	// boost == 1000 must take the mid-motion branch (strict `>1000`).
	{
		rc := newRC(1000, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameMidMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=1000 GF active-best = %d, want gf_mid_motion_minq[%d]=%d (libvpx >1000 strict)", best, q, want)
		}
	}
	// boost == 1001 must take the low-motion branch.
	{
		rc := newRC(1001, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameLowMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=1001 GF active-best = %d, want gf_low_motion_minq[%d]=%d", best, q, want)
		}
	}
	// boost == 400 must take the mid-motion branch (strict `<400`).
	{
		rc := newRC(400, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameMidMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=400 GF active-best = %d, want gf_mid_motion_minq[%d]=%d (libvpx <400 strict)", best, q, want)
		}
	}
	// boost == 399 must take the high-motion branch.
	{
		rc := newRC(399, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameHighMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=399 GF active-best = %d, want gf_high_motion_minq[%d]=%d", best, q, want)
		}
	}
	// boost == 700 (middle band) must take the mid-motion branch.
	{
		rc := newRC(700, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameMidMotionMinQ[q]; best != want {
			t.Fatalf("gfu_boost=700 GF active-best = %d, want gf_mid_motion_minq[%d]=%d", best, q, want)
		}
	}
	// gfuBoostValid=false must fall back to high-motion regardless of
	// the stored boost value.
	{
		rc := newRC(5000, false)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameHighMotionMinQ[q]; best != want {
			t.Fatalf("gfuBoostValid=false GF active-best = %d, want gf_high_motion_minq[%d]=%d (one-pass fallback)", best, q, want)
		}
	}
	// ARF refresh (altRefFrame=true, goldenFrame=false) shares the same
	// branch as GF refresh and must honor the same gfu_boost thresholds.
	{
		rc := newRC(1500, true)
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, false, true)
		if want := libvpxGoldenFrameLowMotionMinQ[q]; best != want {
			t.Fatalf("ARF gfu_boost=1500 active-best = %d, want gf_low_motion_minq[%d]=%d", best, q, want)
		}
	}
	// One-pass fallthrough on the ni_frames>150 branch must use
	// gf_high_motion_minq even when gfuBoost > 1000 (libvpx
	// onyx_if.c:3683 unconditionally selects gf_high_motion_minq for
	// the pass != 2 arm).
	{
		rc := newRC(2000, true)
		rc.pass2ActiveWorstQValid = false
		rc.normalInterFrames = 151
		rc.normalInterAvgQuantizer = q
		rc.bufferOptimalBits = 0
		best, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
		if want := libvpxGoldenFrameHighMotionMinQ[q]; best != want {
			t.Fatalf("one-pass ni>150 GF active-best = %d, want gf_high_motion_minq[%d]=%d (libvpx :3683 arm)", best, q, want)
		}
	}
}

// TestTwoPassGfuBoostPlumbedFromDefineGFGroup verifies that
// twoPassState.defineGFGroup publishes the libvpx-finalized
// `cpi->gfu_boost` value (after the alt_boost reassignment at
// firstpass.c:1785) onto twoPassState.gfuBoost so the encoder driver
// can plumb it through to rateControlState. The accessor
// `gfuBoostValue()` must report `valid=false` before any GF group is
// defined (matching libvpx's calloc-zero default) and `valid=true`
// with the finalized integer after the first defineGFGroup call.
func TestTwoPassGfuBoostPlumbedFromDefineGFGroup(t *testing.T) {
	var ts twoPassState
	if _, ok := ts.gfuBoostValue(); ok {
		t.Fatalf("freshly-zeroed twoPassState reports gfuBoostValue valid=true, want false (libvpx calloc default)")
	}

	// Seed a minimal valid pass-2 state: configure() expects normalized
	// FirstPassFrameStats, so reach into the fields directly.
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 500, Count: 1, PcntInter: 0.7, PcntMotion: 0.3, MVrAbs: 1, MVcAbs: 1, Duration: 1},
		{IntraError: 1000, CodedError: 500, Count: 1, PcntInter: 0.7, PcntMotion: 0.3, MVrAbs: 1, MVcAbs: 1, Duration: 1},
		{IntraError: 1000, CodedError: 500, Count: 1, PcntInter: 0.7, PcntMotion: 0.3, MVrAbs: 1, MVcAbs: 1, Duration: 1},
		{IntraError: 1000, CodedError: 500, Count: 1, PcntInter: 0.7, PcntMotion: 0.3, MVrAbs: 1, MVcAbs: 1, Duration: 1},
		{IntraError: 1000, CodedError: 500, Count: 1, PcntInter: 0.7, PcntMotion: 0.3, MVrAbs: 1, MVcAbs: 1, Duration: 1},
	}
	ts.configure(stats, 1_000_000, 50, 0, 400)
	// Seed kfGroup state so defineGFGroup will run its allocator path.
	ts.framesToKeyRemaining = len(ts.stats)
	ts.kfGroupValid = true
	ts.kfGroupBitsRemaining = 4_000_000
	ts.kfGroupErrorLeft = 1_000_000
	ts.bitsLeft = 4_000_000
	ts.lastKeySeen = 0
	ts.maxGFInterval = 16
	ts.staticSceneMaxGFInterval = 16

	ts.defineGFGroup(0, 0, false)
	got, ok := ts.gfuBoostValue()
	if !ok {
		t.Fatalf("after defineGFGroup gfuBoostValue valid=false, want true")
	}
	if got <= 0 {
		t.Fatalf("after defineGFGroup gfuBoostValue = %d, want > 0 (libvpx boost_score-derived)", got)
	}

	// Re-run defineGFGroup with useAltRef=true on a GF interval > 1: the
	// alt_boost reassignment at firstpass.c:1785 (`cpi->gfu_boost =
	// alt_boost`) must replace the gfu_boost we report. Pin that the
	// published value equals lastAltBoost when useAltRef && altBoost > 0.
	ts.framesToKeyRemaining = len(ts.stats)
	ts.kfGroupValid = true
	ts.kfGroupBitsRemaining = 4_000_000
	ts.kfGroupErrorLeft = 1_000_000
	ts.defineGFGroup(0, 0, true)
	if ts.lastAltBoost > 0 {
		gotAlt, okAlt := ts.gfuBoostValue()
		if !okAlt || gotAlt != ts.lastAltBoost {
			t.Fatalf("after defineGFGroup(useAltRef=true) gfuBoostValue = %d (valid=%v), want lastAltBoost %d (libvpx firstpass.c:1785 cpi->gfu_boost = alt_boost)",
				gotAlt, okAlt, ts.lastAltBoost)
		}
	}
}
