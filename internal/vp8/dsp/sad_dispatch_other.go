package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Shared SAD dispatch mirrors libvpx v1.16.0 vpx_dsp/sad.c semantics.
// The pixel package selects Go SIMD or its scalar fallback.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD16(src, srcStride, ref, refStride, 16)
}

// SAD16x16Fast requires readable 16x16 blocks at the supplied strides.
func SAD16x16Fast(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD16(src, srcStride, ref, refStride, 16)
}

// SAD16x16RowSkip2Fast computes a 2x-decimated 16x16 SAD over the 8 even rows
// and scales the result by 2 for fast HD full-pel motion search.
func SAD16x16RowSkip2Fast(src []byte, srcStride int, ref []byte, refStride int) int {
	return pixel.SAD16(src, srcStride*2, ref, refStride*2, 8) << 1
}

// SAD16x16x4Fast mirrors libvpx's vpx_sad16x16x4d interface.
func SAD16x16x4Fast(src []byte, srcStride int, ref0, ref1, ref2, ref3 []byte, refStride int, out *[4]uint32) {
	pixel.SAD16x4(src, srcStride, ref0, ref1, ref2, ref3, refStride, 16, out)
}

// SAD16x16x4RowSkip2Fast evaluates 4 candidates over the 8 even rows and scales
// each SAD by 2 for fast HD full-pel hex search.
func SAD16x16x4RowSkip2Fast(src []byte, srcStride int, ref0, ref1, ref2, ref3 []byte, refStride int, out *[4]uint32) {
	pixel.SAD16x4(src, srcStride*2, ref0, ref1, ref2, ref3, refStride*2, 8, out)
	out[0] <<= 1
	out[1] <<= 1
	out[2] <<= 1
	out[3] <<= 1
}

// SAD16x16x4LimitFast evaluates each reference with its own row limit.
func SAD16x16x4LimitFast(src []byte, srcStride int, ref0, ref1, ref2, ref3 []byte, refStride int, limits *[4]int32, out *[4]uint32) {
	if pixel.Enabled && limits[0] >= 65280 && limits[1] >= 65280 && limits[2] >= 65280 && limits[3] >= 65280 {
		SAD16x16x4Fast(src, srcStride, ref0, ref1, ref2, ref3, refStride, out)
		return
	}
	out[0] = uint32(SAD16x16LimitFast(src, srcStride, ref0, refStride, int(limits[0])))
	out[1] = uint32(SAD16x16LimitFast(src, srcStride, ref1, refStride, int(limits[1])))
	out[2] = uint32(SAD16x16LimitFast(src, srcStride, ref2, refStride, int(limits[2])))
	out[3] = uint32(SAD16x16LimitFast(src, srcStride, ref3, refStride, int(limits[3])))
}

// SAD16x16LimitFast computes 16x16 SAD with early row exit above limit.
func SAD16x16LimitFast(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	if pixel.Enabled {
		return pixel.SAD16Limit(src, srcStride, ref, refStride, 16, limit)
	}
	return sadBlockLimitScalar(src, srcStride, ref, refStride, 16, 16, limit)
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	return SAD16x16LimitFast(src, srcStride, ref, refStride, limit)
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
	sad := 0
	for y := range height {
		srcRow := src[y*srcStride : y*srcStride+width]
		refRow := ref[y*refStride : y*refStride+width]
		for x := range width {
			diff := int(srcRow[x]) - int(refRow[x])
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
	sad := 0
	for y := range height {
		srcRow := src[y*srcStride : y*srcStride+width]
		refRow := ref[y*refStride : y*refStride+width]
		for x := range width {
			diff := int(srcRow[x]) - int(refRow[x])
			mask := diff >> signShift
			sad += (diff ^ mask) - mask
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}
