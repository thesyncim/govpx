//go:build (!amd64 && !arm64) || purego

package dsp

import "github.com/thesyncim/govpx/internal/vp9/tables"

// Scalar fallback for VpxConvolve8Horiz / Vert / 8 / 8Avg. The
// amd64 / arm64 build paths implement these on SSE2 / NEON via
// convolve_amd64.go / convolve_arm64.go.

// VpxConvolve8Horiz applies the horizontal 8-tap subpel filter.
// Mirrors vpx_convolve8_horiz_c.
func VpxConvolve8Horiz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
}

// VpxConvolve8Vert mirrors vpx_convolve8_vert_c.
func VpxConvolve8Vert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
}

// VpxConvolve8 mirrors vpx_convolve8_c — full 2-pass subpel filter
// (horizontal then vertical) with a scratch buffer matching libvpx's
// 64×135 stride-64 intermediate layout.
func VpxConvolve8(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	VpxConvolve8WithScratch(src, srcStride, dst, dstStride, filter,
		x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset, nil)
}

// VpxConvolve8WithScratch is VpxConvolve8 with optional caller-owned scratch.
func VpxConvolve8WithScratch(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
	scratch *Convolve8Scratch,
) {
	temp, tempBuf := convolve8TempForScratch(scratch)
	intermediateHeight := (((h-1)*yStepQ4 + y0Q4) >> tables.SubpelBits) + tables.SubpelTaps
	horizSrcOffset := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	convolveHoriz(src, srcStride, temp, 64, filter, x0Q4, xStepQ4, w, intermediateHeight, horizSrcOffset)
	vertSrcOffset := 64 * (tables.SubpelTaps/2 - 1)
	convolveVert(temp, 64, dst, dstStride, filter, y0Q4, yStepQ4, w, h, vertSrcOffset)
	if tempBuf != nil {
		convolve8TempPut(tempBuf)
	}
}

// VpxConvolve8Avg mirrors vpx_convolve8_avg_c.
func VpxConvolve8Avg(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	VpxConvolve8AvgWithScratch(src, srcStride, dst, dstStride, filter,
		x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset, nil)
}

// VpxConvolve8AvgWithScratch is VpxConvolve8Avg with optional caller-owned scratch.
func VpxConvolve8AvgWithScratch(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
	scratch *Convolve8Scratch,
) {
	temp, tempBuf := convolve8AvgTempForScratch(scratch)
	VpxConvolve8WithScratch(src, srcStride, temp, 64, filter,
		x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset, scratch)
	VpxConvolveAvg(temp, 64, dst, dstStride, w, h, 0)
	if tempBuf != nil {
		convolve8AvgTempPut(tempBuf)
	}
}
