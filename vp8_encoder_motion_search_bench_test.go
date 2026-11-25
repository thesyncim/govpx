package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"reflect"
	"testing"
)

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

func TestInterFrameMotionSearchDefaultPathHasNoStatsField(t *testing.T) {
	cases := []reflect.Type{
		reflect.TypeFor[interFrameMotionVectorSearch](),
		reflect.TypeFor[interFrameSubpixelSearch](),
	}
	for _, typ := range cases {
		if _, ok := typ.FieldByName("stats"); ok {
			t.Fatalf("%s carries stats field in default search path", typ.Name())
		}
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
