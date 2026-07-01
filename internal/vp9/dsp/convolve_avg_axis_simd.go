//go:build (amd64 || arm64) && !purego

package dsp

import "github.com/thesyncim/govpx/internal/vp9/tables"

func vpxConvolve8AvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	if xStepQ4 != tables.SubpelShifts || (x0Q4>>tables.SubpelBits) != 0 ||
		w <= 0 || h <= 0 || w > 64 || h > 64 || w%8 != 0 {
		convolveAvgHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4,
			w, h, srcOffset)
		return
	}
	tempBuf := convolve8AvgTempGet()
	temp := tempBuf[:]
	VpxConvolve8Horiz(src, srcStride, temp, 64, filter, x0Q4, xStepQ4, 0,
		tables.SubpelShifts, w, h, srcOffset)
	VpxConvolveAvg(temp, 64, dst, dstStride, w, h, 0)
	convolve8AvgTempPut(tempBuf)
}

func vpxConvolve8AvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	if yStepQ4 != tables.SubpelShifts || (y0Q4>>tables.SubpelBits) != 0 ||
		w <= 0 || h <= 0 || w > 64 || h > 64 || w%8 != 0 {
		convolveAvgVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4,
			w, h, srcOffset)
		return
	}
	tempBuf := convolve8AvgTempGet()
	temp := tempBuf[:]
	VpxConvolve8Vert(src, srcStride, temp, 64, filter, 0, tables.SubpelShifts,
		y0Q4, yStepQ4, w, h, srcOffset)
	VpxConvolveAvg(temp, 64, dst, dstStride, w, h, 0)
	convolve8AvgTempPut(tempBuf)
}
