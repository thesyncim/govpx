package govpx

import (
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

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
