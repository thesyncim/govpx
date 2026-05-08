//go:build arm64

package encoder

// NEON batched port of libvpx v1.16.0
// vp8/encoder/arm/neon/fastquantizeb_neon.c vp8_fast_quantize_b_neon.
// Same per-block kernel as fastQuantizeBlockNEON, but the quant
// tables (round / quantFast / dequant / inv-zigzag) are loaded once
// before the loop so processing N blocks costs N kernels plus one
// table-load setup.

//go:noescape
func fastQuantizeBlockBatchNEON(coeff *int16, round *int16, quantFast *int16, dequant *int16, qcoeff *int16, dqcoeff *int16, eobs *uint8, count int)

func fastQuantizeBlockBatchSIMD(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	if count <= 0 {
		return
	}
	fastQuantizeBlockBatchNEON(&coeff[0], &quant.Round[0], &quant.QuantFast[0], &quant.Dequant[0], &qcoeff[0], &dqcoeff[0], &eobs[0], count)
}
