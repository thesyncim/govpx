// Package dsp holds VP9 profile 0 pixel kernels: inverse 4/8/16/32
// transforms, intra prediction, 8-tap subpel inter-convolve filters,
// deblocking edge filters, SAD, variance, and SSE.
//
// The default build picks the architecture-specific implementation; the
// `purego` build tag selects the scalar Go fallback in this package. All
// kernels are leaf-callable, allocation-free, and operate on caller-owned
// slices.
//
// Upstream:
//
//	libvpx v1.16.0 vpx_dsp/{inv_txfm,intrapred,sad,variance,loopfilter,
//	vpx_convolve}.{c,h}; vp9/common/{vp9_filter,vp9_idct}.{c,h}.
package dsp
