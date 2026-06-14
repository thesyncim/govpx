//go:build !arm64 || purego

package dsp

// Ported from libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// var_filter_block2d_bil_w16 specialized for byte output.

func bilinearFilter16x16Horizontal(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	bilinearFilter16x16HorizontalScalar(src, srcStride, dst, height, filter)
}

func bilinearFilter16x16Vertical(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	bilinearFilter16x16VerticalScalar(src, srcStride, dst, height, filter)
}
