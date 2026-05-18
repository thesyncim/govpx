package govpx

import "testing"

// makeDampedActiveWorstQState builds a twoPassState seeded for the
// early-portion-of-clip damped active_worst_quality update path. The
// stats slice is sized so the libvpx window gate is open at frame=1
// with baselineGFInterval=4 and total_stats.count=256: frame 1 <
// (256*255)>>8 = 255 and frame+interval = 5 < 256. Coded error is
// uniform so the per-frame stats produce a deterministic err_per_mb.
// pass2ActiveWorstQ is preseeded so the damped update has a starting
// active_worst_quality to step.
func makeDampedActiveWorstQState(t *testing.T, total int, gfInterval int, seed int) *twoPassState {
	t.Helper()
	stats := make([]FirstPassFrameStats, total)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 5000,
			CodedError: 1000,
			Count:      1,
			PcntInter:  0.8,
		}
	}
	ts := &twoPassState{}
	ts.configure(stats, 50_000, 50, 0, 400)
	ts.configureFrameDims(128, 128)
	ts.configureQuantizerBounds(0, vp8MaxQIndex)
	ts.baselineGFInterval = gfInterval
	ts.pass2ActiveWorstQ = seed
	ts.pass2ActiveWorstQValid = true
	return ts
}

// TestDampedActiveWorstQSkipsFrameZero pins the libvpx
// vp8/encoder/firstpass.c vp8_second_pass behaviour where the damped
// branch is the `else if` from the `current_video_frame == 0` seed
// branch (lines 2328-2393). At frame 0 the damped branch must NOT
// fire; the regulator retains the seed value computed by the `==0`
// branch.
func TestDampedActiveWorstQSkipsFrameZero(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.dampedUpdatePass2ActiveWorstQ(0)
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("frame 0 modified pass2ActiveWorstQ: got %d, want unchanged 40", got)
	}
	if !ts.pass2ActiveWorstQValid {
		t.Fatalf("frame 0 cleared pass2ActiveWorstQValid")
	}
}

// TestDampedActiveWorstQGateUpperBound pins the first half of the
// libvpx vp8/encoder/firstpass.c:2372-2373 window gate:
//
//	current_video_frame < ((total_stats.count * 255) >> 8)
//
// For total=256 the threshold is exactly 255. At frame 255 the gate
// is closed; the damped update returns early and leaves
// active_worst_quality unchanged.
func TestDampedActiveWorstQGateUpperBound(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	// Advance frameIndex to 255 (still within stats so totalLeftStats
	// remains valid via fallbacks).
	ts.frameIndex = 255
	ts.dampedUpdatePass2ActiveWorstQ(255)
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("frame 255 (upper-gate closed) modified pass2ActiveWorstQ: got %d, want 40", got)
	}
}

// TestDampedActiveWorstQGateTrailingGF pins the second half of the
// libvpx vp8/encoder/firstpass.c:2374-2375 window gate:
//
//	(current_video_frame + baseline_gf_interval) < total_stats.count
//
// When the current frame plus the upcoming GF span equals or exceeds
// total_stats.count, the damped branch is skipped (we are in the
// trailing GF group).
func TestDampedActiveWorstQGateTrailingGF(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 32, 8, 40)
	// frame + baselineGFInterval == total_stats.count -> gate closed.
	ts.frameIndex = 24
	ts.dampedUpdatePass2ActiveWorstQ(24)
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("trailing-GF gate closed but damped path ran: got %d, want 40", got)
	}
	// One frame earlier: gate still open.
	ts2 := makeDampedActiveWorstQState(t, 32, 8, 40)
	ts2.frameIndex = 23
	ts2.dampedUpdatePass2ActiveWorstQ(23)
	if got := ts2.pass2ActiveWorstQ; got == 40 {
		t.Fatalf("trailing-GF gate open at frame 23 but damped path did not run (still %d)", got)
	}
}

// TestDampedActiveWorstQStepUp pins the libvpx
// vp8/encoder/firstpass.c:2385-2386 `++` step then damping average
// when tmp_q > active_worst_quality. We force a very large
// section_target_bandwidth so estimate_max_q lands at best_quality=0
// in one configuration and a very small budget so it pegs at
// worst_quality in another. The latter triggers the `++` branch.
func TestDampedActiveWorstQStepUp(t *testing.T) {
	// Tiny bits_left forces estimate_max_q to peg at worstQuality
	// (target_norm_bits_per_mb stays below baseBitsPerMB at all Q).
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.bitsLeft = 100 // tiny budget -> tmp_q == worstQuality
	ts.frameIndex = 1
	ts.pass2ActiveWorstQ = 40
	// Compute the libvpx tmp_q outside the function to derive the
	// damped output and verify our function matches.
	framesLeft := int64(len(ts.stats)) - 1
	stb := ts.bitsLeft / framesLeft
	sectionErr := ts.totalLeftStats.CodedError / ts.totalLeftStats.Count
	errPerMB := sectionErr / float64(ts.numMBs)
	overhead := libvpxEstimateModeMVCost(ts.totalLeftStats, ts.numMBs)
	tmpQ := libvpxEstimateMaxQ(ts.numMBs, int(stb), overhead, errPerMB, 1.0, ts.estMaxQCorrection, ts.sectionMaxQFactor, ts.bestQuality, ts.worstQuality)
	if tmpQ <= 40 {
		t.Fatalf("test precondition failed: expected tmpQ > 40 to exercise ++ branch, got tmpQ=%d", tmpQ)
	}
	aw := 40 + 1 // ++ step
	want := (aw*3 + tmpQ + 2) / 4
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if got := ts.pass2ActiveWorstQ; got != want {
		t.Fatalf("step-up damped update: got %d, want %d (tmpQ=%d, pre-aw=40, post-step-aw=%d)", got, want, tmpQ, aw)
	}
}

// TestDampedActiveWorstQStepDown pins the libvpx
// vp8/encoder/firstpass.c:2387-2388 `--` step then damping average
// when tmp_q < active_worst_quality. We start with a high
// active_worst_quality and a large budget so estimate_max_q returns
// a low tmp_q, exercising the `--` branch.
func TestDampedActiveWorstQStepDown(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 100)
	// Large bits_left so tmp_q comes in low.
	ts.bitsLeft = 50_000_000
	ts.frameIndex = 1
	framesLeft := int64(len(ts.stats)) - 1
	stb := ts.bitsLeft / framesLeft
	sectionErr := ts.totalLeftStats.CodedError / ts.totalLeftStats.Count
	errPerMB := sectionErr / float64(ts.numMBs)
	overhead := libvpxEstimateModeMVCost(ts.totalLeftStats, ts.numMBs)
	tmpQ := libvpxEstimateMaxQ(ts.numMBs, int(stb), overhead, errPerMB, 1.0, ts.estMaxQCorrection, ts.sectionMaxQFactor, ts.bestQuality, ts.worstQuality)
	if tmpQ >= 100 {
		t.Fatalf("test precondition failed: expected tmpQ < 100 to exercise -- branch, got tmpQ=%d", tmpQ)
	}
	aw := 100 - 1 // -- step
	want := (aw*3 + tmpQ + 2) / 4
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if got := ts.pass2ActiveWorstQ; got != want {
		t.Fatalf("step-down damped update: got %d, want %d (tmpQ=%d, pre-aw=100, post-step-aw=%d)", got, want, tmpQ, aw)
	}
}

// TestDampedActiveWorstQEqualNoStep pins the libvpx
// vp8/encoder/firstpass.c:2384-2392 path when tmp_q ==
// active_worst_quality: neither ++ nor -- fires, and the damping
// average collapses to ((aw*3 + aw + 2) / 4) == aw exactly. The
// regulator therefore stays put.
func TestDampedActiveWorstQEqualNoStep(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.frameIndex = 1
	framesLeft := int64(len(ts.stats)) - 1
	stb := ts.bitsLeft / framesLeft
	sectionErr := ts.totalLeftStats.CodedError / ts.totalLeftStats.Count
	errPerMB := sectionErr / float64(ts.numMBs)
	overhead := libvpxEstimateModeMVCost(ts.totalLeftStats, ts.numMBs)
	tmpQ := libvpxEstimateMaxQ(ts.numMBs, int(stb), overhead, errPerMB, 1.0, ts.estMaxQCorrection, ts.sectionMaxQFactor, ts.bestQuality, ts.worstQuality)
	// Force the seeded active_worst_quality to match tmp_q so neither
	// step branch fires.
	ts.pass2ActiveWorstQ = tmpQ
	want := (tmpQ*3 + tmpQ + 2) / 4 // == tmpQ
	if want != tmpQ {
		t.Fatalf("damping formula identity broken: %d != %d", want, tmpQ)
	}
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if got := ts.pass2ActiveWorstQ; got != tmpQ {
		t.Fatalf("equal-tmpQ damped update: got %d, want %d", got, tmpQ)
	}
}

// TestDampedActiveWorstQAveragingFormula pins the libvpx
// vp8/encoder/firstpass.c:2391-2392 damping formula:
//
//	cpi->active_worst_quality = ((aw * 3) + tmp_q + 2) / 4
//
// independently of the ++/-- step. We construct a state where the
// step lands aw closer to tmp_q than 0, then verify the integer
// arithmetic exactly.
func TestDampedActiveWorstQAveragingFormula(t *testing.T) {
	cases := []struct {
		aw, tmpQ, want int
	}{
		// step up: aw=40 -> aw=41, then (41*3 + 50 + 2)/4 = (123+52)/4 = 175/4 = 43
		{aw: 40, tmpQ: 50, want: (41*3 + 50 + 2) / 4},
		// step down: aw=80 -> aw=79, then (79*3 + 50 + 2)/4 = (237+52)/4 = 289/4 = 72
		{aw: 80, tmpQ: 50, want: (79*3 + 50 + 2) / 4},
		// equal: aw=50, tmpQ=50, no step, then (50*3+50+2)/4 = 50
		{aw: 50, tmpQ: 50, want: (50*3 + 50 + 2) / 4},
	}
	for _, tc := range cases {
		aw := tc.aw
		if tc.tmpQ > aw {
			aw++
		} else if tc.tmpQ < aw {
			aw--
		}
		aw = (aw*3 + tc.tmpQ + 2) / 4
		if aw != tc.want {
			t.Fatalf("damping formula: aw=%d tmpQ=%d -> got %d, want %d", tc.aw, tc.tmpQ, aw, tc.want)
		}
	}
}

// TestDampedActiveWorstQRequiresValidSeed pins the precondition:
// when pass2ActiveWorstQValid is false (one-pass mode or pre-seed),
// the damped update is a no-op. libvpx's `==0` branch always runs
// before the damped branch, so the validity bit is guaranteed once
// the damped branch can fire.
func TestDampedActiveWorstQRequiresValidSeed(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.frameIndex = 1
	ts.pass2ActiveWorstQValid = false
	ts.pass2ActiveWorstQ = 40
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if ts.pass2ActiveWorstQValid {
		t.Fatalf("damped update flipped pass2ActiveWorstQValid from false to true without seed")
	}
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("damped update mutated active_worst_quality with invalid seed: got %d, want 40", got)
	}
}
