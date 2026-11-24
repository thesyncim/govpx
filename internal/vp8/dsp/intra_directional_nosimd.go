//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package dsp

// libvpx v1.16.0 reconintra4x4.c scalar dispatch.
func intraDirectionalGo(_ int, _ []byte, _ int, _ []byte, _ []byte, _ byte) bool { return false }
