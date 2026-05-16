package dsp

import (
	"sync"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// convolve8TempBuf is the intermediate-row buffer used by VpxConvolve8 /
// VpxConvolve8Avg between the H and V passes. libvpx declares this as a
// stack-local uint8_t array (vpx_dsp/vpx_convolve.c:177) which has no
// initialization cost in C; Go's `var temp [N]byte` form emits a
// per-call memclr of all 8640 bytes that shows up as ~50ms self-time on
// cpu_used=8 RT for the convolve-heavy realtime inter predictor. Pool
// a reusable buffer so the steady-state path performs no zeroing — the
// H pass always writes the full intermediate region before the V pass
// reads it.
type convolve8TempBuf [64 * 135]byte

var convolve8TempPool = sync.Pool{
	New: func() any { return new(convolve8TempBuf) },
}

// convolve8AvgTempBuf is the smaller scratch for VpxConvolve8Avg.
type convolve8AvgTempBuf [64 * 64]byte

var convolve8AvgTempPool = sync.Pool{
	New: func() any { return new(convolve8AvgTempBuf) },
}

// VP9 8-tap subpel convolve kernels. Ported from libvpx v1.16.0
// vpx_dsp/vpx_convolve.c (the "_c" reference implementations only —
// the SIMD path lives elsewhere). The fractional MV is split into
// (x0_q4, x_step_q4) / (y0_q4, y_step_q4) at 1/16-pel precision; the
// integer part indexes into the source plane and the fractional part
// selects one of 16 InterpKernel rows.

// convolveHoriz applies a single horizontal pass with the supplied
// subpel filter table, stepping the fractional x by xStepQ4 per output
// column. Matches libvpx's convolve_horiz line-for-line; src is biased
// back by SUBPEL_TAPS/2 - 1 so the kernel center aligns with x_q4 >> 4.
func convolveHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	xFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - (tables.SubpelTaps/2 - 1)
	for y := range h {
		xQ4 := x0Q4
		rowSrc := srcStart + y*srcStride
		rowDst := y * dstStride
		for x := range w {
			base := rowSrc + (xQ4 >> tables.SubpelBits)
			filter := &xFilters[xQ4&tables.SubpelMask]
			sum := 0
			for k := range tables.SubpelTaps {
				sum += int(src[base+k]) * int(filter[k])
			}
			dst[rowDst+x] = clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits))
			xQ4 += xStepQ4
		}
	}
}

// convolveVert applies a single vertical pass. Matches convolve_vert in
// libvpx.
func convolveVert(src []byte, srcStride int, dst []byte, dstStride int,
	yFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	y0Q4, yStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	for x := range w {
		yQ4 := y0Q4
		for y := range h {
			base := srcStart + (yQ4>>tables.SubpelBits)*srcStride
			filter := &yFilters[yQ4&tables.SubpelMask]
			sum := 0
			for k := range tables.SubpelTaps {
				sum += int(src[base+k*srcStride]) * int(filter[k])
			}
			dst[y*dstStride+x] = clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits))
			yQ4 += yStepQ4
		}
		srcStart++
	}
}

// convolveAvgHoriz / convolveAvgVert blend the filtered result with the
// pre-existing dst pixel (used when libvpx caller wants 2-reference
// averaging in inter prediction). Matches convolve_avg_horiz/vert.
func convolveAvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	xFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - (tables.SubpelTaps/2 - 1)
	for y := range h {
		xQ4 := x0Q4
		rowSrc := srcStart + y*srcStride
		rowDst := y * dstStride
		for x := range w {
			base := rowSrc + (xQ4 >> tables.SubpelBits)
			filter := &xFilters[xQ4&tables.SubpelMask]
			sum := 0
			for k := range tables.SubpelTaps {
				sum += int(src[base+k]) * int(filter[k])
			}
			c := int(dst[rowDst+x]) + int(clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits)))
			dst[rowDst+x] = uint8((c + 1) >> 1)
			xQ4 += xStepQ4
		}
	}
}

func convolveAvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	yFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	y0Q4, yStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	for x := range w {
		yQ4 := y0Q4
		for y := range h {
			base := srcStart + (yQ4>>tables.SubpelBits)*srcStride
			filter := &yFilters[yQ4&tables.SubpelMask]
			sum := 0
			for k := range tables.SubpelTaps {
				sum += int(src[base+k*srcStride]) * int(filter[k])
			}
			c := int(dst[y*dstStride+x]) + int(clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits)))
			dst[y*dstStride+x] = uint8((c + 1) >> 1)
			yQ4 += yStepQ4
		}
		srcStart++
	}
}

func clipPixel(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// VpxConvolve8AvgHoriz mirrors vpx_convolve8_avg_horiz_c.
func VpxConvolve8AvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	convolveAvgHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
}

// VpxConvolve8AvgVert mirrors vpx_convolve8_avg_vert_c.
func VpxConvolve8AvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	convolveAvgVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
}

// VpxConvolveCopy mirrors vpx_convolve_copy_c — a straight memcpy of
// w x h pixels from src to dst at the given strides.
func VpxConvolveCopy(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	for y := range h {
		copy(dst[y*dstStride:y*dstStride+w], src[srcOffset+y*srcStride:srcOffset+y*srcStride+w])
	}
}

// VpxConvolveAvg mirrors vpx_convolve_avg_c — blend src and dst by
// rounded mean.
func VpxConvolveAvg(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	for y := range h {
		for x := range w {
			c := int(src[srcOffset+y*srcStride+x]) + int(dst[y*dstStride+x])
			dst[y*dstStride+x] = uint8((c + 1) >> 1)
		}
	}
}

// The size-specialized VpxConvolve8Horiz / VpxConvolve8Vert /
// VpxConvolve8 / VpxConvolve8Avg public APIs live in convolve_arm64.go
// (NEON path) and convolve_other.go (scalar fallback). They share the
// scalar helpers above (convolveHoriz, convolveVert, convolveAvgHoriz,
// convolveAvgVert) for the slow paths.
