package encoder

import "github.com/thesyncim/govpx/internal/pixel"

func transformBlockEnergyDispatch(coeffs []int16) uint64 {
	if pixel.Enabled {
		return pixel.Energy(coeffs)
	}
	return transformBlockEnergyScalar(coeffs)
}

func transformBlockErrorDispatch(coeffs, dqcoeffs []int16, n int) uint64 {
	if pixel.Enabled {
		return uint64(pixel.Error(coeffs, dqcoeffs, n))
	}

	return transformBlockErrorScalar(coeffs, dqcoeffs, n)
}
