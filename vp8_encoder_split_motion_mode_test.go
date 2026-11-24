package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
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
