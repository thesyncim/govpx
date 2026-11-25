//go:build !goexperiment.simd || !arm64 || purego

package dsp

func highbdConvolveAvgSIMD(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int, w, h int,
) bool {
	return false
}

func highbdConvolve8HorizSIMD(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	xFilters *[16][8]int16, x0Q4, xStepQ4, w, h, bd int, avg bool,
) bool {
	return false
}

func highbdConvolve8VertSIMD(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	yFilters *[16][8]int16, y0Q4, yStepQ4, w, h, bd int, avg bool,
) bool {
	return false
}

// HighbdSAD computes the sum of absolute differences over n uint16 samples.
func HighbdSAD(a, b []uint16, n int) int {
	sum := 0
	for i := range n {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum
}
