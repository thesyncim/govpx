//go:build arm64 && !purego

package dsp

import "unsafe"

// ARMv8 NEON port of the libvpx v1.16.0
// vpx_dsp/arm/subpel_variance_neon.c second-pass bilinear filter
// specialised to width=16.
//
// varFilterBlock2DBilinearSecondPass16NEON is implemented in
// variance_16x16_arm64.s. It does the same math as
// varFilterBlock2DBilinearSecondPass16Scalar but uses ARMv8 NEON
// (UMULL/UMLAL/RSHRN/UQXTN, encoded via WORD directives because the
// Go arm64 assembler doesn't natively know those mnemonics) to
// process 16 pixels per row in roughly 16 SIMD instructions.
//
// Filter values are passed as uint64 because Go's stack-based ABI
// for assembly passes scalar arguments in machine words; only the
// low 16 bits matter (VDUP from a GP register reads lane-width bits
// and ignores the rest).
//
//go:noescape
func varFilterBlock2DBilinearSecondPass16NEON(src *[17 * 16]uint16, dst *byte, height int, f0 uint64, f1 uint64)

func varFilterBlock2DBilinearSecondPass16(src *[17 * 16]uint16, dst []byte, height int, filter [2]int16) {
	// unsafe.SliceData skips the runtime.panicBounds + stack frame the
	// compiler emits for &dst[0]; the height<=0 guard is dead-code for
	// the only caller (subpelVariance with positive heights).
	varFilterBlock2DBilinearSecondPass16NEON(src, unsafe.SliceData(dst), height, uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}
