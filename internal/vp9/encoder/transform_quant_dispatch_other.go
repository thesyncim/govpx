//go:build (!arm64 && !amd64) || purego

package encoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// Pure-Go fallback dispatchers for the VP9 forward-transform and
// quantizer SIMD entry points. Every entry point routes directly to the
// canonical scalar reference defined in transform_quant.go.

func forwardDCT4x4Dispatch(input []int16, stride int, output []int16) {
	forwardDCT4x4Scalar(input, stride, output)
}

func forwardDCT8x8Dispatch(input []int16, stride int, output []int16) {
	forwardDCT8x8Scalar(input, stride, output)
}

func forwardHT8x8Dispatch(input []int16, stride int, txType common.TxType, output []int16) bool {
	return false
}

func forwardDCT16x16Dispatch(input []int16, stride int, output []int16) {
	forwardDCT16x16Scalar(input, stride, output)
}

func forwardHT16x16Dispatch(input []int16, stride int, txType common.TxType, output []int16) bool {
	return false
}

func forwardDCT32x32Dispatch(input []int16, stride int, output []int16) {
	forwardDCT32x32Scalar(input, stride, output)
}

func forwardDCT32x32RDDispatch(input []int16, stride int, output []int16) {
	forwardDCT32x32RDScalar(input, stride, output)
}

func forwardWHT4x4Dispatch(input []int16, stride int, output []int16) {
	forwardWHT4x4Scalar(input, stride, output)
}

func quantizeFPDispatch(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return quantizeFPScalar(coeff, dequant, scan, dqcoeff)
}

func quantizeFPLibvpxDispatch(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	return quantizeFPLibvpxScalar(coeff, nCoeffs, roundFP, quantFP, dequant,
		scan, iscan, qcoeff, dqcoeff)
}

func quantizeFPLibvpxValidated(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	return quantizeFPLibvpxScalar(coeff, nCoeffs, roundFP, quantFP, dequant,
		scan, iscan, qcoeff, dqcoeff)
}

func quantizeBWithQScanOrderRasterDispatch(coeff []int16, params vp9QuantizeParams,
	dequant [2]int16, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	return quantizeBWithQScanOrderRasterScalar(coeff, params, dequant,
		iscan, qcoeff, dqcoeff)
}

func quantizeBPreferRasterSparseTail(coeff []int16, params vp9QuantizeParams,
	dequant [2]int16, iscan []int16, qcoeff, dqcoeff []int16,
) bool {
	return false
}
