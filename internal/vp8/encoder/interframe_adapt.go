package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c interframe probability
// adaptation.

func adaptZeroReferenceInterFrameModeProbabilities(rows int, cols int, refFrame common.MVReferenceFrame, cfg *InterFrameStateConfig) {
	blocks := rows * cols
	if blocks <= 0 || cfg == nil {
		return
	}
	var skipCounts [2]int
	var intraCounts [2]int
	var lastCounts [2]int
	var goldenCounts [2]int
	skipCounts[1] = blocks
	intraCounts[1] = blocks
	switch refFrame {
	case common.LastFrame:
		lastCounts[0] = blocks
	case common.GoldenFrame:
		lastCounts[1] = blocks
		goldenCounts[0] = blocks
	case common.AltRefFrame:
		lastCounts[1] = blocks
		goldenCounts[1] = blocks
	default:
		return
	}
	cfg.ProbSkipFalse = interFrameSkipFalseProbability(skipCounts, cfg.ProbSkipFalse)
	cfg.ProbIntra = interFrameRefProbability(intraCounts, cfg.ProbIntra)
	cfg.ProbLast = interFrameRefProbability(lastCounts, cfg.ProbLast)
	cfg.ProbGolden = interFrameRefProbability(goldenCounts, cfg.ProbGolden)
}

func adaptInterFrameModeProbabilities(rows int, cols int, modes []InterFrameMacroblockMode, cfg *InterFrameStateConfig) error {
	_, err := adaptInterFrameModeProbabilitiesWithMVBase(rows, cols, modes, tables.DefaultMVContext, cfg)
	return err
}

func adaptInterFrameModeProbabilitiesWithMVBase(rows int, cols int, modes []InterFrameMacroblockMode, mvBase [2][tables.MVPCount]uint8, cfg *InterFrameStateConfig) ([2][tables.MVPCount]uint8, error) {
	_, _, frameMVProbs, err := adaptInterFrameModeProbabilitiesWithBases(rows, cols, modes, tables.DefaultYModeProbs, tables.DefaultUVModeProbs, mvBase, cfg)
	return frameMVProbs, err
}

func adaptInterFrameModeProbabilitiesWithBases(rows int, cols int, modes []InterFrameMacroblockMode, yModeBase [tables.YModeProbCount]uint8, uvModeBase [tables.UVModeProbCount]uint8, mvBase [2][tables.MVPCount]uint8, cfg *InterFrameStateConfig) ([tables.YModeProbCount]uint8, [tables.UVModeProbCount]uint8, [2][tables.MVPCount]uint8, error) {
	if rows < 0 || cols < 0 {
		return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrModeBufferTooSmall
	}
	required := rows * cols
	if cfg == nil || len(modes) < required {
		return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrModeBufferTooSmall
	}
	var skipCounts [2]int
	var intraCounts [2]int
	var lastCounts [2]int
	var goldenCounts [2]int
	var yModeCounts [tables.YModeProbCount][2]int
	var uvModeCounts [tables.UVModeProbCount][2]int
	var mvEvents motionVectorEventCounts
	signBias := interFrameSignBias(cfg)
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if mode.MBSkipCoeff {
				skipCounts[1]++
			} else {
				skipCounts[0]++
			}
			refFrame := interFrameReference(mode)
			if refFrame == common.IntraFrame {
				if !validInterIntraMacroblockMode(mode) {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
				}
				intraCounts[0]++
				if !countTreeTokenBranches(yModeCounts[:], tables.YModeTree[:], interFrameYModeTokens[int(mode.Mode)]) {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
				}
				if !countTreeTokenBranches(uvModeCounts[:], tables.UVModeTree[:], keyFrameUVModeTokens[int(mode.UVMode)]) {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
				}
				continue
			}
			intraCounts[1]++
			var above *InterFrameMacroblockMode
			var left *InterFrameMacroblockMode
			var aboveLeft *InterFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			if !validInterFrameMacroblockModeAt(mode, above, left, aboveLeft, row, col, rows, cols, signBias) {
				return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
			}
			switch refFrame {
			case common.LastFrame:
				lastCounts[0]++
			case common.GoldenFrame:
				lastCounts[1]++
				goldenCounts[0]++
			case common.AltRefFrame:
				lastCounts[1]++
				goldenCounts[1]++
			default:
				return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, ErrInvalidPacketConfig
			}
			switch mode.Mode {
			case common.NewMV:
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols, signBias)
				delta := MotionVector{Row: mode.MV.Row - best.Row, Col: mode.MV.Col - best.Col}
				if err := countMotionVectorEvents(&mvEvents, delta); err != nil {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, err
				}
			case common.SplitMV:
				best := interBestMotionVectorAt(above, left, aboveLeft, refFrame, row, col, rows, cols, signBias)
				if err := countSplitMotionVectorEvents(&mvEvents, mode, left, above, best); err != nil {
					return [tables.YModeProbCount]uint8{}, [tables.UVModeProbCount]uint8{}, [2][tables.MVPCount]uint8{}, err
				}
			}
		}
	}
	cfg.ProbSkipFalse = interFrameSkipFalseProbability(skipCounts, cfg.ProbSkipFalse)
	cfg.ProbIntra = interFrameRefProbability(intraCounts, cfg.ProbIntra)
	cfg.ProbLast = interFrameRefProbability(lastCounts, cfg.ProbLast)
	cfg.ProbGolden = interFrameRefProbability(goldenCounts, cfg.ProbGolden)
	frameYModeProbs := adaptInterFrameYModeProbabilitiesWithBase(&yModeCounts, yModeBase, cfg)
	frameUVModeProbs := adaptInterFrameUVModeProbabilitiesWithBase(&uvModeCounts, uvModeBase, cfg)
	mvCounts := motionVectorBranchCountsFromEvents(&mvEvents)
	frameMVProbs := adaptInterFrameMVProbabilitiesWithBase(&mvCounts, mvBase, cfg)
	return frameYModeProbs, frameUVModeProbs, frameMVProbs, nil
}

func interFrameSkipFalseProbability(counts [2]int, fallback uint8) uint8 {
	total := counts[0] + counts[1]
	if total == 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	// Saturate prob into [1, 255] without branching; the hot adaptation
	// loop calls this once per skip-context.
	return uint8(min(max(counts[0]*256/total, 1), 255))
}

func interFrameRefProbability(counts [2]int, fallback uint8) uint8 {
	total := counts[0] + counts[1]
	if total == 0 {
		if fallback == 0 {
			return 128
		}
		return fallback
	}
	return uint8(min(max(counts[0]*255/total, 1), 255))
}

func adaptInterFrameMVProbabilities(counts *[2][tables.MVPCount][2]int, cfg *InterFrameStateConfig) {
	adaptInterFrameMVProbabilitiesWithBase(counts, tables.DefaultMVContext, cfg)
}

func adaptInterFrameMVProbabilitiesWithBase(counts *[2][tables.MVPCount][2]int, base [2][tables.MVPCount]uint8, cfg *InterFrameStateConfig) [2][tables.MVPCount]uint8 {
	if counts == nil || cfg == nil {
		return base
	}
	if base == ([2][tables.MVPCount]uint8{}) {
		base = tables.DefaultMVContext
	}
	cfg.MVBase = base
	cfg.MVProbs = base
	cfg.MVUpdate = [2][tables.MVPCount]bool{}
	cfg.MVUpdateCount = 0
	frameProbs := base
	for component := range 2 {
		for i := range tables.MVPCount {
			ct := (*counts)[component][i]
			if ct[0]+ct[1] == 0 {
				continue
			}
			oldProb := base[component][i]
			newProb := motionVectorProbabilityFromBranchCount(ct)
			if newProb == oldProb {
				continue
			}
			if motionVectorProbabilityUpdateSavings(ct, oldProb, newProb, tables.MVUpdateProbs[component][i]) <= 0 {
				continue
			}
			cfg.MVProbs[component][i] = newProb
			frameProbs[component][i] = newProb
			cfg.MVUpdate[component][i] = true
			cfg.MVUpdateCount++
		}
	}
	return frameProbs
}

func motionVectorProbabilityFromBranchCount(counts [2]int) uint8 {
	total := counts[0] + counts[1]
	if total <= 0 {
		return 128
	}
	return uint8(max((counts[0]*255/total)&^1, 1))
}

func motionVectorProbabilityUpdateSavings(counts [2]int, oldProb uint8, newProb uint8, updateProb uint8) int {
	oldBits := coefficientBranchCost(counts, oldProb)
	newBits := coefficientBranchCost(counts, newProb)
	updateBits := 7 - 1 + ((coefficientBitCost(updateProb, 1) - coefficientBitCost(updateProb, 0) + 128) >> 8)
	return oldBits - newBits - updateBits
}

func adaptInterFrameYModeProbabilitiesWithBase(counts *[tables.YModeProbCount][2]int, base [tables.YModeProbCount]uint8, cfg *InterFrameStateConfig) [tables.YModeProbCount]uint8 {
	base = normalizeYModeProbabilityBase(base)
	cfg.YModeBase = base
	cfg.YModeProbs = base
	cfg.YModeUpdate = false
	var frameProbs [tables.YModeProbCount]uint8
	if !modeProbabilityUpdateFromBranchCounts(base[:], counts[:], frameProbs[:]) {
		return base
	}
	cfg.YModeProbs = frameProbs
	cfg.YModeUpdate = true
	return cfg.YModeProbs
}

func adaptInterFrameUVModeProbabilitiesWithBase(counts *[tables.UVModeProbCount][2]int, base [tables.UVModeProbCount]uint8, cfg *InterFrameStateConfig) [tables.UVModeProbCount]uint8 {
	base = normalizeUVModeProbabilityBase(base)
	cfg.UVModeBase = base
	cfg.UVModeProbs = base
	cfg.UVModeUpdate = false
	var frameProbs [tables.UVModeProbCount]uint8
	if !modeProbabilityUpdateFromBranchCounts(base[:], counts[:], frameProbs[:]) {
		return base
	}
	cfg.UVModeProbs = frameProbs
	cfg.UVModeUpdate = true
	return cfg.UVModeProbs
}

func modeProbabilityUpdateFromBranchCounts(base []uint8, counts []([2]int), frameProbs []uint8) bool {
	copy(frameProbs, base)
	oldBits := 0
	newBits := 0
	for i := range counts {
		newProb := coefficientProbabilityFromBranchCount(counts[i])
		oldBits += coefficientBranchCost(counts[i], base[i])
		newBits += coefficientBranchCost(counts[i], newProb)
		if newProb == 0 {
			newProb = 1
		}
		frameProbs[i] = newProb
	}
	return newBits+(len(counts)<<8) < oldBits
}

func normalizeYModeProbabilityBase(base [tables.YModeProbCount]uint8) [tables.YModeProbCount]uint8 {
	if base == ([tables.YModeProbCount]uint8{}) {
		return tables.DefaultYModeProbs
	}
	return base
}

func normalizeUVModeProbabilityBase(base [tables.UVModeProbCount]uint8) [tables.UVModeProbCount]uint8 {
	if base == ([tables.UVModeProbCount]uint8{}) {
		return tables.DefaultUVModeProbs
	}
	return base
}

func interFrameYModeProbs(cfg *InterFrameStateConfig) [tables.YModeProbCount]uint8 {
	probs := normalizeYModeProbabilityBase(cfg.YModeBase)
	if cfg.YModeUpdate {
		probs = normalizeYModeProbabilityBase(cfg.YModeProbs)
	}
	return probs
}

func interFrameUVModeProbs(cfg *InterFrameStateConfig) [tables.UVModeProbCount]uint8 {
	probs := normalizeUVModeProbabilityBase(cfg.UVModeBase)
	if cfg.UVModeUpdate {
		probs = normalizeUVModeProbabilityBase(cfg.UVModeProbs)
	}
	return probs
}

func interFrameMVProbs(cfg *InterFrameStateConfig) [2][tables.MVPCount]uint8 {
	probs := cfg.MVBase
	if probs == ([2][tables.MVPCount]uint8{}) {
		probs = tables.DefaultMVContext
	}
	for component := range 2 {
		for i := range tables.MVPCount {
			if cfg.MVUpdate[component][i] {
				probs[component][i] = cfg.MVProbs[component][i]
			}
		}
	}
	return probs
}

func countMotionVectorBranches(counts *[2][tables.MVPCount][2]int, mv MotionVector) error {
	if counts == nil || mv.Row&1 != 0 || mv.Col&1 != 0 {
		return ErrInvalidPacketConfig
	}
	if !countMVComponentBranches(&(*counts)[0], int(mv.Row/2)) {
		return ErrInvalidPacketConfig
	}
	if !countMVComponentBranches(&(*counts)[1], int(mv.Col/2)) {
		return ErrInvalidPacketConfig
	}
	return nil
}

func countMotionVectorEvents(events *motionVectorEventCounts, mv MotionVector) error {
	if events == nil || mv.Row&1 != 0 || mv.Col&1 != 0 {
		return ErrInvalidPacketConfig
	}
	row := int(mv.Row / 2)
	col := int(mv.Col / 2)
	if !validMotionVectorEventComponent(row) || !validMotionVectorEventComponent(col) {
		return nil
	}
	(*events)[0][mvComponentMax+row]++
	(*events)[1][mvComponentMax+col]++
	return nil
}

func validMotionVectorEventComponent(component int) bool {
	return component >= -mvComponentMax && component <= mvComponentMax
}

func motionVectorBranchCountsFromEvents(events *motionVectorEventCounts) [2][tables.MVPCount][2]int {
	var counts [2][tables.MVPCount][2]int
	if events == nil {
		return counts
	}
	for component := range counts {
		counts[component] = motionVectorComponentBranchCountsFromEvents(&(*events)[component])
	}
	return counts
}

func motionVectorComponentBranchCountsFromEvents(events *motionVectorComponentEvents) [tables.MVPCount][2]int {
	var counts [tables.MVPCount][2]int
	if events == nil {
		return counts
	}
	var shortDistribution [mvNumShort]int
	for magnitude := 0; magnitude <= mvComponentMax; magnitude++ {
		positive := (*events)[mvComponentMax+magnitude]
		negative := 0
		if magnitude != 0 {
			negative = (*events)[mvComponentMax-magnitude]
		}
		total := positive + negative
		if total == 0 {
			continue
		}
		if magnitude == 0 {
			counts[mvProbIsShort][0] += total
			shortDistribution[0] += total
			continue
		}
		counts[mvProbSign][0] += positive
		counts[mvProbSign][1] += negative
		if magnitude < mvNumShort {
			counts[mvProbIsShort][0] += total
			shortDistribution[magnitude] += total
			continue
		}
		counts[mvProbIsShort][1] += total
		for bit := mvLongWidth - 1; bit >= 0; bit-- {
			counts[mvProbBits+bit][(magnitude>>bit)&1] += total
		}
	}
	for token, total := range shortDistribution {
		if total == 0 {
			continue
		}
		if !countTreeTokenBranchesWeighted(counts[mvProbShort:], tables.SmallMVTree[:], smallMVTokens[token], total) {
			return [tables.MVPCount][2]int{}
		}
	}
	return counts
}

func countMVComponentBranches(counts *[tables.MVPCount][2]int, component int) bool {
	negative := component < 0
	if negative {
		component = -component
	}
	if component >= 8 {
		return countLargeMVComponentBranches(counts, component, negative)
	}
	counts[mvProbIsShort][0]++
	if !countTreeTokenBranches(counts[mvProbShort:], tables.SmallMVTree[:], smallMVTokens[component]) {
		return false
	}
	if component != 0 {
		countBoolBranch(&counts[mvProbSign], negative)
	}
	return true
}

func countLargeMVComponentBranches(counts *[tables.MVPCount][2]int, component int, negative bool) bool {
	if component < 8 || component > 0x7ff {
		return false
	}
	counts[mvProbIsShort][1]++
	coded := component
	if component < 16 {
		coded = component - 8
	}
	for i := range 3 {
		counts[mvProbBits+i][(coded>>i)&1]++
	}
	for i := mvLongWidth - 1; i > 3; i-- {
		counts[mvProbBits+i][(coded>>i)&1]++
	}
	if coded&0xfff0 != 0 {
		counts[mvProbBits+3][(component>>3)&1]++
	}
	if component != 0 {
		countBoolBranch(&counts[mvProbSign], negative)
	}
	return true
}

func countBoolBranch(counts *[2]int, value bool) {
	if value {
		counts[1]++
		return
	}
	counts[0]++
}

func countTreeTokenBranches(counts []([2]int), tree []int16, token TreeToken) bool {
	node := int16(0)
	for bitIndex := int(token.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(counts) || int(node)+1 >= len(tree) {
			return false
		}
		bit := int((token.Value >> uint(bitIndex)) & 1)
		counts[probIndex][bit]++
		next := tree[int(node)+bit]
		if next <= 0 {
			return bitIndex == 0
		}
		node = next
	}
	return false
}

func countTreeTokenBranchesWeighted(counts []([2]int), tree []int16, token TreeToken, weight int) bool {
	if weight < 0 {
		return false
	}
	if weight == 0 {
		return true
	}
	node := int16(0)
	for bitIndex := int(token.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(counts) || int(node)+1 >= len(tree) {
			return false
		}
		bit := int((token.Value >> uint(bitIndex)) & 1)
		counts[probIndex][bit] += weight
		next := tree[int(node)+bit]
		if next <= 0 {
			return bitIndex == 0
		}
		node = next
	}
	return false
}
