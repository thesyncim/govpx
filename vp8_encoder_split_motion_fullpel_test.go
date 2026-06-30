package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

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
