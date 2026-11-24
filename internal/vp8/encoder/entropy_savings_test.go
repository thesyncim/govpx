package encoder

import (
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestReferenceFrameCostsMatchLibvpxFormula(t *testing.T) {
	intra, last, golden, alt := ReferenceFrameCosts(64, 85, 128)
	wantIntra := vp8tables.ProbCost[64]
	wantLast := vp8tables.ProbCost[255-64] + vp8tables.ProbCost[85]
	wantGolden := vp8tables.ProbCost[255-64] + vp8tables.ProbCost[255-85] + vp8tables.ProbCost[128]
	wantAlt := vp8tables.ProbCost[255-64] + vp8tables.ProbCost[255-85] + vp8tables.ProbCost[255-128]
	if intra != wantIntra || last != wantLast || golden != wantGolden || alt != wantAlt {
		t.Fatalf("ref_frame_cost = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
			intra, last, golden, alt, wantIntra, wantLast, wantGolden, wantAlt)
	}
}

func TestReferenceFrameCostsClampProbabilities(t *testing.T) {
	intra, _, _, _ := ReferenceFrameCosts(-5, 1000, 200)
	if intra != vp8tables.ProbCost[0] {
		t.Fatalf("negative prob_intra not clamped: cost[INTRA] = %d, want %d",
			intra, vp8tables.ProbCost[0])
	}
}

func TestReferenceFrameEntropySavingsReturnsZeroForKeyFrames(t *testing.T) {
	got := ReferenceFrameEntropySavings(true, 100, 200, 50, 10, 64, 85, 128)
	if got != 0 {
		t.Fatalf("key-frame entropy savings = %d, want 0", got)
	}
}

func TestReferenceFrameEntropySavingsMatchesDerivedProbabilities(t *testing.T) {
	got := ReferenceFrameEntropySavings(false, 100, 200, 50, 10, 64, 85, 128)
	rfInter := 200 + 50 + 10
	newIntra := 100 * 255 / (100 + rfInter)
	if newIntra == 0 {
		newIntra = 1
	}
	newLast := 200 * 255 / rfInter
	newGarf := 50 * 255 / (50 + 10)
	ni, nl, ng, na := ReferenceFrameCosts(newIntra, newLast, newGarf)
	oi, ol, og, oa := ReferenceFrameCosts(64, 85, 128)
	newTotal := 100*ni + 200*nl + 50*ng + 10*na
	oldTotal := 100*oi + 200*ol + 50*og + 10*oa
	want := (oldTotal - newTotal) / 256
	if got != want {
		t.Fatalf("entropy savings = %d, want %d (re-computed)", got, want)
	}
}

func TestDecideKeyFrameUsesUnconditionalIntraThresholds(t *testing.T) {
	if !DecideKeyFrame(100, 50, true) {
		t.Fatalf("100%% intra > 50%%+2 should fire")
	}
	if !DecideKeyFrame(96, 90, true) {
		t.Fatalf("96 >= 90+5 should fire")
	}
	if DecideKeyFrame(100, 98, true) {
		t.Fatalf("100>98+2 false; 100<=100, should not fire")
	}
	if DecideKeyFrame(95, 80, true) {
		t.Fatalf("95 not >95, should not fire on second rule")
	}
}

func TestDecideKeyFrameSuppressesSecondTierDuringGoldenRefresh(t *testing.T) {
	if !DecideKeyFrame(70, 30, false) {
		t.Fatalf("70 > 30*2 should fire when no GF refresh")
	}
	if DecideKeyFrame(70, 30, true) {
		t.Fatalf("70 > 30*2 with GF refresh should NOT fire")
	}
}

func TestDecideKeyFrameUsesSecondTierIntraThresholds(t *testing.T) {
	if !DecideKeyFrame(70, 30, false) {
		t.Fatalf("rule 1: 70>60 && 70>60 should fire")
	}
	if !DecideKeyFrame(80, 50, false) {
		t.Fatalf("rule 2: 80>75 && 80>75 should fire")
	}
	if !DecideKeyFrame(92, 80, false) {
		t.Fatalf("rule 3: 92>90 && 92>90 should fire")
	}
	if DecideKeyFrame(60, 30, false) {
		t.Fatalf("60 not >60 should not fire")
	}
}

func TestReferenceFrameEntropySavingsFloorsRareIntraProbability(t *testing.T) {
	got := ReferenceFrameEntropySavings(false, 1, 1000, 0, 0, 200, 64, 128)
	rfInter := 1000
	newIntra := 1 * 255 / (1 + rfInter)
	if newIntra == 0 {
		newIntra = 1
	}
	newLast := 1000 * 255 / rfInter
	newGarf := 128
	ni, nl, ng, na := ReferenceFrameCosts(newIntra, newLast, newGarf)
	oi, ol, og, oa := ReferenceFrameCosts(200, 64, 128)
	newTotal := 1*ni + 1000*nl + 0*ng + 0*na
	oldTotal := 1*oi + 1000*ol + 0*og + 0*oa
	want := (oldTotal - newTotal) / 256
	if got != want {
		t.Fatalf("all-intra entropy savings = %d, want %d", got, want)
	}
}
