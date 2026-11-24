package govpx

import "testing"

func TestFirstInterFrameRDProbsResetAfterKeyFrame(t *testing.T) {
	e := &VP8Encoder{}
	e.updateRefFrameProbsFromKeyFrame()
	if !e.refProbUseDefaultOnNextInterRD {
		t.Fatal("key frame did not arm default ref-prob reset for next inter RD pass")
	}
	e.resetRefFrameProbsToDefaultInterRD()
	e.applyLibvpxRdRefFrameProbRefreshAdjustments(false)
	if got, want := e.refProbIntra, uint8(63); got != want {
		t.Fatalf("prob_intra after first-inter reset = %d, want %d", got, want)
	}
	if got, want := e.refProbLast, uint8(214); got != want {
		t.Fatalf("prob_last after first-inter refresh adjustment = %d, want %d", got, want)
	}
	if got, want := e.refProbGolden, uint8(255); got != want {
		t.Fatalf("prob_gf after first-inter refresh adjustment = %d, want %d", got, want)
	}
}
