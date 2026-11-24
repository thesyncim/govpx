//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

// Ported from libvpx v1.16.0.
package encoder

func quantizeBlockAllZeroSIMD(_ *[16]int16, _ int) bool { return false }
