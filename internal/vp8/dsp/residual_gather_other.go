package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"unsafe"
)

// Shared residual gather dispatch. Mirrors libvpx v1.16.0
// vp8/encoder/encodemb.c (vp8_subtract_mby / vp8_subtract_mbuv) plus the
// govpx-specific block-scan reordering done by
// gatherMacroblockYResiduals4x4Unchecked and
// gatherMacroblockUVResiduals4x4Unchecked.

// ResidualGather16x16PtrFast reads full 16x16 blocks at the supplied
// strides and writes 256 coefficients in 4x4 block-scan order.
func ResidualGather16x16PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
	if pixel.Gather16PtrFast(src, srcStride, pred, predStride, out) {
		return
	}
	if pixel.Enabled {
		pixel.Gather(unsafe.Slice(src, 15*srcStride+16), srcStride, unsafe.Slice(pred, 15*predStride+16), predStride, unsafe.Slice(out, 256), 16)
		return
	}

	srcBase := unsafe.Pointer(src)
	predBase := unsafe.Pointer(pred)
	outBase := unsafe.Pointer(out)
	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			blockOff := (by*4 + bx) * 16 * 2
			srcOff := by*4*srcStride + bx*4
			predOff := by*4*predStride + bx*4
			for r := 0; r < 4; r++ {
				dstRow := unsafe.Add(outBase, blockOff+r*4*2)
				srcRow := unsafe.Add(srcBase, srcOff+r*srcStride)
				predRow := unsafe.Add(predBase, predOff+r*predStride)
				for c := 0; c < 4; c++ {
					a := int(*(*byte)(unsafe.Add(srcRow, c)))
					b := int(*(*byte)(unsafe.Add(predRow, c)))
					*(*int16)(unsafe.Add(dstRow, c*2)) = int16(a - b)
				}
			}
		}
	}
}

// ResidualGather8x8PtrFast gathers an 8x8 chroma block into 64 coefficients.
func ResidualGather8x8PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
	if pixel.Gather8PtrFast(src, srcStride, pred, predStride, out) {
		return
	}
	if pixel.Enabled {
		pixel.Gather(unsafe.Slice(src, 7*srcStride+8), srcStride, unsafe.Slice(pred, 7*predStride+8), predStride, unsafe.Slice(out, 64), 8)
		return
	}

	srcBase := unsafe.Pointer(src)
	predBase := unsafe.Pointer(pred)
	outBase := unsafe.Pointer(out)
	for by := 0; by < 2; by++ {
		for bx := 0; bx < 2; bx++ {
			blockOff := (by*2 + bx) * 16 * 2
			srcOff := by*4*srcStride + bx*4
			predOff := by*4*predStride + bx*4
			for r := 0; r < 4; r++ {
				dstRow := unsafe.Add(outBase, blockOff+r*4*2)
				srcRow := unsafe.Add(srcBase, srcOff+r*srcStride)
				predRow := unsafe.Add(predBase, predOff+r*predStride)
				for c := 0; c < 4; c++ {
					a := int(*(*byte)(unsafe.Add(srcRow, c)))
					b := int(*(*byte)(unsafe.Add(predRow, c)))
					*(*int16)(unsafe.Add(dstRow, c*2)) = int16(a - b)
				}
			}
		}
	}
}
