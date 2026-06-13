package encoder

import "testing"

func TestPrepareKeyFrameGroupSeedsBoostBudgetAndFramesToKey(t *testing.T) {
	stats := synthPanningFirstPassStats(6)
	in := KeyFrameGroupInputs{
		Stats:                stats,
		StartShowIdx:         0,
		KeyFrameFrequency:    128,
		AutoKey:              true,
		MinGFInterval:        MinGFInterval,
		BitsLeft:             140000,
		NormalizedScoreLeft:  6,
		MaxFrameBandwidth:    500000,
		MeanModScore:         1,
		AvErr:                10000,
		MBRows:               4,
		CurrentVideoFrame:    0,
		AvgFrameQIndexInter:  57,
		FrameWidth:           64,
		FrameHeight:          64,
		BoostParams:          DefaultARFBoostParams(4),
		TwoPassVBRMaxSection: DefaultVBRMaxSectionPct,
		TwoPassVBRBiasPct:    DefaultTwoPassVBRBiasPct,
		TwoPassVBRMinSection: 0,
	}

	r := PrepareKeyFrameGroup(in)
	if r.FramesToKey != len(stats) {
		t.Fatalf("frames_to_key=%d, want %d", r.FramesToKey, len(stats))
	}
	if r.KeyFrameBoost < MinKFTotalBoost || r.KeyFrameBoost > MaxKFTotalBoost {
		t.Fatalf("kf_boost=%d outside [%d,%d]",
			r.KeyFrameBoost, MinKFTotalBoost, MaxKFTotalBoost)
	}
	if r.KeyFrameBits <= 0 || r.KFGroupBitsLeft < 0 ||
		r.KFGroupErrorLeft <= 0 {
		t.Fatalf("keyframe group result = %+v, want populated budget/error", r)
	}
}

func TestTwoPassWorstQualityUsesSectionComplexity(t *testing.T) {
	q := TwoPassWorstQuality(TwoPassWorstQualityInputs{
		SectionError:           10000,
		InactiveZone:           0,
		SectionNoise:           SectionNoiseDefault,
		SectionTargetBandwidth: 23000,
		BestQuality:            16,
		WorstQuality:           224,
		AvgFrameBandwidth:      23000,
		MinFrameBandwidth:      FrameOverhead,
		MaxFrameBandwidth:      460000,
		Macroblocks:            64,
		Speed:                  4,
		BPMFactor:              1,
		Width:                  64,
		Height:                 64,
	})
	if q <= 16 || q >= 224 {
		t.Fatalf("two-pass worst quality q=%d, want interior active worst", q)
	}
}
