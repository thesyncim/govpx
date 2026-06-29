//go:build amd64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// SSE2 port of the libvpx v1.16.0 VP8 six-tap subpel predictor.
// Routes SixTapPredict16x16, SixTapPredict16x8, SixTapPredict8x16,
// SixTapPredict8x8, SixTapPredict8x4, SixTapPredict4x4 through
// hand-written SSE2. The 8x16 form uses a direct 8-wide kernel so
// the overlapping horizontal rows are computed once. The 16x8 form
// still composes two 8x8 calls, matching the scalar block geometry
// without adding a second copy of the 16-wide filter math.
//
// The kernel decomposes the 6-tap horizontal/vertical inner product
// into three PMADDWD pairs over byte sources widened to int16 lanes
// — saturated signed pack to int16, then unsigned pack to uint8.
//
// The 16x16 size also has an AVX2 entry point in subpixel_avx2_amd64.s
// that processes 16 columns per iteration in YMM accumulators; gated
// at runtime via internal/cpu.HasAVX2.

//go:noescape
func sixTapPredict16x16SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 16]byte)

//go:noescape
func sixTapPredict16x16AVX2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 16]byte)

//go:noescape
func sixTapPredict8x8SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[13 * 8]byte)

//go:noescape
func sixTapPredict8x16SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 8]byte)

//go:noescape
func sixTapPredict8x4SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[9 * 8]byte)

//go:noescape
func sixTapPredict4x4SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[9 * 4]byte)

func sixTapPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [21 * 16]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	dstPtr := unsafe.SliceData(dst)
	srcPtr := unsafe.SliceData(src)
	if cpu.HasAVX2 {
		sixTapPredict16x16AVX2(dstPtr, dstStride, srcPtr, srcStride, hFilter, vFilter, &tmp)
		return true
	}
	sixTapPredict16x16SSE2(dstPtr, dstStride, srcPtr, srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict16x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [13 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	dstPtr := unsafe.SliceData(dst)
	srcPtr := unsafe.SliceData(src)
	sixTapPredict8x8SSE2(dstPtr, dstStride, srcPtr, srcStride, hFilter, vFilter, &tmp)
	sixTapPredict8x8SSE2((*byte)(unsafe.Add(unsafe.Pointer(dstPtr), 8)), dstStride,
		(*byte)(unsafe.Add(unsafe.Pointer(srcPtr), 8)), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [21 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x16SSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [13 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x8SSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x8PairMaybe(
	src0 []byte, src0Stride int,
	src1 []byte, src1Stride int,
	xoffset int, yoffset int,
	dst0 []byte, dst0Stride int,
	dst1 []byte, dst1Stride int,
) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if src0Stride <= 0 || src1Stride <= 0 || dst0Stride <= 0 || dst1Stride <= 0 {
		return false
	}
	var tmp [13 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x8SSE2(unsafe.SliceData(dst0), dst0Stride, unsafe.SliceData(src0), src0Stride, hFilter, vFilter, &tmp)
	sixTapPredict8x8SSE2(unsafe.SliceData(dst1), dst1Stride, unsafe.SliceData(src1), src1Stride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x4Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [9 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x4SSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict4x4Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if uint(xoffset) >= 8 || uint(yoffset) >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [9 * 4]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict4x4SSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}
