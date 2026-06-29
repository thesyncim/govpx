//go:build arm64 && !purego

package dsp

import "unsafe"

//go:noescape
func convolveAvgNEON(src *byte, srcStride int, dst *byte, dstStride int, w, h int)

func vpxConvolveAvg(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	if h <= 0 || w <= 0 || w%8 != 0 || srcStride < 0 || srcOffset < 0 {
		vpxConvolveAvgScalar(src, srcStride, dst, dstStride, w, h, srcOffset)
		return
	}
	srcLimit := srcOffset + (h-1)*srcStride + w
	if srcLimit < srcOffset || srcLimit > len(src) || !convolveSimdDstOK(dst, dstStride, w, h) {
		vpxConvolveAvgScalar(src, srcStride, dst, dstStride, w, h, srcOffset)
		return
	}
	convolveAvgNEON(unsafe.SliceData(src[srcOffset:]), srcStride, unsafe.SliceData(dst), dstStride, w, h)
}
