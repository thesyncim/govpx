//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

// Arithmetic follows libvpx v1.16.0.
package encoder

func forward4Go(_ []int16, _ int, _ *[16]int16, _ bool) bool { return false }
