// Package decoder implements the VP9 decode pipeline.
//
// Stages, in order: superframe split, uncompressed header (frame type,
// profile, bit depth, color space, size, render size, reference frames,
// loop filter, quant params, segmentation, tile info, header size),
// compressed header (tx mode, coef probability updates, MV probability
// updates), per-tile residual + mode decode, transform and reconstruction,
// intra/inter prediction, motion compensation with 8-tap subpel filters,
// and the in-loop deblocking filter.
//
// The decoder is single-threaded by default; the multi-tile / row-thread
// scheduler lives behind an opt-in option that mirrors libvpx's
// frame-parallel and tile-thread modes.
//
// Upstream:
//
//	libvpx v1.16.0 vp9/decoder/{vp9_decodeframe,vp9_decodemv,vp9_decoder,
//	vp9_detokenize,vp9_dsubexp}.{c,h}
package decoder
