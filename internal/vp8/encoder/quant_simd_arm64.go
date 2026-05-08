//go:build arm64

package encoder

// NEON port of libvpx v1.16.0 vp8/encoder/arm/neon/fastquantizeb_neon.c
// (vp8_fast_quantize_b_neon). Output is byte-identical to FastQuantizeBlock
// scalar reference for VP8 coefficient ranges.

//go:noescape
func fastQuantizeBlockNEON(coeff *int16, round *int16, quantFast *int16, dequant *int16, qcoeff *int16, dqcoeff *int16) int32

func fastQuantizeBlockSIMD(coeff *[16]int16, quant *BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return int(fastQuantizeBlockNEON(&coeff[0], &quant.Round[0], &quant.QuantFast[0], &quant.Dequant[0], &qcoeff[0], &dqcoeff[0]))
}
