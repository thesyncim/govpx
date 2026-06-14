//go:build amd64 && !purego

package dsp

// SSE2 ports of the libvpx v1.16.0 vpx_dsp/x86/subpel_variance_sse2.asm
// bilinear-filter primitives, specialised to widths 8 and 4. The
// 16-wide path keeps its own kernels in variance_firstpass_amd64.s
// and variance_16x16_amd64.s; these helpers cover the remaining sizes
// used by the VP8 inter-mode picker (16x8, 8x16, 8x8, 8x4, 4x8, 4x4).

//go:noescape
func varFilterBlock2DBilinearFirstPass8SSE2(src *byte, srcStride int,
	dst *uint16, height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearFirstPass4SSE2(src *byte, srcStride int,
	dst *uint16, height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearSecondPass8SSE2(src *uint16, dst *byte,
	height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearSecondPass4SSE2(src *uint16, dst *byte,
	height int, f0 uint64, f1 uint64)

func varFilterBlock2DBilinearFirstPass8(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	if !bilinearFilterScratchOK(8, height) || !dspWindowOK(src, srcStride, 9, height) {
		bilinearFirstPassScalar(src, srcStride, dst, 8, height, filter)
		return
	}
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	varFilterBlock2DBilinearFirstPass8SSE2(&src[0], srcStride, &dst[0], height, f0u, f1u)
}

func varFilterBlock2DBilinearFirstPass4(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	if !bilinearFilterScratchOK(4, height) || !dspWindowOK(src, srcStride, 5, height) {
		bilinearFirstPassScalar(src, srcStride, dst, 4, height, filter)
		return
	}
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	varFilterBlock2DBilinearFirstPass4SSE2(&src[0], srcStride, &dst[0], height, f0u, f1u)
}

func varFilterBlock2DBilinearSecondPass8(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	maxInt := int(^uint(0) >> 1)
	if height == maxInt || !bilinearFilterScratchOK(8, height+1) || !dspWindowOK(dst, 8, 8, height) {
		bilinearSecondPassScalar(src, dst, 8, height, filter)
		return
	}
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	varFilterBlock2DBilinearSecondPass8SSE2(&src[0], &dst[0], height, f0u, f1u)
}

func varFilterBlock2DBilinearSecondPass4(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	maxInt := int(^uint(0) >> 1)
	if height == maxInt || !bilinearFilterScratchOK(4, height+1) || !dspWindowOK(dst, 4, 4, height) {
		bilinearSecondPassScalar(src, dst, 4, height, filter)
		return
	}
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	varFilterBlock2DBilinearSecondPass4SSE2(&src[0], &dst[0], height, f0u, f1u)
}
