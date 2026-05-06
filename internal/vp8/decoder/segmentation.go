package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
)

// Ported from libvpx v1.16.0 vp8/decoder/decodeframe.c segmentation header
// parsing.

type SegmentationHeader struct {
	Enabled    bool
	UpdateMap  bool
	UpdateData bool
	AbsDelta   bool

	FeatureData [common.MBLvlMax][common.MaxMBSegments]int8
	TreeProbs   [common.MBFeatureTreeProbs]uint8
}

func parseSegmentationHeader(br *boolcoder.Decoder) SegmentationHeader {
	var h SegmentationHeader
	h.Enabled = br.ReadBit() != 0
	if !h.Enabled {
		return h
	}

	h.UpdateMap = br.ReadBit() != 0
	h.UpdateData = br.ReadBit() != 0

	if h.UpdateData {
		h.AbsDelta = br.ReadBit() != 0
		for feature := 0; feature < int(common.MBLvlMax); feature++ {
			for segment := 0; segment < common.MaxMBSegments; segment++ {
				if br.ReadBit() != 0 {
					value := int8(br.ReadLiteral(int(mbFeatureDataBits[feature])))
					if br.ReadBit() != 0 {
						value = -value
					}
					h.FeatureData[feature][segment] = value
				}
			}
		}
	}

	if h.UpdateMap {
		for i := range h.TreeProbs {
			h.TreeProbs[i] = 255
		}
		for i := range h.TreeProbs {
			if br.ReadBit() != 0 {
				h.TreeProbs[i] = uint8(br.ReadLiteral(8))
			}
		}
	}
	return h
}

var mbFeatureDataBits = [common.MBLvlMax]uint8{7, 6}
