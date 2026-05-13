//go:build (!arm64 && !amd64) || purego

package dsp

import "unsafe"

// Pure-Go fallback for the residual gather. Mirrors libvpx v1.16.0
// vp8/encoder/encodemb.c (vp8_subtract_mby / vp8_subtract_mbuv) plus the
// govpx-specific block-scan reordering done by
// gatherMacroblockYResiduals4x4Unchecked and
// gatherMacroblockUVResiduals4x4Unchecked.

// ResidualGather16x16PtrFast is the scalar fallback for architectures
// without a SIMD residual-gather kernel. The contract matches the arm64
// NEON implementation.
func ResidualGather16x16PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
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

// ResidualGather8x8PtrFast is the chroma scalar fallback.
func ResidualGather8x8PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
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
