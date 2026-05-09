package dsp

// TransformBlockError returns the sum of squared int16 differences between
// a 16-element coefficient vector and its dequantized counterpart. Mirrors
// libvpx v1.16.0 vp8_block_error (vp8/encoder/encodemb.c, ERROR_BLOCK
// helper) which computes
//
//	err = SUM_{i in [0,16)} (coeff[i] - dqcoeff[i])^2
//
// over a single 4x4 block's 16 coefficients. Callers (mode-decision RD code)
// rely on byte-identical output vs the scalar reference, so the SIMD
// kernels widen accumulation to int64 to match int(diff)*int(diff) summation
// even though real-world DCT coefficient ranges keep totals well within int32.
func TransformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	return transformBlockError(coeff, dqcoeff)
}

func transformBlockErrorScalar(coeff *[16]int16, dqcoeff *[16]int16) int {
	err := 0
	for i := range 16 {
		diff := int(coeff[i]) - int(dqcoeff[i])
		err += diff * diff
	}
	return err
}
