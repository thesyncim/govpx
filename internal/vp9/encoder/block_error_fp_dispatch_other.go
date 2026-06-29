//go:build (!amd64 && !arm64) || purego

package encoder

func blockErrorFPDispatch(coeff, dqcoeff []int16, n int) uint64 {
	return blockErrorFPScalar(coeff, dqcoeff, n)
}
