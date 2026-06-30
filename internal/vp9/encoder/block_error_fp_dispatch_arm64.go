//go:build arm64 && !purego

package encoder

import "unsafe"

func blockErrorFPDispatch(coeff, dqcoeff []int16, n int) uint64 {
	if n >= 8 && n&7 == 0 {
		return blockErrorFPNEON(unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), n)
	}
	return blockErrorFPScalar(coeff, dqcoeff, n)
}
