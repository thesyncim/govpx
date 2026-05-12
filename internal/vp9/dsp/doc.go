// Package dsp holds VP9 pixel kernels: forward/inverse 4/8/16/32 transforms
// (DCT, ADST, IDCT, IADST, IWHT), 10 intra-prediction modes for block
// sizes 4..32, 8-tap subpel inter-convolve filters (regular, smooth,
// sharp) at 1/8 precision, deblocking edge filters (4..16 wide), SAD /
// variance / SSE for all VP9 block sizes (4x4 .. 64x64 plus all
// rectangular splits), and quant/dequant scaling.
//
// The default build picks the architecture-specific implementation; the
// `purego` build tag selects the scalar Go fallback in this package. All
// kernels are leaf-callable, allocation-free, and operate on caller-owned
// slices.
//
// Upstream:
//
//	libvpx v1.16.0 vpx_dsp/{fwd_txfm,inv_txfm,intrapred,sad,variance,
//	loopfilter,vpx_convolve,quantize}.{c,h}; vp9/common/{vp9_filter,
//	vp9_idct}.{c,h}.
package dsp
