//go:build !goexperiment.simd || !arm64 || purego

// Ported from libvpx v1.16.0.
package dsp

func sad4RowsGo(_ []byte, _ int, _ []byte, _ int) (int, bool) { return 0, false }
