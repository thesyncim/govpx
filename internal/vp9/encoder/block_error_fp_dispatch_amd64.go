//go:build amd64 && !purego

package encoder

import "unsafe"

func blockErrorFPDispatch(coeff, dqcoeff []int16, n int) uint64 {
	if n >= 64 && n&7 == 0 {
		return blockErrorFPSSE2(unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), n)
	}
	return blockErrorFPScalar(coeff, dqcoeff, n)
}

//go:noescape
func blockErrorFPSSE2(coeff *int16, dqcoeff *int16, n int) uint64
