package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestComputeSkin8x8BlockNeedsTwoSubBlocksToTrigger(t *testing.T) {
	// (Y=120, U=117, V=150) is a known skin tuple per
	// TestCyclicRefreshStaticClassificationMasksSkinBlocks. Build a 16x16
	// MB where exactly one 8x8 sub-block has the skin tuple and the other
	// three are neutral grey. SKIN_8X8 requires two skin sub-blocks =>
	// this MB is not skin.
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := range 8 {
		for col := range 8 {
			src.Y[row*src.YStride+col] = 120
		}
	}
	uvW := (src.Width + 1) >> 1
	uvH := (src.Height + 1) >> 1
	for row := range 4 {
		for col := range 4 {
			src.U[row*src.UStride+col] = 117
			src.V[row*src.VStride+col] = 150
		}
	}
	if computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 0) {
		t.Fatalf("single skin sub-block should not flag MB as skin under SKIN_8X8")
	}
	// Promote a second sub-block to skin colour: now MB qualifies.
	for row := range 8 {
		for col := 8; col < 16; col++ {
			src.Y[row*src.YStride+col] = 120
		}
	}
	for row := range 4 {
		for col := 4; col < 8; col++ {
			src.U[row*src.UStride+col] = 117
			src.V[row*src.VStride+col] = 150
		}
	}
	if !computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 0) {
		t.Fatalf("two skin sub-blocks should flag MB as skin under SKIN_8X8")
	}
	// Long zero-MV streak forces motion=0 and short-circuits past 60 frames.
	if computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 70) {
		t.Fatalf("consec_zero_last > 60 should suppress skin classification")
	}
}

func TestComputeSkinMapUsesSkin8x8ForSmallFramesAndSkin16x16ForLarge(t *testing.T) {
	makeSkinSrc := func(width int, height int) Image {
		src := testImage(width, height)
		// Y=120, U=117, V=150 is a known skin tuple.
		fillImage(src, 120, 117, 150)
		// Flip the top-left 8x8 Y sub-block of MB(0,0) to non-skin.
		for row := range 8 {
			for col := range 8 {
				src.Y[row*src.YStride+col] = 30
			}
		}
		return src
	}
	// Small frame: SKIN_8X8 with 3 of 4 sub-blocks skin classifies as skin.
	smallSrc := makeSkinSrc(16, 16)
	smallMap := make([]uint8, 1)
	computeSkinMap(sourceImageFromPublic(smallSrc), 1, 1, []uint8{0}, smallMap)
	if smallMap[0] != 1 {
		t.Fatalf("small-frame skin map = %d, want 1 (SKIN_8X8 path with majority skin sub-blocks)", smallMap[0])
	}
	// Width*Height > 352*288 selects SKIN_16X16. Use 384x288 (110592 > 101376).
	largeSrc := makeSkinSrc(384, 288)
	rows, cols := geometry.MacroblockRows(288), geometry.MacroblockCols(384)
	largeMap := make([]uint8, rows*cols)
	consec := make([]uint8, rows*cols)
	computeSkinMap(sourceImageFromPublic(largeSrc), rows, cols, consec, largeMap)
	if largeMap[0] != 1 {
		t.Fatalf("large-frame MB(0,0) skin map = %d, want 1 (SKIN_16X16 centre sample inside skin region)", largeMap[0])
	}
}

func TestUpdateConsecutiveZeroLastWithDotSuppressResetsCheckedMBs(t *testing.T) {
	counters := []uint8{40, 25}
	dotChecked := []bool{true, false}
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	updateConsecutiveZeroLastWithDotSuppress(modes, counters, dotChecked)
	if counters[0] != 0 {
		t.Fatalf("dot-checked counter[0] = %d, want reset to 0", counters[0])
	}
	if counters[1] != 26 {
		t.Fatalf("non-checked counter[1] = %d, want incremented to 26", counters[1])
	}
}
