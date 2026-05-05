package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c macroblock inverse transform setup
// - vp8/common/invtrans.h inverse-transform dispatch

type MacroblockResidual struct {
	DQCoeff [25 * 16]int16
}

func (r *MacroblockResidual) Block(index int) *[16]int16 {
	return (*[16]int16)(r.DQCoeff[index*16 : index*16+16])
}

func TransformMacroblockTokens(tokens *MacroblockTokens, dequant *common.MacroblockDequant, is4x4 bool, out *MacroblockResidual) {
	clearMacroblockResidual(out)

	if !is4x4 && tokens.EOB[24] > 0 {
		var y2 [16]int16
		if tokens.EOB[24] > 1 {
			dsp.DequantizeBlock(&tokens.QCoeff[24], &dequant.Y2, &y2)
			dsp.InverseWalsh4x4(&y2, out.DQCoeff[:])
		} else {
			y2[0] = tokens.QCoeff[24][0] * dequant.Y2[0]
			dsp.DCOnlyInverseWalsh4x4(y2[0], out.DQCoeff[:])
		}
	}

	yDequant := &dequant.Y1
	if !is4x4 {
		yDequant = &dequant.Y1DC
	}
	for i := 0; i < 16; i++ {
		if tokens.EOB[i] == 0 {
			continue
		}
		dequantizeInto(&tokens.QCoeff[i], yDequant, out.Block(i))
	}
	for i := 16; i < 24; i++ {
		if tokens.EOB[i] == 0 {
			continue
		}
		dequantizeInto(&tokens.QCoeff[i], &dequant.UV, out.Block(i))
	}
}

func clearMacroblockResidual(out *MacroblockResidual) {
	for i := range out.DQCoeff {
		out.DQCoeff[i] = 0
	}
}

func dequantizeInto(qcoeff *[16]int16, dequant *[16]int16, out *[16]int16) {
	for i := 0; i < 16; i++ {
		out[i] += qcoeff[i] * dequant[i]
	}
}
