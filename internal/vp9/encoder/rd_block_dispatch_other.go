//go:build !arm64 || purego

package encoder

func transformBlockEnergyDispatch(coeffs []int16) uint64 {
	return transformBlockEnergyScalar(coeffs)
}

func transformBlockErrorDispatch(coeffs, dqcoeffs []int16, n int) uint64 {
	return transformBlockErrorScalar(coeffs, dqcoeffs, n)
}
