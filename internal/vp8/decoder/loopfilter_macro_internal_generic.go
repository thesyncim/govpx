//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

// VP8 edge order follows libvpx v1.16.0 vp8/common/loopfilter.c.
package decoder

// The compile-time guard removes the fused call and its slice construction.
const macroInternalLoopFilter = false
