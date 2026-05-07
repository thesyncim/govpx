//go:build arm64

package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// ARMv8 NEON port of the libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c 16x16 path. Routes
// SixTapPredict16x16 (the dominant size in inter-frame
// reconstruction) through hand-written NEON; every other size
// falls through to the scalar reference in subpixel.go.

//go:noescape
func sixTapPredict16x16NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 16]byte)

func sixTapPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [21 * 16]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict16x16NEON(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}
