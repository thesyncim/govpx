//go:build !arm64 && !amd64

package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Pure-Go fallback for the 16-wide first-pass bilinear filter on
// architectures without a NEON port. Mirrors libvpx v1.16.0
// vpx_dsp/variance.c semantics.

func varFilterBlock2DBilinearFirstPass16(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := 0; y < height; y++ {
		srcRow := y * srcStride
		dstRow := y * 16
		for x := 0; x < 16; x++ {
			v := int(src[srcRow+x])*f0 + int(src[srcRow+x+1])*f1
			dst[dstRow+x] = uint16((v + round) >> shift)
		}
	}
}
