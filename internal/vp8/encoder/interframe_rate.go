package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/rdopt.c inter-mode rate and split-MV
// cost helpers.

const (
	FastNewMVBitCostWeight = 128
	RDNewMVBitCostWeight   = 96
)

func largeMotionCost() int {
	return int(^uint(0)>>1) / 4
}

func InterMotionModeVectorCost(mode *InterFrameMacroblockMode,
	above *InterFrameMacroblockMode, left *InterFrameMacroblockMode,
	aboveLeft *InterFrameMacroblockMode,
	mbRow, mbCol, mbRows, mbCols int,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	subMVRefProbs *[3]uint8,
	newMVWeight int,
	signBias [common.MaxRefFrames]bool,
) int {
	if mode == nil || mode.RefFrame == common.IntraFrame {
		return 0
	}
	if mvProbs == nil && mvCosts == nil {
		return largeMotionCost()
	}
	best := InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame,
		mbRow, mbCol, mbRows, mbCols, signBias)
	if mode.Mode == common.SplitMV {
		return SplitMotionModeVectorCost(mode, left, above, best, mvProbs,
			mvCosts, subMVRefProbs)
	}
	if mode.Mode != common.NewMV {
		return 0
	}
	return InterNewMVVectorCost(mode.MV, best, mvProbs, mvCosts, newMVWeight)
}

func InterMotionModeVectorCostWithBestRef(mode *InterFrameMacroblockMode,
	left *InterFrameMacroblockMode, above *InterFrameMacroblockMode,
	bestRefMV MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	subMVRefProbs *[3]uint8,
	newMVWeight int,
) int {
	if mode == nil || mode.RefFrame == common.IntraFrame {
		return 0
	}
	if mvProbs == nil && mvCosts == nil {
		return largeMotionCost()
	}
	if mode.Mode == common.SplitMV {
		return SplitMotionModeVectorCost(mode, left, above, bestRefMV,
			mvProbs, mvCosts, subMVRefProbs)
	}
	if mode.Mode != common.NewMV {
		return 0
	}
	return InterNewMVVectorCost(mode.MV, bestRefMV, mvProbs, mvCosts,
		newMVWeight)
}

func InterMacroblockSkipRate(prob uint8, skip bool) int {
	if prob == 0 {
		prob = 128
	}
	if skip {
		return BoolBitCost(prob, 1)
	}
	return BoolBitCost(prob, 0)
}

func InterReferenceFrameRate(refFrame common.MVReferenceFrame,
	probLast uint8, probGolden uint8,
) int {
	switch refFrame {
	case common.LastFrame:
		return BoolBitCost(probLast, 0)
	case common.GoldenFrame:
		return BoolBitCost(probLast, 1) + BoolBitCost(probGolden, 0)
	case common.AltRefFrame:
		return BoolBitCost(probLast, 1) + BoolBitCost(probGolden, 1)
	default:
		return 1 << 30
	}
}

func InterPredictionModeRate(mode common.MBPredictionMode, counts InterModeCounts) int {
	probs := tables.InterModeContexts
	const maxCtx = tables.InterModeContextCount - 1
	intra := min(int(counts.Intra), maxCtx)
	nearest := min(int(counts.Nearest), maxCtx)
	near := min(int(counts.Near), maxCtx)
	split := min(int(counts.Split), maxCtx)
	switch mode {
	case common.ZeroMV:
		return BoolBitCost(probs[intra][0], 0)
	case common.NearestMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 0)
	case common.NearMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 1) +
			BoolBitCost(probs[near][2], 0)
	case common.NewMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 1) +
			BoolBitCost(probs[near][2], 1) +
			BoolBitCost(probs[split][3], 0)
	case common.SplitMV:
		return BoolBitCost(probs[intra][0], 1) +
			BoolBitCost(probs[nearest][1], 1) +
			BoolBitCost(probs[near][2], 1) +
			BoolBitCost(probs[split][3], 1)
	default:
		return 1 << 30
	}
}

func SplitMotionModeVectorCost(mode *InterFrameMacroblockMode,
	left *InterFrameMacroblockMode, above *InterFrameMacroblockMode,
	best MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	subMVRefProbs *[3]uint8,
) int {
	if mode.Partition >= tables.NumMBSplits {
		return 1 << 30
	}
	if mvProbs == nil && mvCosts == nil {
		return largeMotionCost()
	}
	cost := MBSplitPartitionRate(mode.Partition)
	partitions := int(tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := SplitLeftMV(mode, left, block)
		aboveMV := SplitAboveMV(mode, above, block)
		target := mode.BlockMV[block&15]
		bMode := mode.BModes[block&15]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return largeMotionCost()
		}
		cost += SplitSubMotionLabelRate(bMode, subMVRefProbs)
		if bMode == common.New4x4 {
			delta := MotionVector{
				Row: int16(int(target.Row) - int(best.Row)),
				Col: int16(int(target.Col) - int(best.Col)),
			}
			cost += SplitMotionVectorCost(delta, mvProbs, mvCosts)
		}
	}
	return cost
}

func SplitSubMotionLabelRate(mode common.BPredictionMode, probs *[3]uint8) int {
	if probs == nil {
		return SplitSubMotionLabelCost(mode, DefaultSubMVRefProbs)
	}
	return SplitSubMotionLabelCost(mode, *probs)
}

func SplitSubMotionLabelCost(mode common.BPredictionMode, probs [3]uint8) int {
	if uint(mode-common.Left4x4) > uint(common.New4x4-common.Left4x4) {
		return largeMotionCost()
	}
	return TreeTokenCost(tables.SubMVRefTree[:], probs[:], int(mode))
}

func MBSplitPartitionRate(partition uint8) int {
	if partition >= tables.NumMBSplits {
		return largeMotionCost()
	}
	return TreeTokenCost(tables.MBSplitTree[:], tables.MBSplitProbs[:],
		int(partition))
}

func InterNewMVVectorCost(mv MotionVector, best MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
	weight int,
) int {
	if mvCosts != nil {
		return mvCosts.BitCost(mv, best, weight)
	}
	if mvProbs == nil {
		return largeMotionCost()
	}
	return MotionVectorBitCost(mv, best, mvProbs, weight)
}

func SplitMotionVectorCost(mv MotionVector,
	mvProbs *[2][tables.MVPCount]uint8,
	mvCosts *MotionVectorCostTables,
) int {
	if mvCosts != nil {
		return mvCosts.BitCost(mv, MotionVector{}, 102)
	}
	if mvProbs == nil {
		return largeMotionCost()
	}
	return MotionVectorBitCost(mv, MotionVector{}, mvProbs, 102)
}
