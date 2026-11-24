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
	return adaptInterFrameModeProbabilitiesWithBasesAndBias(rows, cols, modes, yModeBase, uvModeBase, mvBase, nil, nil, cfg)
}

// adaptInterFrameModeProbabilitiesWithBasesAndBias mirrors libvpx
// vp8/encoder/bitstream.c update_mbintra_mode_probs (called from
// pack_inter_mode_mvs). The yModeCountBias / uvModeCountBias parameters
// optionally pre-load the per-mode branch counters before this frame's
// modes contribute. libvpx VP8 MT preserves each helper thread's
// `mb->ymode_count` and `mb->uv_mode_count` across frames
// (vp8/encoder/ethreading.c vp8cx_init_mbrthread_data only zeroes main's
// ymode_count via the `vp8_zero(x->ymode_count)` line at ethreading.c:479;
// the helper-row counts are summed back into `cpi->mb.ymode_count` in
// vp8/encoder/encodeframe.c:835-843 after the helpers complete). govpx's
// inter packet writer rebuilds the count from the final modes grid, so
// the threaded driver passes the helper-history bias here to reproduce
// libvpx's MT-biased probability-update decision.
func adaptInterFrameModeProbabilitiesWithBasesAndBias(rows int, cols int, modes []InterFrameMacroblockMode, yModeBase [tables.YModeProbCount]uint8, uvModeBase [tables.UVModeProbCount]uint8, mvBase [2][tables.MVPCount]uint8, yModeCountBias *[tables.YModeProbCount][2]int, uvModeCountBias *[tables.UVModeProbCount][2]int, cfg *InterFrameStateConfig) ([tables.YModeProbCount]uint8, [tables.UVModeProbCount]uint8, [2][tables.MVPCount]uint8, error) {
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
	if yModeCountBias != nil {
		for i := range yModeCounts {
			yModeCounts[i][0] += yModeCountBias[i][0]
			yModeCounts[i][1] += yModeCountBias[i][1]
		}
	}
	if uvModeCountBias != nil {
		for i := range uvModeCounts {
			uvModeCounts[i][0] += uvModeCountBias[i][0]
			uvModeCounts[i][1] += uvModeCountBias[i][1]
		}
	}
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

// AccumulateKeyFrameHelperRowIntraBranchCounts is the keyframe analogue
// of AccumulateInterFrameHelperRowIntraBranchCounts: it walks the
// helper-thread rows of a keyframe and accumulates each MB's Y/UV mode
// into the same branch-count distribution the inter packet writer's
// update_mbintra_mode_probs decision consumes. libvpx
// vp8/encoder/encodeframe.c:1067 increments `x->ymode_count[m]` for
// every keyframe MB (sum_intra_stats fires unconditionally on KF), and
// vp8cx_init_mbrthread_data does not zero helpers' ymode_count between
// frames, so the keyframe's helper-row contribution rolls into every
// subsequent inter frame's probability-update decision.
func AccumulateKeyFrameHelperRowIntraBranchCounts(rows int, cols int, workerCount int, modes []KeyFrameMacroblockMode, yModeCounts *[tables.YModeProbCount][2]int, uvModeCounts *[tables.UVModeProbCount][2]int) {
	if rows <= 0 || cols <= 0 || workerCount <= 1 {
		return
	}
	if yModeCounts == nil && uvModeCounts == nil {
		return
	}
	required := rows * cols
	if len(modes) < required {
		return
	}
	for row := range rows {
		if row%workerCount == 0 {
			continue
		}
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if yModeCounts != nil {
				yToken := interFrameYModeTokens[int(mode.YMode)]
				_ = countTreeTokenBranches(yModeCounts[:], tables.YModeTree[:], yToken)
			}
			if uvModeCounts != nil {
				uvToken := keyFrameUVModeTokens[int(mode.UVMode)]
				_ = countTreeTokenBranches(uvModeCounts[:], tables.UVModeTree[:], uvToken)
			}
		}
	}
}

// AccumulateInterFrameHelperRowIntraBranchCounts walks the inter-frame
// modes grid and accumulates the per-mode Y / UV intra-mode branch
// counts for the rows whose `row % workerCount` falls in the
// [1, workerCount) range (i.e. helper-thread rows in libvpx VP8's MT
// row dispatch). Ported from libvpx v1.16.0
// vp8/encoder/encodeframe.c:835-843 ymode_count / uv_mode_count
// summation of `cpi->mb_row_ei[i].mb`. Callers maintain the persistent
// helper-history accumulator that pack later feeds back through
// InterFramePacket.YModeCountBias.
func AccumulateInterFrameHelperRowIntraBranchCounts(rows int, cols int, workerCount int, modes []InterFrameMacroblockMode, yModeCounts *[tables.YModeProbCount][2]int, uvModeCounts *[tables.UVModeProbCount][2]int) {
	if rows <= 0 || cols <= 0 || workerCount <= 1 {
		return
	}
	if yModeCounts == nil && uvModeCounts == nil {
		return
	}
	required := rows * cols
	if len(modes) < required {
		return
	}
	for row := range rows {
		if row%workerCount == 0 {
			continue
		}
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if interFrameReference(mode) != common.IntraFrame {
				continue
			}
			if yModeCounts != nil {
				yToken := interFrameYModeTokens[int(mode.Mode)]
				_ = countTreeTokenBranches(yModeCounts[:], tables.YModeTree[:], yToken)
			}
			if uvModeCounts != nil {
				uvToken := keyFrameUVModeTokens[int(mode.UVMode)]
				_ = countTreeTokenBranches(uvModeCounts[:], tables.UVModeTree[:], uvToken)
			}
		}
	}
}

func interFrameYModeProbs(cfg *InterFrameStateConfig) [tables.YModeProbCount]uint8 {
	probs := cfg.YModeBase
	if cfg.YModeUpdate {
		probs = cfg.YModeProbs
	}
	return probs
}

func interFrameUVModeProbs(cfg *InterFrameStateConfig) [tables.UVModeProbCount]uint8 {
	probs := cfg.UVModeBase
	if cfg.UVModeUpdate {
		probs = cfg.UVModeProbs
	}
	return probs
}

func interFrameBModeProbs(cfg *InterFrameStateConfig) [tables.BModeProbCount]uint8 {
	return cfg.BModeBase
}

func interFrameMVProbs(cfg *InterFrameStateConfig) [2][tables.MVPCount]uint8 {
	probs := cfg.MVBase
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
		if uint(probIndex) >= uint(len(counts)) || int(node)+1 >= len(tree) {
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
		if uint(probIndex) >= uint(len(counts)) || int(node)+1 >= len(tree) {
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
