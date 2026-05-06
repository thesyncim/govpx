package decoder

import "github.com/thesyncim/govpx/internal/vp8/common"

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c vp8cx_init_de_quantizer
// - vp8/decoder/decodeframe.c vp8_mb_init_dequantizer

func InitSegmentDequants(quant QuantHeader, segmentation *SegmentationHeader, tables *common.FrameDequantTables, out *[common.MaxMBSegments]common.MacroblockDequant) {
	common.BuildFrameDequantTables(common.QuantDeltas{
		Y1DC: int(quant.Y1DCDelta),
		Y2DC: int(quant.Y2DCDelta),
		Y2AC: int(quant.Y2ACDelta),
		UVDC: int(quant.UVDCDelta),
		UVAC: int(quant.UVACDelta),
	}, tables)

	baseQ := int(quant.BaseQIndex)
	for segment := 0; segment < common.MaxMBSegments; segment++ {
		qIndex := baseQ
		if segmentation != nil && segmentation.Enabled {
			altQ := int(segmentation.FeatureData[common.MBLvlAltQ][segment])
			if segmentation.AbsDelta {
				qIndex = altQ
			} else {
				qIndex = baseQ + altQ
			}
		}
		common.InitMacroblockDequant(tables, qIndex, &out[segment])
	}
}
