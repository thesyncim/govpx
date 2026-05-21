package govpx

import (
	"reflect"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestInterFrameMotionSearchDefaultPathHasNoStatsField(t *testing.T) {
	cases := []reflect.Type{
		reflect.TypeOf(interFrameMotionVectorSearch{}),
		reflect.TypeOf(interFrameSubpixelSearch{}),
	}
	for _, typ := range cases {
		if _, ok := typ.FieldByName("stats"); ok {
			t.Fatalf("%s carries stats field in default search path", typ.Name())
		}
	}
}

func TestSelectInterFrameReferenceMotionVectorChoosesLowestCostReference(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 16, 16, 220, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	alt := testVP8Frame(t, 16, 16, 80, 90, 170)
	for row := range 16 {
		for col := range 16 {
			v := byte(32 + ((row*17 + col*11) & 127))
			src.Y[row*src.YStride+col] = v
			golden.Img.Y[row*golden.Img.YStride+col] = v
			last.Img.Y[row*last.Img.YStride+col] = byte(200 - ((row*7 + col*19) & 63))
			alt.Img.Y[row*alt.Img.YStride+col] = byte(96 + ((row*5 + col*3) & 63))
		}
	}
	last.ExtendBorders()
	golden.ExtendBorders()
	alt.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.GoldenFrame || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("selection = %v %+v, want golden zero MV", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorUsesLibvpxHexCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte(17 + ((row*19 + col*11) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+2)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("selection = %v %+v, want last row +16 from libvpx hex ring", ref.Frame, mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorRealtimeHexWalksNextCheckpoints(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((19 + row*73 + col*151 + row*col*37) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 127, 90, 170)
	for row := range 16 {
		for col := range 16 {
			v := src.Y[(row+16)*src.YStride+col+16]
			last.Img.Y[(row+18)*last.Img.YStride+col+16] = v ^ 1
		}
	}
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{fullPixelSearch: interAnalysisFullPixelSearchHex, fractionalSearch: interAnalysisFractionalSearchStep}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("hex full-pixel MV = %+v, want row +32 from libvpx next_chkpts walk", mv)
	}
}

func TestInterFrameMotionSearchStatsCountsFullPelTopology(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((19 + row*73 + col*151 + row*col*37) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 127, 90, 170)
	for row := range 16 {
		for col := range 16 {
			v := src.Y[(row+16)*src.YStride+col+16]
			last.Img.Y[(row+18)*last.Img.YStride+col+16] = v ^ 1
		}
	}
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{fullPixelSearch: interAnalysisFullPixelSearchHex, fractionalSearch: interAnalysisFractionalSearchStep}
	var stats interFrameMotionSearchStats
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsAndStats(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg, interFrameSearchStart{}, &vp8tables.DefaultMVContext, &stats)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("hex full-pixel MV = %+v, want row +32", mv)
	}
	if stats.fullPelSADCalls == 0 || stats.fullPelSADCandidates == 0 {
		t.Fatalf("full-pel stats did not count SAD work: %+v", stats)
	}
	if stats.fullPelSADCandidates < stats.fullPelSADCalls {
		t.Fatalf("full-pel candidates/calls = %d/%d, want candidates >= calls", stats.fullPelSADCandidates, stats.fullPelSADCalls)
	}
	if stats.fullPelEarlyBreaks == 0 {
		t.Fatalf("full-pel early breaks = 0, want HEX neighborhood stop counted")
	}
}

func TestInterFrameSubpelStepNoStatsMatchesStatsPath(t *testing.T) {
	tests := []struct {
		name       string
		fractional interAnalysisFractionalSearchMethod
	}{
		{name: "step", fractional: interAnalysisFractionalSearchStep},
		{name: "half", fractional: interAnalysisFractionalSearchHalf},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := newTestInterFrameSubpixelStepSearch(t, tc.fractional)
			var stats interFrameMotionSearchStats
			wantMV, wantCost, wantVariance, wantSSE, wantOK := base.refineWithStats(&stats)
			if !wantOK {
				t.Fatalf("stats path refine returned ok=false")
			}
			if stats.subpelCandidates == 0 || stats.subpelVarianceCalls == 0 {
				t.Fatalf("stats path did not count subpel work: %+v", stats)
			}

			gotMV, gotCost, gotVariance, gotSSE, gotOK := base.refine()
			if gotOK != wantOK || gotMV != wantMV || gotCost != wantCost || gotVariance != wantVariance || gotSSE != wantSSE {
				t.Fatalf("no-stats refine = (%+v,%d,%d,%d,%v), want (%+v,%d,%d,%d,%v)",
					gotMV, gotCost, gotVariance, gotSSE, gotOK,
					wantMV, wantCost, wantVariance, wantSSE, wantOK)
			}
		})
	}
}

func newTestInterFrameSubpixelStepSearch(tb testing.TB, fractional interAnalysisFractionalSearchMethod) interFrameSubpixelSearch {
	tb.Helper()
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((19 + row*73 + col*151 + row*col*37) & 255)
		}
	}

	last := testVP8Frame(tb, 64, 64, 127, 90, 170)
	for row := range 16 {
		for col := range 16 {
			v := src.Y[(row+16)*src.YStride+col+16]
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = v
			last.Img.Y[(row+20)*last.Img.YStride+col+17] = v ^ 1
		}
	}
	last.ExtendBorders()

	return interFrameSubpixelSearch{
		ref:       &last.Img,
		mvProbs:   &vp8tables.DefaultMVContext,
		src:       sourceImageFromPublic(src),
		mbRow:     1,
		mbCol:     1,
		qIndex:    testInterSearchQIndex,
		best:      vp8enc.MotionVector{Row: 32},
		bestRefMV: vp8enc.MotionVector{},
		search:    interAnalysisSearchConfig{fractionalSearch: fractional},
	}
}

func BenchmarkInterFrameSubpelStepNoStats(b *testing.B) {
	search := newTestInterFrameSubpixelStepSearch(b, interAnalysisFractionalSearchStep)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, _, _, ok := search.refine(); !ok {
			b.Fatal("refine returned ok=false")
		}
	}
}

func BenchmarkInterFrameSubpelHalfNoStats(b *testing.B) {
	search := newTestInterFrameSubpixelStepSearch(b, interAnalysisFractionalSearchHalf)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, _, _, ok := search.refine(); !ok {
			b.Fatal("refine returned ok=false")
		}
	}
}

func TestSelectInterFrameFullPixelMotionVectorNstepUsesLibvpxSearchSites(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((23 + row*71 + col*139 + row*col*41) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("nstep full-pixel MV = %+v, want row +32 from libvpx search-site contraction", mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorDiamondUsesLibvpxSearchSites(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((23 + row*71 + col*139 + row*col*41) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchDiamond,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	if mv != (vp8enc.MotionVector{Row: 32}) {
		t.Fatalf("diamond full-pixel MV = %+v, want row +32 from libvpx four-site contraction", mv)
	}
}

func TestSelectInterFrameFullPixelMotionVectorDiamondKeepsFourSitePath(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((31 + row*67 + col*149 + row*col*43) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+20)*last.Img.YStride+col+20] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	nstepCfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  0,
		fullPixelFurtherSteps: 7,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	diamondCfg := nstepCfg
	diamondCfg.fullPixelSearch = interAnalysisFullPixelSearchDiamond

	source := sourceImageFromPublic(src)
	nstepMV, _ := selectInterFrameFullPixelMotionVectorWithSearch(source, &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, nstepCfg)
	diamondMV, _ := selectInterFrameFullPixelMotionVectorWithSearch(source, &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, diamondCfg)

	if nstepMV != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("nstep full-pixel MV = %+v, want diagonal +32,+32", nstepMV)
	}
	if diamondMV == nstepMV {
		t.Fatalf("diamond full-pixel MV = %+v, want four-site path distinct from NSTEP diagonal", diamondMV)
	}
}

func TestFullPelSADUsesBorderedReferencePlane(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 31, 90, 170)
	ref := testVP8Frame(t, 16, 16, 200, 90, 170)

	start := ref.Img.YOrigin - 16*ref.Img.YStride - 16
	for row := range 19 {
		for col := range 16 {
			ref.Img.YFull[start+row*ref.Img.YStride+col] = 31
		}
	}

	source := sourceImageFromPublic(src)
	ctx := newFullPelSearchCtx(source, &ref.Img, 0, 0)
	if got := ctx.fullPelSADFull(-16, -16); got != 0 {
		t.Fatalf("bordered full-pel SAD = %d, want 0 from YFull border", got)
	}

	var sad4 [4]uint32
	if ok := ctx.fullPelSADFull4(-16, -16, -15, -16, -14, -16, -13, -16, &sad4); !ok {
		t.Fatalf("bordered x4 full-pel SAD returned ok=false")
	}
	for i, got := range sad4 {
		if got != 0 {
			t.Fatalf("bordered x4 SAD[%d] = %d, want 0 from YFull border", i, got)
		}
	}

	mv := vp8enc.MotionVector{Row: -128, Col: -128}
	if got := macroblockSAD(source, &ref.Img, 0, 0, mv); got != 0 {
		t.Fatalf("macroblockSAD bordered full-pel = %d, want 0", got)
	}
	if got := macroblockSADLimited(source, &ref.Img, 0, 0, mv, maxInt()); got != 0 {
		t.Fatalf("macroblockSADLimited bordered full-pel = %d, want 0", got)
	}
	if variance, sse := macroblockLumaMotionVarianceSSE(source, &ref.Img, 0, 0, mv); variance != 0 || sse != 0 {
		t.Fatalf("bordered full-pel variance/sse = %d/%d, want 0/0", variance, sse)
	}
}

func TestFullPelSADClampsPartialSourceMacroblock(t *testing.T) {
	src := testImage(17, 16)
	fillImage(src, 3, 90, 170)
	for row := range 16 {
		src.Y[row*src.YStride+16] = 77
	}
	ref := testVP8Frame(t, 17, 16, 0, 90, 170)
	for row := range 16 {
		for col := 16; col < 32; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = 77
		}
	}

	source := sourceImageFromPublic(src)
	ctx := newFullPelSearchCtx(source, &ref.Img, 0, 1)
	if got := ctx.fullPelSADFull(0, 0); got != 0 {
		t.Fatalf("partial-source full-pel SAD = %d, want 0 from visible-edge clamp", got)
	}
}

func TestSubpelSearchClampsPartialSourceMacroblock(t *testing.T) {
	src := testImage(17, 16)
	fillImage(src, 3, 90, 170)
	for row := range 16 {
		src.Y[row*src.YStride+16] = 77
	}
	ref := testVP8Frame(t, 17, 16, 0, 90, 170)
	for row := range 16 {
		for col := 16; col < 32; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = 77
		}
	}

	source := sourceImageFromPublic(src)
	ctx, ok := newSubpelSearchCtx(source, &ref.Img, 0, 1)
	if !ok {
		t.Fatalf("partial-source subpel ctx returned ok=false")
	}
	variance, sse, ok := ctx.subpelVarianceForQuarterMV(0, 0)
	if !ok {
		t.Fatalf("partial-source subpel variance returned ok=false")
	}
	if variance != 0 || sse != 0 {
		t.Fatalf("partial-source subpel variance/sse = %d/%d, want 0/0", variance, sse)
	}
}

func TestMacroblockLumaMotionVarianceSSEClampsPartialSourceSubpel(t *testing.T) {
	src := testImage(72, 40)
	fillImage(src, 0, 90, 170)
	for row := range src.Height {
		for col := range src.Width {
			src.Y[row*src.YStride+col] = byte((row*29 + col*17 + row*col*3) & 0xff)
		}
	}
	ref := testVP8Frame(t, 72, 40, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((13 + row*7 + col*23 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()

	mbRow, mbCol := 2, 4
	mv := vp8enc.MotionVector{Row: 8, Col: 18}
	baseY := mbRow * 16
	baseX := mbCol * 16
	refBaseY := baseY + (int(mv.Row) >> 3)
	refBaseX := baseX + (int(mv.Col) >> 3)
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	var srcScratch [16 * 16]byte
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			srcScratch[row*16+col] = src.Y[srcY*src.YStride+srcX]
		}
	}
	refStart := ref.Img.YOrigin + refBaseY*ref.Img.YStride + refBaseX
	wantVariance, wantSSE := dsp.SubpelVariance16x16(ref.Img.YFull[refStart:], ref.Img.YStride, xOffset, yOffset, srcScratch[:], 16)

	gotVariance, gotSSE := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, mbRow, mbCol, mv)
	if gotVariance != wantVariance || gotSSE != wantSSE {
		t.Fatalf("partial-source subpel variance/sse = %d/%d, want %d/%d", gotVariance, gotSSE, wantVariance, wantSSE)
	}

	fallbackVariance, fallbackSSE := scalarFullPelClampedMotionVarianceSSE(sourceImageFromPublic(src), &ref.Img, baseY, baseX, refBaseY, refBaseX)
	if fallbackVariance == wantVariance && fallbackSSE == wantSSE {
		t.Fatalf("test setup full-pel fallback unexpectedly matches subpel oracle: %d/%d", fallbackVariance, fallbackSSE)
	}
}

func scalarFullPelClampedMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int) (int, int) {
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

func TestSelectInterFrameFullPixelMotionVectorRDRefinesNstepResult(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = 200
		}
	}

	last := testVP8Frame(t, 64, 64, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+18)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}
	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  interFrameMaxMVSearchSteps - 1,
		fullPixelFurtherSteps: 0,
	}
	unrefined, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)

	cfg.fullPixelFinalRefine = true
	refined, _ := selectInterFrameFullPixelMotionVectorWithSearch(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, vp8enc.MotionVector{}, testInterSearchQIndex, cfg)
	if refined != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("refined nstep MV = %+v, want libvpx final 1-away refine to row +16", refined)
	}
	if refined == unrefined {
		t.Fatalf("refined nstep MV = unrefined %+v, want final refine to move the candidate", refined)
	}
}

func TestFullPixelFinalRefineKeepsBetterReturnCost(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 90, 170)

	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := range 16 {
		for col := range 16 {
			ref.Img.Y[row*ref.Img.YStride+col] = 126
		}
	}
	for col := range 16 {
		ref.Img.Y[16*ref.Img.YStride+col] = 128
	}
	ref.ExtendBorders()

	source := sourceImageFromPublic(src)
	bestRefMV := vp8enc.MotionVector{Row: 8}
	searcher := newFullPelMotionSearch(
		source,
		&ref.Img,
		0,
		0,
		bestRefMV,
		0,
		vp8enc.InterFrameFullPixelBounds{RowMin: -4, RowMax: 4, ColMin: -4, ColMax: 4},
		nil,
		nil,
		0,
		&interFrameMotionSearchStats{},
	)
	center := vp8enc.MotionVector{}
	centerCost := searcher.cost(center)

	refined, refinedCost := searcher.refine(center, 8)
	if refined != (vp8enc.MotionVector{Row: 8}) {
		t.Fatalf("test setup refine MV = %+v, want row +8", refined)
	}
	if refinedCost <= centerCost {
		t.Fatalf("test setup refine cost = %d, want worse than center cost %d", refinedCost, centerCost)
	}

	got, gotCost := searcher.steppedDiamond(
		center,
		searcher.walkCost(center, maxInt()),
		interAnalysisSearchConfig{fullPixelFinalRefine: true},
		[]vp8enc.MotionVector{{}},
		4,
	)
	if got != center || gotCost != centerCost {
		t.Fatalf("final refine selected %+v/%d, want original %+v/%d when refine return cost is worse", got, gotCost, center, centerCost)
	}
}

func TestSelectInterFrameFullPixelMotionVectorUsesImprovedStartAndBestRefMVCost(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 17, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((41 + row*19 + col*31 + row*col*7) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 129, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}

	bestRefMV := vp8enc.MotionVector{}
	start := newInterFrameSearchStart(vp8enc.MotionVector{Row: 32}, 3, -1)
	cfg := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchNstep,
		fullPixelSearchParam:  interFrameMaxMVSearchSteps - 1,
		fullPixelFurtherSteps: 0,
		fullPixelSpeed:        8,
		fullPixelSpeedAdjust:  3,
		improvedMVPrediction:  true,
		fractionalSearch:      interAnalysisFractionalSearchIterative,
	}
	mv, cost := selectInterFrameFullPixelMotionVectorWithSearchStart(sourceImageFromPublic(src), &last.Img, 1, 1, 4, 4, bestRefMV, testInterSearchQIndex, cfg, start)

	if mv != start.mv {
		t.Fatalf("full-pixel MV = %+v, want improved search start %+v", mv, start.mv)
	}
	variance, _ := macroblockLumaMotionVarianceSSE(sourceImageFromPublic(src), &last.Img, 1, 1, mv)
	wantCost := variance + interMotionSearchErrorVectorCost(mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if cost != wantCost {
		t.Fatalf("full-pixel cost = %d, want variance plus best_ref_mv anchored error cost %d", cost, wantCost)
	}
	if legacyCost := interMotionSearchCost(sourceImageFromPublic(src), &last.Img, 1, 1, mv, bestRefMV, testInterSearchQIndex); cost == legacyCost {
		t.Fatalf("full-pixel cost = legacy SAD plus vector cost %d, want variance plus error vector cost", legacyCost)
	}
}

func TestImprovedInterFrameSearchStartUsesLibvpxSADOrderAndStepRange(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 8, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((73 + row*43 + col*17 + row*col*11) & 255)
		}
	}

	analysis := testVP8Frame(t, 64, 64, 211, 90, 170)
	for row := range 16 {
		for col := range 16 {
			srcPixel := src.Y[(row+16)*src.YStride+col+16]
			analysis.Img.Y[(row+16)*analysis.Img.YStride+col] = srcPixel
			analysis.Img.Y[row*analysis.Img.YStride+col+16] = srcPixel ^ 0xff
		}
	}
	e := &VP8Encoder{analysis: analysis}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 8}}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 40}}
	aboveLeft := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: -24}}
	search := interAnalysisSearchConfig{
		fullPixelSearch:       interAnalysisFullPixelSearchHex,
		fullPixelSearchParam:  2,
		fullPixelFurtherSteps: 5,
		fullPixelSpeed:        5,
		fullPixelSpeedAdjust:  2,
		improvedMVPrediction:  true,
	}

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, &above, &left, &aboveLeft, search)
	if !start.ok() || start.mv != left.MV || start.searchRange() != 3 {
		t.Fatalf("improved search start = %+v, want left MV %+v with sr 3", start, left.MV)
	}
	if start.nearSADIndexInt() != 1 {
		t.Fatalf("near_sadidx = %d, want current-frame left slot 1", start.nearSADIndexInt())
	}
	adjusted := search.adjustedForImprovedMVStart(start)
	if int(adjusted.fullPixelSearchParam) != 5 || int(adjusted.fullPixelFurtherSteps) != 2 {
		t.Fatalf("adjusted search = step %d further %d, want step 5 further 2", adjusted.fullPixelSearchParam, adjusted.fullPixelFurtherSteps)
	}

	search.improvedMVPrediction = false
	if disabled := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, &above, &left, &aboveLeft, search); disabled.ok() {
		t.Fatalf("disabled improved search start = %+v, want not set", disabled)
	}
}

func TestImprovedInterFrameSearchStartReadsPreviousInterFrameModes(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 19, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((31 + row*29 + col*13 + row*col*5) & 255)
		}
	}

	last := testVP8Frame(t, 64, 64, 151, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+16)*last.Img.YStride+col+16] = src.Y[(row+16)*src.YStride+col+16]
		}
	}
	e := &VP8Encoder{
		lastRef:                  last,
		lastFrameInterModes:      make([]vp8enc.InterFrameMacroblockMode, 16),
		lastFrameInterModesValid: true,
	}
	e.lastFrameInterModes[1*4+1] = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 56, Col: -8}}
	search := interAnalysisSearchConfig{improvedMVPrediction: true}

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, nil, nil, nil, search)
	if !start.ok() || start.mv != e.lastFrameInterModes[1*4+1].MV || start.searchRange() != 3 {
		t.Fatalf("previous-frame search start = %+v, want %+v with sr 3", start, e.lastFrameInterModes[1*4+1].MV)
	}
	if start.nearSADIndexInt() != 3 {
		t.Fatalf("near_sadidx = %d, want previous-frame current-MB slot 3", start.nearSADIndexInt())
	}
}

func TestImprovedInterFrameSearchStartBiasesCurrentSlots(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 80, 90, 170)
	analysis := testVP8Frame(t, 32, 32, 80, 90, 170)
	e := &VP8Encoder{analysis: analysis, sourceAltRefActive: true}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	left := above
	aboveLeft := above

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, 2, 2, &above, &left, &aboveLeft, interAnalysisSearchConfig{improvedMVPrediction: true})
	if !start.ok() || start.searchRange() != 0 || start.mv != (vp8enc.MotionVector{Col: -16}) {
		t.Fatalf("sign-biased current-frame start = %+v, want median col -16 with sr 0", start)
	}
}

func TestImprovedInterFrameSearchStartBiasesPreviousFrameSlots(t *testing.T) {
	const mbRows, mbCols = 3, 3
	src := testImage(mbCols*16, mbRows*16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, mbCols*16, mbRows*16, 72, 90, 170)
	modes := make([]vp8enc.InterFrameMacroblockMode, mbRows*mbCols)
	for _, index := range []int{
		1*mbCols + 1,
		0*mbCols + 1,
		1*mbCols + 0,
		1*mbCols + 2,
		2*mbCols + 1,
	} {
		modes[index] = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	}
	e := &VP8Encoder{
		lastRef:                  last,
		lastFrameInterModes:      modes,
		lastFrameInterModeBias:   make([]bool, len(modes)),
		lastFrameInterModesValid: true,
		sourceAltRefActive:       true,
	}

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, nil, nil, nil, interAnalysisSearchConfig{improvedMVPrediction: true})
	if !start.ok() || start.searchRange() != 0 || start.mv != (vp8enc.MotionVector{Col: -16}) {
		t.Fatalf("sign-biased previous-frame start = %+v, want median col -16 with sr 0", start)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsFullPixelCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte(21 + ((row*23 + col*7) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+3)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 24}) {
		t.Fatalf("selection = %v %+v, want last row +24 after exhaustive search", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsExhaustiveCornerCandidate(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte(31 + ((row*29 + col*5) & 127))
		}
	}

	last := testVP8Frame(t, 64, 64, 220, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+4)*last.Img.YStride+col+4] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("selection = %v %+v, want last +32,+32 exhaustive candidate", ref.Frame, mv)
	}
}
