package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

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
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range width {
			srcX := clampEncodeCoord(baseX+col, src.Width)
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
	gatherClampedLumaBlock(sourceImageFromPublic(src), baseY, baseX, width, height, srcScratch[:], 16)
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
	mv, cost, _, _, ok := search.iterative()

	if !ok {
		t.Fatalf("interFrameSubpixelSearch.iterative returned ok=false")
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

func TestInterFrameSubpixelSearchCandidateCount(t *testing.T) {
	if got := interFrameSubpixelSearchCandidateCount(); got != 31 {
		t.Fatalf("subpixel candidate count = %d, want libvpx iterative max 31", got)
	}
}
