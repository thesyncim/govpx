//go:build amd64 && !purego

package dsp

import "unsafe"

// SSE2 port of libvpx v1.16.0 vp8/common/x86/iwalsh_sse2.asm
// (vp8_short_inv_walsh4x4_sse2). Output is byte-identical to
// inverseWalsh4x4Scalar for the decoder's coefficient range.

//go:noescape
func inverseWalsh4x4SSE2(input *int16, mbDQCoeff *int16)

func inverseWalsh4x4SIMD(input *[16]int16, mbDQCoeff []int16) {
	// The single bounds-check covers every byte the SSE2 kernel writes
	// (lanes at offsets 0, 16, 32, ..., 240); skipping it past this
	// point lets unsafe.SliceData fold the &mbDQCoeff[0] access.
	_ = mbDQCoeff[15*16]
	inverseWalsh4x4SSE2(&input[0], unsafe.SliceData(mbDQCoeff))
}
