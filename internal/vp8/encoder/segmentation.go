package encoder

import "github.com/thesyncim/govpx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c segmentation header and
// macroblock segment-id packing.

const StaticSegmentID = 1

type SegmentationConfig struct {
	Enabled    bool
	UpdateMap  bool
	UpdateData bool
	AbsDelta   bool

	FeatureData    [common.MBLvlMax][common.MaxMBSegments]int8
	FeatureEnabled [common.MBLvlMax][common.MaxMBSegments]bool

	TreeProbs       [common.MBFeatureTreeProbs]uint8
	TreeProbUpdated [common.MBFeatureTreeProbs]bool
}

var segmentationFeatureDataBits = [common.MBLvlMax]uint8{7, 6}

func writeSegmentationHeader(w *BoolWriter, cfg SegmentationConfig) error {
	if !validSegmentationConfig(cfg) {
		return ErrInvalidPacketConfig
	}
	writeBoolBit(w, cfg.Enabled)
	if !cfg.Enabled {
		return nil
	}

	writeBoolBit(w, cfg.UpdateMap)
	writeBoolBit(w, cfg.UpdateData)

	if cfg.UpdateData {
		writeBoolBit(w, cfg.AbsDelta)
		for feature := range int(common.MBLvlMax) {
			for segment := range common.MaxMBSegments {
				if !cfg.FeatureEnabled[feature][segment] {
					w.WriteBit(0)
					continue
				}
				value := cfg.FeatureData[feature][segment]
				magnitude, _ := segmentationFeatureMagnitude(value, segmentationFeatureDataBits[feature])
				w.WriteBit(1)
				w.WriteLiteral(magnitude, int(segmentationFeatureDataBits[feature]))
				if value < 0 {
					w.WriteBit(1)
				} else {
					w.WriteBit(0)
				}
			}
		}
	}

	if cfg.UpdateMap {
		for i := range cfg.TreeProbs {
			if cfg.TreeProbUpdated[i] {
				w.WriteBit(1)
				w.WriteLiteral(uint32(cfg.TreeProbs[i]), 8)
			} else {
				w.WriteBit(0)
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func validSegmentationConfig(cfg SegmentationConfig) bool {
	if !cfg.Enabled || !cfg.UpdateData {
		return true
	}
	for feature := range int(common.MBLvlMax) {
		for segment := range common.MaxMBSegments {
			if !cfg.FeatureEnabled[feature][segment] {
				continue
			}
			if _, ok := segmentationFeatureMagnitude(cfg.FeatureData[feature][segment], segmentationFeatureDataBits[feature]); !ok {
				return false
			}
		}
	}
	return true
}

func segmentationFeatureMagnitude(value int8, bits uint8) (uint32, bool) {
	magnitude := int(value)
	mask := magnitude >> intSignShift
	magnitude = (magnitude ^ mask) - mask
	return uint32(magnitude), magnitude < 1<<bits
}

func segmentationTreeProbs(cfg SegmentationConfig) [common.MBFeatureTreeProbs]uint8 {
	probs := [common.MBFeatureTreeProbs]uint8{255, 255, 255}
	if !cfg.Enabled || !cfg.UpdateMap {
		return probs
	}
	for i := range probs {
		if cfg.TreeProbUpdated[i] {
			probs[i] = cfg.TreeProbs[i]
		}
	}
	return probs
}

func writeMacroblockSegmentID(w *BoolWriter, probs *[common.MBFeatureTreeProbs]uint8, segmentID uint8) bool {
	if segmentID >= common.MaxMBSegments {
		return false
	}
	switch segmentID {
	case 0:
		w.WriteBool(0, probs[0])
		w.WriteBool(0, probs[1])
	case 1:
		w.WriteBool(0, probs[0])
		w.WriteBool(1, probs[1])
	case 2:
		w.WriteBool(1, probs[0])
		w.WriteBool(0, probs[2])
	case 3:
		w.WriteBool(1, probs[0])
		w.WriteBool(1, probs[2])
	}
	return w.Err() == nil
}

func UpdateKeyFrameSegmentationTreeProbs(cfg *SegmentationConfig, modes []KeyFrameMacroblockMode) {
	var counts [common.MaxMBSegments]int
	for _, mode := range modes {
		if mode.SegmentID < common.MaxMBSegments {
			counts[mode.SegmentID]++
		}
	}
	UpdateSegmentationTreeProbs(cfg, counts)
}

func UpdateInterFrameSegmentationTreeProbs(cfg *SegmentationConfig, modes []InterFrameMacroblockMode) {
	var counts [common.MaxMBSegments]int
	for _, mode := range modes {
		if mode.SegmentID < common.MaxMBSegments {
			counts[mode.SegmentID]++
		}
	}
	UpdateSegmentationTreeProbs(cfg, counts)
}

func UpdateSegmentationTreeProbs(cfg *SegmentationConfig, counts [common.MaxMBSegments]int) {
	if cfg == nil || !cfg.Enabled || !cfg.UpdateMap {
		return
	}
	for i := range cfg.TreeProbUpdated {
		cfg.TreeProbUpdated[i] = false
		cfg.TreeProbs[i] = 0
	}
	probs := [common.MBFeatureTreeProbs]uint8{255, 255, 255}
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
	return uint8(min(max(prob, 1), 255))
}

func AssignKeyFrameStaticSegments(rows int, cols int, modes []KeyFrameMacroblockMode) {
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			modes[index].SegmentID = 0
		}
	}
}

func AssignInterFrameStaticSegments(rows int, cols int, start int, refreshCount int, modes []InterFrameMacroblockMode) {
	AssignInterFrameStaticSegmentsWithMap(rows, cols, start, refreshCount, nil, modes)
}

func AssignInterFrameStaticSegmentsWithMap(rows int, cols int, start int, refreshCount int, refreshMap []int8, modes []InterFrameMacroblockMode) int {
	count := rows * cols
	if count <= 0 {
		return 0
	}
	start %= count
	if start < 0 {
		start += count
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			modes[index].SegmentID = 0
		}
	}
	if refreshCount <= 0 {
		return start
	}
	if len(refreshMap) < count {
		for refreshed := 0; refreshed < refreshCount && refreshed < count; refreshed++ {
			modes[(start+refreshed)%count].SegmentID = StaticSegmentID
		}
		return (start + min(refreshCount, count)) % count
	}
	i := start
	blockCount := refreshCount
	for {
		if refreshMap[i] == 0 {
			modes[i].SegmentID = StaticSegmentID
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
