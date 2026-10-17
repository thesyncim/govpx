//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func SSEPlane16(_ []byte, _ int, _ []byte, _ int, _, _ int) (uint64, bool) { return 0, false }
