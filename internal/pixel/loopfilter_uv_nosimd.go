//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func LoopFilterUV8(_, _ []byte, _, _, _, _, _, _ int) bool { return false }
