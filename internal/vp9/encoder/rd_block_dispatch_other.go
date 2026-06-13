//go:build !amd64 || purego

package encoder

func transformBlockErrorDispatch(coeffs, dqcoeffs []int16) uint64 {
	return transformBlockErrorScalar(coeffs, dqcoeffs)
}

func transformBlockEnergyDispatch(coeffs []int16) uint64 {
	return transformBlockEnergyScalar(coeffs)
}

func residualSSEDispatch(residue []int16) uint64 {
	return residualSSEScalar(residue)
}
