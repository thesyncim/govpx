//go:build goexperiment.simd && amd64 && !purego

// VP8 edge order follows libvpx v1.16.0 vp8/common/loopfilter.c.
package decoder

const macroInternalLoopFilter = true
