//go:build arm64

package dsp

// libvpx v1.16.0 vpx_dsp/arm/sad_neon.c-style dispatch wrappers.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock16x16NEON(&src[0], srcStride, &ref[0], refStride))
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
