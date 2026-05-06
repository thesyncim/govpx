package gopvx

import (
	vp8common "github.com/thesyncim/gopvx/internal/vp8/common"
	vp8enc "github.com/thesyncim/gopvx/internal/vp8/encoder"
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
	delta := -(e.rc.currentQuantizer / 2)
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

func assignInterFrameStaticSegments(rows int, cols int, modes []vp8enc.InterFrameMacroblockMode) {
	refreshCount := cyclicRefreshMaxMBsPerFrame(rows, cols)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			if index < refreshCount {
				modes[index].SegmentID = staticSegmentID
			} else {
				modes[index].SegmentID = 0
			}
		}
	}
}

func cyclicRefreshMaxMBsPerFrame(rows int, cols int) int {
	if rows <= 0 || cols <= 0 {
		return 0
	}
	return (rows * cols) / 20
}
