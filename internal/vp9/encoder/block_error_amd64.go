//go:build amd64 && !purego

package encoder

import "unsafe"

func blockErrorFPDispatch(coeff, dqcoeff []int16) uint64 {
	n := min(len(coeff), len(dqcoeff))
	if n < 8 {
		return blockErrorFPScalar(coeff[:n], dqcoeff[:n])
	}
	chunkN := n &^ 7
	err := blockErrorFPSSE2(unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), chunkN)
	if chunkN < n {
		err += blockErrorFPScalar(coeff[chunkN:n], dqcoeff[chunkN:n])
	}
	return err
}

func blockErrorFPWithEnergyDispatch(coeff, dqcoeff []int16) (err, energy uint64) {
	n := min(len(coeff), len(dqcoeff))
	if n < 8 {
		return blockErrorFPWithEnergyScalar(coeff[:n], dqcoeff[:n])
	}
	chunkN := n &^ 7
	err, energy = blockErrorFPWithEnergySSE2(
		unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), chunkN,
	)
	if chunkN < n {
		tailErr, tailEnergy := blockErrorFPWithEnergyScalar(
			coeff[chunkN:n], dqcoeff[chunkN:n],
		)
		err += tailErr
		energy += tailEnergy
	}
	return err, energy
}

//go:noescape
func blockErrorFPSSE2(coeff *int16, dqcoeff *int16, n int) uint64

//go:noescape
func blockErrorFPWithEnergySSE2(coeff *int16, dqcoeff *int16, n int) (err uint64, energy uint64)
