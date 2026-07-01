package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
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
	stats := buildPredictedMacroblockCoefficientsRD(&e.coefProbs, sourceImageFromPublic(src), 0, 0, &ref.Img, nil, nil, &quant, 20, e.rc.currentZbinOverQuant, vp8enc.InterZbinModeBoost(&mode), false, false, e.libvpxUseFastQuant(), false, &coeffs)
	wantDistortion := stats.distortionY + stats.distortionUV
	if acct.distortion2 != wantDistortion || acct.distortionUV != stats.distortionUV {
		t.Fatalf("accounting distortion = %d uv=%d, want transform-domain %d uv=%d", acct.distortion2, acct.distortionUV, wantDistortion, stats.distortionUV)
	}
	if pixelSSE := macroblockImageSSE(sourceImageFromPublic(src), &ref.Img, 0, 0); acct.distortion2 == pixelSSE {
		t.Fatalf("accounting distortion = pixel SSE %d, want transform-domain distortion", pixelSSE)
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
				nil, nil, &quant, 20, 0, vp8enc.InterZbinModeBoost(&tc.mode),
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

func TestPredictInterAnalysisSplitMVChromaMatchesFullReconstruct(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	ref := testVP8Frame(t, 32, 32, 0, 0, 0)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte(31 + (row*7+col*11)&0x7f)
		}
	}
	uvWidth := (ref.Img.CodedWidth + 1) >> 1
	uvHeight := (ref.Img.CodedHeight + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			ref.Img.U[row*ref.Img.UStride+col] = byte(53 + (row*13+col*5)&0x7f)
			ref.Img.V[row*ref.Img.VStride+col] = byte(71 + (row*3+col*17)&0x7f)
		}
	}
	ref.ExtendBorders()

	mode := vp8dec.MacroblockMode{
		Mode:        vp8common.SplitMV,
		RefFrame:    vp8common.LastFrame,
		Is4x4:       true,
		MBSkipCoeff: true,
		Partition:   0,
	}
	for block := range mode.BlockMV {
		mode.BlockMV[block] = vp8dec.MotionVector{
			Row: int16(((block%4)-1)*3 + (block >> 2)),
			Col: int16(((block>>2)-1)*5 + (block & 3)),
		}
	}

	full := testVP8Frame(t, 32, 32, 19, 23, 29)
	chromaOnly := testVP8Frame(t, 32, 32, 101, 23, 29)
	if !reconstructInterAnalysisMacroblock(&full.Img, &ref.Img, 1, 1, &mode, nil, &e.dequants[0], &e.reconstructScratch) {
		t.Fatalf("full split reconstruction returned false")
	}
	if !predictInterAnalysisSplitMVChroma(&chromaOnly.Img, &ref.Img, 1, 1, &mode) {
		t.Fatalf("split chroma predictor returned false")
	}

	assertPlaneBlockEqual(t, "split chroma U", full.Img.U, full.Img.UStride, chromaOnly.Img.U, chromaOnly.Img.UStride, uvWidth, uvHeight, 8, 8, 8, 8)
	assertPlaneBlockEqual(t, "split chroma V", full.Img.V, full.Img.VStride, chromaOnly.Img.V, chromaOnly.Img.VStride, uvWidth, uvHeight, 8, 8, 8, 8)
	for row := 16; row < 32; row++ {
		for col := 16; col < 32; col++ {
			if got := chromaOnly.Img.Y[row*chromaOnly.Img.YStride+col]; got != 101 {
				t.Fatalf("chroma-only predictor changed Y[%d,%d] = %d, want 101", row, col, got)
			}
		}
	}
}
