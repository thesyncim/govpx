//go:build amd64 && !purego

package dsp

func subtractBlockNonZeroFast(src []uint8, srcOff, srcStride int,
	pred []uint8, predOff, predStride int,
	out []int16, outOff, outStride int,
	w, h int,
) (bool, bool) {
	return false, false
}
