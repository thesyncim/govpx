//go:build amd64

package dsp

import "github.com/thesyncim/govpx/internal/cpu"

// SSE2 / AVX2 ports of the libvpx v1.16.0 vpx_dsp/variance.c
// second-pass bilinear filter, specialised to width=16. Same math as
// varFilterBlock2DBilinearSecondPass16Scalar but processes 16 pixels
// per row in a handful of SIMD instructions: PMULLW (low 16 of
// u16*u16 is exact for products ≤32640), PADDW (round +64), PSRLW
// (>>7), PACKUSWB (u16 -> u8).
//
// Filter values are passed as uint64 with the 16-bit weight broadcast
// to 4 lanes, so the callee can MOVQ + PSHUFD/VPBROADCASTQ across the
// full XMM/YMM in two instructions.

//go:noescape
func varFilterBlock2DBilinearSecondPass16SSE2(src *[17 * 16]uint16, dst *byte, height int, f0 uint64, f1 uint64)

//go:noescape
func varFilterBlock2DBilinearSecondPass16AVX2(src *[17 * 16]uint16, dst *byte, height int, f0 uint64, f1 uint64)

func varFilterBlock2DBilinearSecondPass16(src *[17 * 16]uint16, dst []byte, height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	if cpu.HasAVX2 {
		varFilterBlock2DBilinearSecondPass16AVX2(src, &dst[0], height, f0u, f1u)
		return
	}
	varFilterBlock2DBilinearSecondPass16SSE2(src, &dst[0], height, f0u, f1u)
}
