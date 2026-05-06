package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// Inspired by libvpx v1.16.0 vp8/encoder/onyx_if.c cyclic background
// refresh setup. StaticThreshold itself feeds encode_breakout; segmentation
// data here mirrors libvpx's cyclic refresh Q boost shape for small CBR clips.

const staticSegmentID = 1

func (e *VP8Encoder) staticSegmentationConfig() vp8enc.SegmentationConfig {
	if e.opts.StaticThreshold <= 0 {
		return vp8enc.SegmentationConfig{}
	}
	delta := e.staticSegmentationQuantizerDelta()
	if delta == 0 {
		return vp8enc.SegmentationConfig{}
	}
	cfg := vp8enc.SegmentationConfig{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] = true
	cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID] = delta
	for i := range cfg.TreeProbs {
		cfg.TreeProbs[i] = 128
		cfg.TreeProbUpdated[i] = true
	}
	return cfg
}

func (e *VP8Encoder) staticSegmentationQuantizerDelta() int8 {
	q := e.rc.currentQuantizer
	delta := q/2 - q
	if delta == 0 {
		return 0
	}
	return int8(delta)
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
