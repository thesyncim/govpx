//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// An identity correction needs no reconstruction or strided stores. The
// libvpx rounding formulas are zero for |f|<4 (normal) or |f|<3 (macro).
func uvRoundedZero(f archsimd.Int8x16, kind int) bool {
	bound := int8(4)
	if kind == 6 {
		bound = 3
	}
	return f.Abs().ToBits().Less(archsimd.BroadcastUint8x16(uint8(bound))).ToBits() == 65535
}
