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

func AddMacroblockResidual(tokens *MacroblockTokens, residual *MacroblockResidual, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int) {
	for i := 0; i < 16; i++ {
		if tokens.EOB[i] == 0 {
			continue
		}
		addTransformBlock(tokens.EOB[i], residual.Block(i), y[yBlockOffset(i, yStride):], yStride)
	}
	for i := 0; i < 4; i++ {
		if tokens.EOB[16+i] != 0 {
			addTransformBlock(tokens.EOB[16+i], residual.Block(16+i), u[uvBlockOffset(i, uStride):], uStride)
		}
		if tokens.EOB[20+i] != 0 {
			addTransformBlock(tokens.EOB[20+i], residual.Block(20+i), v[uvBlockOffset(i, vStride):], vStride)
		}
	}
}

func PredictIntraY16x16(mode common.MBPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, upAvailable bool, leftAvailable bool) bool {
	switch mode {
	case common.DCPred:
		dsp.IntraDCPredict16x16(dst, stride, above, left, upAvailable, leftAvailable)
	case common.VPred:
		dsp.IntraVerticalPredict16x16(dst, stride, above)
	case common.HPred:
		dsp.IntraHorizontalPredict16x16(dst, stride, left)
	case common.TMPred:
		dsp.IntraTMPredict16x16(dst, stride, above, left, topLeft)
	default:
		return false
	}
	return true
}

func PredictIntraUV8x8(mode common.MBPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, upAvailable bool, leftAvailable bool) bool {
	switch mode {
	case common.DCPred:
		dsp.IntraDCPredict8x8(dst, stride, above, left, upAvailable, leftAvailable)
	case common.VPred:
		dsp.IntraVerticalPredict8x8(dst, stride, above)
	case common.HPred:
		dsp.IntraHorizontalPredict8x8(dst, stride, left)
	case common.TMPred:
		dsp.IntraTMPredict8x8(dst, stride, above, left, topLeft)
	default:
		return false
	}
	return true
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

func addTransformBlock(eob uint8, coeff *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(coeff[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(coeff, dst, stride, dst, stride)
}

func yBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func uvBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}
