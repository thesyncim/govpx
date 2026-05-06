package encoder

import "github.com/thesyncim/libgopx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c segmentation header and
// macroblock segment-id packing.

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
		for feature := 0; feature < int(common.MBLvlMax); feature++ {
			for segment := 0; segment < common.MaxMBSegments; segment++ {
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
	for feature := 0; feature < int(common.MBLvlMax); feature++ {
		for segment := 0; segment < common.MaxMBSegments; segment++ {
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
	if magnitude < 0 {
		magnitude = -magnitude
	}
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
