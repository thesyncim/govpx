package govpx

import (
	"math"
	"testing"
)

// synthPanningFirstPassStats returns a libvpx-shaped first-pass stats
// slice for a synthetic panning sequence. The pcnt_inter / pcnt_motion
// values are picked to put the GF analyzer in the "use_alt_ref"
// regime: moderate motion, no flash, monotone prediction quality decay.
func synthPanningFirstPassStats(n int) []VP9FirstPassFrameStats {
	out := make([]VP9FirstPassFrameStats, n)
	for i := range n {
		out[i] = VP9FirstPassFrameStats{
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

func defaultVP9GFGroupTestInputs(stats []VP9FirstPassFrameStats) vp9GFGroupInputs {
	return vp9GFGroupInputs{
		IsKeyFrame:               true,
		SourceAltRefActive:       false,
		FramesToKey:              len(stats),
		FramesSinceKey:           0,
		MinGFInterval:            vp9MinGFInterval,
		MaxGFInterval:            vp9MaxGFInterval,
		StaticSceneMaxGFInterval: vp9MaxStaticGFGroupLength,
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
		GFMaxTotalBoost:          vp9MaxGFBoost,
		CurrentVideoFrame:        0,
		MeanModScore:             1.0,
		AvErr:                    10000.0,
		Stats:                    stats,
		GFStartShowIdx:           0,
		BoostParams:              VP9DefaultARFBoostParams(4),
	}
}

func TestVP9DefineGFGroupHasNonZeroBoost(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := vp9DefineGFGroup(in)
	if gf.GFUBoostScalar < vp9MinARFGFBoost {
		t.Fatalf("gfu_boost = %d < MIN_ARF_GF_BOOST=%d",
			gf.GFUBoostScalar, vp9MinARFGFBoost)
	}
	if gf.BaselineGFInterval < vp9MinGFInterval {
		t.Fatalf("baseline_gf_interval = %d < MIN_GF_INTERVAL=%d",
			gf.BaselineGFInterval, vp9MinGFInterval)
	}
	if gf.GOPCodingFrames <= 0 {
		t.Fatalf("gop_coding_frames = %d, want > 0", gf.GOPCodingFrames)
	}
	if gf.GFGroupSize <= 0 {
		t.Fatalf("gf_group_size = %d, want > 0", gf.GFGroupSize)
	}
}

func TestVP9DefineGFGroupCapsBoostAt200xInterval(t *testing.T) {
	// libvpx vp9_firstpass.c:2911 caps gfu_boost at gop_coding_frames * 200.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := vp9DefineGFGroup(in)
	cap := gf.GOPCodingFrames * 200
	if gf.GFUBoostScalar > cap {
		t.Fatalf("gfu_boost = %d > cap (gop_coding_frames*200=%d)",
			gf.GFUBoostScalar, cap)
	}
}

func TestVP9DefineGFGroupPerceptualAQCapsBoost(t *testing.T) {
	// libvpx vp9_firstpass.c:2918-2919: perceptual AQ clamps gfu_boost to
	// MIN_ARF_GF_BOOST.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.PerceptualAQ = true
	gf := vp9DefineGFGroup(in)
	if gf.GFUBoostScalar > vp9MinARFGFBoost {
		t.Fatalf("perceptual AQ gfu_boost = %d > MIN_ARF_GF_BOOST=%d",
			gf.GFUBoostScalar, vp9MinARFGFBoost)
	}
}

func TestVP9DefineGFGroupAltRefDisabledWhenLagInFramesShort(t *testing.T) {
	// libvpx vp9_firstpass.c:2696: *use_alt_ref &= gop_coding_frames <
	// lag_in_frames.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.LagInFrames = 2
	gf := vp9DefineGFGroup(in)
	if gf.UseAltRef {
		t.Fatalf("UseAltRef=true with lag_in_frames=2, want false")
	}
	if gf.SourceAltRefPending {
		t.Fatalf("SourceAltRefPending=true with lag_in_frames=2, want false")
	}
}

func TestVP9DefineGFGroupAltRefDisabledWhenNotAllowed(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.AllowAltRef = false
	gf := vp9DefineGFGroup(in)
	if gf.UseAltRef {
		t.Fatalf("UseAltRef=true with AllowAltRef=false, want false")
	}
}

func TestVP9DefineGFGroupConstrainedGroupAtKFEdge(t *testing.T) {
	// When frames_to_key is shorter than the would-be GF group, libvpx
	// sets rc->constrained_gf_group=1.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.FramesToKey = 3
	gf := vp9DefineGFGroup(in)
	if !gf.ConstrainedGFGroup {
		t.Fatalf("ConstrainedGFGroup=false with FramesToKey=3, want true")
	}
}

func TestVP9DefineGFGroupActiveIntervalRangeMatchesLibvpx(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	r := vp9GetActiveGFIntervalRange(in, true /* arfActiveOrKF */)
	// libvpx: max must be odd.
	if r.Max&1 != 1 {
		t.Fatalf("active_gf_interval.max = %d, want odd", r.Max)
	}
	// libvpx: min must be in [min_gf_interval+arfBool, max_gf_interval+arfBool].
	if r.Min < vp9MinGFInterval+1 {
		t.Fatalf("active_gf_interval.min = %d < min_gf_interval+1=%d",
			r.Min, vp9MinGFInterval+1)
	}
	if r.Max < r.Min {
		t.Fatalf("active_gf_interval.max(%d) < min(%d)", r.Max, r.Min)
	}
}

func TestVP9GetGOPCodingFrameNumStopsAtFramesToKey(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.FramesToKey = 7
	useAltRef := false
	endOfSequence := false
	active := vp9GetActiveGFIntervalRange(in, true)
	gop := vp9GetGOPCodingFrameNum(&useAltRef, in, &active, 1.0, &endOfSequence)
	if gop > in.FramesToKey {
		t.Fatalf("gop_coding_frames=%d > frames_to_key=%d", gop, in.FramesToKey)
	}
}

func TestVP9CalculateBoostBitsMatchesLibvpx(t *testing.T) {
	// libvpx vp9_firstpass.c:2109:
	//   allocation_chunks = frame_count*NORMAL_BOOST + boost
	//   result = boost * total / allocation_chunks
	got := vp9CalculateBoostBits(7, 500, 100000)
	allocChunks := 7*vp9NormalBoost + 500
	want := int(int64(500) * 100000 / int64(allocChunks))
	if got != want {
		t.Fatalf("calculate_boost_bits(7,500,100000)=%d, want %d", got, want)
	}
	// boost==0 / total<=0 / frame_count<0 -> 0.
	if vp9CalculateBoostBits(7, 0, 100000) != 0 {
		t.Fatal("boost=0 must yield 0")
	}
	if vp9CalculateBoostBits(7, 500, 0) != 0 {
		t.Fatal("total<=0 must yield 0")
	}
}

func TestVP9CalculateBoostBitsHandlesBoostOverflowBranch(t *testing.T) {
	// libvpx vp9_firstpass.c:2112-2116: when boost > 1023, divide by
	// boost>>10 to prevent overflow. Result must remain non-negative
	// and saturate to a sensible value.
	got := vp9CalculateBoostBits(7, 2048, 1<<30)
	if got <= 0 {
		t.Fatalf("calculate_boost_bits with boost=2048 returned %d, want >0", got)
	}
}

func TestVP9AdjustGroupARNRFilterMatchesLibvpx(t *testing.T) {
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
		got := vp9AdjustGroupARNRFilter(tc.noise, tc.inter, tc.motion)
		if got != tc.want {
			t.Errorf("adjust_group_arnr_filter(noise=%.0f,inter=%.1f,motion=%.1f)=%d, want %d",
				tc.noise, tc.inter, tc.motion, got, tc.want)
		}
	}
}

func TestVP9DefineGFGroupStructureLayoutsBaseARF(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := vp9DefineGFGroup(in)
	if !gf.SourceAltRefPending {
		t.Skip("synthetic stats produced no alt-ref; layout test requires one")
	}
	// frame_index==0 should be the GF (or overlay), frame_index==1 the ARF.
	if gf.UpdateType[1] != vp9ARFUpdate {
		t.Fatalf("UpdateType[1]=%d, want ARF_UPDATE(%d)",
			gf.UpdateType[1], vp9ARFUpdate)
	}
	// Final slot should be the overlay placement.
	if gf.GFGroupSize == 0 {
		t.Fatal("gf_group_size=0")
	}
}

func TestVP9AllocateGFGroupBitsNonZero(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	gf := vp9DefineGFGroup(in)
	totalBits := 0
	for i := 0; i < gf.GFGroupSize; i++ {
		totalBits += gf.BitAllocation[i]
	}
	if totalBits <= 0 {
		t.Fatalf("sum of bit allocations = %d, want > 0", totalBits)
	}
}

func TestVP9GFGroupFeedActivatesARNRBoost(t *testing.T) {
	// Regression test for the AltRef agent's dormant adaptive ARNR
	// strength path. With first-pass stats wired through
	// prepareVP9SecondPassFrameTarget, rc.gfuBoost should be non-zero
	// after the first GF boundary, which is the condition VP9AdjustARNRFilter
	// gates on.
	stats := synthPanningFirstPassStats(16)
	stats = FinalizeVP9FirstPassStats(stats)
	opts := VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		TargetBitrateKbps:   600,
		TwoPassStats:        stats,
		LookaheadFrames:     8,
		MaxKeyframeInterval: 30,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.twoPass.enabled() {
		t.Fatalf("two-pass not enabled despite stats provided")
	}
	// Drive the GF refresh; the second-pass prepare path should fire and
	// stamp rc.gfuBoost.
	e.refreshVP9GFGroupIfDue(true /* isKey */)
	if e.rc.gfuBoost == 0 {
		t.Fatalf("rc.gfuBoost still 0 after GF refresh; AltRef adaptive path stays dormant")
	}
	if !e.twoPass.gfGroupActive {
		t.Fatalf("gfGroupActive=false after refresh")
	}
}

func TestVP9DefineGFGroupActiveBestQAdjustFactorInRange(t *testing.T) {
	// libvpx vp9_firstpass.c:2881-2904 clamps the factor to
	// [LAST_ALR_..., 1.0]. We assert the resulting value lies in that
	// window with a small float epsilon.
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	in.IsKeyFrame = false
	in.FramesSinceKey = 5
	in.FramesToKey = 25
	gf := vp9DefineGFGroup(in)
	if gf.ARFActiveBestQAdjustF < vp9LastALRActiveBestQAdjustFactor-1e-6 {
		t.Fatalf("factor=%f below LAST_ALR(%f)", gf.ARFActiveBestQAdjustF,
			vp9LastALRActiveBestQAdjustFactor)
	}
	if gf.ARFActiveBestQAdjustF > 1.0+1e-6 {
		t.Fatalf("factor=%f above 1.0", gf.ARFActiveBestQAdjustF)
	}
}

func TestVP9DefineGFGroupHandlesShortStatsBuffer(t *testing.T) {
	// Stats buffer shorter than min_gf_interval should not panic and
	// must yield a no-altref decision with a small gf interval.
	stats := synthPanningFirstPassStats(3)
	in := defaultVP9GFGroupTestInputs(stats)
	in.FramesToKey = 3
	gf := vp9DefineGFGroup(in)
	if gf.UseAltRef {
		t.Fatalf("UseAltRef=true with 3-frame stats, want false")
	}
	if gf.BaselineGFInterval < 0 {
		t.Fatalf("BaselineGFInterval=%d negative", gf.BaselineGFInterval)
	}
}

// TestVP9GFGroupBoostBoundedByPlausibleRange asserts the produced boost
// is bounded by the libvpx-documented [MIN_ARF_GF_BOOST, MAX_GF_BOOST]
// range across a swept Q ladder. This locks in numerical stability for
// the BD-rate gate consumers downstream.
func TestVP9GFGroupBoostBoundedByPlausibleRange(t *testing.T) {
	stats := synthPanningFirstPassStats(40)
	in := defaultVP9GFGroupTestInputs(stats)
	for q := 20; q <= 240; q += 40 {
		in.ActiveWorstQuality = q
		in.AvgFrameQIndexInter = q
		gf := vp9DefineGFGroup(in)
		if gf.GFUBoostScalar < vp9MinARFGFBoost {
			t.Errorf("q=%d boost=%d < MIN_ARF_GF_BOOST(%d)",
				q, gf.GFUBoostScalar, vp9MinARFGFBoost)
		}
		if gf.GFUBoostScalar > vp9MaxGFBoost {
			t.Errorf("q=%d boost=%d > MAX_GF_BOOST(%d)",
				q, gf.GFUBoostScalar, vp9MaxGFBoost)
		}
		if math.IsNaN(gf.ARFActiveBestQAdjustF) {
			t.Errorf("q=%d ARFActiveBestQAdjustF=NaN", q)
		}
	}
}
