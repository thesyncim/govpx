package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"unsafe"
)

// Shared SAD dispatch mirrors libvpx v1.16.0 vpx_dsp/sad.c semantics.
// The pixel package selects Go SIMD or its scalar fallback.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD16(src, srcStride, ref, refStride, 16)
}

// SAD16x16PtrFast requires readable 16x16 blocks at the supplied strides.
func SAD16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	return pixel.SAD16(unsafe.Slice(src, 15*srcStride+16), srcStride, unsafe.Slice(ref, 15*refStride+16), refStride, 16)
}

// SAD16x16x4PtrFast mirrors libvpx's vpx_sad16x16x4d interface.
func SAD16x16x4PtrFast(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32) {
	pixel.SAD16x4(unsafe.Slice(src, 15*srcStride+16), srcStride, unsafe.Slice(ref0, 15*refStride+16), unsafe.Slice(ref1, 15*refStride+16), unsafe.Slice(ref2, 15*refStride+16), unsafe.Slice(ref3, 15*refStride+16), refStride, 16, out)
}

// SAD16x16x4LimitPtrFast evaluates each reference with its own row limit.
func SAD16x16x4LimitPtrFast(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, limits *[4]int32, out *[4]uint32) {
	if pixel.Enabled && limits[0] >= 65280 && limits[1] >= 65280 && limits[2] >= 65280 && limits[3] >= 65280 && sadX4FullDisjoint(src, srcStride, ref0, ref1, ref2, ref3, refStride, limits, out) {
		SAD16x16x4PtrFast(src, srcStride, ref0, ref1, ref2, ref3, refStride, out)
		return
	}
	out[0] = uint32(SAD16x16LimitPtrFast(src, srcStride, ref0, refStride, int(limits[0])))
	out[1] = uint32(SAD16x16LimitPtrFast(src, srcStride, ref1, refStride, int(limits[1])))
	out[2] = uint32(SAD16x16LimitPtrFast(src, srcStride, ref2, refStride, int(limits[2])))
	out[3] = uint32(SAD16x16LimitPtrFast(src, srcStride, ref3, refStride, int(limits[3])))
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	if pixel.Enabled {
		return SAD16x16LimitPtrFast(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride, limit)
	}
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
	if pixel.Enabled && width == 16 && height <= 64 {
		return pixel.SAD16Limit(unsafe.Slice(src, (height-1)*srcStride+16), srcStride, unsafe.Slice(ref, (height-1)*refStride+16), refStride, height, limit)
	}

	if pixel.Enabled {
		sum := 0
		for y := range height {
			a := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(src), y*srcStride)), width)
			b := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(ref), y*refStride)), width)
			sum += pixel.SAD(a, width, b, width, width, 1)
			if sum > limit {
				return sum
			}
		}
		return sum
	}

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
	return pixel.SAD16(src, srcStride, ref, refStride, 8)
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD(src, srcStride, ref, refStride, 8, 16)
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD(src, srcStride, ref, refStride, 8, 8)
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD(src, srcStride, ref, refStride, 4, 4)
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
	if pixel.Enabled {
		return sadBlockLimitScalarPtr(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride, width, height, limit)
	}

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

// Only disjoint output permits computing all four full sums before storing.
// The stride bound proves reconstructed spans fit before multiplication.
func sadX4FullDisjoint(src *byte, ss int, r0, r1, r2, r3 *byte, rs int, limits *[4]int32, out *[4]uint32) bool {
	if src == nil || r0 == nil || r1 == nil || r2 == nil || r3 == nil || out == nil || (uint(ss)|uint(rs))>>48 != 0 {
		return false
	}
	a := uintptr(unsafe.Pointer(out))
	disjoint := func(p unsafe.Pointer, n uintptr) bool {
		b := uintptr(p)
		if a <= b {
			return b-a >= 16
		}
		return a-b >= n
	}
	return disjoint(unsafe.Pointer(limits), 16) && disjoint(unsafe.Pointer(src), uintptr(15*ss+16)) && disjoint(unsafe.Pointer(r0), uintptr(15*rs+16)) && disjoint(unsafe.Pointer(r1), uintptr(15*rs+16)) && disjoint(unsafe.Pointer(r2), uintptr(15*rs+16)) && disjoint(unsafe.Pointer(r3), uintptr(15*rs+16))
}
