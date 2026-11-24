//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package dsp

// Arithmetic follows libvpx v1.16.0 vp8/common/idctllm.c.
func inverse4Go(_ *[16]int16, _ []byte, _ int, _ []byte, _ int, _ bool, _ []int16) bool { return false }

func dequantInverse4PairGo(_ *[32]int16, _ *[16]int16, _ []byte, _ int) bool { return false }
