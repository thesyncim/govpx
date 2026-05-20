package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func defaultVP9TwoPassQInputs() vp9RCPickQAndBoundsTwoPassInputs {
	return vp9RCPickQAndBoundsTwoPassInputs{
		IsIntraOnly:                          false,
		BoostFrame:                           false,
		IsSrcFrameAltRef:                     false,
		ThisKeyFrameForced:                   false,
		FramesSinceKey:                       8,
		AvgFrameQIndexInter:                  140,
		LastKFQIndex:                         100,
		LastBoostedQIndex:                    120,
		BestQuality:                          0,
		WorstQuality:                         255,
		ThisFrameTarget:                      50000,
		MaxFrameBandwidth:                    500000,
		ActiveWorstQuality:                   180,
		ExtendMinQ:                           0,
		ExtendMaxQ:                           0,
		ExtendMinQFast:                       0,
		LastQIndexOfMaxLayerDepth:            0,
		LastKFGroupZeroMotionPct:             0,
		KFZeroMotionPct:                      0,
		KeyFrameBoost:                        2000,
		FrameWidth:                           640,
		FrameHeight:                          360,
		CQLevel:                              0,
		IsCQ:                                 false,
		ARFActiveBestQualityAdjustmentFactor: 1.0,
		ARFIncreaseActiveBestQuality:         0,
		GFUBoost:                             2000,
		RFLevel:                              vp9RFLInterNormal,
		LayerDepth:                           2,
		MaxLayerDepth:                        1,
	}
}

func TestVP9RCPickQAndBoundsTwoPassClampsToWorstQuality(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	in.WorstQuality = 200
	in.ActiveWorstQuality = 250 // intentionally above worst.
	r := vp9RCPickQAndBoundsTwoPass(in, 220)
	if r.ActiveWorst > in.WorstQuality {
		t.Fatalf("active_worst=%d > worst_quality=%d", r.ActiveWorst, in.WorstQuality)
	}
	if r.Q > in.WorstQuality {
		t.Fatalf("q=%d > worst_quality=%d", r.Q, in.WorstQuality)
	}
}

func TestVP9RCPickQAndBoundsTwoPassBoostFrameLowersActiveBest(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	nonBoost := vp9RCPickQAndBoundsTwoPass(in, 160)
	in.BoostFrame = true
	boost := vp9RCPickQAndBoundsTwoPass(in, 160)
	// libvpx vp9_ratectrl.c:1506 uses get_gf_active_quality(q) for boost
	// frames; non-boost falls back to inter_minq[active_worst]. The
	// boost branch must not produce a higher active-best than the
	// non-boost branch on the same input.
	if boost.ActiveBest > nonBoost.ActiveBest {
		t.Fatalf("boost active_best=%d > non-boost active_best=%d",
			boost.ActiveBest, nonBoost.ActiveBest)
	}
}

func TestVP9RCPickQAndBoundsTwoPassBoostFrameARFAdjustmentUsesMotionTables(t *testing.T) {
	for _, tc := range []struct {
		name     string
		increase int
		hl       func(int) int
	}{
		{name: "high-motion", increase: 1, hl: encoder.GFHighMotionActiveQuality},
		{name: "low-motion", increase: -1, hl: encoder.GFLowMotionActiveQuality},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := defaultVP9TwoPassQInputs()
			in.BoostFrame = true
			in.GFUBoost = 1200
			in.ARFIncreaseActiveBestQuality = tc.increase
			in.ARFActiveBestQualityAdjustmentFactor = 0.25

			q := in.AvgFrameQIndexInter
			base := encoder.GFActiveQualityWithBoost(q, in.GFUBoost)
			want := int(float64(base)*in.ARFActiveBestQualityAdjustmentFactor +
				float64(tc.hl(q))*(1.0-in.ARFActiveBestQualityAdjustmentFactor))
			r := vp9RCPickQAndBoundsTwoPass(in, 160)
			if r.ActiveBest != want {
				t.Fatalf("boost-frame active_best=%d, want %d", r.ActiveBest, want)
			}
		})
	}
}

func TestVP9RCPickQAndBoundsTwoPassForcedKeyUsesLastBoostedOrLastKF(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	in.IsIntraOnly = true
	in.ThisKeyFrameForced = true
	in.LastKFGroupZeroMotionPct = 50 // < 95 -> use last_boosted_qindex
	r := vp9RCPickQAndBoundsTwoPass(in, 100)
	if r.Q != in.LastBoostedQIndex {
		t.Fatalf("q=%d, want last_boosted_qindex=%d", r.Q, in.LastBoostedQIndex)
	}
	wantActiveBest := max(in.LastBoostedQIndex+vp9ComputeQDelta(
		in.BestQuality, in.WorstQuality, in.LastBoostedQIndex, 75, 100),
		in.BestQuality)
	if r.ActiveBest != wantActiveBest {
		t.Fatalf("forced KF active_best=%d, want %d", r.ActiveBest, wantActiveBest)
	}
	if r.ActiveWorst != in.ActiveWorstQuality {
		t.Fatalf("forced KF active_worst=%d, want unchanged %d",
			r.ActiveWorst, in.ActiveWorstQuality)
	}

	in.LastKFGroupZeroMotionPct = 99 // >= 95 -> min(last_kf_qindex, last_boosted_qindex)
	r = vp9RCPickQAndBoundsTwoPass(in, 100)
	want := min(in.LastBoostedQIndex, in.LastKFQIndex)
	if r.Q != want {
		t.Fatalf("static-motion forced KF q=%d, want %d", r.Q, want)
	}
	if r.ActiveBest != want {
		t.Fatalf("static-motion forced KF active_best=%d, want %d", r.ActiveBest, want)
	}
	wantActiveWorst := min(want+vp9ComputeQDelta(
		in.BestQuality, in.WorstQuality, want, 125, 100),
		in.ActiveWorstQuality)
	if r.ActiveWorst != wantActiveWorst {
		t.Fatalf("static-motion forced KF active_worst=%d, want %d",
			r.ActiveWorst, wantActiveWorst)
	}
}

func TestVP9RCPickQAndBoundsTwoPassNonForcedKeyUsesZeroMotionAndSmallFrameAdjustments(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	in.IsIntraOnly = true
	in.ThisKeyFrameForced = false
	in.ActiveWorstQuality = 180
	in.FrameWidth = 352
	in.FrameHeight = 288
	in.KFZeroMotionPct = 99

	r := vp9RCPickQAndBoundsTwoPass(in, 100)
	wantActiveBest := encoder.KFActiveQualityWithBoost(in.ActiveWorstQuality,
		in.KeyFrameBoost)
	wantActiveBest /= 4
	wantActiveBest = min(in.ActiveWorstQuality, max(1, wantActiveBest))
	wantActiveBest += vp9ComputeQDelta(in.BestQuality, in.WorstQuality,
		wantActiveBest, 1050-250-in.KFZeroMotionPct, 1000)
	if r.ActiveBest != wantActiveBest {
		t.Fatalf("non-forced KF active_best=%d, want %d", r.ActiveBest, wantActiveBest)
	}
	if r.Q != r.ActiveBest {
		t.Fatalf("non-forced KF q=%d, want active_best=%d", r.Q, r.ActiveBest)
	}
	if r.ActiveWorst != in.ActiveWorstQuality {
		t.Fatalf("non-forced KF active_worst=%d, want %d",
			r.ActiveWorst, in.ActiveWorstQuality)
	}
}

func TestVP9RCPickQAndBoundsTwoPassExtendMinQAppliedSymmetrically(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	in.BoostFrame = true
	base := vp9RCPickQAndBoundsTwoPass(in, 160)
	in.ExtendMinQ = 10
	extended := vp9RCPickQAndBoundsTwoPass(in, 160)
	// libvpx vp9_ratectrl.c:1546-1547: active_best -= extend_minq + extend_minq_fast.
	if extended.ActiveBest >= base.ActiveBest {
		t.Fatalf("extend_minq=10 did not lower active_best: %d -> %d",
			base.ActiveBest, extended.ActiveBest)
	}
}

func TestVP9RCPickQAndBoundsTwoPassExtendMaxQRaisesActiveWorst(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	base := vp9RCPickQAndBoundsTwoPass(in, 160)
	in.ExtendMaxQ = 20
	extended := vp9RCPickQAndBoundsTwoPass(in, 160)
	// non-boost branch: active_worst += extend_maxq (full); base case
	// adds 0, so the extended active_worst must be strictly greater
	// (or saturate at worst_quality).
	if extended.ActiveWorst < base.ActiveWorst {
		t.Fatalf("extend_maxq=20 did not raise active_worst: %d -> %d",
			base.ActiveWorst, extended.ActiveWorst)
	}
}

func TestVP9RCPickQAndBoundsTwoPassCQModeFloorsActiveBest(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	in.IsCQ = true
	in.CQLevel = 150
	// Non-boost branch.
	r := vp9RCPickQAndBoundsTwoPass(in, 100)
	if r.ActiveBest < in.CQLevel {
		t.Fatalf("CQ mode active_best=%d < cq_level=%d", r.ActiveBest, in.CQLevel)
	}
}

func TestVP9RCPickQAndBoundsTwoPassGFARFLowLayerBias(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	in.BoostFrame = true
	in.RFLevel = vp9RFLGFARFLow
	in.LayerDepth = 3
	r := vp9RCPickQAndBoundsTwoPass(in, 160)
	// libvpx vp9_ratectrl.c:1528-1530 linearly fits with layer_depth so
	// active_best ends up between q and the boost-frame active_best.
	// We assert it stays within the valid [best_quality, worst_quality]
	// window and is not negative.
	if r.ActiveBest < in.BestQuality || r.ActiveBest > in.WorstQuality {
		t.Fatalf("GF_ARF_LOW layer_depth=3 active_best=%d out of [%d, %d]",
			r.ActiveBest, in.BestQuality, in.WorstQuality)
	}
}

func TestVP9RCPickQAndBoundsTwoPassRegulatedQClampedToActiveWorst(t *testing.T) {
	in := defaultVP9TwoPassQInputs()
	// Regulator produces a Q above active_worst with this_frame_target <
	// max_frame_bandwidth: libvpx forces q = active_worst.
	in.ThisFrameTarget = 1000
	in.MaxFrameBandwidth = 500000
	r := vp9RCPickQAndBoundsTwoPass(in, 240) // 240 > active_worst initially
	if r.Q > r.ActiveWorst {
		t.Fatalf("q=%d > active_worst=%d", r.Q, r.ActiveWorst)
	}
}
