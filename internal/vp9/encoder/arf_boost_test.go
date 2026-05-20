package encoder

import (
	"math"
	"testing"
)

// libvpxAdjustARNRFilterReference is a direct translation of libvpx's
// `adjust_arnr_filter` (vp9/encoder/vp9_temporal_filter.c:1255) used as a
// black-box oracle to validate AdjustARNRFilter.
func libvpxAdjustARNRFilterReference(in AdjustARNRFilterInput) TemporalFilterAdjustment {
	maxFwd := max(in.LookaheadDepth-in.Distance-1, 0)
	maxBwd := max(in.Distance, 0)
	frames := max(in.ARNRMaxFrames, 1)

	var baseStrength int
	if in.Pass == 2 {
		baseStrength = min(max(in.ARNRStrengthBase+in.ARNRStrengthAdjustment, 0), 6)
	} else {
		baseStrength = in.ARNRStrengthBase
	}

	var q int
	if in.CurrentVideoFrame > 1 {
		q = int(ConvertQIndexToQ(in.AvgFrameQIndexInter))
	} else {
		q = int(ConvertQIndexToQ(in.AvgFrameQIndexKey))
	}
	var strength int
	if q > 16 {
		strength = baseStrength
	} else {
		strength = max(baseStrength-((16-q)/2), 0)
	}

	if cap := in.GroupBoost / 150; cap < frames {
		frames = cap
	}
	if cap := in.GroupBoost / 300; strength > cap {
		strength = cap
	}

	var framesBackward, framesForward int
	minSide := min(maxBwd, maxFwd)
	if minSide >= frames/2 {
		framesBackward = frames / 2
		framesForward = (frames - 1) / 2
	} else if maxFwd < frames/2 {
		framesForward = maxFwd
		fb := min(maxBwd, frames-1-framesForward)
		framesBackward = fb
	} else {
		framesBackward = maxBwd
		ff := min(maxFwd, frames-1-framesBackward)
		framesForward = ff
	}

	frames = framesBackward + 1 + framesForward
	if frames <= 1 {
		frames = 1
		framesBackward = 0
		framesForward = 0
	}

	return TemporalFilterAdjustment{
		ARNRFrames:     frames,
		FramesBackward: framesBackward,
		FramesForward:  framesForward,
		ARNRStrength:   strength,
	}
}

// TestAdjustARNRFilterMatchesLibvpx exercises AdjustARNRFilter
// across the libvpx parameter ranges that drive the adaptive temporal-filter
// strength and even/odd window placement. Every input combination is
// double-checked against a direct translation of libvpx
// adjust_arnr_filter (vp9_temporal_filter.c:1255), so any future drift from
// the C reference produces a test failure.
func TestAdjustARNRFilterMatchesLibvpx(t *testing.T) {
	cases := []AdjustARNRFilterInput{
		// libvpx default: vpxenc good-pass, 7-frame ARNR window, strength
		// 3, mid-QP inter section.
		{
			LookaheadDepth:      16,
			Distance:            7,
			GroupBoost:          900,
			ARNRMaxFrames:       7,
			ARNRStrengthBase:    3,
			Pass:                2,
			CurrentVideoFrame:   8,
			AvgFrameQIndexInter: 100,
			AvgFrameQIndexKey:   80,
		},
		// Very low Q (high quality) — strength should drop by (16-q)/2.
		{
			LookaheadDepth:      16,
			Distance:            7,
			GroupBoost:          900,
			ARNRMaxFrames:       5,
			ARNRStrengthBase:    3,
			Pass:                1,
			CurrentVideoFrame:   2,
			AvgFrameQIndexInter: 8,
			AvgFrameQIndexKey:   8,
		},
		// Low boost → frame cap collapses.
		{
			LookaheadDepth:      16,
			Distance:            5,
			GroupBoost:          250,
			ARNRMaxFrames:       7,
			ARNRStrengthBase:    5,
			Pass:                2,
			CurrentVideoFrame:   20,
			AvgFrameQIndexInter: 200,
			AvgFrameQIndexKey:   180,
		},
		// Asymmetric: no forward window available (alt-ref at end of
		// lookahead).
		{
			LookaheadDepth:      8,
			Distance:            7,
			GroupBoost:          900,
			ARNRMaxFrames:       7,
			ARNRStrengthBase:    3,
			Pass:                2,
			CurrentVideoFrame:   30,
			AvgFrameQIndexInter: 120,
			AvgFrameQIndexKey:   100,
		},
		// Asymmetric: no backward window (start of stream).
		{
			LookaheadDepth:      16,
			Distance:            0,
			GroupBoost:          800,
			ARNRMaxFrames:       7,
			ARNRStrengthBase:    3,
			Pass:                2,
			CurrentVideoFrame:   1,
			AvgFrameQIndexInter: 70,
			AvgFrameQIndexKey:   90,
		},
		// Pass-1 strength adjustment is ignored even when nonzero.
		{
			LookaheadDepth:         16,
			Distance:               5,
			GroupBoost:             900,
			ARNRMaxFrames:          7,
			ARNRStrengthBase:       3,
			ARNRStrengthAdjustment: 2,
			Pass:                   1,
			CurrentVideoFrame:      8,
			AvgFrameQIndexInter:    120,
			AvgFrameQIndexKey:      100,
		},
		// Pass-2 strength adjustment is consumed and clamped to [0,6].
		{
			LookaheadDepth:         16,
			Distance:               5,
			GroupBoost:             1800,
			ARNRMaxFrames:          7,
			ARNRStrengthBase:       4,
			ARNRStrengthAdjustment: 5,
			Pass:                   2,
			CurrentVideoFrame:      8,
			AvgFrameQIndexInter:    120,
			AvgFrameQIndexKey:      100,
		},
		// Single-frame stream — frames=1 path.
		{
			LookaheadDepth:      1,
			Distance:            0,
			GroupBoost:          900,
			ARNRMaxFrames:       7,
			ARNRStrengthBase:    3,
			Pass:                2,
			CurrentVideoFrame:   1,
			AvgFrameQIndexInter: 100,
			AvgFrameQIndexKey:   80,
		},
		// Maximum boost — frame cap saturates at the configured max.
		{
			LookaheadDepth:      20,
			Distance:            10,
			GroupBoost:          5000,
			ARNRMaxFrames:       15,
			ARNRStrengthBase:    6,
			Pass:                2,
			CurrentVideoFrame:   50,
			AvgFrameQIndexInter: 150,
			AvgFrameQIndexKey:   130,
		},
	}
	for i, in := range cases {
		got := AdjustARNRFilter(in)
		want := libvpxAdjustARNRFilterReference(in)
		if got != want {
			t.Errorf("case %d: AdjustARNRFilter%+v\n  got=%+v\n want=%+v",
				i, in, got, want)
		}
		// Invariants from libvpx contract.
		if got.ARNRFrames < 1 {
			t.Errorf("case %d: ARNRFrames=%d, want >= 1", i, got.ARNRFrames)
		}
		if got.ARNRStrength < 0 || got.ARNRStrength > 6 {
			t.Errorf("case %d: ARNRStrength=%d, want in [0,6]", i, got.ARNRStrength)
		}
		if got.FramesBackward+1+got.FramesForward != got.ARNRFrames {
			t.Errorf("case %d: frame count mismatch back=%d fwd=%d total=%d",
				i, got.FramesBackward, got.FramesForward, got.ARNRFrames)
		}
	}
}

// TestAdjustARNRFilterSymmetricWindowOnEvenFrames verifies libvpx's
// even/odd window split: for even frames=N, framesBackward = N/2 and
// framesForward = (N-1)/2 = N/2-1.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1297 (even/odd case).
func TestAdjustARNRFilterSymmetricWindowOnEvenFrames(t *testing.T) {
	in := AdjustARNRFilterInput{
		LookaheadDepth:      30,
		Distance:            15,
		GroupBoost:          5000,
		ARNRMaxFrames:       7, // (odd) → adjust_arnr_filter calls 7/2=3, (7-1)/2=3
		ARNRStrengthBase:    3,
		Pass:                2,
		CurrentVideoFrame:   10,
		AvgFrameQIndexInter: 100,
		AvgFrameQIndexKey:   90,
	}
	got := AdjustARNRFilter(in)
	if got.FramesBackward != 3 || got.FramesForward != 3 || got.ARNRFrames != 7 {
		t.Fatalf("symmetric odd: got %+v, want back=3 fwd=3 frames=7", got)
	}
	in.ARNRMaxFrames = 6 // even → back=3, fwd=2
	got = AdjustARNRFilter(in)
	if got.FramesBackward != 3 || got.FramesForward != 2 || got.ARNRFrames != 6 {
		t.Fatalf("symmetric even: got %+v, want back=3 fwd=2 frames=6", got)
	}
}

// TestAdjustARNRFilterStrengthScalesWithBoost confirms that the
// `strength <= group_boost/300` clamp engages at low boosts. libvpx
// reduces ARNR aggressiveness for low-energy sections.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1292
func TestAdjustARNRFilterStrengthScalesWithBoost(t *testing.T) {
	in := AdjustARNRFilterInput{
		LookaheadDepth:      16,
		Distance:            7,
		GroupBoost:          300,
		ARNRMaxFrames:       7,
		ARNRStrengthBase:    5,
		Pass:                2,
		CurrentVideoFrame:   8,
		AvgFrameQIndexInter: 100,
		AvgFrameQIndexKey:   90,
	}
	got := AdjustARNRFilter(in)
	// boost=300 → strength cap = 300/300 = 1, so strength clamps to 1.
	if got.ARNRStrength != 1 {
		t.Fatalf("strength=%d, want clamped to 1 (boost=300/300)", got.ARNRStrength)
	}
	in.GroupBoost = 1800 // 1800/300 = 6, no clamping
	got = AdjustARNRFilter(in)
	if got.ARNRStrength != 5 {
		t.Fatalf("strength=%d, want base=5 (boost=1800 no clamp)", got.ARNRStrength)
	}
}

// TestAdjustARNRFilterLowQReducesStrength verifies libvpx's q<=16 path
// that lowers the temporal-filter strength on high-quality (low Q) sections.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1282
func TestAdjustARNRFilterLowQReducesStrength(t *testing.T) {
	in := AdjustARNRFilterInput{
		LookaheadDepth:      16,
		Distance:            7,
		GroupBoost:          5000,
		ARNRMaxFrames:       7,
		ARNRStrengthBase:    6,
		Pass:                2,
		CurrentVideoFrame:   8,
		AvgFrameQIndexInter: 0, // q = ac_quant_lookup[0]/4 = 4/4 = 1 → low
		AvgFrameQIndexKey:   0,
	}
	got := AdjustARNRFilter(in)
	// q=1; strength = 6 - ((16-1)/2) = 6 - 7 = -1 → clamp to 0
	if got.ARNRStrength != 0 {
		t.Fatalf("low Q strength=%d, want 0", got.ARNRStrength)
	}
}

// TestComputeARFBoostMonotonicInWindow exercises ComputeARFBoost across
// a synthesized first-pass stat sequence and confirms the libvpx contract:
//   - boost is bounded below by MIN_ARF_GF_BOOST,
//   - boost is bounded below by (b_frames+f_frames)*40,
//   - widening the window can only increase the boost when the underlying
//     frames are similar.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1936 compute_arf_boost
func TestComputeARFBoostMatchesLibvpx(t *testing.T) {
	mbRows := 4
	params := DefaultARFBoostParams(mbRows)

	// Synthesize a uniform mid-motion sequence so the per-frame boost is
	// stable and we can verify the iteration mechanics.
	stats := make([]FirstPassFrameStats, 32)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			Frame:            uint64(i),
			CodedError:       500,
			IntraError:       3000,
			SRCodedError:     520,
			PcntInter:        0.85,
			PcntMotion:       0.30,
			PcntSecondRef:    0.05,
			PcntNeutral:      0.05,
			PcntIntraLow:     0.05,
			MVrAbs:           10,
			MVcAbs:           10,
			MVInOutCount:     0.0,
			IntraSkipPct:     0.0,
			InactiveZoneRows: 0.0,
		}
	}
	boost := ComputeARFBoost(stats, 16, 4, 4, 100, params)
	if boost < MinARFGFBoost {
		t.Fatalf("boost=%d < MIN_ARF_GF_BOOST=%d", boost, MinARFGFBoost)
	}
	if boost < (4+4)*40 {
		t.Fatalf("boost=%d < (b+f)*40=%d", boost, (4+4)*40)
	}
	wider := ComputeARFBoost(stats, 16, 8, 8, 100, params)
	if wider < boost {
		t.Fatalf("widening window dropped boost: 4+4=%d, 8+8=%d", boost, wider)
	}

	// MIN_ARF_GF_BOOST floor: zero-length window still returns >= floor.
	if got := ComputeARFBoost(stats, 16, 0, 0, 100, params); got != MinARFGFBoost {
		t.Fatalf("zero window: got %d, want %d", got, MinARFGFBoost)
	}
}

// TestComputeARFBoostDecayDropsWithLowInter confirms the decay accumulator
// reduces contributions when zero-motion is low (high motion content). This
// is the libvpx mechanism that keeps the ARF boost from over-inflating on
// scenes where prediction quality collapses.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1970 decay_accumulator update
func TestComputeARFBoostDecayDropsWithLowInter(t *testing.T) {
	mbRows := 4
	params := DefaultARFBoostParams(mbRows)

	// Use small coded_error so per-frame boost dominates the (b+f)*40
	// floor; otherwise both branches collapse to the same floor and the
	// decay distinction is invisible.
	easy := make([]FirstPassFrameStats, 16)
	for i := range easy {
		easy[i] = FirstPassFrameStats{
			CodedError:    10,
			IntraError:    3000,
			SRCodedError:  10,
			PcntInter:     0.95,
			PcntMotion:    0.05,
			PcntSecondRef: 0.05,
			MVrAbs:        2,
			MVcAbs:        2,
		}
	}
	hard := make([]FirstPassFrameStats, 16)
	for i := range hard {
		hard[i] = FirstPassFrameStats{
			CodedError:    10,
			IntraError:    3000,
			SRCodedError:  2700, // huge sr_diff → strong sr_decay drop
			PcntInter:     0.40,
			PcntMotion:    0.40,
			PcntSecondRef: 0.10,
			MVrAbs:        30,
			MVcAbs:        30,
		}
	}
	easyBoost := ComputeARFBoost(easy, 8, 4, 4, 100, params)
	hardBoost := ComputeARFBoost(hard, 8, 4, 4, 100, params)
	if easyBoost <= hardBoost {
		t.Fatalf("easy boost=%d should be > hard boost=%d", easyBoost, hardBoost)
	}
}

// TestComputeARFBoostUsesFloors validates the libvpx floor enforcement:
//   - arf_boost = max(arf_boost, (b+f)*40)
//   - arf_boost = max(arf_boost, MIN_ARF_GF_BOOST)
//
// libvpx: vp9/encoder/vp9_firstpass.c:2021-2023
func TestComputeARFBoostUsesFloors(t *testing.T) {
	mbRows := 4
	params := DefaultARFBoostParams(mbRows)

	// Empty stats → 0 boost from the loop, but floor applies.
	if got := ComputeARFBoost(nil, 0, 3, 3, 100, params); got != MinARFGFBoost {
		t.Fatalf("empty stats: got %d, want %d", got, MinARFGFBoost)
	}

	stats := []FirstPassFrameStats{
		{CodedError: 1e9, IntraError: 1e9, SRCodedError: 1e9, PcntInter: 0.5, PcntMotion: 0.5},
		{CodedError: 1e9, IntraError: 1e9, SRCodedError: 1e9, PcntInter: 0.5, PcntMotion: 0.5},
	}
	// Per-frame boost will be tiny (huge coded_error denominator); floor
	// (b+f)*40 = 8*40 = 320 ≥ MIN_ARF_GF_BOOST(250), so final = 320.
	got := ComputeARFBoost(stats, 1, 4, 4, 100, params)
	if got < (4+4)*40 {
		t.Fatalf("got %d, want >= (b+f)*40 = %d", got, (4+4)*40)
	}
}

// TestAdjustARNRFilterCollapsesAtMinBoostFloor confirms that the minimum
// GF/ARF boost collapses to the libvpx single-frame temporal-filter window.
func TestAdjustARNRFilterCollapsesAtMinBoostFloor(t *testing.T) {
	in := AdjustARNRFilterInput{
		LookaheadDepth:      8,
		Distance:            4,
		GroupBoost:          MinARFGFBoost,
		ARNRMaxFrames:       7,
		ARNRStrengthBase:    3,
		Pass:                1,
		CurrentVideoFrame:   4,
		AvgFrameQIndexInter: 100,
		AvgFrameQIndexKey:   90,
	}
	got := AdjustARNRFilter(in)
	// 250/150 = 1 → frames=1; collapsed to single-frame no-op window.
	if got.ARNRFrames != 1 || got.FramesBackward != 0 || got.FramesForward != 0 {
		t.Fatalf("min-boost collapse: got %+v, want frames=1 back=0 fwd=0", got)
	}
}

// TestCalcFrameBoostFiniteOnDegenerateInput stresses calc_frame_boost
// against zero / NaN-prone inputs to confirm DOUBLE_DIVIDE_CHECK shields the
// boost computation.
func TestCalcFrameBoostFiniteOnDegenerateInput(t *testing.T) {
	mbRows := 4
	frame := FirstPassFrameStats{
		CodedError:       0,
		IntraError:       0,
		SRCodedError:     0,
		PcntInter:        0,
		PcntMotion:       0,
		IntraSkipPct:     1.0,
		InactiveZoneRows: float64(mbRows),
	}
	got := CalcFrameBoost(frame, BaselineErrPerMB, GFMaxFrameBoost,
		mbRows, 100, 0.0)
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("calc_frame_boost returned %v on degenerate input", got)
	}
}
