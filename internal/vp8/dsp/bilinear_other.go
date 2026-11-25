package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Pure-Go fallback for BilinearPredict on architectures without a SIMD
// port. Returns false so the generic libvpx v1.16.0 scalar path in
// subpixel.go runs.

func bilinearPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if pixel.Enabled {
		bilinearPredictGo(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 16)
		return true
	}

	return false
}

func bilinearPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if pixel.Enabled {
		bilinearPredictGo(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 8)
		return true
	}

	return false
}

func bilinearPredictGo(src []byte, ss, x, y int, dst []byte, ds, w, h int) {
	if y == 0 {
		fx := tables.BilinearFilters[x]
		pixel.FilterBilinear(src, ss, dst, ds, w, h, 1, uint8(fx[0]), uint8(fx[1]))
		return
	}
	if x == 0 {
		fy := tables.BilinearFilters[y]
		pixel.FilterBilinear(src, ss, dst, ds, w, h, ss, uint8(fy[0]), uint8(fy[1]))
		return
	}
	fx := tables.BilinearFilters[x]
	fy := tables.BilinearFilters[y]
	if pixel.FilterBilinear2D(src, ss, dst, ds, w, h, uint8(fx[0]), uint8(fx[1]), uint8(fy[0]), uint8(fy[1])) {
		return
	}
	bilinearPredict2DFallback(src, ss, dst, ds, w, h, uint8(fx[0]), uint8(fx[1]), uint8(fy[0]), uint8(fy[1]))
}

//go:noinline
func bilinearPredict2DFallback(src []byte, ss int, dst []byte, ds, w, h int, fx0, fx1, fy0, fy1 uint8) {
	var tmp [17 * 16]byte
	pixel.FilterBilinear(src, ss, tmp[:], w, w, h+1, 1, fx0, fx1)
	pixel.FilterBilinear(tmp[:], w, dst, ds, w, h, w, fy0, fy1)
}
