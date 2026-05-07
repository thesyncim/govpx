package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// Ported from libvpx v1.16.0 vp8/encoder/onyx_if.c cyclic background
// refresh setup. StaticThreshold itself feeds encode_breakout; cyclic refresh
// segmentation is enabled independently for CBR and error-resilient encodes.

const staticSegmentID = 1

func (e *VP8Encoder) cyclicRefreshSegmentationConfig() vp8enc.SegmentationConfig {
	if !e.cyclicRefreshModeEnabled() {
		return vp8enc.SegmentationConfig{}
	}
	cfg := vp8enc.SegmentationConfig{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	if delta := e.cyclicRefreshQuantizerDelta(); delta != 0 {
		cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] = true
		cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID] = delta
	}
	return cfg
}

func (e *VP8Encoder) cyclicRefreshModeEnabled() bool {
	if e == nil {
		return false
	}
	return e.opts.ErrorResilient || e.rc.mode == RateControlCBR
}

func (e *VP8Encoder) cyclicRefreshQuantizerDelta() int8 {
	q := e.rc.currentQuantizer
	return int8(q/2 - q)
}

func updateKeyFrameSegmentationTreeProbs(cfg *vp8enc.SegmentationConfig, modes []vp8enc.KeyFrameMacroblockMode) {
	var counts [vp8common.MaxMBSegments]int
	for _, mode := range modes {
		if mode.SegmentID < vp8common.MaxMBSegments {
			counts[mode.SegmentID]++
		}
	}
	updateSegmentationTreeProbs(cfg, counts)
}

func updateInterFrameSegmentationTreeProbs(cfg *vp8enc.SegmentationConfig, modes []vp8enc.InterFrameMacroblockMode) {
	var counts [vp8common.MaxMBSegments]int
	for _, mode := range modes {
		if mode.SegmentID < vp8common.MaxMBSegments {
			counts[mode.SegmentID]++
		}
	}
	updateSegmentationTreeProbs(cfg, counts)
}

func updateSegmentationTreeProbs(cfg *vp8enc.SegmentationConfig, counts [vp8common.MaxMBSegments]int) {
	if cfg == nil || !cfg.Enabled || !cfg.UpdateMap {
		return
	}
	for i := range cfg.TreeProbUpdated {
		cfg.TreeProbUpdated[i] = false
		cfg.TreeProbs[i] = 0
	}
	probs := [vp8common.MBFeatureTreeProbs]uint8{255, 255, 255}
	total := counts[0] + counts[1] + counts[2] + counts[3]
	if total > 0 {
		probs[0] = nonZeroSegmentTreeProb(((counts[0] + counts[1]) * 255) / total)
		leftTotal := counts[0] + counts[1]
		if leftTotal > 0 {
			probs[1] = nonZeroSegmentTreeProb((counts[0] * 255) / leftTotal)
		}
		rightTotal := counts[2] + counts[3]
		if rightTotal > 0 {
			probs[2] = nonZeroSegmentTreeProb((counts[2] * 255) / rightTotal)
		}
	}
	for i, prob := range probs {
		if prob == 255 {
			continue
		}
		cfg.TreeProbs[i] = prob
		cfg.TreeProbUpdated[i] = true
	}
}

func nonZeroSegmentTreeProb(prob int) uint8 {
	if prob <= 0 {
		return 1
	}
	if prob >= 255 {
		return 255
	}
	return uint8(prob)
}

func assignKeyFrameStaticSegments(rows int, cols int, modes []vp8enc.KeyFrameMacroblockMode) {
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			modes[index].SegmentID = 0
		}
	}
}

func assignInterFrameStaticSegments(rows int, cols int, start int, refreshCount int, modes []vp8enc.InterFrameMacroblockMode) {
	assignInterFrameStaticSegmentsWithMap(rows, cols, start, refreshCount, nil, modes)
}

func assignInterFrameStaticSegmentsWithMap(rows int, cols int, start int, refreshCount int, refreshMap []int8, modes []vp8enc.InterFrameMacroblockMode) int {
	count := rows * cols
	if count <= 0 {
		return 0
	}
	start %= count
	if start < 0 {
		start += count
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			modes[index].SegmentID = 0
		}
	}
	if refreshCount <= 0 {
		return start
	}
	if len(refreshMap) < count {
		for refreshed := 0; refreshed < refreshCount && refreshed < count; refreshed++ {
			modes[(start+refreshed)%count].SegmentID = staticSegmentID
		}
		return (start + min(refreshCount, count)) % count
	}
	i := start
	blockCount := refreshCount
	for {
		if refreshMap[i] == 0 {
			modes[i].SegmentID = staticSegmentID
			blockCount--
		} else if refreshMap[i] < 0 {
			refreshMap[i]++
		}
		i++
		if i == count {
			i = 0
		}
		if blockCount == 0 || i == start {
			break
		}
	}
	return i
}

func (e *VP8Encoder) assignInterFrameStaticSegments(rows int, cols int, modes []vp8enc.InterFrameMacroblockMode) int {
	count := rows * cols
	if count <= 0 {
		return 0
	}
	if len(e.cyclicRefreshMap) < count || len(e.cyclicRefreshAttemptMap) < count {
		return assignInterFrameStaticSegmentsWithMap(rows, cols, e.cyclicRefreshIndex, e.cyclicRefreshMaxMBsPerFrame(rows, cols), nil, modes)
	}
	copy(e.cyclicRefreshAttemptMap[:count], e.cyclicRefreshMap[:count])
	return assignInterFrameStaticSegmentsWithMap(rows, cols, e.cyclicRefreshIndex, e.cyclicRefreshMaxMBsPerFrame(rows, cols), e.cyclicRefreshAttemptMap[:count], modes)
}

func (e *VP8Encoder) commitCyclicRefresh(rows int, cols int, nextIndex int, modes []vp8enc.InterFrameMacroblockMode) {
	count := rows * cols
	if count <= 0 {
		e.cyclicRefreshIndex = 0
		return
	}
	if len(e.cyclicRefreshMap) >= count && len(e.cyclicRefreshAttemptMap) >= count && len(modes) >= count {
		copy(e.cyclicRefreshMap[:count], e.cyclicRefreshAttemptMap[:count])
		updateCyclicRefreshMapFromInterFrame(modes[:count], e.cyclicRefreshMap[:count])
	}
	nextIndex %= count
	if nextIndex < 0 {
		nextIndex += count
	}
	e.cyclicRefreshIndex = nextIndex
}

func updateCyclicRefreshMapFromInterFrame(modes []vp8enc.InterFrameMacroblockMode, refreshMap []int8) {
	count := min(len(modes), len(refreshMap))
	for index := 0; index < count; index++ {
		mode := modes[index]
		if mode.SegmentID != 0 {
			refreshMap[index] = -1
		} else if mode.Mode == vp8common.ZeroMV && mode.RefFrame == vp8common.LastFrame {
			if refreshMap[index] == 1 {
				refreshMap[index] = 0
			}
		} else {
			refreshMap[index] = 1
		}
	}
}

func clearCyclicRefreshMap(refreshMap []int8) {
	for i := range refreshMap {
		refreshMap[i] = 0
	}
}

func (e *VP8Encoder) advanceCyclicRefresh(rows int, cols int) {
	count := rows * cols
	if count <= 0 {
		e.cyclicRefreshIndex = 0
		return
	}
	e.cyclicRefreshIndex = (e.cyclicRefreshIndex + e.cyclicRefreshMaxMBsPerFrame(rows, cols)) % count
}

func (e *VP8Encoder) cyclicRefreshMaxMBsPerFrame(rows int, cols int) int {
	layers := 1
	if e != nil && e.temporal.enabled {
		layers = e.temporal.pattern.Layers
	}
	return cyclicRefreshMaxMBsPerFrameForLayers(rows, cols, layers)
}

func cyclicRefreshMaxMBsPerFrame(rows int, cols int) int {
	return cyclicRefreshMaxMBsPerFrameForLayers(rows, cols, 1)
}

func cyclicRefreshMaxMBsPerFrameForLayers(rows int, cols int, layers int) int {
	if rows <= 0 || cols <= 0 {
		return 0
	}
	count := rows * cols
	switch layers {
	case 1:
		return count / 20
	case 2:
		return count / 10
	default:
		return count / 7
	}
}
