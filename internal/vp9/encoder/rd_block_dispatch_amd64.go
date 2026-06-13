//go:build amd64 && !purego

package encoder

import "unsafe"

func transformBlockErrorDispatch(coeffs, dqcoeffs []int16) uint64 {
	return blockErrorFPDispatch(coeffs, dqcoeffs)
}

func transformBlockEnergyDispatch(coeffs []int16) uint64 {
	return squareSumDispatch(coeffs)
}

func residualSSEDispatch(residue []int16) uint64 {
	return squareSumDispatch(residue)
}

func squareSumDispatch(values []int16) uint64 {
	n := len(values)
	if n < 8 {
		return squareSumScalar(values)
	}
	chunkN := n &^ 7
	sum := squareSumSSE2(unsafe.SliceData(values), chunkN)
	if chunkN < n {
		sum += squareSumScalar(values[chunkN:])
	}
	return sum
}

//go:noescape
func squareSumSSE2(values *int16, n int) uint64
