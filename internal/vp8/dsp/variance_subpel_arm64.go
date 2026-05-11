//go:build arm64 && !purego

package dsp

import "unsafe"

// ARMv8 NEON ports of the libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// bilinear filter primitives, specialised to widths 8 and 4. The
// 16-wide path keeps its own kernels in variance_firstpass_arm64.s
// and variance_16x16_arm64.s; these helpers cover the remaining sizes
// used by the VP8 inter-mode picker (16x8, 8x16, 8x8, 8x4, 4x8, 4x4).
//
// Wrappers route slice base pointers via unsafe.SliceData so the
// dispatch stays inlineable (no runtime.panicBounds, no stack frame
// for &src[0]). The height<=0 guard is dead-code for the only caller
// (subpelVariance with height in {4,8,16}+1).

//go:noescape
func varFilterBlock2DBilinearFirstPass8NEON(src *byte, srcStride int,
	dst *uint16, height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearFirstPass4NEON(src *byte, srcStride int,
	dst *uint16, height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearSecondPass8NEON(src *uint16, dst *byte,
	height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearSecondPass4NEON(src *uint16, dst *byte,
	height int, f0 uint64, f1 uint64)

func varFilterBlock2DBilinearFirstPass8(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	varFilterBlock2DBilinearFirstPass8NEON(unsafe.SliceData(src), srcStride, &dst[0], height,
		uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}

func varFilterBlock2DBilinearFirstPass4(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	varFilterBlock2DBilinearFirstPass4NEON(unsafe.SliceData(src), srcStride, &dst[0], height,
		uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}

func varFilterBlock2DBilinearSecondPass8(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	varFilterBlock2DBilinearSecondPass8NEON(&src[0], unsafe.SliceData(dst), height,
		uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}

func varFilterBlock2DBilinearSecondPass4(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	varFilterBlock2DBilinearSecondPass4NEON(&src[0], unsafe.SliceData(dst), height,
		uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}
