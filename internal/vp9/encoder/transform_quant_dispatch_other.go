package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// Shared Go transform and quantizer dispatch, with independent scalar
// references retained for unsupported inputs and fallback builds.

func forwardDCT4x4Dispatch(input []int16, stride int, output []int16) {
	if forwardTransformGo(input, stride, output, 4, common.DctDct) {
		return
	}
	forwardDCT4x4Scalar(input, stride, output)
}

func forwardDCT8x8Dispatch(input []int16, stride int, output []int16) {
	if forwardTransformGo(input, stride, output, 8, common.DctDct) {
		return
	}
	forwardDCT8x8Scalar(input, stride, output)
}

func forwardHT8x8Dispatch(input []int16, stride int, txType common.TxType, output []int16) bool {
	return forwardTransformGo(input, stride, output, 8, txType)
}

func forwardDCT16x16Dispatch(input []int16, stride int, output []int16) {
	if forwardTransformGo(input, stride, output, 16, common.DctDct) {
		return
	}
	forwardDCT16x16Scalar(input, stride, output)
}

func forwardHT16x16Dispatch(input []int16, stride int, txType common.TxType, output []int16) bool {
	return forwardTransformGo(input, stride, output, 16, txType)
}

func forwardDCT32x32Dispatch(input []int16, stride int, output []int16) {
	if forwardDCT32Go(input, stride, output, false) {
		return
	}
	forwardDCT32x32Scalar(input, stride, output)
}

func forwardDCT32x32RDDispatch(input []int16, stride int, output []int16) {
	if forwardDCT32Go(input, stride, output, true) {
		return
	}
	forwardDCT32x32RDScalar(input, stride, output)
}

func forwardWHT4x4Dispatch(input []int16, stride int, output []int16) {
	if !forwardWHTGo(input, stride, output) {
		forwardWHT4x4Scalar(input, stride, output)
	}
}

func quantizeFPDispatch(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return quantizeFPScalar(coeff, dequant, scan, dqcoeff)
}

func quantizeFPLibvpxDispatch(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	if pixel.Enabled && nCoeffs > 0 && len(iscan) >= nCoeffs && roundFP[0] >= 0 && roundFP[1] >= 0 && quantFP[0] >= 0 && quantFP[1] >= 0 && dequant[0] > 0 && dequant[1] > 0 {
		return quantizeFPGo(coeff, iscan, qcoeff, dqcoeff, nCoeffs, roundFP, quantFP, dequant)
	}

	return quantizeFPLibvpxScalar(coeff, nCoeffs, roundFP, quantFP, dequant,
		scan, iscan, qcoeff, dqcoeff)
}

func quantizeFPLibvpxValidated(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	if pixel.Enabled && nCoeffs > 0 && len(iscan) >= nCoeffs && roundFP[0] >= 0 && roundFP[1] >= 0 && quantFP[0] >= 0 && quantFP[1] >= 0 && dequant[0] > 0 && dequant[1] > 0 {
		return quantizeFPGo(coeff, iscan, qcoeff, dqcoeff, nCoeffs, roundFP, quantFP, dequant)
	}

	return quantizeFPLibvpxScalar(coeff, nCoeffs, roundFP, quantFP, dequant,
		scan, iscan, qcoeff, dqcoeff)
}

// Quantization and token-class generation share one SIMD block walk.
func quantizeFPLibvpxValidatedClasses(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16, scan, iscan []int16, qcoeff, dqcoeff []int16, classes []uint8) (int, bool) {
	if pixel.Enabled && nCoeffs > 0 && len(classes) >= nCoeffs && len(iscan) >= nCoeffs && roundFP[0] >= 0 && roundFP[1] >= 0 && quantFP[0] >= 0 && quantFP[1] >= 0 && dequant[0] > 0 && dequant[1] > 0 {
		return pixel.QuantizeFPBlock(coeff, iscan, qcoeff, dqcoeff, nCoeffs, roundFP, quantFP, dequant, classes), true
	}
	return quantizeFPLibvpxValidated(coeff, nCoeffs, roundFP, quantFP, dequant, scan, iscan, qcoeff, dqcoeff), false
}

func quantizeBWithQScanOrderRasterDispatch(coeff []int16, params vp9QuantizeParams,
	dequant [2]int16, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	if pixel.Enabled {
		n := len(coeff)
		if n > 0 {
			eob := pixel.Quantize(coeff[:1], iscan[:1], qcoeffSlice(qcoeff, 0, 1), dqcoeff[:1], 1, params.zbin[0], params.round[0], params.quant[0], params.quantShift[0], int(dequant[0]), false)
			return max(eob, pixel.Quantize(coeff[1:], iscan[1:], qcoeffSlice(qcoeff, 1, n), dqcoeff[1:], n-1, params.zbin[1], params.round[1], params.quant[1], params.quantShift[1], int(dequant[1]), false))
		}
	}

	return quantizeBWithQScanOrderRasterScalar(coeff, params, dequant,
		iscan, qcoeff, dqcoeff)
}

func quantizeBPreferRasterSparseTail(coeff []int16, params vp9QuantizeParams,
	dequant [2]int16, iscan []int16, qcoeff, dqcoeff []int16,
) bool {
	return false
}

func qcoeffSlice(s []int16, a, b int) []int16 {
	if s == nil {
		return nil
	}
	return s[a:b]
}
func quantizeFPGo(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16) int {
	return pixel.QuantizeFPBlock(coeff, iscan, qcoeff, dqcoeff, n, r, q, d, nil)
}

func quantizeBACGo(coeff, iscan, qcoeff, dqcoeff []int16, n, z, r, q, s, d int) int {
	return pixel.Quantize(coeff, iscan, qcoeff, dqcoeff, n, z, r, q, s, d, false)
}
