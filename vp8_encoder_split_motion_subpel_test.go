package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestSelectInterFrameSplitSubsetMotionModeRefinesNew4x4Subpixel(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((19 + row*17 + col*13 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()
	targetMV := vp8enc.MotionVector{Row: 18, Col: 18}
	refBaseY := int(targetMV.Row >> 3)
	refBaseX := int(targetMV.Col >> 3)
	refStart := ref.Img.YFull[ref.Img.YOrigin+(refBaseY-2)*ref.Img.YStride+refBaseX-2:]
	dsp.SixTapPredict4x4(refStart, ref.Img.YStride, int(targetMV.Col)&7, int(targetMV.Row)&7, src.Y, src.YStride)

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))

	mv, bMode := selectInterFrameSplitSubsetMotionModeWithSearch(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height, vp8enc.MotionVector{}, vp8enc.MotionVector{Row: 16, Col: 16}, 7, false, testInterSearchQIndex, nil, nil, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext)

	if bMode != vp8common.New4x4 || (int(mv.Row)&7 == 0 && int(mv.Col)&7 == 0) {
		t.Fatalf("subset candidate = %+v/%v, want NEW4X4 subpixel MV", mv, bMode)
	}
	if sad := splitBlockSAD(sourceImageFromPublic(src), &ref.Img, 0, 0, 0, 4, 4, targetMV); sad != 0 {
		t.Fatalf("subpixel split SAD = %d, want exact predictor match", sad)
	}
}

func TestSplitBlockSADUsesSubpixelPredictorForAllShapes(t *testing.T) {
	cases := []struct {
		name    string
		block   int
		width   int
		height  int
		predict func(src []byte, srcStride int, xOffset int, yOffset int, dst []byte, dstStride int)
	}{
		{name: "16x8", block: 0, width: 16, height: 8, predict: dsp.SixTapPredict16x8},
		{name: "8x16", block: 0, width: 8, height: 16, predict: dsp.SixTapPredict8x16},
		{name: "8x8", block: 0, width: 8, height: 8, predict: dsp.SixTapPredict8x8},
		{name: "4x4", block: 5, width: 4, height: 4, predict: dsp.SixTapPredict4x4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := testImage(32, 32)
			fillImage(src, 0, 90, 170)
			ref := testVP8Frame(t, 32, 32, 0, 90, 170)
			for row := 0; row < ref.Img.CodedHeight; row++ {
				for col := 0; col < ref.Img.CodedWidth; col++ {
					ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*19 + col*23 + row*col*11) & 0xff)
				}
			}
			ref.ExtendBorders()

			baseY := (tc.block >> 2) * 4
			baseX := (tc.block & 3) * 4
			refStart := ref.Img.YOrigin + (baseY-2)*ref.Img.YStride + baseX - 2
			tc.predict(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[baseY*src.YStride+baseX:], src.YStride)

			if sad := splitBlockSAD(sourceImageFromPublic(src), &ref.Img, 0, 0, tc.block, tc.width, tc.height, vp8enc.MotionVector{Row: 2, Col: 2}); sad != 0 {
				t.Fatalf("splitBlockSAD = %d, want exact subpixel predictor match", sad)
			}
		})
	}
}

func TestRefineInterFrameSplitBlockSubpixelMotionVectorUsesBilinearVariance(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin
	dsp.BilinearPredict4x4(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y, src.YStride)

	mv, cost, ok := refineInterFrameSplitBlockSubpixelMotionVector(sourceImageFromPublic(src), &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, vp8enc.MotionVector{}, testInterSearchQIndex, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext)

	if !ok {
		t.Fatalf("refineInterFrameSplitBlockSubpixelMotionVector returned ok=false")
	}
	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mv = %+v, want +2,+2 quarter-pel candidate", mv)
	}
	if want := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost != want {
		t.Fatalf("cost = %d, want zero distortion plus mv cost %d", cost, want)
	}
}

func TestSelectInterFrameReferenceMotionVectorRefinesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 13, 90, 170)
	last := testVP8Frame(t, 48, 48, 40, 90, 170)
	for row := 0; row < last.Img.CodedHeight; row++ {
		for col := 0; col < last.Img.CodedWidth; col++ {
			last.Img.Y[row*last.Img.YStride+col] = byte((19 + row*17 + col*13 + row*col*3) & 0xff)
		}
	}
	last.ExtendBorders()
	refStart := last.Img.YFull[last.Img.YOrigin+(16-2)*last.Img.YStride+16-2:]
	dsp.SixTapPredict16x16(refStart, last.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 1, 1, 2, 2, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("selection = %v %+v, want last subpixel +2,+2", ref.Frame, mv)
	}
}

func TestMacroblockSubpixelSADHonorsLimit(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 255, 90, 170)
	ref := testVP8Frame(t, 16, 16, 0, 90, 170)
	source := sourceImageFromPublic(src)

	full, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, maxInt())
	if !ok {
		t.Fatalf("macroblockSubpixelSAD returned ok=false")
	}
	limited, ok := macroblockSubpixelSAD(source, &ref.Img, 0, 0, 0, 0, 2, 2, 1024)
	if !ok {
		t.Fatalf("limited macroblockSubpixelSAD returned ok=false")
	}
	if limited <= 1024 || limited >= full {
		t.Fatalf("limited SAD = %d, full = %d, want early result above limit and below full", limited, full)
	}
}

func TestSplitBlockSADClampsPartialSourceSubpel(t *testing.T) {
	src := testImage(72, 40)
	fillImage(src, 0, 90, 170)
	for row := range src.Height {
		for col := range src.Width {
			src.Y[row*src.YStride+col] = byte((5 + row*31 + col*11 + row*col*7) & 0xff)
		}
	}
	ref := testVP8Frame(t, 72, 40, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((19 + row*13 + col*29 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()

	mbRow, mbCol, block := 2, 4, 5
	width, height := 8, 8
	mv := vp8enc.MotionVector{Row: 2, Col: 2}
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	refBaseY := baseY + (int(mv.Row) >> 3)
	refBaseX := baseX + (int(mv.Col) >> 3)
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7

	var srcScratch [16 * 16]byte
	for row := range height {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		for col := range width {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			srcScratch[row*16+col] = src.Y[srcY*src.YStride+srcX]
		}
	}
	// libvpx allocates the reference YV12 buffer with 16-aligned width/
	// height (alloccommon.c:56-65 rounds up before vp8_yv12_alloc_frame_buffer),
	// so y_crop_height==y_height==coded height. The post-LF
	// vp8_yv12_extend_frame_borders therefore extends from coded-edge-1,
	// leaving the coded-but-invisible MB padding populated with the live
	// LF reconstruction. splitBlockSAD's SixTap fallback clamps the
	// scratch read window to the coded extent to mirror that state.
	var refScratch [(8 + 5) * (8 + 5)]byte
	gatherCodedClampedRefBlock(&ref.Img, refBaseY-2, refBaseX-2, 8+5, 8+5, refScratch[:], 8+5)
	var pred [16 * 16]byte
	dsp.SixTapPredict8x8(refScratch[:], 8+5, xOffset, yOffset, pred[:], 8)
	want := dsp.SAD8x8(srcScratch[:], 16, pred[:], 8)

	if got := splitBlockSAD(sourceImageFromPublic(src), &ref.Img, mbRow, mbCol, block, width, height, mv); got != want {
		t.Fatalf("partial-source split subpel SAD = %d, want %d", got, want)
	}
	if fallback := splitBlockSAD(sourceImageFromPublic(src), &ref.Img, mbRow, mbCol, block, width, height, vp8enc.MotionVector{}); fallback == want {
		t.Fatalf("test setup full-pel fallback unexpectedly matches subpel SAD %d", fallback)
	}
}

func TestSplitBlockSubpixelVarianceClampsPartialSource(t *testing.T) {
	src := testImage(72, 40)
	fillImage(src, 0, 90, 170)
	for row := range src.Height {
		for col := range src.Width {
			src.Y[row*src.YStride+col] = byte((7 + row*17 + col*13 + row*col*5) & 0xff)
		}
	}
	ref := testVP8Frame(t, 72, 40, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*19 + col*11 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()

	mbRow, mbCol, block := 2, 4, 5
	width, height := 8, 8
	quarterRow, quarterCol := 1, 1
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	refBaseY := baseY + (quarterRow >> 2)
	refBaseX := baseX + (quarterCol >> 2)
	xOffset := (quarterCol & 3) << 1
	yOffset := (quarterRow & 3) << 1

	var srcScratch [16 * 16]byte
	vp8enc.GatherClampedLumaBlock(sourceImageFromPublic(src), baseY, baseX, width, height, srcScratch[:], 16)
	var refScratch [(8 + 1) * (8 + 1)]byte
	gatherCodedClampedRefBlock(&ref.Img, refBaseY, refBaseX, width+1, height+1, refScratch[:], width+1)
	want, _ := dsp.SubpelVariance8x8(refScratch[:], width+1, xOffset, yOffset, srcScratch[:], 16)

	got, ok := splitBlockSubpixelVarianceForQuarterMV(sourceImageFromPublic(src), &ref.Img, mbRow, mbCol, block, width, height, quarterRow, quarterCol)
	if !ok {
		t.Fatalf("splitBlockSubpixelVarianceForQuarterMV returned ok=false")
	}
	if got != want {
		t.Fatalf("partial-source split subpel variance = %d, want %d", got, want)
	}
}

func TestPredictSplitMotionSubpixelBlock4x4UsesCodedClamp(t *testing.T) {
	ref := testVP8Frame(t, 5, 5, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte(10 + row*7 + col*3)
		}
		ref.Img.Y[row*ref.Img.YStride+4] = byte(90 + row)
	}
	ref.ExtendBorders()

	var got [16]byte
	if !predictSplitMotionBlock4x4(&ref.Img, 0, 0, 1, vp8enc.MotionVector{Col: 2}, &got) {
		t.Fatalf("predictSplitMotionBlock4x4 returned false")
	}
	var want [16]byte
	start := ref.Img.YOrigin - 2*ref.Img.YStride + 2
	dsp.SixTapPredict4x4(ref.Img.YFull[start:], ref.Img.YStride, 2, 0, want[:], 4)
	if got != want {
		t.Fatalf("subpel coded-clamp predictor mismatch\ngot  %v\nwant %v", got, want)
	}
}

func TestMacroblockSubpixelVarianceMatchesBilinearPredictor(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*13 + col*19 + row*col*3) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 6, src.Y[16*src.YStride+16:], src.YStride)

	variance, sse, ok := macroblockSubpixelVariance(sourceImageFromPublic(src), &ref.Img, 16, 16, 16, 16, 2, 6)

	if !ok {
		t.Fatalf("macroblockSubpixelVariance returned ok=false")
	}
	if variance != 0 || sse != 0 {
		t.Fatalf("subpixel variance = %d/%d, want exact bilinear match", variance, sse)
	}
}

func TestIterativeInterFrameSubpixelMotionVectorUsesBilinearVariance(t *testing.T) {
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

	mvProbs := vp8tables.DefaultMVContext
	var mvCosts vp8enc.MotionVectorCostTables
	mvCosts.Build(&mvProbs)
	search := interFrameSubpixelSearch{
		src:     sourceImageFromPublic(src),
		ref:     &ref.Img,
		mbRow:   1,
		mbCol:   1,
		qIndex:  testInterSearchQIndex,
		mvProbs: &mvProbs,
		mvCosts: &mvCosts,
	}
	mv, cost, _, _, ok := search.iterativeNoStats()

	if !ok {
		t.Fatalf("interFrameSubpixelSearch.iterativeNoStats returned ok=false")
	}
	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mv = %+v, want +2,+2 quarter-pel candidate", mv)
	}
	if want := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost != want {
		t.Fatalf("cost = %d, want zero distortion plus mv cost %d", cost, want)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesSubpixelCandidate(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((31 + row*5 + col*17 + row*col*11) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 1, 1, 3, 3, testInterSearchQIndex, nil, nil, nil, &vp8tables.DefaultMVContext, &candidates)

	if count != 2 {
		t.Fatalf("candidate count = %d, want full-pixel plus subpixel", count)
	}
	if candidates[0].MV != (vp8enc.MotionVector{}) {
		t.Fatalf("full-pixel candidate = %+v, want zero MV", candidates[0].MV)
	}
	if candidates[1].MV != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("subpixel candidate = %+v, want +2,+2", candidates[1].MV)
	}
}

func TestInterFrameMotionSearchStatsCountsSubpelTopology(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((31 + row*5 + col*17 + row*col*11) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)

	cfg := interAnalysisSearchConfig{
		fullPixelSearch:  interAnalysisFullPixelSearchHex,
		fractionalSearch: interAnalysisFractionalSearchIterative,
	}
	var stats interFrameMotionSearchStats
	mv, _ := selectInterFrameMotionVectorWithSearchStartAndStats(sourceImageFromPublic(src), &ref.Img, 1, 1, 3, 3, vp8enc.MotionVector{}, testInterSearchQIndex, cfg, interFrameSearchStart{}, &vp8tables.DefaultMVContext, &stats)

	if mv != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("motion search MV = %+v, want +2,+2", mv)
	}
	if stats.subpelCandidates == 0 || stats.subpelVarianceCalls == 0 {
		t.Fatalf("subpel stats did not count candidate variance work: %+v", stats)
	}
	if stats.subpelCandidates < stats.subpelVarianceCalls {
		t.Fatalf("subpel candidates/variance calls = %d/%d, want candidates >= variance calls", stats.subpelCandidates, stats.subpelVarianceCalls)
	}
	if stats.subpelCandidates > interFrameSubpixelSearchCandidateCount() {
		t.Fatalf("subpel candidates = %d, want <= libvpx-shaped max %d", stats.subpelCandidates, interFrameSubpixelSearchCandidateCount())
	}
}

func TestInterFrameSubpixelSearchCandidateCount(t *testing.T) {
	if got := interFrameSubpixelSearchCandidateCount(); got != 31 {
		t.Fatalf("subpixel candidate count = %d, want libvpx iterative max 31", got)
	}
}
