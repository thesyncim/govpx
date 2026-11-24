//go:build !goexperiment.simd || !amd64 || purego

package encoder

func blockSSEOnlyR2(_ []byte, _, _ int, _ []byte, _, _, _, _ int) (uint64, bool) { return 0, false }
