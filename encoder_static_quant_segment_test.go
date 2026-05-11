package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestStaticInterRDEncodeBreakoutUsesChromaGate(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 129, 90, 170)
	quant := testMacroblockQuant(80)

	if !staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want uniform low-luma residual skipped")
	}

	src.U[0] = 110
	if staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want chroma SSE to prevent skip")
	}
}

func TestStaticInterEncodeBreakoutUsesPickinterChromaGate(t *testing.T) {
	ref := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testMacroblockQuant(80)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	_, lumaSSE := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, 0, 0, mode.MV)

	src.U[0] = 92
	if staticInterRDEncodeBreakout(sourceImageFromPublic(src), &ref.Img, 0, 0, &quant, 1) {
		t.Fatalf("RD static breakout = true, want pickinter encode_breakout chroma gate to reject")
	}
	if staticInterFastEncodeBreakout(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, &quant, 1, lumaSSE) {
		t.Fatalf("fast static breakout = true, want pickinter encode_breakout chroma gate to reject")
	}
}

func TestSelectFastInterFrameModeDecisionStopsOnStaticEncodeBreakout(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.opts.StaticThreshold = 1
	e.refProbIntra = 1
	e.refProbLast = 1
	e.refProbGolden = 1
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 128, 90, 170)
	golden := testVP8Frame(t, 16, 16, 30, 90, 170)
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img, RefRateSet: true, RefRate: 1 << 20},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img, RefRateSet: true, RefRate: 0},
	}
	quant := testRegularMacroblockQuant(t, 20)

	decision, ok := e.selectFastInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 20, 0, nil, nil, nil, &quant, false)
	if !ok {
		t.Fatalf("fast mode decision returned ok=false")
	}
	if decision.useIntra || decision.interMode.RefFrame != vp8common.LastFrame || decision.interMode.Mode != vp8common.ZeroMV {
		t.Fatalf("decision = %+v, want LAST/ZEROMV static breakout candidate", decision)
	}
	if !decision.interMode.MBSkipCoeff {
		t.Fatalf("fast decision MBSkipCoeff = false, want candidate-level static breakout skip")
	}
	if e.interModeTestHitCounts[libvpxThrDC] != 0 {
		t.Fatalf("DC mode hit count = %d, want fast loop to break after static breakout", e.interModeTestHitCounts[libvpxThrDC])
	}
}

func TestBuildReconstructingInterFrameCoefficientsUsesStaticEncodeBreakout(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	src.Y[0] = 160

	noBreakout := newSizedTestEncoder(t, 16, 16)
	if err := noBreakout.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	fillBenchmarkVP8Image(&noBreakout.lastRef.Img, 128, 90, 170)
	noBreakout.lastRef.ExtendBorders()
	noBreakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	noBreakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if _, err := noBreakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, noBreakoutModes, noBreakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("no-breakout inter reconstruction returned error: %v", err)
	}
	if noBreakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&noBreakoutCoeffs[0]) == 0 {
		t.Fatalf("no-breakout mode skip=%t coeff sum=%d, want coded residual", noBreakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&noBreakoutCoeffs[0]))
	}

	breakout := newSizedTestEncoder(t, 16, 16)
	if err := breakout.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	breakout.opts.StaticThreshold = 7000
	fillBenchmarkVP8Image(&breakout.lastRef.Img, 128, 90, 170)
	breakout.lastRef.ExtendBorders()
	breakoutModes := make([]vp8enc.InterFrameMacroblockMode, 1)
	breakoutCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if _, err := breakout.buildReconstructingInterFrameCoefficients(sourceImageFromPublic(src), 20, breakoutModes, breakoutCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("breakout inter reconstruction returned error: %v", err)
	}
	if !breakoutModes[0].MBSkipCoeff || macroblockCoeffAbsSum(&breakoutCoeffs[0]) != 0 {
		t.Fatalf("breakout mode skip=%t coeff sum=%d, want forced skip", breakoutModes[0].MBSkipCoeff, macroblockCoeffAbsSum(&breakoutCoeffs[0]))
	}
}

func TestMacroblockCoefficientTokenRateChargesNonZeroResiduals(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var zero vp8enc.MacroblockCoefficients
	zeroRate := macroblockCoefficientTokenRate(&probs, false, &zero)

	nonzero := zero
	nonzero.QCoeff[24][0] = 2
	nonzero.SetBlockEOB(24, 1)
	nonzero.QCoeff[0][1] = -1
	nonzero.SetBlockEOB(0, 2)
	nonzero.QCoeff[16][0] = 1
	nonzero.SetBlockEOB(16, 1)
	nonzeroRate := macroblockCoefficientTokenRate(&probs, false, &nonzero)

	if zeroRate <= 0 {
		t.Fatalf("zero residual token rate = %d, want positive EOB signalling cost", zeroRate)
	}
	if nonzeroRate <= zeroRate {
		t.Fatalf("nonzero residual token rate = %d, zero = %d, want higher rate", nonzeroRate, zeroRate)
	}

	clearMacroblockCoefficients(&nonzero)
	if clearedRate := macroblockCoefficientTokenRate(&probs, false, &nonzero); clearedRate != zeroRate {
		t.Fatalf("cleared residual rate = %d, want zero residual rate %d", clearedRate, zeroRate)
	}
}

func TestOptimizeQuantizedBlockDropsTrailingCoefficientWhenRateWins(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 1 || qcoeff[1] != 0 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockUsesProvidedCoefficientProbs(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 9
	qcoeff[1] = 1

	defaultQ := qcoeff
	defaultEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &defaultQ, 2)
	if defaultEOB != 1 || defaultQ[1] != 0 {
		t.Fatalf("default optimized eob/qcoeff = %d/%d, want trailing coefficient dropped", defaultEOB, defaultQ[1])
	}

	liveProbs := vp8tables.DefaultCoefProbs
	liveProbs[0][1][0][0] = 1
	liveProbs[0][1][0][1] = 1
	liveProbs[0][1][0][2] = 255
	nextBand := vp8tables.CoefBandsTable[2]
	nextCtx := vp8tables.PrevTokenClass[vp8tables.OneToken]
	liveProbs[0][nextBand][nextCtx][0] = 255

	liveQ := qcoeff
	liveEOB := optimizeQuantizedBlock(&liveProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &liveQ, 2)
	if liveEOB != 2 || liveQ[1] != 1 {
		t.Fatalf("live-prob optimized eob/qcoeff = %d/%d, want coefficient preserved", liveEOB, liveQ[1])
	}
}

func TestOptimizeQuantizedBlockUsesElidedPostZeroTokenCost(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	rc1 := int(vp8tables.DefaultZigZag1D[1])
	rc2 := int(vp8tables.DefaultZigZag1D[2])
	coeff[rc1] = 9
	qcoeff[rc1] = 1
	coeff[rc2] = 10
	qcoeff[rc2] = 1

	probs := vp8tables.DefaultCoefProbs
	band := int(vp8tables.CoefBandsTable[2])
	// Make the root non-EOB bit expensive. libvpx's token_costs elides that
	// bit after a ZERO_TOKEN in this band; the old raw tree cost keeps q1.
	probs[0][band][0][0] = 255

	eob := optimizeQuantizedBlock(&probs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 3)

	if eob != 3 || qcoeff[rc1] != 0 || qcoeff[rc2] != 1 {
		t.Fatalf("optimized eob/q1/q2 = %d/%d/%d, want post-zero coefficient kept with leading coeff dropped", eob, qcoeff[rc1], qcoeff[rc2])
	}
}

func TestOptimizeQuantizedBlockKeepsCoefficientWhenDistortionDominates(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 100
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 100
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 2 || qcoeff[1] != 1 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want coefficient preserved", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockKeepsUndershootCoefficient(t *testing.T) {
	// Undershoot |x|*dq=10 < |c|=11: the libvpx Viterbi only considers the
	// shift-toward-zero shortcut for overshoots inside one quant step, so the
	// trailing coefficient must stay even though the greedy optimizer would
	// drop it.
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[1] = 11
	qcoeff[1] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 2 || qcoeff[1] != 1 {
		t.Fatalf("optimized eob/qcoeff = %d/%d, want libvpx Viterbi to keep undershoot coefficient", eob, qcoeff[1])
	}
}

func TestOptimizeQuantizedBlockShortensTrailingZerosWithInteriorRetained(t *testing.T) {
	// First non-zero overshoots inside one quant step (Viterbi keeps it via the
	// shortcut), trailing non-zero overshoots and is rate-dominated, so the
	// trellis drops only the trailing coefficient and pulls EOB back.
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var coeff [16]int16
	var qcoeff [16]int16
	rc1 := int(vp8tables.DefaultZigZag1D[1])
	rc2 := int(vp8tables.DefaultZigZag1D[2])
	coeff[rc1] = 60
	qcoeff[rc1] = 6
	coeff[rc2] = 9
	qcoeff[rc2] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 3)

	if eob != 2 || qcoeff[rc1] != 6 || qcoeff[rc2] != 0 {
		t.Fatalf("trellis output eob/q1/q2 = %d/%d/%d, want trailing dropped while interior retained", eob, qcoeff[rc1], qcoeff[rc2])
	}
}

func TestQuantizeBlockWithZbinUsesZeroRunBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	boostedRC := int(vp8tables.DefaultZigZag1D[7])
	coeff[boostedRC] = 75

	eob := quantizeBlockWithZbin(&coeff, &quant, 0, 0, &qcoeff, &dqcoeff)

	if eob != 0 || qcoeff[boostedRC] != 0 || dqcoeff[boostedRC] != 0 {
		t.Fatalf("boosted coefficient eob/q/dq = %d/%d/%d, want suppressed", eob, qcoeff[boostedRC], dqcoeff[boostedRC])
	}

	coeff = [16]int16{}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	earlyRC := int(vp8tables.DefaultZigZag1D[1])
	coeff[earlyRC] = 80
	eob = quantizeBlockWithZbin(&coeff, &quant, 0, 0, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[earlyRC] == 0 || dqcoeff[earlyRC] == 0 {
		t.Fatalf("early coefficient eob/q/dq = %d/%d/%d, want quantized", eob, qcoeff[earlyRC], dqcoeff[earlyRC])
	}
}

func TestQuantizeBlockWithZbinUsesModeBoost(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 66

	if eob := quantizeBlockWithZbin(&coeff, &quant, 0, 0, &qcoeff, &dqcoeff); eob != 2 || qcoeff[rc] == 0 {
		t.Fatalf("unboosted eob/q = %d/%d, want coefficient quantized", eob, qcoeff[rc])
	}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	if eob := quantizeBlockWithZbin(&coeff, &quant, 0, lastFrameZeroMVZbinBoost, &qcoeff, &dqcoeff); eob != 0 || qcoeff[rc] != 0 {
		t.Fatalf("boosted eob/q = %d/%d, want coefficient suppressed", eob, qcoeff[rc])
	}
}

func TestQuantizeBlockWithZbinUsesOverQuant(t *testing.T) {
	quant := testRegularBlockQuant(80, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	zbinOverQuant := 128
	extra := (int(quant.Dequant[1]) * zbinOverQuant) >> 7
	coeff[rc] = int16(int(quant.Zbin[rc]) + int(quant.ZbinBoost[0]) + extra/2)

	if eob := quantizeBlockWithZbin(&coeff, &quant, 0, 0, &qcoeff, &dqcoeff); eob != 2 || qcoeff[rc] == 0 {
		t.Fatalf("unboosted eob/q = %d/%d, want coefficient quantized", eob, qcoeff[rc])
	}
	qcoeff = [16]int16{}
	dqcoeff = [16]int16{}
	if eob := quantizeBlockWithZbin(&coeff, &quant, zbinOverQuant, 0, &qcoeff, &dqcoeff); eob != 0 || qcoeff[rc] != 0 || dqcoeff[rc] != 0 {
		t.Fatalf("over-quant eob/q/dq = %d/%d/%d, want coefficient suppressed", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockUpdatesDequantizedCoefficients(t *testing.T) {
	quant := testRegularBlockQuant(127, 10)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	// Overshoot inside one quant step: |x|*dq=10 > |c|=9 < |c|+dq=19, so the
	// libvpx Viterbi trellis explores the shift-toward-zero shortcut.
	coeff[rc] = 9

	eob := quantizeOptimizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 1 || qcoeff[rc] != 0 || dqcoeff[rc] != 0 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want trailing coefficient dropped and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeOptimizedBlockKeepsDequantizedCoefficient(t *testing.T) {
	quant := testRegularBlockQuant(4, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 100

	eob := quantizeOptimizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, 0, 0, false, &coeff, &quant, &qcoeff, &dqcoeff)

	if eob != 2 || qcoeff[rc] != 1 || dqcoeff[rc] != 100 {
		t.Fatalf("optimized eob/q/dq = %d/%d/%d, want coefficient kept and dequantized", eob, qcoeff[rc], dqcoeff[rc])
	}
}

func TestQuantizeEncodedBlockHonorsOptimizeGate(t *testing.T) {
	quant := testRegularBlockQuant(127, 10)
	var coeff [16]int16
	var optimizedQ [16]int16
	var optimizedDQ [16]int16
	var plainQ [16]int16
	var plainDQ [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	// Overshoot inside one quant step (|x|*dq=10 > |c|=9 < |c|+dq=19): the
	// optimize path's libvpx Viterbi shortcut picks x=0; the plain path keeps
	// the unoptimized x=1.
	coeff[rc] = 9

	optimizedEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, false, true, &coeff, &quant, &optimizedQ, &optimizedDQ)
	plainEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, false, false, &coeff, &quant, &plainQ, &plainDQ)

	if optimizedEOB != 1 || optimizedQ[rc] != 0 || optimizedDQ[rc] != 0 {
		t.Fatalf("optimized encoding eob/q/dq = %d/%d/%d, want dropped coefficient", optimizedEOB, optimizedQ[rc], optimizedDQ[rc])
	}
	if plainEOB != 2 || plainQ[rc] != 1 || plainDQ[rc] != 10 {
		t.Fatalf("plain encoding eob/q/dq = %d/%d/%d, want unoptimized quantized coefficient", plainEOB, plainQ[rc], plainDQ[rc])
	}
}

func TestQuantizeEncodedBlockUsesFastQuantWhenSpeedFeatureRequestsIt(t *testing.T) {
	quant := testRegularBlockQuant(4, 100)
	var coeff [16]int16
	var regularQ [16]int16
	var regularDQ [16]int16
	var fastQ [16]int16
	var fastDQ [16]int16
	rc := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc] = 64

	regularEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, false, false, &coeff, &quant, &regularQ, &regularDQ)
	fastEOB := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, 0, false, true, false, &coeff, &quant, &fastQ, &fastDQ)

	if regularEOB != 0 || regularQ[rc] != 0 || regularDQ[rc] != 0 {
		t.Fatalf("regular encoding eob/q/dq = %d/%d/%d, want zbin-suppressed coefficient", regularEOB, regularQ[rc], regularDQ[rc])
	}
	if fastEOB != 2 || fastQ[rc] != 1 || fastDQ[rc] != 100 {
		t.Fatalf("fast encoding eob/q/dq = %d/%d/%d, want fast-quantized coefficient", fastEOB, fastQ[rc], fastDQ[rc])
	}
}

func TestResetLibvpxSmallSecondOrderCoefficientsClearsTinyY2(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var qcoeff [16]int16
	var dqcoeff [16]int16
	qcoeff[0] = 3
	dqcoeff[0] = 30

	eob := resetLibvpxSmallSecondOrderCoefficients(&quant, &qcoeff, &dqcoeff, 1)

	if eob != 0 || qcoeff[0] != 0 || dqcoeff[0] != 0 {
		t.Fatalf("small Y2 reset = eob:%d q:%d dq:%d, want cleared", eob, qcoeff[0], dqcoeff[0])
	}
}

func TestResetLibvpxSmallSecondOrderCoefficientsKeepsVisibleY2(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 10
	}
	var qcoeff [16]int16
	var dqcoeff [16]int16
	qcoeff[0] = 4
	dqcoeff[0] = 40

	eob := resetLibvpxSmallSecondOrderCoefficients(&quant, &qcoeff, &dqcoeff, 1)

	if eob != 1 || qcoeff[0] != 4 || dqcoeff[0] != 40 {
		t.Fatalf("visible Y2 reset = eob:%d q:%d dq:%d, want preserved", eob, qcoeff[0], dqcoeff[0])
	}
}

func TestResetLibvpxSmallSecondOrderCoefficientsHonorsDequantGuard(t *testing.T) {
	var quant vp8enc.BlockQuant
	for i := range quant.Dequant {
		quant.Dequant[i] = 35
	}
	var qcoeff [16]int16
	qcoeff[0] = 1

	eob := resetLibvpxSmallSecondOrderCoefficients(&quant, &qcoeff, nil, 1)

	if eob != 1 || qcoeff[0] != 1 {
		t.Fatalf("guarded Y2 reset = eob:%d q:%d, want preserved when dequant >= 35", eob, qcoeff[0])
	}
}

func testRegularBlockQuant(qIndex int, dequantValue int16) vp8enc.BlockQuant {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = dequantValue
	}
	var quant vp8enc.BlockQuant
	vp8enc.InitRegularBlockQuant(qIndex, &dequant, &quant)
	return quant
}

func TestInterZbinModeBoostMatchesLibvpxClasses(t *testing.T) {
	tests := []struct {
		name string
		mode vp8enc.InterFrameMacroblockMode
		want int
	}{
		{name: "last zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}, want: lastFrameZeroMVZbinBoost},
		{name: "golden zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "alt zeromv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.ZeroMV}, want: goldenAltZeroMVZbinBoost},
		{name: "newmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV}, want: nonZeroInterModeZbinBoost},
		{name: "splitmv", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV}, want: splitInterModeZbinBoost},
		{name: "intra", mode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}, want: intraInterFrameZbinBoost},
	}
	for _, tt := range tests {
		if got := interZbinModeBoost(&tt.mode); got != tt.want {
			t.Fatalf("%s boost = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestEncoderSegmentQIndex(t *testing.T) {
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][1] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = -10
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 10 {
		t.Fatalf("delta segment q = %d, want 10", got)
	}
	if got := encoderSegmentQIndex(4, segmentation, 1); got != vp8common.MinQ {
		t.Fatalf("clamped delta segment q = %d, want MinQ", got)
	}
	segmentation.AbsDelta = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = 63
	if got := encoderSegmentQIndex(20, segmentation, 1); got != 63 {
		t.Fatalf("absolute segment q = %d, want 63", got)
	}
	if got := encoderSegmentQIndex(20, segmentation, 2); got != 20 {
		t.Fatalf("disabled segment q = %d, want base q", got)
	}
}

func TestBuildReconstructingKeyFrameCoefficientsWithSegmentationQuantizesPerSegment(t *testing.T) {
	lowEncoder := newSizedTestEncoder(t, 32, 16)
	highEncoder := newSizedTestEncoder(t, 32, 16)
	src := segmentedQuantizationTestImage()
	lowModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	highModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	lowCoeffs := make([]vp8enc.MacroblockCoefficients, 2)
	highCoeffs := make([]vp8enc.MacroblockCoefficients, 2)

	lowSegmentation := testAltQSegmentation(1, 0)
	highSegmentation := testAltQSegmentation(1, 100)
	if _, err := lowEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, lowSegmentation, true, lowModes, lowCoeffs, 1, 2); err != nil {
		t.Fatalf("low-q keyframe reconstruction returned error: %v", err)
	}
	if _, err := highEncoder.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, highSegmentation, true, highModes, highCoeffs, 1, 2); err != nil {
		t.Fatalf("high-q keyframe reconstruction returned error: %v", err)
	}

	if lowModes[0].SegmentID != 0 || lowModes[1].SegmentID != 1 || highModes[0].SegmentID != 0 || highModes[1].SegmentID != 1 {
		t.Fatalf("segment IDs low=%d/%d high=%d/%d, want preserved 0/1", lowModes[0].SegmentID, lowModes[1].SegmentID, highModes[0].SegmentID, highModes[1].SegmentID)
	}
	if highEncoder.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", highEncoder.reconstructModes[1].SegmentID)
	}
	if highEncoder.dequants[0].Y1[0] == highEncoder.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", highEncoder.dequants[0].Y1[0], highEncoder.dequants[1].Y1[0])
	}

	lowSum := macroblockCoeffAbsSum(&lowCoeffs[1])
	highSum := macroblockCoeffAbsSum(&highCoeffs[1])
	if lowSum <= highSum {
		t.Fatalf("segment 1 coefficient abs sum low/high = %d/%d, want high segment q to quantize harder", lowSum, highSum)
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationPreservesSegmentDequants(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	fillBenchmarkVP8Image(&e.lastRef.Img, 128, 128, 128)
	for row := range 16 {
		for col := 16; col < 32; col++ {
			if (row+col)&1 == 0 {
				e.lastRef.Img.Y[row*e.lastRef.Img.YStride+col] = 32
			} else {
				e.lastRef.Img.Y[row*e.lastRef.Img.YStride+col] = 224
			}
		}
	}
	e.lastRef.ExtendBorders()
	src := segmentedQuantizationTestImage()
	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 1}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 2)
	segmentation := testAltQSegmentation(1, 100)

	if _, err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 0, segmentation, true, modes, coeffs, 1, 2, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}

	if modes[0].SegmentID != 0 || modes[1].SegmentID != 1 {
		t.Fatalf("segment IDs = %d/%d, want preserved 0/1", modes[0].SegmentID, modes[1].SegmentID)
	}
	if e.reconstructModes[1].SegmentID != 1 {
		t.Fatalf("decoder reconstruct segment ID = %d, want 1", e.reconstructModes[1].SegmentID)
	}
	if e.dequants[0].Y1[0] == e.dequants[1].Y1[0] {
		t.Fatalf("segment dequant Y1 DC = %d/%d, want segment-specific dequant", e.dequants[0].Y1[0], e.dequants[1].Y1[0])
	}
	if got := macroblockCoeffAbsSum(&coeffs[1]); got == 0 {
		t.Fatalf("segment 1 coefficient abs sum = 0, want residual coefficients")
	}
}

func TestBuildReconstructingInterFrameCoefficientsWithSegmentationClearsCyclicSegmentForNonLastZero(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	copyFrameImage(&e.goldenRef.Img, &golden.Img)
	e.goldenRef.ExtendBorders()
	fillBenchmarkVP8Image(&e.lastRef.Img, 220, 90, 170)
	e.lastRef.ExtendBorders()

	modes := []vp8enc.InterFrameMacroblockMode{{SegmentID: staticSegmentID}}
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)
	segmentation := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID] = -10

	_, err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(
		sourceImageFromPublic(src), 20, segmentation, true, modes, coeffs, 1, 1,
		EncodeNoReferenceLast|EncodeNoReferenceAltRef,
	)
	if err != nil {
		t.Fatalf("inter reconstruction returned error: %v", err)
	}
	if modes[0].RefFrame != vp8common.GoldenFrame || modes[0].Mode != vp8common.ZeroMV {
		t.Fatalf("mode = %+v, want GOLDEN/ZEROMV setup", modes[0])
	}
	if modes[0].SegmentID != 0 || e.reconstructModes[0].SegmentID != 0 {
		t.Fatalf("segment IDs = mode:%d reconstruct:%d, want cleared cyclic segment for non-LAST/ZEROMV", modes[0].SegmentID, e.reconstructModes[0].SegmentID)
	}
}

func TestBuildReconstructingCoefficientsWithSegmentationRejectsInvalidSegmentID(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	src := segmentedQuantizationTestImage()
	segmentation := testAltQSegmentation(1, 63)
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	keyCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if _, err := e.buildReconstructingKeyFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, keyModes, keyCoeffs, 1, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("keyframe invalid segment error = %v, want ErrInvalidConfig", err)
	}

	interModes := []vp8enc.InterFrameMacroblockMode{{SegmentID: vp8common.MaxMBSegments}}
	interCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	if _, err := e.buildReconstructingInterFrameCoefficientsWithSegmentation(sourceImageFromPublic(src), 20, segmentation, true, interModes, interCoeffs, 1, 1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("inter invalid segment error = %v, want ErrInvalidConfig", err)
	}
}
