package govpx

import "testing"

// TestNiMaxQLimitClampBelow150IsNoOp covers the libvpx firstpass.c
// lines 994-1006 lower gate: when ni_frames <= 150 the clamp must not
// fire even if the count gate would be met. Verifies the persistent
// maxq limits stay at their seeded (best, worst) bounds.
func TestNiMaxQLimitClampBelow150IsNoOp(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		niFrames:     149,
		niAvQi:       60,
		totalStats:   FirstPassFrameStats{Count: 1000},
		maxqMinLimit: 4,
		maxqMaxLimit: 127,
	}
	minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
	if minLimit != 4 || maxLimit != 127 {
		t.Fatalf("ni_frames=149: got (min=%d,max=%d), want (4,127) — clamp must be a no-op below the 150 floor", minLimit, maxLimit)
	}
	if state.maxqMinLimit != 4 || state.maxqMaxLimit != 127 {
		t.Fatalf("ni_frames=149: persistent limits mutated to (%d,%d); want (4,127)", state.maxqMinLimit, state.maxqMaxLimit)
	}
}

// TestNiMaxQLimitClampSmallTotalIsNoOp covers the libvpx firstpass.c
// upper gate: when ni_frames > (total_stats.count >> 8) is false, the
// clamp must not fire. With ni_frames=200 and total=100000, the right-
// shift gives 390, so ni_frames=200 <= 390 means the predicate is
// false and limits stay seeded.
func TestNiMaxQLimitClampSmallTotalIsNoOp(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		niFrames:     200,
		niAvQi:       60,
		totalStats:   FirstPassFrameStats{Count: 100000},
		maxqMinLimit: 4,
		maxqMaxLimit: 127,
	}
	minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
	if minLimit != 4 || maxLimit != 127 {
		t.Fatalf("ni_frames=200, total=100000: got (min=%d,max=%d), want (4,127) — total/256=390 exceeds ni_frames, clamp must not fire", minLimit, maxLimit)
	}
}

// TestNiMaxQLimitClampFiresAndNarrows covers the libvpx firstpass.c
// lines 994-1006 hot path: when both gates are met (ni_frames>150 AND
// ni_frames > total/256), the persistent maxq limits are narrowed to
// ni_av_qi±32, bounded by best/worst. Uses ni_frames=200, total=500
// (total/256=1 << 200) and ni_av_qi=60 so the narrowed band is [28,92].
func TestNiMaxQLimitClampFiresAndNarrows(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		niFrames:     200,
		niAvQi:       60,
		totalStats:   FirstPassFrameStats{Count: 500},
		maxqMinLimit: 4,
		maxqMaxLimit: 127,
	}
	minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
	if minLimit != 28 || maxLimit != 92 {
		t.Fatalf("ni_frames=200, total=500, ni_av_qi=60: got (min=%d,max=%d), want (28,92)", minLimit, maxLimit)
	}
	// libvpx mutates cpi->twopass.maxq_{min,max}_limit in place.
	if state.maxqMinLimit != 28 || state.maxqMaxLimit != 92 {
		t.Fatalf("ni_frames=200, total=500: persistent limits=(%d,%d), want (28,92) — clamp must mutate state", state.maxqMinLimit, state.maxqMaxLimit)
	}
}

// TestNiMaxQLimitClampClampsToWorstAndBest covers the libvpx
// firstpass.c lines 1000-1005 ternary bounds: when ni_av_qi+32 exceeds
// worst_quality, the narrowed maxq_max_limit is pinned at
// worst_quality; symmetrically the lower side is pinned at
// best_quality. Uses ni_av_qi=110 (so +32=142 > worst=127 → 127) and
// ni_av_qi=20 (so -32=-12 < best=4 → 4) in two sub-cases.
func TestNiMaxQLimitClampClampsToWorstAndBest(t *testing.T) {
	t.Run("upper_pinned_at_worst", func(t *testing.T) {
		state := twoPassState{
			bestQuality:  4,
			worstQuality: 127,
			niFrames:     200,
			niAvQi:       110,
			totalStats:   FirstPassFrameStats{Count: 500},
			maxqMinLimit: 4,
			maxqMaxLimit: 127,
		}
		minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
		if maxLimit != 127 {
			t.Fatalf("ni_av_qi=110: maxLimit=%d, want 127 (ni_av_qi+32=142 should clamp to worst_quality)", maxLimit)
		}
		if minLimit != 78 {
			t.Fatalf("ni_av_qi=110: minLimit=%d, want 78", minLimit)
		}
	})
	t.Run("lower_pinned_at_best", func(t *testing.T) {
		state := twoPassState{
			bestQuality:  4,
			worstQuality: 127,
			niFrames:     200,
			niAvQi:       20,
			totalStats:   FirstPassFrameStats{Count: 500},
			maxqMinLimit: 4,
			maxqMaxLimit: 127,
		}
		minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
		if minLimit != 4 {
			t.Fatalf("ni_av_qi=20: minLimit=%d, want 4 (ni_av_qi-32=-12 should clamp to best_quality)", minLimit)
		}
		if maxLimit != 52 {
			t.Fatalf("ni_av_qi=20: maxLimit=%d, want 52", maxLimit)
		}
	})
}

// TestRecordInterFrameQuantizerSkipsKeyAndGFRefresh covers the libvpx
// vp8/encoder/onyx_if.c lines 4478-4480 update gate: KEY frames and
// (single-layer) golden/alt-ref refresh frames must NOT increment
// ni_frames. Verifies the recorder respects all three skip conditions.
func TestRecordInterFrameQuantizerSkipsKeyAndGFRefresh(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		stats:        []FirstPassFrameStats{{}, {}, {}, {}},
	}
	// Key frame: skip regardless of refresh flags.
	state.recordInterFrameQuantizer(50, true, false, false, 1, true)
	// Single-layer golden refresh: skip.
	state.recordInterFrameQuantizer(50, false, true, false, 1, true)
	// Single-layer altref refresh: skip.
	state.recordInterFrameQuantizer(50, false, false, true, 1, true)
	if state.niFrames != 0 {
		t.Fatalf("recordInterFrameQuantizer skip cases incremented niFrames=%d, want 0", state.niFrames)
	}
	if state.niTotQi != 0 {
		t.Fatalf("recordInterFrameQuantizer skip cases mutated niTotQi=%d, want 0", state.niTotQi)
	}
}

// TestRecordInterFrameQuantizerPass2RunningAverage covers the libvpx
// onyx_if.c lines 4486-4488 pass-2 branch: ni_av_qi tracks the simple
// cumulative average of recorded Q values. Verifies the running mean
// is exact after several increments.
func TestRecordInterFrameQuantizerPass2RunningAverage(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		stats:        []FirstPassFrameStats{{}, {}, {}, {}},
	}
	qs := []int{40, 50, 60, 70}
	want := 0
	for _, q := range qs {
		state.recordInterFrameQuantizer(q, false, false, false, 1, true)
		want += q
	}
	if state.niFrames != len(qs) {
		t.Fatalf("niFrames=%d, want %d", state.niFrames, len(qs))
	}
	if state.niTotQi != want {
		t.Fatalf("niTotQi=%d, want %d", state.niTotQi, want)
	}
	if state.niAvQi != want/len(qs) {
		t.Fatalf("niAvQi=%d, want %d (pass=2: simple cumulative average)", state.niAvQi, want/len(qs))
	}
}

// TestRecordInterFrameQuantizerLayeredCountsGoldenRefresh covers the
// libvpx onyx_if.c line 4479 layered branch:
//
//	if ((frame_type != KEY) &&
//	    ((number_of_layers > 1) ||
//	     (!refresh_golden && !refresh_alt))) { ni_frames++; }
//
// When number_of_layers > 1 the second disjunct is the always-true
// upper branch, so golden / altref refresh frames DO increment
// ni_frames in layered mode.
func TestRecordInterFrameQuantizerLayeredCountsGoldenRefresh(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		stats:        []FirstPassFrameStats{{}, {}},
	}
	// number_of_layers=2: golden refresh now increments ni_frames.
	state.recordInterFrameQuantizer(50, false, true, false, 2, true)
	if state.niFrames != 1 || state.niAvQi != 50 {
		t.Fatalf("layered golden refresh: niFrames=%d niAvQi=%d, want (1, 50)", state.niFrames, state.niAvQi)
	}
}
