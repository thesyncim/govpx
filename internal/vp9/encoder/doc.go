// Package encoder implements the VP9 encode pipeline.
//
// Components: rate control (CBR, VBR, CQ, Q; one-pass and two-pass),
// frame-type selection (key / inter / arnr / golden / altref / overlay /
// invisible), partition search across BLOCK_4x4..BLOCK_64x64, intra and
// inter mode RD with reference frame selection, full-pel and subpel
// motion search (NSTEP / DIAMOND / HEX / BIGDIA / SQUARE / FAST_HEX), 8/16
// /32 transforms with quant and reconstruct, segmentation (cyclic
// refresh, AQ), in-loop deblock with libvpx's lf-pick, scene-cut, alt-ref
// temporal filter, lookahead, denoiser, ROI map, and the bitstream pack
// stage.
//
// As with VP8, hot encode paths are zero-allocation after init: per-frame
// allocations happen in Reset and EncoderOptions changes, not in steady
// state EncodeInto.
//
// Upstream:
//
//	libvpx v1.16.0 vp9/encoder/*.{c,h}
package encoder
