package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestFastInterMotionModeRateKeepsPickInterNewMVWeight(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	refRate := 17
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, defaultInterFrameSignBias())

	got := e.fastInterMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 1, refRate)
	want := vp8enc.BoolBitCost(63, 1) +
		refRate +
		vp8enc.InterPredictionModeRate(vp8common.NewMV, counts) +
		vp8enc.MotionVectorBitCost(mode.MV, above.MV, &vp8tables.DefaultMVContext, vp8enc.FastNewMVBitCostWeight)
	if got != want {
		t.Fatalf("fast NEWMV mode rate = %d, want pickinter weight-128 rate %d", got, want)
	}
	if rdRate := e.interMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 1, refRate); got == rdRate {
		t.Fatalf("fast NEWMV mode rate = RD rate %d, want separate pickinter weight", rdRate)
	}
}

func TestEncoderInterMotionModeRateUsesAltRefSignBias(t *testing.T) {
	e := &VP8Encoder{
		refProbIntra:       63,
		refProbLast:        128,
		refProbGolden:      128,
		sourceAltRefActive: true,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: -24}}
	refRate := 23

	signBias := e.interFrameSignBias()
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, signBias)
	vectorCost := vp8enc.InterMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 2, &vp8tables.DefaultMVContext, nil, nil, vp8enc.RDNewMVBitCostWeight, signBias)
	wantVectorCost := vp8enc.MotionVectorBitCost(mode.MV, vp8enc.MotionVector{Col: -16}, &vp8tables.DefaultMVContext, vp8enc.RDNewMVBitCostWeight)
	if vectorCost != wantVectorCost {
		t.Fatalf("sign-biased NEWMV vector cost = %d, want cost against inverted best ref MV %d", vectorCost, wantVectorCost)
	}

	want := vp8enc.BoolBitCost(63, 1) +
		refRate +
		vp8enc.InterPredictionModeRate(vp8common.NewMV, counts) +
		vectorCost
	if got := e.interMotionModeRateWithReferenceRate(&mode, &above, nil, nil, 0, 0, 1, 2, refRate); got != want {
		t.Fatalf("sign-biased inter mode rate = %d, want %d", got, want)
	}
}

func TestEncoderInterReferenceMotionPredictorsUseAltRefSignBias(t *testing.T) {
	e := &VP8Encoder{sourceAltRefActive: true}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}

	nearest, near := e.interAnalysisReferenceMotionPredictors(vp8common.AltRefFrame, &above, nil, nil, 0, 0, 1, 2)
	if nearest != (vp8enc.MotionVector{Col: -16}) || !near.IsZero() {
		t.Fatalf("ALTREF predictors = %+v/%+v, want inverted nearest col -16 and zero near", nearest, near)
	}
}

func TestInterMotionModeRateChargesReferenceModeAndVector(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, probSkipFalse: 200}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 24}}
	counts := vp8enc.InterFrameModeCounts(&above, nil, nil, mode.RefFrame, defaultInterFrameSignBias())
	want := vp8enc.BoolBitCost(63, 1) +
		e.interReferenceFrameRate(vp8common.GoldenFrame) +
		vp8enc.InterPredictionModeRate(vp8common.NewMV, counts) +
		vp8enc.InterMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1, &vp8tables.DefaultMVContext, nil, nil, vp8enc.RDNewMVBitCostWeight, defaultInterFrameSignBias())

	if got := e.interMotionModeRate(&mode, &above, nil, nil, 0, 0, 1, 1); got != want {
		t.Fatalf("inter mode rate = %d, want %d", got, want)
	}
	if got := vp8enc.InterMacroblockSkipRate(128, false); got != vp8enc.BoolBitCost(128, 0) {
		t.Fatalf("coded skip rate = %d, want prob-128 false cost", got)
	}
	if got := vp8enc.InterMacroblockSkipRate(128, true); got != vp8enc.BoolBitCost(128, 1) {
		t.Fatalf("skipped rate = %d, want prob-128 true cost", got)
	}
	if got := e.interMacroblockSkipRate(false); got != vp8enc.BoolBitCost(200, 0) {
		t.Fatalf("live coded skip rate = %d, want prob-200 false cost", got)
	}
	if got := e.interMacroblockSkipRate(true); got != vp8enc.BoolBitCost(200, 1) {
		t.Fatalf("live skipped rate = %d, want prob-200 true cost", got)
	}
	if got, want := e.interIntraMacroblockModeRate(), vp8enc.BoolBitCost(200, 0)+vp8enc.BoolBitCost(63, 0); got != want {
		t.Fatalf("inter-intra mode rate = %d, want skip plus intra-reference rate %d", got, want)
	}
}
