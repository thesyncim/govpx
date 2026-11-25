package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func vpxConvolve8AvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
	scratch *Convolve8Scratch,
) {
	_ = y0Q4
	_ = yStepQ4
	_ = scratch
	convolveAvgHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4,
		w, h, srcOffset)
}

func vpxConvolve8AvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
	scratch *Convolve8Scratch,
) {
	_ = x0Q4
	_ = xStepQ4
	_ = scratch
	convolveAvgVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4,
		w, h, srcOffset)
}

func vpxConvolveAvg(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	vpxConvolveAvgScalar(src, srcStride, dst, dstStride, w, h, srcOffset)
}

// Shared convolution entry points select Go SIMD for supported filter
// shapes and retain scalar handling of scaled coordinates.

// VpxConvolve8Horiz applies the horizontal 8-tap subpel filter.
// Mirrors vpx_convolve8_horiz_c.
func VpxConvolve8Horiz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	if pixel.Enabled && xStepQ4 == tables.SubpelShifts && srcOffset >= 0 && (w == 4 || (w > 0 && w%8 == 0)) && h > 0 && srcStride >= 0 && dstStride >= 0 {
		f := &filter[x0Q4]
		if (f[0]|f[1]|f[2]|f[5]|f[6]|f[7]) == 0 && f[3] >= 0 && f[4] >= 0 &&
			len(src)-srcOffset >= (h-1)*srcStride+w+1 && len(dst) >= (h-1)*dstStride+w {
			pixel.FilterBilinear(src[srcOffset:], srcStride, dst, dstStride, w, h, 1, uint8(f[3]), uint8(f[4]))
			return
		}
	}
	convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
}

// VpxConvolve8Vert mirrors vpx_convolve8_vert_c.
func VpxConvolve8Vert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	if pixel.Enabled && yStepQ4 == tables.SubpelShifts && srcOffset >= 0 && (w == 4 || (w > 0 && w%8 == 0)) && h > 0 && srcStride >= 0 && dstStride >= 0 {
		f := &filter[y0Q4]
		if (f[0]|f[1]|f[2]|f[5]|f[6]|f[7]) == 0 && f[3] >= 0 && f[4] >= 0 &&
			len(src)-srcOffset >= h*srcStride+w && len(dst) >= (h-1)*dstStride+w {
			pixel.FilterBilinear(src[srcOffset:], srcStride, dst, dstStride, w, h, srcStride, uint8(f[3]), uint8(f[4]))
			return
		}
	}
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
	if xStepQ4 == tables.SubpelShifts && yStepQ4 == tables.SubpelShifts && srcOffset >= 0 && srcOffset < len(src) {
		fx := &filter[x0Q4]
		fy := &filter[y0Q4]
		if (fx[0]|fx[1]|fx[2]|fx[5]|fx[6]|fx[7]|fy[0]|fy[1]|fy[2]|fy[5]|fy[6]|fy[7]) == 0 &&
			fx[3] >= 0 && fx[4] >= 0 && fy[3] >= 0 && fy[4] >= 0 &&
			pixel.FilterBilinear2D(src[srcOffset:], srcStride, dst, dstStride, w, h,
				uint8(fx[3]), uint8(fx[4]), uint8(fy[3]), uint8(fy[4])) {
			return
		}
	}
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
