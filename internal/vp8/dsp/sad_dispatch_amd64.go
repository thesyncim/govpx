//go:build amd64

package dsp

// libvpx v1.16.0 vpx_dsp/x86/sad_sse2.asm-style dispatch wrappers. SSE2 is
// part of the x86-64 baseline so the SIMD entry points are always safe to
// call without runtime detection.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock16x16SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	if limit > 0x7fffffff {
		limit = 0x7fffffff
	}
	if limit < 0 {
		limit = 0
	}
	return int(sadBlock16x16LimitSSE2(&src[0], srcStride, &ref[0], refStride, int32(limit)))
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock16x8SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock8x16SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock8x8SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock4x4SSE2(&src[0], srcStride, &ref[0], refStride))
}
