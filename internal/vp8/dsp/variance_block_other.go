package dsp

import "github.com/thesyncim/govpx/internal/pixel"

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum, sse := pixel.Stats(src, srcStride, ref, refStride, 16, 16)
	return sum, int(sse)
}

// VarianceBlock16x16Fast computes 16x16 sum and SSE directly.
func VarianceBlock16x16Fast(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum, sse := pixel.Stats(src, srcStride, ref, refStride, 16, 16)
	return sum, int(sse)
}
