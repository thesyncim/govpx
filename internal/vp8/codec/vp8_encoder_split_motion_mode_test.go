package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestSplitMotionSearchSeedsFrom8x8UsesLibvpxBlocks(t *testing.T) {
	mode := vp8enc.InterFrameMacroblockMode{
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	mode.BlockMV[0] = vp8enc.MotionVector{Col: 16}
	mode.BlockMV[2] = vp8enc.MotionVector{Col: 24}
	mode.BlockMV[8] = vp8enc.MotionVector{Row: 32}
	mode.BlockMV[10] = vp8enc.MotionVector{Row: 40}

	seeds := splitMotionSearchSeedsFrom8x8(&mode)

	if !seeds.valid {
		t.Fatalf("8x8 seeds are not valid")
	}
	want := [4]vp8enc.MotionVector{mode.BlockMV[0], mode.BlockMV[2], mode.BlockMV[8], mode.BlockMV[10]}
	if seeds.mv != want {
		t.Fatalf("seeds = %+v, want %+v", seeds.mv, want)
	}
	if seeds.step8x16 != [2]int8{5, 5} || seeds.step16x8 != [2]int8{7, 7} {
		t.Fatalf("seed steps 8x16=%v 16x8=%v, want [5 5] and [7 7]", seeds.step8x16, seeds.step16x8)
	}
}

func TestCollectInterFrameMotionCandidatesIncludesNearestAndNear(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 80, 90, 170)
	ref := testVP8Frame(t, 16, 16, 80, 90, 170)
	refs := []interAnalysisReference{{Frame: vp8common.LastFrame, Img: &ref.Img}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 8}}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Row: 8}}
	aboveLeft := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate

	count := collectInterFrameMotionCandidates(sourceImageFromPublic(src), refs, len(refs), 0, 0, 1, 1, testInterSearchQIndex, &above, &left, &aboveLeft, &vp8tables.DefaultMVContext, &candidates)

	if count != 3 {
		t.Fatalf("candidate count = %d, want zero, nearest, near", count)
	}
	want := [...]vp8enc.MotionVector{{}, {Col: 8}, {Row: 8}}
	for i := range want {
		if candidates[i].MV != want[i] {
			t.Fatalf("candidate[%d] MV = %+v, want %+v", i, candidates[i].MV, want[i])
		}
	}
}

func TestSelectInterFrameSplitBlockFullPixelMotionVectorUsesSearchCenter(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*53 + col*97 + row*col*29 + col*col*7) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 4, 4, 4, 0, 12)
	ref.ExtendBorders()

	bestRefMV := vp8enc.MotionVector{}
	reusedCenter := vp8enc.MotionVector{Col: 64}
	mv, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 4, 4, reusedCenter, bestRefMV, 0, 5, false, &vp8tables.DefaultMVContext)
	noReuseMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 4, 4, bestRefMV, bestRefMV, 0, 5, false, &vp8tables.DefaultMVContext)
	bestQualityMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(sourceImageFromPublic(src), &ref.Img, 0, 0, 1, 4, 4, bestRefMV, bestRefMV, 0)

	if mv != (vp8enc.MotionVector{Col: 96}) {
		t.Fatalf("search-centered split MV = %+v, want col +96", mv)
	}
	if noReuseMV == mv {
		t.Fatalf("zero-centered search unexpectedly reached %+v; test no longer proves predictor reuse", mv)
	}
	if bestQualityMV != (vp8enc.MotionVector{Col: 96}) {
		t.Fatalf("best-quality full-search fallback MV = %+v, want col +96", bestQualityMV)
	}
}

func TestSelectInterFrameSplitBlockFullPixelMotionVectorUsesStepParam(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*67 + col*43 + row*col*19 + col*col*5) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 4, 4, 0, 2)
	ref.ExtendBorders()

	source := sourceImageFromPublic(src)
	stepTwoMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(source, &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, 6, false, &vp8tables.DefaultMVContext)
	stepOneMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(source, &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, 7, false, &vp8tables.DefaultMVContext)

	if stepTwoMV != (vp8enc.MotionVector{Col: 16}) {
		t.Fatalf("step_param 6 MV = %+v, want col +16", stepTwoMV)
	}
	if stepOneMV == stepTwoMV {
		t.Fatalf("step_param 7 reached %+v; want smaller diamond window than step_param 6", stepOneMV)
	}
}

func TestSelectInterFrameSplitBlockFullPixelMotionVectorReturnsMVErrorCost(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*83 + col*41 + row*col*23 + row*row*3) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 4, 4, 2, 1)
	ref.ExtendBorders()

	source := sourceImageFromPublic(src)
	bestRefMV := vp8enc.MotionVector{Row: -10, Col: 6}
	mv, gotCost := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(source, &ref.Img, 0, 0, 0, 4, 4, vp8enc.MotionVector{}, bestRefMV, testInterSearchQIndex, 6, false, &vp8tables.DefaultMVContext)

	if int(mv.Row)&7 != 0 || int(mv.Col)&7 != 0 {
		t.Fatalf("split full-pel MV = %+v, want full-pel aligned", mv)
	}
	wantCost, ok := interMotionSplitBlockFullPixelReturnCost(source, &ref.Img, 0, 0, 0, 4, 4, mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if !ok {
		t.Fatalf("interMotionSplitBlockFullPixelReturnCost returned ok=false")
	}
	if gotCost != wantCost {
		t.Fatalf("split full-pel return cost = %d, want variance+mv_err_cost %d", gotCost, wantCost)
	}
	if walkCost := interMotionSplitBlockSearchCost(source, &ref.Img, 0, 0, 0, 4, 4, mv, bestRefMV, testInterSearchQIndex); gotCost == walkCost {
		t.Fatalf("test setup did not distinguish return cost from SAD walk cost: %d", gotCost)
	}
}

func TestSplitFullPelSearchCtxMatchesSplitBlockSAD(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((17 + row*37 + col*59 + row*col*11) & 255)
		}
	}
	ref.ExtendBorders()
	source := sourceImageFromPublic(src)

	tests := []struct {
		name          string
		mbRow, mbCol  int
		block         int
		width, height int
		mvs           []vp8enc.MotionVector
	}{
		{
			name:   "16x8",
			width:  16,
			height: 8,
			mvs: []vp8enc.MotionVector{
				{},
				{Row: 8, Col: -8},
				{Row: -8, Col: 16},
			},
		},
		{
			name:   "8x16",
			width:  8,
			height: 16,
			mvs: []vp8enc.MotionVector{
				{},
				{Row: 16, Col: -8},
				{Row: -8, Col: 8},
			},
		},
		{
			name:   "8x8",
			width:  8,
			height: 8,
			mvs: []vp8enc.MotionVector{
				{},
				{Row: 8, Col: 16},
				{Row: -8, Col: -8},
			},
		},
		{
			name:   "4x4",
			block:  5,
			width:  4,
			height: 4,
			mvs: []vp8enc.MotionVector{
				{},
				{Row: 8, Col: 8},
				{Row: -8, Col: 16},
			},
		},
		{
			name:   "partial source edge",
			mbRow:  1,
			mbCol:  1,
			block:  15,
			width:  8,
			height: 8,
			mvs: []vp8enc.MotionVector{
				{},
				{Row: -8, Col: -8},
			},
		},
	}
	for _, tt := range tests {
		ctx := newSplitFullPelSearchCtx(source, &ref.Img, tt.mbRow, tt.mbCol, tt.block, tt.width, tt.height)
		for _, mv := range tt.mvs {
			got := ctx.fullPelSADFull(int(mv.Row)>>3, int(mv.Col)>>3)
			want := splitBlockSAD(source, &ref.Img, tt.mbRow, tt.mbCol, tt.block, tt.width, tt.height, mv)
			if got != want {
				t.Fatalf("%s mv=%+v ctx SAD = %d, want %d", tt.name, mv, got, want)
			}
		}
	}
}

func TestSplitFullPelSearchCtxCostMatchesGenericCost(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	ref.ExtendBorders()
	source := sourceImageFromPublic(src)
	bestRefMV := vp8enc.MotionVector{Row: -10, Col: 15}
	ctx := newSplitFullPelSearchCtx(source, &ref.Img, 0, 0, 0, 8, 8)
	for _, mv := range []vp8enc.MotionVector{
		{},
		{Row: 8, Col: -16},
		{Row: -24, Col: 32},
	} {
		got := ctx.fullPelCostFull(int(mv.Row)>>3, int(mv.Col)>>3, int(bestRefMV.Row)>>3, int(bestRefMV.Col)>>3, testInterSearchQIndex)
		want := interMotionSplitBlockSearchCost(source, &ref.Img, 0, 0, 0, 8, 8, mv, bestRefMV, testInterSearchQIndex)
		if got != want {
			t.Fatalf("mv=%+v ctx cost = %d, want %d", mv, got, want)
		}
	}
}

func TestSplitMotionSubsetSearchCenterMatchesLibvpxSeedReuse(t *testing.T) {
	bestRefMV := vp8enc.MotionVector{Row: 8, Col: -16}
	mode := vp8enc.InterFrameMacroblockMode{Partition: 3}
	mode.BlockMV[0] = vp8enc.MotionVector{Col: 64}
	mode.BlockMV[4] = vp8enc.MotionVector{Row: 32}
	seeds := splitMotionSearchSeeds{
		valid: true,
		mv: [4]vp8enc.MotionVector{
			{Col: 16},
			{Col: 24},
			{Row: 32},
			{Row: 40},
		},
	}

	if got := splitMotionSubsetSearchCenter(1, 0, &mode, bestRefMV, 1, &seeds); got != seeds.mv[0] {
		t.Fatalf("8x16 subset 0 search center = %+v, want 8x8 seed %+v", got, seeds.mv[0])
	}
	if got := splitMotionSubsetSearchCenter(1, 1, &mode, bestRefMV, 1, &seeds); got != seeds.mv[1] {
		t.Fatalf("8x16 subset 1 search center = %+v, want 8x8 seed %+v", got, seeds.mv[1])
	}
	if got := splitMotionSubsetSearchCenter(0, 0, &mode, bestRefMV, 1, &seeds); got != seeds.mv[0] {
		t.Fatalf("16x8 subset 0 search center = %+v, want 8x8 seed %+v", got, seeds.mv[0])
	}
	if got := splitMotionSubsetSearchCenter(0, 1, &mode, bestRefMV, 1, &seeds); got != seeds.mv[2] {
		t.Fatalf("16x8 subset 1 search center = %+v, want 8x8 seed %+v", got, seeds.mv[2])
	}
	if got := splitMotionSubsetSearchCenter(3, 0, &mode, bestRefMV, 1, &seeds); got != seeds.mv[0] {
		t.Fatalf("4x4 subset 0 search center = %+v, want 8x8 seed %+v", got, seeds.mv[0])
	}
	if got := splitMotionSubsetSearchCenter(3, 1, &mode, bestRefMV, 1, &seeds); got != mode.BlockMV[0] {
		t.Fatalf("subset 1 search center = %+v, want left block %+v", got, mode.BlockMV[0])
	}
	if got := splitMotionSubsetSearchCenter(3, 8, &mode, bestRefMV, 1, &seeds); got != mode.BlockMV[4] {
		t.Fatalf("subset 8 search center = %+v, want above block %+v", got, mode.BlockMV[4])
	}
	if got := splitMotionSubsetSearchCenter(1, 1, &mode, bestRefMV, 0, &seeds); got != bestRefMV {
		t.Fatalf("best-quality search center = %+v, want bestRefMV %+v", got, bestRefMV)
	}
}

func BenchmarkSplitBlockFullPelNstep8x8(b *testing.B) {
	src, ref := splitMotionSourceAndReference(b)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*29 + col*47 + row*col*13 + 19) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 8, 8, 1, 2)
	ref.ExtendBorders()
	source := sourceImageFromPublic(src)
	mvProbs := vp8tables.DefaultMVContext
	var mvCosts vp8enc.MotionVectorCostTables
	mvCosts.Build(&mvProbs)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mv, cost := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStepWithErrorPerBitAndCostTables(source, &ref.Img, 0, 0, 0, 8, 8, vp8enc.MotionVector{}, vp8enc.MotionVector{}, testInterSearchQIndex, vp8enc.ErrorPerBit(testInterSearchQIndex), 6, false, &mvProbs, &mvCosts)
		if int(mv.Row)&7 != 0 || int(mv.Col)&7 != 0 {
			b.Fatalf("split full-pel MV = %+v, want full-pel aligned", mv)
		}
		if cost == maxInt() {
			b.Fatalf("split full-pel cost = maxInt")
		}
	}
}

func TestSelectInterFrameSplitSubsetMotionModeTrialsReusableLabels(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := range 32 {
		copy(src.Y[row*src.YStride:row*src.YStride+32], ref.Img.Y[row*ref.Img.YStride:row*ref.Img.YStride+32])
	}
	ref.ExtendBorders()

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))

	mv, bMode := selectInterFrameSplitSubsetMotionMode(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height, vp8enc.MotionVector{}, testInterSearchQIndex, &left, &above)

	if mv != (vp8enc.MotionVector{}) || bMode != vp8common.Above4x4 {
		t.Fatalf("subset candidate = %+v/%v, want ABOVE4X4 zero-MV reuse", mv, bMode)
	}
}

func TestSelectInterFrameReferenceMotionVectorPrefersCheaperMotionOnTie(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	_, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if mv != (vp8enc.MotionVector{}) {
		t.Fatalf("mv = %+v, want zero MV for equal-SAD candidates", mv)
	}
}

func TestPredictSplitMotionBlock4x4UsesCodedClamp(t *testing.T) {
	ref := testVP8Frame(t, 5, 5, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = 200
		}
		ref.Img.Y[row*ref.Img.YStride+4] = byte(40 + row)
	}
	ref.ExtendBorders()

	var out [16]byte
	if !predictSplitMotionBlock4x4(&ref.Img, 0, 0, 1, vp8enc.MotionVector{}, &out) {
		t.Fatalf("predictSplitMotionBlock4x4 returned false")
	}
	for row := range 4 {
		for col := range 4 {
			want := byte(200)
			if col == 0 {
				want = byte(40 + row)
			}
			if got := out[row*4+col]; got != want {
				t.Fatalf("fullpel coded-clamp out[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
}

func TestSelectInterFrameSplitMotionModeFindsQuadrantMotion(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	ref := testVP8Frame(t, 32, 32, 0, 90, 170)
	for row := range 32 {
		for col := range 32 {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*37 + col*13) & 255)
		}
	}
	copyShifted8x8FromReference(src, &ref.Img, 0, 0, 0, 1)
	copyShifted8x8FromReference(src, &ref.Img, 0, 8, 1, 0)
	copyShifted8x8FromReference(src, &ref.Img, 8, 0, 0, 2)
	copyShifted8x8FromReference(src, &ref.Img, 8, 8, 2, 0)
	ref.ExtendBorders()

	mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 2)

	if !ok {
		t.Fatalf("split mode selection returned false")
	}
	if mode.Mode != vp8common.SplitMV || mode.RefFrame != vp8common.LastFrame || mode.Partition != 2 {
		t.Fatalf("mode = %+v, want LAST/SPLITMV partition 2", mode)
	}
	want := [4]vp8enc.MotionVector{
		{Col: 8},
		{Row: 8},
		{Col: 16},
		{Row: 16},
	}
	for subset, mv := range want {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		if mode.BlockMV[block] != mv {
			t.Fatalf("subset %d block %d MV = %+v, want %+v", subset, block, mode.BlockMV[block], mv)
		}
	}
	if mode.MV != mode.BlockMV[15] {
		t.Fatalf("mode MV = %+v, want last block %+v", mode.MV, mode.BlockMV[15])
	}
}

func TestSelectInterFrameSplitMotionModeWithSearchUses8x8SeedFor8x16(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((row*71 + col*37 + row*col*17 + col*col*11) & 255)
		}
	}
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 8, 16, 0, 9)
	copyShiftedBlockFromReference(src, &ref.Img, 0, 8, 8, 16, 0, 0)
	ref.ExtendBorders()
	seeds := splitMotionSearchSeeds{
		valid: true,
		mv: [4]vp8enc.MotionVector{
			{Col: 64},
			{},
			{Col: 64},
			{},
		},
	}

	mode, ok := selectInterFrameSplitMotionModeWithSearch(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, 0, 1, nil, nil, defaultInterAnalysisSearchConfig(), 1, &seeds, &vp8tables.DefaultMVContext)

	if !ok || mode.Partition != 1 {
		t.Fatalf("mode = %+v ok=%t, want 8x16 SplitMV", mode, ok)
	}
	if mode.BlockMV[0] != (vp8enc.MotionVector{Col: 72}) {
		t.Fatalf("seeded 8x16 left MV = %+v, want col +72", mode.BlockMV[0])
	}
	if mode.BlockMV[2] != (vp8enc.MotionVector{}) {
		t.Fatalf("8x16 right MV = %+v, want zero", mode.BlockMV[2])
	}
}

func TestSelectInterFrameSplitMotionModeFindsAllPartitionShapes(t *testing.T) {
	t.Run("horizontal", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 8, 0, 1)
		copyShiftedBlockFromReference(src, &ref.Img, 8, 0, 16, 8, 2, 0)

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 0)

		if !ok || mode.Partition != 0 {
			t.Fatalf("mode = %+v ok=%t, want partition 0", mode, ok)
		}
		if mode.BlockMV[0] != (vp8enc.MotionVector{Col: 8}) || mode.BlockMV[8] != (vp8enc.MotionVector{Row: 16}) {
			t.Fatalf("partition 0 MVs = %+v/%+v, want col +8 and row +16", mode.BlockMV[0], mode.BlockMV[8])
		}
	})
	t.Run("vertical", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 8, 16, 1, 0)
		copyShiftedBlockFromReference(src, &ref.Img, 0, 8, 8, 16, 0, 2)

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 1)

		if !ok || mode.Partition != 1 {
			t.Fatalf("mode = %+v ok=%t, want partition 1", mode, ok)
		}
		if mode.BlockMV[0] != (vp8enc.MotionVector{Row: 8}) || mode.BlockMV[2] != (vp8enc.MotionVector{Col: 16}) {
			t.Fatalf("partition 1 MVs = %+v/%+v, want row +8 and col +16", mode.BlockMV[0], mode.BlockMV[2])
		}
	})
	t.Run("four-by-four", func(t *testing.T) {
		src, ref := splitMotionSourceAndReference(t)
		var want [16]vp8enc.MotionVector
		for block := range 16 {
			y := (block >> 2) * 4
			x := (block & 3) * 4
			dy := block >> 2
			dx := block & 3
			copyShiftedBlockFromReference(src, &ref.Img, y, x, 4, 4, dy, dx)
			want[block] = vp8enc.MotionVector{Row: int16(dy * 8), Col: int16(dx * 8)}
		}

		mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 3)

		if !ok || mode.Partition != 3 {
			t.Fatalf("mode = %+v ok=%t, want partition 3", mode, ok)
		}
		for block := range want {
			if mode.BlockMV[block] != want[block] {
				t.Fatalf("partition 3 block %d MV = %+v, want %+v", block, mode.BlockMV[block], want[block])
			}
		}
	})
}

func TestSelectInterFrameSplitMotionModeKeepsAllSamePartition(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	copyShiftedBlockFromReference(src, &ref.Img, 0, 0, 16, 16, 1, 2)
	ref.ExtendBorders()

	mode, ok := selectInterFrameSplitMotionMode(sourceImageFromPublic(src), &ref.Img, vp8common.LastFrame, 0, 0, vp8enc.MotionVector{}, testInterSearchQIndex, 0)
	if !ok {
		t.Fatalf("all-same SPLITMV mode returned ok=false")
	}
	if mode.Partition != 0 || mode.MV != (vp8enc.MotionVector{Row: 8, Col: 16}) {
		t.Fatalf("mode partition/MV = %d/%+v, want partition 0 with block15 MV +8,+16", mode.Partition, mode.MV)
	}
	for block, mv := range mode.BlockMV {
		if mv != (vp8enc.MotionVector{Row: 8, Col: 16}) {
			t.Fatalf("block %d MV = %+v, want all-same +8,+16", block, mv)
		}
	}
}
