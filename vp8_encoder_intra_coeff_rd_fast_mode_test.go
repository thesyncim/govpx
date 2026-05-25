package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestEstimateFastInterModeScoreUsesLibvpxPickInterDistortion(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	ref := testVP8Frame(t, 16, 16, 50, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	qIndex := testInterSearchQIndex

	got, ok := e.estimateFastInterModeScore(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, qIndex)
	if !ok {
		t.Fatalf("estimateFastInterModeScore returned ok=false")
	}
	variance, sse := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, 0, 0, mode.MV)
	if variance != 0 || sse == 0 {
		t.Fatalf("variance/sse = %d/%d, want flat luma offset with zero variance and nonzero SSE", variance, sse)
	}
	rate := e.interMotionModeRate(&mode, nil, nil, nil, 0, 0, 1, 1)
	want := vp8enc.RDModeScore(qIndex, rate, variance)
	if got != want {
		t.Fatalf("fast inter score = %d, want rate plus luma variance %d", got, want)
	}
	if sseScore := vp8enc.RDModeScore(qIndex, rate, sse); got == sseScore {
		t.Fatalf("fast inter score used SSE %d, want libvpx variance distortion", sse)
	}
}

func TestSelectFastInterFrameModeDecisionCanChooseInterleavedIntra(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*29 + col*53) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if !decision.useIntra || decision.intraMode.Mode != vp8common.DCPred || decision.intraMode.RefFrame != vp8common.IntraFrame {
		t.Fatalf("decision = %+v, want intra DC from libvpx interleaved mode loop", decision)
	}
}

// TestSelectFastInterFrameModeDecisionPicksLibvpxUVMode verifies that
// selectFastInterFrameModeDecision mirrors libvpx pickinter.c
// vp8_pick_inter_mode lines 1301-1303: when the winning mode is intra
// (mode <= B_PRED), pick_intra_mbuv_mode runs and sets mbmi.uv_mode to
// the predictor with lowest pred_error against the source. Earlier,
// govpx hardcoded UVMode=DC_PRED which caused 128x128 frame 1 chroma
// reconstruction divergence at the col-7 right-edge B_PRED MBs (R14-E).
// The fixture shapes neighbors so V_PRED has near-zero pred error
// against the source, which is exactly the case libvpx's
// pick_intra_mbuv_mode would resolve to V_PRED.
func TestSelectFastInterFrameModeDecisionPicksLibvpxUVMode(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(32, 32, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 128, 128)
	// Shape chroma predictor references so V_PRED matches the source
	// for the lower 8x8 chroma block: above-row[8..15]=40 makes V_PRED
	// fill the column with 40, matching src.U[8..15][8..15]=40.
	// pick_intra_mbuv_mode picks the mode with minimum SSE.
	for i := range 8 {
		e.analysis.Img.U[7*e.analysis.Img.UStride+8+i] = 40
		e.analysis.Img.V[7*e.analysis.Img.VStride+8+i] = 40
		e.analysis.Img.U[(8+i)*e.analysis.Img.UStride+7] = 220
		e.analysis.Img.V[(8+i)*e.analysis.Img.VStride+7] = 220
	}
	e.analysis.ExtendBorders()

	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	for row := 8; row < 16; row++ {
		for col := 8; col < 16; col++ {
			src.U[row*src.UStride+col] = 40
			src.V[row*src.VStride+col] = 40
		}
	}
	last := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := range 32 {
		for col := range 32 {
			last.Img.Y[row*last.Img.YStride+col] = byte((row*29 + col*53 + 17) & 255)
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 1, 1, 2, 2, testInterSearchQIndex, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if !decision.useIntra {
		t.Fatalf("decision = %+v, want intra mode to exercise fast pickinter UV policy", decision)
	}
	// V_PRED above-row=40 perfectly predicts source rows 8..15 col 8..15
	// (=40). DC_PRED, H_PRED, and TM_PRED all incur larger SSE.
	// pick_intra_mbuv_mode picks V_PRED.
	if decision.intraMode.UVMode != vp8common.VPred {
		t.Fatalf("fast intra UV mode = %v, want libvpx pickinter V_PRED (lowest SSE)", decision.intraMode.UVMode)
	}
}

func TestSelectFastInterFrameModeDecisionUsesLibvpxReferenceSlots(t *testing.T) {
	e := &VP8Encoder{
		opts:          EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8},
		refProbIntra:  63,
		refProbLast:   128,
		refProbGolden: 128,
		probSkipFalse: 128,
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	if err := e.analysis.Resize(16, 16, 32, 32); err != nil {
		t.Fatalf("analysis resize returned error: %v", err)
	}
	fillBenchmarkVP8Image(&e.analysis.Img, 127, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 127, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte((17 + row*43 + col*71 + row*col*11) & 255)
		}
	}
	last := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[row*last.Img.YStride+col] = byte((231 - row*17 - col*31) & 255)
		}
	}
	last.ExtendBorders()
	golden := testVP8Frame(t, 16, 16, 0, 90, 170)
	for row := range 16 {
		copy(golden.Img.Y[row*golden.Img.YStride:], src.Y[row*src.YStride:row*src.YStride+16])
	}
	golden.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
	}

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, testInterSearchQIndex, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.ref.Frame != vp8common.GoldenFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want GOLDEN/ZEROMV from libvpx slot-2 loop entry", decision)
	}
}

func TestSelectFastInterFrameModeDecisionUsesThresholdState(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.Deadline = DeadlineRealtime
	e.opts.CpuUsed = 8
	fillBenchmarkVP8Image(&e.analysis.Img, 96, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	last := testVP8Frame(t, 16, 16, 96, 90, 170)
	for row := range 16 {
		for col := range 16 {
			y := byte((11 + row*37 + col*19 + row*col*5) & 255)
			src.Y[row*src.YStride+col] = y
			last.Img.Y[row*last.Img.YStride+col] = y
		}
	}
	last.ExtendBorders()
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	e.resetInterRDThresholdMultipliers()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	e.beginInterRDModeDecisionMacroblock()

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 40, 0, nil, nil, nil, nil, false)

	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.ref.Frame != vp8common.LastFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want LAST/ZEROMV on matching reference", decision)
	}
	if got := e.interModeTestHitCounts[libvpxThrZero1]; got != 1 {
		t.Fatalf("ZERO1 hit count = %d, want 1", got)
	}
	if !e.interRDThreshTouched[libvpxThrZero1] {
		t.Fatalf("ZERO1 threshold was not touched")
	}
	if got := e.interRDThreshMult[libvpxThrZero1]; got >= libvpxRDThreshMultStart {
		t.Fatalf("ZERO1 threshold multiplier = %d, want below start after improvement", got)
	}
}
