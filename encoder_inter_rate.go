package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func interMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return interMotionModeVectorCostWithNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, libvpxRDNewMVBitCostWeight)
}

func interMotionModeVectorCostWithNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int) int {
	return interMotionModeVectorCostWithNewMVWeightAndSignBias(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, newMVWeight, defaultInterFrameSignBias())
}

func interMotionModeVectorCostWithNewMVWeightAndSignBias(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int, signBias [vp8common.MaxRefFrames]bool) int {
	return interMotionModeVectorCostWithNewMVWeightAndSignBiasAndCosts(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, nil, newMVWeight, signBias)
}

func interMotionModeVectorCostWithNewMVWeightAndSignBiasAndCosts(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, newMVWeight int, signBias [vp8common.MaxRefFrames]bool) int {
	return interMotionModeVectorCostWithNewMVWeightAndSignBiasCostsAndSubMVRefProbs(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, mvCosts, nil, newMVWeight, signBias)
}

func interMotionModeVectorCostWithNewMVWeightAndSignBiasCostsAndSubMVRefProbs(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, subMVRefProbs *[3]uint8, newMVWeight int, signBias [vp8common.MaxRefFrames]bool) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	if mvProbs == nil && mvCosts == nil {
		return maxInt() / 4
	}
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame, mbRow, mbCol, mbRows, mbCols, signBias)
	if mode.Mode == vp8common.SplitMV {
		if mvCosts != nil {
			return splitMotionModeVectorCostWithCostTablesAndSubMVRefProbs(mode, left, above, best, mvCosts, subMVRefProbs)
		}
		return splitMotionModeVectorCostWithSubMVRefProbs(mode, left, above, best, mvProbs, subMVRefProbs)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	if mvCosts != nil {
		return interNewMVVectorCostWithCostTables(mode.MV, best, mvCosts, newMVWeight)
	}
	return interNewMVVectorCost(mode.MV, best, mvProbs, newMVWeight)
}

func interMotionModeVectorCostWithBestRefMV(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, bestRefMV vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int) int {
	return interMotionModeVectorCostWithBestRefMVAndCosts(mode, left, above, bestRefMV, mvProbs, nil, newMVWeight)
}

func interMotionModeVectorCostWithBestRefMVAndCosts(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, bestRefMV vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, newMVWeight int) int {
	return interMotionModeVectorCostWithBestRefMVCostsAndSubMVRefProbs(mode, left, above, bestRefMV, mvProbs, mvCosts, nil, newMVWeight)
}

func interMotionModeVectorCostWithBestRefMVCostsAndSubMVRefProbs(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, bestRefMV vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, subMVRefProbs *[3]uint8, newMVWeight int) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	if mvProbs == nil && mvCosts == nil {
		return maxInt() / 4
	}
	if mode.Mode == vp8common.SplitMV {
		if mvCosts != nil {
			return splitMotionModeVectorCostWithCostTablesAndSubMVRefProbs(mode, left, above, bestRefMV, mvCosts, subMVRefProbs)
		}
		return splitMotionModeVectorCostWithSubMVRefProbs(mode, left, above, bestRefMV, mvProbs, subMVRefProbs)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	if mvCosts != nil {
		return interNewMVVectorCostWithCostTables(mode.MV, bestRefMV, mvCosts, newMVWeight)
	}
	return interNewMVVectorCost(mode.MV, bestRefMV, mvProbs, newMVWeight)
}

func interMacroblockSkipRate(skip bool) int {
	return interMacroblockSkipRateWithProb(128, skip)
}

func interMacroblockSkipRateWithProb(prob uint8, skip bool) int {
	if prob == 0 {
		prob = 128
	}
	if skip {
		return boolBitCost(prob, 1)
	}
	return boolBitCost(prob, 0)
}

func (e *VP8Encoder) interMacroblockSkipRate(skip bool) int {
	return interMacroblockSkipRateWithProb(e.probSkipFalse, skip)
}

func (e *VP8Encoder) interIntraReferenceRate() int {
	if e.threadedHelperRowsActive {
		return 0
	}
	return boolBitCost(e.refProbIntra, 0)
}

func (e *VP8Encoder) interInterReferenceRate(refRate int) int {
	if e.threadedHelperRowsActive {
		return 0
	}
	return boolBitCost(e.refProbIntra, 1) + refRate
}

// interIntraMacroblockModeRate models libvpx vp8_calc_ref_frame_costs for the
// intra-coded ref-frame branch: skip-bit + intra/inter selector with the
// previous-frame prob_intra_coded.
func (e *VP8Encoder) interIntraMacroblockModeRate() int {
	return e.interMacroblockSkipRate(false) + e.interIntraReferenceRate()
}

func (e *VP8Encoder) interMotionModeRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return e.interIntraReferenceRate()
	}
	return e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, e.interReferenceFrameRate(mode.RefFrame))
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxRDNewMVBitCostWeight)
}

func (e *VP8Encoder) fastInterMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxFastNewMVBitCostWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return e.interIntraReferenceRate()
	}
	signBias := e.interFrameSignBias()
	return e.interInterReferenceRate(refRate) +
		interPredictionModeRate(mode.Mode, vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame, signBias)) +
		interMotionModeVectorCostWithNewMVWeightAndSignBiasCostsAndSubMVRefProbs(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, &e.modeProbs.MV, e.currentMotionVectorCostTables(), &e.subMVRefProbs, newMVWeight, signBias)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndModeContext(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, refRate int, modeCounts vp8enc.InterModeCounts, bestRefMV vp8enc.MotionVector, newMVWeight int) int {
	return e.interMotionModeRateWithReferenceRateAndModeContextAndCosts(mode, left, above, refRate, modeCounts, bestRefMV, e.currentMotionVectorCostTables(), newMVWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndModeContextAndCosts(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, refRate int, modeCounts vp8enc.InterModeCounts, bestRefMV vp8enc.MotionVector, mvCosts *vp8enc.MotionVectorCostTables, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return e.interIntraReferenceRate()
	}
	return e.interInterReferenceRate(refRate) +
		interPredictionModeRate(mode.Mode, modeCounts) +
		interMotionModeVectorCostWithBestRefMVCostsAndSubMVRefProbs(mode, left, above, bestRefMV, &e.modeProbs.MV, mvCosts, &e.subMVRefProbs, newMVWeight)
}

// interReferenceFrameRate ports libvpx vp8_calc_ref_frame_costs (bitstream.c):
// the LAST/GOLDEN/ALTREF tree uses the previous-frame prob_last_coded and
// prob_gf_coded, NOT a per-frame static 128.
func (e *VP8Encoder) interReferenceFrameRate(refFrame vp8common.MVReferenceFrame) int {
	if e.threadedHelperRowsActive {
		return 0
	}
	return interReferenceFrameRateWithProbs(refFrame, e.refProbLast, e.refProbGolden)
}

func (e *VP8Encoder) interReferenceFrameRateForReference(ref interAnalysisReference) int {
	if e.threadedHelperRowsActive {
		return 0
	}
	if ref.RefRateSet {
		return ref.RefRate
	}
	return e.interReferenceFrameRate(ref.Frame)
}

func (e *VP8Encoder) interReferenceFrameRatesForFlags(flags EncodeFlags) (last int, golden int, alt int) {
	probLast := e.refProbLast
	probGolden := e.refProbGolden
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	temporalSingleRef := e.interReferenceFrameRatesUseTemporalSingleRefSpecialCase()
	switch {
	case lastEnabled && !goldenEnabled && !altEnabled:
		probLast = 255
		probGolden = 128
	case temporalSingleRef && !lastEnabled && goldenEnabled && !altEnabled:
		probLast = 1
		probGolden = 255
	case temporalSingleRef && !lastEnabled && !goldenEnabled && altEnabled:
		probLast = 1
		probGolden = 1
	}
	return interReferenceFrameRateWithProbs(vp8common.LastFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.GoldenFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.AltRefFrame, probLast, probGolden)
}

func (e *VP8Encoder) interReferenceFrameRatesUseTemporalSingleRefSpecialCase() bool {
	if !e.opts.TemporalScalability.Enabled {
		return false
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	return ok && pattern.Layers > 1
}

func interReferenceFrameRateWithProbs(refFrame vp8common.MVReferenceFrame, probLast uint8, probGolden uint8) int {
	switch refFrame {
	case vp8common.LastFrame:
		return boolBitCost(probLast, 0)
	case vp8common.GoldenFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 0)
	case vp8common.AltRefFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 1)
	default:
		return 1 << 30
	}
}

func interPredictionModeRate(mode vp8common.MBPredictionMode, counts vp8enc.InterModeCounts) int {
	probs := vp8tables.InterModeContexts
	// Clamp the four context counts to [0, InterModeContextCount-1=5]
	// once at function entry. counts in practice never exceed this
	// (counts accumulate at most a few units per MB context), so the
	// min() is a no-op functionally but lets the compiler elide the
	// per-branch bounds check on the [6][4]uint8 InterModeContexts load.
	const maxCtx = vp8tables.InterModeContextCount - 1
	intra := min(int(counts.Intra), maxCtx)
	nearest := min(int(counts.Nearest), maxCtx)
	near := min(int(counts.Near), maxCtx)
	split := min(int(counts.Split), maxCtx)
	switch mode {
	case vp8common.ZeroMV:
		return boolBitCost(probs[intra][0], 0)
	case vp8common.NearestMV:
		return boolBitCost(probs[intra][0], 1) +
			boolBitCost(probs[nearest][1], 0)
	case vp8common.NearMV:
		return boolBitCost(probs[intra][0], 1) +
			boolBitCost(probs[nearest][1], 1) +
			boolBitCost(probs[near][2], 0)
	case vp8common.NewMV:
		return boolBitCost(probs[intra][0], 1) +
			boolBitCost(probs[nearest][1], 1) +
			boolBitCost(probs[near][2], 1) +
			boolBitCost(probs[split][3], 0)
	case vp8common.SplitMV:
		return boolBitCost(probs[intra][0], 1) +
			boolBitCost(probs[nearest][1], 1) +
			boolBitCost(probs[near][2], 1) +
			boolBitCost(probs[split][3], 1)
	default:
		return 1 << 30
	}
}

func splitMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return splitMotionModeVectorCostWithSubMVRefProbs(mode, left, above, best, mvProbs, nil)
}

func splitMotionModeVectorCostWithSubMVRefProbs(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, subMVRefProbs *[3]uint8) int {
	if mode.Partition >= vp8tables.NumMBSplits {
		return 1 << 30
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	cost := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(vp8tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := analysisSplitLeftMV(mode, left, block)
		aboveMV := analysisSplitAboveMV(mode, above, block)
		// block is read from MBSplitOffset whose cells are uint8 in
		// [0,16); mode.BlockMV and mode.BModes are both [16]-sized. The
		// AND-mask is a no-op functionally but elides the bounds check.
		target := mode.BlockMV[block&15]
		bMode := mode.BModes[block&15]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return maxInt() / 4
		}
		cost += splitSubMotionLabelRateWithProbs(bMode, subMVRefProbs)
		if bMode == vp8common.New4x4 {
			delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
			cost += splitMotionVectorCost(delta, mvProbs)
		}
	}
	return cost
}

func splitMotionModeVectorCostWithCostTables(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvCosts *vp8enc.MotionVectorCostTables) int {
	return splitMotionModeVectorCostWithCostTablesAndSubMVRefProbs(mode, left, above, best, mvCosts, nil)
}

func splitMotionModeVectorCostWithCostTablesAndSubMVRefProbs(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvCosts *vp8enc.MotionVectorCostTables, subMVRefProbs *[3]uint8) int {
	if mode.Partition >= vp8tables.NumMBSplits {
		return 1 << 30
	}
	if mvCosts == nil {
		return maxInt() / 4
	}
	cost := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition&3])
	for subset := range partitions {
		block := int(vp8tables.MBSplitOffset[mode.Partition&3][subset&15])
		leftMV := analysisSplitLeftMV(mode, left, block)
		aboveMV := analysisSplitAboveMV(mode, above, block)
		target := mode.BlockMV[block&15]
		bMode := mode.BModes[block&15]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return maxInt() / 4
		}
		cost += splitSubMotionLabelRateWithProbs(bMode, subMVRefProbs)
		if bMode == vp8common.New4x4 {
			delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
			cost += splitMotionVectorCostWithCostTables(delta, mvCosts)
		}
	}
	return cost
}

var libvpxDefaultSubMVRefProbs = [3]uint8{180, 162, 25}

func splitSubMotionLabelRate(mode vp8common.BPredictionMode) int {
	// libvpx RD uses x->inter_bmode_costs, built from fc.sub_mv_ref_prob,
	// while bitstream writing uses context-specific sub-MV probabilities.
	return splitSubMotionLabelCostWithProbs(mode, libvpxDefaultSubMVRefProbs)
}

func splitSubMotionLabelRateWithProbs(mode vp8common.BPredictionMode, probs *[3]uint8) int {
	if probs == nil {
		return splitSubMotionLabelRate(mode)
	}
	return splitSubMotionLabelCostWithProbs(mode, *probs)
}

func splitSubMotionLabelCostWithProbs(mode vp8common.BPredictionMode, probs [3]uint8) int {
	// Single unsigned-range check; the prior two-branch form left the
	// function 2 cost-points over the inliner budget.
	if uint(mode-vp8common.Left4x4) > uint(vp8common.New4x4-vp8common.Left4x4) {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.SubMVRefTree[:], probs[:], int(mode))
}

func splitSubMotionLabelMatchesMV(mode vp8common.BPredictionMode, target vp8enc.MotionVector, left vp8enc.MotionVector, above vp8enc.MotionVector) bool {
	switch mode {
	case vp8common.Left4x4:
		return target == left
	case vp8common.Above4x4:
		return above != left && target == above
	case vp8common.Zero4x4:
		return target.IsZero()
	case vp8common.New4x4:
		return true
	default:
		return false
	}
}

func mbSplitPartitionRate(partition uint8) int {
	if partition >= vp8tables.NumMBSplits {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.MBSplitTree[:], vp8tables.MBSplitProbs[:], int(partition))
}

func analysisSplitLeftMV(cur *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	// BlockMV is [16]MotionVector. The guards keep every indexed access
	// in [0,16); AND-mask with 15 elides the bounds checks.
	if block&3 == 0 {
		if left == nil {
			return vp8enc.MotionVector{}
		}
		if left.Mode == vp8common.SplitMV {
			return left.BlockMV[(block+3)&15]
		}
		return left.MV
	}
	return cur.BlockMV[(block-1)&15]
}

func analysisSplitAboveMV(cur *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	// BlockMV is [16]MotionVector. The guards keep every indexed access
	// in [0,16); AND-mask with 15 elides the bounds checks.
	if block>>2 == 0 {
		if above == nil {
			return vp8enc.MotionVector{}
		}
		if above.Mode == vp8common.SplitMV {
			return above.BlockMV[(block+12)&15]
		}
		return above.MV
	}
	return cur.BlockMV[(block-4)&15]
}

func interNewMVVectorCost(mv vp8enc.MotionVector, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, weight int) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, best, mvProbs, weight)
}

func interNewMVVectorCostWithCostTables(mv vp8enc.MotionVector, best vp8enc.MotionVector, mvCosts *vp8enc.MotionVectorCostTables, weight int) int {
	if mvCosts == nil {
		return maxInt() / 4
	}
	return mvCosts.BitCost(mv, best, weight)
}

func splitMotionVectorCost(mv vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, mvProbs, 102)
}

func splitMotionVectorCostWithCostTables(mv vp8enc.MotionVector, mvCosts *vp8enc.MotionVectorCostTables) int {
	if mvCosts == nil {
		return maxInt() / 4
	}
	return mvCosts.BitCost(mv, vp8enc.MotionVector{}, 102)
}

func macroblockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvCol := int(mv.Col)
	mvRow := int(mv.Row)
	refBaseY := baseY + (mvRow >> 3)
	refBaseX := baseX + (mvCol >> 3)
	if (mvCol|mvRow)&7 == 0 &&
		uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SAD16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
		}
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, maxInt())
}

func macroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	// Uint range collapses (base >= 0) and (base+16 <= dim) into one
	// compare per dimension (works when dim >= 16; smaller dims fall
	// through to the per-pixel clamped path).
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		} else {
			var srcScratch [16 * 16]byte
			gatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if sse, ok := macroblockSubpixelSSEBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return sse
			}
		}
	}
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SSE16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
		}
	}

	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}

func macroblockLumaMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		} else {
			var srcScratch [16 * 16]byte
			gatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if variance, sse, ok := macroblockSubpixelVarianceBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return variance, sse
			}
		}
	}
	// Uint range collapses (baseY/X >= 0) and (baseY/X+16 <= dim) into
	// one compare each, matching the pattern in macroblockLumaSSE.
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride)
			return sse - ((sum * sum) >> 8), sse
		}
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

// macroblockSADLimited dispatches the limit-aware 16x16 SAD between the
// full-pel bordered-reference SIMD kernel, the sub-pel six-tap predict path,
// and the scalar fallback for invalid buffers / non-UMV callers.
func macroblockSADLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvCol := int(mv.Col)
	mvRow := int(mv.Row)
	refBaseY := baseY + (mvRow >> 3)
	refBaseX := baseX + (mvCol >> 3)
	if (mvCol|mvRow)&7 == 0 &&
		uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refPtr, ok := refFullPelYPtr(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SAD16x16LimitPtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, refPtr, ref.YStride, limit)
		}
	}
	return macroblockSADLimitedSlow(src, ref, baseY, baseX, refBaseY, refBaseX, mvCol, mvRow, limit)
}

func macroblockSADLimitedSlow(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, mvCol int, mvRow int, limit int) int {
	xOffset := mvCol & 7
	yOffset := mvRow & 7
	srcInBounds := baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width
	if xOffset|yOffset != 0 {
		if srcInBounds {
			if sad, ok := macroblockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset, limit); ok {
				return sad
			}
		} else {
			var srcScratch [16 * 16]byte
			gatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if sad, ok := macroblockSubpixelSADBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16, limit); ok {
				return sad
			}
		}
	}
	if srcInBounds &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride, limit)
	}

	srcY0 := src.Y
	refY0 := ref.Y
	srcStride := src.YStride
	refStride := ref.YStride
	srcH := src.Height
	srcW := src.Width
	refH := ref.CodedHeight
	refW := ref.CodedWidth
	var srcXs [16]int
	var refXs [16]int
	for col := range 16 {
		srcXs[col] = clampEncodeCoord(baseX+col, srcW)
		refXs[col] = clampEncodeCoord(refBaseX+col, refW)
	}
	sad := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, srcH)
		refY := clampEncodeCoord(refBaseY+row, refH)
		srcRow := srcY * srcStride
		refRow := refY * refStride
		for col := range 16 {
			diff := int(srcY0[srcRow+srcXs[col]]) - int(refY0[refRow+refXs[col]])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

func splitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
			if sad, ok := splitBlockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset); ok {
				return sad
			}
		} else {
			var srcScratch [16 * 16]byte
			gatherClampedLumaBlock(src, baseY, baseX, width, height, srcScratch[:], 16)
			if sad, ok := splitBlockSubpixelSADBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, srcScratch[:], 16); ok {
				return sad
			}
		}
	}
	if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock, ok := refFullPelYSlice(ref, refBaseY, refBaseX, width, height)
		if ok {
			switch {
			case width == 16 && height == 8:
				return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 16:
				return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 8:
				return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 4 && height == 4:
				return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
			}
		}
	}

	// libvpx publishes cm->frame_to_show post-loop-filter via
	// vp8_yv12_extend_frame_borders, which extends from y_crop_width /
	// y_crop_height. That overwrites the padded-but-uncoded region between
	// visible (Width/Height) and 16-aligned coded (CodedWidth/CodedHeight)
	// with the visible-edge sample. The split-MB picker's NEW4X4 SAD walk
	// on padded-edge MBs reads from that region; clamping ref reads to the
	// visible extent here mirrors libvpx's effective reference state without
	// requiring an extra full-plane overwrite of the live reconstruction
	// (which regressed other previously-passing odd-axis fixtures via
	// downstream prediction reads).
	refClampHeight := refVisibleClampDim(ref.Height, ref.CodedHeight)
	refClampWidth := refVisibleClampDim(ref.Width, ref.CodedWidth)
	sad := 0
	for row := range height {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, refClampHeight)
		for col := range width {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, refClampWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}

// refVisibleClampDim returns the visible-extent clamp limit for ref
// coordinates used by the SPLITMV picker's padded-edge fallbacks, mirroring
// libvpx's effective post-extend reference state. The fallback to coded is
// defensive: callers tolerate visible <= 0 by going through the coded clamp
// (matching the legacy behaviour) on malformed buffers.
func refVisibleClampDim(visible int, coded int) int {
	if visible <= 0 || visible > coded {
		return coded
	}
	return visible
}

func splitBlockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, bool) {
	return splitBlockSubpixelSADBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func splitBlockSubpixelSADBlock(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+height+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+width+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+(height+4)*ref.YStride+width+5 > len(ref.YFull) {
		return 0, false
	}
	// libvpx's bordered reference reads through SixTap on a buffer where
	// vp8_yv12_extend_frame_borders has already replaced the
	// padded-but-uncoded region (between visible and 16-aligned coded) with
	// the visible-edge sample. govpx keeps the live reconstruction in that
	// region, so the SPLITMV picker's NEW4X4 sub-pel SAD diverges on the
	// padded edge. Mirror libvpx's effective state by routing the SixTap
	// input through a visible-clamped scratch when the read window spills
	// past the visible extent. Pure-visible-extent reads stay on the SIMD
	// direct-buffer path.
	visibleH := refVisibleClampDim(ref.Height, ref.CodedHeight)
	visibleW := refVisibleClampDim(ref.Width, ref.CodedWidth)
	useScratch := refBaseY-2 < 0 || refBaseX-2 < 0 ||
		refBaseY+height+3 > visibleH || refBaseX+width+3 > visibleW
	var pred [16 * 16]byte
	switch {
	case width == 16 && height == 8:
		if useScratch {
			var scratch [(8 + 5) * (16 + 5)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY-2, refBaseX-2, 16+5, 8+5, scratch[:], 16+5)
			dsp.SixTapPredict16x8(scratch[:], 16+5, xOffset, yOffset, pred[:], 16)
		} else {
			dsp.SixTapPredict16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
		}
		return dsp.SAD16x8(srcBlock, srcStride, pred[:], 16), true
	case width == 8 && height == 16:
		if useScratch {
			var scratch [(16 + 5) * (8 + 5)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY-2, refBaseX-2, 8+5, 16+5, scratch[:], 8+5)
			dsp.SixTapPredict8x16(scratch[:], 8+5, xOffset, yOffset, pred[:], 8)
		} else {
			dsp.SixTapPredict8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		}
		return dsp.SAD8x16(srcBlock, srcStride, pred[:], 8), true
	case width == 8 && height == 8:
		if useScratch {
			var scratch [(8 + 5) * (8 + 5)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY-2, refBaseX-2, 8+5, 8+5, scratch[:], 8+5)
			dsp.SixTapPredict8x8(scratch[:], 8+5, xOffset, yOffset, pred[:], 8)
		} else {
			dsp.SixTapPredict8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		}
		return dsp.SAD8x8(srcBlock, srcStride, pred[:], 8), true
	case width == 4 && height == 4:
		if useScratch {
			var scratch [(4 + 5) * (4 + 5)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY-2, refBaseX-2, 4+5, 4+5, scratch[:], 4+5)
			dsp.SixTapPredict4x4(scratch[:], 4+5, xOffset, yOffset, pred[:], 4)
		} else {
			dsp.SixTapPredict4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 4)
		}
		return dsp.SAD4x4(srcBlock, srcStride, pred[:], 4), true
	default:
		return 0, false
	}
}

func macroblockSubpixelSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, bool) {
	return macroblockSubpixelSSEBlock(ref, refBaseY, refBaseX, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func macroblockSubpixelSSEBlock(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SSE16x16(srcBlock, srcStride, pred[:], 16), true
}

func macroblockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int, limit int) (int, bool) {
	return macroblockSubpixelSADBlock(ref, refBaseY, refBaseX, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride, limit)
}

func macroblockSubpixelSADBlock(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, srcBlock []byte, srcStride int, limit int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SAD16x16Limit(srcBlock, srcStride, pred[:], 16, limit), true
}

func splitBlockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, int, bool) {
	return splitBlockSubpixelVarianceBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func splitBlockSubpixelVarianceBlock(ref *vp8common.Image, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+height+1 > ref.CodedHeight+ref.YBorder ||
		refBaseX+width+1 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+height*ref.YStride+width+1 > len(ref.YFull) {
		return 0, 0, false
	}
	// On padded-edge MBs the bilinear subpel-variance read window
	// (refBaseY..refBaseY+height, refBaseX..refBaseX+width+1) overlaps
	// the padded-but-uncoded region; route through a visible-clamped
	// scratch buffer so the bilinear input matches libvpx's effective
	// post vp8_yv12_extend_frame_borders reference state.
	visibleH := refVisibleClampDim(ref.Height, ref.CodedHeight)
	visibleW := refVisibleClampDim(ref.Width, ref.CodedWidth)
	useScratch := refBaseY < 0 || refBaseX < 0 ||
		refBaseY+height+1 > visibleH || refBaseX+width+1 > visibleW
	switch {
	case width == 16 && height == 8:
		if useScratch {
			var scratch [(8 + 1) * (16 + 1)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY, refBaseX, 16+1, 8+1, scratch[:], 16+1)
			variance, sse := dsp.SubpelVariance16x8(scratch[:], 16+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	case width == 8 && height == 16:
		if useScratch {
			var scratch [(16 + 1) * (8 + 1)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY, refBaseX, 8+1, 16+1, scratch[:], 8+1)
			variance, sse := dsp.SubpelVariance8x16(scratch[:], 8+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	case width == 8 && height == 8:
		if useScratch {
			var scratch [(8 + 1) * (8 + 1)]byte
			gatherVisibleClampedRefBlock(ref, refBaseY, refBaseX, 8+1, 8+1, scratch[:], 8+1)
			variance, sse := dsp.SubpelVariance8x8(scratch[:], 8+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	case width == 4 && height == 4:
		if useScratch {
			var scratch [(4 + 1) * (4 + 1)]byte
			// libvpx's 4x4 bilinear variance reads the coded-edge sample here;
			// using the visible edge changes the tie-breaker on odd-size SPLITMV.
			gatherCodedClampedRefBlock(ref, refBaseY, refBaseX, 4+1, 4+1, scratch[:], 4+1)
			variance, sse := dsp.SubpelVariance4x4(scratch[:], 4+1, xOffset, yOffset, srcBlock, srcStride)
			return variance, sse, true
		}
		variance, sse := dsp.SubpelVariance4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
		return variance, sse, true
	default:
		return 0, 0, false
	}
}

func macroblockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, int, bool) {
	return macroblockSubpixelVarianceBlock(ref, refBaseY, refBaseX, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
}

func macroblockSubpixelVarianceBlock(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, srcBlock []byte, srcStride int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+17 > ref.CodedHeight+ref.YBorder ||
		refBaseX+17 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+16*ref.YStride+17 > len(ref.YFull) {
		return 0, 0, false
	}
	variance, sse := dsp.SubpelVariance16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, srcStride)
	return variance, sse, true
}

func gatherClampedLumaBlock(src vp8enc.SourceImage, baseY int, baseX int, width int, height int, dst []byte, dstStride int) {
	if min(width, height) <= 0 || src.Width <= 0 || src.Height <= 0 {
		return
	}
	srcY := src.Y
	srcStride := src.YStride
	fullX := baseX >= 0 && baseX+width <= src.Width
	var srcXs [16]int
	precomputedX := !fullX && width <= len(srcXs)
	if precomputedX {
		for col := range width {
			srcXs[col] = clampEncodeCoord(baseX+col, src.Width)
		}
	}
	for row := range height {
		y := clampEncodeCoord(baseY+row, src.Height)
		dstRow := row * dstStride
		srcRow := y * srcStride
		if fullX {
			copy(dst[dstRow:dstRow+width], srcY[srcRow+baseX:srcRow+baseX+width])
			continue
		}
		if precomputedX {
			for col := range width {
				dst[dstRow+col] = srcY[srcRow+srcXs[col]]
			}
		} else {
			for col := range width {
				srcX := clampEncodeCoord(baseX+col, src.Width)
				dst[dstRow+col] = srcY[srcRow+srcX]
			}
		}
	}
}

func macroblockChromaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth, uvHeight := sourceImageUVDimensions(src)
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	// Uint-range collapse on each chroma dimension; the 4 src-dim and
	// ref-dim guards become 4 single compares (was 6 in the original
	// boolean chain).
	if uint(baseY) <= uint(uvHeight-8) && uint(baseX) <= uint(uvWidth-8) &&
		uint(baseY) <= uint(refUVHeight-8) && uint(baseX) <= uint(refUVWidth-8) {
		srcUOffset := baseY*src.UStride + baseX
		refUOffset := baseY*ref.UStride + baseX
		srcVOffset := baseY*src.VStride + baseX
		refVOffset := baseY*ref.VStride + baseX
		return dsp.SSE8x8PtrFast(&src.U[srcUOffset], src.UStride, &ref.U[refUOffset], ref.UStride) +
			dsp.SSE8x8PtrFast(&src.V[srcVOffset], src.VStride, &ref.V[refVOffset], ref.VStride)
	}

	sse := 0
	for row := range 8 {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		refY := clampEncodeCoord(baseY+row, refUVHeight)
		for col := range 8 {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			refX := clampEncodeCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

func macroblockLumaVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) &&
		uint(baseY) <= uint(ref.CodedHeight-16) && uint(baseX) <= uint(ref.CodedWidth-16) {
		// R15-C: fused (sum, sse) read collapses Variance16x16 + SSE16x16
		// into one SIMD pass (variance = sse - sum*sum/256).
		sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[baseY*ref.YStride+baseX], ref.YStride)
		return sse - ((sum * sum) >> 8), sse
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}
