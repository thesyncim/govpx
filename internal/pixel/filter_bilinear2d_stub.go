//go:build !goexperiment.simd || !arm64 || purego

package pixel

func FilterBilinear2D(_ []byte, _ int, _ []byte, _, _, _ int, _, _, _, _ uint8) bool {
	return false
}

func FilterBilinear2DPair8x8(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _, _, _, _ uint8) bool {
	return false
}

func FilterHalfPelDisjoint(src []byte, ss int, dst []byte, ds, w, h, step int) {
	FilterBilinear(src, ss, dst, ds, w, h, step, 64, 64)
}
