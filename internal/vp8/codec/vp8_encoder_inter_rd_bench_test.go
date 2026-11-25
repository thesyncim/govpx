package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

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

func BenchmarkEstimateInterSplitResidualRDAccounting(b *testing.B) {
	e := newSizedTestEncoder(b, 32, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		b.Fatalf("SetDeadline returned error: %v", err)
	}
	var decSeg vp8dec.SegmentationHeader
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: testInterSearchQIndex}, &decSeg, &e.dequantTables, &e.dequants)
	src, ref, _, quant, qIndex := splitMVDecisionRDFixture(b)
	var labelRD splitMotionLabelRDEvaluator
	labelRD.init(e.rc.currentZbinOverQuant, 0, nil, nil, e.libvpxUseFastQuantForPick(), false)
	shapeCtx := splitMotionShapeContext{
		src:                 src,
		ref:                 ref,
		refFrame:            vp8common.LastFrame,
		mbRow:               0,
		mbCol:               0,
		qIndex:              qIndex,
		partition:           0,
		search:              defaultInterAnalysisSearchConfig(),
		mvProbs:             &e.modeProbs.MV,
		subMVRefProbs:       &e.subMVRefProbs,
		labelRD:             &labelRD,
		quant:               &quant,
		coefProbs:           e.pickerCoefProbs(),
		segmentYRDCap:       maxInt(),
		segmentOverheadRate: vp8enc.MBSplitPartitionRate(0) + vp8enc.InterPredictionModeRate(vp8common.SplitMV, vp8enc.InterModeCounts{}),
	}
	shape := shapeCtx.selectShape()
	if !shape.OK || shape.Cutoff {
		b.Fatalf("split shape setup failed: %+v", shape)
	}
	mode := shape.Mode
	mode.SegmentID = 0
	ctx := interSplitModeRDContext{
		src:       src,
		ref:       interAnalysisReference{Frame: vp8common.LastFrame, Img: ref},
		mbRow:     0,
		mbCol:     0,
		qIndex:    qIndex,
		segmentID: 0,
		quant:     &quant,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acct, ok := e.estimateInterSplitResidualRDAccounting(&ctx, &mode, &shape)
		if !ok || acct.distortionUV == 0 {
			b.Fatalf("estimateInterSplitResidualRDAccounting returned ok=%t acct=%+v", ok, acct)
		}
	}
}

func BenchmarkPredictInterAnalysisSplitMVChroma(b *testing.B) {
	e := newSizedTestEncoder(b, 32, 32)
	ref := testVP8Frame(b, 32, 32, 0, 0, 0)
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

	b.Run("FullReconstruct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !reconstructInterAnalysisMacroblock(&e.analysis.Img, &ref.Img, 1, 1, &mode, nil, &e.dequants[0], &e.reconstructScratch) {
				b.Fatalf("full split reconstruction returned false")
			}
		}
	})
	b.Run("ChromaOnly", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !predictInterAnalysisSplitMVChroma(&e.analysis.Img, &ref.Img, 1, 1, &mode) {
				b.Fatalf("split chroma predictor returned false")
			}
		}
	})
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

func TestEstimateInterIntraModeRDScoreChromaCacheMatchesUncached(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.UVMode = [vp8tables.UVModeProbCount]uint8{245, 8, 230}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testRegularMacroblockQuant(t, 20)
	modes := []vp8common.MBPredictionMode{
		vp8common.DCPred,
		vp8common.VPred,
		vp8common.HPred,
		vp8common.TMPred,
		vp8common.BPred,
	}

	var cache interIntraChromaRDCache
	for _, mode := range modes {
		fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
		e.analysis.ExtendBorders()
		got, gotOK := e.estimateInterIntraModeRDScoreWithChromaCache(sourceImageFromPublic(src), 20, 0, 0, mode, maxInt(), nil, nil, &quant, &cache)

		fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
		e.analysis.ExtendBorders()
		want, wantOK := e.estimateInterIntraModeRDScore(sourceImageFromPublic(src), 20, 0, 0, mode, maxInt(), nil, nil, &quant)

		if gotOK != wantOK {
			t.Fatalf("%v cached ok=%v, want %v", mode, gotOK, wantOK)
		}
		if !gotOK {
			continue
		}
		if got.mode != want.mode ||
			got.score != want.score ||
			got.yrd != want.yrd ||
			got.rate != want.rate ||
			got.rateY != want.rateY ||
			got.rateUV != want.rateUV ||
			got.distortion != want.distortion ||
			got.distortionUV != want.distortionUV {
			t.Fatalf("%v cached result = %+v, want %+v", mode, got, want)
		}
	}
	if !cache.valid || !cache.ok {
		t.Fatalf("chroma cache was not populated: %+v", cache)
	}
}

func BenchmarkEstimateInterIntraModeRDScoreChromaCache(b *testing.B) {
	e := newSizedTestEncoder(b, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		b.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)
	quant := testRegularMacroblockQuant(b, 20)
	source := sourceImageFromPublic(src)
	modes := []vp8common.MBPredictionMode{
		vp8common.DCPred,
		vp8common.VPred,
		vp8common.HPred,
		vp8common.TMPred,
		vp8common.BPred,
	}

	b.Run("Uncached", func(b *testing.B) {
		var sink int
		b.ReportAllocs()
		for range b.N {
			fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
			e.analysis.ExtendBorders()
			for _, mode := range modes {
				result, ok := e.estimateInterIntraModeRDScore(source, 20, 0, 0, mode, maxInt(), nil, nil, &quant)
				if !ok {
					b.Fatalf("estimateInterIntraModeRDScore(%v) returned ok=false", mode)
				}
				sink += result.score
			}
		}
		_ = sink
	})
	b.Run("Cached", func(b *testing.B) {
		var sink int
		b.ReportAllocs()
		for range b.N {
			fillBenchmarkVP8Image(&e.analysis.Img, 128, 90, 170)
			e.analysis.ExtendBorders()
			var cache interIntraChromaRDCache
			for _, mode := range modes {
				result, ok := e.estimateInterIntraModeRDScoreWithChromaCache(source, 20, 0, 0, mode, maxInt(), nil, nil, &quant, &cache)
				if !ok {
					b.Fatalf("estimateInterIntraModeRDScoreWithChromaCache(%v) returned ok=false", mode)
				}
				sink += result.score
			}
		}
		_ = sink
	})
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

func TestSplitSubMotionLabelSearchCostUsesLibvpxDefaultSubMVRefProb(t *testing.T) {
	const qIndex = 127

	got := splitSubMotionLabelSearchCost(vp8common.Above4x4, qIndex)
	wantRate := vp8enc.SplitSubMotionLabelCost(vp8common.Above4x4, vp8enc.DefaultSubMVRefProbs)
	want := (wantRate*vp8enc.SADPerBit4(qIndex) + 128) >> 8
	if got != want {
		t.Fatalf("ABOVE4X4 search cost = %d, want libvpx default cost %d", got, want)
	}
	contextualRate := vp8enc.SplitSubMotionLabelCost(vp8common.Above4x4, vp8tables.SubMVRefProb3[4])
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
