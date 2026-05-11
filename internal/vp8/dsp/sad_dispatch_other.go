//go:build (!arm64 && !amd64) || purego

package dsp

import "unsafe"

// Pure-Go fallback for the SAD primitives on architectures without a SIMD
// port. Mirrors libvpx v1.16.0 vpx_dsp/sad.c semantics.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 16, 16)
}

// SAD16x16PtrFast is the pointer-form fallback used when callers have
// already validated bounds. See sad_dispatch_arm64.go for the contract.
func SAD16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	return sadBlockScalarPtr(src, srcStride, ref, refStride, 16, 16)
}

// SAD16x16LimitPtrFast is the limited pointer-form fallback. See
// sad_dispatch_arm64.go for the contract.
func SAD16x16LimitPtrFast(src *byte, srcStride int, ref *byte, refStride int, limit int) int {
	return sadBlockLimitScalarPtr(src, srcStride, ref, refStride, 16, 16, limit)
}

// SAD16x16x4PtrFast mirrors libvpx's vpx_sad16x16x4d interface.
func SAD16x16x4PtrFast(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32) {
	out[0] = uint32(sadBlockScalarPtr(src, srcStride, ref0, refStride, 16, 16))
	out[1] = uint32(sadBlockScalarPtr(src, srcStride, ref1, refStride, 16, 16))
	out[2] = uint32(sadBlockScalarPtr(src, srcStride, ref2, refStride, 16, 16))
	out[3] = uint32(sadBlockScalarPtr(src, srcStride, ref3, refStride, 16, 16))
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	return sadBlockLimitScalar(src, srcStride, ref, refStride, 16, 16, limit)
}

func sadBlockScalarPtr(src *byte, srcStride int, ref *byte, refStride int, width, height int) int {
	srcBase := unsafe.Pointer(src)
	refBase := unsafe.Pointer(ref)
	sad := 0
	for y := 0; y < height; y++ {
		srcRow := unsafe.Add(srcBase, y*srcStride)
		refRow := unsafe.Add(refBase, y*refStride)
		for x := 0; x < width; x++ {
			a := int(*(*byte)(unsafe.Add(srcRow, x)))
			b := int(*(*byte)(unsafe.Add(refRow, x)))
			diff := a - b
			mask := diff >> signShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}

func sadBlockLimitScalarPtr(src *byte, srcStride int, ref *byte, refStride int, width, height, limit int) int {
	srcBase := unsafe.Pointer(src)
	refBase := unsafe.Pointer(ref)
	sad := 0
	for y := 0; y < height; y++ {
		srcRow := unsafe.Add(srcBase, y*srcStride)
		refRow := unsafe.Add(refBase, y*refStride)
		for x := 0; x < width; x++ {
			a := int(*(*byte)(unsafe.Add(srcRow, x)))
			b := int(*(*byte)(unsafe.Add(refRow, x)))
			diff := a - b
			mask := diff >> signShift
			sad += (diff ^ mask) - mask
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 16, 8)
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 8, 16)
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 8, 8)
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return sadBlockScalar(src, srcStride, ref, refStride, 4, 4)
}

func sadBlockScalar(src []byte, srcStride int, ref []byte, refStride int, width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	_ = src[(height-1)*srcStride+(width-1)]
	_ = ref[(height-1)*refStride+(width-1)]
	srcBase := unsafe.Pointer(&src[0])
	refBase := unsafe.Pointer(&ref[0])
	sad := 0
	for y := 0; y < height; y++ {
		srcRow := unsafe.Add(srcBase, y*srcStride)
		refRow := unsafe.Add(refBase, y*refStride)
		for x := 0; x < width; x++ {
			a := int(*(*byte)(unsafe.Add(srcRow, x)))
			b := int(*(*byte)(unsafe.Add(refRow, x)))
			diff := a - b
			mask := diff >> signShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}

func sadBlockLimitScalar(src []byte, srcStride int, ref []byte, refStride int, width, height, limit int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	_ = src[(height-1)*srcStride+(width-1)]
	_ = ref[(height-1)*refStride+(width-1)]
	srcBase := unsafe.Pointer(&src[0])
	refBase := unsafe.Pointer(&ref[0])
	sad := 0
	for y := 0; y < height; y++ {
		srcRow := unsafe.Add(srcBase, y*srcStride)
		refRow := unsafe.Add(refBase, y*refStride)
		for x := 0; x < width; x++ {
			a := int(*(*byte)(unsafe.Add(srcRow, x)))
			b := int(*(*byte)(unsafe.Add(refRow, x)))
			diff := a - b
			mask := diff >> signShift
			sad += (diff ^ mask) - mask
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}
