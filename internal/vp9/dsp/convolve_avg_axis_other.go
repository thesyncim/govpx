//go:build (!amd64 && !arm64) || purego

package dsp

import "github.com/thesyncim/govpx/internal/vp9/tables"

func vpxConvolve8AvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	convolveAvgHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4,
		w, h, srcOffset)
}

func vpxConvolve8AvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	convolveAvgVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4,
		w, h, srcOffset)
}
