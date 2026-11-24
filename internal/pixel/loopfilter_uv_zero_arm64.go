//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Only identity corrections can skip stores. Unsigned ABS preserves -128.
func uvRoundedZero(f archsimd.Int8x16, kind int) bool {
	bound := uint8(4)
	if kind == 6 {
		bound = 3
	}
	return f.Abs().ToBits().ReduceMax() < bound
}
