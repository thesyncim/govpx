package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Pure-Go fallback for the 16-wide second-pass bilinear filter on
// architectures without a NEON port. Mirrors libvpx v1.16.0
// vpx_dsp/variance.c semantics via varFilterBlock2DBilinearSecondPass16Scalar.

func varFilterBlock2DBilinearSecondPass16(src *[17 * 16]uint16, dst []byte, height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearSecond(src[:], dst, 16, height, filter)
		return
	}

	varFilterBlock2DBilinearSecondPass16Scalar(src, dst, height, filter)
}
