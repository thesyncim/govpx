//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

// Ported from libvpx v1.16.0 vp8/common/recon.c.
package dsp

func addResidual4Go(_ []byte, _ int, _ *[16]int16) bool { return false }
