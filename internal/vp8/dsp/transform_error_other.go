package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Scalar fallback for the libvpx v1.16.0 vp8_block_error kernel on
// architectures without a SIMD port.

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	if pixel.Enabled {
		return int(pixel.Error(coeff[:], dqcoeff[:], 16))
	}

	return transformBlockErrorScalar(coeff, dqcoeff)
}
