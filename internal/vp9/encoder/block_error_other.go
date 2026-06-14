//go:build !amd64 || purego

package encoder

func blockErrorFPDispatch(coeff, dqcoeff []int16) uint64 {
	return blockErrorFPScalar(coeff, dqcoeff)
}

func blockErrorFPWithEnergyDispatch(coeff, dqcoeff []int16) (err, energy uint64) {
	return blockErrorFPWithEnergyScalar(coeff, dqcoeff)
}
