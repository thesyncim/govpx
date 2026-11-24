//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package encoder

func quantizeBlockAllZeroSIMD(_ *[16]int16, _ int) bool { return false }
