package govpx

import "testing"

func TestSelectFastInterFrameModeDecisionRaisesBPredThresholdWhenEstimateFails(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.Deadline = DeadlineBestQuality
	e.opts.CpuUsed = 0
	e.rc.currentQuantizer = 40
	fillBenchmarkVP8Image(&e.analysis.Img, 0, 128, 128)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 0, 128, 128)
	for row := range 16 {
		for col := range 16 {
			if (row+col)&1 == 0 {
				src.Y[row*src.YStride+col] = 255
			}
		}
	}

	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()

	before := e.interRDThreshMult[libvpxThrBPred]
	_, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), nil, 0, 0, 0, 1, 1, 40, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if got := e.interModeTestHitCounts[libvpxThrBPred]; got != 1 {
		t.Fatalf("B_PRED hit count = %d, want 1", got)
	}
	if !e.interRDThreshTouched[libvpxThrBPred] {
		t.Fatalf("B_PRED threshold was not touched")
	}
	if got, want := e.interRDThreshMult[libvpxThrBPred], before+4; got != want {
		t.Fatalf("B_PRED threshold multiplier = %d, want raised to %d after failed estimate", got, want)
	}
}
