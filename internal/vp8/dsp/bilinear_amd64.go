//go:build amd64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// SSE2 port of the libvpx v1.16.0 VP8 bilinear (two-tap) subpel
// predictor. Routes BilinearPredict16x16 and BilinearPredict8x8 (the
// dominant sizes used when bilinear inter-prediction is selected)
// through hand-written SSE2; every other size falls through to the
// scalar reference in subpixel.go.
//
// The kernel decomposes each row of the horizontal/vertical 2-tap
// inner product into PMADDWL pairs over byte sources widened to int16
// lanes, sums to int32, then narrows int32 -> int16 (signed-saturate)
// -> uint8 (unsigned-saturate). Filter coefficients are non-negative
// and sum to 128, so all intermediates fit safely in the signed-int16
// PACKSSLW saturation domain.

//go:noescape
func bilinearPredict16x16SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[2]int16, vFilter *[2]int16, tmp *[17 * 16]byte)

//go:noescape
func bilinearPredict8x8SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[2]int16, vFilter *[2]int16, tmp *[9 * 8]byte)

func bilinearPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [17 * 16]byte
	hFilter := &tables.BilinearFilters[xoffset]
	vFilter := &tables.BilinearFilters[yoffset]
	bilinearPredict16x16SSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func bilinearPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [9 * 8]byte
	hFilter := &tables.BilinearFilters[xoffset]
	vFilter := &tables.BilinearFilters[yoffset]
	bilinearPredict8x8SSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}
