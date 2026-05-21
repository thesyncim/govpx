package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestSelectRDInterFrameModeDecisionStopsOnStaticEncodeBreakout(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.opts.StaticThreshold = 1

	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	last := testVP8Frame(t, 16, 16, 128, 90, 170)
	refs := [...]interAnalysisReference{{
		Frame:      vp8common.LastFrame,
		Img:        &last.Img,
		RefRateSet: true,
		RefRate:    1 << 20,
	}}
	quant := testRegularMacroblockQuant(t, 20)

	decision, ok := e.selectRDInterFrameModeDecision(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, 20, vp8enc.StaticSegmentID, nil, nil, nil, nil, nil, &quant, false)

	if !ok {
		t.Fatalf("RD mode decision returned ok=false")
	}
	if !decision.cyclicRefreshEligible() || decision.interMode.SegmentID != vp8enc.StaticSegmentID {
		t.Fatalf("decision = %+v, want static breakout to stop on LAST/ZEROMV with cyclic segment", decision)
	}
}

func TestEstimateInterResidualRDScoreUsesLibvpxStaticBreakoutRate(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.opts.StaticThreshold = 1
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	ref := testVP8Frame(t, 16, 16, 128, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)

	score, rdLoopSkip, ok := e.estimateInterResidualRDScoreWithReferenceRateAndSkip(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)

	if !ok || !rdLoopSkip {
		t.Fatalf("static breakout score ok=%t rdLoopSkip=%t, want true/true", ok, rdLoopSkip)
	}
	if want := vp8enc.RDModeScoreWithZbin(20, 0, 500, 0); score != want {
		t.Fatalf("static breakout RD score = %d, want libvpx rate-500 score %d", score, want)
	}

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)
	if !ok || !acct.rdLoopSkip || !acct.mbSkipCoeff {
		t.Fatalf("static breakout accounting ok=%t rdLoopSkip=%t mbSkipCoeff=%t, want true/true/true", ok, acct.rdLoopSkip, acct.mbSkipCoeff)
	}
	if acct.rate2 != 500 || acct.distortion2 != 0 || acct.rd != score {
		t.Fatalf("static breakout accounting rate/dist/rd = %d/%d/%d, want 500/0/%d", acct.rate2, acct.distortion2, acct.rd, score)
	}

	rdCtx := interResidualRDContext{
		src:           sourceImageFromPublic(src),
		ref:           &ref.Img,
		mbRow:         0,
		mbCol:         0,
		mode:          &mode,
		quant:         &quant,
		qIndex:        20,
		segmentID:     0,
		denoiseActive: true,
	}
	acct, ok = e.estimateInterResidualRDAccountingWithModeContext(&rdCtx)
	if !ok || !acct.rdLoopSkip || !acct.mbSkipCoeff {
		t.Fatalf("denoiser static breakout accounting ok=%t rdLoopSkip=%t mbSkipCoeff=%t, want true/true/true", ok, acct.rdLoopSkip, acct.mbSkipCoeff)
	}
}

func TestEstimateInterResidualRDAccountingUsesTransformDomainDistortion(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	for i := range src.Y {
		src.Y[i] = byte(64 + (i*17)%96)
	}
	for i := range src.U {
		src.U[i] = byte(80 + (i*11)%48)
		src.V[i] = byte(144 + (i*7)%48)
	}
	ref := testVP8Frame(t, 16, 16, 96, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)
	if !ok {
		t.Fatalf("estimateInterResidualRDAccounting returned ok=false")
	}
	var coeffs vp8enc.MacroblockCoefficients
	stats := buildPredictedMacroblockCoefficientsRD(&e.coefProbs, sourceImageFromPublic(src), 0, 0, &ref.Img, nil, nil, &quant, 20, e.rc.currentZbinOverQuant, interZbinModeBoost(&mode), false, false, e.libvpxUseFastQuant(), false, &coeffs)
	wantDistortion := stats.distortionY + stats.distortionUV
	if acct.distortion2 != wantDistortion || acct.distortionUV != stats.distortionUV {
		t.Fatalf("accounting distortion = %d uv=%d, want transform-domain %d uv=%d", acct.distortion2, acct.distortionUV, wantDistortion, stats.distortionUV)
	}
	if pixelSSE := macroblockImageSSE(sourceImageFromPublic(src), &ref.Img, 0, 0); acct.distortion2 == pixelSSE {
		t.Fatalf("accounting distortion = pixel SSE %d, want transform-domain distortion", pixelSSE)
	}
}

func BenchmarkEstimateInterResidualRDAccounting(b *testing.B) {
	e := newSizedTestEncoder(b, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		b.Fatalf("SetDeadline returned error: %v", err)
	}
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 96, 90, 170)
	for i := range src.Y {
		src.Y[i] = byte(64 + (i*17)%96)
	}
	for i := range src.U {
		src.U[i] = byte(80 + (i*11)%48)
		src.V[i] = byte(144 + (i*7)%48)
	}
	ref := testVP8Frame(b, 16, 16, 96, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(b, 20)
	source := sourceImageFromPublic(src)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acct, ok := e.estimateInterResidualRDAccounting(source, &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, 0)
		if !ok || acct.distortion2 == 0 {
			b.Fatalf("estimateInterResidualRDAccounting returned ok=%t acct=%+v", ok, acct)
		}
	}
}

func TestEstimateInterResidualRDAccountingReturnsLibvpxRate2AndYRD(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.refProbIntra = 63
	e.refProbLast = 128
	e.refProbGolden = 128
	e.probSkipFalse = 200
	e.modeProbs.MV = vp8tables.DefaultMVContext
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	for i := range src.Y {
		src.Y[i] = byte(32 + (i*13)%128)
	}
	ref := testVP8Frame(t, 16, 16, 96, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)
	refRate := 17

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, refRate)
	if !ok {
		t.Fatalf("estimateInterResidualRDAccounting returned ok=false")
	}
	if acct.mbSkipCoeff {
		t.Fatalf("mbSkipCoeff = true, want coded residual for rate2 accounting test")
	}
	modeRate := e.interMotionModeRateWithReferenceRate(&mode, nil, nil, nil, 0, 0, 1, 1, refRate)
	wantOtherCost := e.interMacroblockSkipRate(false)
	wantRate2 := modeRate + wantOtherCost + acct.rateY + acct.rateUV
	wantRefCost := vp8enc.BoolBitCost(e.refProbIntra, 1) + refRate
	if acct.rate2 != wantRate2 || acct.otherCost != wantOtherCost || acct.refCost != wantRefCost {
		t.Fatalf("rate2/other/ref = %d/%d/%d, want %d/%d/%d", acct.rate2, acct.otherCost, acct.refCost, wantRate2, wantOtherCost, wantRefCost)
	}
	if wantRD := vp8enc.RDModeScoreWithZbin(20, e.rc.currentZbinOverQuant, acct.rate2, acct.distortion2); acct.rd != wantRD {
		t.Fatalf("rd = %d, want %d", acct.rd, wantRD)
	}
	wantYRD := vp8enc.RDModeScoreWithZbin(20, e.rc.currentZbinOverQuant, acct.rate2-acct.rateUV-acct.otherCost-acct.refCost, acct.distortion2-acct.distortionUV)
	if acct.yrd != wantYRD {
		t.Fatalf("yrd = %d, want %d", acct.yrd, wantYRD)
	}
}

func TestEstimateInterResidualRDAccountingEmptyCoeffSkipBacksOutTokenRates(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.refProbIntra = 63
	e.probSkipFalse = 200
	e.opts.StaticThreshold = 0
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	ref := testVP8Frame(t, 16, 16, 128, 90, 170)
	mode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	quant := testRegularMacroblockQuant(t, 20)
	refRate := 23

	acct, ok := e.estimateInterResidualRDAccounting(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 1, &mode, nil, nil, nil, nil, nil, &quant, 20, 0, refRate)
	if !ok {
		t.Fatalf("estimateInterResidualRDAccounting returned ok=false")
	}
	if !acct.mbSkipCoeff || acct.rdLoopSkip {
		t.Fatalf("mbSkipCoeff/rdLoopSkip = %t/%t, want true/false", acct.mbSkipCoeff, acct.rdLoopSkip)
	}
	modeRate := e.interMotionModeRateWithReferenceRate(&mode, nil, nil, nil, 0, 0, 1, 1, refRate)
	wantRate2 := modeRate + e.interMacroblockSkipRate(true)
	if acct.rate2 != wantRate2 || acct.rateUV != 0 || acct.otherCost != e.interMacroblockSkipRate(true) {
		t.Fatalf("skip accounting rate2/rateUV/other = %d/%d/%d, want %d/0/%d", acct.rate2, acct.rateUV, acct.otherCost, wantRate2, e.interMacroblockSkipRate(true))
	}
	if wantRD := vp8enc.RDModeScoreWithZbin(20, e.rc.currentZbinOverQuant, wantRate2, acct.distortion2); acct.rd != wantRD {
		t.Fatalf("skip accounting rd = %d, want %d", acct.rd, wantRD)
	}
}

func TestAddInterResidualToAnalysisMacroblockMatchesFullReconstruct(t *testing.T) {
	cases := []struct {
		name string
		mode vp8enc.InterFrameMacroblockMode
	}{
		{
			name: "WholeMV",
			mode: vp8enc.InterFrameMacroblockMode{
				RefFrame: vp8common.LastFrame,
				Mode:     vp8common.ZeroMV,
				UVMode:   vp8common.DCPred,
			},
		},
		{
			name: "SplitMV",
			mode: vp8enc.InterFrameMacroblockMode{
				RefFrame: vp8common.LastFrame,
				Mode:     vp8common.SplitMV,
				UVMode:   vp8common.DCPred,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSizedTestEncoder(t, 32, 32)
			var decSeg vp8dec.SegmentationHeader
			vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: 20}, &decSeg, &e.dequantTables, &e.dequants)
			src := testImage(32, 32)
			for i := range src.Y {
				src.Y[i] = byte(32 + (i*13)%160)
			}
			for i := range src.U {
				src.U[i] = byte(48 + (i*11)%128)
				src.V[i] = byte(64 + (i*7)%128)
			}
			ref := testVP8Frame(t, 32, 32, 96, 90, 170)
			quant := testRegularMacroblockQuant(t, 20)
			is4x4 := vp8enc.InterFrameModeUses4x4Tokens(tc.mode.Mode)

			var decMode vp8dec.MacroblockMode
			vp8enc.ConvertInterFrameMode(&tc.mode, &decMode)
			predMode := decMode
			predMode.MBSkipCoeff = true
			var zeroTokens vp8dec.MacroblockTokens
			reuse := testVP8Frame(t, 32, 32, 0, 0, 0)
			if !reconstructInterAnalysisMacroblock(&reuse.Img, &ref.Img, 0, 0, &predMode, &zeroTokens, &e.dequants[0], &e.reconstructScratch) {
				t.Fatalf("predictor reconstruction returned false")
			}

			var coeffs vp8enc.MacroblockCoefficients
			stats := buildPredictedMacroblockCoefficientsRD(
				&vp8tables.DefaultCoefProbs, sourceImageFromPublic(src), 0, 0, &reuse.Img,
				nil, nil, &quant, 20, 0, interZbinModeBoost(&tc.mode),
				is4x4, false, false, false, &coeffs,
			)
			if stats.tteob == 0 {
				t.Fatalf("test fixture produced empty residual")
			}

			mode := tc.mode
			mode.MBSkipCoeff = false
			vp8enc.ConvertInterFrameMode(&mode, &decMode)
			var tokens vp8dec.MacroblockTokens
			vp8enc.ConvertMacroblockCoefficients(&coeffs, is4x4, &tokens)

			full := testVP8Frame(t, 32, 32, 0, 0, 0)
			if !reconstructInterAnalysisMacroblock(&full.Img, &ref.Img, 0, 0, &decMode, &tokens, &e.dequants[0], &e.reconstructScratch) {
				t.Fatalf("full reconstruction returned false")
			}
			if !addInterResidualToAnalysisMacroblock(&reuse.Img, 0, 0, &decMode, &tokens, &e.dequants[0], &e.reconstructScratch) {
				t.Fatalf("residual-only reconstruction returned false")
			}
			assertImagesEqual(t, "residual-only reconstruction", publicImageFromVP8(&full.Img), publicImageFromVP8(&reuse.Img))
		})
	}
}

func TestEstimateInterIntraModeRDScoreAddsLibvpxPenalty(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	result, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.DCPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	decMode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, 0, 0, &decMode, &e.reconstructScratch) {
		t.Fatalf("predictAnalysisMacroblock returned false")
	}
	yRate, yDist, _, _ := wholeBlockYTransformRD(sourceImageFromPublic(src), &e.analysis.Img, 0, 0, 0, 0, nil, nil, &quant, &e.coefProbs, false)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD mode=%v ok=false", uvMode)
	}
	rate := yRate + uvRate + intraYModeRate(false, vp8common.DCPred) + e.interIntraMacroblockModeRate()
	want := vp8enc.RDModeScoreWithZbin(20, 0, rate, yDist+uvDist) + vp8enc.InterIntraRDPenalty(20)
	if result.score != want {
		t.Fatalf("inter-intra RD score = %d, want %d with libvpx penalty", result.score, want)
	}
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	wantYRD := vp8enc.RDModeScoreWithZbin(20, 0, yRate+intraYModeRate(false, vp8common.DCPred)+uvModeRate, yDist)
	if result.yrd != wantYRD {
		t.Fatalf("inter-intra YRD = %d, want libvpx Y plus UV-mode RD %d", result.yrd, wantYRD)
	}
}

func TestEstimateInterIntraModeRDScoreUsesLiveInterIntraModeProbs(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.YMode = [vp8tables.YModeProbCount]uint8{250, 4, 220, 9}
	e.modeProbs.UVMode = [vp8tables.UVModeProbCount]uint8{245, 8, 230}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	result, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.DCPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	decMode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, 0, 0, &decMode, &e.reconstructScratch) {
		t.Fatalf("predictAnalysisMacroblock returned false")
	}
	yRate, yDist, _, _ := wholeBlockYTransformRD(sourceImageFromPublic(src), &e.analysis.Img, 0, 0, 0, 0, nil, nil, &quant, &e.coefProbs, false)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, e.modeProbs.UVMode[:], false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRDWithProbs mode=%v ok=false", uvMode)
	}
	liveYModeRate := e.interIntraYModeRate(vp8common.DCPred)
	if liveYModeRate == intraYModeRate(false, vp8common.DCPred) {
		t.Fatalf("live Y mode rate still matches default, test fixture is ineffective")
	}
	rate := yRate + uvRate + liveYModeRate + e.interIntraMacroblockModeRate()
	want := vp8enc.RDModeScoreWithZbin(20, 0, rate, yDist+uvDist) + vp8enc.InterIntraRDPenalty(20)
	if result.score != want {
		t.Fatalf("inter-intra RD score = %d, want %d from live Y/UV mode probabilities", result.score, want)
	}
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	wantYRD := vp8enc.RDModeScoreWithZbin(20, 0, yRate+liveYModeRate+uvModeRate, yDist)
	if result.yrd != wantYRD {
		t.Fatalf("inter-intra YRD = %d, want %d from live Y and UV-mode probabilities", result.yrd, wantYRD)
	}
}

func TestEstimateInterIntraBPredYRDExcludesUVTokensAndRefCosts(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	quant := testRegularMacroblockQuant(t, 20)

	result, ok := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, vp8common.BPred, maxInt(), nil, nil, &quant)
	if !ok {
		t.Fatalf("estimateInterIntraModeRDScore BPred returned ok=false")
	}

	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, maxInt(), &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestBPredLumaModeRD returned ok=false")
	}
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(sourceImageFromPublic(src), 20, 0, false, 0, 0, nil, nil, &quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, false)
	if !ok {
		t.Fatalf("predictBestIntraChromaModeRD mode=%v bModes=%v ok=false", uvMode, bModes)
	}
	yRate := bRate + intraYModeRate(false, vp8common.BPred)
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	wantYRD := vp8enc.RDModeScoreWithZbin(20, 0, yRate+uvModeRate, bDist)
	if result.yrd != wantYRD {
		t.Fatalf("BPred YRD = %d, want libvpx Y plus UV-mode RD %d", result.yrd, wantYRD)
	}
	rate := yRate + uvRate + e.interIntraMacroblockModeRate()
	want := vp8enc.RDModeScoreWithZbin(20, 0, rate, bDist+uvDist) + vp8enc.InterIntraRDPenalty(20)
	if result.score != want {
		t.Fatalf("BPred RD score = %d, want %d with UV/ref costs and penalty", result.score, want)
	}
}

func TestFastZeroMVLastRDAdjustmentMirrorsLibvpxLocalMotionBias(t *testing.T) {
	zero := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	moving := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	intra := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}

	if got := fastZeroMVLastRDAdjustment(0, 2, nil, &zero, nil); got != 80 {
		t.Fatalf("edge adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &moving, &intra); got != 90 {
		t.Fatalf("single local zero adjustment = %d, want 90", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &zero, &zero); got != 80 {
		t.Fatalf("three local zero adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, nil, &moving, &intra); got != 100 {
		t.Fatalf("moving adjustment = %d, want 100", got)
	}
}

func TestMBSplitPartitionRateMirrorsWriterBranches(t *testing.T) {
	tests := []struct {
		partition uint8
		want      int
	}{
		{partition: 3, want: vp8enc.BoolBitCost(vp8tables.MBSplitProbs[0], 0)},
		{partition: 2, want: vp8enc.BoolBitCost(vp8tables.MBSplitProbs[0], 1) + vp8enc.BoolBitCost(vp8tables.MBSplitProbs[1], 0)},
		{partition: 0, want: vp8enc.BoolBitCost(vp8tables.MBSplitProbs[0], 1) + vp8enc.BoolBitCost(vp8tables.MBSplitProbs[1], 1) + vp8enc.BoolBitCost(vp8tables.MBSplitProbs[2], 0)},
		{partition: 1, want: vp8enc.BoolBitCost(vp8tables.MBSplitProbs[0], 1) + vp8enc.BoolBitCost(vp8tables.MBSplitProbs[1], 1) + vp8enc.BoolBitCost(vp8tables.MBSplitProbs[2], 1)},
	}
	for _, tt := range tests {
		if got := mbSplitPartitionRate(tt.partition); got != tt.want {
			t.Fatalf("partition %d rate = %d, want %d", tt.partition, got, tt.want)
		}
	}
}

func TestSplitMotionModeVectorCostChargesPartitionAndNew4x4Weight(t *testing.T) {
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	fillInterFrameSplitSubset(&mode, 0, vp8enc.MotionVector{Col: 16})
	fillInterFrameSplitSubset(&mode, 1, vp8enc.MotionVector{Row: 16})
	fillInterFrameSplitSubset(&mode, 2, vp8enc.MotionVector{Col: -16})
	fillInterFrameSplitSubset(&mode, 3, vp8enc.MotionVector{Row: -16})

	mvProbs := vp8tables.DefaultMVContext
	best := vp8enc.MotionVector{Col: 8}
	want := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		target := mode.BlockMV[block]
		want += splitSubMotionLabelRate(vp8common.New4x4)
		delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
		want += vp8enc.MotionVectorBitCost(delta, vp8enc.MotionVector{}, &mvProbs, 102)
	}

	defaultCost := splitMotionModeVectorCost(&mode, nil, nil, best, &mvProbs)
	if defaultCost != want {
		t.Fatalf("split vector cost = %d, want partition + NEW4X4 weight-102 cost %d", defaultCost, want)
	}

	liveProbs := mvProbs
	liveProbs[1][0] = 1
	if liveCost := splitMotionModeVectorCost(&mode, nil, nil, best, &liveProbs); liveCost == defaultCost {
		t.Fatalf("live split vector cost = default cost %d, want MV probs to affect SPLITMV sub-vector cost", liveCost)
	}
}

func TestSplitMotionModeVectorCostUsesExplicitSubMVLabel(t *testing.T) {
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 0,
	}
	fillInterFrameSplitSubsetWithMode(&mode, 0, left.MV, vp8common.New4x4)
	fillInterFrameSplitSubsetWithMode(&mode, 1, left.MV, vp8common.Left4x4)
	mode.MV = mode.BlockMV[15]
	mvProbs := vp8tables.DefaultMVContext

	newCost := splitMotionModeVectorCost(&mode, &left, nil, vp8enc.MotionVector{}, &mvProbs)
	mode.BModes[0] = vp8common.Left4x4
	leftCost := splitMotionModeVectorCost(&mode, &left, nil, vp8enc.MotionVector{}, &mvProbs)

	if newCost <= leftCost {
		t.Fatalf("explicit NEW4X4 cost = %d, want greater than LEFT4X4 cost %d for same MV", newCost, leftCost)
	}
}

func TestSplitSubMotionLabelSearchCostUsesLibvpxDefaultSubMVRefProb(t *testing.T) {
	const qIndex = 127

	got := splitSubMotionLabelSearchCost(vp8common.Above4x4, qIndex)
	wantRate := splitSubMotionLabelCostWithProbs(vp8common.Above4x4, vp8enc.DefaultSubMVRefProbs)
	want := (wantRate*vp8enc.SADPerBit4(qIndex) + 128) >> 8
	if got != want {
		t.Fatalf("ABOVE4X4 search cost = %d, want libvpx default cost %d", got, want)
	}
	contextualRate := splitSubMotionLabelCostWithProbs(vp8common.Above4x4, vp8tables.SubMVRefProb3[4])
	contextual := (contextualRate*vp8enc.SADPerBit4(qIndex) + 128) >> 8
	if got == contextual {
		t.Fatalf("ABOVE4X4 search cost matched contextual bitstream cost %d; want libvpx RD default cost", got)
	}
}

func TestSelectInterFrameSplitSubsetMotionModeRanksLabelsByRD(t *testing.T) {
	leftMV := vp8enc.MotionVector{Col: 32}
	aboveMV := leftMV
	const qIndex = 20
	const leftDiff = 64
	const zeroDiff = 0

	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	ref := testVP8Frame(t, 32, 32, 255, 128, 128)
	for row := range 4 {
		for col := range 4 {
			ref.Img.Y[row*ref.Img.YStride+col] = byte(128 + zeroDiff)
			ref.Img.Y[row*ref.Img.YStride+col+4] = byte(128 + leftDiff)
		}
	}
	ref.ExtendBorders()

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV, Partition: 3}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV, Partition: 3}
	for block := range 16 {
		left.BlockMV[block] = leftMV
		above.BlockMV[block] = aboveMV
	}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))
	mv, bMode := selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold(
		sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height,
		vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, false, qIndex,
		&left, &above, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext, maxInt(),
	)
	if bMode != vp8common.Zero4x4 || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("split-label choice = %v/%+v, want ZERO4X4 by lower RDCOST distortion", bMode, mv)
	}
}

// TestInterReferenceFrameRateUsesLivePrevFrameProbs locks in libvpx parity for
// vp8_calc_ref_frame_costs: ref-frame selection bits are charged against the
// previous frame's prob_last_coded / prob_gf_coded, not a static 128 prior.
func TestInterReferenceFrameRateUsesLivePrevFrameProbs(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 50, refProbLast: 200, refProbGolden: 90}
	if got, want := e.interReferenceFrameRate(vp8common.LastFrame), vp8enc.BoolBitCost(200, 0); got != want {
		t.Fatalf("LAST rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.GoldenFrame), vp8enc.BoolBitCost(200, 1)+vp8enc.BoolBitCost(90, 0); got != want {
		t.Fatalf("GOLDEN rate = %d, want %d", got, want)
	}
	if got, want := e.interReferenceFrameRate(vp8common.AltRefFrame), vp8enc.BoolBitCost(200, 1)+vp8enc.BoolBitCost(90, 1); got != want {
		t.Fatalf("ALTREF rate = %d, want %d", got, want)
	}
}

func TestThreadedHelperRowsUseZeroReferenceFrameRate(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 200, refProbGolden: 90, probSkipFalse: 200}
	normalGolden := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 0)
	if got := e.interReferenceFrameRate(vp8common.GoldenFrame); got != normalGolden {
		t.Fatalf("normal helper-disabled GOLDEN rate = %d, want %d", got, normalGolden)
	}
	if got, want := e.interIntraMacroblockModeRate(), vp8enc.BoolBitCost(200, 0)+vp8enc.BoolBitCost(63, 0); got != want {
		t.Fatalf("normal helper-disabled intra MB rate = %d, want %d", got, want)
	}

	e.threadedHelperRowsActive = true
	if got := e.interIntraReferenceRate(); got != 0 {
		t.Fatalf("helper intra-reference rate = %d, want 0", got)
	}
	if got := e.interInterReferenceRate(12345); got != 0 {
		t.Fatalf("helper inter-reference rate = %d, want 0", got)
	}
	if got := e.interReferenceFrameRate(vp8common.GoldenFrame); got != 0 {
		t.Fatalf("helper GOLDEN rate = %d, want 0", got)
	}
	ref := interAnalysisReference{Frame: vp8common.GoldenFrame, RefRateSet: true, RefRate: 12345}
	if got := e.interReferenceFrameRateForReference(ref); got != 0 {
		t.Fatalf("helper explicit reference rate = %d, want 0", got)
	}
	if got, want := e.interIntraMacroblockModeRate(), e.interMacroblockSkipRate(false); got != want {
		t.Fatalf("helper intra MB rate = %d, want skip-only %d", got, want)
	}
}

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

func TestInterReferenceFrameRatesForFlagsMirrorLibvpxSingleReferenceSpecialCases(t *testing.T) {
	e := &VP8Encoder{refProbLast: 200, refProbGolden: 90}
	last, golden, alt := e.interReferenceFrameRatesForFlags(0)
	if want := vp8enc.BoolBitCost(200, 0); last != want {
		t.Fatalf("all-ref LAST rate = %d, want %d", last, want)
	}
	if want := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 0); golden != want {
		t.Fatalf("all-ref GOLDEN rate = %d, want %d", golden, want)
	}
	if want := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 1); alt != want {
		t.Fatalf("all-ref ALTREF rate = %d, want %d", alt, want)
	}

	last, _, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceGolden | EncodeNoReferenceAltRef)
	if want := vp8enc.BoolBitCost(255, 0); last != want {
		t.Fatalf("single-LAST rate = %d, want libvpx special-case %d", last, want)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer}
	_, golden, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceAltRef)
	if want := vp8enc.BoolBitCost(200, 1) + vp8enc.BoolBitCost(90, 0); golden != want {
		t.Fatalf("one-layer single-GOLDEN rate = %d, want non-temporal live cost %d", golden, want)
	}

	e.opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}
	_, golden, _ = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceAltRef)
	if want := vp8enc.BoolBitCost(1, 1) + vp8enc.BoolBitCost(255, 0); golden != want {
		t.Fatalf("temporal single-GOLDEN rate = %d, want libvpx special-case %d", golden, want)
	}
	_, _, alt = e.interReferenceFrameRatesForFlags(EncodeNoReferenceLast | EncodeNoReferenceGolden)
	if want := vp8enc.BoolBitCost(1, 1) + vp8enc.BoolBitCost(1, 1); alt != want {
		t.Fatalf("temporal single-ALTREF rate = %d, want libvpx special-case %d", alt, want)
	}
}

func TestInterAnalysisReferencesCarryLibvpxFlagSpecificReferenceRates(t *testing.T) {
	e := &VP8Encoder{refProbLast: 200, refProbGolden: 90}
	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(EncodeNoReferenceGolden|EncodeNoReferenceAltRef, &refs)
	if count != 1 || refs[0].Frame != vp8common.LastFrame || !refs[0].RefRateSet {
		t.Fatalf("single-LAST refs = count:%d ref:%+v, want one LAST with explicit rate", count, refs[0])
	}
	if want := vp8enc.BoolBitCost(255, 0); refs[0].RefRate != want {
		t.Fatalf("single-LAST carried rate = %d, want %d", refs[0].RefRate, want)
	}
}

func TestInterAnalysisReferencesPruneLibvpxAliasFlagsAfterKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	e.refreshKeyFrameReferencesFromAnalysis()
	e.frameCount = 1

	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(0, &refs)
	if count != 1 || refs[0].Frame != vp8common.LastFrame {
		t.Fatalf("post-key refs = count:%d first:%+v, want only LAST after libvpx alias pruning", count, refs[0])
	}
	if want := vp8enc.BoolBitCost(255, 0); refs[0].RefRate != want {
		t.Fatalf("post-key LAST rate = %d, want single-reference libvpx cost %d", refs[0].RefRate, want)
	}
	// Explicit NO_REF_* user masks route through libvpx's
	// vp8_use_as_reference path, which replaces ref_frame_flags with the
	// user-derived mask and bypasses the post-keyframe alias filter. When
	// LAST is explicitly masked, both GOLDEN and ALTREF remain available
	// even though the keyframe refresh seeded them with the LAST
	// reconstruction.
	if e.shouldEncodeKeyFrame(EncodeNoReferenceLast) {
		t.Fatalf("shouldEncodeKeyFrame with LAST disabled = true, want inter frame using user-selected aliased refs")
	}
	count = e.interAnalysisReferences(EncodeNoReferenceLast, &refs)
	if count != 2 || refs[0].Frame != vp8common.GoldenFrame || refs[1].Frame != vp8common.AltRefFrame {
		t.Fatalf("NoReferenceLast picker refs = count:%d refs:%+v, want GOLDEN and ALTREF user-selected aliases", count, refs[:count])
	}
	count = e.interAnalysisReferences(EncodeNoReferenceAltRef, &refs)
	if count != 2 || refs[0].Frame != vp8common.LastFrame || refs[1].Frame != vp8common.GoldenFrame {
		t.Fatalf("NoReferenceAltRef picker refs = count:%d refs:%+v, want LAST and GOLDEN user-selected aliases", count, refs[:count])
	}
}

func TestInterAnalysisReferencesKeepAltAfterInternalGoldenRefreshCopiesOldGF(t *testing.T) {
	e := newTestEncoder(t)
	fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
	e.analysis.ExtendBorders()
	e.refreshKeyFrameReferencesFromAnalysis()
	e.updateInterReferenceAliases(vp8enc.InterFrameStateConfig{
		RefreshLast:        true,
		RefreshGolden:      true,
		CopyBufferToAltRef: 2,
	})

	var refs [3]interAnalysisReference
	count := e.interAnalysisReferences(0, &refs)
	if count != 2 || refs[0].Frame != vp8common.LastFrame || refs[1].Frame != vp8common.AltRefFrame {
		t.Fatalf("post-GF-refresh refs = count:%d refs:%+v/%+v, want LAST and old-GF ALTREF", count, refs[0], refs[1])
	}
}

func TestRdBlockScoreAppliesLibvpxPlaneAndIntraMultipliers(t *testing.T) {
	if got := rdBlockScore(40, 4, false, 100, 20); got != 79 {
		t.Fatalf("inter block rd = %d, want 79", got)
	}
	if got := rdBlockScore(40, 4, true, 100, 20); got != 53 {
		t.Fatalf("intra block rd = %d, want 53", got)
	}
}

func TestStaticInterRDEncodeBreakoutUsesStrictLibvpxThreshold(t *testing.T) {
	pred := testVP8Frame(t, 16, 16, 128, 90, 170)
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testMacroblockQuant(20)

	src.Y[0] = 133
	if !staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = false, want skip below AC threshold")
	}

	src.Y[0] = 134
	if staticInterRDEncodeBreakout(sourceImageFromPublic(src), &pred.Img, 0, 0, &quant, 1) {
		t.Fatalf("static breakout = true, want no skip at strict AC threshold")
	}
}
