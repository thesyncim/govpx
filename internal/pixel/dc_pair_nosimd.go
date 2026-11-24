//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func AddConstantPair4(_ []byte, _ int, _ []byte, _ int, _, _ int16) bool { return false }
