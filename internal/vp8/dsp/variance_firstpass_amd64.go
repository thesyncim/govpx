//go:build amd64

package dsp

// SSE2 port of the libvpx v1.16.0 vpx_dsp/variance.c first-pass
// bilinear filter, specialised to width=16. Routes the inner-loop hot
// path used by 16x16 motion search through hand-written SSE2; smaller
// widths fall through to the scalar reference in variance.go.

//go:noescape
func varFilterBlock2DBilinearFirstPass16SSE2(src *byte, srcStride int,
	dst *uint16, height int, f0 uint64, f1 uint64)

func varFilterBlock2DBilinearFirstPass16(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if height <= 0 {
		return
	}
	// Broadcast f0 / f1 to 4 lanes packed in a uint64 so the
	// callee can MOVQ + PSHUFD-broadcast across the full 8-lane XMM.
	f0u := uint64(uint16(filter[0])) * 0x0001000100010001
	f1u := uint64(uint16(filter[1])) * 0x0001000100010001
	varFilterBlock2DBilinearFirstPass16SSE2(&src[0], srcStride, &dst[0], height, f0u, f1u)
}
