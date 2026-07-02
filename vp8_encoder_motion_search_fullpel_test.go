package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

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

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, &above, &left, &aboveLeft, search, nil)
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
	if disabled := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, &above, &left, &aboveLeft, search, nil); disabled.ok() {
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

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.LastFrame, 1, 1, 4, 4, nil, nil, nil, search, nil)
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

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, 2, 2, &above, &left, &aboveLeft, interAnalysisSearchConfig{improvedMVPrediction: true}, nil)
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

	start := e.improvedInterFrameSearchStart(sourceImageFromPublic(src), vp8common.AltRefFrame, 1, 1, mbRows, mbCols, nil, nil, nil, interAnalysisSearchConfig{improvedMVPrediction: true}, nil)
	if !start.ok() || start.searchRange() != 0 || start.mv != (vp8enc.MotionVector{Col: -16}) {
		t.Fatalf("sign-biased previous-frame start = %+v, want median col -16 with sr 0", start)
	}
}
