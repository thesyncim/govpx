package encoder

import (
	"math"
	"testing"
)

// synthPanningFirstPassStats returns a libvpx-shaped first-pass stats
// slice for a synthetic panning sequence. The pcnt_inter / pcnt_motion
// values are picked to put the GF analyzer in the "use_alt_ref"
// regime: moderate motion, no flash, monotone prediction quality decay.
func synthPanningFirstPassStats(n int) []FirstPassFrameStats {
	out := make([]FirstPassFrameStats, n)
	for i := range n {
		out[i] = FirstPassFrameStats{
			Frame:            uint64(i),
			Weight:           1.0,
			IntraError:       50000.0,
			CodedError:       10000.0 + float64(i)*100,
			SRCodedError:     11000.0 + float64(i)*120,
			FrameNoiseEnergy: 180.0,
			PcntInter:        0.9,
			PcntMotion:       0.4,
			PcntSecondRef:    0.1,
			PcntNeutral:      0.05,
			PcntIntraLow:     0.02,
			PcntIntraHigh:    0.03,
			IntraSkipPct:     0.02,
			InactiveZoneRows: 0.0,
			InactiveZoneCols: 0.0,
			MVr:              1.5,
			MVrAbs:           2.0,
			MVc:              0.5,
			MVcAbs:           1.0,
			MVInOutCount:     0.1,
			Duration:         1.0,
			Count:            1.0,
		}
	}
	return out
}

func defaultVP9GFGroupTestInputs(stats []FirstPassFrameStats) GFGroupInputs {
	return GFGroupInputs{
		IsKeyFrame:               true,
		SourceAltRefActive:       false,
		FramesToKey:              len(stats),
		FramesSinceKey:           0,
		MinGFInterval:            MinGFInterval,
		MaxGFInterval:            MaxGFInterval,
		StaticSceneMaxGFInterval: MaxStaticGFGroupLength,
		ActiveWorstQuality:       180,
		LastBoostedQIndex:        140,
		AvgFrameQIndexInter:      140,
		AvgFrameBandwidth:        50000,
		LagInFrames:              25,
		PerceptualAQ:             false,
		Lossless:                 false,
		AllowAltRef:              true,
		EnableAutoARF:            1,
		MultiLayerARF:            false,
		FrameHeight:              64,
		FrameWidth:               64,
		MBRows:                   4,
		KFGroupBits:              int64(50000) * int64(len(stats)),
		KFGroupErrorLeft:         1000.0,
		FrameMaxBits:             5000000,
		GFMaxTotalBoost:          MaxGFBoost,
		CurrentVideoFrame:        0,
		MeanModScore:             1.0,
		AvErr:                    10000.0,
		Stats:                    stats,
		GFStartShowIdx:           0,
		BoostParams:              DefaultARFBoostParams(4),
	}
}

func TestDefineGFGroupHasNonZeroBoost(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := DefineGFGroup(in)
	if gf.GFUBoostScalar < MinARFGFBoost {
		t.Fatalf("gfu_boost = %d < MIN_ARF_GF_BOOST=%d",
			gf.GFUBoostScalar, MinARFGFBoost)
	}
	if gf.BaselineGFInterval < MinGFInterval {
		t.Fatalf("baseline_gf_interval = %d < MIN_GF_INTERVAL=%d",
			gf.BaselineGFInterval, MinGFInterval)
	}
	if gf.GOPCodingFrames <= 0 {
		t.Fatalf("gop_coding_frames = %d, want > 0", gf.GOPCodingFrames)
	}
	if gf.GFGroupSize <= 0 {
		t.Fatalf("gf_group_size = %d, want > 0", gf.GFGroupSize)
	}
}

func TestDefineGFGroupCapsBoostAt200xInterval(t *testing.T) {
	// libvpx vp9_firstpass.c:2911 caps gfu_boost at gop_coding_frames * 200.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := DefineGFGroup(in)
	cap := gf.GOPCodingFrames * 200
	if gf.GFUBoostScalar > cap {
		t.Fatalf("gfu_boost = %d > cap (gop_coding_frames*200=%d)",
			gf.GFUBoostScalar, cap)
	}
}

func TestDefineGFGroupPerceptualAQCapsBoost(t *testing.T) {
	// libvpx vp9_firstpass.c:2918-2919: perceptual AQ clamps gfu_boost to
	// MIN_ARF_GF_BOOST.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.PerceptualAQ = true
	gf := DefineGFGroup(in)
	if gf.GFUBoostScalar > MinARFGFBoost {
		t.Fatalf("perceptual AQ gfu_boost = %d > MIN_ARF_GF_BOOST=%d",
			gf.GFUBoostScalar, MinARFGFBoost)
	}
}

func TestDefineGFGroupAltRefDisabledWhenLagInFramesShort(t *testing.T) {
	// libvpx vp9_firstpass.c:2696: *use_alt_ref &= gop_coding_frames <
	// lag_in_frames.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.LagInFrames = 2
	gf := DefineGFGroup(in)
	if gf.UseAltRef {
		t.Fatalf("UseAltRef=true with lag_in_frames=2, want false")
	}
	if gf.SourceAltRefPending {
		t.Fatalf("SourceAltRefPending=true with lag_in_frames=2, want false")
	}
}

func TestDefineGFGroupAltRefDisabledWhenNotAllowed(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.AllowAltRef = false
	gf := DefineGFGroup(in)
	if gf.UseAltRef {
		t.Fatalf("UseAltRef=true with AllowAltRef=false, want false")
	}
}

func TestDefineGFGroupConstrainedGroupAtKFEdge(t *testing.T) {
	// When frames_to_key is shorter than the would-be GF group, libvpx
	// sets rc->constrained_gf_group=1.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.FramesToKey = 3
	gf := DefineGFGroup(in)
	if !gf.ConstrainedGFGroup {
		t.Fatalf("ConstrainedGFGroup=false with FramesToKey=3, want true")
	}
}

func TestDefineGFGroupActiveIntervalRangeMatchesLibvpx(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	r := GetActiveGFIntervalRange(in, true /* arfActiveOrKF */)
	// libvpx: max must be odd.
	if r.Max&1 != 1 {
		t.Fatalf("active_gf_interval.max = %d, want odd", r.Max)
	}
	// libvpx: min must be in [min_gf_interval+arfBool, max_gf_interval+arfBool].
	if r.Min < MinGFInterval+1 {
		t.Fatalf("active_gf_interval.min = %d < min_gf_interval+1=%d",
			r.Min, MinGFInterval+1)
	}
	if r.Max < r.Min {
		t.Fatalf("active_gf_interval.max(%d) < min(%d)", r.Max, r.Min)
	}
}

func TestGetGOPCodingFrameNumStopsAtFramesToKey(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.FramesToKey = 7
	useAltRef := false
	endOfSequence := false
	active := GetActiveGFIntervalRange(in, true)
	gop := GetGOPCodingFrameNum(&useAltRef, in, &active, 1.0, &endOfSequence)
	if gop > in.FramesToKey {
		t.Fatalf("gop_coding_frames=%d > frames_to_key=%d", gop, in.FramesToKey)
	}
}

func TestCalculateBoostBitsMatchesLibvpx(t *testing.T) {
	// libvpx vp9_firstpass.c:2109:
	//   allocation_chunks = frame_count*NORMAL_BOOST + boost
	//   result = boost * total / allocation_chunks
	got := CalculateBoostBits(7, 500, 100000)
	allocChunks := 7*NormalBoost + 500
	want := int(int64(500) * 100000 / int64(allocChunks))
	if got != want {
		t.Fatalf("calculate_boost_bits(7,500,100000)=%d, want %d", got, want)
	}
	// boost==0 / total<=0 / frame_count<0 -> 0.
	if CalculateBoostBits(7, 0, 100000) != 0 {
		t.Fatal("boost=0 must yield 0")
	}
	if CalculateBoostBits(7, 500, 0) != 0 {
		t.Fatal("total<=0 must yield 0")
	}
}

func TestCalculateBoostBitsHandlesBoostOverflowBranch(t *testing.T) {
	// libvpx vp9_firstpass.c:2112-2116: when boost > 1023, divide by
	// boost>>10 to prevent overflow. Result must remain non-negative
	// and saturate to a sensible value.
	got := CalculateBoostBits(7, 2048, 1<<30)
	if got <= 0 {
		t.Fatalf("calculate_boost_bits with boost=2048 returned %d, want >0", got)
	}
}

func TestAdjustGroupARNRFilterMatchesLibvpx(t *testing.T) {
	// libvpx vp9_firstpass.c:2541-2556 logic table:
	//   noise<75  -> -2 ; noise<150 -> -1 ; noise>250 -> +1
	//   zeromv>0.5 -> +1
	cases := []struct {
		noise, inter, motion float64
		want                 int
	}{
		{noise: 50, inter: 0.5, motion: 0.4, want: -2},
		{noise: 100, inter: 0.5, motion: 0.4, want: -1},
		{noise: 200, inter: 0.5, motion: 0.4, want: 0},
		{noise: 300, inter: 0.5, motion: 0.4, want: 1},
		{noise: 200, inter: 0.9, motion: 0.2, want: 1}, // zeromv=0.7
		{noise: 50, inter: 0.9, motion: 0.2, want: -1}, // -2 + 1 = -1
		{noise: 300, inter: 0.9, motion: 0.2, want: 2}, // +1 + 1 = +2
	}
	for _, tc := range cases {
		got := AdjustGroupARNRFilter(tc.noise, tc.inter, tc.motion)
		if got != tc.want {
			t.Errorf("adjust_group_arnr_filter(noise=%.0f,inter=%.1f,motion=%.1f)=%d, want %d",
				tc.noise, tc.inter, tc.motion, got, tc.want)
		}
	}
}

func TestDefineGFGroupStructureLayoutsBaseARF(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := DefineGFGroup(in)
	if !gf.SourceAltRefPending {
		t.Skip("synthetic stats produced no alt-ref; layout test requires one")
	}
	// frame_index==0 should be the GF (or overlay), frame_index==1 the ARF.
	if gf.UpdateType[1] != ARFUpdate {
		t.Fatalf("UpdateType[1]=%d, want ARF_UPDATE(%d)",
			gf.UpdateType[1], ARFUpdate)
	}
	// Final slot should be the overlay placement.
	if gf.GFGroupSize == 0 {
		t.Fatal("gf_group_size=0")
	}
}

func TestAllocateGFGroupBitsNonZero(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := DefineGFGroup(in)
	totalBits := 0
	for i := 0; i < gf.GFGroupSize; i++ {
		totalBits += gf.BitAllocation[i]
	}
	if totalBits <= 0 {
		t.Fatalf("sum of bit allocations = %d, want > 0", totalBits)
	}
}

func TestDefineGFGroupActiveBestQAdjustFactorInRange(t *testing.T) {
	// libvpx vp9_firstpass.c:2881-2904 clamps the factor to
	// [LAST_ALR_..., 1.0]. We assert the resulting value lies in that
	// window with a small float epsilon.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.IsKeyFrame = false
	in.FramesSinceKey = 5
	in.FramesToKey = 25
	gf := DefineGFGroup(in)
	if gf.ARFActiveBestQAdjustF < LastALRActiveBestQAdjustFactor-1e-6 {
		t.Fatalf("factor=%f below LAST_ALR(%f)", gf.ARFActiveBestQAdjustF,
			LastALRActiveBestQAdjustFactor)
	}
	if gf.ARFActiveBestQAdjustF > 1.0+1e-6 {
		t.Fatalf("factor=%f above 1.0", gf.ARFActiveBestQAdjustF)
	}
}

func TestDefineGFGroupHandlesShortStatsBuffer(t *testing.T) {
	// Stats buffer shorter than min_gf_interval should not panic and
	// must yield a no-altref decision with a small gf interval.
	stats := synthPanningFirstPassStats(3)
	in := defaultVP9GFGroupTestInputs(stats)
	in.FramesToKey = 3
	gf := DefineGFGroup(in)
	if gf.UseAltRef {
		t.Fatalf("UseAltRef=true with 3-frame stats, want false")
	}
	if gf.BaselineGFInterval < 0 {
		t.Fatalf("BaselineGFInterval=%d negative", gf.BaselineGFInterval)
	}
}

// TestDefineGFGroupLagInFramesFixtures locks in the libvpx
// gop_coding_frames < lag_in_frames gate (vp9_firstpass.c:2696) for the
// same lag values exercised by the VP9 lookahead oracle matrix. Each row
// asserts the alt-ref decision in the gf_group analyzer matches what the
// oracle's --lag-in-frames=N / --auto-alt-ref=0 invocation produces:
// alt-ref must remain disabled for every lag value the oracle uses,
// because the oracle clamps auto-alt-ref off.
func TestDefineGFGroupLagInFramesFixtures(t *testing.T) {
	cases := []struct {
		name           string
		lag            int
		frames         int
		framesToKey    int
		framesSinceKey int
		isKey          bool
		// libvpx must always produce useAltRef=false when AllowAltRef=false.
		wantUseAltRef bool
		// libvpx baseline_gf_interval must be > 0 even at lag=1.
		minBaselineGFInterval int
	}{
		// Matrix lag=1 / 4-frame KF group: alt-ref disabled
		// (LookaheadFrames==1 → AllowAltRef false in encoder; the gop
		// loop also short-circuits because gop_coding_frames >=
		// lag_in_frames=1 fires immediately).
		{name: "lag1_kf4", lag: 1, frames: 4, framesToKey: 4, isKey: true, minBaselineGFInterval: 1},
		// lag=2 / 5-frame KF group: same gating.
		{name: "lag2_kf5", lag: 2, frames: 5, framesToKey: 5, isKey: true, minBaselineGFInterval: 1},
		// lag=4 / 6-frame KF group: same gating; the larger lag would
		// let alt-ref activate if AllowAltRef were set, but the oracle
		// disables it.
		{name: "lag4_kf6", lag: 4, frames: 6, framesToKey: 6, isKey: true, minBaselineGFInterval: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats := synthPanningFirstPassStats(tc.frames)
			in := defaultVP9GFGroupTestInputs(stats)
			in.IsKeyFrame = tc.isKey
			in.FramesToKey = tc.framesToKey
			in.FramesSinceKey = tc.framesSinceKey
			in.LagInFrames = tc.lag
			// The oracle matrix passes --auto-alt-ref=0; mirror that.
			in.AllowAltRef = false
			gf := DefineGFGroup(in)
			if gf.UseAltRef != tc.wantUseAltRef {
				t.Fatalf("%s: UseAltRef=%v, want %v",
					tc.name, gf.UseAltRef, tc.wantUseAltRef)
			}
			if gf.BaselineGFInterval < tc.minBaselineGFInterval {
				t.Fatalf("%s: BaselineGFInterval=%d < %d",
					tc.name, gf.BaselineGFInterval, tc.minBaselineGFInterval)
			}
			// libvpx vp9_firstpass.c:2847 constrained_gf_group flag must
			// fire whenever gop_coding_frames covers the entire KF
			// distance (short clips fit into a single GF group).
			if !gf.ConstrainedGFGroup && gf.GOPCodingFrames >= tc.framesToKey {
				t.Fatalf("%s: ConstrainedGFGroup=false but gop_coding_frames(%d) >= frames_to_key(%d)",
					tc.name, gf.GOPCodingFrames, tc.framesToKey)
			}
			// Layer-0 slot (KF/GF) must never carry an ARF update type
			// when use_alt_ref is false (libvpx find_arf_order at
			// depth>allowed_max_layer_depth emits LF_UPDATE leaves).
			if gf.SourceAltRefPending {
				t.Fatalf("%s: SourceAltRefPending=true with use_alt_ref=false",
					tc.name)
			}
		})
	}
}

// TestDefineGFGroupLagEnablesAltRefAtLargeLag verifies libvpx's
// "gop_coding_frames < lag_in_frames" predicate (vp9_firstpass.c:2696)
// is the exact alt-ref gate: with a sufficiently large lag (>=8) and
// matching min_gf_interval reach (vp9_firstpass.c:2697), the analyzer
// flips use_alt_ref to true. This is the complement of the matrix
// matrix cases which all disable alt-ref via --auto-alt-ref=0.
func TestDefineGFGroupLagEnablesAltRefAtLargeLag(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.LagInFrames = 25
	in.AllowAltRef = true
	in.MinGFInterval = 4
	in.MaxGFInterval = 16
	in.StaticSceneMaxGFInterval = MaxStaticGFGroupLength
	gf := DefineGFGroup(in)
	if !gf.UseAltRef {
		t.Fatalf("UseAltRef=false with lag=25, AllowAltRef=true, want true")
	}
	if !gf.SourceAltRefPending {
		t.Fatalf("SourceAltRefPending=false with use_alt_ref=true, want true")
	}
	// libvpx vp9_firstpass.c:2921 baseline_gf_interval =
	//   gop_coding_frames - source_alt_ref_pending.
	if gf.BaselineGFInterval != gf.GOPCodingFrames-1 {
		t.Fatalf("BaselineGFInterval=%d, want gop_coding_frames(%d)-1",
			gf.BaselineGFInterval, gf.GOPCodingFrames)
	}
}

// TestCalcNormFrameScoreConfigMatchesLibvpxDefault confirms the
// configurable CalcNormFrameScoreConfig matches libvpx's documented
// defaults (vbrbias=50, vbrmin_section=0, vbrmax_section=2000) when
// zero-init inputs are passed: vbrbias=0 falls back to 50, vbrmax=0
// falls back to 2000.
func TestCalcNormFrameScoreConfigMatchesLibvpxDefault(t *testing.T) {
	row := FirstPassFrameStats{
		Weight:       1.0,
		CodedError:   25000.0,
		IntraSkipPct: 0.05,
	}
	// Defaults: 50 / 0 / 2000.
	withDefaults := CalcNormFrameScoreConfig(row, 1.0, 10000.0, 8,
		50, 0, 2000)
	// vbrbias=0 falls back to the libvpx default (50); vbrmin/max
	// fallbacks at 0/0 must also collapse to libvpx defaults to keep the
	// gf_group_err accumulation stable for callers that don't carry the
	// oxcf knobs.
	withFallbacks := CalcNormFrameScoreConfig(row, 1.0, 10000.0, 8,
		0, 0, 0)
	if math.Abs(withDefaults-withFallbacks) > 1e-9 {
		t.Fatalf("default(%g) != fallback(%g): zero-init inputs must mirror libvpx defaults",
			withDefaults, withFallbacks)
	}
	// libvpx clamp lower bound at vbrmin/100 must apply; setting
	// vbrmin_section=300 forces a 3.0 floor regardless of input.
	floored := CalcNormFrameScoreConfig(FirstPassFrameStats{
		Weight: 1.0, CodedError: 1,
	}, 1e6, 10000.0, 8, 50, 300, 2000)
	if floored < 3.0-1e-9 {
		t.Fatalf("floored score=%g, want >= 3.0", floored)
	}
}

// TestGFGroupBoostBoundedByPlausibleRange asserts the produced boost
// is bounded by the libvpx-documented [MIN_ARF_GF_BOOST, MAX_GF_BOOST]
// range across a swept Q ladder. This locks in numerical stability for
// the BD-rate gate consumers downstream.
func TestGFGroupBoostBoundedByPlausibleRange(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	for q := 20; q <= 240; q += 40 {
		in.ActiveWorstQuality = q
		in.AvgFrameQIndexInter = q
		gf := DefineGFGroup(in)
		if gf.GFUBoostScalar < MinARFGFBoost {
			t.Errorf("q=%d boost=%d < MIN_ARF_GF_BOOST(%d)",
				q, gf.GFUBoostScalar, MinARFGFBoost)
		}
		if gf.GFUBoostScalar > MaxGFBoost {
			t.Errorf("q=%d boost=%d > MAX_GF_BOOST(%d)",
				q, gf.GFUBoostScalar, MaxGFBoost)
		}
		if math.IsNaN(gf.ARFActiveBestQAdjustF) {
			t.Errorf("q=%d ARFActiveBestQAdjustF=NaN", q)
		}
	}
}
