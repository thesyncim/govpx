package encoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/vp8_quantize.c fast block
// quantization setup and vp8_fast_quantize_b_c.

const quantRoundFactor = 48

type BlockQuant struct {
	QuantFast [16]int16
	Round     [16]int16
	Dequant   [16]int16
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

func FastQuantizeBlock(coeff *[16]int16, quant *BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := -1
	for i := 0; i < 16; i++ {
		rc := int(tables.DefaultZigZag1D[i])
		z := int(coeff[rc])
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
