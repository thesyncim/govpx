package encoder

import "github.com/thesyncim/libgopx/internal/vp8/common"

// Ported from libvpx v1.16.0 vp8/encoder/encodeframe.c and
// vp8/encoder/encodemb.c intra residual transform/quantization flow, limited
// to DC-predicted keyframe macroblocks against a neutral predictor.

type SourceImage struct {
	Width  int
	Height int

	Y []byte
	U []byte
	V []byte

	YStride int
	UStride int
	VStride int
}

func BuildNeutralPredictorKeyFrameCoefficients(src SourceImage, qIndex int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients) error {
	if !validSourceImage(src) || qIndex < common.MinQ || qIndex > common.MaxQ {
		return ErrInvalidPacketConfig
	}
	rows := (src.Height + 15) >> 4
	cols := (src.Width + 15) >> 4
	required := rows * cols
	if len(modes) < required || len(coeffs) < required {
		return ErrModeBufferTooSmall
	}

	var dequantTables common.FrameDequantTables
	var dequant common.MacroblockDequant
	var quant MacroblockQuant
	common.BuildFrameDequantTables(common.QuantDeltas{}, &dequantTables)
	common.InitMacroblockDequant(&dequantTables, qIndex, &dequant)
	InitFastMacroblockQuant(&dequant, &quant)

	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			modes[index] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
			buildNeutralPredictorMacroblockCoefficients(src, row, col, &quant, &coeffs[index])
		}
	}
	return nil
}

func buildNeutralPredictorMacroblockCoefficients(src SourceImage, mbRow int, mbCol int, quant *MacroblockQuant, coeffs *MacroblockCoefficients) {
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16

	for block := 0; block < 16; block++ {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		var input [16]int16
		var dct [16]int16
		fillResidual4x4(src.Y, src.YStride, src.Width, src.Height, x, y, &input)
		ForwardDCT4x4(input[:], 4, &dct)
		y2Input[block] = dct[0]
		dct[0] = 0
		coeffs.SetBlockEOB(block, FastQuantizeBlock(&dct, &quant.Y1DC, &coeffs.QCoeff[block], &dq))
	}
	ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	coeffs.SetBlockEOB(24, FastQuantizeBlock(&y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq))

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		y := mbRow*8 + (block>>1)*4
		var input [16]int16
		var dct [16]int16
		fillResidual4x4(src.U, src.UStride, uvWidth, uvHeight, x, y, &input)
		ForwardDCT4x4(input[:], 4, &dct)
		coeffs.SetBlockEOB(16+block, FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[16+block], &dq))

		fillResidual4x4(src.V, src.VStride, uvWidth, uvHeight, x, y, &input)
		ForwardDCT4x4(input[:], 4, &dct)
		coeffs.SetBlockEOB(20+block, FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[20+block], &dq))
	}
}

func fillResidual4x4(plane []byte, stride int, width int, height int, x int, y int, out *[16]int16) {
	for row := 0; row < 4; row++ {
		sampleY := clampCoord(y+row, height)
		for col := 0; col < 4; col++ {
			sampleX := clampCoord(x+col, width)
			out[row*4+col] = int16(int(plane[sampleY*stride+sampleX]) - 128)
		}
	}
}

func validSourceImage(src SourceImage) bool {
	if src.Width <= 0 || src.Height <= 0 {
		return false
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if src.YStride < src.Width || src.UStride < uvWidth || src.VStride < uvWidth {
		return false
	}
	if len(src.Y) < sourcePlaneLen(src.YStride, src.Height, src.Width) {
		return false
	}
	if len(src.U) < sourcePlaneLen(src.UStride, uvHeight, uvWidth) {
		return false
	}
	if len(src.V) < sourcePlaneLen(src.VStride, uvHeight, uvWidth) {
		return false
	}
	return true
}

func sourcePlaneLen(stride int, rows int, visibleWidth int) int {
	if rows <= 0 {
		return 0
	}
	return stride*(rows-1) + visibleWidth
}

func clampCoord(v int, limit int) int {
	if v >= limit {
		return limit - 1
	}
	return v
}
