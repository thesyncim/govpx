package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestInterPredictionModeRateMatchesTreeCostForAllContexts(t *testing.T) {
	modes := []common.MBPredictionMode{
		common.ZeroMV,
		common.NearestMV,
		common.NearMV,
		common.NewMV,
		common.SplitMV,
	}
	for c0 := range tables.InterModeContextCount {
		for c1 := range tables.InterModeContextCount {
			for c2 := range tables.InterModeContextCount {
				for c3 := range tables.InterModeContextCount {
					counts := InterModeCounts{
						Intra:   uint8(c0),
						Nearest: uint8(c1),
						Near:    uint8(c2),
						Split:   uint8(c3),
					}
					probs := [4]uint8{
						tables.InterModeContexts[c0][0],
						tables.InterModeContexts[c1][1],
						tables.InterModeContexts[c2][2],
						tables.InterModeContexts[c3][3],
					}
					for _, mode := range modes {
						want := expectedInterPredictionModeRate(probs, mode)
						if got := InterPredictionModeRate(mode, counts); got != want {
							t.Fatalf("counts=(%d,%d,%d,%d) mode=%v rate=%d, want %d",
								c0, c1, c2, c3, mode, got, want)
						}
					}
				}
			}
		}
	}
}

func TestInterFrameModeCountsDriveDynamicProbLookup(t *testing.T) {
	zeroCounts := InterFrameModeCounts(nil, nil, nil,
		common.LastFrame, [common.MaxRefFrames]bool{})
	if zeroCounts != (InterModeCounts{}) {
		t.Fatalf("edge counts = %+v, want zero", zeroCounts)
	}

	aboveZeroMV := &InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.ZeroMV,
		MV:       MotionVector{},
	}
	zeroMVCounts := InterFrameModeCounts(aboveZeroMV, nil, nil,
		common.LastFrame, [common.MaxRefFrames]bool{})
	if zeroMVCounts.Intra != 2 {
		t.Fatalf("above-zero-MV counts.Intra = %d, want 2", zeroMVCounts.Intra)
	}

	aboveNewMV := &InterFrameMacroblockMode{
		RefFrame: common.LastFrame,
		Mode:     common.NewMV,
		MV:       MotionVector{Row: 16},
	}
	newMVCounts := InterFrameModeCounts(aboveNewMV, nil, nil,
		common.LastFrame, [common.MaxRefFrames]bool{})
	if newMVCounts.Nearest != 2 {
		t.Fatalf("above-new-MV counts.Nearest = %d, want 2", newMVCounts.Nearest)
	}
	if newMVCounts.Intra != 0 {
		t.Fatalf("above-new-MV counts.Intra = %d, want 0", newMVCounts.Intra)
	}

	zeroMVProb := tables.InterModeContexts[zeroMVCounts.Intra][0]
	newMVProb := tables.InterModeContexts[newMVCounts.Intra][0]
	if zeroMVProb == newMVProb {
		t.Fatalf("prob[0] identical for zero-MV ct=%d and new-MV ct=%d; want dynamic mode-context row lookup",
			zeroMVCounts.Intra, newMVCounts.Intra)
	}
}

func TestZeroMVRateUsesDynamicModeContext(t *testing.T) {
	for ct := range tables.InterModeContextCount {
		counts := InterModeCounts{Intra: uint8(ct)}
		want := BoolBitCost(tables.InterModeContexts[ct][0], 0)
		if got := InterPredictionModeRate(common.ZeroMV, counts); got != want {
			t.Fatalf("ZEROMV cost at counts.Intra=%d = %d, want %d", ct, got, want)
		}
	}

	costNoZeroNeighbor := InterPredictionModeRate(common.ZeroMV, InterModeCounts{Intra: 0})
	costZeroNeighbor := InterPredictionModeRate(common.ZeroMV, InterModeCounts{Intra: 2})
	if costNoZeroNeighbor <= costZeroNeighbor {
		t.Fatalf("ZEROMV cost ct=0 minus ct=2 = %d, want positive dynamic-context gap",
			costNoZeroNeighbor-costZeroNeighbor)
	}
}

func TestInterFrameModeCountsAtFrameOriginAreZero(t *testing.T) {
	for _, refFrame := range []common.MVReferenceFrame{
		common.LastFrame,
		common.GoldenFrame,
		common.AltRefFrame,
	} {
		for _, signBiasGF := range []bool{false, true} {
			for _, signBiasAR := range []bool{false, true} {
				signBias := [common.MaxRefFrames]bool{
					common.GoldenFrame: signBiasGF,
					common.AltRefFrame: signBiasAR,
				}
				counts := InterFrameModeCounts(nil, nil, nil, refFrame, signBias)
				if counts != (InterModeCounts{}) {
					t.Fatalf("ref=%v signBias=(GF=%t,AR=%t) frame-origin counts=%+v, want zero",
						refFrame, signBiasGF, signBiasAR, counts)
				}
			}
		}
	}
}

func TestSplitSubMotionLabelRateMatchesDefaultCostTree(t *testing.T) {
	probs := DefaultSubMVRefProbs
	want := map[common.BPredictionMode]int{
		common.Left4x4:  BoolBitCost(probs[0], 0),
		common.Above4x4: BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 0),
		common.Zero4x4:  BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 0),
		common.New4x4:   BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 1),
	}
	for mode, wantCost := range want {
		if got := SplitSubMotionLabelRate(mode, nil); got != wantCost {
			t.Fatalf("SplitSubMotionLabelRate(%v) = %d, want %d", mode, got, wantCost)
		}
	}
}

func expectedInterPredictionModeRate(probs [4]uint8, mode common.MBPredictionMode) int {
	switch mode {
	case common.ZeroMV:
		return BoolBitCost(probs[0], 0)
	case common.NearestMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 0)
	case common.NearMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 0)
	case common.NewMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 1) + BoolBitCost(probs[3], 0)
	case common.SplitMV:
		return BoolBitCost(probs[0], 1) + BoolBitCost(probs[1], 1) + BoolBitCost(probs[2], 1) + BoolBitCost(probs[3], 1)
	default:
		panic("unknown inter prediction mode")
	}
}

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

func TestInterFrameNstepSearchSitesMirrorLibvpx3StepTable(t *testing.T) {
	sites := InterFrameNstepSearchSites[:]
	if len(sites) != 65 {
		t.Fatalf("nstep search sites = %d, want 65", len(sites))
	}
	wantFirst := [...]MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
		{Row: -128, Col: -128},
		{Row: -128, Col: 128},
		{Row: 128, Col: -128},
		{Row: 128, Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[57] != (MotionVector{Row: -1}) || sites[64] != (MotionVector{Row: 1, Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1,+1", sites[57], sites[64])
	}
}

func TestInterFrameDiamondSearchSitesMirrorLibvpxDSMotionTable(t *testing.T) {
	sites := InterFrameDiamondSearchSites[:]
	if len(sites) != 33 {
		t.Fatalf("diamond search sites = %d, want 33", len(sites))
	}
	wantFirst := [...]MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[29] != (MotionVector{Row: -1}) || sites[32] != (MotionVector{Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1 col", sites[29], sites[32])
	}
}

func TestInterFrameSubpixelMotionVectorInRange(t *testing.T) {
	best := MotionVector{Row: 16, Col: -8}
	if !InterFrameSubpixelMotionVectorInRange(MotionVector{Row: best.Row + 2040, Col: best.Col - 2040}, best) {
		t.Fatalf("max full-pel delta unexpectedly out of range")
	}
	if InterFrameSubpixelMotionVectorInRange(MotionVector{Row: best.Row + 2048, Col: best.Col}, best) {
		t.Fatalf("delta beyond max full-pel range unexpectedly accepted")
	}
	if InterFrameSubpixelMotionVectorInRange(MotionVector{Row: best.Row, Col: best.Col - 2048}, best) {
		t.Fatalf("negative delta beyond max full-pel range unexpectedly accepted")
	}
}

func TestInterFrameSubpelSearchBoundsForFrameOrigin(t *testing.T) {
	got := InterFrameSubpelSearchBoundsFor(MotionVector{}, 0, 0, 45, 80)
	want := InterFrameSubpelSearchBounds{
		RowMin: -64,
		RowMax: subpelMVQuarterPelLongLimit,
		ColMin: -64,
		ColMax: subpelMVQuarterPelLongLimit,
	}
	if got != want {
		t.Fatalf("subpel bounds at frame origin = %+v, want %+v", got, want)
	}
	if !got.Contains(-64, 1023) {
		t.Fatalf("bounds do not contain inclusive edge")
	}
	if got.Contains(-65, 0) || got.Contains(0, 1024) {
		t.Fatalf("bounds contain point outside inclusive edge")
	}
}
