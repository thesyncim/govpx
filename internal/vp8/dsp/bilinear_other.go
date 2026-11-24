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
		pixel.Filter(src, ss, dst, ds, w, h, 1, tables.BilinearFilters[x][:])
		return
	}
	if x == 0 {
		pixel.Filter(src, ss, dst, ds, w, h, ss, tables.BilinearFilters[y][:])
		return
	}
	var tmp [17 * 16]byte
	pixel.Filter(src, ss, tmp[:], w, w, h+1, 1, tables.BilinearFilters[x][:])
	pixel.Filter(tmp[:], w, dst, ds, w, h, w, tables.BilinearFilters[y][:])
}
