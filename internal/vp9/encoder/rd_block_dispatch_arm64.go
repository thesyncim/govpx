//go:build arm64 && !purego

package encoder

import "unsafe"

func transformBlockEnergyDispatch(coeffs []int16) uint64 {
	n := len(coeffs)
	if n >= 8 && n&7 == 0 {
		return sumSquaresI16NEON(unsafe.SliceData(coeffs), n)
	}
	return transformBlockEnergyScalar(coeffs)
}

func transformBlockErrorDispatch(coeffs, dqcoeffs []int16, n int) uint64 {
	if n >= 8 && n&7 == 0 {
		return blockErrorFPNEON(unsafe.SliceData(coeffs), unsafe.SliceData(dqcoeffs), n)
	}
	return transformBlockErrorScalar(coeffs, dqcoeffs, n)
}

//go:noescape
func blockErrorFPNEON(coeff *int16, dqcoeff *int16, n int) uint64

//go:noescape
func sumSquaresI16NEON(src *int16, n int) uint64
