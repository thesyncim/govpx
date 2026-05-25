package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestFastZeroMVLastRDAdjustmentMirrorsLibvpxLocalMotionBias(t *testing.T) {
	zero := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	moving := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	intra := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}

	if got := fastZeroMVLastRDAdjustment(0, 2, nil, &zero, nil); got != 80 {
		t.Fatalf("edge adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &moving, &intra); got != 90 {
		t.Fatalf("single local zero adjustment = %d, want 90", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, &zero, &zero, &zero); got != 80 {
		t.Fatalf("three local zero adjustment = %d, want 80", got)
	}
	if got := fastZeroMVLastRDAdjustment(2, 2, nil, &moving, &intra); got != 100 {
		t.Fatalf("moving adjustment = %d, want 100", got)
	}
}

func TestSplitSubMotionLabelSearchCostUsesLibvpxDefaultSubMVRefProb(t *testing.T) {
	const qIndex = 127

	got := splitSubMotionLabelSearchCost(vp8common.Above4x4, qIndex)
	wantRate := vp8enc.SplitSubMotionLabelCost(vp8common.Above4x4, vp8enc.DefaultSubMVRefProbs)
	want := (wantRate*vp8enc.SADPerBit4(qIndex) + 128) >> 8
	if got != want {
		t.Fatalf("ABOVE4X4 search cost = %d, want libvpx default cost %d", got, want)
	}
	contextualRate := vp8enc.SplitSubMotionLabelCost(vp8common.Above4x4, vp8tables.SubMVRefProb3[4])
	contextual := (contextualRate*vp8enc.SADPerBit4(qIndex) + 128) >> 8
	if got == contextual {
		t.Fatalf("ABOVE4X4 search cost matched contextual bitstream cost %d; want libvpx RD default cost", got)
	}
}

func TestSelectInterFrameSplitSubsetMotionModeRanksLabelsByRD(t *testing.T) {
	leftMV := vp8enc.MotionVector{Col: 32}
	aboveMV := leftMV
	const qIndex = 20
	const leftDiff = 64
	const zeroDiff = 0

	src := testImage(32, 32)
	fillImage(src, 128, 128, 128)
	ref := testVP8Frame(t, 32, 32, 255, 128, 128)
	for row := range 4 {
		for col := range 4 {
			ref.Img.Y[row*ref.Img.YStride+col] = byte(128 + zeroDiff)
			ref.Img.Y[row*ref.Img.YStride+col+4] = byte(128 + leftDiff)
		}
	}
	ref.ExtendBorders()

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 3,
	}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV, Partition: 3}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV, Partition: 3}
	for block := range 16 {
		left.BlockMV[block] = leftMV
		above.BlockMV[block] = aboveMV
	}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))
	mv, bMode := selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold(
		sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height,
		vp8enc.MotionVector{}, vp8enc.MotionVector{}, 0, false, qIndex,
		&left, &above, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext, maxInt(),
	)
	if bMode != vp8common.Zero4x4 || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("split-label choice = %v/%+v, want ZERO4X4 by lower RDCOST distortion", bMode, mv)
	}
}
