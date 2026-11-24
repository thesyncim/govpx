package pixel

// QuantizeVP8Block fuses quantization, dequantization and EOB over one block.
// round and quant retain their full signed scalar domain; the unsigned SIMD
// multiply handles nonnegative tables, including coefficient -32768 exactly.
// Quantized/dequantized output blocks must not overlap each other or tables.
func QuantizeVP8Block(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan *[16]int16) int {
	if Enabled {
		if eob, ok := quantizeVP8SIMD(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan); ok {
			return eob
		}
	}
	VP8QuantScalar(coeff[:], round[:], quant[:], dequant[:], qcoeff[:], dqcoeff[:])
	eob := 0
	for i, v := range qcoeff {
		if v != 0 {
			eob = max(eob, int(iscan[i]))
		}
	}
	return eob
}
