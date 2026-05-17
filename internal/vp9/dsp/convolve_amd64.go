//go:build amd64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 AMD64 SSE2 8-tap horizontal and vertical convolve kernels.
//
// Each kernel handles widths that are multiples of 8 (i.e. 8, 16, 32,
// 64) and arbitrary heights. The kernel uses PMADDWD on int16-widened
// source bytes and the int16 filter to fold 8 taps × 8 outputs into
// fewer SSE2 instructions than a per-mul scalar loop.
//
// The Go wrappers VpxConvolve8Horiz / VpxConvolve8Vert validate
// preconditions (x_step_q4 == SubpelShifts, integral x0_q4>>4 == 0,
// contiguous row window in-bounds) and fall back to the scalar
// reference on a mismatch.

//go:noescape
func convolveHoriz8wSSE2(src *byte, srcStride int, dst *byte, dstStride int,
	filter *int16, w, h int)

//go:noescape
func convolveVert8wSSE2(src *byte, srcStride int, dst *byte, dstStride int,
	filter *int16, w, h int)

// convolveSimdDstOK validates the (w, h) write window for dst is
// in-range. The src window depends on direction and is checked by the
// caller.
func convolveSimdDstOK(dst []byte, dstStride, w, h int) bool {
	if dstStride < 0 {
		return false
	}
	limit := (h-1)*dstStride + w
	return limit >= 0 && limit <= len(dst)
}

// VpxConvolve8Horiz applies the horizontal 8-tap subpel filter.
func VpxConvolve8Horiz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	xFrac := x0Q4 & tables.SubpelMask
	if xStepQ4 != tables.SubpelShifts || (x0Q4>>tables.SubpelBits) != 0 || w%8 != 0 || h <= 0 {
		convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
		return
	}
	srcStart := srcOffset - (tables.SubpelTaps/2 - 1)
	if srcStart < 0 || srcStride < 0 {
		convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
		return
	}
	limit := srcStart + (h-1)*srcStride + w + tables.SubpelTaps - 1
	if limit < srcStart || limit > len(src) {
		convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
		return
	}
	if !convolveSimdDstOK(dst, dstStride, w, h) {
		convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
		return
	}
	filterRow := &filter[xFrac]
	convolveHoriz8wSSE2(
		unsafe.SliceData(src[srcStart:]), srcStride,
		unsafe.SliceData(dst), dstStride,
		&filterRow[0], w, h,
	)
}

// VpxConvolve8Vert applies the vertical 8-tap subpel filter.
func VpxConvolve8Vert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	yFrac := y0Q4 & tables.SubpelMask
	if yStepQ4 != tables.SubpelShifts || (y0Q4>>tables.SubpelBits) != 0 || w%8 != 0 || h <= 0 {
		convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
		return
	}
	srcStart := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	if srcStart < 0 || srcStride < 0 {
		convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
		return
	}
	limit := srcStart + (h+tables.SubpelTaps-2)*srcStride + w
	if limit < srcStart || limit > len(src) {
		convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
		return
	}
	if !convolveSimdDstOK(dst, dstStride, w, h) {
		convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
		return
	}
	filterRow := &filter[yFrac]
	convolveVert8wSSE2(
		unsafe.SliceData(src[srcStart:]), srcStride,
		unsafe.SliceData(dst), dstStride,
		&filterRow[0], w, h,
	)
}

// VpxConvolve8 mirrors vpx_convolve8_c -- full 2-pass subpel filter
// (horizontal then vertical) using SSE2 for both passes when sizes
// align; falls back to scalar otherwise.
func VpxConvolve8(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	// See convolve.go for the pool rationale: avoids the per-call
	// 8.6 KiB memclr Go inserts for stack-local arrays.
	tempBuf := convolve8TempGet()
	temp := tempBuf[:]
	intermediateHeight := (((h-1)*yStepQ4 + y0Q4) >> tables.SubpelBits) + tables.SubpelTaps
	horizSrcOffset := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	VpxConvolve8Horiz(src, srcStride, temp, 64, filter, x0Q4, xStepQ4, y0Q4, yStepQ4, w, intermediateHeight, horizSrcOffset)
	vertSrcOffset := 64 * (tables.SubpelTaps/2 - 1)
	VpxConvolve8Vert(temp, 64, dst, dstStride, filter, x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, vertSrcOffset)
	convolve8TempPut(tempBuf)
}

// VpxConvolve8Avg mirrors vpx_convolve8_avg_c.
func VpxConvolve8Avg(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	tempBuf := convolve8AvgTempGet()
	temp := tempBuf[:]
	VpxConvolve8(src, srcStride, temp, 64, filter, x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset)
	VpxConvolveAvg(temp, 64, dst, dstStride, w, h, 0)
	convolve8AvgTempPut(tempBuf)
}
