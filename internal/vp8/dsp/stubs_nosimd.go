//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package dsp

// libvpx v1.16.0 reconintra4x4.c scalar dispatch.
func intraDirectionalGo(_ int, _ []byte, _ int, _ []byte, _ []byte, _ byte) bool { return false }

// Ported from libvpx v1.16.0 vp8/common/recon.c.
func addResidual4Go(_ []byte, _ int, _ *[16]int16) bool { return false }

// Arithmetic follows libvpx v1.16.0 vp8/common/idctllm.c.
func inverse4Go(_ *[16]int16, _ []byte, _ int, _ []byte, _ int, _ bool, _ []int16) bool { return false }

func dequantInverse4PairGo(_ *[32]int16, _ *[16]int16, _ []byte, _ int) bool { return false }
