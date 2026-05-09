//go:build arm64

package dsp

// libvpx v1.16.0 vpx_dsp/arm/sad_neon.c-style dispatch wrappers.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock16x16NEON(&src[0], srcStride, &ref[0], refStride))
}

// SAD16x16PtrFast is the SIMD-bypass entry point for the inter motion
// picker. It assumes the caller has already validated:
//
//   - src and ref point to 16x16 windows fully in-bounds for their
//     respective buffers (`baseY+16 <= height && baseX+16 <= width`)
//   - srcStride and refStride match the stride of the originating slice
//
// The whole-MB SAD result fits in [0, 16*16*255 = 65280] so the int32
// kernel return is always exact (no saturation possible).
func SAD16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	return int(sadBlock16x16NEON(src, srcStride, ref, refStride))
}

// SAD16x16LimitPtrFast is the limited SIMD-bypass entry point. The
// caller must have already validated the in-bounds 16x16 windows AND
// that limit is in [0, 0x7fffffff] (typical motion-search limits are
// at most a few hundred thousand, so this is satisfied trivially by
// the cost-pruned picker walks).
func SAD16x16LimitPtrFast(src *byte, srcStride int, ref *byte, refStride int, limit int) int {
	return int(sadBlock16x16LimitNEON(src, srcStride, ref, refStride, int32(limit)))
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	if limit > 0x7fffffff {
		limit = 0x7fffffff
	}
	if limit < 0 {
		limit = 0
	}
	return int(sadBlock16x16LimitNEON(&src[0], srcStride, &ref[0], refStride, int32(limit)))
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock16x8NEON(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock8x16NEON(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock8x8NEON(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock4x4NEON(&src[0], srcStride, &ref[0], refStride))
}
