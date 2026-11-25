package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
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

func TestInterModeForRDLoopEntryAllowsZeroNewMVOnFlatMatch(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	fillBenchmarkVP8Image(&e.analysis.Img, 72, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, 16, 16, 72, 90, 170)
	ref := interAnalysisReference{Frame: vp8common.LastFrame, Img: &last.Img}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}

	mode, ok := e.interModeForRDLoopEntry(sourceImageFromPublic(src), ref, 0, vp8common.NewMV, 0, 0, 1, 1, testInterSearchQIndex, nil, nil, nil, &newMVCandidates, nil)
	if !ok {
		t.Fatalf("RD NEWMV loop entry rejected zero MV on flat matching frame")
	}
	if mode.Mode != vp8common.NewMV || mode.RefFrame != vp8common.LastFrame || !mode.MV.IsZero() {
		t.Fatalf("RD NEWMV loop entry mode = %+v, want LAST/NEWMV with zero MV", mode)
	}
}

func TestSelectRDInterFrameMotionVectorAllowsSubpixelRefinementWithBestRefMVCost(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	bestRefMV := vp8enc.MotionVector{Row: 2, Col: 2}

	mv, cost := selectRDInterFrameMotionVectorWithSearchStart(sourceImageFromPublic(src), &ref.Img, 1, 1, 3, 3, bestRefMV, testInterSearchQIndex, defaultInterAnalysisSearchConfig(), interFrameSearchStart{}, &vp8tables.DefaultMVContext)

	if mv != bestRefMV {
		t.Fatalf("RD NEWMV search MV = %+v, want accepted subpel refinement %+v", mv, bestRefMV)
	}
	want := interMotionSearchErrorVectorCost(mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if cost != want {
		t.Fatalf("RD NEWMV search cost = %d, want best_ref_mv anchored subpel cost %d", cost, want)
	}
	if zeroAnchor := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost == zeroAnchor {
		t.Fatalf("RD NEWMV search cost = zero-anchor cost %d, want best_ref_mv anchor", zeroAnchor)
	}
}
