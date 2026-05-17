//go:build arm64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 ARMv8 NEON 8-tap horizontal and vertical convolve kernels.
//
// Each kernel handles widths that are multiples of 8 (i.e. 8, 16, 32,
// 64) and arbitrary heights. The kernel uses three-same int16x8
// saturating arithmetic, matching libvpx convolve8_8 exactly.
//
// The Go wrappers VpxConvolve8Horiz / VpxConvolve8Vert validate
// preconditions (x_step_q4 == 16, integral x0_q4>>4 == 0, contiguous
// row window in-bounds) and fall back to the scalar reference on a
// mismatch.

//go:noescape
func convolveHoriz8wNEON(src *byte, srcStride int, dst *byte, dstStride int,
	filter *int16, w, h int)

//go:noescape
func convolveVert8wNEON(src *byte, srcStride int, dst *byte, dstStride int,
	filter *int16, w, h int)

// convolveSimdWindowOK validates the (w, h) write window for dst is
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
	// NEON path requires:
	//   - x_step_q4 == SubpelShifts (matches libvpx assumption)
	//   - integral src column index = x0Q4 >> 4 == 0 so dst[y][x] uses
	//     src[y][x + xFrac kernel center]; for the decoder pre-offset
	//     src+srcOffset is the kernel-center base.
	//   - width must be a multiple of 8.
	xFrac := x0Q4 & tables.SubpelMask
	if xStepQ4 != tables.SubpelShifts || (x0Q4>>tables.SubpelBits) != 0 || w%8 != 0 || h <= 0 {
		convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
		return
	}
	// src[y][srcOffset - 3 + x] is the first kernel-window byte for output
	// column x at row y. Validate the entire read window before entering
	// asm.
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
	convolveHoriz8wNEON(
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
	convolveVert8wNEON(
		unsafe.SliceData(src[srcStart:]), srcStride,
		unsafe.SliceData(dst), dstStride,
		&filterRow[0], w, h,
	)
}

// VpxConvolve8AvgHoriz / VpxConvolve8AvgVert keep the scalar
// reference (the rounded-average blend lives in convolve.go); they
// are cold paths used by compound prediction.

// VpxConvolve8 mirrors vpx_convolve8_c -- full 2-pass subpel filter
// (horizontal then vertical) using NEON for both passes when sizes
// align; falls back to scalar otherwise.
func VpxConvolve8(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	// Re-use the scalar dispatcher for setup, but route the inner H/V
	// passes through the NEON paths when each meets its preconditions.
	// Pull the H-V intermediate buffer from a pool so the steady-state
	// path skips Go's mandatory stack-local zero-init (~8.6 KiB / call
	// → ~50ms cumulative on cpu_used=8 RT). libvpx leaves the stack
	// array uninitialized (vpx_dsp/vpx_convolve.c:177) and the H pass
	// fully overwrites every byte before the V pass reads it.
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
