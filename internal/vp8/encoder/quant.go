package encoder

import (
	"math/bits"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/vp8_quantize.c block
// quantization setup, vp8_fast_quantize_b_c, and vp8_regular_quantize_b_c.

const quantRoundFactor = 48

var quantZbinBoost = [...]int{0, 0, 8, 10, 12, 14, 16, 20, 24, 28, 32, 36, 40, 44, 44, 44}

type BlockQuant struct {
	Quant      [16]int16
	QuantFast  [16]int16
	QuantShift [16]int16
	Zbin       [16]int16
	Round      [16]int16
	ZbinBoost  [16]int16
	Dequant    [16]int16
}

type MacroblockQuant struct {
	Y1   BlockQuant
	Y1DC BlockQuant
	Y2   BlockQuant
	UV   BlockQuant
}

func InitFastBlockQuant(dequant *[16]int16, out *BlockQuant) {
	for i := 0; i < 16; i++ {
		d := int(dequant[i])
		out.QuantFast[i] = int16((1 << 16) / d)
		out.Round[i] = int16((quantRoundFactor * d) >> 7)
		out.Dequant[i] = dequant[i]
	}
}

func InitFastMacroblockQuant(dequant *common.MacroblockDequant, out *MacroblockQuant) {
	InitFastBlockQuant(&dequant.Y1, &out.Y1)
	InitFastBlockQuant(&dequant.Y1DC, &out.Y1DC)
	InitFastBlockQuant(&dequant.Y2, &out.Y2)
	InitFastBlockQuant(&dequant.UV, &out.UV)
}

func InitRegularBlockQuant(qIndex int, dequant *[16]int16, out *BlockQuant) {
	InitFastBlockQuant(dequant, out)
	q := common.ClampQIndex(qIndex)
	zbinFactor := 80
	if q < 48 {
		zbinFactor = 84
	}
	for i := 0; i < 16; i++ {
		d := int(dequant[i])
		out.Zbin[i] = int16((zbinFactor*d + 64) >> 7)
		out.ZbinBoost[i] = int16((d * quantZbinBoost[i]) >> 7)
		invertRegularQuant(d, &out.Quant[i], &out.QuantShift[i])
	}
}

func InitRegularMacroblockQuant(qIndex int, dequant *common.MacroblockDequant, out *MacroblockQuant) {
	InitRegularBlockQuant(qIndex, &dequant.Y1, &out.Y1)
	InitRegularBlockQuant(qIndex, &dequant.Y1DC, &out.Y1DC)
	InitRegularBlockQuant(qIndex, &dequant.Y2, &out.Y2)
	InitRegularBlockQuant(qIndex, &dequant.UV, &out.UV)
}

func invertRegularQuant(dequant int, quant *int16, shift *int16) {
	l := bits.Len(uint(dequant)) - 1
	m := 1 + (1<<(16+l))/dequant
	*quant = int16(m - (1 << 16))
	*shift = int16(1 << (16 - l))
}

func InitSegmentMacroblockQuants(baseQIndex int, deltas common.QuantDeltas, segmentation SegmentationConfig, out *[common.MaxMBSegments]MacroblockQuant) error {
	if baseQIndex < common.MinQ || baseQIndex > common.MaxQ || out == nil || !validSegmentationConfig(segmentation) {
		return ErrInvalidPacketConfig
	}
	var tables common.FrameDequantTables
	var dequant common.MacroblockDequant
	common.BuildFrameDequantTables(deltas, &tables)
	for segment := 0; segment < common.MaxMBSegments; segment++ {
		qIndex := baseQIndex
		if segmentation.Enabled && segmentation.FeatureEnabled[common.MBLvlAltQ][segment] {
			altQ := int(segmentation.FeatureData[common.MBLvlAltQ][segment])
			if segmentation.AbsDelta {
				qIndex = altQ
			} else {
				qIndex = baseQIndex + altQ
			}
		}
		common.InitMacroblockDequant(&tables, qIndex, &dequant)
		InitRegularMacroblockQuant(qIndex, &dequant, &out[segment])
	}
	return nil
}

func FastQuantizeBlock(coeff *[16]int16, quant *BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := -1
	for i := 0; i < 16; i++ {
		rc := int(tables.DefaultZigZag1D[i])
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			continue
		}
		x := z
		if x < 0 {
			x = -x
		}
		y := ((x + int(quant.Round[rc])) * int(quant.QuantFast[rc])) >> 16
		if z < 0 {
			y = -y
		}
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = i
		}
	}
	return eob + 1
}
