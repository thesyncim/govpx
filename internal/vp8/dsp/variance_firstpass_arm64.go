//go:build arm64 && !purego

package dsp

import "unsafe"

// ARMv8 NEON port of the libvpx v1.16.0 vpx_dsp/variance.c first-pass
// bilinear filter, specialised to width=16. Routes the inner-loop hot
// path used by 16x16 motion search through hand-written NEON; smaller
// widths fall through to the scalar reference in variance.go.

//go:noescape
func varFilterBlock2DBilinearFirstPass16NEON(src *byte, srcStride int,
	dst *uint16, height int, f0 uint64, f1 uint64)

func varFilterBlock2DBilinearFirstPass16(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	if !bilinearFilterScratchOK(16, height) || !dspWindowOK(src, srcStride, 32, height) {
		bilinearFirstPassScalar(src, srcStride, dst, 16, height, filter)
		return
	}
	// unsafe.SliceData skips the runtime.panicBounds + stack frame the
	// compiler emits for &src[0].
	varFilterBlock2DBilinearFirstPass16NEON(unsafe.SliceData(src), srcStride, &dst[0], height,
		uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}
