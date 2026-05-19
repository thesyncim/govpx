// Package bitstream implements VP9 boolean range coding and superframe index
// helpers.
//
// VP9 uses an arithmetic coder over a 257-state probability space, derived
// from the same family as VP8's bool coder but with separate end-of-stream
// rules, marker bits, and reference-tile reset semantics. This package
// provides matched reader and writer halves: the reader is used by the
// uncompressed header parser, the compressed header parser, the tile data
// parser, and the residual detokenizer; the writer is used by the
// encoder's frame-pack stage. Superframe helpers parse and write the trailing
// VP9 frame index used to bundle up to eight frames in one packet.
//
// The reader and writer types are designed to be allocation-free in
// steady state — both keep their backing buffer as a caller-owned slice
// and never grow internal storage past the constants in this package.
//
// Upstream:
//
//	libvpx v1.16.0 vpx_dsp/bitreader.{h,c}, vpx_dsp/bitwriter.{h,c}
//	libvpx v1.16.0 vp9/decoder/vp9_decoder.c vp9_parse_superframe_index
//	libvpx v1.16.0 vp9/vp9_cx_iface.c write_superframe_index
package bitstream
