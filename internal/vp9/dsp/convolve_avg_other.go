//go:build (!amd64 && !arm64) || purego

package dsp

func vpxConvolveAvg(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	vpxConvolveAvgScalar(src, srcStride, dst, dstStride, w, h, srcOffset)
}
