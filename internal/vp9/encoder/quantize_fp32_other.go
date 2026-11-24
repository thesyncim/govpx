//go:build !goexperiment.simd || !arm64 || purego

package encoder

func quantizeFP32x32RasterSIMD(_ []int16, _ [2]int16, _, _ [2]int,
	_ []int16, _, _ []int16,
) (int, bool) {
	return 0, false
}
