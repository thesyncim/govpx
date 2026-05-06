package libgopx

import (
	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
)

// Inspired by libvpx v1.16.0 vp8/encoder/segmentation.c cyclic background
// segmentation setup. This keeps libgopx conservative: segmentation is only
// enabled when StaticThreshold is set by the caller.

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

func assignKeyFrameStaticSegments(src vp8enc.SourceImage, rows int, cols int, threshold int, modes []vp8enc.KeyFrameMacroblockMode) {
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			modes[index].SegmentID = sourceStaticSegmentID(src, row, col, threshold)
		}
	}
}

func assignInterFrameStaticSegments(src vp8enc.SourceImage, rows int, cols int, threshold int, modes []vp8enc.InterFrameMacroblockMode) {
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			modes[index].SegmentID = sourceStaticSegmentID(src, row, col, threshold)
		}
	}
}

func sourceStaticSegmentID(src vp8enc.SourceImage, mbRow int, mbCol int, threshold int) uint8 {
	if sourceMacroblockLumaVariance(src, mbRow, mbCol) <= threshold {
		return staticSegmentID
	}
	return 0
}

func sourceMacroblockLumaVariance(src vp8enc.SourceImage, mbRow int, mbCol int) int {
	startY := mbRow * 16
	startX := mbCol * 16
	sum := 0
	sse := 0
	samples := 0
	for row := 0; row < 16; row++ {
		y := startY + row
		if y >= src.Height {
			y = src.Height - 1
		}
		for col := 0; col < 16; col++ {
			x := startX + col
			if x >= src.Width {
				x = src.Width - 1
			}
			v := int(src.Y[y*src.YStride+x])
			sum += v
			sse += v * v
			samples++
		}
	}
	if samples == 0 {
		return 0
	}
	return (sse / samples) - ((sum / samples) * (sum / samples))
}
