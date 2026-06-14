//go:build (!arm64 && !amd64) || purego

package dsp

// Pure-Go fallbacks for the width-8 / width-4 bilinear-filter subpel
// kernels used by SubpelVariance{8x16,8x8,8x4,4x8,4x4}. Mirrors
// libvpx v1.16.0 vpx_dsp/variance.c.

func varFilterBlock2DBilinearFirstPass8(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	bilinearFirstPassScalar(src, srcStride, dst, 8, height, filter)
}

func varFilterBlock2DBilinearFirstPass4(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	bilinearFirstPassScalar(src, srcStride, dst, 4, height, filter)
}

func varFilterBlock2DBilinearSecondPass8(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	bilinearSecondPassScalar(src, dst, 8, height, filter)
}

func varFilterBlock2DBilinearSecondPass4(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	bilinearSecondPassScalar(src, dst, 4, height, filter)
}
