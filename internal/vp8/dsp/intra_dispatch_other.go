package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Pure-Go fallback for VP8 intra-prediction primitives. Mirrors
// libvpx v1.16.0 vp8/common/reconintra.c and vpx_dsp/intrapred.c
// semantics.

func intraDCPredict16x16(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	if pixel.Enabled {
		if pixel.IntraDCFillNative && !upAvailable && !leftAvailable {
			pixel.FillBlock(dst, dstStride, 16, 16, 128)
			return
		}
		if pixel.IntraDCFill(dst, dstStride, above, left, 16, upAvailable, leftAvailable) {
			return
		}
		intraDCGo(dst, dstStride, above, left, 16, upAvailable, leftAvailable)
	} else {
		intraDCPredictScalar(dst, dstStride, above, left, 16, upAvailable, leftAvailable)
	}
}

func intraDCPredict8x8(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	if pixel.Enabled {
		if pixel.IntraDCFillNative && !upAvailable && !leftAvailable {
			pixel.FillBlock(dst, dstStride, 8, 8, 128)
			return
		}
		if pixel.IntraDCFill(dst, dstStride, above, left, 8, upAvailable, leftAvailable) {
			return
		}
		intraDCGo(dst, dstStride, above, left, 8, upAvailable, leftAvailable)
	} else {
		intraDCPredictScalar(dst, dstStride, above, left, 8, upAvailable, leftAvailable)
	}
}

func intraVerticalPredict16x16(dst []byte, dstStride int, above []byte) {
	if pixel.Enabled {
		pixel.RepeatRow(dst, dstStride, above, 16)
	} else {
		intraVerticalPredictScalar(dst, dstStride, above, 16)
	}
}

func intraVerticalPredict8x8(dst []byte, dstStride int, above []byte) {
	if pixel.Enabled {
		pixel.RepeatRow(dst, dstStride, above, 8)
	} else {
		intraVerticalPredictScalar(dst, dstStride, above, 8)
	}
}

func intraHorizontalPredict16x16(dst []byte, dstStride int, left []byte) {
	if pixel.Enabled {
		pixel.Horizontal(dst, dstStride, left, 16)
	} else {
		intraHorizontalPredictScalar(dst, dstStride, left, 16)
	}
}

func intraHorizontalPredict8x8(dst []byte, dstStride int, left []byte) {
	if pixel.Enabled {
		pixel.Horizontal(dst, dstStride, left, 8)
	} else {
		intraHorizontalPredictScalar(dst, dstStride, left, 8)
	}
}

func intraTMPredict16x16(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if pixel.Enabled {
		if pixel.TMNative {
			pixel.TMPredictNative(dst, dstStride, above, left, topLeft, 16)
		} else {
			pixel.TMPredict(dst, dstStride, above, left, topLeft, 16)
		}
	} else {
		intraTMPredictScalar(dst, dstStride, above, left, topLeft, 16)
	}
}

func intraTMPredict8x8(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if pixel.Enabled {
		if pixel.TMNative {
			pixel.TMPredictNative(dst, dstStride, above, left, topLeft, 8)
		} else {
			pixel.TMPredict(dst, dstStride, above, left, topLeft, 8)
		}
	} else {
		intraTMPredictScalar(dst, dstStride, above, left, topLeft, 8)
	}
}

func intraDCGo(dst []byte, ds int, above, left []byte, n int, up, lf bool) {
	dc, sum, count := 128, 0, 0
	if up {
		sum += pixel.Sum(above, n, n, 1)
		count += n
	}
	if lf {
		sum += pixel.Sum(left, n, n, 1)
		count += n
	}
	if count > 0 {
		dc = (sum + count/2) / count
	}
	pixel.FillBlock(dst, ds, n, n, byte(dc))
}
