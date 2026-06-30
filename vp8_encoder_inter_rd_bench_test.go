package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

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
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
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
