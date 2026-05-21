package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestInterMotionModeVectorCostOnlyChargesNewMVDelta(t *testing.T) {
	above := InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       MotionVector{Col: 16},
	}
	newMode := InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       MotionVector{Col: 24},
	}
	signBias := [common.MaxRefFrames]bool{}

	got := InterMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1,
		&tables.DefaultMVContext, nil, nil, RDNewMVBitCostWeight, signBias)
	want := MotionVectorBitCost(newMode.MV, above.MV, &tables.DefaultMVContext,
		RDNewMVBitCostWeight)
	if got != want {
		t.Fatalf("NEWMV vector cost = %d, want delta cost %d", got, want)
	}

	nearest := InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NearestMV,
		MV:       above.MV,
	}
	if got := InterMotionModeVectorCost(&nearest, &above, nil, nil, 0, 0, 1, 1,
		&tables.DefaultMVContext, nil, nil, RDNewMVBitCostWeight, signBias); got != 0 {
		t.Fatalf("NEARESTMV vector cost = %d, want 0", got)
	}

	liveProbs := tables.DefaultMVContext
	liveProbs[1][0] = 1
	liveCost := InterMotionModeVectorCost(&newMode, &above, nil, nil, 0, 0, 1, 1,
		&liveProbs, nil, nil, RDNewMVBitCostWeight, signBias)
	wantLive := MotionVectorBitCost(newMode.MV, above.MV, &liveProbs,
		RDNewMVBitCostWeight)
	if liveCost != wantLive {
		t.Fatalf("live NEWMV vector cost = %d, want live-prob delta cost %d",
			liveCost, wantLive)
	}
	defaultCost := MotionVectorBitCost(newMode.MV, above.MV,
		&tables.DefaultMVContext, RDNewMVBitCostWeight)
	if liveCost == defaultCost {
		t.Fatalf("live NEWMV vector cost = default cost %d, want MV probs to affect RD cost", liveCost)
	}
}

func TestInterMotionModeVectorCostChargesRDNewMVWithLibvpxWeight(t *testing.T) {
	mvProbs := tables.DefaultMVContext
	bestRefMV := MotionVector{Row: 8, Col: -16}
	mode := InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       MotionVector{Row: 24, Col: 8},
	}
	above := InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       bestRefMV,
	}

	got := InterMotionModeVectorCost(&mode, &above, nil, nil, 0, 0, 1, 1,
		&mvProbs, nil, nil, RDNewMVBitCostWeight, [common.MaxRefFrames]bool{})
	want := MotionVectorBitCost(mode.MV, bestRefMV, &mvProbs,
		RDNewMVBitCostWeight)
	if got != want {
		t.Fatalf("RD NEWMV vector cost = %d, want MotionVectorBitCost weight-96 cost %d",
			got, want)
	}
	if fastWeight := MotionVectorBitCost(mode.MV, bestRefMV, &mvProbs,
		FastNewMVBitCostWeight); got == fastWeight {
		t.Fatalf("RD NEWMV vector cost = fast weight-128 cost %d, want weight 96", fastWeight)
	}
}

func TestInterPredictionModeRateMirrorsWriterBranches(t *testing.T) {
	counts := InterModeCounts{Intra: 3, Nearest: 4, Near: 2, Split: 1}
	probs := tables.InterModeContexts
	tests := []struct {
		name string
		mode common.MBPredictionMode
		want int
	}{
		{name: "zero", mode: common.ZeroMV, want: BoolBitCost(probs[3][0], 0)},
		{name: "nearest", mode: common.NearestMV, want: BoolBitCost(probs[3][0], 1) + BoolBitCost(probs[4][1], 0)},
		{name: "near", mode: common.NearMV, want: BoolBitCost(probs[3][0], 1) + BoolBitCost(probs[4][1], 1) + BoolBitCost(probs[2][2], 0)},
		{name: "new", mode: common.NewMV, want: BoolBitCost(probs[3][0], 1) + BoolBitCost(probs[4][1], 1) + BoolBitCost(probs[2][2], 1) + BoolBitCost(probs[1][3], 0)},
		{name: "split", mode: common.SplitMV, want: BoolBitCost(probs[3][0], 1) + BoolBitCost(probs[4][1], 1) + BoolBitCost(probs[2][2], 1) + BoolBitCost(probs[1][3], 1)},
	}
	for _, tt := range tests {
		if got := InterPredictionModeRate(tt.mode, counts); got != tt.want {
			t.Fatalf("%s mode rate = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestMBSplitPartitionRateMirrorsWriterBranches(t *testing.T) {
	tests := []struct {
		partition uint8
		want      int
	}{
		{partition: 3, want: BoolBitCost(tables.MBSplitProbs[0], 0)},
		{partition: 2, want: BoolBitCost(tables.MBSplitProbs[0], 1) + BoolBitCost(tables.MBSplitProbs[1], 0)},
		{partition: 0, want: BoolBitCost(tables.MBSplitProbs[0], 1) + BoolBitCost(tables.MBSplitProbs[1], 1) + BoolBitCost(tables.MBSplitProbs[2], 0)},
		{partition: 1, want: BoolBitCost(tables.MBSplitProbs[0], 1) + BoolBitCost(tables.MBSplitProbs[1], 1) + BoolBitCost(tables.MBSplitProbs[2], 1)},
	}
	for _, tt := range tests {
		if got := MBSplitPartitionRate(tt.partition); got != tt.want {
			t.Fatalf("partition %d rate = %d, want %d", tt.partition, got, tt.want)
		}
	}
}

func TestSplitMotionModeVectorCostChargesPartitionAndNew4x4Weight(t *testing.T) {
	mode := InterFrameMacroblockMode{
		RefFrame:  common.LastFrame,
		Mode:      common.SplitMV,
		Partition: 2,
	}
	fillTestSplitSubset(&mode, 0, MotionVector{Col: 16}, common.New4x4)
	fillTestSplitSubset(&mode, 1, MotionVector{Row: 16}, common.New4x4)
	fillTestSplitSubset(&mode, 2, MotionVector{Col: -16}, common.New4x4)
	fillTestSplitSubset(&mode, 3, MotionVector{Row: -16}, common.New4x4)

	mvProbs := tables.DefaultMVContext
	best := MotionVector{Col: 8}
	want := MBSplitPartitionRate(mode.Partition)
	partitions := int(tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition][subset])
		target := mode.BlockMV[block]
		want += SplitSubMotionLabelRate(common.New4x4, nil)
		delta := MotionVector{
			Row: int16(int(target.Row) - int(best.Row)),
			Col: int16(int(target.Col) - int(best.Col)),
		}
		want += MotionVectorBitCost(delta, MotionVector{}, &mvProbs, 102)
	}

	defaultCost := SplitMotionModeVectorCost(&mode, nil, nil, best,
		&mvProbs, nil, nil)
	if defaultCost != want {
		t.Fatalf("split vector cost = %d, want partition + NEW4X4 weight-102 cost %d",
			defaultCost, want)
	}

	liveProbs := mvProbs
	liveProbs[1][0] = 1
	if liveCost := SplitMotionModeVectorCost(&mode, nil, nil, best,
		&liveProbs, nil, nil); liveCost == defaultCost {
		t.Fatalf("live split vector cost = default cost %d, want MV probs to affect SPLITMV sub-vector cost",
			liveCost)
	}
}

func TestSplitMotionModeVectorCostUsesExplicitSubMVLabel(t *testing.T) {
	left := InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       MotionVector{Col: 16},
	}
	mode := InterFrameMacroblockMode{
		RefFrame:  common.LastFrame,
		Mode:      common.SplitMV,
		Partition: 0,
	}
	fillTestSplitSubset(&mode, 0, left.MV, common.New4x4)
	fillTestSplitSubset(&mode, 1, left.MV, common.Left4x4)
	mode.MV = mode.BlockMV[15]
	mvProbs := tables.DefaultMVContext

	newCost := SplitMotionModeVectorCost(&mode, &left, nil, MotionVector{},
		&mvProbs, nil, nil)
	mode.BModes[0] = common.Left4x4
	leftCost := SplitMotionModeVectorCost(&mode, &left, nil, MotionVector{},
		&mvProbs, nil, nil)

	if newCost <= leftCost {
		t.Fatalf("explicit NEW4X4 cost = %d, want greater than LEFT4X4 cost %d for same MV",
			newCost, leftCost)
	}
}

func fillTestSplitSubset(mode *InterFrameMacroblockMode, subset int,
	mv MotionVector, firstMode common.BPredictionMode,
) {
	fillCount := int(tables.MBSplitFillCount[mode.Partition])
	fillStart := subset * fillCount
	for i := range fillCount {
		block := int(tables.MBSplitFillOffset[mode.Partition][fillStart+i])
		mode.BlockMV[block] = mv
		if i == 0 {
			mode.BModes[block] = firstMode
		} else {
			mode.BModes[block] = common.Left4x4
		}
	}
	mode.MV = mode.BlockMV[15]
}
