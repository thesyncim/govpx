package govpx

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
